package normalizer

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const defaultShortNoteThreshold = 200

type documentContext struct {
	Source       SourceDocument
	NodeID       string
	ParentNodeID *string
	PageID       string
	ParentPageID *string
	RootPageID   string
	Depth        int
	AncestorIDs  []string
	Breadcrumbs  []string
	Leaf         bool
}

type workspacePlan struct {
	Workspace SourceWorkspace
	Documents []documentContext
	Tree      DocumentTree
}

var bitrixDocumentPathPattern = regexp.MustCompile(`^/note/document/(\d+)/`)

func Normalize(options Options) (Result, error) {
	started := now(options).UTC()
	runDir, outputDir, err := validatePaths(options.RunDir, options.OutputDir)
	if err != nil {
		return Result{}, err
	}
	threshold := options.ShortNoteThreshold
	if threshold == 0 {
		threshold = defaultShortNoteThreshold
	}
	if threshold < 1 {
		return Result{}, errors.New("short note threshold must be a positive integer")
	}
	if err := validateFilters(options.Include, options.Exclude); err != nil {
		return Result{}, err
	}
	if _, err := os.Stat(outputDir); err == nil && !options.Force {
		return Result{}, fmt.Errorf("output already exists: %s; use --force to replace it", outputDir)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Result{}, fmt.Errorf("inspect output directory: %w", err)
	}

	manifest, runID, err := readSourceManifest(runDir)
	if err != nil {
		return Result{}, err
	}
	selected, err := selectWorkspaces(manifest.Workspaces, options.Include, options.Exclude)
	if err != nil {
		return Result{}, err
	}
	log := options.Progress
	if log == nil {
		log = func(string) {}
	}

	allDocumentIDs := collectAllDocumentIDs(manifest.Workspaces)
	parent := filepath.Dir(outputDir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return Result{}, fmt.Errorf("create output parent: %w", err)
	}
	staging, err := os.MkdirTemp(parent, "."+filepath.Base(outputDir)+".tmp-")
	if err != nil {
		return Result{}, fmt.Errorf("create normalization staging directory: %w", err)
	}
	defer os.RemoveAll(staging)

	outputs := make([]WorkspaceOutput, 0, len(selected))
	processedWorkspaces := 0
	totalDocuments := 0
	totalProcessed := 0
	totalSuspicious := 0
	totalErrors := 0
	totalWarnings := 0
	for index, workspace := range selected {
		log(fmt.Sprintf("[%d/%d] Normalizing workspace %s (%d)...", index+1, len(selected), workspace.Name, workspace.ID))
		workspaceOutput, report, err := normalizeWorkspace(runDir, staging, manifest, workspace, threshold, allDocumentIDs, log)
		if err != nil {
			return Result{}, err
		}
		outputs = append(outputs, workspaceOutput)
		totalDocuments += report.FoundFiles
		totalProcessed += report.ProcessedDocuments
		totalSuspicious += report.SuspiciousDocuments
		totalErrors += report.Errors
		warnings := workspaceWarnings(report)
		totalWarnings += warnings
		if report.Status != StatusFailed {
			processedWorkspaces++
		}
	}

	status := StatusComplete
	if totalErrors > 0 || totalWarnings > 0 {
		status = StatusCompleteWithWarnings
	}
	if len(selected) > 0 && processedWorkspaces == 0 {
		status = StatusFailed
	}
	finished := now(options).UTC()
	normalizationManifest := Manifest{
		FormatVersion: FormatVersion, GeneratedAt: finished.Format(time.RFC3339Nano),
		SourceRun: runID, Source: manifest.Source, Workspaces: outputs,
	}
	report := Report{
		FormatVersion: FormatVersion, Status: status, SourceRun: runID,
		StartedAt: started.Format(time.RFC3339Nano), FinishedAt: finished.Format(time.RFC3339Nano),
		DurationMS: finished.Sub(started).Milliseconds(), WorkspaceCount: len(selected),
		ProcessedWorkspaces: processedWorkspaces, DocumentCount: totalDocuments,
		ProcessedDocuments: totalProcessed, SuspiciousDocuments: totalSuspicious,
		Errors: totalErrors, Warnings: totalWarnings, Workspaces: outputs,
	}
	if err := writeJSON(filepath.Join(staging, "normalization_manifest.json"), normalizationManifest); err != nil {
		return Result{}, fmt.Errorf("write normalization manifest: %w", err)
	}
	if err := writeJSON(filepath.Join(staging, "normalization_report.json"), report); err != nil {
		return Result{}, fmt.Errorf("write normalization report: %w", err)
	}
	if err := publishDirectory(staging, outputDir, options.Force); err != nil {
		return Result{}, err
	}
	staging = ""
	result := Result{
		OutputDir: outputDir, Status: status, WorkspaceCount: len(selected),
		ProcessedDocuments: totalProcessed, SuspiciousDocuments: totalSuspicious,
		Errors: totalErrors, Warnings: totalWarnings,
	}
	if status == StatusFailed {
		return result, fmt.Errorf("normalization failed: no selected workspace was processed successfully; see %s", filepath.Join(outputDir, "normalization_report.json"))
	}
	return result, nil
}

