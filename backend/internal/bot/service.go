package bot

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/stNeweR/family-budget/internal/domain"
	"github.com/stNeweR/family-budget/internal/store"
	"github.com/stNeweR/family-budget/internal/telegram"
)

// secretHeader is the header Telegram uses to authenticate webhook calls.
const secretHeader = "X-Telegram-Bot-Api-Secret-Token"

// TelegramAPI is the slice of the Bot API the service relies on. *telegram.Client
// implements it; tests can substitute a fake.
type TelegramAPI interface {
	SendMessage(ctx context.Context, params telegram.SendMessageParams) (telegram.Message, error)
	SetWebhook(ctx context.Context, params telegram.SetWebhookParams) error
}

// Service glues the Telegram transport to the AI agent.
type Service struct {
	tg    TelegramAPI
	agent *Agent
	store store.Store
	log   *slog.Logger
}

// NewService constructs the bot service.
func NewService(tg TelegramAPI, agent *Agent, st store.Store, log *slog.Logger) *Service {
	return &Service{tg: tg, agent: agent, store: st, log: log}
}

// RegisterWebhook points Telegram at the public webhook URL. It is safe to
// call on every start; the URL from a tunnel may change between runs.
func (s *Service) RegisterWebhook(ctx context.Context, publicURL, secret string) error {
	return s.tg.SetWebhook(ctx, telegram.SetWebhookParams{
		URL:                publicURL,
		SecretToken:        secret,
		AllowedUpdates:     []string{"message", "edited_message"},
		DropPendingUpdates: true,
	})
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

		go s.process(update)
	}
}

func (s *Service) process(update telegram.Update) {
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

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	user := domain.User{
		UserID:   msg.From.ID,
		Username: msg.From.Username,
		FullName: fullName(*msg.From),
	}
	if err := s.store.UpsertUser(ctx, user); err != nil {
		s.log.Error("upsert user", "user_id", user.UserID, "error", err)
	}

	reply, err := s.agent.Handle(ctx, user, text)
	if err != nil {
		s.log.Error("agent handle", "user_id", user.UserID, "error", err)
		reply = "Извините, не удалось обработать запрос. Попробуйте ещё раз."
	}

	if _, err := s.tg.SendMessage(ctx, telegram.SendMessageParams{
		ChatID:           msg.Chat.ID,
		Text:             reply,
		ReplyToMessageID: msg.MessageID,
	}); err != nil {
		s.log.Error("send message", "chat_id", msg.Chat.ID, "error", err)
	}
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
