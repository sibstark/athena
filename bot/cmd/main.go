package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/sibstark/athena/bot/internal/api"
	"github.com/sibstark/athena/bot/internal/bitrix"
	"github.com/sibstark/athena/bot/internal/config"
	"github.com/sibstark/athena/bot/internal/logging"
	"github.com/sibstark/athena/bot/internal/webhook"
)

func main() {
	os.Exit(execute())
}

func execute() int {
	cfg, err := config.Load()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "configuration error: %v\n", err)
		return 1
	}

	appLogger, err := logging.New(cfg.LogDir)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "logger initialization error: %v\n", err)
		return 1
	}
	slog.SetDefault(appLogger.Logger)

	exitCode := 0
	if err := run(context.Background(), cfg, appLogger.Logger); err != nil {
		appLogger.Error("application stopped with error", "error", err)
		exitCode = 1
	}
	if err := appLogger.Close(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "logger close error: %v\n", err)
		exitCode = 1
	}
	return exitCode
}

func run(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
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
	webhookHandler := webhook.CreateWebhookHandler(webhook.Options{
		Api:    apiClient,
		Bitrix: bitrixClient,
		Logger: logger,
		Options: webhook.WebhookOptions{
			ApplicationToken: cfg.ApplicationToken,
			Collection:       cfg.Collection,
			TimeoutMs:        cfg.RequestTimeoutMs,
			Context:          ctx,
		},
	})

	handler := http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		startedAt := time.Now()
		path := request.URL.Path
		statusCode := http.StatusNotFound
		responseBody := "Not found"
		logger.WarnContext(request.Context(), "Request path", "path", path, "WebhookPath", cfg.WebhookPath)

		switch {
		case path == "/health/live" && request.Method == http.MethodGet:
			statusCode = http.StatusOK
			responseBody = "Ok"
		case path == cfg.WebhookPath:
			statusCode, responseBody = webhookHandler(request)
		}

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(statusCode)
		if _, err := w.Write([]byte(responseBody)); err != nil {
			logger.WarnContext(request.Context(), "HTTP response write failed", "error", err)
		}

		if path != "/health/live" || request.Method != http.MethodGet {
			logger.InfoContext(
				request.Context(),
				"HTTP request completed",
				"method", request.Method,
				"path", path,
				"status", statusCode,
				"duration_ms", time.Since(startedAt).Milliseconds(),
			)
		}
	})

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}
	logger.Info("server listening", "port", cfg.Port, "webhook_path", cfg.WebhookPath)
	return server.ListenAndServe()
}
