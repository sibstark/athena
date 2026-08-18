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
		ApiKey:     "test-api-key",
		Collection: "test-collection",
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
	t.Logf("Client created successfully: %v", client)
}

func TestCallAnswer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("Expected POST method, got %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer key" {
			t.Fatalf("Expected Bearer authorization, got %q", r.Header.Get("Authorization"))
		}
		var req answerRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			t.Fatalf("Error decoding request body: %v", err)
		}
		if req.Query != "test query" {
			t.Fatalf("Expected query 'test query', got '%s'", req.Query)
		}
		if len(req.Collections) != 1 || req.Collections[0] != "test" {
			t.Fatalf("Expected collections ['test'], got %v", req.Collections)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"answer":"test answer"}`))
	}))
	t.Cleanup(server.Close)
	client, _ := New(server.URL, Options{
		ApiKey:     "key",
		Collection: "test",
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
