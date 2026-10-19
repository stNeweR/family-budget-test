package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
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

	st, err := store.NewPostgres(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer st.Close()

	if err := st.CheckSchema(ctx); err != nil {
		return err
	}

	opts := []api.Option{}

	if cfg.Telegram.Enabled() {
		tg := telegram.NewClient(cfg.Telegram.BotToken, telegram.WithBaseURL(cfg.Telegram.APIBaseURL))
		agent := bot.NewAgent(llm.NewClient(cfg.LLM), st)
		svc := bot.NewService(tg, agent, st, log)

		publicURL := cfg.Telegram.WebhookURL + cfg.Telegram.WebhookPath
		if err := svc.RegisterWebhook(ctx, publicURL, cfg.Telegram.WebhookSecret); err != nil {
			return err
		}
		log.Info("telegram webhook registered", "url", publicURL)

		opts = append(opts, api.WithWebhook(cfg.Telegram.WebhookPath, svc.WebhookHandler(cfg.Telegram.WebhookSecret)))
	} else {
		log.Warn("telegram bot disabled: set TELEGRAM_BOT_TOKEN and TELEGRAM_WEBHOOK_URL")
	}

	srv := api.New(st, log, opts...)
	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("http server listening", "addr", cfg.HTTPAddr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		log.Info("shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return httpServer.Shutdown(shutdownCtx)
}
