package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/stNeweR/family-budget/config"
	"github.com/stNeweR/family-budget/internal/entity"
	"github.com/stNeweR/family-budget/internal/repo"
)

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
	Model      string           `json:"model"`
	Messages   []entity.Message `json:"messages"`
	Tools      []entity.Tool    `json:"tools,omitempty"`
	ToolChoice string           `json:"tool_choice,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message      entity.Message `json:"message"`
		FinishReason string         `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Code    any    `json:"code"`
	} `json:"error"`
}

func (c *Client) Chat(ctx context.Context, messages []entity.Message, tools []entity.Tool) (entity.Message, error) {
	payload := chatRequest{
		Model:    c.model,
		Messages: messages,
		Tools:    tools,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return entity.Message{}, fmt.Errorf("marshal chat request: %w", err)
	}

	url := c.baseURL + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return entity.Message{}, fmt.Errorf("build chat request: %w", err)
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
		return entity.Message{}, fmt.Errorf("call chat completions: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return entity.Message{}, fmt.Errorf("read chat response: %w", err)
	}

	var parsed chatResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return entity.Message{}, fmt.Errorf("decode chat response (status %d): %w", resp.StatusCode, err)
	}
	if parsed.Error != nil {
		return entity.Message{}, fmt.Errorf("llm error: %s", parsed.Error.Message)
	}
	if resp.StatusCode >= 300 {
		return entity.Message{}, fmt.Errorf("llm request failed: status %d: %s", resp.StatusCode, string(raw))
	}
	if len(parsed.Choices) == 0 {
		return entity.Message{}, fmt.Errorf("llm returned no choices")
	}
	return parsed.Choices[0].Message, nil
}

var _ repo.LLM = (*Client)(nil)
