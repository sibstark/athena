package snapshot

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sibstark/athena/fetcher/internal/bitrix"
)

type BitrixClient interface {
	PortalOrigin() string
	Redact(string) string
	ListWorkspaces(context.Context) ([]bitrix.Workspace, error)
	GetDocumentTree(context.Context, int) (bitrix.DocumentTreeResult, error)
	GetDocument(context.Context, int) (bitrix.Document, error)
}

type Options struct {
	OutputDir           string
	Include             map[int]struct{}
	Exclude             map[int]struct{}
	SkipAssets          bool
	AssetHTTPClient     *http.Client
	DocumentConcurrency int
	AssetConcurrency    int
	MaxAssetBytes       int64
	Now                 func() time.Time
	Progress            func(string)
}

type Exporter struct {
	client BitrixClient
}

func NewExporter(client BitrixClient) *Exporter {
	return &Exporter{client: client}
}

func (exporter *Exporter) Fetch(ctx context.Context, options Options) (Summary, error) {
	root, err := validateOutputRoot(options.OutputDir)
	if err != nil {
		return Summary{}, err
	}
	lock, err := acquireArchiveLock(root)
	if err != nil {
		return Summary{}, err
	}
	defer lock.Close()

	now := options.Now
	if now == nil {
		now = time.Now
	}
	started := now().UTC()
	runID, err := allocateRunID(root, started.UnixMilli())
	if err != nil {
		return Summary{}, err
	}
	finalPath := filepath.Join(root, runID)
	stagingPath := filepath.Join(root, "."+runID+".tmp")
	log := options.Progress
	if log == nil {
		log = func(string) {}
	}
	assetMode := "download"
	if options.SkipAssets {
		assetMode = "skip"
	}
	state := runState{runID: runID, root: root, finalPath: finalPath, stagingPath: stagingPath, started: started, assetMode: assetMode, now: now}

	if err := validateFilters(options.Include, options.Exclude); err != nil {
		return exporter.failRun(state, err)
	}
	log("Loading accessible workspaces...")
	workspaces, err := exporter.client.ListWorkspaces(ctx)
	if err != nil {
		return exporter.failRun(state, err)
	}
	selected, err := selectWorkspaces(workspaces, options.Include, options.Exclude)
	if err != nil {
		return exporter.failRun(state, err)
	}
	state.workspaceCount = len(selected)

	planned := make([]plannedWorkspace, 0, len(selected))
	for index, workspace := range selected {
		log(fmt.Sprintf("[%d/%d] Loading tree for %s (%d)...", index+1, len(selected), workspace.Name, workspace.ID))
		tree, err := exporter.client.GetDocumentTree(ctx, workspace.ID)
		if err != nil {
			return exporter.failRun(state, err)
		}
		if tree.Truncated {
			return exporter.failRun(state, fmt.Errorf("workspace %d tree exceeds the Bitrix limit of 5000 nodes and was truncated", workspace.ID))
		}
		plan, err := planWorkspace(exporter.client.PortalOrigin(), workspace, tree.Items)
		if err != nil {
			return exporter.failRun(state, err)
		}
		state.documentCount += len(plan.Documents)
		planned = append(planned, plan)
	}

	if err := os.Mkdir(stagingPath, 0o755); err != nil {
		return exporter.failRun(state, fmt.Errorf("create run staging directory: %w", err))
	}
	for _, workspace := range planned {
		path := filepath.Join(stagingPath, filepath.FromSlash(workspace.Manifest.Path))
		if err := os.MkdirAll(path, 0o755); err != nil {
			return exporter.failRun(state, fmt.Errorf("create workspace directory: %w", err))
		}
	}

	assetManager := newAssetManager(assetManagerOptions{
		Root: stagingPath, Portal: exporter.client.PortalOrigin(), HTTPClient: options.AssetHTTPClient,
		Redact: exporter.client.Redact, Skip: options.SkipAssets, MaxBytes: options.MaxAssetBytes,
		Concurrency: options.AssetConcurrency,
	})
	documentConcurrency := options.DocumentConcurrency
	if documentConcurrency == 0 {
		documentConcurrency = 4
	}
	if documentConcurrency < 1 {
		return exporter.failRun(state, errors.New("document concurrency must be positive"))
	}
	tasks := flattenTasks(planned)
	log(fmt.Sprintf("Fetching %d documents...", len(tasks)))
	if err := exporter.fetchDocuments(ctx, tasks, stagingPath, assetManager, documentConcurrency, log); err != nil {
		return exporter.failRun(state, err)
	}

	assetRecords := assetManager.recordsList()
	downloaded, failed := countAssets(assetRecords)
	status := StatusComplete
	if failed > 0 {
		status = StatusCompleteWithWarnings
	}
	finished := now().UTC()
	manifest := Manifest{
		FormatVersion: FormatVersion,
		ExportedAt:    finished.Format(time.RFC3339Nano),
		Source:        Source{Provider: "bitrix24-note", Portal: exporter.client.PortalOrigin(), URL: strings.TrimRight(exporter.client.PortalOrigin(), "/") + "/note/"},
		AssetMode:     assetMode,
		Workspaces:    workspaceManifests(planned),
	}
	report := Report{
		FormatVersion: FormatVersion, RunID: runID, Status: status,
		StartedAt: started.Format(time.RFC3339Nano), FinishedAt: finished.Format(time.RFC3339Nano),
		DurationMS: finished.Sub(started).Milliseconds(), WorkspaceCount: state.workspaceCount, DocumentCount: state.documentCount,
		Assets: AssetsReport{Mode: assetMode, Downloaded: downloaded, Failed: failed, Items: assetRecords},
	}
	if err := writeJSON(filepath.Join(stagingPath, "manifest.json"), manifest); err != nil {
		return exporter.failRun(state, fmt.Errorf("write manifest: %w", err))
	}
	if err := writeJSON(filepath.Join(stagingPath, "report.json"), report); err != nil {
		return exporter.failRun(state, fmt.Errorf("write report: %w", err))
	}
	if err := os.Rename(stagingPath, finalPath); err != nil {
		return exporter.failRun(state, fmt.Errorf("publish run: %w", err))
	}
	if err := rebuildRegistry(root); err != nil {
		return Summary{RunID: runID, RunPath: finalPath, Status: status, WorkspaceCount: state.workspaceCount, DocumentCount: state.documentCount, AssetsDownloaded: downloaded, AssetsFailed: failed}, fmt.Errorf("run %s was published but runs.json could not be rebuilt: %w", runID, err)
	}
	return Summary{RunID: runID, RunPath: finalPath, Status: status, WorkspaceCount: state.workspaceCount, DocumentCount: state.documentCount, AssetsDownloaded: downloaded, AssetsFailed: failed}, nil
}

