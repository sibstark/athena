package ragapi_test

import (
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/sibstark/athena/rag-api/internal/deepseek"
	"github.com/sibstark/athena/rag-api/internal/models"
	"github.com/sibstark/athena/rag-api/internal/qdrant"
	"github.com/sibstark/athena/rag-api/internal/tei"
)

func TestRealTEIAndQdrant(t *testing.T) {
	if os.Getenv("RUN_RAG_API_INTEGRATION") != "1" {
		t.Skip("set RUN_RAG_API_INTEGRATION=1")
	}
	contract := integrationContract(t)
	teiClient := tei.New(env("TEI_URL", "http://127.0.0.1:8080"), 30*time.Second, contract, nil)
	qdrantClient := qdrant.New(env("QDRANT_URL", "http://127.0.0.1:6333"), os.Getenv("QDRANT_API_KEY"), env("QDRANT_COLLECTION", "methodology_e5_small_v1"), 10*time.Second, contract, nil)
	if err := teiClient.Ready(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := qdrantClient.Ready(t.Context()); err != nil {
		t.Fatal(err)
	}
	vector, err := teiClient.EmbedQuery(t.Context(), "Что делать, если у ребёнка нет абакуса?")
	if err != nil {
		t.Fatal(err)
	}
	hits, err := qdrantClient.Search(t.Context(), qdrant.SearchParams{Vector: vector, Limit: 3, ScoreThreshold: -1, Filters: models.Filters{Language: "ru"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("real search returned no hits")
	}
}

func integrationContract(t *testing.T) models.EmbeddingContract {
	t.Helper()
	required := func(name string, allowEmpty bool) string {
		value, exists := os.LookupEnv(name)
		if !exists || (!allowEmpty && value == "") {
			t.Fatalf("%s is required", name)
		}
		return value
	}
	integer := func(name string) int {
		value, err := strconv.Atoi(required(name, false))
		if err != nil || value <= 0 {
			t.Fatalf("%s must be a positive integer", name)
		}
		return value
	}
	boolean := func(name string) bool {
		value, err := strconv.ParseBool(required(name, false))
		if err != nil {
			t.Fatalf("%s must be true or false", name)
		}
		return value
	}
	return models.EmbeddingContract{
		ModelID: required("EMBEDDING_MODEL_ID", false), Revision: required("EMBEDDING_MODEL_REVISION", false),
		Dimension: integer("EMBEDDING_DIMENSION"), DType: required("EMBEDDING_DTYPE", false),
		Pooling: required("EMBEDDING_POOLING", false), Normalized: boolean("EMBEDDING_NORMALIZED"),
		DocumentPrefix: required("EMBEDDING_DOCUMENT_PREFIX", true), QueryPrefix: required("EMBEDDING_QUERY_PREFIX", false),
		MaxInputTokens: integer("EMBEDDING_MAX_INPUT_TOKENS"), AutoTruncate: boolean("EMBEDDING_AUTO_TRUNCATE"),
		PipelineVersion: required("EMBEDDING_PIPELINE_VERSION", false), PointNamespace: required("EMBEDDING_POINT_NAMESPACE", false),
	}
}

func TestRealDeepSeek(t *testing.T) {
	if os.Getenv("RUN_DEEPSEEK_INTEGRATION") != "1" {
		t.Skip("set RUN_DEEPSEEK_INTEGRATION=1")
	}
	key := os.Getenv("DEEPSEEK_API_KEY")
	if key == "" {
		t.Fatal("DEEPSEEK_API_KEY is required")
	}
	maxTokens := 1200
	if raw := os.Getenv("DEEPSEEK_MAX_OUTPUT_TOKENS"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			maxTokens = parsed
		}
	}
	client := deepseek.New(deepseek.Options{BaseURL: env("DEEPSEEK_BASE_URL", "https://api.deepseek.com"), APIKey: key, Model: env("DEEPSEEK_MODEL", "deepseek-v4-flash"), Timeout: 60 * time.Second, MaxTokens: maxTokens, MaxConcurrency: 1})
	result, err := client.Generate(t.Context(), "Отвечай кратко.", "Ответь одним словом: готов?")
	if err != nil {
		t.Fatal(err)
	}
	if result.Content == "" {
		t.Fatal("empty completion")
	}
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
