package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Port             string
	WebhookPath      string
	LogDir           string
	ApplicationToken string
	RestWebhookUrl   string
	BotId            int
	BotToken         string
	ApiUrl           string
	ApiKey           string
	Collection       string
	RequestTimeoutMs int
}

func Load() (Config, error) {
	if err := validateEnvirnonment(); err != nil {
		return Config{}, err
	}
	cfg := Config{
		Port:             os.Getenv("PORT"),
		WebhookPath:      os.Getenv("WEBHOOK_PATH"),
		LogDir:           envString(os.Getenv("LOG_DIR"), "./logs"),
		ApplicationToken: os.Getenv("APPLICATION_TOKEN"),
		RestWebhookUrl:   os.Getenv("REST_WEBHOOK_URL"),
		BotId:            envInt(os.Getenv("BOT_ID"), 0),
		BotToken:         os.Getenv("BOT_TOKEN"),
		ApiUrl:           os.Getenv("API_URL"),
		ApiKey:           os.Getenv("API_KEY"),
		Collection:       os.Getenv("COLLECTION"),
		RequestTimeoutMs: envInt(os.Getenv("REQUEST_TIMEOUT_MS"), 5000),
	}
	return cfg, nil
}

func envString(value, fallback string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return fallback
}

func envInt(value string, fallback int) int {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func validateEnvirnonment() error {
	var missing []string
	envNames := []string{
		"PORT",
		"WEBHOOK_PATH",
		"APPLICATION_TOKEN",
		"REST_WEBHOOK_URL",
		"BOT_ID",
		"BOT_TOKEN",
		"API_URL",
		"API_KEY",
		"COLLECTION",
		"REQUEST_TIMEOUT_MS",
	}
	for _, key := range envNames {
		if val := strings.TrimSpace(os.Getenv(key)); val == "" {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required environment variables: %s", strings.Join(missing, " "))
	}
	return nil
}
