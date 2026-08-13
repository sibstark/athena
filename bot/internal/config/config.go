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
	ApplicationToken string
	RestWebhookUrl   string
	BotId            int
	BotToken         string
	RagApiUrl        string
	RagApiKey        string
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
		ApplicationToken: os.Getenv("APPLICATION_TOKEN"),
		RestWebhookUrl:   os.Getenv("REST_WEBHOOK_URL"),
		BotId:            envInt(os.Getenv("BotId"), 0),
		BotToken:         os.Getenv("BOT_TOKEN"),
		RagApiUrl:        os.Getenv("RAG_API_URL"),
		RagApiKey:        os.Getenv("RAG_API_KEY"),
		Collection:       os.Getenv("COLLECTION"),
		RequestTimeoutMs: envInt(os.Getenv("REQUEST_TIMEOUT_MS"), 5000),
	}
	return cfg, nil
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
		"RAG_API_URL",
		"RAG_API_KEY",
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
