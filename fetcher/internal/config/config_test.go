package config

import "testing"

func TestLoadFromEnvironment(t *testing.T) {
	t.Setenv("BITRIX_WEBHOOK_URL", " https://example.bitrix24.ru/rest/1/token/ ")
	config, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if config.WebhookURL != "https://example.bitrix24.ru/rest/1/token/" {
		t.Fatalf("webhook URL = %q", config.WebhookURL)
	}
}

func TestLoadRequiresWebhookURL(t *testing.T) {
	t.Setenv("BITRIX_WEBHOOK_URL", "")
	if _, err := Load(); err == nil {
		t.Fatal("expected missing webhook error")
	}
}
