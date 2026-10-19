package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/stNeweR/family-budget/config"
	"github.com/stNeweR/family-budget/internal/controller/restapi"
	telegramcontroller "github.com/stNeweR/family-budget/internal/controller/telegram"
	expenserepo "github.com/stNeweR/family-budget/internal/repo/persistent/expense"
	userrepo "github.com/stNeweR/family-budget/internal/repo/persistent/user"
	"github.com/stNeweR/family-budget/internal/repo/webapi/llm"
	agentuc "github.com/stNeweR/family-budget/internal/usecase/agent"
	expenseuc "github.com/stNeweR/family-budget/internal/usecase/expense"
	usersuc "github.com/stNeweR/family-budget/internal/usecase/users"
	"github.com/stNeweR/family-budget/pkg/postgres"
	tgapi "github.com/stNeweR/family-budget/pkg/telegram"
)

func newApp(ctx context.Context, cfg config.Config, log *slog.Logger) (*App, error) {
	fmt.Println("[server] запуск Family Budget...")
	fmt.Println("[server] подключаюсь к PostgreSQL...")

	pg, err := postgres.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}

	if err := pg.CheckSchema(ctx); err != nil {
		pg.Close()
		return nil, err
	}
	fmt.Println("[server] база данных доступна, схема актуальна")

	expensesRepo := expenserepo.New(pg)
	usersRepo := userrepo.New(pg)

	expenses := expenseuc.New(expensesRepo)
	users := usersuc.New(usersRepo)
	agent := agentuc.New(llm.NewClient(cfg.LLM), expenses)

	var bot *telegramcontroller.Service
	opts := []restapi.Option{}

	if cfg.Telegram.Mode() == "disabled" {
		fmt.Println("[bot] Telegram-бот отключён: не задан TELEGRAM_BOT_TOKEN (панель работает без бота)")
	} else {
		tg := tgapi.NewClient(cfg.Telegram.BotToken, tgapi.WithBaseURL(cfg.Telegram.APIBaseURL))
		bot = telegramcontroller.NewService(tg, users, agent, log, telegramcontroller.WithPanelURL(cfg.PanelURL))

		if cfg.Telegram.Mode() == "webhook" {
			opts = append(opts, restapi.WithWebhook(cfg.Telegram.WebhookPath, bot.WebhookHandler(cfg.Telegram.WebhookSecret)))
		}
	}

	srv := restapi.New(expenses, users, pg, opts...)

	return &App{
		cfg: cfg,
		pg:  pg,
		bot: bot,
		httpServer: &http.Server{
			Addr:              cfg.HTTPAddr,
			Handler:           srv.Handler(),
			ReadHeaderTimeout: 10 * time.Second,
		},
	}, nil
}
