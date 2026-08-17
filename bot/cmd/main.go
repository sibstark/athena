package main

import (
	"context"
	"fmt"
	"os"

	"github.com/sibstark/athena/bot/internal/api"
	"github.com/sibstark/athena/bot/internal/bitrix"
	"github.com/sibstark/athena/bot/internal/config"
	"github.com/sibstark/athena/bot/internal/webhook"
)

func main() {
	err := run()
	if err != nil {
		fmt.Printf("init bot run err %s", err.Error())
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("configuration err %w", err)
	}
	apiClient, err := api.New(cfg.RagApiUrl, api.Options{
		ApiKey:     cfg.RagApiKey,
		Collection: cfg.Collection,
	})
	if err != nil {
		return fmt.Errorf("API client initialization err %w", err)
	}
	bitrixClient, err := bitrix.New(cfg.WebhookPath, bitrix.Options{
		BotId:    cfg.BotId,
		BotToken: cfg.ApplicationToken,
	})
	if err != nil {
		return fmt.Errorf("Bitrix client initialization err %w", err)
	}
	ctx := context.Background()
	webhook := webhook.CreateWebhookHandler(webhook.Options{
		Api:    apiClient,
		Bitrix: bitrixClient,
		Options: webhook.WebhookOptions{
			Context:          ctx,
			ApplicationToken: cfg.ApplicationToken,
			Collection:       cfg.Collection,
			TimeoutMs:        cfg.RequestTimeoutMs,
		},
	})

	return nil
}
