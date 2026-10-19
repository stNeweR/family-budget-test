package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/stNeweR/family-budget/config"
	telegramcontroller "github.com/stNeweR/family-budget/internal/controller/telegram"
	tgapi "github.com/stNeweR/family-budget/pkg/telegram"
)

func main() {
	show := flag.Bool("show", false, "print the commands currently registered in Telegram")
	flag.Parse()

	if err := run(*show); err != nil {
		fmt.Fprintf(os.Stderr, "botcommands: %v\n", err)
		os.Exit(1)
	}
}

func run(show bool) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if !cfg.Telegram.Enabled() {
		return fmt.Errorf("TELEGRAM_BOT_TOKEN is not set")
	}

	ctx := context.Background()
	client := tgapi.NewClient(cfg.Telegram.BotToken, tgapi.WithBaseURL(cfg.Telegram.APIBaseURL))

	if show {
		commands, err := client.GetMyCommands(ctx)
		if err != nil {
			return fmt.Errorf("getMyCommands: %w", err)
		}
		if len(commands) == 0 {
			fmt.Println("в Telegram не зарегистрировано ни одной команды")
			return nil
		}
		fmt.Println("зарегистрированные команды:")
		printCommands(commands)
		return nil
	}

	commands := telegramcontroller.CommandList()
	if len(commands) == 0 {
		return fmt.Errorf("command list is empty")
	}
	if err := client.SetMyCommands(ctx, commands); err != nil {
		return fmt.Errorf("setMyCommands: %w", err)
	}
	fmt.Printf("зарегистрировано команд: %d\n", len(commands))
	printCommands(commands)
	fmt.Println("проверить: go run ./cmd/botcommands -show")
	return nil
}

func printCommands(commands []tgapi.BotCommand) {
	for _, c := range commands {
		fmt.Printf("  /%s — %s\n", c.Command, c.Description)
	}
}
