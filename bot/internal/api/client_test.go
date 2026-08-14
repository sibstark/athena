package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewClient(t *testing.T) {
	baseUrl := "http://localhost:8080"
	options := Options{
		ApiKey:     "test-api-key",
		Collection: "test-collection",
		TimeoutMs:  5000,
	}
	client, err := New(baseUrl, options)
	if err != nil {
		t.Fatalf("Error creating client: %v", err)
	}
	if client.endpoint != "http://localhost:8080/v1/answer" {
		t.Fatalf("Expected endpoint %s, got %s", "http://localhost:8080/v1/answer", client.endpoint)
	}
	if client.apiKey != options.ApiKey {
		t.Fatalf("Expected apiKey %s, got %s", options.ApiKey, client.apiKey)
	}
	if client.collection != options.Collection {
		t.Fatalf("Expected collection %s, got %s", options.Collection, client.collection)
	}
	if client.httpClient.Timeout != (time.Duration(options.TimeoutMs) * time.Millisecond) {
		t.Fatalf("Expected timeout %v, got %v", time.Duration(options.TimeoutMs)*time.Millisecond, client.httpClient.Timeout)
	}
	t.Logf("Client created successfully: %v", client)
}

func TestCallAnswer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Fatalf("Expected POST method, got %s", r.Method)
		}
		var req answerRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			t.Fatalf("Error decoding request body: %v", err)
		}
		if req.Query != "test query" {
			t.Fatalf("Expected query 'test query', got '%s'", req.Query)
		}
		if req.Collection != "test" {
			t.Fatalf("Expected collection 'test', got '%s'", req.Collection)
		}
	}))
	defer server.Close()
	client, _ := New(server.URL, Options{
		ApiKey:     "key",
		Collection: "test",
		TimeoutMs:  1000,
	})
	ctx := context.Background()
	client.Answer(ctx, "test query")
}

func TestWrongHTTPStatusCode(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
	}{
		{"500", 500, `{"error":"fail"}`},
		{"404", 404, `{}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(test.statusCode)
				_, _ = w.Write([]byte(test.body))
			}))
			t.Cleanup(server.Close)

			client, _ := New(server.URL, Options{
				ApiKey:     "key",
				Collection: "test",
				TimeoutMs:  1000,
			})

			_, err := client.Answer(context.Background(), "test query")
			if err == nil {
				t.Fatal("expected error")
			}

			if !strings.Contains(err.Error(), test.name) {
				t.Fatalf("expected error to contain %s, got %v", test.name, err)
			}
		})
	}
}
