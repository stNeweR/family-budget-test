package bot

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/stNeweR/family-budget/internal/domain"
	"github.com/stNeweR/family-budget/internal/llm"
	"github.com/stNeweR/family-budget/internal/storetest"
)

// scriptedLLM returns pre-canned assistant messages and records the history it
// was called with, so tests can assert on tool results.
type scriptedLLM struct {
	steps    []llm.Message
	calls    int
	received [][]llm.Message
}

func (s *scriptedLLM) Chat(_ context.Context, messages []llm.Message, _ []llm.Tool) (llm.Message, error) {
	history := make([]llm.Message, len(messages))
	copy(history, messages)
	s.received = append(s.received, history)
	if s.calls >= len(s.steps) {
		return llm.Message{}, errors.New("scriptedLLM: no more steps")
	}
	msg := s.steps[s.calls]
	s.calls++
	return msg, nil
}

func toolCallMessage(id, name, args string) llm.Message {
	return llm.Message{
		Role: llm.RoleAssistant,
		ToolCalls: []llm.ToolCall{{
			ID:       id,
			Type:     "function",
			Function: llm.FunctionCall{Name: name, Arguments: args},
		}},
	}
}

func textMessage(text string) llm.Message {
	return llm.Message{Role: llm.RoleAssistant, Content: text}
}

func newTestAgent(t *testing.T, client llm.ChatClient, st *storetest.Memory, today time.Time) *Agent {
	t.Helper()
	agent := NewAgent(client, st)
	agent.now = func() time.Time { return today }
	return agent
}

func TestHandleAddExpense(t *testing.T) {
	ctx := context.Background()
	st := storetest.New()
	today := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	client := &scriptedLLM{steps: []llm.Message{
		toolCallMessage("call-1", toolAddExpense, `{"amount":350,"category":"еда"}`),
		textMessage("Записал кофе за 350 ₽."),
	}}
	agent := newTestAgent(t, client, st, today)
	user := domain.User{UserID: 1, FullName: "Мама"}

	reply, err := agent.Handle(ctx, user, "кофе 350")
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if reply != "Записал кофе за 350 ₽." {
		t.Fatalf("unexpected reply: %q", reply)
	}

	expenses, err := st.ListExpenses(ctx, domain.ExpenseFilter{UserID: &user.UserID})
	if err != nil {
		t.Fatalf("ListExpenses: %v", err)
	}
	if len(expenses) != 1 {
		t.Fatalf("expected 1 expense, got %d", len(expenses))
	}
	got := expenses[0]
	if got.Amount != 350 || got.Category != domain.CategoryFood {
		t.Fatalf("unexpected expense: %+v", got)
	}
	if got.SpentAt.Format(dateLayout) != today.Format(dateLayout) {
		t.Fatalf("expected spent_at=%s, got %s", today.Format(dateLayout), got.SpentAt.Format(dateLayout))
	}
}

func TestHandleUpdateForeignExpenseIsForbidden(t *testing.T) {
	ctx := context.Background()
	st := storetest.New()
	today := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)

	foreign := domain.Expense{
		UserID:   2,
		Amount:   500,
		Category: domain.CategoryTransport,
		SpentAt:  today,
	}
	if err := st.CreateExpense(ctx, &foreign); err != nil {
		t.Fatalf("seed foreign expense: %v", err)
	}

	client := &scriptedLLM{steps: []llm.Message{
		toolCallMessage("call-1", toolUpdateExpense, `{"id":"`+foreign.ID.String()+`","amount":999}`),
		textMessage("Готово."),
	}}
	agent := newTestAgent(t, client, st, today)
	user := domain.User{UserID: 1, FullName: "Мама"}

	if _, err := agent.Handle(ctx, user, "измени такси"); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	// The tool result must have reported "not found".
	var toolResult string
	for _, msg := range client.received[1] {
		if msg.Role == llm.RoleTool {
			toolResult = msg.Content
		}
	}
	if !strings.Contains(toolResult, "не найдена") {
		t.Fatalf("expected not-found tool result, got %q", toolResult)
	}

	// The foreign expense must be untouched.
	fresh, err := st.GetExpense(ctx, foreign.ID)
	if err != nil {
		t.Fatalf("GetExpense: %v", err)
	}
	if fresh.Amount != 500 {
		t.Fatalf("foreign expense was modified: %+v", fresh)
	}
}

func TestHandleDeleteOwnExpense(t *testing.T) {
	ctx := context.Background()
	st := storetest.New()
	today := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)

	own := domain.Expense{UserID: 1, Amount: 1200, Category: domain.CategoryTransport, SpentAt: today}
	if err := st.CreateExpense(ctx, &own); err != nil {
		t.Fatalf("seed expense: %v", err)
	}

	client := &scriptedLLM{steps: []llm.Message{
		toolCallMessage("call-1", toolDeleteExpense, `{"id":"`+own.ID.String()+`"}`),
		textMessage("Удалил."),
	}}
	agent := newTestAgent(t, client, st, today)
	user := domain.User{UserID: 1}

	if _, err := agent.Handle(ctx, user, "удали такси"); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if _, err := st.GetExpense(ctx, own.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected expense to be deleted, got err=%v", err)
	}
}

func TestHandleListOnlyOwnExpenses(t *testing.T) {
	ctx := context.Background()
	st := storetest.New()
	today := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)

	mine := domain.Expense{UserID: 1, Amount: 100, Category: domain.CategoryFood, SpentAt: today}
	theirs := domain.Expense{UserID: 2, Amount: 200, Category: domain.CategoryFood, SpentAt: today}
	_ = st.CreateExpense(ctx, &mine)
	_ = st.CreateExpense(ctx, &theirs)

	client := &scriptedLLM{steps: []llm.Message{
		toolCallMessage("call-1", toolListExpenses, `{}`),
		textMessage("Вот твои траты."),
	}}
	agent := newTestAgent(t, client, st, today)

	if _, err := agent.Handle(ctx, domain.User{UserID: 1}, "покажи траты"); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	var toolResult string
	for _, msg := range client.received[1] {
		if msg.Role == llm.RoleTool {
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
	cases := map[string]domain.Category{
		"еда":           domain.CategoryFood,
		"Еда":           domain.CategoryFood,
		"food":          domain.CategoryOther,
		"":              domain.CategoryOther,
		"развлечения":   domain.CategoryFun,
		"транспорт":     domain.CategoryTransport,
		"какая-то дичь": domain.CategoryOther,
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
	st := storetest.New()
	steps := make([]llm.Message, 0, maxSteps+1)
	for i := 0; i < maxSteps+1; i++ {
		steps = append(steps, toolCallMessage(uuid.NewString(), toolListExpenses, `{}`))
	}
	agent := newTestAgent(t, &scriptedLLM{steps: steps}, st, time.Now())

	reply, err := agent.Handle(ctx, domain.User{UserID: 1}, "крутись")
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if !strings.Contains(reply, "много шагов") {
		t.Fatalf("expected loop guard message, got %q", reply)
	}
}