type runState struct {
	runID          string
	root           string
	finalPath      string
	stagingPath    string
	started        time.Time
	assetMode      string
	now            func() time.Time
	workspaceCount int
	documentCount  int
}

func (exporter *Exporter) failRun(state runState, cause error) (Summary, error) {
	_ = os.RemoveAll(state.stagingPath)
	if err := os.Mkdir(state.stagingPath, 0o755); err != nil {
		return Summary{}, errors.Join(cause, fmt.Errorf("create failed run directory: %w", err))
	}
	finished := state.now().UTC()
	report := Report{
		FormatVersion: FormatVersion, RunID: state.runID, Status: StatusFailed,
		StartedAt: state.started.Format(time.RFC3339Nano), FinishedAt: finished.Format(time.RFC3339Nano),
		DurationMS: finished.Sub(state.started).Milliseconds(), WorkspaceCount: state.workspaceCount, DocumentCount: state.documentCount,
		Assets: AssetsReport{Mode: state.assetMode, Items: []AssetRecord{}},
		Error:  &Failure{Message: exporter.client.Redact(cause.Error())},
	}
	if err := writeJSON(filepath.Join(state.stagingPath, "report.json"), report); err != nil {
		_ = os.RemoveAll(state.stagingPath)
		return Summary{}, errors.Join(cause, fmt.Errorf("write failed run report: %w", err))
	}
	if err := os.Rename(state.stagingPath, state.finalPath); err != nil {
		_ = os.RemoveAll(state.stagingPath)
		return Summary{}, errors.Join(cause, fmt.Errorf("publish failed run report: %w", err))
	}
	registryErr := rebuildRegistry(state.root)
	wrapped := fmt.Errorf("run %s failed: %w", state.runID, cause)
	if registryErr != nil {
		wrapped = errors.Join(wrapped, fmt.Errorf("rebuild runs.json: %w", registryErr))
	}
	return Summary{RunID: state.runID, RunPath: state.finalPath, Status: StatusFailed, WorkspaceCount: state.workspaceCount, DocumentCount: state.documentCount}, wrapped
}

