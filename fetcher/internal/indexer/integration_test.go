package indexer

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"
)

func TestRealTEIAndQdrantIntegration(t *testing.T) {
	if os.Getenv("RUN_INDEXER_INTEGRATION") != "1" {
		t.Skip("set RUN_INDEXER_INTEGRATION=1 to use real TEI and Qdrant")
	}
	qdrantURL := envOr("QDRANT_URL", "http://127.0.0.1:6333")
	teiURL := envOr("TEI_URL", "http://127.0.0.1:8080")
	apiKey := os.Getenv("QDRANT_API_KEY")
	collection := fmt.Sprintf("athena_fetcher_go_test_%d", time.Now().UnixNano())
	defer deleteTestCollection(t, qdrantURL, apiKey, collection)
	config := testConfig(t, []ChunkRecord{testChunk("integration", "chunk_integration")})
	config.Collection = collection
	config.QdrantURL = qdrantURL
	config.QdrantAPIKey = apiKey
	config.TEIURL = teiURL
	report, err := Index(context.Background(), config, Dependencies{})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Errors) != 0 || report.EmbeddedChunks != 1 {
		t.Fatalf("report = %+v", report)
	}
	empty := testConfig(t, nil)
	empty.Collection = collection
	empty.QdrantURL = qdrantURL
	empty.QdrantAPIKey = apiKey
	empty.TEIURL = teiURL
	empty.Prune = true
	report, err = Index(context.Background(), empty, Dependencies{})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Errors) != 0 || report.DeletedChunks != 1 {
		t.Fatalf("prune report = %+v", report)
	}
}

func deleteTestCollection(t *testing.T, baseURL, apiKey, collection string) {
	t.Helper()
	request, err := http.NewRequest(http.MethodDelete, baseURL+"/collections/"+url.PathEscape(collection), nil)
	if err != nil {
		t.Error(err)
		return
	}
	if apiKey != "" {
		request.Header.Set("api-key", apiKey)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Errorf("cleanup collection: %v", err)
		return
	}
	_ = response.Body.Close()
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
