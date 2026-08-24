package indexer

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const runReportFormatVersion = 1

type normalizationManifest struct {
	FormatVersion int                 `json:"formatVersion"`
	SourceRun     string              `json:"sourceRun"`
	Workspaces    []manifestWorkspace `json:"workspaces"`
}

type manifestWorkspace struct {
	ID               int    `json:"id"`
	Name             string `json:"name"`
	Path             string `json:"path"`
	QdrantCollection string `json:"qdrantCollection"`
	Status           string `json:"status"`
	Errors           int    `json:"errors"`
}

type workspacePaths struct {
	chunks      string
	chunkReport string
	indexReport string
}

func IndexRun(ctx context.Context, config RunConfig, dependencies Dependencies) (RunReport, error) {
	startedAt := time.Now().UTC()
	started := time.Now()
	normalizeRunConfig(&config)
	manifest, workspaces, pathsByID, err := validateRun(config.RunDir)
	if err != nil {
		return RunReport{}, err
	}
	provider := dependencies.Provider
	if provider == nil {
		provider = NewTEIClient(config.TEIURL, config.TEITimeout)
	}
	store := dependencies.Store
	if store == nil {
		store = NewQdrantClient(config.QdrantURL, config.QdrantAPIKey, config.QdrantTimeout)
	}

	report := RunReport{
		FormatVersion: runReportFormatVersion, Status: "complete", RunDir: portableRelative(config.RunDir),
		SourceRun: manifest.SourceRun, StartedAt: startedAt.Format(time.RFC3339Nano),
		DryRun: config.DryRun, Prune: true, WorkspaceCount: len(workspaces),
		Workspaces: make([]RunWorkspaceReport, 0, len(workspaces)),
	}
	for index, workspace := range workspaces {
		paths := pathsByID[workspace.ID]
		if config.Progress != nil {
			config.Progress(fmt.Sprintf("[%d/%d] Syncing workspace %s (%d) to %s...", index+1, len(workspaces), workspace.Name, workspace.ID, workspace.QdrantCollection))
		}
		if workspace.Status == "failed" || workspace.Errors > 0 {
			message := "normalization status is failed; the Qdrant collection was not changed"
			if workspace.Errors > 0 {
				message = fmt.Sprintf("normalization contains %d error(s); the Qdrant collection was not changed", workspace.Errors)
			}
			report.Workspaces = append(report.Workspaces, skippedWorkspace(workspace, paths, config.RunDir, message))
			report.SkippedWorkspaces++
			continue
		}
		workspaceConfig := Config{
			InputFile: paths.chunks, ReportFile: paths.indexReport, Collection: workspace.QdrantCollection,
			QdrantURL: config.QdrantURL, QdrantAPIKey: config.QdrantAPIKey, QdrantTimeout: config.QdrantTimeout,
			TEIURL: config.TEIURL, TEITimeout: config.TEITimeout, BatchSize: config.BatchSize,
			PipelineVersion: config.PipelineVersion, VectorCacheSize: config.VectorCacheSize,
			StoreEmbeddingText: config.StoreEmbeddingText, DryRun: config.DryRun,
			ForceReembed: config.ForceReembed, Prune: true, ContinueOnError: config.ContinueOnError,
			LogLevel: config.LogLevel, Progress: config.Progress,
		}
		workspaceReport, indexErr := Index(ctx, workspaceConfig, Dependencies{Provider: provider, Store: store})
		addReportTotals(&report.Totals, workspaceReport)
		entry := completeWorkspace(workspace, paths, config.RunDir, workspaceReport)
		if indexErr != nil || workspaceReport.FailedChunks > 0 || len(workspaceReport.Errors) > 0 {
			entry.Status = "failed"
			message := "indexing completed with chunk or pipeline errors; prune was not applied"
			if indexErr != nil {
				message = indexErr.Error()
			}
			entry.Error = &message
			report.FailedWorkspaces++
		} else {
			report.SuccessfulWorkspaces++
		}
		report.ProcessedWorkspaces++
		report.Workspaces = append(report.Workspaces, entry)
	}
	if report.FailedWorkspaces > 0 || report.SkippedWorkspaces > 0 {
		if report.SuccessfulWorkspaces == 0 {
			report.Status = "failed"
		} else {
			report.Status = "complete_with_errors"
		}
	}
	report.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
	report.DurationMS = time.Since(started).Milliseconds()
	if err := writeRunReport(config.ReportFile, report); err != nil {
		return report, err
	}
	return report, nil
}

