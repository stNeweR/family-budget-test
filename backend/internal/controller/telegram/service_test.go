package telegram

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

	"github.com/stNeweR/family-budget/internal/entity"
	"github.com/stNeweR/family-budget/internal/testsupport"
	agentuc "github.com/stNeweR/family-budget/internal/usecase/agent"
	expenseuc "github.com/stNeweR/family-budget/internal/usecase/expense"
	usersuc "github.com/stNeweR/family-budget/internal/usecase/users"
	tgapi "github.com/stNeweR/family-budget/pkg/telegram"
)

type fakeTelegram struct {
	mu                 sync.Mutex
	sent               []tgapi.SendMessageParams
	webhook            tgapi.SetWebhookParams
	deleted            []bool
	deletedMessages    []tgapi.DeleteMessageParams
	deleteMessageErr   error
	deleteMessageCalls int
	deleteFailures     int
	batches            [][]tgapi.Update
	getUpdates         []tgapi.GetUpdatesParams
	getMeCalls         int
	getMeErr           error
	nextMessageID      int64
}

func (f *fakeTelegram) GetMe(context.Context) (tgapi.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.getMeCalls++
	if f.getMeErr != nil {
		return tgapi.User{}, f.getMeErr
	}
	return tgapi.User{ID: 42, IsBot: true, Username: "family_budget_bot", FirstName: "Family Budget"}, nil
}

func (f *fakeTelegram) SendMessage(_ context.Context, params tgapi.SendMessageParams) (tgapi.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextMessageID++
	f.sent = append(f.sent, params)
	return tgapi.Message{MessageID: f.nextMessageID, Text: params.Text}, nil
}

func (f *fakeTelegram) DeleteMessage(_ context.Context, params tgapi.DeleteMessageParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleteMessageCalls++
	if f.deleteMessageErr != nil {
		return f.deleteMessageErr
	}
	f.deletedMessages = append(f.deletedMessages, params)
	return nil
}

func (f *fakeTelegram) SetWebhook(_ context.Context, params tgapi.SetWebhookParams) error {
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

func (f *fakeTelegram) GetUpdates(ctx context.Context, params tgapi.GetUpdatesParams) ([]tgapi.Update, error) {
	f.mu.Lock()
	f.getUpdates = append(f.getUpdates, params)
	var batch []tgapi.Update
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

func (f *fakeTelegram) pollCalls() []tgapi.GetUpdatesParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]tgapi.GetUpdatesParams(nil), f.getUpdates...)
}

func (f *fakeTelegram) lastMessage() tgapi.SendMessageParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sent) == 0 {
		return tgapi.SendMessageParams{}
	}
	return f.sent[len(f.sent)-1]
}

func newServiceWithSteps(t *testing.T, env testsupport.Env, tg *fakeTelegram, steps []entity.Message) *Service {
	t.Helper()
	agent := agentuc.New(&scriptedLLM{steps: steps}, expenseuc.New(env.Expenses))
	log := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	return NewService(tg, usersuc.New(env.Users), agent, log, WithPanelURL("http://panel.test"))
}

type scriptedLLM struct {
	steps []entity.Message
	calls int
}