func normalizeWorkspace(runDir, staging string, manifest SourceManifest, workspace SourceWorkspace, threshold int, allDocumentIDs map[int]struct{}, log func(string)) (WorkspaceOutput, WorkspaceReport, error) {
	report := newWorkspaceReport(workspace)
	report.FoundFiles = workspace.DocumentCount
	workspaceDir := filepath.Join(staging, fmt.Sprintf("workspace_%d", workspace.ID))
	reportPath := filepath.Join(workspaceDir, "normalization_report.json")
	output := WorkspaceOutput{
		ID: workspace.ID, Name: workspace.Name, Path: fmt.Sprintf("workspace_%d", workspace.ID),
		QdrantCollection: workspace.QdrantCollection,
		ReportPath:       filepath.ToSlash(filepath.Join(fmt.Sprintf("workspace_%d", workspace.ID), "normalization_report.json")),
	}
	plan, issues := planWorkspace(runDir, workspace)
	if len(issues) > 0 {
		report.Status = StatusFailed
		report.TreeErrorFiles = append(report.TreeErrorFiles, issues...)
		report.TreeErrors = len(report.TreeErrorFiles)
		report.Errors = report.TreeErrors
		output.Status = report.Status
		output.Errors = report.Errors
		if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
			return output, report, fmt.Errorf("create workspace %d output: %w", workspace.ID, err)
		}
		if err := writeJSON(reportPath, report); err != nil {
			return output, report, fmt.Errorf("write workspace %d failed report: %w", workspace.ID, err)
		}
		return output, report, nil
	}
	report.TreeNodes = len(plan.Tree.Nodes)
	report.MaxDepth = maxDepth(plan.Documents)
	documents := make([]NormalizedDocument, 0, len(plan.Documents))
	for index, context := range plan.Documents {
		source := context.Source.ContentPath
		absolute, err := safeJoin(runDir, source)
		if err != nil {
			recordProcessingError(&report, context, "unsafe_content_path", err)
			continue
		}
		bytes, err := os.ReadFile(absolute)
		if err != nil {
			recordProcessingError(&report, context, "read_content", err)
			continue
		}
		parsed, err := parseAndNormalizeMarkdown(bytes, threshold, context.Source.ID, context.Leaf)
		if err != nil {
			recordProcessingError(&report, context, "normalize_markdown", err)
			continue
		}
		frontMatterIssues := validateFrontMatter(parsed.Attributes, workspace, context.Source)
		for _, issue := range frontMatterIssues {
			report.FrontMatterMismatches++
			report.IntegrityWarningFiles = append(report.IntegrityWarningFiles, issue)
		}
		checkAssetsAndLinks(runDir, manifest.AssetMode, context, parsed, allDocumentIDs, &report)
		for _, replacement := range parsed.ControlReplacements {
			report.ControlCharacterReplacements += replacement.Count
			report.ControlCharacterWarnings = append(report.ControlCharacterWarnings, ControlCharacterWarning{
				Source: source, DocumentID: context.NodeID, Character: replacement.Character, Count: replacement.Count,
			})
		}
		report.Images += len(parsed.Analysis.Assets)
		report.ExternalLinks += len(parsed.Analysis.ExternalURLs)
		if strings.TrimSpace(context.Source.Title) == "" {
			report.FilesWithoutTitle++
		}
		if len(parsed.SuspiciousReasons) > 0 {
			report.SuspiciousDocuments++
			report.SuspiciousFiles = append(report.SuspiciousFiles, Issue{
				Source: source, DocumentID: context.NodeID, Code: strings.Join(parsed.SuspiciousReasons, ","),
				Message: "content is suspicious: " + strings.Join(parsed.SuspiciousReasons, ", "),
			})
		}
		if parsed.DocumentType == DocumentEmpty {
			report.EmptyFiles++
			log(fmt.Sprintf("  [%d/%d] empty: %s", index+1, len(plan.Documents), source))
			continue
		}
		document := createNormalizedDocument(workspace, context, parsed)
		documents = append(documents, document)
		switch parsed.DocumentType {
		case DocumentShortNote:
			report.ShortDocuments++
		case DocumentExternalLink:
			report.ExternalLinkDocuments++
		}
		log(fmt.Sprintf("  [%d/%d] normalized: %s", index+1, len(plan.Documents), source))
	}
	annotateDuplicates(documents, &report)
	report.ProcessedDocuments = len(documents)
	report.SkippedDocuments = report.FoundFiles - report.ProcessedDocuments - report.EmptyFiles
	for _, document := range documents {
		if document.Metadata.Depth == 0 {
			report.RootDocuments++
		}
	}
	finalizeWorkspaceReport(&report)
	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		return output, report, fmt.Errorf("create workspace %d output: %w", workspace.ID, err)
	}
	normalizedPath := filepath.Join(workspaceDir, "normalized.jsonl")
	treePath := filepath.Join(workspaceDir, "document_tree.json")
	if err := writeJSONL(normalizedPath, documents); err != nil {
		return output, report, fmt.Errorf("write workspace %d normalized JSONL: %w", workspace.ID, err)
	}
	if err := writeJSON(treePath, plan.Tree); err != nil {
		return output, report, fmt.Errorf("write workspace %d document tree: %w", workspace.ID, err)
	}
	finalizeWorkspaceReport(&report)
	if err := writeJSON(reportPath, report); err != nil {
		return output, report, fmt.Errorf("write workspace %d report: %w", workspace.ID, err)
	}
	output.NormalizedPath = filepath.ToSlash(filepath.Join(output.Path, "normalized.jsonl"))
	output.TreePath = filepath.ToSlash(filepath.Join(output.Path, "document_tree.json"))
	output.Status = report.Status
	output.ProcessedDocuments = report.ProcessedDocuments
	output.SuspiciousDocuments = report.SuspiciousDocuments
	output.Errors = report.Errors
	output.Warnings = workspaceWarnings(report)
	return output, report, nil
}

