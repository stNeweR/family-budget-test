package main

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

	"github.com/stNeweR/family-budget/internal/api"
	"github.com/stNeweR/family-budget/internal/bot"
	"github.com/stNeweR/family-budget/internal/config"
	"github.com/stNeweR/family-budget/internal/llm"
	"github.com/stNeweR/family-budget/internal/store"
	"github.com/stNeweR/family-budget/internal/telegram"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	if err := run(log); err != nil {
		log.Error("server stopped with error", "error", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fmt.Println("[server] запуск Family Budget...")
	fmt.Println("[server] подключаюсь к PostgreSQL...")

	st, err := store.NewPostgres(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer st.Close()

	if err := st.CheckSchema(ctx); err != nil {
		return err
	}
	fmt.Println("[server] база данных доступна, схема актуальна")

	opts := []api.Option{}
	errCh := make(chan error, 1)
	var botWG sync.WaitGroup

	if cfg.Telegram.Mode() == "disabled" {
		fmt.Println("[bot] Telegram-бот отключён: не задан TELEGRAM_BOT_TOKEN (панель работает без бота)")
	} else {
		tg := telegram.NewClient(cfg.Telegram.BotToken, telegram.WithBaseURL(cfg.Telegram.APIBaseURL))
		svc := bot.NewService(tg, bot.NewAgent(llm.NewClient(cfg.LLM), st), st, log)

		if cfg.Telegram.Mode() == "webhook" {
			publicURL := cfg.Telegram.WebhookURL + cfg.Telegram.WebhookPath
			if err := svc.RegisterWebhook(ctx, publicURL, cfg.Telegram.WebhookSecret); err != nil {
				return err
			}
			fmt.Printf("[bot] Telegram webhook зарегистрирован: %s\n", publicURL)
			opts = append(opts, api.WithWebhook(cfg.Telegram.WebhookPath, svc.WebhookHandler(cfg.Telegram.WebhookSecret)))
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

	srv := api.New(st, log, opts...)
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