func (exporter *Exporter) fetchDocuments(ctx context.Context, tasks []plannedDocument, stagingPath string, assets *assetManager, concurrency int, log func(string)) error {
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan plannedDocument)
	var workers sync.WaitGroup
	var firstErr error
	var errorOnce sync.Once
	var completed atomic.Int64
	for range concurrency {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for task := range jobs {
				if workerCtx.Err() != nil {
					return
				}
				document, err := exporter.client.GetDocument(workerCtx, task.Node.ID)
				if err == nil {
					err = validateDocument(document, task.Node)
				}
				if err == nil {
					absolutePath := filepath.Join(stagingPath, filepath.FromSlash(task.Manifest.ContentPath))
					err = ensureWithin(stagingPath, absolutePath)
					if err == nil {
						err = os.MkdirAll(filepath.Dir(absolutePath), 0o755)
					}
					if err == nil {
						markdown := assets.processMarkdown(workerCtx, task.Node.CollectionID, document.Markdown, absolutePath)
						err = os.WriteFile(absolutePath, []byte(renderMarkdown(document, task.Node.CollectionID, exporter.client.PortalOrigin(), markdown)), 0o644)
					}
				}
				if err != nil {
					errorOnce.Do(func() {
						firstErr = fmt.Errorf("document %d: %w", task.Node.ID, err)
						cancel()
					})
					return
				}
				populateDocumentManifest(task.Manifest, document)
				count := completed.Add(1)
				log(fmt.Sprintf("[%d/%d] %s", count, len(tasks), document.Title))
			}
		}()
	}

sendLoop:
	for _, task := range tasks {
		select {
		case <-workerCtx.Done():
			break sendLoop
		case jobs <- task:
		}
	}
	close(jobs)
	workers.Wait()
	if firstErr != nil {
		return firstErr
	}
	return ctx.Err()
}

func planWorkspace(portal string, workspace bitrix.Workspace, nodes []bitrix.TreeNode) (plannedWorkspace, error) {
	workspacePath := fmt.Sprintf("workspace_%d", workspace.ID)
	documents, err := planDocumentNodes(portal, workspace.ID, workspacePath, nil, nodes, make(map[int]struct{}))
	if err != nil {
		return plannedWorkspace{}, err
	}
	manifest := WorkspaceManifest{
		ID: workspace.ID, Name: workspace.Name, Position: workspace.Position, PolicyLevel: workspace.PolicyLevel,
		CreatedBy: workspace.CreatedBy, UpdatedBy: workspace.UpdatedBy, CreatedAt: workspace.CreatedAt, UpdatedAt: workspace.UpdatedAt,
		Path: workspacePath, URL: fmt.Sprintf("%s/note/workspace/%d/", strings.TrimRight(portal, "/"), workspace.ID),
		QdrantCollection: workspacePath, Documents: documents,
	}
	tasks := make([]plannedDocument, 0)
	collectPlannedDocuments(nodes, manifest.Documents, &tasks)
	manifest.DocumentCount = len(tasks)
	return plannedWorkspace{Workspace: workspace, Manifest: manifest, Documents: tasks}, nil
}

