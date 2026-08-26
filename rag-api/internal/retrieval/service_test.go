package retrieval

import (
	"context"
	"testing"

	"github.com/sibstark/athena/rag-api/internal/models"
	"github.com/sibstark/athena/rag-api/internal/qdrant"
)

type fakeEmbedder struct{ called int }

func (f *fakeEmbedder) EmbedQuery(context.Context, string) ([]float64, error) {
	f.called++
	return []float64{1}, nil
}

type fakeSearcher struct {
	params qdrant.SearchParams
	hits   []qdrant.Hit
}

func (f *fakeSearcher) Search(_ context.Context, p qdrant.SearchParams) ([]qdrant.Hit, error) {
	f.params = p
	return f.hits, nil
}
func (f *fakeSearcher) Collection() string { return "collection" }

func TestSearchDeduplicatesHashAndNormalizedContent(t *testing.T) {
	searcher := &fakeSearcher{hits: []qdrant.Hit{
		{Score: .9, Payload: payload("a", "hash", "Текст  один")}, {Score: .8, Payload: payload("b", "hash", "другой")}, {Score: .7, Payload: payload("c", "other", "Текст один")}, {Score: .6, Payload: payload("d", "four", "Уникальный")},
	}}
	service := New(&fakeEmbedder{}, searcher, 20)
	results, _, err := service.Search(t.Context(), models.SearchOptions{Query: "q", TopK: 8, ScoreThreshold: .65, Filters: models.Filters{Status: "active"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("results=%+v", results)
	}
	if results[0].SourceID != "S1" || results[1].ChunkID != "d" {
		t.Fatalf("unexpected order %+v", results)
	}
	if searcher.params.Limit != 24 {
		t.Fatalf("limit=%d", searcher.params.Limit)
	}
}

func TestSearchPassesRequestedCollectionsWithOneEmbedding(t *testing.T) {
	embedder := &fakeEmbedder{}
	searcher := &fakeSearcher{}
	service := New(embedder, searcher, 20)

	_, _, err := service.Search(t.Context(), models.SearchOptions{
		Query:       "q",
		TopK:        8,
		Collections: []string{"methodology_e5_small_v1", "accounting_e5_small_v1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if embedder.called != 1 {
		t.Fatalf("embedding calls=%d", embedder.called)
	}
	if len(searcher.params.Collections) != 2 {
		t.Fatalf("collections=%v", searcher.params.Collections)
	}
}

func TestSearchMapsDocumentURLFromQdrantPayload(t *testing.T) {
	searcher := &fakeSearcher{hits: []qdrant.Hit{{Score: .9, Payload: payload("a", "hash", "Текст")}}}
	service := New(&fakeEmbedder{}, searcher, 20)
	results, _, err := service.Search(t.Context(), models.SearchOptions{Query: "q", TopK: 1, ScoreThreshold: .65})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].URL != "https://hwschool.bitrix24.ru/note/document/1/" {
		t.Fatalf("results=%+v", results)
	}
}

func payload(id, hash, content string) map[string]any {
	return map[string]any{"chunk_id": id, "document_id": "page_1", "content": content, "content_hash": hash, "document_url": "https://hwschool.bitrix24.ru/note/document/1/", "breadcrumbs": []any{"Root"}, "heading_path": []any{}}
}
