package config

import (
	"strings"
	"testing"
)

func TestLoadRequiredEnvs(t *testing.T) {
	emptyEnvNames := []string{
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

	for _, name := range emptyEnvNames {
		t.Setenv(name, "")
	}
	_, err := Load()
	if err == nil {
		t.Fatalf("must have error")
	}
	errText := err.Error()
	for _, name := range emptyEnvNames {
		if !strings.Contains(errText, name) {
			t.Fatalf("must contains %s", name)
		}
	}
}

func TestMustContainsAllRequiredEnvs(t *testing.T) {
	t.Setenv("PORT", "8080")
	t.Setenv("WEBHOOK_PATH", "/webhook")
	t.Setenv("APPLICATION_TOKEN", "test-token")
	t.Setenv("REST_WEBHOOK_URL", "http://localhost:8080/webhook")
	t.Setenv("BOT_ID", "test-bot-id")
	t.Setenv("BOT_TOKEN", "test-bot-token")
	t.Setenv("RAG_API_URL", "http://localhost:8080/rag")
	t.Setenv("RAG_API_KEY", "test-rag-api-key")
	t.Setenv("COLLECTION", "test-collection")
	t.Setenv("REQUEST_TIMEOUT_MS", "5000")
	cfg, err := Load()

	if err != nil {
		t.Fatalf("must not have error")
	}
	expectedCfg := Config{
		Port:             "8080",
		WebhookPath:      "/webhook",
		ApplicationToken: "test-token",
		RestWebhookUrl:   "http://localhost:8080/webhook",
		BotId:            0,
		BotToken:         "test-bot-token",
		RagApiUrl:        "http://localhost:8080/rag",
		RagApiKey:        "test-rag-api-key",
		Collection:       "test-collection",
		RequestTimeoutMs: 5000,
	}
	if cfg != expectedCfg {
		t.Fatalf("expected %v, got %v", expectedCfg, cfg)
	}
}
