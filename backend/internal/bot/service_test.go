package bot

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stNeweR/family-budget/internal/domain"
	"github.com/stNeweR/family-budget/internal/llm"
	"github.com/stNeweR/family-budget/internal/storetest"
	"github.com/stNeweR/family-budget/internal/telegram"
)

// fakeTelegram records outgoing messages, webhook registrations and polling
// calls. GetUpdates serves queued batches and then blocks until the context is
// cancelled, imitating a long poll with no updates.
type fakeTelegram struct {
	mu             sync.Mutex
	sent           []telegram.SendMessageParams
	webhook        telegram.SetWebhookParams
	deleted        []bool
	deleteFailures int
	batches        [][]telegram.Update
	getUpdates     []telegram.GetUpdatesParams
}

func (f *fakeTelegram) SendMessage(_ context.Context, params telegram.SendMessageParams) (telegram.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, params)
	return telegram.Message{MessageID: 999, Text: params.Text}, nil
}

func (f *fakeTelegram) SetWebhook(_ context.Context, params telegram.SetWebhookParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.webhook = params
	return nil
}

func (f *fakeTelegram) DeleteWebhook(_ context.Context, dropPending bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.deleteFailures > 0 {
		f.deleteFailures--
		return errors.New("network unreachable")
	}
	f.deleted = append(f.deleted, dropPending)
	return nil
}

func (f *fakeTelegram) GetUpdates(ctx context.Context, params telegram.GetUpdatesParams) ([]telegram.Update, error) {
	f.mu.Lock()
	f.getUpdates = append(f.getUpdates, params)
	var batch []telegram.Update
	if len(f.batches) > 0 {
		batch = f.batches[0]
		f.batches = f.batches[1:]
	}
	f.mu.Unlock()

	if batch != nil {
		return batch, nil
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func (f *fakeTelegram) pollCalls() []telegram.GetUpdatesParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]telegram.GetUpdatesParams(nil), f.getUpdates...)
}

func (f *fakeTelegram) lastMessage() telegram.SendMessageParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sent) == 0 {
		return telegram.SendMessageParams{}
	}
	return f.sent[len(f.sent)-1]
}

func newServiceWithSteps(t *testing.T, st *storetest.Memory, tg *fakeTelegram, steps []llm.Message) *Service {
	t.Helper()
	agent := NewAgent(&scriptedLLM{steps: steps}, st)
	agent.now = func() time.Time { return time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC) }
	log := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	return NewService(tg, agent, st, log)
}

func TestWebhookSecretEnforced(t *testing.T) {
	st := storetest.New()
	tg := &fakeTelegram{}
	svc := newServiceWithSteps(t, st, tg, nil)
	handler := svc.WebhookHandler("s3cret")

	cases := []struct {
		name   string
		method string
		secret string
		want   int
	}{
		{"missing secret", http.MethodPost, "", http.StatusUnauthorized},
		{"wrong secret", http.MethodPost, "wrong", http.StatusUnauthorized},
		{"wrong method", http.MethodGet, "s3cret", http.StatusMethodNotAllowed},
		{"valid", http.MethodPost, "s3cret", http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "/telegram/webhook", bytes.NewBufferString(`{}`))
			if tc.secret != "" {
				req.Header.Set(secretHeader, tc.secret)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("got %d, want %d", rec.Code, tc.want)
			}
		})
	}
}

func TestWebhookRejectsBadJSON(t *testing.T) {
	st := storetest.New()
	tg := &fakeTelegram{}
	svc := newServiceWithSteps(t, st, tg, nil)
	handler := svc.WebhookHandler("s3cret")

	req := httptest.NewRequest(http.MethodPost, "/telegram/webhook", bytes.NewBufferString(`not json`))
	req.Header.Set(secretHeader, "s3cret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rec.Code)
	}
}

func TestRegisterWebhook(t *testing.T) {
	st := storetest.New()
	tg := &fakeTelegram{}
	svc := newServiceWithSteps(t, st, tg, nil)

	if err := svc.RegisterWebhook(context.Background(), "https://abc.local/telegram/webhook", "s3cret"); err != nil {
		t.Fatalf("RegisterWebhook: %v", err)
	}
	if tg.webhook.URL != "https://abc.local/telegram/webhook" {
		t.Fatalf("unexpected webhook url %q", tg.webhook.URL)
	}
	if tg.webhook.SecretToken != "s3cret" {
		t.Fatalf("unexpected secret %q", tg.webhook.SecretToken)
	}
}

func TestProcessRepliesToUser(t *testing.T) {
	ctx := t.Context()
	st := storetest.New()
	tg := &fakeTelegram{}
	svc := newServiceWithSteps(t, st, tg, []llm.Message{
		toolCallMessage("c1", toolAddExpense, `{"amount":350,"category":"еда"}`),
		textMessage("Записал 350 ₽."),
	})

	update := telegram.Update{
		UpdateID: 1,
		Message: &telegram.Message{
			MessageID: 42,
			From:      &telegram.User{ID: 1, FirstName: "Мама", Username: "mama"},
			Chat:      telegram.Chat{ID: 100, Type: "private"},
			Text:      "кофе 350",
		},
	}
	svc.process(ctx, update)

	got := tg.lastMessage()
	if got.ChatID != 100 || got.ReplyToMessageID != 42 {
		t.Fatalf("unexpected send params: %+v", got)
	}
	if got.Text != "Записал 350 ₽." {
		t.Fatalf("unexpected reply text: %q", got.Text)
	}

	expenses, err := st.ListExpenses(ctx, domain.ExpenseFilter{})
	if err != nil {
		t.Fatalf("ListExpenses: %v", err)
	}
	if len(expenses) != 1 || expenses[0].UserID != 1 || expenses[0].Amount != 350 {
		t.Fatalf("unexpected expenses: %+v", expenses)
	}

	users, _ := st.ListUsers(ctx)
	if len(users) != 1 || users[0].Username != "mama" || users[0].FullName != "Мама" {
		t.Fatalf("user not registered: %+v", users)
	}
}

