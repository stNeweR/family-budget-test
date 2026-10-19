package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	DatabaseURL string
	HTTPAddr    string

	PanelURL string

	LLM      LLMConfig
	Telegram TelegramConfig
}

type LLMConfig struct {
	BaseURL     string
	APIKey      string
	Model       string
	HTTPReferer string
	Title       string
	Timeout     time.Duration
}

type TelegramConfig struct {
	BotToken      string
	WebhookURL    string
	WebhookSecret string
	WebhookPath   string
	APIBaseURL    string
}

func (c TelegramConfig) Enabled() bool {
	return c.BotToken != ""
}

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

func Load() (Config, error) {
	cfg := Config{
		DatabaseURL: getEnv("DATABASE_URL", "postgres://family:family@localhost:5432/family?sslmode=disable"),
		HTTPAddr:    getEnv("HTTP_ADDR", ":8080"),
		PanelURL:    getEnv("PANEL_URL", "http://localhost:5173"),
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

func GetInt(key string, fallback int) int {
	if v, ok := os.LookupEnv(key); ok {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}
