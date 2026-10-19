package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/stNeweR/family-budget/internal/entity"
	"github.com/stNeweR/family-budget/internal/repo"
	"github.com/stNeweR/family-budget/internal/usecase"
)

const maxSteps = 6

type UseCase struct {
	llm      repo.LLM
	expenses usecase.Expense
	now      func() time.Time
}

func New(client repo.LLM, expenses usecase.Expense) *UseCase {
	return &UseCase{llm: client, expenses: expenses, now: time.Now}
}

func (a *UseCase) Handle(ctx context.Context, user entity.User, text string) (string, error) {
	messages := []entity.Message{
		{Role: entity.RoleSystem, Content: a.systemPrompt(user)},
		{Role: entity.RoleUser, Content: text},
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

		messages = append(messages, entity.Message{
			Role:      entity.RoleAssistant,
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
			messages = append(messages, entity.Message{
				Role:       entity.RoleTool,
				ToolCallID: call.ID,
				Content:    result,
			})
		}
	}

	return "Слишком много шагов обработки. Сформулируйте запрос проще.", nil
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "..."
}

func (a *UseCase) systemPrompt(user entity.User) string {
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
