package bot

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stNeweR/family-budget/internal/domain"
	"github.com/stNeweR/family-budget/internal/llm"
	"github.com/stNeweR/family-budget/internal/storetest"
	"github.com/stNeweR/family-budget/internal/telegram"
)

// fakeTelegram records outgoing messages and webhook registrations.
type fakeTelegram struct {
	mu      sync.Mutex
	sent    []telegram.SendMessageParams
	webhook telegram.SetWebhookParams
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
	svc.process(update)

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

func TestProcessIgnoresNonTextMessages(t *testing.T) {
	st := storetest.New()
	tg := &fakeTelegram{}
	svc := newServiceWithSteps(t, st, tg, nil)

	// No message at all.
	svc.process(telegram.Update{UpdateID: 1})

	// Message without a From (e.g. channel post).
	svc.process(telegram.Update{
		UpdateID: 2,
		Message:  &telegram.Message{MessageID: 1, Chat: telegram.Chat{ID: 1}, Text: "hi"},
	})

	// Non-text message.
	svc.process(telegram.Update{
		UpdateID: 3,
		Message:  &telegram.Message{MessageID: 1, From: &telegram.User{ID: 1}, Chat: telegram.Chat{ID: 1}},
	})

	if len(tg.sent) != 0 {
		t.Fatalf("expected no messages sent, got %d", len(tg.sent))
	}
}
