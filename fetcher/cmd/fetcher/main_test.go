package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIDSetAcceptsRepeatedCommaSeparatedValues(t *testing.T) {
	var values idSet
	if err := values.Set("27, 17"); err != nil {
		t.Fatal(err)
	}
	if err := values.Set("27"); err != nil {
		t.Fatal(err)
	}
	if len(values.values) != 2 {
		t.Fatalf("values = %#v", values.values)
	}
	if _, ok := values.values[27]; !ok {
		t.Fatal("workspace 27 missing")
	}
	if _, ok := values.values[17]; !ok {
		t.Fatal("workspace 17 missing")
	}
}

func TestIDSetRejectsInvalidValues(t *testing.T) {
	for _, value := range []string{"", "27,", "zero", "0", "-1"} {
		var values idSet
		if err := values.Set(value); err == nil {
			t.Errorf("%q was accepted", value)
		}
	}
}

func TestRunNormalizeCreatesWorkspaceOutputsWithoutBitrixConfiguration(t *testing.T) {
	t.Parallel()
	runDirectory := filepath.Join(t.TempDir(), "run_1")
	if err := os.MkdirAll(runDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := map[string]any{
		"formatVersion": 1,
		"source": map[string]any{
			"provider": "bitrix24-note", "portal": "https://example.com", "url": "https://example.com/note/",
		},
		"assetMode": "skip",
		"workspaces": []any{map[string]any{
			"id": 17, "name": "Empty", "path": "workspace_17", "url": "https://example.com/note/workspace/17/",
			"qdrantCollection": "workspace_17", "documentCount": 0, "documents": []any{},
		}},
	}
	writeMainTestJSON(t, filepath.Join(runDirectory, "manifest.json"), manifest)
	writeMainTestJSON(t, filepath.Join(runDirectory, "report.json"), map[string]any{"runId": "run_1", "status": "complete"})
	output := filepath.Join(t.TempDir(), "normalized_1")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := runNormalize([]string{"--run", runDirectory, "--out", output, "--include", "17"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "Status: complete") || !strings.Contains(stdout.String(), "Workspaces: 1") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	for _, relative := range []string{
		"normalization_manifest.json", "normalization_report.json", "workspace_17/normalized.jsonl",
		"workspace_17/document_tree.json", "workspace_17/normalization_report.json",
	} {
		if _, err := os.Stat(filepath.Join(output, filepath.FromSlash(relative))); err != nil {
			t.Fatalf("%s missing: %v", relative, err)
		}
	}
}

func TestRunNormalizeRejectsNonNumericThreshold(t *testing.T) {
	t.Parallel()
	err := runNormalize([]string{"--run", "unused", "--out", "unused-output", "--short-note-threshold", "zero"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "invalid value") {
		t.Fatalf("expected flag parsing error, got %v", err)
	}
}

func writeMainTestJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
