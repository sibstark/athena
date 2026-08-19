package bitrix

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewClient(t *testing.T) {
	options := Options{
		BotId:    42,
		BotToken: "test-bot-token",
	}

	client, err := New("https://portal.bitrix24.test/rest/1/webhook-code/", options)
	if err != nil {
		t.Fatalf("Error creating client: %v", err)
	}
	if client == nil {
		t.Fatal("Expected client, got nil")
	}
	expectedEndpoint := "https://portal.bitrix24.test/rest/1/webhook-code/imbot.v2.Chat.Message.send"
	if client.endpoint != expectedEndpoint {
		t.Fatalf("Expected endpoint %s, got %s", expectedEndpoint, client.endpoint)
	}
	if client.botId != options.BotId {
		t.Fatalf("Expected botId %d, got %d", options.BotId, client.botId)
	}
	if client.botToken != options.BotToken {
		t.Fatalf("Expected botToken %s, got %s", options.BotToken, client.botToken)
	}
}

func TestSendReply(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("Expected POST method, got %s", r.Method)
		}
		if r.Header.Get("Accept") != "application/json" {
			t.Errorf("Expected Accept header application/json, got %s", r.Header.Get("Accept"))
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Expected Content-Type header application/json, got %s", r.Header.Get("Content-Type"))
		}

		var req bitrixRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("Error decoding request body: %v", err)
		}
		if req.BotId != 42 {
			t.Errorf("Expected botId 42, got %d", req.BotId)
		}
		if req.BotToken != "test-bot-token" {
			t.Errorf("Expected botToken test-bot-token, got %s", req.BotToken)
		}
		if req.DialogId != "chat123" {
			t.Errorf("Expected dialogId chat123, got %s", req.DialogId)
		}
		if req.Fields.Message != "test message" {
			t.Errorf("Expected message test message, got %s", req.Fields.Message)
		}
		if req.Fields.ReplyId != 789 {
			t.Errorf("Expected replyId 789, got %d", req.Fields.ReplyId)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":true}`))
	}))
	t.Cleanup(server.Close)

	client := &BitrixClient{
		endpoint:   server.URL,
		botId:      42,
		botToken:   "test-bot-token",
		httpClient: server.Client(),
	}

	if err := client.SendReply(context.Background(), 789, "chat123", "test message"); err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
}

func TestWrongHTTPStatusCode(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
	}{
		{name: "404", statusCode: http.StatusNotFound},
		{name: "500", statusCode: http.StatusInternalServerError},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.statusCode)
				_, _ = w.Write([]byte(`{"error":"fail"}`))
			}))
			t.Cleanup(server.Close)

			client := &BitrixClient{
				endpoint:   server.URL,
				httpClient: server.Client(),
			}

			err := client.SendReply(context.Background(), 789, "chat123", "test message")
			if err == nil {
				t.Fatal("Expected error")
			}
			if !strings.Contains(err.Error(), test.name) {
				t.Fatalf("Expected error to contain %s, got %v", test.name, err)
			}
		})
	}
}

func TestInvalidJSONResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"result":`))
	}))
	t.Cleanup(server.Close)

	client := &BitrixClient{
		endpoint:   server.URL,
		httpClient: server.Client(),
	}

	err := client.SendReply(context.Background(), 789, "chat123", "test message")
	if err == nil {
		t.Fatal("Expected invalid JSON error")
	}
}
