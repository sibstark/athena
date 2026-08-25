package normalizer

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestNormalizeCurrentFetcherLayout(t *testing.T) {
	t.Parallel()
	run := filepath.Join(t.TempDir(), "run_100")
	workspace := testWorkspace(17, "Ментальная арифметика")
	root := testDocument(3039, 17, nil, "Библиотека ресурсов", "workspace_17/document_3039/content.md")
	parent := 3039
	link := testDocument(3051, 17, &parent, "Задания в картинках", "workspace_17/document_3039/document_3051/content.md")
	empty := testDocument(3053, 17, &parent, "Пустой раздел", "workspace_17/document_3039/document_3053/content.md")
	root.Children = []SourceDocument{link, empty}
	workspace.Documents = []SourceDocument{root}
	workspace.DocumentCount = 3
	writeTestRun(t, run, []SourceWorkspace{workspace}, map[int][]byte{
		3039: testMarkdown(root, workspace, "Собраны все необходимые материалы для уроков."),
		3051: testMarkdown(link, workspace, "[https://example.com](https://example.com)"),
		3053: testMarkdown(empty, workspace, ""),
	})

	output := filepath.Join(t.TempDir(), "normalized_100")
	result, err := Normalize(Options{RunDir: run, OutputDir: output, Now: fixedClock()})
	if err != nil {
		t.Fatal(err)
	}
	if result.WorkspaceCount != 1 || result.ProcessedDocuments != 2 {
		t.Fatalf("result = %+v", result)
	}
	documents := readJSONL(t, filepath.Join(output, "workspace_17", "normalized.jsonl"))
	if len(documents) != 2 {
		t.Fatalf("documents = %d", len(documents))
	}
	if documents[0].DocumentID != "page_3039" || documents[0].ParentDocumentID != nil {
		t.Fatalf("root = %+v", documents[0])
	}
	if documents[1].DocumentID != "page_3051" || documents[1].ParentDocumentID == nil || *documents[1].ParentDocumentID != "page_3039" {
		t.Fatalf("child = %+v", documents[1])
	}
	if documents[1].Metadata.DocumentType != DocumentExternalLink || documents[1].Metadata.Indexable {
		t.Fatalf("external link metadata = %+v", documents[1].Metadata)
	}
	if documents[1].Metadata.RootPageID == nil || *documents[1].Metadata.RootPageID != "3039" || !reflect.DeepEqual(documents[1].Metadata.AncestorIDs, []string{"3039"}) {
		t.Fatalf("tree metadata = %+v", documents[1].Metadata)
	}
	if documents[0].Metadata.WorkspaceID != 17 || documents[0].Metadata.DocumentURL != root.URL {
		t.Fatalf("Bitrix metadata = %+v", documents[0].Metadata)
	}
	rawJSONL, err := os.ReadFile(filepath.Join(output, "workspace_17", "normalized.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rawJSONL), `"assets":[]`) || !strings.Contains(string(rawJSONL), `"external_urls":[]`) {
		t.Fatalf("collection fields must be JSON arrays: %s", rawJSONL)
	}
	var tree DocumentTree
	readJSONFile(t, filepath.Join(output, "workspace_17", "document_tree.json"), &tree)
	if len(tree.Nodes) != 3 || tree.Nodes[2].NodeID != "page_3053" || !tree.Nodes[2].HasDocument {
		t.Fatalf("tree = %+v", tree)
	}
	var report WorkspaceReport
	readJSONFile(t, filepath.Join(output, "workspace_17", "normalization_report.json"), &report)
	if report.FoundFiles != 3 || report.ProcessedDocuments != 2 || report.EmptyFiles != 1 || report.Errors != 0 {
		t.Fatalf("report = %+v", report)
	}
}

func TestNormalizeRequiresMatchingRunAndNormalizedSuffixes(t *testing.T) {
	t.Parallel()
	run := filepath.Join(t.TempDir(), "run_42")
	if err := os.MkdirAll(run, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := validatePaths(run, filepath.Join(t.TempDir(), "normalized_41")); err == nil || !strings.Contains(err.Error(), "normalized_42") {
		t.Fatalf("expected suffix validation error, got %v", err)
	}
}

func TestMarkdownParityAndCodeProtection(t *testing.T) {
	t.Parallel()
	input := strings.Join([]string{
		"---", "title: Test", "---", "  First line   \r", "", "", "", "Second\t", "", "```ts", "const value = 1;   ", "", "", "```", "", "After   ",
	}, "\n")
	parsed, err := parseAndNormalizeMarkdown([]byte(input), 200, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	expected := "  First line\n\n\nSecond\n\n```ts\nconst value = 1;   \n\n\n```\n\nAfter"
	if parsed.Content != expected {
		t.Fatalf("content:\n%q\nexpected:\n%q", parsed.Content, expected)
	}
	if parsed.DocumentType != DocumentShortNote || !parsed.Indexable {
		t.Fatalf("classification = %s indexable=%v", parsed.DocumentType, parsed.Indexable)
	}
}

func TestSuspiciousTruncationHeuristics(t *testing.T) {
	t.Parallel()
	document351 := strings.Join([]string{
		"1. [Доступ к платформе](/note/document/351/#one)",
		"2. [Профиль преподавателя](/note/document/351/#two)",
		"", "## Доступ к платформе",
	}, "\n")
	parsed351, err := parseAndNormalizeMarkdown([]byte(document351), 200, 351, true)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(parsed351.SuspiciousReasons, "missing_toc_sections") || !contains(parsed351.SuspiciousReasons, "ends_with_heading") {
		t.Fatalf("351 reasons = %#v", parsed351.SuspiciousReasons)
	}
	if !reflect.DeepEqual(parsed351.MissingTOCSections, []string{"Профиль преподавателя"}) {
		t.Fatalf("missing sections = %#v", parsed351.MissingTOCSections)
	}
	parsed353, err := parseAndNormalizeMarkdown([]byte(strings.Repeat("Текст ", 20)+"\n- "), 200, 353, true)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(parsed353.SuspiciousReasons, "ends_with_empty_list_item") {
		t.Fatalf("353 reasons = %#v", parsed353.SuspiciousReasons)
	}
}

func TestControlCharactersAndMarkdownAnalysis(t *testing.T) {
	t.Parallel()
	markdown := "# Заголовок\n\nТекст\u001f с [ссылкой](https://example.com?a=1&amp;b=2) и ![alt](../assets/a.png)\n\n<img src=\"../assets/b.png\">"
	parsed, err := parseAndNormalizeMarkdown([]byte(markdown), 20, 42, false)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Analysis.HeadingCount != 1 || parsed.Analysis.WordCount == 0 {
		t.Fatalf("analysis = %+v", parsed.Analysis)
	}
	if !reflect.DeepEqual(parsed.Analysis.ExternalURLs, []string{"https://example.com?a=1&b=2"}) {
		t.Fatalf("URLs = %#v", parsed.Analysis.ExternalURLs)
	}
	if len(parsed.Analysis.Assets) != 2 || parsed.Analysis.Assets[0].Path != "../assets/a.png" || parsed.Analysis.Assets[1].Path != "../assets/b.png" {
		t.Fatalf("assets = %#v", parsed.Analysis.Assets)
	}
	if len(parsed.ControlReplacements) != 1 || parsed.ControlReplacements[0].Character != "U+001F" {
		t.Fatalf("replacements = %#v", parsed.ControlReplacements)
	}
}

func TestDuplicatesRemainAndAreAnnotated(t *testing.T) {
	t.Parallel()
	run := filepath.Join(t.TempDir(), "run_101")
	workspace := testWorkspace(1, "Welcome")
	first := testDocument(10, 1, nil, "First", "workspace_1/document_10/content.md")
	second := testDocument(11, 1, nil, "Second", "workspace_1/document_11/content.md")
	workspace.Documents = []SourceDocument{first, second}
	workspace.DocumentCount = 2
	writeTestRun(t, run, []SourceWorkspace{workspace}, map[int][]byte{
		10: testMarkdown(first, workspace, "# Same\n\nText"),
		11: testMarkdown(second, workspace, "# Same\r\n\r\nText   \r\n"),
	})
	output := filepath.Join(t.TempDir(), "normalized_101")
	if _, err := Normalize(Options{RunDir: run, OutputDir: output}); err != nil {
		t.Fatal(err)
	}
	documents := readJSONL(t, filepath.Join(output, "workspace_1", "normalized.jsonl"))
	if len(documents) != 2 || documents[1].Metadata.DuplicateOf != "page_10" || documents[0].ContentHash != documents[1].ContentHash {
		t.Fatalf("documents = %+v", documents)
	}
}

func TestPartialResultSkipsInvalidDocument(t *testing.T) {
	t.Parallel()
	run := filepath.Join(t.TempDir(), "run_102")
	workspace := testWorkspace(1, "Welcome")
	valid := testDocument(10, 1, nil, "Valid", "workspace_1/document_10/content.md")
	broken := testDocument(11, 1, nil, "Broken", "workspace_1/document_11/content.md")
	workspace.Documents = []SourceDocument{valid, broken}
	workspace.DocumentCount = 2
	writeTestRun(t, run, []SourceWorkspace{workspace}, map[int][]byte{
		10: testMarkdown(valid, workspace, "Useful content that is long enough to normalize without being empty."),
		11: {0xc3, 0x28},
	})
	output := filepath.Join(t.TempDir(), "normalized_102")
	result, err := Normalize(Options{RunDir: run, OutputDir: output})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusCompleteWithWarnings || result.Errors != 1 {
		t.Fatalf("result = %+v", result)
	}
	documents := readJSONL(t, filepath.Join(output, "workspace_1", "normalized.jsonl"))
	if len(documents) != 1 || documents[0].DocumentID != "page_10" {
		t.Fatalf("documents = %+v", documents)
	}
	var report WorkspaceReport
	readJSONFile(t, filepath.Join(output, "workspace_1", "normalization_report.json"), &report)
	if report.ProcessingErrors != 1 || report.SkippedDocuments != 1 || report.ErrorFiles[0].Code != "normalize_markdown" {
		t.Fatalf("report = %+v", report)
	}
}

func TestIntegrityWarningsForFrontMatterAssetsAndLinks(t *testing.T) {
	t.Parallel()
	run := filepath.Join(t.TempDir(), "run_103")
	workspace := testWorkspace(1, "Welcome")
	document := testDocument(10, 1, nil, "Expected", "workspace_1/document_10/content.md")
	workspace.Documents = []SourceDocument{document}
	workspace.DocumentCount = 1
	body := strings.Join([]string{
		"---", "document_id: 999", "workspace_id: 1", "parent_document_id: null", "title: Wrong", "position: 1",
		"created_by: 1", "updated_by: 2", `created_at: "2026-01-01T00:00:00Z"`, `updated_at: "2026-01-02T00:00:00Z"`,
		`workspace_url: "https://example.com/note/workspace/1/"`, `document_url: "https://example.com/note/document/10/"`, "---", "",
		"![Missing](../assets/missing.png)", "", "![Mail](mailto:test@example.com)", "", "![Mail again](mailto:test@example.com)", "", "[Unknown](https://example.com/note/document/999/)",
	}, "\n")
	writeTestRun(t, run, []SourceWorkspace{workspace}, map[int][]byte{10: []byte(body)})
	output := filepath.Join(t.TempDir(), "normalized_103")
	result, err := Normalize(Options{RunDir: run, OutputDir: output})
	if err != nil {
		t.Fatal(err)
	}
	if result.Warnings < 3 {
		t.Fatalf("result = %+v", result)
	}
	var report WorkspaceReport
	readJSONFile(t, filepath.Join(output, "workspace_1", "normalization_report.json"), &report)
	if report.FrontMatterMismatches != 2 || report.MissingAssets != 1 || report.BrokenInternalLinks != 1 {
		t.Fatalf("report = %+v", report)
	}
}

func TestFrontMatterComparisonIgnoresOuterTitleWhitespace(t *testing.T) {
	t.Parallel()
	workspace := testWorkspace(1, "Welcome")
	document := testDocument(10, 1, nil, "Title ", "workspace_1/document_10/content.md")
	issues := validateFrontMatter(map[string]any{
		"document_id": 10, "workspace_id": 1, "parent_document_id": nil, "title": "Title ", "position": 1,
		"created_by": 1, "updated_by": 2, "created_at": document.CreatedAt, "updated_at": document.UpdatedAt,
		"workspace_url": workspace.URL, "document_url": document.URL,
	}, workspace, document)
	if len(issues) != 0 {
		t.Fatalf("issues = %+v", issues)
	}
}

func TestWorkspaceFiltersForceAndDeterminism(t *testing.T) {
	t.Parallel()
	run := filepath.Join(t.TempDir(), "run_104")
	firstWorkspace := testWorkspace(1, "One")
	firstDocument := testDocument(10, 1, nil, "First", "workspace_1/document_10/content.md")
	firstWorkspace.Documents = []SourceDocument{firstDocument}
	firstWorkspace.DocumentCount = 1
	secondWorkspace := testWorkspace(2, "Two")
	secondDocument := testDocument(20, 2, nil, "Second", "workspace_2/document_20/content.md")
	secondWorkspace.Documents = []SourceDocument{secondDocument}
	secondWorkspace.DocumentCount = 1
	writeTestRun(t, run, []SourceWorkspace{firstWorkspace, secondWorkspace}, map[int][]byte{
		10: testMarkdown(firstDocument, firstWorkspace, strings.Repeat("First ", 40)),
		20: testMarkdown(secondDocument, secondWorkspace, strings.Repeat("Second ", 40)),
	})
	output := filepath.Join(t.TempDir(), "normalized_104")
	clock := fixedClock()
	options := Options{RunDir: run, OutputDir: output, Include: map[int]struct{}{2: {}}, Now: clock}
	if _, err := Normalize(options); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(filepath.Join(output, "workspace_2", "normalized.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(output, "workspace_1")); !os.IsNotExist(err) {
		t.Fatalf("excluded workspace exists: %v", err)
	}
	if _, err := Normalize(options); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("expected output collision, got %v", err)
	}
	options.Force = true
	if _, err := Normalize(options); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(filepath.Join(output, "workspace_2", "normalized.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("JSONL changed between deterministic runs")
	}
}

func TestInvalidWorkspaceDoesNotBlockOtherWorkspaces(t *testing.T) {
	t.Parallel()
	run := filepath.Join(t.TempDir(), "run_105")
	goodWorkspace := testWorkspace(1, "Good")
	goodDocument := testDocument(10, 1, nil, "Good", "workspace_1/document_10/content.md")
	goodWorkspace.Documents = []SourceDocument{goodDocument}
	goodWorkspace.DocumentCount = 1
	badWorkspace := testWorkspace(2, "Bad")
	badDocument := testDocument(20, 999, nil, "Bad", "workspace_2/document_20/content.md")
	badWorkspace.Documents = []SourceDocument{badDocument}
	badWorkspace.DocumentCount = 1
	writeTestRun(t, run, []SourceWorkspace{goodWorkspace, badWorkspace}, map[int][]byte{
		10: testMarkdown(goodDocument, goodWorkspace, strings.Repeat("Good content ", 30)),
		20: testMarkdown(badDocument, badWorkspace, strings.Repeat("Bad content ", 30)),
	})
	output := filepath.Join(t.TempDir(), "normalized_105")
	result, err := Normalize(Options{RunDir: run, OutputDir: output})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusCompleteWithWarnings || result.Errors == 0 || result.ProcessedDocuments != 1 {
		t.Fatalf("result = %+v", result)
	}
	if _, err := os.Stat(filepath.Join(output, "workspace_1", "normalized.jsonl")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(output, "workspace_2", "normalized.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("invalid workspace unexpectedly has JSONL: %v", err)
	}
	var report WorkspaceReport
	readJSONFile(t, filepath.Join(output, "workspace_2", "normalization_report.json"), &report)
	if report.Status != StatusFailed || report.TreeErrors == 0 {
		t.Fatalf("bad workspace report = %+v", report)
	}
}

func testWorkspace(id int, name string) SourceWorkspace {
	return SourceWorkspace{
		ID: id, Name: name, Position: id, Path: "workspace_" + strconv.Itoa(id),
		URL:              "https://example.com/note/workspace/" + strconv.Itoa(id) + "/",
		QdrantCollection: "workspace_" + strconv.Itoa(id), Documents: []SourceDocument{},
	}
}

func testDocument(id, workspaceID int, parent *int, title, contentPath string) SourceDocument {
	return SourceDocument{
		ID: id, WorkspaceID: workspaceID, ParentID: parent, Title: title, Position: 1,
		CreatedBy: 1, UpdatedBy: 2, CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-02T00:00:00Z",
		Path: filepath.ToSlash(filepath.Dir(contentPath)), ContentPath: contentPath,
		URL: "https://example.com/note/document/" + strconv.Itoa(id) + "/", Children: []SourceDocument{},
	}
}

func testMarkdown(document SourceDocument, workspace SourceWorkspace, markdown string) []byte {
	parent := "null"
	if document.ParentID != nil {
		parent = strconv.Itoa(*document.ParentID)
	}
	frontMatter := strings.Join([]string{
		"---", "document_id: " + strconv.Itoa(document.ID), "workspace_id: " + strconv.Itoa(workspace.ID),
		"parent_document_id: " + parent, "title: " + strconv.Quote(document.Title), "position: " + strconv.Itoa(document.Position),
		"created_by: " + strconv.Itoa(document.CreatedBy), "updated_by: " + strconv.Itoa(document.UpdatedBy),
		"created_at: " + strconv.Quote(document.CreatedAt), "updated_at: " + strconv.Quote(document.UpdatedAt),
		"workspace_url: " + strconv.Quote(workspace.URL), "document_url: " + strconv.Quote(document.URL), "---", "",
	}, "\n")
	return []byte(frontMatter + markdown + "\n")
}

func writeTestRun(t *testing.T, run string, workspaces []SourceWorkspace, contents map[int][]byte) {
	t.Helper()
	if err := os.MkdirAll(run, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := SourceManifest{
		FormatVersion: SourceFormatVersion, ExportedAt: "2026-01-03T00:00:00Z", AssetMode: "download",
		Source: Source{Provider: "bitrix24-note", Portal: "https://example.com", URL: "https://example.com/note/"}, Workspaces: workspaces,
	}
	writeTestJSON(t, filepath.Join(run, "manifest.json"), manifest)
	writeTestJSON(t, filepath.Join(run, "report.json"), map[string]any{"runId": filepath.Base(run), "status": "complete"})
	var writeDocuments func([]SourceDocument)
	writeDocuments = func(documents []SourceDocument) {
		for _, document := range documents {
			path := filepath.Join(run, filepath.FromSlash(document.ContentPath))
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, contents[document.ID], 0o644); err != nil {
				t.Fatal(err)
			}
			writeDocuments(document.Children)
		}
	}
	for _, workspace := range workspaces {
		writeDocuments(workspace.Documents)
	}
}

func readJSONL(t *testing.T, path string) []NormalizedDocument {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	result := []NormalizedDocument{}
	scanner := bufio.NewScanner(file)
	buffer := make([]byte, 64*1024)
	scanner.Buffer(buffer, 16*1024*1024)
	for scanner.Scan() {
		var document NormalizedDocument
		if err := json.Unmarshal(scanner.Bytes(), &document); err != nil {
			t.Fatal(err)
		}
		result = append(result, document)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return result
}

func readJSONFile(t *testing.T, path string, target any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatal(err)
	}
}

func writeTestJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func fixedClock() func() time.Time {
	return func() time.Time { return time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC) }
}

func contains(items []string, expected string) bool {
	for _, item := range items {
		if item == expected {
			return true
		}
	}
	return false
}
