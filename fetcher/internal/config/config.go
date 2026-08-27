package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
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

// LoadEnvironment loads the single repository-level .env without overriding
// variables already provided by the process.
func LoadEnvironment() error {
	workingDirectory, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve working directory: %w", err)
	}
	candidates := []string{filepath.Join(workingDirectory, ".env")}
	if filepath.Base(workingDirectory) == "fetcher" {
		candidates = []string{filepath.Join(workingDirectory, "..", ".env"), filepath.Join(workingDirectory, ".env")}
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			if err := godotenv.Load(candidate); err != nil {
				return fmt.Errorf("load %s: %w", candidate, err)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect %s: %w", candidate, err)
		}
	}
	return nil
}
