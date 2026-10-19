// Command botcommands registers the bot's command menu in Telegram so the
// commands and their descriptions are shown in the client UI.
//
// Typical usage:
//
//	go run ./cmd/botcommands        # push the command list from internal/bot
//	go run ./cmd/botcommands -show  # print what is registered right now
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/stNeweR/family-budget/internal/bot"
	"github.com/stNeweR/family-budget/internal/config"
	"github.com/stNeweR/family-budget/internal/telegram"
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
	client := telegram.NewClient(cfg.Telegram.BotToken, telegram.WithBaseURL(cfg.Telegram.APIBaseURL))

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

	commands := bot.CommandList()
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

func printCommands(commands []telegram.BotCommand) {
	for _, c := range commands {
		fmt.Printf("  /%s — %s\n", c.Command, c.Description)
	}
}
