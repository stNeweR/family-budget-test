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
	telegramcontroller "github.com/stNeweR/family-budget/internal/controller/telegram"
	"github.com/stNeweR/family-budget/pkg/postgres"
)

type App struct {
	cfg        config.Config
	pg         *postgres.Postgres
	bot        *telegramcontroller.Service
	httpServer *http.Server
}

func Run(cfg config.Config, log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	application, err := newApp(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer application.Close()

	return application.run(ctx)
}

func (a *App) Close() {
	a.pg.Close()
}

func (a *App) run(ctx context.Context) error {
	var botWG sync.WaitGroup
	runCtx, cancel := context.WithCancel(ctx)
	defer func() {
		cancel()
		botWG.Wait()
	}()

	errCh := make(chan error, 2)

	if a.bot != nil {
		if a.cfg.Telegram.Mode() == "webhook" {
			publicURL := a.cfg.Telegram.WebhookURL + a.cfg.Telegram.WebhookPath
			if err := a.bot.RegisterWebhook(runCtx, publicURL, a.cfg.Telegram.WebhookSecret); err != nil {
				return err
			}
			fmt.Printf("[bot] Telegram webhook зарегистрирован: %s\n", publicURL)
		} else {
			botWG.Add(1)
			go func() {
				defer botWG.Done()
				if err := a.bot.RunPolling(runCtx); err != nil {
					errCh <- err
				}
			}()
		}
	}

	go func() {
		fmt.Printf("[server] HTTP API слушает %s\n", a.cfg.HTTPAddr)
		if err := a.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		fmt.Println("[server] получен сигнал остановки, завершаю работу...")
	}

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()
	if err := a.httpServer.Shutdown(shutdownCtx); err != nil {
		return err
	}
	fmt.Println("[server] сервер остановлен")
	return nil
}
