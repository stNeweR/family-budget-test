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
	getMeCalls     int
	getMeErr       error
}

func (f *fakeTelegram) GetMe(context.Context) (telegram.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.getMeCalls++
	if f.getMeErr != nil {
		return telegram.User{}, f.getMeErr
	}
	return telegram.User{ID: 42, IsBot: true, Username: "family_budget_bot", FirstName: "Family Budget"}, nil
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
	return NewService(tg, agent, st, log, WithPanelURL("http://panel.test"))
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

func startUpdate(userID int64, text string) telegram.Update {
	return telegram.Update{
		UpdateID: userID,
		Message: &telegram.Message{
			MessageID: 42,
			From:      &telegram.User{ID: userID, FirstName: "Тест"},
			Chat:      telegram.Chat{ID: userID, Type: "private"},
			Text:      text,
		},
	}
}

func seedFamily(t *testing.T, st *storetest.Memory, headID, memberID int64, code string) {
	t.Helper()
	ctx := context.Background()
	if err := st.UpsertUser(ctx, domain.User{UserID: headID, FullName: "Анна"}); err != nil {
		t.Fatalf("upsert head: %v", err)
	}
	if err := st.BecomeHead(ctx, headID, code); err != nil {
		t.Fatalf("become head: %v", err)
	}
	if memberID == 0 {
		return
	}
	if err := st.UpsertUser(ctx, domain.User{UserID: memberID, FullName: "Игорь"}); err != nil {
		t.Fatalf("upsert member: %v", err)
	}
	if err := st.JoinFamily(ctx, memberID, headID); err != nil {
		t.Fatalf("join family: %v", err)
	}
}

func TestStartMakesNewUserHead(t *testing.T) {
	ctx := t.Context()
	st := storetest.New()
	tg := &fakeTelegram{}
	svc := newServiceWithSteps(t, st, tg, nil)

	svc.process(ctx, startUpdate(1, "/start"))

	reply := tg.lastMessage().Text
	if !strings.Contains(reply, "глава семьи") || !strings.Contains(reply, "/invite") {
		t.Fatalf("unexpected reply: %q", reply)
	}
	u, err := st.GetUser(ctx, 1)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if !u.IsHead() {
		t.Fatalf("user must be a head: %+v", u)
	}
	if len(u.InviteCode) != inviteCodeLength {
		t.Fatalf("invite code not generated: %+v", u)
	}
}

func TestStartKeepsMemberRole(t *testing.T) {
	ctx := t.Context()
	st := storetest.New()
	seedFamily(t, st, 1, 2, "HEADCODE01")
	tg := &fakeTelegram{}
	svc := newServiceWithSteps(t, st, tg, nil)

	svc.process(ctx, startUpdate(2, "/start"))

	reply := tg.lastMessage().Text
	if !strings.Contains(reply, "Вы в семье") || !strings.Contains(reply, "глава — Анна") {
		t.Fatalf("unexpected reply: %q", reply)
	}
	u, _ := st.GetUser(ctx, 2)
	if u.InviteUserID == nil || *u.InviteUserID != 1 {
		t.Fatalf("member role changed: %+v", u)
	}
}

func TestStartWithCodeJoinsFamilyAndDropsHeadRole(t *testing.T) {
	ctx := t.Context()
	st := storetest.New()
	seedFamily(t, st, 1, 0, "HEADCODE01")
	// User 2 became a head of a one-person family by accident.
	_ = st.UpsertUser(ctx, domain.User{UserID: 2, FullName: "Игорь"})
	_ = st.BecomeHead(ctx, 2, "OWNCODE001")

	tg := &fakeTelegram{}
	svc := newServiceWithSteps(t, st, tg, nil)
	svc.process(ctx, startUpdate(2, "/start HEADCODE01"))

	reply := tg.lastMessage().Text
	if !strings.Contains(reply, "Теперь вы в семье") {
		t.Fatalf("unexpected reply: %q", reply)
	}
	u, _ := st.GetUser(ctx, 2)
	if u.InviteUserID == nil || *u.InviteUserID != 1 {
		t.Fatalf("user not joined: %+v", u)
	}
	if u.InviteCode != "" {
		t.Fatalf("head credentials must be dropped: %+v", u)
	}
}

func TestStartWithUnknownCodeFallsBackToHead(t *testing.T) {
	ctx := t.Context()
	st := storetest.New()
	tg := &fakeTelegram{}
	svc := newServiceWithSteps(t, st, tg, nil)

	svc.process(ctx, startUpdate(3, "/start NOSUCHCODE"))

	reply := tg.lastMessage().Text
	if !strings.Contains(reply, "недействительна") {
		t.Fatalf("unexpected reply: %q", reply)
	}
	u, _ := st.GetUser(ctx, 3)
	if !u.IsHead() || u.InviteCode == "" {
		t.Fatalf("fallback head not activated: %+v", u)
	}
}

func TestStartWithOwnCode(t *testing.T) {
	ctx := t.Context()
	st := storetest.New()
	seedFamily(t, st, 1, 0, "HEADCODE01")
	tg := &fakeTelegram{}
	svc := newServiceWithSteps(t, st, tg, nil)

	svc.process(ctx, startUpdate(1, "/start HEADCODE01"))

	reply := tg.lastMessage().Text
	if !strings.Contains(reply, "собственная ссылка") {
		t.Fatalf("unexpected reply: %q", reply)
	}
	u, _ := st.GetUser(ctx, 1)
	if u.InviteCode != "HEADCODE01" {
		t.Fatalf("credentials must not change: %+v", u)
	}
}

func TestStartHeadWithMembersCannotJoin(t *testing.T) {
	ctx := t.Context()
	st := storetest.New()
	seedFamily(t, st, 1, 2, "HEADCODE01") // head 1 has member 2
	seedFamily(t, st, 5, 0, "HEADCODE05")

	tg := &fakeTelegram{}
	svc := newServiceWithSteps(t, st, tg, nil)
	svc.process(ctx, startUpdate(1, "/start HEADCODE05"))

	reply := tg.lastMessage().Text
	if !strings.Contains(reply, "нельзя") {
		t.Fatalf("unexpected reply: %q", reply)
	}
	u, _ := st.GetUser(ctx, 1)
	if !u.IsHead() || u.InviteCode != "HEADCODE01" {
		t.Fatalf("head with members must be unchanged: %+v", u)
	}
}

func TestInviteAlwaysPointsToHead(t *testing.T) {
	ctx := t.Context()
	st := storetest.New()
	seedFamily(t, st, 1, 2, "HEADCODE01")
	tg := &fakeTelegram{}
	svc := newServiceWithSteps(t, st, tg, nil)

	svc.process(ctx, startUpdate(1, "/invite"))
	headReply := tg.lastMessage().Text
	if !strings.Contains(headReply, "t.me/family_budget_bot?start=HEADCODE01") {
		t.Fatalf("unexpected head invite: %q", headReply)
	}

	svc.process(ctx, startUpdate(2, "/invite"))
	memberReply := tg.lastMessage().Text
	if !strings.Contains(memberReply, "t.me/family_budget_bot?start=HEADCODE01") {
		t.Fatalf("member invite must point to head code: %q", memberReply)
	}

	if tg.getMeCalls != 1 {
		t.Fatalf("getMe must be cached, got %d calls", tg.getMeCalls)
	}
}

func TestInviteActivatesLegacyHead(t *testing.T) {
	ctx := t.Context()
	st := storetest.New()
	_ = st.UpsertUser(ctx, domain.User{UserID: 7, FullName: "Старый"})
	tg := &fakeTelegram{}
	svc := newServiceWithSteps(t, st, tg, nil)

	svc.process(ctx, startUpdate(7, "/invite"))

	reply := tg.lastMessage().Text
	if !strings.Contains(reply, "t.me/family_budget_bot?start=") {
		t.Fatalf("unexpected reply: %q", reply)
	}
	u, _ := st.GetUser(ctx, 7)
	if u.InviteCode == "" {
		t.Fatalf("invite code not generated for legacy head: %+v", u)
	}
}

func TestPanelLinkForEveryone(t *testing.T) {
	ctx := t.Context()
	st := storetest.New()
	seedFamily(t, st, 1, 2, "HEADCODE01")
	tg := &fakeTelegram{}
	svc := newServiceWithSteps(t, st, tg, nil)

	for _, userID := range []int64{1, 2} {
		svc.process(ctx, startUpdate(userID, "/panel"))
		reply := tg.lastMessage().Text
		if !strings.Contains(reply, "http://panel.test") {
			t.Fatalf("user %d: unexpected panel reply: %q", userID, reply)
		}
		if strings.Contains(reply, "token") {
			t.Fatalf("user %d: panel link must not contain a token: %q", userID, reply)
		}
	}
}

func TestUnknownCommandShowsHelpWithoutLLM(t *testing.T) {
	ctx := t.Context()
	st := storetest.New()
	tg := &fakeTelegram{}
	llmStub := &scriptedLLM{steps: []llm.Message{textMessage("не должно вызываться")}}
	log := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	svc := NewService(tg, NewAgent(llmStub, st), st, log)

	svc.process(ctx, startUpdate(1, "/help"))

	reply := tg.lastMessage().Text
	if !strings.Contains(reply, "/start") || !strings.Contains(reply, "/panel") {
		t.Fatalf("unexpected help reply: %q", reply)
	}
	if llmStub.calls != 0 {
		t.Fatalf("LLM must not be called for commands, got %d calls", llmStub.calls)
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
