// Package llm is a small OpenAI-compatible chat completions client.
//
// It supports function/tool calling and works with any provider exposing the
// OpenAI wire format. The default configuration targets OpenRouter.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/stNeweR/family-budget/internal/config"
)

// Role constants for chat messages.
const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
)

// Message is a single chat message in the OpenAI format.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

// ToolCall is a function invocation requested by the model.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

// FunctionCall holds the name and JSON-encoded arguments of a tool call.
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Tool describes a callable function exposed to the model.
type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

// ToolFunction is the schema of a callable function.
type ToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// NewFunctionTool is a helper building a function-type tool.
func NewFunctionTool(name, description string, parameters json.RawMessage) Tool {
	return Tool{
		Type: "function",
		Function: ToolFunction{
			Name:        name,
			Description: description,
			Parameters:  parameters,
		},
	}
}

// ChatClient is the behaviour the agent depends on, so it can be mocked.
type ChatClient interface {
	Chat(ctx context.Context, messages []Message, tools []Tool) (Message, error)
}

// Client is an OpenAI-compatible chat completions client.
type Client struct {
	baseURL    string
	apiKey     string
	model      string
	referer    string
	title      string
	httpClient *http.Client
}

func NewClient(cfg config.LLMConfig) *Client {
	return &Client{
		baseURL: cfg.BaseURL,
		apiKey:  cfg.APIKey,
		model:   cfg.Model,
		referer: cfg.HTTPReferer,
		title:   cfg.Title,
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
		},
	}
}

type chatRequest struct {
	Model      string    `json:"model"`
	Messages   []Message `json:"messages"`
	Tools      []Tool    `json:"tools,omitempty"`
	ToolChoice string    `json:"tool_choice,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message      Message `json:"message"`
		FinishReason string  `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Code    any    `json:"code"`
	} `json:"error"`
}

// Chat performs a chat completion request and returns the assistant message.
func (c *Client) Chat(ctx context.Context, messages []Message, tools []Tool) (Message, error) {
	payload := chatRequest{
		Model:    c.model,
		Messages: messages,
		Tools:    tools,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return Message{}, fmt.Errorf("marshal chat request: %w", err)
	}

	url := c.baseURL + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return Message{}, fmt.Errorf("build chat request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	if c.referer != "" {
		req.Header.Set("HTTP-Referer", c.referer)
	}
	if c.title != "" {
		req.Header.Set("X-Title", c.title)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Message{}, fmt.Errorf("call chat completions: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return Message{}, fmt.Errorf("read chat response: %w", err)
	}

	var parsed chatResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return Message{}, fmt.Errorf("decode chat response (status %d): %w", resp.StatusCode, err)
	}
	if parsed.Error != nil {
		return Message{}, fmt.Errorf("llm error: %s", parsed.Error.Message)
	}
	if resp.StatusCode >= 300 {
		return Message{}, fmt.Errorf("llm request failed: status %d: %s", resp.StatusCode, string(raw))
	}
	if len(parsed.Choices) == 0 {
		return Message{}, fmt.Errorf("llm returned no choices")
	}
	return parsed.Choices[0].Message, nil
}

// ensure Client satisfies the interface.
var _ ChatClient = (*Client)(nil)
