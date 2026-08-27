package indexer

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTEIClientExactContract(t *testing.T) {
	embedding := testEmbeddingConfig()
	calls := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls[request.URL.Path]++
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/health":
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write([]byte("{}"))
		case "/info":
			_ = json.NewEncoder(writer).Encode(map[string]any{"model_id": embedding.ModelID, "model_sha": embedding.ModelRevision, "max_input_length": embedding.MaxInputTokens, "auto_truncate": embedding.AutoTruncate, "max_batch_requests": 4})
		case "/tokenize":
			var body map[string]any
			_ = json.NewDecoder(request.Body).Decode(&body)
			if body["inputs"] != embedding.DocumentPrefix+"hello" || body["add_special_tokens"] != true {
				t.Errorf("tokenize body = %#v", body)
			}
			_ = json.NewEncoder(writer).Encode([]int{1, 2, 3})
		case "/embed":
			var body map[string]any
			_ = json.NewDecoder(request.Body).Decode(&body)
			if body["normalize"] != true || body["truncate"] != false {
				t.Errorf("embed body = %#v", body)
			}
			_ = json.NewEncoder(writer).Encode([][]float64{unitVector()})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	client := NewTEIClient(server.URL, time.Second, embedding)
	if err := client.Ready(context.Background()); err != nil {
		t.Fatal(err)
	}
	count, err := client.CountTokens(context.Background(), embedding.DocumentPrefix+"hello")
	if err != nil || count != 3 {
		t.Fatalf("count=%d err=%v", count, err)
	}
	vectors, err := client.EmbedDocuments(context.Background(), []string{embedding.DocumentPrefix + "hello"})
	if err != nil || len(vectors) != 1 {
		t.Fatalf("vectors=%d err=%v", len(vectors), err)
	}
	if calls["/health"] != 1 || calls["/info"] != 1 {
		t.Fatalf("calls = %#v", calls)
	}
}

func TestQdrantVectorReuseRequiresTEIBackend(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("api-key") != "secret" {
			t.Error("missing api-key")
		}
		var body map[string]any
		_ = json.NewDecoder(request.Body).Decode(&body)
		filter := body["filter"].(map[string]any)
		conditions := filter["must"].([]any)
		found := false
		for _, raw := range conditions {
			condition := raw.(map[string]any)
			if condition["key"] == "embedding_backend" {
				found = condition["match"].(map[string]any)["value"] == "tei"
			}
		}
		if !found {
			t.Error("embedding_backend=tei filter missing")
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{"result": map[string]any{"points": []any{map[string]any{"id": "one", "vector": unitVector()}}, "next_page_offset": nil}, "status": "ok"})
	}))
	defer server.Close()
	client := NewQdrantClient(server.URL, "secret", time.Second)
	embedding := testEmbeddingConfig()
	vector, err := client.FindVector(context.Background(), "collection", VectorLookup{EmbeddingInputHash: "hash", Pipeline: pipeline(embedding)})
	if err != nil || len(vector) != embedding.Dimension {
		t.Fatalf("vector=%d err=%v", len(vector), err)
	}
}

func TestQdrantCollectionCreationContract(t *testing.T) {
	embedding := testEmbeddingConfig()
	created := false
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/collections/test/exists":
			_ = json.NewEncoder(writer).Encode(map[string]any{"result": map[string]any{"exists": false}, "status": "ok"})
		case request.Method == http.MethodPut && request.URL.Path == "/collections/test":
			var body map[string]any
			_ = json.NewDecoder(request.Body).Decode(&body)
			vectors := body["vectors"].(map[string]any)
			metadata := body["metadata"].(map[string]any)
			if vectors["size"] != float64(embedding.Dimension) || vectors["distance"] != "Cosine" {
				t.Errorf("vectors = %#v", vectors)
			}
			if metadata["embedding_model"] != embedding.ModelID || metadata["point_namespace"] != embedding.PointNamespace {
				t.Errorf("metadata = %#v", metadata)
			}
			created = true
			_ = json.NewEncoder(writer).Encode(map[string]any{"result": true, "status": "ok"})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	client := NewQdrantClient(server.URL, "", time.Second)
	state, err := client.EnsureCollection(context.Background(), "test", embedding.Dimension, "Cosine", pipeline(embedding), false)
	if err != nil {
		t.Fatal(err)
	}
	if !created || !state.Created || state.Exists {
		t.Fatalf("state=%+v created=%v", state, created)
	}
}
