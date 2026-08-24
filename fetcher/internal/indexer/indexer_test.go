package indexer

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPointIDGolden(t *testing.T) {
	id, err := pointID("chunk_example")
	if err != nil {
		t.Fatal(err)
	}
	if id != "88934f4d-6b38-5279-954c-c2cfa186a86b" {
		t.Fatalf("point ID = %s", id)
	}
}

func TestBuildPayloadGoldenAndBackend(t *testing.T) {
	chunk := testChunk("one", "chunk_example")
	payload, err := buildPayload(chunk, hashText("passage: one"), pipeline("1"), false)
	if err != nil {
		t.Fatal(err)
	}
	if payload["embedding_backend"] != "tei" {
		t.Fatalf("backend = %#v", payload["embedding_backend"])
	}
	if payload["payload_hash"] != "8fe3519d2af83213df3b70815650ec6644b4b19d779322e39dd3d65da25266e4" {
		t.Fatalf("payload hash changed: %v", payload["payload_hash"])
	}
}

func TestIndexEmbedsThenSkipsUnchanged(t *testing.T) {
	provider := &fakeProvider{}
	store := newFakeStore()
	config := testConfig(t, []ChunkRecord{testChunk("one", "chunk_one")})
	first, err := Index(context.Background(), config, Dependencies{Provider: provider, Store: store})
	if err != nil {
		t.Fatal(err)
	}
	if first.EmbeddedChunks != 1 || first.NewChunks != 1 {
		t.Fatalf("first report = %+v", first)
	}
	second, err := Index(context.Background(), config, Dependencies{Provider: provider, Store: store})
	if err != nil {
		t.Fatal(err)
	}
	if second.EmbeddedChunks != 0 || second.SkippedUnchanged != 1 {
		t.Fatalf("second report = %+v", second)
	}
}

func TestIndexMigratesPointWithoutTEIBackend(t *testing.T) {
	provider := &fakeProvider{}
	store := newFakeStore()
	config := testConfig(t, []ChunkRecord{testChunk("one", "chunk_one")})
	if _, err := Index(context.Background(), config, Dependencies{Provider: provider, Store: store}); err != nil {
		t.Fatal(err)
	}
	for id, point := range store.points {
		delete(point.Payload, "embedding_backend")
		store.points[id] = point
	}
	before := provider.embedCalls
	report, err := Index(context.Background(), config, Dependencies{Provider: provider, Store: store})
	if err != nil {
		t.Fatal(err)
	}
	if report.ChangedEmbeddings != 1 || provider.embedCalls != before+1 {
		t.Fatalf("report=%+v embedCalls=%d", report, provider.embedCalls)
	}
}

func TestIndexPayloadOnlyUpdatePreservesVector(t *testing.T) {
	provider := &fakeProvider{}
	store := newFakeStore()
	original := testChunk("one", "chunk_one")
	config := testConfig(t, []ChunkRecord{original})
	if _, err := Index(context.Background(), config, Dependencies{Provider: provider, Store: store}); err != nil {
		t.Fatal(err)
	}
	var before []float64
	for _, point := range store.points {
		before = point.Vector
	}
	changed := original
	changed.Content = "rendered changed"
	changed.ContentHash = hashText(changed.Content)
	config = testConfig(t, []ChunkRecord{changed})
	report, err := Index(context.Background(), config, Dependencies{Provider: provider, Store: store})
	if err != nil {
		t.Fatal(err)
	}
	if report.PayloadOnlyUpdates != 1 || report.EmbeddedChunks != 0 {
		t.Fatalf("report = %+v", report)
	}
	for _, point := range store.points {
		if &point.Vector[0] != &before[0] {
			t.Fatal("vector was replaced")
		}
	}
}