func planWorkspace(runDir string, workspace SourceWorkspace) (workspacePlan, []Issue) {
	issues := make([]Issue, 0)
	if workspace.ID <= 0 {
		issues = append(issues, Issue{Source: workspace.Path, Code: "invalid_workspace_id", Message: "workspace ID must be positive"})
	}
	if _, err := safeJoin(runDir, workspace.Path); err != nil {
		issues = append(issues, Issue{Source: workspace.Path, Code: "unsafe_workspace_path", Message: err.Error()})
	}
	seenIDs := map[int]string{}
	seenPaths := map[string]int{}
	documents := make([]documentContext, 0, workspace.DocumentCount)
	tree := DocumentTree{Nodes: []DocumentTreeNode{}}
	var walk func([]SourceDocument, *int, int, []string, []string, string)
	walk = func(items []SourceDocument, expectedParent *int, depth int, ancestors, breadcrumbs []string, rootPageID string) {
		for _, document := range items {
			source := document.ContentPath
			if document.ID <= 0 {
				issues = append(issues, Issue{Source: source, Code: "invalid_document_id", Message: "document ID must be positive"})
				continue
			}
			if previous, exists := seenIDs[document.ID]; exists {
				issues = append(issues, Issue{Source: source, DocumentID: pageNodeID(document.ID), Code: "duplicate_document_id", Message: fmt.Sprintf("document ID also belongs to %s", previous)})
				continue
			}
			seenIDs[document.ID] = source
			if previous, exists := seenPaths[source]; exists {
				issues = append(issues, Issue{Source: source, DocumentID: pageNodeID(document.ID), Code: "duplicate_content_path", Message: fmt.Sprintf("content path also belongs to document %d", previous)})
			}
			seenPaths[source] = document.ID
			if document.WorkspaceID != workspace.ID {
				issues = append(issues, Issue{Source: source, DocumentID: pageNodeID(document.ID), Code: "workspace_mismatch", Message: fmt.Sprintf("workspace is %d, expected %d", document.WorkspaceID, workspace.ID)})
			}
			if !sameOptionalInt(document.ParentID, expectedParent) {
				issues = append(issues, Issue{Source: source, DocumentID: pageNodeID(document.ID), Code: "parent_mismatch", Message: fmt.Sprintf("parent is %s, expected %s", optionalInt(document.ParentID), optionalInt(expectedParent))})
			}
			if _, err := safeJoin(runDir, document.Path); err != nil {
				issues = append(issues, Issue{Source: source, DocumentID: pageNodeID(document.ID), Code: "unsafe_document_path", Message: err.Error()})
			}
			if _, err := safeJoin(runDir, source); err != nil {
				issues = append(issues, Issue{Source: source, DocumentID: pageNodeID(document.ID), Code: "unsafe_content_path", Message: err.Error()})
			}
			pageID := strconv.Itoa(document.ID)
			currentRoot := rootPageID
			if currentRoot == "" {
				currentRoot = pageID
			}
			title := strings.TrimSpace(document.Title)
			if title == "" {
				title = fmt.Sprintf("Document %d", document.ID)
			}
			currentBreadcrumbs := appendCopy(breadcrumbs, title)
			context := documentContext{
				Source: document, NodeID: pageNodeID(document.ID), PageID: pageID,
				RootPageID: currentRoot, Depth: depth, AncestorIDs: appendCopy(ancestors),
				Breadcrumbs: currentBreadcrumbs, Leaf: len(document.Children) == 0,
			}
			if expectedParent != nil {
				parentNode := pageNodeID(*expectedParent)
				context.ParentNodeID = &parentNode
				parentPage := strconv.Itoa(*expectedParent)
				context.ParentPageID = &parentPage
			}
			documents = append(documents, context)
			sourceCopy := source
			pageCopy := pageID
			tree.Nodes = append(tree.Nodes, DocumentTreeNode{
				NodeID: context.NodeID, ParentNodeID: context.ParentNodeID, PageID: &pageCopy,
				Title: title, Depth: depth, HasDocument: true, Source: &sourceCopy,
			})
			parent := document.ID
			walk(document.Children, &parent, depth+1, appendCopy(ancestors, pageID), currentBreadcrumbs, currentRoot)
		}
	}
	walk(workspace.Documents, nil, 0, []string{}, []string{}, "")
	if len(documents) != workspace.DocumentCount {
		issues = append(issues, Issue{Source: workspace.Path, Code: "document_count_mismatch", Message: fmt.Sprintf("manifest declares %d documents, tree contains %d", workspace.DocumentCount, len(documents))})
	}
	return workspacePlan{Workspace: workspace, Documents: documents, Tree: tree}, issues
}