func (s *scriptedLLM) Chat(context.Context, []entity.Message, []entity.Tool) (entity.Message, error) {
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

func TestWebhookSecretEnforced(t *testing.T) {
	env := testsupport.NewEnv(t)
	tg := &fakeTelegram{}
	svc := newServiceWithSteps(t, env, tg, nil)
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
	env := testsupport.NewEnv(t)
	tg := &fakeTelegram{}
	svc := newServiceWithSteps(t, env, tg, nil)
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
	env := testsupport.NewEnv(t)
	tg := &fakeTelegram{}
	svc := newServiceWithSteps(t, env, tg, nil)

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
	env := testsupport.NewEnv(t)
	tg := &fakeTelegram{}
	svc := newServiceWithSteps(t, env, tg, []entity.Message{
		toolCallMessage("c1", "add_expense", `{"amount":350,"category":"еда"}`),
		textMessage("Записал 350 ₽."),
	})

	update := tgapi.Update{
		UpdateID: 1,
		Message: &tgapi.Message{
			MessageID: 42,
			From:      &tgapi.User{ID: 1, FirstName: "Мама", Username: "mama"},
			Chat:      tgapi.Chat{ID: 100, Type: "private"},
			Text:      "кофе 350",
		},
	}
	svc.process(ctx, update)

	if len(tg.sent) != 2 {
		t.Fatalf("expected processing and result messages, got %+v", tg.sent)
	}
	if tg.sent[0].Text != "Запрос в обработке..." || tg.sent[0].ReplyToMessageID != 42 {
		t.Fatalf("unexpected processing message: %+v", tg.sent[0])
	}

	got := tg.lastMessage()
	if got.ChatID != 100 || got.ReplyToMessageID != 42 {
		t.Fatalf("unexpected send params: %+v", got)
	}
	if got.Text != "Записал 350 ₽." {
		t.Fatalf("unexpected reply text: %q", got.Text)
	}

	if len(tg.deletedMessages) != 1 {
		t.Fatalf("expected processing message to be deleted, got %+v", tg.deletedMessages)
	}
	if tg.deletedMessages[0].ChatID != 100 || tg.deletedMessages[0].MessageID != 1 {
		t.Fatalf("unexpected delete params: %+v", tg.deletedMessages[0])
	}

	expenses, err := env.Expenses.ListExpenses(ctx, entity.ExpenseFilter{})
	if err != nil {
		t.Fatalf("ListExpenses: %v", err)
	}
	if len(expenses) != 1 || expenses[0].UserID != 1 || expenses[0].Amount != 350 {
		t.Fatalf("unexpected expenses: %+v", expenses)
	}

	users, _ := env.Users.ListUsers(ctx)
	if len(users) != 1 || users[0].Username != "mama" || users[0].FullName != "Мама" {
		t.Fatalf("user not registered: %+v", users)
	}
}

func TestProcessSendsResultWhenDeleteFails(t *testing.T) {
	ctx := t.Context()
	env := testsupport.NewEnv(t)
	tg := &fakeTelegram{deleteMessageErr: errors.New("delete failed")}
	svc := newServiceWithSteps(t, env, tg, []entity.Message{
		textMessage("Записал 350 ₽."),
	})

	svc.process(ctx, startUpdate(1, "кофе 350"))

	if len(tg.sent) != 2 {
		t.Fatalf("expected processing and result messages, got %+v", tg.sent)
	}
	if got := tg.lastMessage(); got.Text != "Записал 350 ₽." {
		t.Fatalf("unexpected reply text: %q", got.Text)
	}
	if tg.deleteMessageCalls != 1 {
		t.Fatalf("expected one delete attempt, got %d", tg.deleteMessageCalls)
	}
}

func TestProcessCleansPlaceholderOnAgentError(t *testing.T) {
	ctx := t.Context()
	env := testsupport.NewEnv(t)
	tg := &fakeTelegram{}
	svc := newServiceWithSteps(t, env, tg, nil)

	svc.process(ctx, startUpdate(1, "кофе 350"))

	if len(tg.sent) != 2 {
		t.Fatalf("expected processing and error messages, got %+v", tg.sent)
	}
	if got := tg.lastMessage(); !strings.Contains(got.Text, "не удалось обработать") {
		t.Fatalf("unexpected error reply: %q", got.Text)
	}
	if len(tg.deletedMessages) != 1 {
		t.Fatalf("expected placeholder to be deleted, got %+v", tg.deletedMessages)
	}
}

func TestStartCommandRegistersUserWithoutLLM(t *testing.T) {
	ctx := t.Context()
	env := testsupport.NewEnv(t)
	tg := &fakeTelegram{}
	llmStub := &scriptedLLM{steps: []entity.Message{textMessage("не должно вызываться")}}
	log := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	svc := NewService(tg, usersuc.New(env.Users), agentuc.New(llmStub, expenseuc.New(env.Expenses)), log)

	svc.process(ctx, tgapi.Update{
		UpdateID: 1,
		Message: &tgapi.Message{
			MessageID: 42,
			From:      &tgapi.User{ID: 7, FirstName: "Папа", Username: "papa"},
			Chat:      tgapi.Chat{ID: 700, Type: "private"},
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
	if len(tg.sent) != 1 || len(tg.deletedMessages) != 0 {
		t.Fatalf("commands must not use processing placeholder: sent=%d deleted=%d", len(tg.sent), len(tg.deletedMessages))
	}

	users, err := env.Users.ListUsers(ctx)
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 1 || users[0].UserID != 7 || users[0].FullName != "Папа" {
		t.Fatalf("user not registered: %+v", users)
	}
}

func TestProcessIgnoresNonTextMessages(t *testing.T) {
	ctx := t.Context()
	env := testsupport.NewEnv(t)
	tg := &fakeTelegram{}
	svc := newServiceWithSteps(t, env, tg, nil)

	svc.process(ctx, tgapi.Update{UpdateID: 1})

	svc.process(ctx, tgapi.Update{
		UpdateID: 2,
		Message:  &tgapi.Message{MessageID: 1, Chat: tgapi.Chat{ID: 1}, Text: "hi"},
	})

	svc.process(ctx, tgapi.Update{
		UpdateID: 3,
		Message:  &tgapi.Message{MessageID: 1, From: &tgapi.User{ID: 1}, Chat: tgapi.Chat{ID: 1}},
	})

	if len(tg.sent) != 0 {
		t.Fatalf("expected no messages sent, got %d", len(tg.sent))
	}
}

func TestRunPollingProcessesUpdatesAndAdvancesOffset(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	env := testsupport.NewEnv(t)
	tg := &fakeTelegram{batches: [][]tgapi.Update{{
		{
			UpdateID: 7,
			Message: &tgapi.Message{
				MessageID: 42,
				From:      &tgapi.User{ID: 1, FirstName: "Мама", Username: "mama"},
				Chat:      tgapi.Chat{ID: 100, Type: "private"},
				Text:      "кофе 350",
			},
		},
	}}}
	svc := newServiceWithSteps(t, env, tg, []entity.Message{
		toolCallMessage("c1", "add_expense", `{"amount":350,"category":"еда"}`),
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

	expenses, err := env.Expenses.ListExpenses(context.Background(), entity.ExpenseFilter{})
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

func startUpdate(userID int64, text string) tgapi.Update {
	return tgapi.Update{
		UpdateID: userID,
		Message: &tgapi.Message{
			MessageID: 42,
			From:      &tgapi.User{ID: userID, FirstName: "Тест"},
			Chat:      tgapi.Chat{ID: userID, Type: "private"},
			Text:      text,
		},
	}
}

func seedFamily(t *testing.T, env testsupport.Env, headID, memberID int64, code string) {
	t.Helper()
	ctx := context.Background()
	if err := env.Users.UpsertUser(ctx, entity.User{UserID: headID, FullName: "Анна"}); err != nil {
		t.Fatalf("upsert head: %v", err)
	}
	if err := env.Users.BecomeHead(ctx, headID, code); err != nil {
		t.Fatalf("become head: %v", err)
	}
	if memberID == 0 {
		return
	}
	if err := env.Users.UpsertUser(ctx, entity.User{UserID: memberID, FullName: "Игорь"}); err != nil {
		t.Fatalf("upsert member: %v", err)
	}
	if err := env.Users.JoinFamily(ctx, memberID, headID); err != nil {
		t.Fatalf("join family: %v", err)
	}
}

func TestStartMakesNewUserHead(t *testing.T) {
	ctx := t.Context()
	env := testsupport.NewEnv(t)
	tg := &fakeTelegram{}
	svc := newServiceWithSteps(t, env, tg, nil)

	svc.process(ctx, startUpdate(1, "/start"))

	reply := tg.lastMessage().Text
	if !strings.Contains(reply, "глава семьи") || !strings.Contains(reply, "/invite") {
		t.Fatalf("unexpected reply: %q", reply)
	}
	u, err := env.Users.GetUser(ctx, 1)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if !u.IsHead() {
		t.Fatalf("user must be a head: %+v", u)
	}
	if len(u.InviteCode) != 10 {
		t.Fatalf("invite code not generated: %+v", u)
	}
}

func TestStartKeepsMemberRole(t *testing.T) {
	ctx := t.Context()
	env := testsupport.NewEnv(t)
	seedFamily(t, env, 1, 2, "HEADCODE01")
	tg := &fakeTelegram{}
	svc := newServiceWithSteps(t, env, tg, nil)

	svc.process(ctx, startUpdate(2, "/start"))

	reply := tg.lastMessage().Text
	if !strings.Contains(reply, "Вы в семье") || !strings.Contains(reply, "глава — Анна") {
		t.Fatalf("unexpected reply: %q", reply)
	}
	u, _ := env.Users.GetUser(ctx, 2)
	if u.InviteUserID == nil || *u.InviteUserID != 1 {
		t.Fatalf("member role changed: %+v", u)
	}
}

func TestStartWithCodeJoinsFamilyAndDropsHeadRole(t *testing.T) {
	ctx := t.Context()
	env := testsupport.NewEnv(t)
	seedFamily(t, env, 1, 0, "HEADCODE01")

	_ = env.Users.UpsertUser(ctx, entity.User{UserID: 2, FullName: "Игорь"})
	_ = env.Users.BecomeHead(ctx, 2, "OWNCODE001")

	tg := &fakeTelegram{}
	svc := newServiceWithSteps(t, env, tg, nil)
	svc.process(ctx, startUpdate(2, "/start HEADCODE01"))

	reply := tg.lastMessage().Text
	if !strings.Contains(reply, "Теперь вы в семье") {
		t.Fatalf("unexpected reply: %q", reply)
	}
	u, _ := env.Users.GetUser(ctx, 2)
	if u.InviteUserID == nil || *u.InviteUserID != 1 {
		t.Fatalf("user not joined: %+v", u)
	}
	if u.InviteCode != "" {
		t.Fatalf("head credentials must be dropped: %+v", u)
	}
}

func TestStartWithUnknownCodeFallsBackToHead(t *testing.T) {
	ctx := t.Context()
	env := testsupport.NewEnv(t)
	tg := &fakeTelegram{}
	svc := newServiceWithSteps(t, env, tg, nil)

	svc.process(ctx, startUpdate(3, "/start NOSUCHCODE"))

	reply := tg.lastMessage().Text
	if !strings.Contains(reply, "недействительна") {
		t.Fatalf("unexpected reply: %q", reply)
	}
	u, _ := env.Users.GetUser(ctx, 3)
	if !u.IsHead() || u.InviteCode == "" {
		t.Fatalf("fallback head not activated: %+v", u)
	}
}

func TestStartWithOwnCode(t *testing.T) {
	ctx := t.Context()
	env := testsupport.NewEnv(t)
	seedFamily(t, env, 1, 0, "HEADCODE01")
	tg := &fakeTelegram{}
	svc := newServiceWithSteps(t, env, tg, nil)

	svc.process(ctx, startUpdate(1, "/start HEADCODE01"))

	reply := tg.lastMessage().Text
	if !strings.Contains(reply, "собственная ссылка") {
		t.Fatalf("unexpected reply: %q", reply)
	}
	u, _ := env.Users.GetUser(ctx, 1)
	if u.InviteCode != "HEADCODE01" {
		t.Fatalf("credentials must not change: %+v", u)
	}
}

func TestStartHeadWithMembersCannotJoin(t *testing.T) {
	ctx := t.Context()
	env := testsupport.NewEnv(t)
	seedFamily(t, env, 1, 2, "HEADCODE01")
	seedFamily(t, env, 5, 0, "HEADCODE05")

	tg := &fakeTelegram{}
	svc := newServiceWithSteps(t, env, tg, nil)
	svc.process(ctx, startUpdate(1, "/start HEADCODE05"))

	reply := tg.lastMessage().Text
	if !strings.Contains(reply, "нельзя") {
		t.Fatalf("unexpected reply: %q", reply)
	}
	u, _ := env.Users.GetUser(ctx, 1)
	if !u.IsHead() || u.InviteCode != "HEADCODE01" {
		t.Fatalf("head with members must be unchanged: %+v", u)
	}
}

func TestInviteAlwaysPointsToHead(t *testing.T) {
	ctx := t.Context()
	env := testsupport.NewEnv(t)
	seedFamily(t, env, 1, 2, "HEADCODE01")
	tg := &fakeTelegram{}
	svc := newServiceWithSteps(t, env, tg, nil)

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
	env := testsupport.NewEnv(t)
	_ = env.Users.UpsertUser(ctx, entity.User{UserID: 7, FullName: "Старый"})
	tg := &fakeTelegram{}
	svc := newServiceWithSteps(t, env, tg, nil)

	svc.process(ctx, startUpdate(7, "/invite"))

	reply := tg.lastMessage().Text
	if !strings.Contains(reply, "t.me/family_budget_bot?start=") {
		t.Fatalf("unexpected reply: %q", reply)
	}
	u, _ := env.Users.GetUser(ctx, 7)
	if u.InviteCode == "" {
		t.Fatalf("invite code not generated for legacy head: %+v", u)
	}
}

func TestPanelLinkForEveryone(t *testing.T) {
	ctx := t.Context()
	env := testsupport.NewEnv(t)
	seedFamily(t, env, 1, 2, "HEADCODE01")
	tg := &fakeTelegram{}
	svc := newServiceWithSteps(t, env, tg, nil)

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
	env := testsupport.NewEnv(t)
	tg := &fakeTelegram{}
	llmStub := &scriptedLLM{steps: []entity.Message{textMessage("не должно вызываться")}}
	log := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	svc := NewService(tg, usersuc.New(env.Users), agentuc.New(llmStub, expenseuc.New(env.Expenses)), log)

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

	env := testsupport.NewEnv(t)
	tg := &fakeTelegram{
		deleteFailures: 1,
		batches: [][]tgapi.Update{{
			{
				UpdateID: 1,
				Message: &tgapi.Message{
					MessageID: 42,
					From:      &tgapi.User{ID: 7, FirstName: "Папа"},
					Chat:      tgapi.Chat{ID: 700, Type: "private"},
					Text:      "/start",
				},
			},
		}},
	}
	svc := newServiceWithSteps(t, env, tg, nil)

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