func validateRun(runDir string) (normalizationManifest, []manifestWorkspace, map[int]workspacePaths, error) {
	var manifest normalizationManifest
	manifestPath := filepath.Join(runDir, "normalization_manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return manifest, nil, nil, fmt.Errorf("cannot read normalization manifest %s: %w", manifestPath, err)
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return manifest, nil, nil, fmt.Errorf("cannot parse normalization manifest %s: %w", manifestPath, err)
	}
	if manifest.FormatVersion != 1 {
		return manifest, nil, nil, fmt.Errorf("unsupported normalization manifest formatVersion: %d", manifest.FormatVersion)
	}
	if strings.TrimSpace(manifest.SourceRun) == "" {
		return manifest, nil, nil, fmt.Errorf("normalization manifest sourceRun must be non-empty")
	}
	if len(manifest.Workspaces) == 0 {
		return manifest, nil, nil, fmt.Errorf("normalization manifest contains no workspaces")
	}
	ids, paths, collections := map[int]struct{}{}, map[string]struct{}{}, map[string]struct{}{}
	resolved := make(map[int]workspacePaths, len(manifest.Workspaces))
	for index, workspace := range manifest.Workspaces {
		if workspace.ID <= 0 || strings.TrimSpace(workspace.Name) == "" || strings.TrimSpace(workspace.Path) == "" || strings.TrimSpace(workspace.QdrantCollection) == "" {
			return manifest, nil, nil, fmt.Errorf("workspace %d has invalid required fields", index+1)
		}
		if workspace.Status != "complete" && workspace.Status != "complete_with_warnings" && workspace.Status != "failed" {
			return manifest, nil, nil, fmt.Errorf("workspace %d has invalid status %q", workspace.ID, workspace.Status)
		}
		if workspace.Errors < 0 {
			return manifest, nil, nil, fmt.Errorf("workspace %d has invalid errors count", workspace.ID)
		}
		workspaceDir, err := resolveWithin(runDir, workspace.Path)
		if err != nil {
			return manifest, nil, nil, fmt.Errorf("workspace %d path: %w", workspace.ID, err)
		}
		normalizedPath, _ := filepath.Rel(runDir, workspaceDir)
		if _, found := ids[workspace.ID]; found {
			return manifest, nil, nil, fmt.Errorf("duplicate workspace id: %d", workspace.ID)
		}
		if _, found := paths[normalizedPath]; found {
			return manifest, nil, nil, fmt.Errorf("duplicate workspace path: %s", workspace.Path)
		}
		if _, found := collections[workspace.QdrantCollection]; found {
			return manifest, nil, nil, fmt.Errorf("duplicate qdrantCollection: %s", workspace.QdrantCollection)
		}
		ids[workspace.ID] = struct{}{}
		paths[normalizedPath] = struct{}{}
		collections[workspace.QdrantCollection] = struct{}{}
		workspaceFiles := workspacePaths{
			chunks: filepath.Join(workspaceDir, "chunks.jsonl"), chunkReport: filepath.Join(workspaceDir, "chunk_report.json"),
			indexReport: filepath.Join(workspaceDir, "index-report."+safeCollectionName(workspace.QdrantCollection)+".json"),
		}
		resolved[workspace.ID] = workspaceFiles
		if workspace.Status == "failed" || workspace.Errors > 0 {
			continue
		}
		if err := validateChunkArtifacts(workspaceFiles); err != nil {
			return manifest, nil, nil, fmt.Errorf("workspace %d: %w", workspace.ID, err)
		}
	}
	return manifest, manifest.Workspaces, resolved, nil
}

