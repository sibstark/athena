package indexer

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestIndexRunProcessesEveryWorkspaceAndPrunes(t *testing.T) {
	root := filepath.Join(t.TempDir(), "chunks_1")
	writeRunFixture(t, root, []map[string]any{
		{"id": 1, "name": "One", "path": "workspace_1", "qdrantCollection": "workspace_1", "status": "complete", "errors": 0},
		{"id": 2, "name": "Two", "path": "workspace_2", "qdrantCollection": "workspace_2", "status": "complete", "errors": 0},
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

func TestIndexRunAcceptsRelativeRunDirectoryWithMultipleWorkspaces(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "chunks_1")
	writeRunFixture(t, root, []map[string]any{
		{"id": 1, "name": "One", "path": "workspace_1", "qdrantCollection": "workspace_1", "status": "complete", "errors": 0},
		{"id": 2, "name": "Two", "path": "workspace_2", "qdrantCollection": "workspace_2", "status": "complete", "errors": 0},
	})
	t.Chdir(parent)

	report, err := IndexRun(context.Background(), RunConfig{RunDir: "chunks_1", BatchSize: 2}, Dependencies{Provider: &fakeProvider{}, Store: newFakeStore()})
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "complete" || report.SuccessfulWorkspaces != 2 {
		t.Fatalf("report = %+v", report)
	}
}

func TestIndexRunValidatesAllArtifactsBeforeQdrant(t *testing.T) {
	root := filepath.Join(t.TempDir(), "chunks_1")
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

func TestIndexRunSkipsFailedChunkWorkspaceWithoutChangingQdrant(t *testing.T) {
	root := filepath.Join(t.TempDir(), "chunks_1")
	writeRunFixture(t, root, []map[string]any{
		{"id": 1, "name": "Failed", "path": "workspace_1", "qdrantCollection": "workspace_1", "status": "failed", "errors": 1},
	})
	store := newFakeStore()
	report, err := IndexRun(context.Background(), RunConfig{RunDir: root}, Dependencies{Provider: &fakeProvider{}, Store: store})
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "failed" || report.SkippedWorkspaces != 1 || store.pingCalls != 0 {
		t.Fatalf("report=%+v pingCalls=%d", report, store.pingCalls)
	}
}

func TestIndexRunProcessesWarningWorkspaceAndPrunesFailedDocumentPoints(t *testing.T) {
	root := filepath.Join(t.TempDir(), "chunks_1")
	writeRunFixtureVersion(t, root, []map[string]any{
		{"id": 25, "name": "Marketing", "path": "workspace_25", "qdrantCollection": "workspace_25", "status": "complete_with_warnings", "errors": 1},
	}, 2)
	store := newFakeStore()
	store.points["stale-failed-document"] = Point{ID: "stale-failed-document", Vector: unitVector(), Payload: map[string]any{}}

	report, err := IndexRun(context.Background(), RunConfig{RunDir: root, BatchSize: 2}, Dependencies{Provider: &fakeProvider{}, Store: store})
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "complete_with_warnings" || report.SuccessfulWorkspaces != 1 || report.WarningWorkspaces != 1 || report.FailedWorkspaces != 0 {
		t.Fatalf("report = %+v", report)
	}
	if report.Totals.EmbeddedChunks != 1 || report.Totals.DeletedChunks != 1 || len(store.points) != 1 {
		t.Fatalf("totals=%+v points=%+v", report.Totals, store.points)
	}
	workspace := report.Workspaces[0]
	if workspace.Status != "complete_with_warnings" || workspace.SourceStatus != "complete_with_warnings" || workspace.SourceDocumentErrors != 1 {
		t.Fatalf("workspace = %+v", workspace)
	}
	if workspace.ChunkReportFile == nil || *workspace.ChunkReportFile != "workspace_25/chunk_report.json" {
		t.Fatalf("chunk report = %v", workspace.ChunkReportFile)
	}
}

func TestIndexRunEmptyWorkspacePrunesEveryManagedPoint(t *testing.T) {
	root := filepath.Join(t.TempDir(), "chunks_1")
	writeRunFixture(t, root, []map[string]any{
		{"id": 1, "name": "Empty", "path": "workspace_1", "qdrantCollection": "workspace_1", "status": "complete", "errors": 0},
	})
	if err := os.WriteFile(filepath.Join(root, "workspace_1", "chunks.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	store := newFakeStore()
	store.points["stale"] = Point{ID: "stale", Vector: unitVector(), Payload: map[string]any{}}
	report, err := IndexRun(context.Background(), RunConfig{RunDir: root}, Dependencies{Provider: &fakeProvider{}, Store: store})
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "complete" || report.Totals.DeletedChunks != 1 || len(store.points) != 0 {
		t.Fatalf("report=%+v points=%+v", report, store.points)
	}
}

func writeRunFixture(t *testing.T, root string, workspaces []map[string]any) {
	t.Helper()
	writeRunFixtureVersion(t, root, workspaces, 1)
}

func writeRunFixtureVersion(t *testing.T, root string, workspaces []map[string]any, formatVersion int) {
	t.Helper()
	for _, raw := range workspaces {
		path := filepath.Join(root, raw["path"].(string))
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		writeJSONFile(t, filepath.Join(path, "chunk_report.json"), map[string]any{"errors": raw["errors"]})
		raw["chunksPath"] = filepath.ToSlash(filepath.Join(raw["path"].(string), "chunks.jsonl"))
		raw["reportPath"] = filepath.ToSlash(filepath.Join(raw["path"].(string), "chunk_report.json"))
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
	writeJSONFile(t, filepath.Join(root, "chunks_manifest.json"), map[string]any{
		"formatVersion": formatVersion, "sourceRun": "run_1", "sourceNormalization": "normalized_1", "workspaces": workspaces,
	})
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
