package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sibstark/athena/rag-api/internal/config"
	"github.com/sibstark/athena/rag-api/internal/deepseek"
	"github.com/sibstark/athena/rag-api/internal/httpapi"
	"github.com/sibstark/athena/rag-api/internal/observability"
	"github.com/sibstark/athena/rag-api/internal/qdrant"
	"github.com/sibstark/athena/rag-api/internal/retrieval"
	"github.com/sibstark/athena/rag-api/internal/tei"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		healthcheck()
		return
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("configuration error: %w", err)
	}
	logger := observability.NewLogger(cfg.LogLevel)
	metrics := observability.NewMetrics()
	teiClient := tei.New(cfg.TEIURL, cfg.TEITimeout, func(d time.Duration, e error) { metrics.Observe("tei", d, e) })
	qdrantClient := qdrant.New(cfg.QdrantURL, cfg.QdrantAPIKey, cfg.QdrantCollection, cfg.QdrantTimeout, func(d time.Duration, e error) { metrics.Observe("qdrant", d, e) })
	deepSeekClient := deepseek.New(deepseek.Options{BaseURL: cfg.DeepSeekBaseURL, APIKey: cfg.DeepSeekAPIKey, Model: cfg.DeepSeekModel, Timeout: cfg.DeepSeekTimeout, MaxTokens: cfg.DeepSeekMaxOutputTokens, Thinking: cfg.DeepSeekThinkingEnabled, MaxConcurrency: cfg.DeepSeekMaxConcurrency, Observe: func(d time.Duration, e error) { metrics.Observe("deepseek", d, e) }, OnRetry: metrics.Retry})
	retriever := retrieval.New(teiClient, qdrantClient, cfg.MaxTopK)
	api := httpapi.New(cfg, retriever, deepSeekClient, teiClient, qdrantClient, metrics, logger)
	httpServer := &http.Server{Addr: cfg.HTTPAddr, Handler: api.Handler(), ReadHeaderTimeout: cfg.ReadHeaderTimeout, ReadTimeout: cfg.ReadTimeout, WriteTimeout: cfg.WriteTimeout, IdleTimeout: cfg.IdleTimeout}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return serve(ctx, httpServer, cfg.ShutdownTimeout, logger)
}

func serve(ctx context.Context, server *http.Server, shutdownTimeout time.Duration, logger *slog.Logger) error {
	errorsChannel := make(chan error, 1)
	go func() {
		logger.Info("rag-api listening", "addr", server.Addr)
		errorsChannel <- server.ListenAndServe()
	}()
	select {
	case err := <-errorsChannel:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		logger.Info("rag-api shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			return fmt.Errorf("graceful shutdown failed: %w", err)
		}
		if err := <-errorsChannel; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}

func healthcheck() {
	url := os.Getenv("HEALTHCHECK_URL")
	if url == "" {
		url = "http://127.0.0.1:8080/health/live"
	}
	client := http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck failed")
		os.Exit(1)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, "healthcheck failed")
		os.Exit(1)
	}
}