func TestStartCommandRegistersUserWithoutLLM(t *testing.T) {
	ctx := t.Context()
	st := storetest.New()
	tg := &fakeTelegram{}
	llmStub := &scriptedLLM{steps: []llm.Message{textMessage("не должно вызываться")}}
	log := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	svc := NewService(tg, NewAgent(llmStub, st), st, log)

	svc.process(ctx, telegram.Update{
		UpdateID: 1,
		Message: &telegram.Message{
			MessageID: 42,
			From:      &telegram.User{ID: 7, FirstName: "Папа", Username: "papa"},
			Chat:      telegram.Chat{ID: 700, Type: "private"},
			Text:      "/start",
		},
	})

	got := tg.lastMessage()
	if got.ChatID != 700 || got.ReplyToMessageID != 42 {
		t.Fatalf("unexpected send params: %+v", got)
	}
	if !strings.Contains(got.Text, "Привет, Папа!") || !strings.Contains(got.Text, "кофе 350") {
		t.Fatalf("unexpected welcome text: %q", got.Text)
	}
	if llmStub.calls != 0 {
		t.Fatalf("LLM must not be called for /start, got %d calls", llmStub.calls)
	}

	users, err := st.ListUsers(ctx)
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 1 || users[0].UserID != 7 || users[0].FullName != "Папа" {
		t.Fatalf("user not registered: %+v", users)
	}
}

func TestProcessIgnoresNonTextMessages(t *testing.T) {
	ctx := t.Context()
	st := storetest.New()
	tg := &fakeTelegram{}
	svc := newServiceWithSteps(t, st, tg, nil)

	// No message at all.
	svc.process(ctx, telegram.Update{UpdateID: 1})

	// Message without a From (e.g. channel post).
	svc.process(ctx, telegram.Update{
		UpdateID: 2,
		Message:  &telegram.Message{MessageID: 1, Chat: telegram.Chat{ID: 1}, Text: "hi"},
	})

	// Non-text message.
	svc.process(ctx, telegram.Update{
		UpdateID: 3,
		Message:  &telegram.Message{MessageID: 1, From: &telegram.User{ID: 1}, Chat: telegram.Chat{ID: 1}},
	})

	if len(tg.sent) != 0 {
		t.Fatalf("expected no messages sent, got %d", len(tg.sent))
	}
}

func TestRunPollingProcessesUpdatesAndAdvancesOffset(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	st := storetest.New()
	tg := &fakeTelegram{batches: [][]telegram.Update{{
		{
			UpdateID: 7,
			Message: &telegram.Message{
				MessageID: 42,
				From:      &telegram.User{ID: 1, FirstName: "Мама", Username: "mama"},
				Chat:      telegram.Chat{ID: 100, Type: "private"},
				Text:      "кофе 350",
			},
		},
	}}}
	svc := newServiceWithSteps(t, st, tg, []llm.Message{
		toolCallMessage("c1", toolAddExpense, `{"amount":350,"category":"еда"}`),
		textMessage("Записал 350 ₽."),
	})

	done := make(chan error, 1)
	go func() { done <- svc.RunPolling(ctx) }()

	waitFor(t, func() bool { return tg.lastMessage().Text == "Записал 350 ₽." })
	cancel()

	if err := <-done; err != nil {
		t.Fatalf("RunPolling: %v", err)
	}

	if len(tg.deleted) != 1 || tg.deleted[0] {
		t.Fatalf("expected DeleteWebhook(false) once, got %v", tg.deleted)
	}

	expenses, err := st.ListExpenses(context.Background(), domain.ExpenseFilter{})
	if err != nil || len(expenses) != 1 {
		t.Fatalf("expense not stored: %+v (err=%v)", expenses, err)
	}
	if expenses[0].UserID != 1 || expenses[0].Amount != 350 {
		t.Fatalf("unexpected expense: %+v", expenses[0])
	}

	calls := tg.pollCalls()
	if len(calls) < 2 {
		t.Fatalf("expected at least 2 getUpdates calls, got %d", len(calls))
	}
	if calls[0].Offset != 0 || calls[0].Limit != pollLimit || calls[0].Timeout != pollTimeoutSeconds {
		t.Fatalf("unexpected first poll params: %+v", calls[0])
	}
	if calls[1].Offset != 8 {
		t.Fatalf("expected offset 8 on second poll, got %d", calls[1].Offset)
	}
}

func TestRunPollingRetriesDeleteWebhook(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	st := storetest.New()
	tg := &fakeTelegram{
		deleteFailures: 1,
		batches: [][]telegram.Update{{
			{
				UpdateID: 1,
				Message: &telegram.Message{
					MessageID: 42,
					From:      &telegram.User{ID: 7, FirstName: "Папа"},
					Chat:      telegram.Chat{ID: 700, Type: "private"},
					Text:      "/start",
				},
			},
		}},
	}
	svc := newServiceWithSteps(t, st, tg, nil)

	done := make(chan error, 1)
	go func() { done <- svc.RunPolling(ctx) }()

	waitFor(t, func() bool { return tg.lastMessage().Text != "" })
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("RunPolling: %v", err)
	}
	if len(tg.deleted) != 1 {
		t.Fatalf("expected deleteWebhook to succeed once after retry, got %v", tg.deleted)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met within timeout")
}