func createNormalizedDocument(workspace SourceWorkspace, context documentContext, parsed parsedDocument) NormalizedDocument {
	title := strings.TrimSpace(context.Source.Title)
	var titlePointer *string
	if title != "" {
		titlePointer = &title
	}
	pageID := context.PageID
	rootPageID := context.RootPageID
	parentDocumentID := context.ParentNodeID
	return NormalizedDocument{
		DocumentID: context.NodeID, ParentDocumentID: parentDocumentID, Content: parsed.Content,
		Metadata: NormalizedMetadata{
			Title: titlePointer, Source: context.Source.ContentPath, PageID: &pageID,
			ParentPageID: context.ParentPageID, RootPageID: &rootPageID, Depth: context.Depth,
			AncestorIDs: appendCopy(context.AncestorIDs), Breadcrumbs: appendCopy(context.Breadcrumbs),
			ContextText: strings.Join(context.Breadcrumbs, " → "), DocumentType: parsed.DocumentType,
			Indexable: parsed.Indexable, ExternalURLs: appendCopy(parsed.Analysis.ExternalURLs), Assets: append([]NormalizedAsset{}, parsed.Analysis.Assets...),
			CharacterCount: utf8.RuneCountInString(parsed.Content), WordCount: parsed.Analysis.WordCount,
			HeadingCount: parsed.Analysis.HeadingCount, ExternalURLCount: len(parsed.Analysis.ExternalURLs), AssetCount: len(parsed.Analysis.Assets),
			Version: parsed.Version, Date: parsed.Date, Category: parsed.Category, Status: parsed.Status, Language: parsed.Language,
			WorkspaceID: workspace.ID, Position: context.Source.Position, CreatedBy: context.Source.CreatedBy, UpdatedBy: context.Source.UpdatedBy,
			CreatedAt: context.Source.CreatedAt, UpdatedAt: context.Source.UpdatedAt, WorkspaceURL: workspace.URL, DocumentURL: context.Source.URL,
			Suspicious: len(parsed.SuspiciousReasons) > 0, SuspiciousReasons: appendCopy(parsed.SuspiciousReasons), MissingTOCSections: appendCopy(parsed.MissingTOCSections),
		}, ContentHash: parsed.ContentHash,
	}
}

