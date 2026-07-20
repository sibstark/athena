package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sibstark/athena/rag-api/internal/config"
	"github.com/sibstark/athena/rag-api/internal/models"
	"github.com/sibstark/athena/rag-api/internal/observability"
)

type fakeRetriever struct {
	results []models.SearchResult
	err     error
	calls   atomic.Int32
	wait    bool
}

func (f *fakeRetriever) Search(ctx context.Context, _ models.SearchOptions) ([]models.SearchResult, time.Duration, error) {
	f.calls.Add(1)
	if f.wait {
		<-ctx.Done()
		return nil, 0, ctx.Err()
	}
	return f.results, time.Millisecond, f.err
}
func (f *fakeRetriever) Collection() string { return "methodology_e5_small_v1" }

type fakeGenerator struct {
	results []models.LLMResult
	err     error
	calls   atomic.Int32
	prompts []string
}

func (f *fakeGenerator) Generate(_ context.Context, _ string, user string) (models.LLMResult, error) {
	index := int(f.calls.Add(1)) - 1
	f.prompts = append(f.prompts, user)
	if f.err != nil {
		return models.LLMResult{}, f.err
	}
	if index >= len(f.results) {
		return models.LLMResult{}, f.err
	}
	return f.results[index], nil
}
func (f *fakeGenerator) Model() string { return "deepseek-v4-flash" }

type readyStub struct{ err error }

func (r readyStub) Ready(context.Context) error { return r.err }

func TestSearchRequiresAuthAndNeverCallsGenerator(t *testing.T) {
	retriever := &fakeRetriever{results: sampleResults()}
	generator := &fakeGenerator{}
	handler := testServer(retriever, generator).Handler()
	unauthorized := perform(handler, "/v1/search", `{"query":"q"}`, "")
	if unauthorized.Code != 401 {
		t.Fatalf("status=%d", unauthorized.Code)
	}
	response := perform(handler, "/v1/search", `{"query":"q","filters":{"root_page_id":"3035"}}`, "key")
	if response.Code != 200 {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if generator.calls.Load() != 0 {
		t.Fatal("search called generator")
	}
}

func TestAnswerSkipsGeneratorWithoutContext(t *testing.T) {
	generator := &fakeGenerator{}
	response := perform(testServer(&fakeRetriever{}, generator).Handler(), "/v1/answer", `{"query":"q"}`, "key")
	if response.Code != 200 {
		t.Fatalf("status=%d", response.Code)
	}
	if generator.calls.Load() != 0 {
		t.Fatal("generator called")
	}
	if !strings.Contains(response.Body.String(), "insufficient_context") {
		t.Fatalf("body=%s", response.Body.String())
	}
}

func TestAnswerReturnsOnlyMentionedQdrantSources(t *testing.T) {
	generator := &fakeGenerator{results: []models.LLMResult{{Content: "Шаг из первого источника [S1]."}}}
	response := perform(testServer(&fakeRetriever{results: sampleResults()}, generator).Handler(), "/v1/answer", `{"query":"Как провести урок?"}`, "key")
	if response.Code != 200 {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var result models.AnswerResponse
	json.NewDecoder(response.Body).Decode(&result)
	if len(result.Sources) != 1 || result.Sources[0].ChunkID != "chunk_1" {
		t.Fatalf("sources=%+v", result.Sources)
	}
	if strings.Contains(generator.prompts[0], "Второй") == false {
		t.Fatal("selected context missing")
	}
}

func TestAnswerRepairsInvalidCitationOnce(t *testing.T) {
	generator := &fakeGenerator{results: []models.LLMResult{{Content: "Ответ [S99]."}, {Content: "Исправленный ответ [S2]."}}}
	response := perform(testServer(&fakeRetriever{results: sampleResults()}, generator).Handler(), "/v1/answer", `{"query":"q"}`, "key")
	if response.Code != 200 || generator.calls.Load() != 2 {
		t.Fatalf("status=%d calls=%d body=%s", response.Code, generator.calls.Load(), response.Body.String())
	}
	if strings.Contains(response.Body.String(), "S99") {
		t.Fatal("invented source returned")
	}
}

func TestAnswerFailsAfterOneBadRepair(t *testing.T) {
	generator := &fakeGenerator{results: []models.LLMResult{{Content: "Без цитат"}, {Content: "Снова [S42]"}}}
	response := perform(testServer(&fakeRetriever{results: sampleResults()}, generator).Handler(), "/v1/answer", `{"query":"q"}`, "key")
	if response.Code != 502 || !strings.Contains(response.Body.String(), "generation_validation_failed") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestUnknownMetadataFilterIsRejected(t *testing.T) {
	response := perform(testServer(&fakeRetriever{}, &fakeGenerator{}).Handler(), "/v1/search", `{"query":"q","filters":{"metadata":{"page_id":"1"}}}`, "key")
	if response.Code != 400 {
		t.Fatalf("status=%d", response.Code)
	}
}

func TestClientCancellationReachesRetriever(t *testing.T) {
	retriever := &fakeRetriever{wait: true}
	handler := testServer(retriever, &fakeGenerator{}).Handler()
	ctx, cancel := context.WithCancel(context.Background())
	request := httptest.NewRequest(http.MethodPost, "/v1/search", bytes.NewBufferString(`{"query":"q"}`)).WithContext(ctx)
	request.Header.Set("Authorization", "Bearer key")
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { handler.ServeHTTP(response, request); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("request did not cancel")
	}
}

func testServer(retriever *fakeRetriever, generator *fakeGenerator) *Server {
	cfg := config.Config{APIKey: "key", HTTPMaxBodyBytes: 1 << 20, DefaultTopK: 8, MaxTopK: 20, ScoreThreshold: .65, MaxQueryChars: 4000, MaxContextChars: 30000, MaxContextChunks: 8, TEITimeout: time.Second, QdrantTimeout: time.Second}
	return New(cfg, retriever, generator, readyStub{}, readyStub{}, observability.NewMetrics(), slog.New(slog.NewTextHandler(io.Discard, nil)))
}
func perform(handler http.Handler, path, body, key string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	if key != "" {
		request.Header.Set("Authorization", "Bearer "+key)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
func sampleResults() []models.SearchResult {
	return []models.SearchResult{{SourceID: "S1", ChunkID: "chunk_1", DocumentID: "page_1", Score: .9, Content: "Первый", Title: "Документ 1", Breadcrumbs: []string{"Root"}}, {SourceID: "S2", ChunkID: "chunk_2", DocumentID: "page_2", Score: .8, Content: "Второй", Title: "Документ 2", Breadcrumbs: []string{"Root"}}}
}
