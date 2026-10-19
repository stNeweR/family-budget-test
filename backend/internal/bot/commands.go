package bot

import (
	"fmt"
	"strings"

	"github.com/stNeweR/family-budget/internal/telegram"
)

// Command is one bot command with the description shown in Telegram clients.
type Command struct {
	Name        string
	Description string
}

// Commands lists every command the bot handles. cmd/botcommands pushes this
// list to Telegram so the client menu stays in sync with the code.
var Commands = []Command{
	{Name: "start", Description: "Зарегистрироваться и создать семью"},
	{Name: "invite", Description: "Ссылка-приглашение в семью"},
	{Name: "panel", Description: "Веб-панель расходов (только для главы)"},
}

// CommandList converts Commands into the Bot API payload.
func CommandList() []telegram.BotCommand {
	commands := make([]telegram.BotCommand, 0, len(Commands))
	for _, c := range Commands {
		commands = append(commands, telegram.BotCommand{Command: c.Name, Description: c.Description})
	}
	return commands
}

// parseCommand extracts a "/command" at the start of the text, strips the
// optional @botname suffix and returns the rest as args. cmd is empty when
// the text is not a command.
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

// commandsHelp is the reply for unknown commands; it lists the command menu.
func commandsHelp() string {
	var sb strings.Builder
	sb.WriteString("Я понимаю обычный текст про траты и команды:\n")
	for _, c := range Commands {
		fmt.Fprintf(&sb, "/%s — %s\n", c.Name, c.Description)
	}
	return strings.TrimRight(sb.String(), "\n")
}