func validateFrontMatter(attributes map[string]any, workspace SourceWorkspace, document SourceDocument) []Issue {
	expected := map[string]string{
		"document_id": strconv.Itoa(document.ID), "workspace_id": strconv.Itoa(workspace.ID),
		"title": strings.TrimSpace(document.Title), "position": strconv.Itoa(document.Position),
		"created_by": strconv.Itoa(document.CreatedBy), "updated_by": strconv.Itoa(document.UpdatedBy),
		"created_at": document.CreatedAt, "updated_at": document.UpdatedAt,
		"workspace_url": workspace.URL, "document_url": document.URL,
	}
	if document.ParentID == nil {
		expected["parent_document_id"] = "null"
	} else {
		expected["parent_document_id"] = strconv.Itoa(*document.ParentID)
	}
	issues := make([]Issue, 0)
	for field, expectedValue := range expected {
		actualValue, present := frontMatterValue(attributes, field)
		if !present || actualValue != expectedValue {
			actual := actualValue
			if !present {
				actual = "<missing>"
			}
			issues = append(issues, Issue{
				Source: document.ContentPath, DocumentID: pageNodeID(document.ID), Code: "front_matter_mismatch",
				Message: fmt.Sprintf("front matter %s is %q, expected %q", field, actual, expectedValue),
			})
		}
	}
	sort.Slice(issues, func(i, j int) bool { return issues[i].Message < issues[j].Message })
	return issues
}

func frontMatterValue(attributes map[string]any, field string) (string, bool) {
	value, exists := attributes[field]
	if !exists {
		return "", false
	}
	if value == nil {
		return "null", true
	}
	parsed, err := optionalScalar(attributes, field)
	if err != nil || parsed == nil {
		return "", false
	}
	return *parsed, true
}

func checkAssetsAndLinks(runDir, assetMode string, context documentContext, parsed parsedDocument, allDocumentIDs map[int]struct{}, report *WorkspaceReport) {
	contentDirectory := filepath.Dir(context.Source.ContentPath)
	seenAssets := map[string]struct{}{}
	for _, asset := range parsed.Analysis.Assets {
		if _, duplicate := seenAssets[asset.Path]; duplicate {
			continue
		}
		seenAssets[asset.Path] = struct{}{}
		if assetMode == "skip" || isRemoteReference(asset.Path) {
			continue
		}
		candidate := filepath.ToSlash(filepath.Join(contentDirectory, filepath.FromSlash(asset.Path)))
		absolute, err := safeJoin(runDir, candidate)
		if err != nil {
			recordIntegrityWarning(report, context, "unsafe_asset_path", err.Error())
			continue
		}
		info, err := os.Stat(absolute)
		if err != nil || !info.Mode().IsRegular() {
			report.MissingAssets++
			recordIntegrityWarning(report, context, "missing_asset", fmt.Sprintf("local asset does not exist: %s", asset.Path))
		}
	}
	for _, link := range parsed.Analysis.Links {
		destination := strings.TrimSpace(link.Destination)
		if destination == "" || strings.HasPrefix(destination, "#") {
			continue
		}
		parsedURL, err := url.Parse(destination)
		if err != nil {
			continue
		}
		if match := bitrixDocumentPathPattern.FindStringSubmatch(parsedURL.Path); len(match) == 2 {
			id, _ := strconv.Atoi(match[1])
			if _, exists := allDocumentIDs[id]; !exists {
				report.BrokenInternalLinks++
				recordIntegrityWarning(report, context, "broken_document_link", fmt.Sprintf("Bitrix document link targets unknown document %d", id))
			}
			continue
		}
		if parsedURL.Scheme != "" || parsedURL.Host != "" || (!strings.HasSuffix(strings.ToLower(parsedURL.Path), ".md") && filepath.Base(parsedURL.Path) != "content.md") {
			continue
		}
		candidate := filepath.ToSlash(filepath.Join(contentDirectory, filepath.FromSlash(parsedURL.Path)))
		absolute, joinErr := safeJoin(runDir, candidate)
		if joinErr != nil {
			report.BrokenInternalLinks++
			recordIntegrityWarning(report, context, "unsafe_internal_link", joinErr.Error())
			continue
		}
		if info, statErr := os.Stat(absolute); statErr != nil || !info.Mode().IsRegular() {
			report.BrokenInternalLinks++
			recordIntegrityWarning(report, context, "broken_internal_link", fmt.Sprintf("local Markdown link does not exist: %s", destination))
		}
	}
}

