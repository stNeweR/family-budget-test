package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/stNeweR/family-budget/internal/entity"
	"github.com/stNeweR/family-budget/internal/usecase"
	tgapi "github.com/stNeweR/family-budget/pkg/telegram"
)

const secretHeader = "X-Telegram-Bot-Api-Secret-Token"

const (
	pollTimeoutSeconds = 50
	pollLimit          = 100
	pollBackoffMin     = time.Second
	pollBackoffMax     = 30 * time.Second
	replyTimeout       = 90 * time.Second
)

var allowedUpdates = []string{"message", "edited_message"}

type TelegramAPI interface {
	SendMessage(ctx context.Context, params tgapi.SendMessageParams) (tgapi.Message, error)
	SetWebhook(ctx context.Context, params tgapi.SetWebhookParams) error
	DeleteWebhook(ctx context.Context, dropPending bool) error
	GetUpdates(ctx context.Context, params tgapi.GetUpdatesParams) ([]tgapi.Update, error)
	GetMe(ctx context.Context) (tgapi.User, error)
}

type Service struct {
	tg       TelegramAPI
	family   usecase.Family
	agent    usecase.Agent
	log      *slog.Logger
	panelURL string

	mu               sync.Mutex
	botUsernameCache string
}

type Option func(*Service)

func WithPanelURL(url string) Option {
	return func(s *Service) { s.panelURL = url }
}

func NewService(tg TelegramAPI, family usecase.Family, agent usecase.Agent, log *slog.Logger, opts ...Option) *Service {
	s := &Service{tg: tg, family: family, agent: agent, log: log, panelURL: "http://localhost:5173"}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *Service) RegisterWebhook(ctx context.Context, publicURL, secret string) error {
	return s.tg.SetWebhook(ctx, tgapi.SetWebhookParams{
		URL:                publicURL,
		SecretToken:        secret,
		AllowedUpdates:     allowedUpdates,
		DropPendingUpdates: true,
	})
}

func (s *Service) RunPolling(ctx context.Context) error {
	var offset int64
	backoff := pollBackoffMin

	for {
		err := s.tg.DeleteWebhook(ctx, false)
		if err == nil {
			break
		}
		if ctx.Err() != nil {
			return nil
		}
		var apiErr *tgapi.APIError
		if errors.As(err, &apiErr) && apiErr.Code == http.StatusUnauthorized {
			return fmt.Errorf("delete webhook before polling: %w", err)
		}
		fmt.Printf("[bot] не удалось снять webhook: %v (повтор через %s)\n", err, backoff)
		if !sleepCtx(ctx, backoff) {
			return nil
		}
		backoff = min(backoff*2, pollBackoffMax)
	}
	fmt.Println("[bot] режим long polling: слушаю сообщения в Telegram...")
	backoff = pollBackoffMin
	for {
		updates, err := s.tg.GetUpdates(ctx, tgapi.GetUpdatesParams{
			Offset:         offset,
			Limit:          pollLimit,
			Timeout:        pollTimeoutSeconds,
			AllowedUpdates: allowedUpdates,
		})
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			var apiErr *tgapi.APIError
			if errors.As(err, &apiErr) && apiErr.IsRetryable() && apiErr.RetryAfter > 0 {
				s.log.Warn("telegram polling rate limited", "retry_after_s", apiErr.RetryAfter)
				if !sleepCtx(ctx, time.Duration(apiErr.RetryAfter)*time.Second) {
					return nil
				}
				continue
			}
			s.log.Error("telegram getUpdates", "error", err, "retry_in", backoff)
			if !sleepCtx(ctx, backoff) {
				return nil
			}
			backoff = min(backoff*2, pollBackoffMax)
			continue
		}
		backoff = pollBackoffMin

		if len(updates) > 0 {
			fmt.Printf("[bot] получено обновлений: %d\n", len(updates))
		}
		for i := range updates {
			offset = updates[i].UpdateID + 1
			s.process(ctx, updates[i])
		}
	}
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func (s *Service) WebhookHandler(secret string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if secret != "" && r.Header.Get(secretHeader) != secret {
			http.Error(w, "forbidden", http.StatusUnauthorized)
			return
		}

		var update tgapi.Update
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&update); err != nil {
			s.log.Warn("decode telegram update", "error", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)

		go s.process(context.Background(), update)
	}
}

func (s *Service) process(ctx context.Context, update tgapi.Update) {
	msg := update.Message
	if msg == nil {
		msg = update.EditedMessage
	}
	if msg == nil || msg.From == nil {
		return
	}
	text := msg.Text
	if text == "" {
		return
	}

	ctx, cancel := context.WithTimeout(ctx, replyTimeout)
	defer cancel()

	user := entity.User{
		UserID:   msg.From.ID,
		Username: msg.From.Username,
		FullName: fullName(*msg.From),
	}
	if _, err := s.family.EnsureUser(ctx, user); err != nil {
		s.log.Error("ensure user", "user_id", user.UserID, "error", err)
	}

	fmt.Printf("[bot] сообщение от %s (id=%d): %s\n", userLabel(user), user.UserID, clip(text, 200))

	var reply string
	if cmd, args := parseCommand(text); cmd != "" {
		reply = s.handleCommand(ctx, user, cmd, args)
	} else {
		start := time.Now()
		var err error
		reply, err = s.agent.Handle(ctx, user, text)
		if err != nil {
			s.log.Error("agent handle", "user_id", user.UserID, "error", err)
			reply = "Извините, не удалось обработать запрос. Попробуйте ещё раз."
		}
		fmt.Printf("[bot] ответ (%s, %s): %s\n", userLabel(user), time.Since(start).Round(time.Millisecond), clip(reply, 300))
	}

	if _, err := s.tg.SendMessage(ctx, tgapi.SendMessageParams{
		ChatID:           msg.Chat.ID,
		Text:             reply,
		ReplyToMessageID: msg.MessageID,
	}); err != nil {
		s.log.Error("send message", "chat_id", msg.Chat.ID, "error", err)
	}
}

func userLabel(u entity.User) string {
	if u.FullName != "" {
		return u.FullName
	}
	if u.Username != "" {
		return "@" + u.Username
	}
	return fmt.Sprintf("user_%d", u.UserID)
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "..."
}

func fullName(u tgapi.User) string {
	name := u.FirstName
	if u.LastName != "" {
		if name != "" {
			name += " "
		}
		name += u.LastName
	}
	return name
}