func planDocumentNodes(portal string, workspaceID int, parentPath string, expectedParent *int, nodes []bitrix.TreeNode, seen map[int]struct{}) ([]DocumentManifest, error) {
	sorted := append([]bitrix.TreeNode(nil), nodes...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Position != sorted[j].Position {
			return sorted[i].Position < sorted[j].Position
		}
		return sorted[i].ID < sorted[j].ID
	})
	result := make([]DocumentManifest, 0, len(sorted))
	for _, node := range sorted {
		if _, duplicate := seen[node.ID]; duplicate {
			return nil, fmt.Errorf("workspace %d tree contains duplicate document %d", workspaceID, node.ID)
		}
		seen[node.ID] = struct{}{}
		if node.CollectionID != workspaceID {
			return nil, fmt.Errorf("document %d belongs to workspace %d instead of %d", node.ID, node.CollectionID, workspaceID)
		}
		if !sameOptionalInt(node.ParentID, expectedParent) {
			return nil, fmt.Errorf("document %d has parent %s, expected %s", node.ID, optionalInt(node.ParentID), optionalInt(expectedParent))
		}
		path := filepath.ToSlash(filepath.Join(parentPath, fmt.Sprintf("document_%d", node.ID)))
		parentID := node.ID
		children, err := planDocumentNodes(portal, workspaceID, path, &parentID, node.Children, seen)
		if err != nil {
			return nil, err
		}
		result = append(result, DocumentManifest{
			ID: node.ID, WorkspaceID: workspaceID, ParentID: node.ParentID, Title: node.Title, Position: node.Position,
			Path: path, ContentPath: filepath.ToSlash(filepath.Join(path, "content.md")),
			URL: fmt.Sprintf("%s/note/document/%d/", strings.TrimRight(portal, "/"), node.ID), Children: children,
		})
	}
	return result, nil
}

func collectPlannedDocuments(nodes []bitrix.TreeNode, manifests []DocumentManifest, target *[]plannedDocument) {
	for index := range nodes {
		// manifests are sorted independently; resolve by ID rather than relying on input order.
		for manifestIndex := range manifests {
			if manifests[manifestIndex].ID != nodes[index].ID {
				continue
			}
			*target = append(*target, plannedDocument{Node: nodes[index], Manifest: &manifests[manifestIndex]})
			collectPlannedDocuments(nodes[index].Children, manifests[manifestIndex].Children, target)
			break
		}
	}
}

func flattenTasks(workspaces []plannedWorkspace) []plannedDocument {
	var result []plannedDocument
	for index := range workspaces {
		result = append(result, workspaces[index].Documents...)
	}
	return result
}

func workspaceManifests(workspaces []plannedWorkspace) []WorkspaceManifest {
	result := make([]WorkspaceManifest, len(workspaces))
	for index := range workspaces {
		result[index] = workspaces[index].Manifest
	}
	return result
}

func validateDocument(document bitrix.Document, node bitrix.TreeNode) error {
	if document.ID != node.ID {
		return fmt.Errorf("Bitrix returned document %d", document.ID)
	}
	if document.CollectionID == nil || *document.CollectionID != node.CollectionID {
		return fmt.Errorf("workspace is %s, expected %d", optionalInt(document.CollectionID), node.CollectionID)
	}
	if !sameOptionalInt(document.ParentID, node.ParentID) {
		return fmt.Errorf("parent is %s, expected %s", optionalInt(document.ParentID), optionalInt(node.ParentID))
	}
	return nil
}

func populateDocumentManifest(manifest *DocumentManifest, document bitrix.Document) {
	manifest.Title = document.Title
	manifest.Position = document.Position
	manifest.CreatedBy = document.CreatedBy
	manifest.UpdatedBy = document.UpdatedBy
	manifest.CreatedAt = document.CreatedAt
	manifest.UpdatedAt = document.UpdatedAt
}