func annotateDuplicates(documents []NormalizedDocument, report *WorkspaceReport) {
	firstByHash := map[string]string{}
	for index := range documents {
		first, exists := firstByHash[documents[index].ContentHash]
		if exists {
			documents[index].Metadata.DuplicateOf = first
			report.DuplicateFiles++
			report.ExactDuplicates++
			continue
		}
		firstByHash[documents[index].ContentHash] = documents[index].DocumentID
	}
}

func newWorkspaceReport(workspace SourceWorkspace) WorkspaceReport {
	return WorkspaceReport{
		FormatVersion: FormatVersion, Status: StatusComplete, WorkspaceID: workspace.ID, WorkspaceName: workspace.Name,
		ControlCharacterWarnings: []ControlCharacterWarning{}, SuspiciousFiles: []Issue{},
		IntegrityWarningFiles: []Issue{}, TreeErrorFiles: []Issue{}, ErrorFiles: []Issue{},
	}
}

func finalizeWorkspaceReport(report *WorkspaceReport) {
	report.TreeErrors = len(report.TreeErrorFiles)
	report.ProcessingErrors = len(report.ErrorFiles)
	report.IntegrityWarnings = len(report.IntegrityWarningFiles)
	report.Errors = report.TreeErrors + report.ProcessingErrors
	if report.Errors > 0 || workspaceWarnings(*report) > 0 {
		report.Status = StatusCompleteWithWarnings
	} else {
		report.Status = StatusComplete
	}
}

func workspaceWarnings(report WorkspaceReport) int {
	return report.IntegrityWarnings + report.SuspiciousDocuments + len(report.ControlCharacterWarnings)
}

func recordProcessingError(report *WorkspaceReport, context documentContext, code string, err error) {
	report.ErrorFiles = append(report.ErrorFiles, Issue{Source: context.Source.ContentPath, DocumentID: context.NodeID, Code: code, Message: err.Error()})
}

func recordIntegrityWarning(report *WorkspaceReport, context documentContext, code, message string) {
	report.IntegrityWarningFiles = append(report.IntegrityWarningFiles, Issue{Source: context.Source.ContentPath, DocumentID: context.NodeID, Code: code, Message: message})
}