func validateChunkArtifacts(paths workspacePaths) error {
	info, err := os.Stat(paths.chunks)
	if err != nil || !info.Mode().IsRegular() {
		if err == nil {
			err = fmt.Errorf("path is not a file")
		}
		return fmt.Errorf("cannot read chunks file %s: %w", paths.chunks, err)
	}
	data, err := os.ReadFile(paths.chunkReport)
	if err != nil {
		return fmt.Errorf("cannot read chunk report %s: %w", paths.chunkReport, err)
	}
	var report struct {
		Errors *int `json:"errors"`
	}
	if err := json.Unmarshal(data, &report); err != nil || report.Errors == nil || *report.Errors < 0 {
		return fmt.Errorf("chunk report %s has an invalid errors count", paths.chunkReport)
	}
	if *report.Errors > 0 {
		return fmt.Errorf("chunk report %s contains %d error(s); the Qdrant collection was not changed", paths.chunkReport, *report.Errors)
	}
	return nil
}

func resolveWithin(root, value string) (string, error) {
	if filepath.IsAbs(value) || windowsAbsolute.MatchString(value) || strings.Contains(value, "\\") {
		return "", fmt.Errorf("must be a portable relative path: %s", value)
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.Abs(filepath.Join(rootAbs, filepath.FromSlash(value)))
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(rootAbs, resolved)
	if err != nil {
		return "", err
	}
	if relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("must point inside the run directory: %s", value)
	}
	return resolved, nil
}

func completeWorkspace(workspace manifestWorkspace, paths workspacePaths, runDir string, report Report) RunWorkspaceReport {
	input, reportPath := filepath.ToSlash(relativeOrValue(runDir, paths.chunks)), filepath.ToSlash(relativeOrValue(runDir, paths.indexReport))
	return RunWorkspaceReport{WorkspaceID: workspace.ID, WorkspaceName: workspace.Name, WorkspacePath: workspace.Path,
		Collection: workspace.QdrantCollection, Status: "complete", InputFile: &input, ReportFile: &reportPath,
		InputChunks: report.InputChunks, EmbeddedChunks: report.EmbeddedChunks, PayloadOnlyUpdates: report.PayloadOnlyUpdates,
		SkippedUnchanged: report.SkippedUnchanged, StaleChunksFound: report.StaleChunksFound,
		DeletedChunks: report.DeletedChunks, FailedChunks: report.FailedChunks}
}

func skippedWorkspace(workspace manifestWorkspace, paths workspacePaths, runDir, message string) RunWorkspaceReport {
	entry := completeWorkspace(workspace, paths, runDir, Report{})
	entry.Status = "skipped"
	entry.Error = &message
	return entry
}

func addReportTotals(totals *ReportTotals, report Report) {
	totals.InputChunks += report.InputChunks
	totals.ValidChunks += report.ValidChunks
	totals.NewChunks += report.NewChunks
	totals.ChangedEmbeddings += report.ChangedEmbeddings
	totals.EmbeddedChunks += report.EmbeddedChunks
	totals.ReusedVectorsInRun += report.ReusedVectorsInRun
	totals.PayloadOnlyUpdates += report.PayloadOnlyUpdates
	totals.SkippedUnchanged += report.SkippedUnchanged
	totals.StaleChunksFound += report.StaleChunksFound
	totals.DeletedChunks += report.DeletedChunks
	totals.FailedChunks += report.FailedChunks
}

func writeRunReport(path string, report RunReport) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func normalizeRunConfig(config *RunConfig) {
	base := Config{QdrantURL: config.QdrantURL, QdrantTimeout: config.QdrantTimeout, TEIURL: config.TEIURL, TEITimeout: config.TEITimeout,
		BatchSize: config.BatchSize, PipelineVersion: config.PipelineVersion, VectorCacheSize: config.VectorCacheSize}
	normalizeConfig(&base)
	config.QdrantURL = base.QdrantURL
	config.QdrantTimeout = base.QdrantTimeout
	config.TEIURL = base.TEIURL
	config.TEITimeout = base.TEITimeout
	config.BatchSize = base.BatchSize
	config.PipelineVersion = base.PipelineVersion
	config.VectorCacheSize = base.VectorCacheSize
	if config.ReportFile == "" {
		config.ReportFile = filepath.Join(config.RunDir, "index-report.json")
	}
}

func relativeOrValue(root, value string) string {
	relative, err := filepath.Rel(root, value)
	if err != nil {
		return value
	}
	return relative
}
