package telegram

import (
	"fmt"
	"strings"

	tgapi "github.com/stNeweR/family-budget/pkg/telegram"
)

type Command struct {
	Name        string
	Description string
}

var Commands = []Command{
	{Name: "start", Description: "Зарегистрироваться и создать семью"},
	{Name: "invite", Description: "Ссылка-приглашение в семью"},
	{Name: "panel", Description: "Веб-панель расходов"},
}

func CommandList() []tgapi.BotCommand {
	commands := make([]tgapi.BotCommand, 0, len(Commands))
	for _, c := range Commands {
		commands = append(commands, tgapi.BotCommand{Command: c.Name, Description: c.Description})
	}
	return commands
}

func parseCommand(text string) (cmd, args string) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/") {
		return "", ""
	}
	head, rest, _ := strings.Cut(text, " ")
	head = strings.TrimPrefix(head, "/")
	if at := strings.Index(head, "@"); at >= 0 {
		head = head[:at]
	}
	return strings.ToLower(head), strings.TrimSpace(rest)
}

func commandsHelp() string {
	var sb strings.Builder
	sb.WriteString("Я понимаю обычный текст про траты и команды:\n")
	for _, c := range Commands {
		fmt.Fprintf(&sb, "/%s — %s\n", c.Name, c.Description)
	}
	return strings.TrimRight(sb.String(), "\n")
}
