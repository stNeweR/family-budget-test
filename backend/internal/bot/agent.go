package bot

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/stNeweR/family-budget/internal/domain"
	"github.com/stNeweR/family-budget/internal/llm"
	"github.com/stNeweR/family-budget/internal/store"
)

// maxSteps bounds the tool-calling loop to avoid infinite model loops.
const maxSteps = 6

// Agent turns natural language into expense operations using an LLM with
// function calling. It never trusts the model with identity: the acting user
// is always supplied by the transport layer.
type Agent struct {
	llm   llm.ChatClient
	store store.Store
	now   func() time.Time
}

// NewAgent wires the LLM and storage together.
func NewAgent(client llm.ChatClient, st store.Store) *Agent {
	return &Agent{llm: client, store: st, now: time.Now}
}

// Handle processes one message from a user and returns the reply text.
func (a *Agent) Handle(ctx context.Context, user domain.User, text string) (string, error) {
	messages := []llm.Message{
		{Role: llm.RoleSystem, Content: a.systemPrompt(user)},
		{Role: llm.RoleUser, Content: text},
	}

	for step := 0; step < maxSteps; step++ {
		start := time.Now()
		reply, err := a.llm.Chat(ctx, messages, tools())
		if err != nil {
			return "", err
		}
		fmt.Printf("[agent] шаг %d: ответ LLM за %s (вызовов инструментов: %d)\n",
			step+1, time.Since(start).Round(time.Millisecond), len(reply.ToolCalls))

		if len(reply.ToolCalls) == 0 {
			content := strings.TrimSpace(reply.Content)
			if content == "" {
				return "Не удалось понять запрос. Уточните, пожалуйста.", nil
			}
			return content, nil
		}

		messages = append(messages, llm.Message{
			Role:      llm.RoleAssistant,
			Content:   reply.Content,
			ToolCalls: reply.ToolCalls,
		})

		for _, call := range reply.ToolCalls {
			fmt.Printf("[agent] инструмент %s, аргументы: %s\n", call.Function.Name, clip(call.Function.Arguments, 200))
			result, err := a.executeTool(ctx, user, call.Function.Name, call.Function.Arguments)
			if err != nil {
				fmt.Printf("[agent] ошибка инструмента %s: %v\n", call.Function.Name, err)
				result = fmt.Sprintf("Ошибка при выполнении %s: %v", call.Function.Name, err)
			} else {
				fmt.Printf("[agent] результат %s: %s\n", call.Function.Name, clip(result, 200))
			}
			messages = append(messages, llm.Message{
				Role:       llm.RoleTool,
				ToolCallID: call.ID,
				Content:    result,
			})
		}
	}

	return "Слишком много шагов обработки. Сформулируйте запрос проще.", nil
}

func (a *Agent) systemPrompt(user domain.User) string {
	name := user.FullName
	if name == "" {
		name = user.Username
	}
	return fmt.Sprintf(`Ты — ассистент семейного бюджета. Ты управляешь тратами одного пользователя через функции.

Сегодняшняя дата: %s.
Пользователь: %s (telegram_id=%d).
Категории: %s.

Правила:
- Понимай обычную речь на русском и вызывай нужную функцию.
- Суммы в рублях. Если категория неясна — используй "другое".
- "вчера", "сегодня", "позавчера" и подобное переводи в дату YYYY-MM-DD.
- Чтобы изменить или удалить трату, сначала вызови list_expenses и найди её id, затем update_expense/delete_expense.
- Ты работаешь только с тратами этого пользователя; чужих трат не существует.
- После действий дай краткий ответ на русском: что сделано и итог.`,
		a.today().Format(dateLayout),
		name,
		user.UserID,
		strings.Join(categoryValues(), ", "),
	)
}
