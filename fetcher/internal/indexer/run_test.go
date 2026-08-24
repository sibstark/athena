package indexer

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestIndexRunProcessesEveryWorkspaceAndPrunes(t *testing.T) {
	root := t.TempDir()
	writeRunFixture(t, root, []map[string]any{
		{"id": 1, "name": "One", "path": "workspace_1", "qdrantCollection": "workspace_1", "status": "complete", "errors": 0},
		{"id": 2, "name": "Two", "path": "workspace_2", "qdrantCollection": "workspace_2", "status": "complete_with_warnings", "errors": 0},
	})
	provider, store := &fakeProvider{}, newFakeStore()
	report, err := IndexRun(context.Background(), RunConfig{RunDir: root, BatchSize: 2}, Dependencies{Provider: provider, Store: store})
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "complete" || report.SuccessfulWorkspaces != 2 || report.Totals.EmbeddedChunks != 2 {
		t.Fatalf("report = %+v", report)
	}
}

func TestIndexRunValidatesAllArtifactsBeforeQdrant(t *testing.T) {
	root := t.TempDir()
	writeRunFixture(t, root, []map[string]any{
		{"id": 1, "name": "One", "path": "workspace_1", "qdrantCollection": "workspace_1", "status": "complete", "errors": 0},
		{"id": 2, "name": "Two", "path": "workspace_2", "qdrantCollection": "workspace_2", "status": "complete", "errors": 0},
	})
	if err := os.Remove(filepath.Join(root, "workspace_2", "chunks.jsonl")); err != nil {
		t.Fatal(err)
	}
	store := newFakeStore()
	if _, err := IndexRun(context.Background(), RunConfig{RunDir: root}, Dependencies{Provider: &fakeProvider{}, Store: store}); err == nil {
		t.Fatal("expected validation error")
	}
	if store.pingCalls != 0 {
		t.Fatalf("Qdrant was contacted %d times", store.pingCalls)
	}
}

func writeRunFixture(t *testing.T, root string, workspaces []map[string]any) {
	t.Helper()
	writeJSONFile(t, filepath.Join(root, "normalization_manifest.json"), map[string]any{"formatVersion": 1, "sourceRun": "run_1", "workspaces": workspaces})
	for _, raw := range workspaces {
		path := filepath.Join(root, raw["path"].(string))
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		writeJSONFile(t, filepath.Join(path, "chunk_report.json"), map[string]any{"errors": 0})
		file, err := os.Create(filepath.Join(path, "chunks.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		if err := json.NewEncoder(file).Encode(testChunk("text "+raw["path"].(string), "chunk_"+raw["path"].(string))); err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func writeJSONFile(t *testing.T, path string, value any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
