package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/stNeweR/family-budget/internal/entity"
	"github.com/stNeweR/family-budget/internal/repo"
	"github.com/stNeweR/family-budget/internal/testsupport"
	expenseuc "github.com/stNeweR/family-budget/internal/usecase/expense"
)

type scriptedLLM struct {
	steps    []entity.Message
	calls    int
	received [][]entity.Message
}

func (s *scriptedLLM) Chat(_ context.Context, messages []entity.Message, _ []entity.Tool) (entity.Message, error) {
	history := make([]entity.Message, len(messages))
	copy(history, messages)
	s.received = append(s.received, history)
	if s.calls >= len(s.steps) {
		return entity.Message{}, errors.New("scriptedLLM: no more steps")
	}
	msg := s.steps[s.calls]
	s.calls++
	return msg, nil
}

func toolCallMessage(id, name, args string) entity.Message {
	return entity.Message{
		Role: entity.RoleAssistant,
		ToolCalls: []entity.ToolCall{{
			ID:       id,
			Type:     "function",
			Function: entity.FunctionCall{Name: name, Arguments: args},
		}},
	}
}

func textMessage(text string) entity.Message {
	return entity.Message{Role: entity.RoleAssistant, Content: text}
}

func newTestAgent(t *testing.T, client repo.LLM, env testsupport.Env, today time.Time) *UseCase {
	t.Helper()
	agent := New(client, expenseuc.New(env.Expenses))
	agent.now = func() time.Time { return today }
	return agent
}

func mustSeedUsers(t *testing.T, env testsupport.Env, ids ...int64) {
	t.Helper()
	for _, id := range ids {
		if err := env.Users.UpsertUser(context.Background(), entity.User{UserID: id, FullName: "Тест"}); err != nil {
			t.Fatalf("seed user %d: %v", id, err)
		}
	}
}

func TestHandleAddExpense(t *testing.T) {
	ctx := context.Background()
	env := testsupport.NewEnv(t)
	today := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	client := &scriptedLLM{steps: []entity.Message{
		toolCallMessage("call-1", toolAddExpense, `{"amount":350,"category":"еда"}`),
		textMessage("Записал кофе за 350 ₽."),
	}}
	agent := newTestAgent(t, client, env, today)
	user := entity.User{UserID: 1, FullName: "Мама"}
	mustSeedUsers(t, env, user.UserID)

	reply, err := agent.Handle(ctx, user, "кофе 350")
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if reply != "Записал кофе за 350 ₽." {
		t.Fatalf("unexpected reply: %q", reply)
	}

	expenses, err := env.Expenses.ListExpenses(ctx, entity.ExpenseFilter{UserID: &user.UserID})
	if err != nil {
		t.Fatalf("ListExpenses: %v", err)
	}
	if len(expenses) != 1 {
		t.Fatalf("expected 1 expense, got %d", len(expenses))
	}
	got := expenses[0]
	if got.Amount != 350 || got.Category != entity.CategoryFood {
		t.Fatalf("unexpected expense: %+v", got)
	}
	if got.SpentAt.Format(dateLayout) != today.Format(dateLayout) {
		t.Fatalf("expected spent_at=%s, got %s", today.Format(dateLayout), got.SpentAt.Format(dateLayout))
	}
}

func TestHandleUpdateForeignExpenseIsForbidden(t *testing.T) {
	ctx := context.Background()
	env := testsupport.NewEnv(t)
	today := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)

	mustSeedUsers(t, env, 1, 2)

	foreign := entity.Expense{
		UserID:   2,
		Amount:   500,
		Category: entity.CategoryTransport,
		SpentAt:  today,
	}
	if err := env.Expenses.CreateExpense(ctx, &foreign); err != nil {
		t.Fatalf("seed foreign expense: %v", err)
	}

	client := &scriptedLLM{steps: []entity.Message{
		toolCallMessage("call-1", toolUpdateExpense, `{"id":"`+foreign.ID.String()+`","amount":999}`),
		textMessage("Готово."),
	}}
	agent := newTestAgent(t, client, env, today)
	user := entity.User{UserID: 1, FullName: "Мама"}

	if _, err := agent.Handle(ctx, user, "измени такси"); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	var toolResult string
	for _, msg := range client.received[1] {
		if msg.Role == entity.RoleTool {
			toolResult = msg.Content
		}
	}
	if !strings.Contains(toolResult, "не найдена") {
		t.Fatalf("expected not-found tool result, got %q", toolResult)
	}

	fresh, err := env.Expenses.GetExpense(ctx, foreign.ID)
	if err != nil {
		t.Fatalf("GetExpense: %v", err)
	}
	if fresh.Amount != 500 {
		t.Fatalf("foreign expense was modified: %+v", fresh)
	}
}

