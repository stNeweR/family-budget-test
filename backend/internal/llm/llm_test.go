package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stNeweR/family-budget/internal/config"
)

func TestChatSuccessWithToolCall(t *testing.T) {
	var gotAuth, gotReferer string
	var gotRequest chatRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		gotReferer = r.Header.Get("HTTP-Referer")
		_ = json.NewDecoder(r.Body).Decode(&gotRequest)

		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{
					"role": "assistant",
					"tool_calls": []map[string]any{{
						"id":   "call_1",
						"type": "function",
						"function": map[string]any{
							"name":      "add_expense",
							"arguments": `{"amount":350}`,
						},
					}},
				}},
			},
		})
	}))
	defer srv.Close()

	client := NewClient(config.LLMConfig{
		BaseURL:     srv.URL,
		APIKey:      "key-123",
		Model:       "openrouter/free",
		HTTPReferer: "https://example.com",
		Timeout:     0,
	})
	client.httpClient = srv.Client()

	tools := []Tool{NewFunctionTool("add_expense", "desc", json.RawMessage(`{"type":"object"}`))}
	msg, err := client.Chat(context.Background(), []Message{{Role: RoleUser, Content: "кофе 350"}}, tools)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if gotAuth != "Bearer key-123" {
		t.Errorf("auth header = %q", gotAuth)
	}
	if gotReferer != "https://example.com" {
		t.Errorf("referer header = %q", gotReferer)
	}
	if gotRequest.Model != "openrouter/free" {
		t.Errorf("model = %q", gotRequest.Model)
	}
	if len(gotRequest.Tools) != 1 || gotRequest.Tools[0].Function.Name != "add_expense" {
		t.Errorf("tools not forwarded: %+v", gotRequest.Tools)
	}
	if len(msg.ToolCalls) != 1 || msg.ToolCalls[0].Function.Name != "add_expense" {
		t.Errorf("unexpected message: %+v", msg)
	}
}

func TestChatErrorBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"message": "free model unavailable"},
		})
	}))
	defer srv.Close()

	client := NewClient(config.LLMConfig{BaseURL: srv.URL, Model: "x"})
	client.httpClient = srv.Client()

	_, err := client.Chat(context.Background(), []Message{{Role: RoleUser, Content: "hi"}}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestChatNoChoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{}})
	}))
	defer srv.Close()

	client := NewClient(config.LLMConfig{BaseURL: srv.URL})
	client.httpClient = srv.Client()

	if _, err := client.Chat(context.Background(), nil, nil); err == nil {
		t.Fatal("expected error for empty choices")
	}
}