func readSourceManifest(runDir string) (SourceManifest, string, error) {
	manifestPath := filepath.Join(runDir, "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return SourceManifest{}, "", fmt.Errorf("read source manifest: %w", err)
	}
	var manifest SourceManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return SourceManifest{}, "", fmt.Errorf("decode source manifest: %w", err)
	}
	if manifest.FormatVersion != SourceFormatVersion {
		return SourceManifest{}, "", fmt.Errorf("unsupported source manifest formatVersion %d", manifest.FormatVersion)
	}
	if manifest.Source.Provider != "bitrix24-note" {
		return SourceManifest{}, "", fmt.Errorf("unsupported source provider %q", manifest.Source.Provider)
	}
	runID := filepath.Base(runDir)
	if !strings.HasPrefix(runID, "run_") {
		return SourceManifest{}, "", fmt.Errorf("source directory must be a finalized run_* directory: %s", runDir)
	}
	var sourceReport struct {
		Status string `json:"status"`
		RunID  string `json:"runId"`
	}
	reportData, err := os.ReadFile(filepath.Join(runDir, "report.json"))
	if err != nil {
		return SourceManifest{}, "", fmt.Errorf("read source report: %w", err)
	}
	if err := json.Unmarshal(reportData, &sourceReport); err != nil {
		return SourceManifest{}, "", fmt.Errorf("decode source report: %w", err)
	}
	if sourceReport.Status != "complete" && sourceReport.Status != "complete_with_warnings" {
		return SourceManifest{}, "", fmt.Errorf("cannot normalize source run with status %q", sourceReport.Status)
	}
	if sourceReport.RunID != runID {
		return SourceManifest{}, "", fmt.Errorf("source report belongs to %s instead of %s", sourceReport.RunID, runID)
	}
	seenDocuments := map[int]int{}
	var validateGlobalIDs func(int, []SourceDocument) error
	validateGlobalIDs = func(workspaceID int, documents []SourceDocument) error {
		for _, document := range documents {
			if previousWorkspace, duplicate := seenDocuments[document.ID]; duplicate {
				if previousWorkspace != workspaceID {
					return fmt.Errorf("document %d appears in workspaces %d and %d", document.ID, previousWorkspace, workspaceID)
				}
			} else {
				seenDocuments[document.ID] = workspaceID
			}
			if err := validateGlobalIDs(workspaceID, document.Children); err != nil {
				return err
			}
		}
		return nil
	}
	for _, workspace := range manifest.Workspaces {
		if err := validateGlobalIDs(workspace.ID, workspace.Documents); err != nil {
			return SourceManifest{}, "", err
		}
	}
	return manifest, runID, nil
}

func collectAllDocumentIDs(workspaces []SourceWorkspace) map[int]struct{} {
	result := map[int]struct{}{}
	var walk func([]SourceDocument)
	walk = func(documents []SourceDocument) {
		for _, document := range documents {
			result[document.ID] = struct{}{}
			walk(document.Children)
		}
	}
	for _, workspace := range workspaces {
		walk(workspace.Documents)
	}
	return result
}

func selectWorkspaces(items []SourceWorkspace, include, exclude map[int]struct{}) ([]SourceWorkspace, error) {
	available := map[int]struct{}{}
	for _, workspace := range items {
		if workspace.ID <= 0 {
			return nil, fmt.Errorf("source manifest contains invalid workspace ID %d", workspace.ID)
		}
		if _, duplicate := available[workspace.ID]; duplicate {
			return nil, fmt.Errorf("source manifest contains duplicate workspace %d", workspace.ID)
		}
		available[workspace.ID] = struct{}{}
	}
	for id := range include {
		if _, exists := available[id]; !exists {
			return nil, fmt.Errorf("included workspace %d does not exist in the source run", id)
		}
	}
	for id := range exclude {
		if _, exists := available[id]; !exists {
			return nil, fmt.Errorf("excluded workspace %d does not exist in the source run", id)
		}
	}
	selected := make([]SourceWorkspace, 0, len(items))
	for _, workspace := range items {
		if len(include) > 0 {
			if _, exists := include[workspace.ID]; !exists {
				continue
			}
		}
		if _, excluded := exclude[workspace.ID]; excluded {
			continue
		}
		selected = append(selected, workspace)
	}
	return selected, nil
}

func validateFilters(include, exclude map[int]struct{}) error {
	for id := range include {
		if id <= 0 {
			return fmt.Errorf("workspace ID must be positive: %d", id)
		}
		if _, overlap := exclude[id]; overlap {
			return fmt.Errorf("workspace %d appears in both --include and --exclude", id)
		}
	}
	for id := range exclude {
		if id <= 0 {
			return fmt.Errorf("workspace ID must be positive: %d", id)
		}
	}
	return nil
}

