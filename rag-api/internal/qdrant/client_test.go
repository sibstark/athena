package qdrant

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/sibstark/athena/rag-api/internal/models"
)

func TestListCollectionsUsesReadOnlyEndpointAndReturnsSortedUniqueNames(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/collections" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("api-key") != "secret" {
			t.Error("missing API key")
		}
		json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"collections": []any{
			map[string]any{"name": "workspace_7"},
			map[string]any{"name": "workspace_1"},
			map[string]any{"name": "workspace_7"},
		}}})
	}))
	defer server.Close()

	collections, err := New(server.URL, "secret", "default", time.Second, nil).ListCollections(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(collections) != 2 || collections[0] != "workspace_1" || collections[1] != "workspace_7" {
		t.Fatalf("collections=%v", collections)
	}
}

func TestListCollectionsReturnsNonNilEmptySlice(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"collections": []any{}}})
	}))
	defer server.Close()

	collections, err := New(server.URL, "", "default", time.Second, nil).ListCollections(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if collections == nil || len(collections) != 0 {
		t.Fatalf("collections=%v", collections)
	}
}

func TestListCollectionsWrapsInvalidAndHTTPResponses(t *testing.T) {
	t.Run("missing result", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{"result": nil})
		}))
		defer server.Close()
		_, err := New(server.URL, "", "default", time.Second, nil).ListCollections(t.Context())
		if !errors.Is(err, models.ErrQdrantUnavailable) {
			t.Fatalf("error=%v", err)
		}
	})

	t.Run("empty name", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"collections": []any{map[string]any{"name": " "}}}})
		}))
		defer server.Close()
		_, err := New(server.URL, "", "default", time.Second, nil).ListCollections(t.Context())
		if !errors.Is(err, models.ErrQdrantUnavailable) {
			t.Fatalf("error=%v", err)
		}
	})

	t.Run("http error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		}))
		defer server.Close()
		_, err := New(server.URL, "", "default", time.Second, nil).ListCollections(t.Context())
		if !errors.Is(err, models.ErrQdrantUnavailable) {
			t.Fatalf("error=%v", err)
		}
	})
}

func TestSearchBuildsFlatPayloadFilters(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/collections/methodology_e5_small_v1/points/search" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("api-key") != "secret" {
			t.Error("missing API key")
		}
		json.NewDecoder(r.Body).Decode(&body)
		json.NewEncoder(w).Encode(map[string]any{"result": []any{map[string]any{"id": "1", "score": 0.8, "payload": map[string]any{"chunk_id": "chunk_1"}}}})
	}))
	defer server.Close()
	client := New(server.URL, "secret", "methodology_e5_small_v1", time.Second, nil)
	hits, err := client.Search(t.Context(), SearchParams{Vector: []float64{1}, Limit: 8, ScoreThreshold: .65, Filters: models.Filters{RootPageID: "3035", Language: "ru"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits=%d", len(hits))
	}
	filter := body["filter"].(map[string]any)
	must := filter["must"].([]any)
	if len(must) != 2 {
		t.Fatalf("must=%v", must)
	}
}

func TestSearchFanoutsAcrossRequestedCollections(t *testing.T) {
	seen := make(map[string]bool)
	var seenMu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		collection := r.URL.Path[len("/collections/"):]
		collection = collection[:len(collection)-len("/points/search")]
		seenMu.Lock()
		seen[collection] = true
		seenMu.Unlock()
		json.NewEncoder(w).Encode(map[string]any{"result": []any{map[string]any{
			"id":      collection,
			"score":   0.8,
			"payload": map[string]any{"chunk_id": collection + "_chunk"},
		}}})
	}))
	defer server.Close()

	client := New(server.URL, "secret", "methodology_e5_small_v1", time.Second, nil)
	hits, err := client.Search(t.Context(), SearchParams{
		Vector:      []float64{1},
		Limit:       8,
		Collections: []string{"methodology_e5_small_v1", "accounting_e5_small_v1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 || !seen["methodology_e5_small_v1"] || !seen["accounting_e5_small_v1"] {
		t.Fatalf("seen=%v hits=%+v", seen, hits)
	}
	for _, hit := range hits {
		if hit.Collection == "" {
			t.Fatalf("hit has no collection: %+v", hit)
		}
	}
}

func TestReadyValidatesCollection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"status": "green", "points_count": 1, "config": map[string]any{"params": map[string]any{"vectors": map[string]any{"size": 384, "distance": "Cosine"}}, "metadata": map[string]any{"embedding_model": models.EmbeddingModel, "embedding_model_revision": models.EmbeddingRevision}}}})
	}))
	defer server.Close()
	if err := New(server.URL, "", "collection", time.Second, nil).Ready(t.Context()); err != nil {
		t.Fatal(err)
	}
}