func renderMarkdown(document bitrix.Document, workspaceID int, portal, markdown string) string {
	parent := "null"
	if document.ParentID != nil {
		parent = strconv.Itoa(*document.ParentID)
	}
	workspaceURL := fmt.Sprintf("%s/note/workspace/%d/", strings.TrimRight(portal, "/"), workspaceID)
	documentURL := fmt.Sprintf("%s/note/document/%d/", strings.TrimRight(portal, "/"), document.ID)
	frontMatter := strings.Join([]string{
		"---",
		"document_id: " + strconv.Itoa(document.ID),
		"workspace_id: " + strconv.Itoa(workspaceID),
		"parent_document_id: " + parent,
		"title: " + strconv.Quote(document.Title),
		"position: " + strconv.Itoa(document.Position),
		"created_by: " + strconv.Itoa(document.CreatedBy),
		"updated_by: " + strconv.Itoa(document.UpdatedBy),
		"created_at: " + strconv.Quote(document.CreatedAt),
		"updated_at: " + strconv.Quote(document.UpdatedAt),
		"workspace_url: " + strconv.Quote(workspaceURL),
		"document_url: " + strconv.Quote(documentURL),
		"---",
		"",
	}, "\n")
	body := strings.TrimRight(strings.ReplaceAll(strings.ReplaceAll(markdown, "\r\n", "\n"), "\r", "\n"), "\n")
	if body == "" {
		return frontMatter
	}
	return frontMatter + body + "\n"
}

func selectWorkspaces(items []bitrix.Workspace, include, exclude map[int]struct{}) ([]bitrix.Workspace, error) {
	available := make(map[int]struct{}, len(items))
	for _, item := range items {
		available[item.ID] = struct{}{}
	}
	for id := range include {
		if _, ok := available[id]; !ok {
			return nil, fmt.Errorf("included workspace %d is not accessible", id)
		}
	}
	for id := range exclude {
		if _, ok := available[id]; !ok {
			return nil, fmt.Errorf("excluded workspace %d is not accessible", id)
		}
	}
	selected := make([]bitrix.Workspace, 0, len(items))
	for _, item := range items {
		if len(include) > 0 {
			if _, ok := include[item.ID]; !ok {
				continue
			}
		}
		if _, excluded := exclude[item.ID]; excluded {
			continue
		}
		selected = append(selected, item)
	}
	sort.Slice(selected, func(i, j int) bool {
		if selected[i].Position != selected[j].Position {
			return selected[i].Position < selected[j].Position
		}
		return selected[i].ID < selected[j].ID
	})
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

func allocateRunID(root string, unixMilliseconds int64) (string, error) {
	for offset := int64(0); offset < 1000; offset++ {
		runID := fmt.Sprintf("run_%d", unixMilliseconds+offset)
		paths := []string{filepath.Join(root, runID), filepath.Join(root, "."+runID+".tmp")}
		available := true
		for _, path := range paths {
			if _, err := os.Stat(path); err == nil {
				available = false
				break
			} else if !errors.Is(err, os.ErrNotExist) {
				return "", err
			}
		}
		if available {
			return runID, nil
		}
	}
	return "", errors.New("could not allocate a unique run ID")
}

func validateOutputRoot(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", errors.New("--out is required")
	}
	root, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	root = filepath.Clean(root)
	if filepath.Dir(root) == root {
		return "", errors.New("refusing to use a filesystem root as --out")
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(root, cwd)
	if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("refusing to use the current working directory or one of its parents as --out")
	}
	return root, nil
}

func ensureWithin(root, candidate string) error {
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	candidate, err = filepath.Abs(candidate)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(root, candidate)
	if err != nil {
		return err
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path escapes run directory: %s", candidate)
	}
	return nil
}

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

func countAssets(records []AssetRecord) (downloaded, failed int) {
	for _, record := range records {
		if record.Status == "downloaded" {
			downloaded++
		} else {
			failed++
		}
	}
	return downloaded, failed
}