func TestHandleDeleteOwnExpense(t *testing.T) {
	ctx := context.Background()
	env := testsupport.NewEnv(t)
	today := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)

	mustSeedUsers(t, env, 1)

	own := entity.Expense{UserID: 1, Amount: 1200, Category: entity.CategoryTransport, SpentAt: today}
	if err := env.Expenses.CreateExpense(ctx, &own); err != nil {
		t.Fatalf("seed expense: %v", err)
	}

	client := &scriptedLLM{steps: []entity.Message{
		toolCallMessage("call-1", toolDeleteExpense, `{"id":"`+own.ID.String()+`"}`),
		textMessage("Удалил."),
	}}
	agent := newTestAgent(t, client, env, today)
	user := entity.User{UserID: 1}

	if _, err := agent.Handle(ctx, user, "удали такси"); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if _, err := env.Expenses.GetExpense(ctx, own.ID); !errors.Is(err, entity.ErrNotFound) {
		t.Fatalf("expected expense to be deleted, got err=%v", err)
	}
}

func TestHandleListOnlyOwnExpenses(t *testing.T) {
	ctx := context.Background()
	env := testsupport.NewEnv(t)
	today := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)

	mustSeedUsers(t, env, 1, 2)

	mine := entity.Expense{UserID: 1, Amount: 100, Category: entity.CategoryFood, SpentAt: today}
	theirs := entity.Expense{UserID: 2, Amount: 200, Category: entity.CategoryFood, SpentAt: today}
	_ = env.Expenses.CreateExpense(ctx, &mine)
	_ = env.Expenses.CreateExpense(ctx, &theirs)

	client := &scriptedLLM{steps: []entity.Message{
		toolCallMessage("call-1", toolListExpenses, `{}`),
		textMessage("Вот твои траты."),
	}}
	agent := newTestAgent(t, client, env, today)

	if _, err := agent.Handle(ctx, entity.User{UserID: 1}, "покажи траты"); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	var toolResult string
	for _, msg := range client.received[1] {
		if msg.Role == entity.RoleTool {
			toolResult = msg.Content
		}
	}
	if strings.Contains(toolResult, "200.00") {
		t.Fatalf("tool result leaked another user's expense: %q", toolResult)
	}
	if !strings.Contains(toolResult, "100.00") {
		t.Fatalf("tool result should contain own expense: %q", toolResult)
	}
}

func TestNormalizeCategory(t *testing.T) {
	cases := map[string]entity.Category{
		"еда":           entity.CategoryFood,
		"Еда":           entity.CategoryFood,
		"food":          entity.CategoryOther,
		"":              entity.CategoryOther,
		"развлечения":   entity.CategoryFun,
		"транспорт":     entity.CategoryTransport,
		"какая-то дичь": entity.CategoryOther,
	}
	for input, want := range cases {
		if got := normalizeCategory(input); got != want {
			t.Errorf("normalizeCategory(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestParseDate(t *testing.T) {
	fallback := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	if got, err := parseDate("", fallback); err != nil || !got.Equal(fallback) {
		t.Fatalf("empty date should fall back, got %v err=%v", got, err)
	}
	if got, err := parseDate("2026-01-05", fallback); err != nil || got.Format("2006-01-02") != "2026-01-05" {
		t.Fatalf("unexpected parse result %v err=%v", got, err)
	}
	if _, err := parseDate("not-a-date", fallback); err == nil {
		t.Fatal("expected error for invalid date")
	}
}

func TestMaxStepsStopsLoop(t *testing.T) {
	ctx := context.Background()
	env := testsupport.NewEnv(t)
	steps := make([]entity.Message, 0, maxSteps+1)
	for i := 0; i < maxSteps+1; i++ {
		steps = append(steps, toolCallMessage(uuid.NewString(), toolListExpenses, `{}`))
	}
	agent := newTestAgent(t, &scriptedLLM{steps: steps}, env, time.Now())

	reply, err := agent.Handle(ctx, entity.User{UserID: 1}, "крутись")
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if !strings.Contains(reply, "много шагов") {
		t.Fatalf("expected loop guard message, got %q", reply)
	}
}
