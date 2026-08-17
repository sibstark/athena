package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/sibstark/athena/bot/internal/api"
	"github.com/sibstark/athena/bot/internal/bitrix"
	"github.com/sibstark/athena/bot/internal/config"
	"github.com/sibstark/athena/bot/internal/webhook"
)

func main() {
	err := run(context.Background())
	if err != nil {
		fmt.Printf("init bot run err %s", err.Error())
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("configuration err %w", err)
	}
	apiClient, err := api.New(cfg.ApiUrl, api.Options{
		ApiKey:     cfg.ApiKey,
		Collection: cfg.Collection,
	})
	if err != nil {
		return fmt.Errorf("API client initialization err %w", err)
	}
	bitrixClient, err := bitrix.New(cfg.RestWebhookUrl, bitrix.Options{
		BotId:    cfg.BotId,
		BotToken: cfg.BotToken,
	})
	if err != nil {
		return fmt.Errorf("Bitrix client initialization err %w", err)
	}
	webhook := webhook.CreateWebhookHandler(webhook.Options{
		Api:    apiClient,
		Bitrix: bitrixClient,
		Options: webhook.WebhookOptions{
			ApplicationToken: cfg.ApplicationToken,
			Collection:       cfg.Collection,
			TimeoutMs:        cfg.RequestTimeoutMs,
			Context:          ctx,
		},
	})

	handler := http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		path := request.URL.Path
		switch {
		case path == "/health/live" && request.Method == http.MethodGet:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("Ok"))
		case path == cfg.WebhookPath:
			statusCode, status := webhook(request)
			w.WriteHeader(statusCode)
			_, _ = w.Write([]byte(status))

		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("Not found"))
		}
	})

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}
	fmt.Printf("server listening on port %s\n", cfg.Port)
	return server.ListenAndServe()
}
