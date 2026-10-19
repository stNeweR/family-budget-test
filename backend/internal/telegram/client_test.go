package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSendMessageSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/botTOKEN/sendMessage" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		var body SendMessageParams
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		if body.ChatID != 5 || body.Text != "привет" {
			t.Errorf("unexpected payload: %+v", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":     true,
			"result": Message{MessageID: 7, Chat: Chat{ID: 5}, Text: body.Text},
		})
	}))
	defer srv.Close()

	c := NewClient("TOKEN", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	msg, err := c.SendMessage(context.Background(), SendMessageParams{ChatID: 5, Text: "привет"})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if msg.MessageID != 7 {
		t.Fatalf("unexpected message id %d", msg.MessageID)
	}
}

func TestSendMessageAPIErrorWithRetryAfter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":          false,
			"error_code":  429,
			"description": "Too Many Requests",
			"parameters":  map[string]any{"retry_after": 12},
		})
	}))
	defer srv.Close()

	c := NewClient("TOKEN", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	_, err := c.SendMessage(context.Background(), SendMessageParams{ChatID: 5, Text: "x"})
	if err == nil {
		t.Fatal("expected error")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.Code != 429 || apiErr.RetryAfter != 12 {
		t.Fatalf("unexpected APIError: %+v", apiErr)
	}
	if !apiErr.IsRetryable() {
		t.Fatal("429 should be retryable")
	}
}

func TestSetWebhook(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/botTOKEN/setWebhook" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		var body SetWebhookParams
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.URL == "" || body.SecretToken == "" {
			t.Errorf("payload missing url/secret: %+v", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": true})
	}))
	defer srv.Close()

	c := NewClient("TOKEN", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	err := c.SetWebhook(context.Background(), SetWebhookParams{
		URL:         "https://example.com/hook",
		SecretToken: "s3cret",
	})
	if err != nil {
		t.Fatalf("SetWebhook: %v", err)
	}
}

func TestGetUpdates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/botTOKEN/getUpdates" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		var body GetUpdatesParams
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		if body.Offset != 7 || body.Limit != 100 || body.Timeout != 50 {
			t.Errorf("unexpected payload: %+v", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"result": []Update{
				{
					UpdateID: 7,
					Message: &Message{
						MessageID: 1,
						From:      &User{ID: 5, FirstName: "Мама"},
						Chat:      Chat{ID: 5},
						Text:      "кофе 350",
					},
				},
			},
		})
	}))
	defer srv.Close()

	c := NewClient("TOKEN", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	updates, err := c.GetUpdates(context.Background(), GetUpdatesParams{
		Offset:         7,
		Limit:          100,
		Timeout:        50,
		AllowedUpdates: []string{"message"},
	})
	if err != nil {
		t.Fatalf("GetUpdates: %v", err)
	}
	if len(updates) != 1 || updates[0].UpdateID != 7 {
		t.Fatalf("unexpected updates: %+v", updates)
	}
	if updates[0].Message == nil || updates[0].Message.Text != "кофе 350" {
		t.Fatalf("unexpected message: %+v", updates[0].Message)
	}
}

func TestTransportErrorsDoNotLeakToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()

	c := NewClient("123456:SECRET", WithBaseURL(url), WithHTTPClient(srv.Client()))
	_, err := c.GetUpdates(context.Background(), GetUpdatesParams{})
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("bot token leaked in error: %v", err)
	}
	if !strings.Contains(err.Error(), "***") {
		t.Fatalf("expected redacted token placeholder, got: %v", err)
	}
}

func TestClientTimeoutRespected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(50 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": Message{}})
	}))
	defer srv.Close()

	c := NewClient("TOKEN", WithBaseURL(srv.URL), WithHTTPClient(&http.Client{Timeout: 10 * time.Millisecond}))
	if _, err := c.SendMessage(context.Background(), SendMessageParams{ChatID: 1}); err == nil {
		t.Fatal("expected timeout error")
	}
}
