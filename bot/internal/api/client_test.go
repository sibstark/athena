package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewClient(t *testing.T) {
	baseUrl := "http://localhost:8080"
	options := Options{
		ApiKey: "test-api-key",
	}
	client, err := New(baseUrl, options)
	if err != nil {
		t.Fatalf("Error creating client: %v", err)
	}
	if client.answerEndpoint != "http://localhost:8080/v1/answer" {
		t.Fatalf("Expected endpoint %s, got %s", "http://localhost:8080/v1/answer", client.answerEndpoint)
	}
	if client.collectionsEndpoint != "http://localhost:8080/v1/collections" {
		t.Fatalf("Expected endpoint %s, got %s", "http://localhost:8080/v1/collections", client.collectionsEndpoint)
	}
	if client.apiKey != options.ApiKey {
		t.Fatalf("Expected apiKey %s, got %s", options.ApiKey, client.apiKey)
	}
	t.Logf("Client created successfully: %v", client)
}

func TestCallAnswer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer key" {
			t.Fatalf("Expected Bearer authorization, got %q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/v1/collections":
			if r.Method != http.MethodGet {
				t.Fatalf("Expected GET method, got %s", r.Method)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"collections":["workspace_1","workspace_2"]}`))
		case "/v1/answer":
			if r.Method != http.MethodPost {
				t.Fatalf("Expected POST method, got %s", r.Method)
			}
			var req answerRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("Error decoding request body: %v", err)
			}
			if req.Query != "test query" {
				t.Fatalf("Expected query 'test query', got '%s'", req.Query)
			}
			if len(req.Collections) != 2 || req.Collections[0] != "workspace_1" || req.Collections[1] != "workspace_2" {
				t.Fatalf("Expected all collections, got %v", req.Collections)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"answer":"test answer"}`))
		default:
			t.Fatalf("Unexpected path %s", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)
	client, _ := New(server.URL, Options{
		ApiKey: "key",
	})
	answer, err := client.Answer(context.Background(), "test query")
	if err != nil {
		t.Fatalf("Answer() error = %v", err)
	}
	if answer != "test answer" {
		t.Fatalf("Answer() = %q", answer)
	}
}

func TestWrongHTTPStatusCode(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		statusCode int
	}{
		{"collections 500", "/v1/collections", 500},
		{"answer 404", "/v1/answer", 404},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == test.path {
					w.WriteHeader(test.statusCode)
					return
				}
				if r.URL.Path == "/v1/collections" {
					_, _ = w.Write([]byte(`{"collections":["test"]}`))
					return
				}
				_, _ = w.Write([]byte(`{"answer":"test answer"}`))
			}))
			t.Cleanup(server.Close)

			client, _ := New(server.URL, Options{
				ApiKey: "key",
			})

			_, err := client.Answer(context.Background(), "test query")
			if err == nil {
				t.Fatal("expected error")
			}

			status := strings.Fields(test.name)[1]
			if !strings.Contains(err.Error(), status) {
				t.Fatalf("expected error to contain %s, got %v", status, err)
			}
		})
	}
}

func TestInvalidCollectionsResponsePreventsAnswerRequest(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"invalid JSON", `{"collections":`},
		{"empty list", `{"collections":[]}`},
		{"missing list", `{}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			answerCalled := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/answer" {
					answerCalled = true
				}
				_, _ = w.Write([]byte(test.body))
			}))
			t.Cleanup(server.Close)

			client, _ := New(server.URL, Options{ApiKey: "key"})
			if _, err := client.Answer(context.Background(), "test query"); err == nil {
				t.Fatal("expected error")
			}
			if answerCalled {
				t.Fatal("answer endpoint must not be called")
			}
		})
	}
}
