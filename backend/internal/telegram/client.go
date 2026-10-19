package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client talks to the Telegram Bot API.
type Client struct {
	token      string
	baseURL    string
	httpClient *http.Client
}

// Option customises the client.
type Option func(*Client)

// WithBaseURL overrides the API base URL (useful for tests).
func WithBaseURL(baseURL string) Option {
	return func(c *Client) { c.baseURL = baseURL }
}

// WithHTTPClient overrides the underlying HTTP client.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) { c.httpClient = hc }
}

// NewClient creates a Bot API client for the given token.
func NewClient(token string, opts ...Option) *Client {
	c := &Client{
		token:      token,
		baseURL:    "https://api.telegram.org",
		httpClient: &http.Client{Timeout: 70 * time.Second},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// SendMessage sends a text message and returns the created message.
func (c *Client) SendMessage(ctx context.Context, params SendMessageParams) (Message, error) {
	return do[Message](ctx, c, "sendMessage", params)
}

// SetWebhook registers the public webhook URL.
func (c *Client) SetWebhook(ctx context.Context, params SetWebhookParams) error {
	_, err := do[bool](ctx, c, "setWebhook", params)
	return err
}

// GetUpdates fetches updates via long polling. The call is held open by
// Telegram for up to Timeout seconds when no updates are available.
func (c *Client) GetUpdates(ctx context.Context, params GetUpdatesParams) ([]Update, error) {
	return do[[]Update](ctx, c, "getUpdates", params)
}

// DeleteWebhook removes the webhook. When dropPending is true queued updates
// are discarded as well.
func (c *Client) DeleteWebhook(ctx context.Context, dropPending bool) error {
	_, err := do[bool](ctx, c, "deleteWebhook", map[string]bool{"drop_pending_updates": dropPending})
	return err
}

// GetWebhookInfo returns the current webhook configuration.
func (c *Client) GetWebhookInfo(ctx context.Context) (WebhookInfo, error) {
	return do[WebhookInfo](ctx, c, "getWebhookInfo", map[string]any{})
}

// GetMe returns the bot's own account, e.g. to build t.me deep links.
func (c *Client) GetMe(ctx context.Context) (User, error) {
	return do[User](ctx, c, "getMe", map[string]any{})
}

// SetMyCommands replaces the bot's command menu.
func (c *Client) SetMyCommands(ctx context.Context, commands []BotCommand) error {
	_, err := do[bool](ctx, c, "setMyCommands", SetMyCommandsParams{Commands: commands})
	return err
}

// GetMyCommands returns the bot's current command menu.
func (c *Client) GetMyCommands(ctx context.Context) ([]BotCommand, error) {
	return do[[]BotCommand](ctx, c, "getMyCommands", map[string]any{})
}

// redact hides the bot token that appears in request URLs inside transport
// errors so it never reaches the logs.
func (c *Client) redact(err error) error {
	if err == nil || c.token == "" || !strings.Contains(err.Error(), c.token) {
		return err
	}
	return errors.New(strings.ReplaceAll(err.Error(), c.token, "***"))
}

func do[T any](ctx context.Context, c *Client, method string, payload any) (T, error) {
	var zero T

	body, err := json.Marshal(payload)
	if err != nil {
		return zero, fmt.Errorf("marshal %s payload: %w", method, err)
	}

	url := fmt.Sprintf("%s/bot%s/%s", c.baseURL, c.token, method)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return zero, fmt.Errorf("build %s request: %w", method, c.redact(err))
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return zero, fmt.Errorf("call %s: %w", method, c.redact(err))
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return zero, fmt.Errorf("read %s response: %w", method, err)
	}

	var parsed apiResponse[T]
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return zero, fmt.Errorf("decode %s response (status %d): %w", method, resp.StatusCode, err)
	}
	if !parsed.OK {
		return zero, &APIError{
			Method:      method,
			Code:        parsed.ErrorCode,
			Description: parsed.Description,
			RetryAfter:  parsed.Parameters.RetryAfter,
		}
	}
	return parsed.Result, nil
}