func TestIndexPruneIsSkippedAfterInputError(t *testing.T) {
	provider := &fakeProvider{}
	store := newFakeStore()
	config := testConfig(t, []ChunkRecord{testChunk("one", "chunk_one")})
	if _, err := Index(context.Background(), config, Dependencies{Provider: provider, Store: store}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.InputFile, []byte("bad\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	config.Prune = true
	report, err := Index(context.Background(), config, Dependencies{Provider: provider, Store: store})
	if err != nil {
		t.Fatal(err)
	}
	if !report.PruneSkippedDueToErrors || len(store.points) != 1 {
		t.Fatalf("report=%+v points=%d", report, len(store.points))
	}
}

func TestIndexEmptyValidInputPrunesManagedPoints(t *testing.T) {
	provider := &fakeProvider{}
	store := newFakeStore()
	config := testConfig(t, []ChunkRecord{testChunk("one", "chunk_one")})
	if _, err := Index(context.Background(), config, Dependencies{Provider: provider, Store: store}); err != nil {
		t.Fatal(err)
	}
	empty := testConfig(t, nil)
	empty.Collection = config.Collection
	empty.Prune = true
	report, err := Index(context.Background(), empty, Dependencies{Provider: provider, Store: store})
	if err != nil {
		t.Fatal(err)
	}
	if report.DeletedChunks != 1 || len(store.points) != 0 {
		t.Fatalf("report=%+v points=%d", report, len(store.points))
	}
}

func TestIndexDryRunDoesNotEmbedOrWrite(t *testing.T) {
	provider := &fakeProvider{}
	store := newFakeStore()
	config := testConfig(t, []ChunkRecord{testChunk("one", "chunk_one")})
	config.DryRun = true
	report, err := Index(context.Background(), config, Dependencies{Provider: provider, Store: store})
	if err != nil {
		t.Fatal(err)
	}
	if report.NewChunks != 1 || report.EmbeddedChunks != 0 || provider.embedCalls != 0 || len(store.points) != 0 {
		t.Fatalf("report=%+v embeds=%d points=%d", report, provider.embedCalls, len(store.points))
	}
}

func TestIndexEmbedsDuplicateInputsOnce(t *testing.T) {
	provider := &fakeProvider{}
	store := newFakeStore()
	config := testConfig(t, []ChunkRecord{testChunk("same", "chunk_a"), testChunk("same", "chunk_b")})
	report, err := Index(context.Background(), config, Dependencies{Provider: provider, Store: store})
	if err != nil {
		t.Fatal(err)
	}
	if provider.embedCalls != 1 || report.EmbeddedChunks != 2 || report.ReusedVectorsInRun != 1 {
		t.Fatalf("report=%+v embeds=%d", report, provider.embedCalls)
	}
}

func TestIndexContinuesAfterInvalidLineWithoutContinueOnError(t *testing.T) {
	provider := &fakeProvider{}
	store := newFakeStore()
	config := testConfig(t, nil)
	file, err := os.OpenFile(config.InputFile, os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("bad\n"); err != nil {
		t.Fatal(err)
	}
	if err := json.NewEncoder(file).Encode(testChunk("valid", "chunk_valid")); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	config.BatchSize = 1
	report, err := Index(context.Background(), config, Dependencies{Provider: provider, Store: store})
	if err != nil {
		t.Fatal(err)
	}
	if report.FailedChunks != 1 || report.EmbeddedChunks != 1 {
		t.Fatalf("report = %+v", report)
	}
}

func TestIndexPreflightsProviderBeforeQdrant(t *testing.T) {
	provider := &fakeProvider{readyErr: errors.New("wrong model")}
	store := newFakeStore()
	config := testConfig(t, nil)
	report, err := Index(context.Background(), config, Dependencies{Provider: provider, Store: store})
	if err != nil {
		t.Fatal(err)
	}
	if store.pingCalls != 0 || len(report.Errors) != 1 || report.Errors[0].Stage != "provider" {
		t.Fatalf("report=%+v ping=%d", report, store.pingCalls)
	}
}

func testChunk(text, id string) ChunkRecord {
	title, page, category, version, date := "Title", "1", "docs", "1", "2026-01-01"
	return ChunkRecord{ChunkID: id, DocumentID: "document_1", Content: text, EmbeddingText: text,
		ContentHash: hashText(text), EmbeddingTextHash: hashText(text),
		Metadata: ChunkMetadata{Title: &title, Source: "document_1.md", PageID: &page, RootPageID: &page,
			Depth: 0, AncestorIDs: []string{}, Breadcrumbs: []string{"Title"}, HeadingPath: []string{},
			SectionTitle: "Title", Language: "ru", Status: "published", Category: &category, Version: &version, Date: &date}}
}

func testConfig(t *testing.T, chunks []ChunkRecord) Config {
	t.Helper()
	root := t.TempDir()
	input := filepath.Join(root, "chunks.jsonl")
	file, err := os.Create(input)
	if err != nil {
		t.Fatal(err)
	}
	encoder := json.NewEncoder(file)
	for _, chunk := range chunks {
		if err := encoder.Encode(chunk); err != nil {
			t.Fatal(err)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return Config{InputFile: input, ReportFile: filepath.Join(root, "report.json"), Collection: "test", BatchSize: 2, PipelineVersion: "1", VectorCacheSize: 8, Prune: false}
}

type fakeProvider struct {
	readyErr   error
	embedCalls int
}

func (provider *fakeProvider) Ready(context.Context) error                      { return provider.readyErr }
func (provider *fakeProvider) CountTokens(context.Context, string) (int, error) { return 10, nil }
func (provider *fakeProvider) EmbedDocuments(_ context.Context, inputs []string) ([][]float64, error) {
	provider.embedCalls++
	result := make([][]float64, len(inputs))
	for i := range result {
		result[i] = unitVector()
	}
	return result, nil
}
func unitVector() []float64 {
	vector := make([]float64, DefaultDimension)
	vector[0] = 1
	return vector
}

type fakeStore struct {
	points    map[string]Point
	pingCalls int
	deleted   int
}

func newFakeStore() *fakeStore                      { return &fakeStore{points: make(map[string]Point)} }
func (store *fakeStore) Ping(context.Context) error { store.pingCalls++; return nil }
func (store *fakeStore) EnsureCollection(context.Context, string, int, string, PipelineIdentity, bool) (CollectionState, error) {
	return CollectionState{Exists: true}, nil
}
func (store *fakeStore) EnsurePayloadIndexes(context.Context, string, []string, bool) error {
	return nil
}
func (store *fakeStore) GetPoints(_ context.Context, _ string, ids []string) ([]StoredPoint, error) {
	result := []StoredPoint{}
	for _, id := range ids {
		if point, found := store.points[id]; found {
			result = append(result, StoredPoint{ID: id, Payload: point.Payload})
		}
	}
	return result, nil
}
func (store *fakeStore) FindVector(_ context.Context, _ string, lookup VectorLookup) ([]float64, error) {
	for _, point := range store.points {
		if point.Payload["embedding_input_hash"] == lookup.EmbeddingInputHash && point.Payload["embedding_backend"] == EmbeddingBackend {
			return point.Vector, nil
		}
	}
	return nil, nil
}
func (store *fakeStore) Upsert(_ context.Context, _ string, points []Point) error {
	for _, point := range points {
		store.points[point.ID] = point
	}
	return nil
}
func (store *fakeStore) UpdatePayload(_ context.Context, _ string, points []StoredPoint) error {
	for _, update := range points {
		point := store.points[update.ID]
		point.Payload = update.Payload
		store.points[update.ID] = point
	}
	return nil
}
func (store *fakeStore) DeletePoints(_ context.Context, _ string, ids []string) error {
	for _, id := range ids {
		delete(store.points, id)
		store.deleted++
	}
	return nil
}
func (store *fakeStore) ListManagedPointIDs(context.Context, string, PipelineIdentity) ([]string, error) {
	result := make([]string, 0, len(store.points))
	for id := range store.points {
		result = append(result, id)
	}
	return result, nil
}
