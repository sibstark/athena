package bitrix

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestListWorkspacesPaginatesWithCursor(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/rest/api/1/secret/note.collection.list" {
			t.Errorf("unexpected path %s", request.URL.Path)
		}
		var body struct {
			Pagination struct {
				Limit       int     `json:"limit"`
				AfterCursor *Cursor `json:"afterCursor"`
			} `json:"pagination"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		mu.Lock()
		defer mu.Unlock()
		requests++
		writer.Header().Set("Content-Type", "application/json")
		if requests == 1 {
			if body.Pagination.Limit != 200 || body.Pagination.AfterCursor != nil {
				t.Errorf("unexpected first pagination: %+v", body.Pagination)
			}
			_, _ = writer.Write([]byte(`{"result":{"items":[{"id":27,"name":"Учебный отдел","position":1}],"nextCursor":{"position":1,"id":27}}}`))
			return
		}
		if body.Pagination.AfterCursor == nil || body.Pagination.AfterCursor.ID != 27 {
			t.Errorf("missing next cursor: %+v", body.Pagination.AfterCursor)
		}
		_, _ = writer.Write([]byte(`{"result":{"items":[{"id":25,"name":"Маркетинг","position":2}],"nextCursor":null}}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL+"/rest/1/secret/", ClientOptions{HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	items, err := client.ListWorkspaces(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].ID != 27 || items[1].ID != 25 {
		t.Fatalf("unexpected items: %+v", items)
	}
}

func TestCallRetriesRateLimitAndTransientCode(t *testing.T) {
	t.Parallel()
	var attempts int
	var delays []time.Duration
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		attempts++
		writer.Header().Set("Content-Type", "application/json")
		switch attempts {
		case 1:
			writer.Header().Set("Retry-After", "2")
			writer.WriteHeader(http.StatusTooManyRequests)
			_, _ = writer.Write([]byte(`{"error":"QUERY_LIMIT_EXCEEDED","error_description":"slow down"}`))
		case 2:
			_, _ = writer.Write([]byte(`{"error":{"code":"OVERLOAD_LIMIT","message":"busy"}}`))
		default:
			_, _ = writer.Write([]byte(`{"result":{"items":[],"nextCursor":null}}`))
		}
	}))
	defer server.Close()
	client, err := NewClient(server.URL+"/rest/api/1/secret/", ClientOptions{
		HTTPClient: server.Client(),
		Sleep: func(_ context.Context, delay time.Duration) error {
			delays = append(delays, delay)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListWorkspaces(context.Background()); err != nil {
		t.Fatal(err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d", attempts)
	}
	if len(delays) != 2 || delays[0] != 2*time.Second || delays[1] != time.Second {
		t.Fatalf("delays = %v", delays)
	}
}

func TestSecretIsRedactedFromAPIError(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusForbidden)
		_, _ = writer.Write([]byte(`{"error":{"code":"ACCESS_DENIED","message":"token secret was rejected"}}`))
	}))
	defer server.Close()
	client, err := NewClient(server.URL+"/rest/1/secret/", ClientOptions{HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ListWorkspaces(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "secret") || !strings.Contains(err.Error(), "[REDACTED]") {
		t.Fatalf("error was not redacted: %v", err)
	}
}