func validatePaths(runValue, outputValue string) (string, string, error) {
	if strings.TrimSpace(runValue) == "" {
		return "", "", errors.New("source run directory is required")
	}
	if strings.TrimSpace(outputValue) == "" {
		return "", "", errors.New("output directory is required")
	}
	runDir, err := filepath.Abs(runValue)
	if err != nil {
		return "", "", fmt.Errorf("resolve source run: %w", err)
	}
	outputDir, err := filepath.Abs(outputValue)
	if err != nil {
		return "", "", fmt.Errorf("resolve output directory: %w", err)
	}
	info, err := os.Stat(runDir)
	if err != nil || !info.IsDir() {
		return "", "", fmt.Errorf("source run does not exist or is not a directory: %s", runDir)
	}
	runName := filepath.Base(runDir)
	if !strings.HasPrefix(runName, "run_") || len(runName) == len("run_") {
		return "", "", fmt.Errorf("source directory must be named run_<id>: %s", runDir)
	}
	expectedOutput := "normalized_" + strings.TrimPrefix(runName, "run_")
	if filepath.Base(outputDir) != expectedOutput {
		return "", "", fmt.Errorf("normalization output directory must be named %s: %s", expectedOutput, outputDir)
	}
	if pathsOverlap(runDir, outputDir) {
		return "", "", errors.New("source run and output directory must not contain one another")
	}
	return filepath.Clean(runDir), filepath.Clean(outputDir), nil
}

func pathsOverlap(left, right string) bool {
	left = filepath.Clean(left)
	right = filepath.Clean(right)
	if left == right {
		return true
	}
	leftRelative, leftErr := filepath.Rel(left, right)
	rightRelative, rightErr := filepath.Rel(right, left)
	return (leftErr == nil && leftRelative != ".." && !strings.HasPrefix(leftRelative, ".."+string(filepath.Separator))) ||
		(rightErr == nil && rightRelative != ".." && !strings.HasPrefix(rightRelative, ".."+string(filepath.Separator)))
}

func safeJoin(root, relative string) (string, error) {
	if relative == "" || filepath.IsAbs(relative) || strings.HasPrefix(strings.ToLower(relative), "file://") {
		return "", fmt.Errorf("path must be relative to the source run: %s", relative)
	}
	joined := filepath.Join(root, filepath.FromSlash(relative))
	rel, err := filepath.Rel(root, joined)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes the source run: %s", relative)
	}
	return joined, nil
}

func writeJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(file)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	encodeErr := encoder.Encode(value)
	closeErr := file.Close()
	return errors.Join(encodeErr, closeErr)
}

func writeJSONL(path string, documents []NormalizedDocument) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(file)
	encoder.SetEscapeHTML(false)
	var encodeErr error
	for _, document := range documents {
		if err := encoder.Encode(document); err != nil {
			encodeErr = err
			break
		}
	}
	closeErr := file.Close()
	return errors.Join(encodeErr, closeErr)
}

func publishDirectory(staging, target string, force bool) error {
	if !force {
		if err := os.Rename(staging, target); err != nil {
			return fmt.Errorf("publish normalization result: %w", err)
		}
		return nil
	}
	backup := target + fmt.Sprintf(".backup-%d-%d", os.Getpid(), time.Now().UnixNano())
	hadTarget := false
	if _, err := os.Stat(target); err == nil {
		if err := os.Rename(target, backup); err != nil {
			return fmt.Errorf("backup existing output: %w", err)
		}
		hadTarget = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect existing output: %w", err)
	}
	if err := os.Rename(staging, target); err != nil {
		if hadTarget {
			_ = os.Rename(backup, target)
		}
		return fmt.Errorf("publish normalization result: %w", err)
	}
	if hadTarget {
		if err := os.RemoveAll(backup); err != nil {
			return fmt.Errorf("remove normalization backup: %w", err)
		}
	}
	return nil
}

func pageNodeID(id int) string { return "page_" + strconv.Itoa(id) }

func sameOptionalInt(left, right *int) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func optionalInt(value *int) string {
	if value == nil {
		return "null"
	}
	return strconv.Itoa(*value)
}

func appendCopy[T any](items []T, extra ...T) []T {
	result := make([]T, len(items), len(items)+len(extra))
	copy(result, items)
	return append(result, extra...)
}

func maxDepth(documents []documentContext) int {
	maximum := 0
	for _, document := range documents {
		if document.Depth > maximum {
			maximum = document.Depth
		}
	}
	return maximum
}

func isRemoteReference(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme != ""
}

func now(options Options) time.Time {
	if options.Now != nil {
		return options.Now()
	}
	return time.Now()
}
