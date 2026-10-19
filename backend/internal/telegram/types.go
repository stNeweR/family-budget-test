// Package telegram is a minimal hand-written client for the Telegram Bot API.
//
// It implements only what this service needs: receiving webhook updates and
// sending messages. Everything is built on net/http and encoding/json without
// external dependencies so the transport stays transparent and testable.
package telegram

// Update represents an incoming update from Telegram.
type Update struct {
	UpdateID      int64          `json:"update_id"`
	Message       *Message       `json:"message"`
	EditedMessage *Message       `json:"edited_message"`
	CallbackQuery *CallbackQuery `json:"callback_query"`
}

// User is a Telegram account.
type User struct {
	ID           int64  `json:"id"`
	IsBot        bool   `json:"is_bot"`
	FirstName    string `json:"first_name"`
	LastName     string `json:"last_name"`
	Username     string `json:"username"`
	LanguageCode string `json:"language_code"`
}

// DisplayName returns a human friendly name for the user.
func (u User) DisplayName() string {
	if u.Username != "" {
		return u.Username
	}
	name := u.FirstName
	if u.LastName != "" {
		name += " " + u.LastName
	}
	return name
}

// Chat identifies a conversation.
type Chat struct {
	ID        int64  `json:"id"`
	Type      string `json:"type"`
	Title     string `json:"title"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Username  string `json:"username"`
}

// Message is a message inside a chat.
type Message struct {
	MessageID int64  `json:"message_id"`
	From      *User  `json:"from"`
	Chat      Chat   `json:"chat"`
	Date      int64  `json:"date"`
	Text      string `json:"text"`
}

// CallbackQuery is present when a user presses an inline button.
type CallbackQuery struct {
	ID      string   `json:"id"`
	From    User     `json:"from"`
	Message *Message `json:"message"`
	Data    string   `json:"data"`
}

// WebhookInfo describes the current webhook configuration.
type WebhookInfo struct {
	URL                string `json:"url"`
	HasCustomCert      bool   `json:"has_custom_certificate"`
	PendingUpdateCount int    `json:"pending_update_count"`
	LastErrorDate      int64  `json:"last_error_date"`
	LastErrorMessage   string `json:"last_error_message"`
}

// SendMessageParams configures a sendMessage call.
type SendMessageParams struct {
	ChatID                int64  `json:"chat_id"`
	Text                  string `json:"text"`
	ParseMode             string `json:"parse_mode,omitempty"`
	ReplyToMessageID      int64  `json:"reply_to_message_id,omitempty"`
	DisableWebPagePreview bool   `json:"disable_web_page_preview,omitempty"`
}

// SetWebhookParams configures a setWebhook call.
type SetWebhookParams struct {
	URL                string   `json:"url"`
	SecretToken        string   `json:"secret_token,omitempty"`
	AllowedUpdates     []string `json:"allowed_updates,omitempty"`
	DropPendingUpdates bool     `json:"drop_pending_updates,omitempty"`
}

// GetUpdatesParams configures a getUpdates call (long polling).
type GetUpdatesParams struct {
	Offset         int64    `json:"offset,omitempty"`
	Limit          int      `json:"limit,omitempty"`
	Timeout        int      `json:"timeout,omitempty"`
	AllowedUpdates []string `json:"allowed_updates,omitempty"`
}

// BotCommand is one entry of the bot's command menu shown in Telegram clients.
type BotCommand struct {
	Command     string `json:"command"`
	Description string `json:"description"`
}

// SetMyCommandsParams configures a setMyCommands call.
type SetMyCommandsParams struct {
	Commands []BotCommand `json:"commands"`
}

type responseParameters struct {
	RetryAfter int `json:"retry_after"`
}

type apiResponse[T any] struct {
	OK          bool               `json:"ok"`
	Result      T                  `json:"result"`
	ErrorCode   int                `json:"error_code"`
	Description string             `json:"description"`
	Parameters  responseParameters `json:"parameters"`
}
