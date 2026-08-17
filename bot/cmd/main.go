package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

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
	ctx, stopServer := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stopServer()
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
	_ = server.Shutdown(ctx)
	fmt.Printf("server listening on port %s\n", cfg.Port)

	if err := server.ListenAndServe(); err != nil {
		return fmt.Errorf("HTTP server failed: %w", err)
	}
	return nil
}
