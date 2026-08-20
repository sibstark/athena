package snapshot

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sibstark/athena/fetcher/internal/bitrix"
)

func TestFetcherCreatesImmutableRunsAndCompleteManifest(t *testing.T) {
	t.Parallel()
	assetServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "image/png")
		_, _ = writer.Write([]byte{137, 80, 78, 71})
	}))
	defer assetServer.Close()
	parent := 4869
	workspaceID := 27
	client := &fakeClient{
		portal:     "https://hwschool.bitrix24.ru",
		workspaces: []bitrix.Workspace{{ID: workspaceID, Name: "Учебный отдел", Position: 1, PolicyLevel: "none", CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-08-12T13:58:46Z"}},
		trees: map[int]bitrix.DocumentTreeResult{workspaceID: {Items: []bitrix.TreeNode{{
			ID: parent, CollectionID: workspaceID, Title: "Кластер RU, D", Position: 1,
			Children: []bitrix.TreeNode{{ID: 2983, CollectionID: workspaceID, ParentID: &parent, Title: "Онбординг преподавателя", Position: 1}},
		}}}},
		documents: map[int]bitrix.Document{
			parent: {ID: parent, CollectionID: &workspaceID, Title: "Кластер RU, D", Position: 1, CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-08-01T00:00:00Z"},
			2983:   {ID: 2983, CollectionID: &workspaceID, ParentID: &parent, Title: "Онбординг преподавателя", Markdown: "# Intro\n\n![Picture](" + assetServer.URL + "/picture.png)\n\n![Same](" + assetServer.URL + "/picture.png)", Position: 1, CreatedAt: "2026-01-02T00:00:00Z", UpdatedAt: "2026-08-02T00:00:00Z"},
		},
	}
	clock := newTestClock(time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC))
	root := filepath.Join(t.TempDir(), "archive")
	exporter := NewExporter(client)
	options := Options{OutputDir: root, AssetHTTPClient: assetServer.Client(), Now: clock.Now}
	first, err := exporter.Fetch(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	firstManifestPath := filepath.Join(first.RunPath, "manifest.json")
	firstManifestBytes, err := os.ReadFile(firstManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	firstHash := sha256.Sum256(firstManifestBytes)

	second, err := exporter.Fetch(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if first.RunID == second.RunID || first.RunPath == second.RunPath {
		t.Fatalf("runs are not unique: %+v %+v", first, second)
	}
	if first.AssetsDownloaded != 1 || second.AssetsDownloaded != 1 {
		t.Fatalf("assets were not deduplicated: first=%+v second=%+v", first, second)
	}
	after, err := os.ReadFile(firstManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if sha256.Sum256(after) != firstHash {
		t.Fatal("first run was modified")
	}

	var manifest Manifest
	if err := json.Unmarshal(firstManifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Workspaces) != 1 {
		t.Fatalf("workspaces = %d", len(manifest.Workspaces))
	}
	workspace := manifest.Workspaces[0]
	if workspace.URL != "https://hwschool.bitrix24.ru/note/workspace/27/" || workspace.QdrantCollection != "workspace_27" || workspace.DocumentCount != 2 {
		t.Fatalf("workspace manifest = %+v", workspace)
	}
	if len(workspace.Documents) != 1 || len(workspace.Documents[0].Children) != 1 {
		t.Fatalf("unexpected document tree: %+v", workspace.Documents)
	}
	child := workspace.Documents[0].Children[0]
	if child.URL != "https://hwschool.bitrix24.ru/note/document/2983/" || child.ContentPath != "workspace_27/document_4869/document_2983/content.md" {
		t.Fatalf("child manifest = %+v", child)
	}
	content, err := os.ReadFile(filepath.Join(first.RunPath, filepath.FromSlash(child.ContentPath)))
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	if !strings.Contains(text, "document_url: \"https://hwschool.bitrix24.ru/note/document/2983/\"") || !strings.Contains(text, "../../assets/") {
		t.Fatalf("unexpected content:\n%s", text)
	}

	var registry Registry
	readJSON(t, filepath.Join(root, "runs.json"), &registry)
	if len(registry.Runs) != 2 || registry.Runs[0].ID != first.RunID || registry.Runs[1].ID != second.RunID {
		t.Fatalf("registry = %+v", registry)
	}
}

func TestFailedRunContainsOnlyRedactedReport(t *testing.T) {
	t.Parallel()
	client := &fakeClient{
		portal:     "https://hwschool.bitrix24.ru",
		workspaces: []bitrix.Workspace{{ID: 27, Name: "Учебный отдел"}},
		trees:      map[int]bitrix.DocumentTreeResult{27: {Truncated: true}},
	}
	root := filepath.Join(t.TempDir(), "archive")
	summary, err := NewExporter(client).Fetch(context.Background(), Options{OutputDir: root, SkipAssets: true, Now: newTestClock(time.Now()).Now})
	if err == nil {
		t.Fatal("expected failure")
	}
	entries, readErr := os.ReadDir(summary.RunPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 1 || entries[0].Name() != "report.json" {
		t.Fatalf("failed run contains partial data: %+v", entries)
	}
	var report Report
	readJSON(t, filepath.Join(summary.RunPath, "report.json"), &report)
	if report.Status != StatusFailed || report.Error == nil || !strings.Contains(report.Error.Message, "truncated") {
		t.Fatalf("report = %+v", report)
	}
	if _, statErr := os.Stat(filepath.Join(summary.RunPath, "manifest.json")); !os.IsNotExist(statErr) {
		t.Fatalf("manifest should not exist: %v", statErr)
	}
}

func TestSkipAssetsAndFilterValidation(t *testing.T) {
	t.Parallel()
	workspaceID := 27
	client := &fakeClient{
		portal:     "https://hwschool.bitrix24.ru",
		workspaces: []bitrix.Workspace{{ID: workspaceID, Name: "Учебный отдел"}},
		trees:      map[int]bitrix.DocumentTreeResult{workspaceID: {Items: []bitrix.TreeNode{{ID: 4869, CollectionID: workspaceID}}}},
		documents:  map[int]bitrix.Document{4869: {ID: 4869, CollectionID: &workspaceID, Markdown: "![x](https://cdn.example.com/a.png)"}},
	}
	root := filepath.Join(t.TempDir(), "archive")
	summary, err := NewExporter(client).Fetch(context.Background(), Options{OutputDir: root, SkipAssets: true, Include: map[int]struct{}{27: {}}, Now: newTestClock(time.Now()).Now})
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(summary.RunPath, "workspace_27", "document_4869", "content.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "https://cdn.example.com/a.png") {
		t.Fatal("remote URL was unexpectedly rewritten")
	}

	failureRoot := filepath.Join(t.TempDir(), "archive")
	failed, err := NewExporter(client).Fetch(context.Background(), Options{OutputDir: failureRoot, SkipAssets: true, Include: map[int]struct{}{27: {}}, Exclude: map[int]struct{}{27: {}}, Now: newTestClock(time.Now()).Now})
	if err == nil || failed.Status != StatusFailed {
		t.Fatalf("expected failed overlap run, summary=%+v err=%v", failed, err)
	}
}

func TestAssetFailurePublishesRunWithWarningsAndOriginalURL(t *testing.T) {
	t.Parallel()
	assetServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Error(writer, "unavailable", http.StatusServiceUnavailable)
	}))
	defer assetServer.Close()
	workspaceID := 27
	documentID := 4869
	assetURL := assetServer.URL + "/missing.png"
	client := &fakeClient{
		portal:     "https://hwschool.bitrix24.ru",
		workspaces: []bitrix.Workspace{{ID: workspaceID, Name: "Учебный отдел"}},
		trees:      map[int]bitrix.DocumentTreeResult{workspaceID: {Items: []bitrix.TreeNode{{ID: documentID, CollectionID: workspaceID}}}},
		documents:  map[int]bitrix.Document{documentID: {ID: documentID, CollectionID: &workspaceID, Markdown: "![missing](" + assetURL + ")"}},
	}
	summary, err := NewExporter(client).Fetch(context.Background(), Options{
		OutputDir: filepath.Join(t.TempDir(), "archive"), AssetHTTPClient: assetServer.Client(), Now: newTestClock(time.Now()).Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if summary.Status != StatusCompleteWithWarnings || summary.AssetsFailed != 1 {
		t.Fatalf("summary = %+v", summary)
	}
	content, err := os.ReadFile(filepath.Join(summary.RunPath, "workspace_27", "document_4869", "content.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), assetURL) {
		t.Fatal("failed asset URL was not preserved")
	}
}

func TestEmptyWorkspaceStillGetsDirectoryAndManifestEntry(t *testing.T) {
	t.Parallel()
	client := &fakeClient{
		portal:     "https://hwschool.bitrix24.ru",
		workspaces: []bitrix.Workspace{{ID: 35, Name: "Менеджмент"}},
		trees:      map[int]bitrix.DocumentTreeResult{35: {}},
	}
	summary, err := NewExporter(client).Fetch(context.Background(), Options{
		OutputDir: filepath.Join(t.TempDir(), "archive"), SkipAssets: true, Now: newTestClock(time.Now()).Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(summary.RunPath, "workspace_35"))
	if err != nil || !info.IsDir() {
		t.Fatalf("empty workspace directory missing: info=%v err=%v", info, err)
	}
	var manifest Manifest
	readJSON(t, filepath.Join(summary.RunPath, "manifest.json"), &manifest)
	if len(manifest.Workspaces) != 1 || manifest.Workspaces[0].DocumentCount != 0 || len(manifest.Workspaces[0].Documents) != 0 {
		t.Fatalf("manifest = %+v", manifest)
	}
}

func TestArchiveLockRejectsConcurrentWriter(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "archive")
	first, err := acquireArchiveLock(root)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := acquireArchiveLock(root)
	if err == nil {
		second.Close()
		t.Fatal("second writer acquired the archive lock")
	}
	if !strings.Contains(err.Error(), "already active") {
		t.Fatalf("unexpected lock error: %v", err)
	}
}

func TestPrivateAddressesAreRejected(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"127.0.0.1", "10.0.0.1", "192.168.1.1", "169.254.1.1", "100.64.0.1", "::1", "fc00::1"} {
		if isPublicIP(net.ParseIP(value)) {
			t.Errorf("%s was treated as public", value)
		}
	}
	if !isPublicIP(net.ParseIP("8.8.8.8")) {
		t.Fatal("public IP was rejected")
	}
}

type fakeClient struct {
	portal     string
	workspaces []bitrix.Workspace
	trees      map[int]bitrix.DocumentTreeResult
	documents  map[int]bitrix.Document
}

func (client *fakeClient) PortalOrigin() string { return client.portal }
func (client *fakeClient) Redact(value string) string {
	return strings.ReplaceAll(value, "secret", "[REDACTED]")
}
func (client *fakeClient) ListWorkspaces(context.Context) ([]bitrix.Workspace, error) {
	return append([]bitrix.Workspace(nil), client.workspaces...), nil
}
func (client *fakeClient) GetDocumentTree(_ context.Context, id int) (bitrix.DocumentTreeResult, error) {
	return client.trees[id], nil
}
func (client *fakeClient) GetDocument(_ context.Context, id int) (bitrix.Document, error) {
	return client.documents[id], nil
}

type testClock struct {
	mu   sync.Mutex
	time time.Time
}

func newTestClock(start time.Time) *testClock { return &testClock{time: start} }
func (clock *testClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	result := clock.time
	clock.time = clock.time.Add(time.Millisecond)
	return result
}

func readJSON(t *testing.T, path string, target any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatal(err)
	}
}
