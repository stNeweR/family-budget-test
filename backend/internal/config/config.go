package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds the whole application configuration, populated from
// environment variables. Every external dependency (database, LLM, Telegram)
// is configured here so the service stays provider-agnostic.
type Config struct {
	DatabaseURL string
	HTTPAddr    string

	LLM      LLMConfig
	Telegram TelegramConfig
}

// LLMConfig describes an OpenAI-compatible chat completions endpoint.
// It is not tied to a specific vendor: by default it points to OpenRouter.
type LLMConfig struct {
	BaseURL     string
	APIKey      string
	Model       string
	HTTPReferer string
	Title       string
	Timeout     time.Duration
}

// TelegramConfig holds everything needed to receive updates via webhook.
type TelegramConfig struct {
	BotToken      string
	WebhookURL    string
	WebhookSecret string
	WebhookPath   string
	APIBaseURL    string
}

// Enabled reports whether the bot can be started.
func (c TelegramConfig) Enabled() bool {
	return c.BotToken != ""
}

// Mode returns the update delivery mode: "webhook" when a public URL is
// configured, "polling" for long polling, or "disabled" without a token.
func (c TelegramConfig) Mode() string {
	switch {
	case !c.Enabled():
		return "disabled"
	case c.WebhookURL != "":
		return "webhook"
	default:
		return "polling"
	}
}

// Load reads configuration from the environment applying sane defaults.
func Load() (Config, error) {
	cfg := Config{
		DatabaseURL: getEnv("DATABASE_URL", "postgres://family:family@localhost:5432/family?sslmode=disable"),
		HTTPAddr:    getEnv("HTTP_ADDR", ":8080"),
		LLM: LLMConfig{
			BaseURL:     getEnv("LLM_BASE_URL", "https://openrouter.ai/api/v1"),
			APIKey:      getEnv("LLM_API_KEY", ""),
			Model:       getEnv("LLM_MODEL", "openrouter/free"),
			HTTPReferer: getEnv("LLM_HTTP_REFERER", ""),
			Title:       getEnv("LLM_TITLE", "Family Budget"),
			Timeout:     getDuration("LLM_TIMEOUT", 60*time.Second),
		},
		Telegram: TelegramConfig{
			BotToken:      getEnv("TELEGRAM_BOT_TOKEN", ""),
			WebhookURL:    getEnv("TELEGRAM_WEBHOOK_URL", ""),
			WebhookSecret: getEnv("TELEGRAM_WEBHOOK_SECRET", ""),
			WebhookPath:   getEnv("TELEGRAM_WEBHOOK_PATH", "/telegram/webhook"),
			APIBaseURL:    getEnv("TELEGRAM_API_BASE_URL", "https://api.telegram.org"),
		},
	}

	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL must not be empty")
	}
	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return fallback
}

func getDuration(key string, fallback time.Duration) time.Duration {
	if v, ok := os.LookupEnv(key); ok {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}

// GetInt is a tiny helper used by the seed command.
func GetInt(key string, fallback int) int {
	if v, ok := os.LookupEnv(key); ok {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}
