package config

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	WebhookURL string
}

func Load() (Config, error) {
	if _, err := os.Stat(".env"); err == nil {
		if err := godotenv.Load(".env"); err != nil {
			return Config{}, fmt.Errorf("load .env: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return Config{}, fmt.Errorf("inspect .env: %w", err)
	}

	webhookURL := strings.TrimSpace(os.Getenv("BITRIX_WEBHOOK_URL"))
	if webhookURL == "" {
		return Config{}, errors.New("BITRIX_WEBHOOK_URL is not set; copy .env.example to .env or set the environment variable")
	}
	return Config{WebhookURL: webhookURL}, nil
}
