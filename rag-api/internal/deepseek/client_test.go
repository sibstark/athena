package deepseek

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sibstark/athena/rag-api/internal/models"
)

func TestGenerateDisablesThinkingAndReturnsUsage(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer top-secret" {
			t.Error("authorization missing")
		}
		json.NewDecoder(r.Body).Decode(&body)
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": "Ответ [S1]"}}}, "usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 3}})
	}))
	defer server.Close()
	client := testClient(server.URL, "top-secret")
	result, err := client.Generate(t.Context(), "system", "user")
	if err != nil {
		t.Fatal(err)
	}
	if body["model"] != "deepseek-v4-flash" {
		t.Fatalf("model=%v", body["model"])
	}
	thinking := body["thinking"].(map[string]any)
	if thinking["type"] != "disabled" {
		t.Fatalf("thinking=%v", thinking)
	}
	if result.InputTokens == nil || *result.InputTokens != 10 {
		t.Fatalf("usage=%+v", result)
	}
}

func TestGenerateRetriesRateLimitAndServerErrors(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := calls.Add(1)
		if attempt < 3 {
			if attempt == 1 {
				w.WriteHeader(429)
			} else {
				w.WriteHeader(503)
			}
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": "ok"}}}})
	}))
	defer server.Close()
	retries := 0
	client := testClient(server.URL, "secret")
	client.onRetry = func() { retries++ }
	result, err := client.Generate(t.Context(), "s", "u")
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "ok" || calls.Load() != 3 || retries != 2 {
		t.Fatalf("result=%+v calls=%d retries=%d", result, calls.Load(), retries)
	}
}

func TestGenerateDoesNotRetryOrdinary4xx(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(400) }))
	defer server.Close()
	client := testClient(server.URL, "never-log-this")
	_, err := client.Generate(t.Context(), "s", "u")
	if err == nil || calls.Load() != 1 {
		t.Fatalf("err=%v calls=%d", err, calls.Load())
	}
	if strings.Contains(err.Error(), "never-log-this") {
		t.Fatal("secret leaked")
	}
}

func TestGenerateHonorsCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	client := testClient(server.URL, "secret")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := client.Generate(ctx, "s", "u")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
}

func testClient(url, key string) *Client {
	return New(Options{BaseURL: url, APIKey: key, Model: "deepseek-v4-flash", Timeout: time.Second, MaxTokens: 1200, MaxConcurrency: 2, Sleep: func(context.Context, time.Duration) error { return nil }, Jitter: func() float64 { return 0 }})
}

var _ = models.ErrLLMUnavailable
