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
	if err := LoadEnvironment(); err != nil {
		return Config{}, err
	}

	webhookURL := strings.TrimSpace(os.Getenv("BITRIX_WEBHOOK_URL"))
	if webhookURL == "" {
		return Config{}, errors.New("BITRIX_WEBHOOK_URL is not set; copy .env.example to .env or set the environment variable")
	}
	return Config{WebhookURL: webhookURL}, nil
}

// LoadEnvironment loads the optional local .env without requiring Bitrix
// credentials. Commands such as normalize and index do not use the webhook.
func LoadEnvironment() error {
	if _, err := os.Stat(".env"); err == nil {
		if err := godotenv.Load(".env"); err != nil {
			return fmt.Errorf("load .env: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect .env: %w", err)
	}
	return nil
}
