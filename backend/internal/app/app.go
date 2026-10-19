package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/stNeweR/family-budget/config"
	"github.com/stNeweR/family-budget/internal/controller/restapi"
	telegramcontroller "github.com/stNeweR/family-budget/internal/controller/telegram"
	expenserepo "github.com/stNeweR/family-budget/internal/repo/persistent/expense"
	userrepo "github.com/stNeweR/family-budget/internal/repo/persistent/user"
	"github.com/stNeweR/family-budget/internal/repo/webapi/llm"
	agentuc "github.com/stNeweR/family-budget/internal/usecase/agent"
	expenseuc "github.com/stNeweR/family-budget/internal/usecase/expense"
	familyuc "github.com/stNeweR/family-budget/internal/usecase/family"
	"github.com/stNeweR/family-budget/pkg/postgres"
	tgapi "github.com/stNeweR/family-budget/pkg/telegram"
)

func Run(cfg config.Config, log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fmt.Println("[server] запуск Family Budget...")
	fmt.Println("[server] подключаюсь к PostgreSQL...")

	pg, err := postgres.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pg.Close()

	if err := pg.CheckSchema(ctx); err != nil {
		return err
	}
	fmt.Println("[server] база данных доступна, схема актуальна")

	expensesRepo := expenserepo.New(pg)
	usersRepo := userrepo.New(pg)

	expenses := expenseuc.New(expensesRepo)
	family := familyuc.New(usersRepo)
	agent := agentuc.New(llm.NewClient(cfg.LLM), expenses)
	services := restapi.Services{Expenses: expenses, Family: family, Pinger: pg}

	opts := []restapi.Option{}
	errCh := make(chan error, 1)
	var botWG sync.WaitGroup

	if cfg.Telegram.Mode() == "disabled" {
		fmt.Println("[bot] Telegram-бот отключён: не задан TELEGRAM_BOT_TOKEN (панель работает без бота)")
	} else {
		tg := tgapi.NewClient(cfg.Telegram.BotToken, tgapi.WithBaseURL(cfg.Telegram.APIBaseURL))
		svc := telegramcontroller.NewService(tg, family, agent, log, telegramcontroller.WithPanelURL(cfg.PanelURL))

		if cfg.Telegram.Mode() == "webhook" {
			publicURL := cfg.Telegram.WebhookURL + cfg.Telegram.WebhookPath
			if err := svc.RegisterWebhook(ctx, publicURL, cfg.Telegram.WebhookSecret); err != nil {
				return err
			}
			fmt.Printf("[bot] Telegram webhook зарегистрирован: %s\n", publicURL)
			opts = append(opts, restapi.WithWebhook(cfg.Telegram.WebhookPath, svc.WebhookHandler(cfg.Telegram.WebhookSecret)))
		} else {
			botWG.Add(1)
			go func() {
				defer botWG.Done()
				if err := svc.RunPolling(ctx); err != nil {
					errCh <- err
				}
			}()
		}
	}

	srv := restapi.New(services, log, opts...)
	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		fmt.Printf("[server] HTTP API слушает %s\n", cfg.HTTPAddr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		fmt.Println("[server] получен сигнал остановки, завершаю работу...")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		return err
	}
	botWG.Wait()
	fmt.Println("[server] сервер остановлен")
	return nil
}
