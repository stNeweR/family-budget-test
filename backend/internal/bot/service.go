package bot

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

	"github.com/stNeweR/family-budget/internal/domain"
	"github.com/stNeweR/family-budget/internal/store"
	"github.com/stNeweR/family-budget/internal/telegram"
)

// secretHeader is the header Telegram uses to authenticate webhook calls.
const secretHeader = "X-Telegram-Bot-Api-Secret-Token"

const (
	// pollTimeoutSeconds is the long-poll timeout requested from Telegram.
	pollTimeoutSeconds = 50
	// pollLimit caps how many updates a single getUpdates call may return.
	pollLimit = 100
	// Backoff bounds for retrying failed getUpdates calls.
	pollBackoffMin = time.Second
	pollBackoffMax = 30 * time.Second
	// replyTimeout bounds the whole handling of one update.
	replyTimeout = 90 * time.Second
)

// allowedUpdates is the set of update types the bot subscribes to.
var allowedUpdates = []string{"message", "edited_message"}

// TelegramAPI is the slice of the Bot API the service relies on. *telegram.Client
// implements it; tests can substitute a fake.
type TelegramAPI interface {
	SendMessage(ctx context.Context, params telegram.SendMessageParams) (telegram.Message, error)
	SetWebhook(ctx context.Context, params telegram.SetWebhookParams) error
	DeleteWebhook(ctx context.Context, dropPending bool) error
	GetUpdates(ctx context.Context, params telegram.GetUpdatesParams) ([]telegram.Update, error)
	GetMe(ctx context.Context) (telegram.User, error)
}

// Service glues the Telegram transport to the AI agent.
type Service struct {
	tg       TelegramAPI
	agent    *Agent
	store    store.Store
	log      *slog.Logger
	panelURL string

	// mu guards the cached bot username resolved via getMe.
	mu               sync.Mutex
	botUsernameCache string
}

// Option customises the bot service.
type Option func(*Service)

// WithPanelURL sets the public base URL of the web panel.
func WithPanelURL(url string) Option {
	return func(s *Service) { s.panelURL = url }
}

// NewService constructs the bot service.
func NewService(tg TelegramAPI, agent *Agent, st store.Store, log *slog.Logger, opts ...Option) *Service {
	s := &Service{tg: tg, agent: agent, store: st, log: log, panelURL: "http://localhost:5173"}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// RegisterWebhook points Telegram at the public webhook URL. It is safe to
// call on every start; the URL from a tunnel may change between runs.
func (s *Service) RegisterWebhook(ctx context.Context, publicURL, secret string) error {
	return s.tg.SetWebhook(ctx, telegram.SetWebhookParams{
		URL:                publicURL,
		SecretToken:        secret,
		AllowedUpdates:     allowedUpdates,
		DropPendingUpdates: true,
	})
}

// RunPolling receives updates via long polling until ctx is cancelled. It is
// an alternative to the webhook transport for environments without a public
// HTTPS URL. Any active webhook is removed first, as getUpdates conflicts with
// a registered webhook; updates queued while the bot was down are kept.
func (s *Service) RunPolling(ctx context.Context) error {
	var offset int64
	backoff := pollBackoffMin

	// A transient network failure must not take the whole backend down, so
	// deleteWebhook is retried with backoff; a rejected token is fatal.
	for {
		err := s.tg.DeleteWebhook(ctx, false)
		if err == nil {
			break
		}
		if ctx.Err() != nil {
			return nil
		}
		var apiErr *telegram.APIError
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
		updates, err := s.tg.GetUpdates(ctx, telegram.GetUpdatesParams{
			Offset:         offset,
			Limit:          pollLimit,
			Timeout:        pollTimeoutSeconds,
			AllowedUpdates: allowedUpdates,
		})
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			var apiErr *telegram.APIError
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

// sleepCtx waits for d and reports whether the wait completed; it returns
// false as soon as ctx is cancelled.
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

// WebhookHandler returns the HTTP handler for Telegram updates. It validates
// the secret token, answers 200 immediately and processes the update
// asynchronously so Telegram does not retry on slow LLM calls.
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

		var update telegram.Update
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&update); err != nil {
			s.log.Warn("decode telegram update", "error", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)

		go s.process(context.Background(), update)
	}
}

func (s *Service) process(ctx context.Context, update telegram.Update) {
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

	user := domain.User{
		UserID:   msg.From.ID,
		Username: msg.From.Username,
		FullName: fullName(*msg.From),
	}
	if err := s.store.UpsertUser(ctx, user); err != nil {
		s.log.Error("upsert user", "user_id", user.UserID, "error", err)
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

	if _, err := s.tg.SendMessage(ctx, telegram.SendMessageParams{
		ChatID:           msg.Chat.ID,
		Text:             reply,
		ReplyToMessageID: msg.MessageID,
	}); err != nil {
		s.log.Error("send message", "chat_id", msg.Chat.ID, "error", err)
	}
}

func userLabel(u domain.User) string {
	if u.FullName != "" {
		return u.FullName
	}
	if u.Username != "" {
		return "@" + u.Username
	}
	return fmt.Sprintf("user_%d", u.UserID)
}

// clip shortens s to n runes so console lines stay readable.
func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "..."
}

func fullName(u telegram.User) string {
	name := u.FirstName
	if u.LastName != "" {
		if name != "" {
			name += " "
		}
		name += u.LastName
	}
	return name
}
