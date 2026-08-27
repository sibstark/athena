package indexer

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

type preparedChunk struct {
	line               int
	pointID            string
	chunk              ChunkRecord
	embeddingInput     string
	embeddingInputHash string
	payload            map[string]any
	reason             string
}

type rawLine struct {
	number int
	value  string
}

type batchOutcome struct {
	hadInputErrors bool
	batchFailed    bool
}

type batchContext struct {
	ctx                 context.Context
	config              Config
	provider            Provider
	store               Store
	pipeline            PipelineIdentity
	report              *Report
	cache               *vectorCache
	seenInputHashes     map[string]struct{}
	seenPointIDs        map[string]struct{}
	stalePointIDs       map[string]struct{}
	collectionAvailable bool
}

func Index(ctx context.Context, config Config, dependencies Dependencies) (Report, error) {
	started := time.Now()
	normalizeConfig(&config)
	report := newReport(config)
	if strings.TrimSpace(config.Collection) == "" {
		addError(&report, nil, nil, "configuration", fmt.Errorf("Qdrant collection is required"))
		report.TotalMS = elapsedMS(started)
		return report, writeReport(config.ReportFile, report)
	}
	if err := config.Embedding.Validate(); err != nil {
		addError(&report, nil, nil, "configuration", err)
		report.TotalMS = elapsedMS(started)
		return report, writeReport(config.ReportFile, report)
	}
	provider := dependencies.Provider
	if provider == nil {
		provider = NewTEIClient(config.TEIURL, config.TEITimeout, config.Embedding)
	}
	store := dependencies.Store
	if store == nil {
		store = NewQdrantClient(config.QdrantURL, config.QdrantAPIKey, config.QdrantTimeout)
	}

	providerStarted := time.Now()
	if err := provider.Ready(ctx); err != nil {
		report.ProviderCheckMS = elapsedMS(providerStarted)
		addError(&report, nil, nil, "provider", err)
		report.TotalMS = elapsedMS(started)
		return report, writeReport(config.ReportFile, report)
	}
	report.ProviderCheckMS = elapsedMS(providerStarted)

	pipe := pipeline(config.Embedding)
	qdrantStarted := time.Now()
	if err := store.Ping(ctx); err != nil {
		report.QdrantReadMS += elapsedMS(qdrantStarted)
		addError(&report, nil, nil, "qdrant", err)
		report.TotalMS = elapsedMS(started)
		return report, writeReport(config.ReportFile, report)
	}
	report.QdrantReadMS += elapsedMS(qdrantStarted)

	qdrantStarted = time.Now()
	state, err := store.EnsureCollection(ctx, config.Collection, config.Embedding.Dimension, "Cosine", pipe, config.DryRun)
	report.QdrantReadMS += elapsedMS(qdrantStarted)
	if err != nil {
		addError(&report, nil, nil, "qdrant", err)
		report.TotalMS = elapsedMS(started)
		return report, writeReport(config.ReportFile, report)
	}
	collectionAvailable := state.Exists || state.Created
	if collectionAvailable {
		qdrantStarted = time.Now()
		err = store.EnsurePayloadIndexes(ctx, config.Collection, payloadIndexes, config.DryRun)
		report.QdrantWriteMS += elapsedMS(qdrantStarted)
		if err != nil {
			addError(&report, nil, nil, "qdrant", err)
			report.TotalMS = elapsedMS(started)
			return report, writeReport(config.ReportFile, report)
		}
	}

	stale := make(map[string]struct{})
	if collectionAvailable && (config.Prune || config.DryRun) {
		qdrantStarted = time.Now()
		ids, listErr := store.ListManagedPointIDs(ctx, config.Collection, pipe)
		report.QdrantReadMS += elapsedMS(qdrantStarted)
		if listErr != nil {
			addError(&report, nil, nil, "qdrant", listErr)
			report.TotalMS = elapsedMS(started)
			return report, writeReport(config.ReportFile, report)
		}
		for _, id := range ids {
			stale[id] = struct{}{}
		}
	}

	file, err := os.Open(config.InputFile)
	if err != nil {
		addError(&report, nil, nil, "input", err)
		report.TotalMS = elapsedMS(started)
		return report, writeReport(config.ReportFile, report)
	}
	defer file.Close()

	batchCtx := &batchContext{
		ctx: ctx, config: config, provider: provider, store: store, pipeline: pipe,
		report: &report, cache: newVectorCache(config.Embedding.VectorCacheSize),
		seenInputHashes: make(map[string]struct{}), seenPointIDs: make(map[string]struct{}),
		stalePointIDs: stale, collectionAvailable: collectionAvailable,
	}
	batch := make([]rawLine, 0, config.BatchSize)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 32<<20)
	lineNumber := 0
	hadErrors := false
	aborted := false
	for scanner.Scan() {
		lineNumber++
		raw := scanner.Text()
		if strings.TrimSpace(raw) == "" {
			continue
		}
		report.InputChunks++
		batch = append(batch, rawLine{lineNumber, raw})
		if len(batch) < config.BatchSize {
			continue
		}
		outcome := processBatch(batchCtx, batch)
		hadErrors = hadErrors || outcome.hadInputErrors || outcome.batchFailed
		batch = batch[:0]
		if outcome.batchFailed && !config.ContinueOnError {
			aborted = true
			break
		}
	}
	if scanErr := scanner.Err(); scanErr != nil {
		hadErrors = true
		addError(&report, nil, nil, "input", scanErr)
	}
	if !aborted && len(batch) > 0 {
		outcome := processBatch(batchCtx, batch)
		hadErrors = hadErrors || outcome.hadInputErrors || outcome.batchFailed
	}

	report.StaleChunksFound = len(stale)
	if config.Prune && len(stale) > 0 {
		if hadErrors || report.FailedChunks > 0 || len(report.Errors) > 0 {
			report.PruneSkippedDueToErrors = true
		} else if !config.DryRun {
			ids := make([]string, 0, len(stale))
			for id := range stale {
				ids = append(ids, id)
			}
			for start := 0; start < len(ids); start += 128 {
				end := start + 128
				if end > len(ids) {
					end = len(ids)
				}
				writeStarted := time.Now()
				err := store.DeletePoints(ctx, config.Collection, ids[start:end])
				report.QdrantWriteMS += elapsedMS(writeStarted)
				if err != nil {
					addError(&report, nil, nil, "prune", err)
					report.PruneSkippedDueToErrors = true
					break
				}
				report.DeletedChunks += end - start
			}
		}
	}
	report.TotalMS = elapsedMS(started)
	if err := writeReport(config.ReportFile, report); err != nil {
		return report, err
	}
	return report, nil
}

func processBatch(context *batchContext, input []rawLine) batchOutcome {
	prepared := make([]preparedChunk, 0, len(input))
	hadErrors := false
	for _, item := range input {
		chunk, err := parseChunk(item.value)
		if err == nil {
			input := embeddingInput(chunk.EmbeddingText, context.config.Embedding.DocumentPrefix)
			count, tokenErr := context.provider.CountTokens(context.ctx, input)
			if tokenErr != nil {
				err = fmt.Errorf("tokenization failed: %w", tokenErr)
			} else if count > context.config.Embedding.MaxInputTokens {
				err = fmt.Errorf("model input has %d tokens; maximum is %d; re-run chunker with the configured tokenizer limits", count, context.config.Embedding.MaxInputTokens)
			} else {
				id, idErr := pointID(chunk.ChunkID, context.config.Embedding.PointNamespace)
				if idErr != nil {
					err = idErr
				} else if _, duplicate := context.seenPointIDs[id]; duplicate {
					err = fmt.Errorf("duplicate chunk_id in input: %s", chunk.ChunkID)
				} else {
					context.seenPointIDs[id] = struct{}{}
					delete(context.stalePointIDs, id)
					inputHash := hashText(input)
					payload, payloadErr := buildPayload(chunk, inputHash, context.pipeline, context.config.StoreEmbeddingText)
					if payloadErr != nil {
						err = payloadErr
					} else {
						prepared = append(prepared, preparedChunk{line: item.number, pointID: id, chunk: chunk, embeddingInput: input, embeddingInputHash: inputHash, payload: payload})
						context.report.ValidChunks++
					}
				}
			}
		}
		if err != nil {
			hadErrors = true
			context.report.FailedChunks++
			var id *string
			if chunk.ChunkID != "" {
				id = &chunk.ChunkID
			}
			line := item.number
			addError(context.report, &line, id, "validation", err)
		}
	}
	if len(prepared) == 0 {
		return batchOutcome{hadInputErrors: hadErrors}
	}

	readStarted := time.Now()
	existing := []StoredPoint(nil)
	var err error
	if context.collectionAvailable {
		existing, err = context.store.GetPoints(context.ctx, context.config.Collection, pointIDs(prepared))
	}
	context.report.QdrantReadMS += elapsedMS(readStarted)
	if err != nil {
		markBatchFailed(context.report, prepared, "qdrant", err)
		return batchOutcome{hadInputErrors: hadErrors, batchFailed: true}
	}
	byID := make(map[string]StoredPoint, len(existing))
	for _, point := range existing {
		byID[point.ID] = point
	}
	candidates := make([]preparedChunk, 0)
	payloadUpdates := make([]preparedChunk, 0)
	for _, item := range prepared {
		current, found := byID[item.pointID]
		switch {
		case !found:
			context.report.NewChunks++
			item.reason = "new"
			candidates = append(candidates, item)
		case context.config.ForceReembed || requiresEmbedding(current.Payload, item.embeddingInputHash, context.pipeline):
			context.report.ChangedEmbeddings++
			if context.config.ForceReembed {
				item.reason = "forced"
			} else {
				item.reason = "changed"
			}
			candidates = append(candidates, item)
		case fmt.Sprint(current.Payload["payload_hash"]) != fmt.Sprint(item.payload["payload_hash"]):
			payloadUpdates = append(payloadUpdates, item)
		default:
			context.report.SkippedUnchanged++
		}
	}
	if context.config.DryRun {
		context.report.PayloadOnlyUpdates += len(payloadUpdates)
		return batchOutcome{hadInputErrors: hadErrors}
	}

	points, err := vectorize(context, candidates)
	if err != nil {
		markBatchFailed(context.report, prepared, "embedding", err)
		return batchOutcome{hadInputErrors: hadErrors, batchFailed: true}
	}
	if len(points) > 0 {
		writeStarted := time.Now()
		err = context.store.Upsert(context.ctx, context.config.Collection, points)
		context.report.QdrantWriteMS += elapsedMS(writeStarted)
		if err != nil {
			markBatchFailed(context.report, prepared, "qdrant", err)
			return batchOutcome{hadInputErrors: hadErrors, batchFailed: true}
		}
		context.report.EmbeddedChunks += len(points)
		for _, item := range candidates {
			context.seenInputHashes[item.embeddingInputHash] = struct{}{}
		}
	}
	if len(payloadUpdates) > 0 {
		updates := make([]StoredPoint, 0, len(payloadUpdates))
		for _, item := range payloadUpdates {
			updates = append(updates, StoredPoint{ID: item.pointID, Payload: item.payload})
		}
		writeStarted := time.Now()
		err = context.store.UpdatePayload(context.ctx, context.config.Collection, updates)
		context.report.QdrantWriteMS += elapsedMS(writeStarted)
		if err != nil {
			markBatchFailed(context.report, prepared, "qdrant", err)
			return batchOutcome{hadInputErrors: hadErrors, batchFailed: true}
		}
		context.report.PayloadOnlyUpdates += len(payloadUpdates)
	}
	return batchOutcome{hadInputErrors: hadErrors}
}

func vectorize(context *batchContext, candidates []preparedChunk) ([]Point, error) {
	if len(candidates) == 0 {
		return nil, nil
	}
	groups := make(map[string][]preparedChunk)
	order := make([]string, 0)
	for _, item := range candidates {
		if _, found := groups[item.embeddingInputHash]; !found {
			order = append(order, item.embeddingInputHash)
		}
		groups[item.embeddingInputHash] = append(groups[item.embeddingInputHash], item)
	}
	vectors := make(map[string][]float64)
	toEmbed := make([]preparedChunk, 0)
	for _, hash := range order {
		group := groups[hash]
		force := false
		for _, item := range group {
			force = force || item.reason == "forced"
		}
		_, seen := context.seenInputHashes[hash]
		canReuse := !force || seen
		var vector []float64
		if canReuse {
			vector = context.cache.Get(hash)
		}
		if canReuse && vector == nil && context.collectionAvailable {
			readStarted := time.Now()
			var err error
			vector, err = context.store.FindVector(context.ctx, context.config.Collection, VectorLookup{EmbeddingInputHash: hash, Pipeline: context.pipeline})
			context.report.QdrantReadMS += elapsedMS(readStarted)
			if err != nil {
				return nil, err
			}
		}
		if vector != nil {
			if err := validateVector(vector, context.config.Embedding); err != nil {
				return nil, err
			}
			vectors[hash] = vector
			context.cache.Set(hash, vector)
			context.report.ReusedVectorsInRun += len(group)
		} else {
			toEmbed = append(toEmbed, group[0])
		}
	}
	if len(toEmbed) > 0 {
		inputs := make([]string, len(toEmbed))
		for i, item := range toEmbed {
			inputs[i] = item.embeddingInput
		}
		embedStarted := time.Now()
		computed, err := context.provider.EmbedDocuments(context.ctx, inputs)
		context.report.EmbeddingMS += elapsedMS(embedStarted)
		if err != nil {
			return nil, err
		}
		if len(computed) != len(toEmbed) {
			return nil, fmt.Errorf("embedding provider returned %d vectors for %d inputs", len(computed), len(toEmbed))
		}
		for i, item := range toEmbed {
			if err := validateVector(computed[i], context.config.Embedding); err != nil {
				return nil, err
			}
			vectors[item.embeddingInputHash] = computed[i]
			context.cache.Set(item.embeddingInputHash, computed[i])
			context.report.ReusedVectorsInRun += len(groups[item.embeddingInputHash]) - 1
		}
	}
	points := make([]Point, 0, len(candidates))
	for _, item := range candidates {
		vector := vectors[item.embeddingInputHash]
		if vector == nil {
			return nil, fmt.Errorf("no vector available for %s", item.chunk.ChunkID)
		}
		points = append(points, Point{ID: item.pointID, Vector: vector, Payload: item.payload})
	}
	return points, nil
}

func parseChunk(raw string) (ChunkRecord, error) {
	var object map[string]json.RawMessage
	var chunk ChunkRecord
	if err := json.Unmarshal([]byte(raw), &object); err != nil {
		return chunk, fmt.Errorf("invalid JSON: %w", err)
	}
	if object == nil {
		return chunk, fmt.Errorf("JSONL line must be an object")
	}
	if err := json.Unmarshal([]byte(raw), &chunk); err != nil {
		return chunk, err
	}
	for _, field := range []string{"chunk_id", "document_id", "content", "embedding_text", "content_hash", "embedding_text_hash", "metadata", "parent_document_id", "previous_chunk_id", "next_chunk_id"} {
		if _, found := object[field]; !found {
			return chunk, fmt.Errorf("%s is required", field)
		}
	}
	if strings.TrimSpace(chunk.ChunkID) == "" || strings.TrimSpace(chunk.DocumentID) == "" {
		return chunk, fmt.Errorf("chunk_id and document_id must be non-empty strings")
	}
	if strings.TrimSpace(chunk.Content) == "" || strings.TrimSpace(chunk.EmbeddingText) == "" {
		return chunk, fmt.Errorf("content and embedding_text must not be empty")
	}
	if hasForbiddenControl(chunk.Content) || hasForbiddenControl(chunk.EmbeddingText) {
		return chunk, fmt.Errorf("chunk contains forbidden control characters")
	}
	if hashText(chunk.Content) != chunk.ContentHash {
		return chunk, fmt.Errorf("content_hash does not match content")
	}
	if hashText(chunk.EmbeddingText) != chunk.EmbeddingTextHash {
		return chunk, fmt.Errorf("embedding_text_hash does not match embedding_text")
	}
	var metadata map[string]json.RawMessage
	if err := json.Unmarshal(object["metadata"], &metadata); err != nil || metadata == nil {
		return chunk, fmt.Errorf("metadata must be an object")
	}
	for _, field := range []string{"title", "source", "page_id", "parent_page_id", "root_page_id", "depth", "ancestor_ids", "breadcrumbs", "heading_path", "section_title", "language", "status", "category", "version", "date"} {
		if _, found := metadata[field]; !found {
			return chunk, fmt.Errorf("metadata.%s is required", field)
		}
	}
	if strings.TrimSpace(chunk.Metadata.Source) == "" || strings.TrimSpace(chunk.Metadata.Language) == "" || strings.TrimSpace(chunk.Metadata.Status) == "" || strings.TrimSpace(chunk.Metadata.SectionTitle) == "" {
		return chunk, fmt.Errorf("metadata source, language, status and section_title must be non-empty strings")
	}
	if chunk.Metadata.DocumentURL != nil {
		documentURL := strings.TrimSpace(*chunk.Metadata.DocumentURL)
		parsed, err := url.Parse(documentURL)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return chunk, fmt.Errorf("metadata.document_url must be an absolute HTTP(S) URL")
		}
		chunk.Metadata.DocumentURL = &documentURL
	}
	if chunk.Metadata.Depth < 0 {
		return chunk, fmt.Errorf("metadata.depth must be a non-negative integer")
	}
	if chunk.Metadata.AncestorIDs == nil || chunk.Metadata.Breadcrumbs == nil || chunk.Metadata.HeadingPath == nil {
		return chunk, fmt.Errorf("metadata ancestry fields must be string arrays")
	}
	if filepath.IsAbs(chunk.Metadata.Source) || windowsAbsolute.MatchString(chunk.Metadata.Source) || strings.Contains(chunk.Metadata.Source, "\\") {
		return chunk, fmt.Errorf("metadata.source must be a portable relative path")
	}
	if !utf8.ValidString(chunk.Content) || !utf8.ValidString(chunk.EmbeddingText) {
		return chunk, fmt.Errorf("chunk text must be valid UTF-8")
	}
	return chunk, nil
}

var windowsAbsolute = regexp.MustCompile(`^[A-Za-z]:[\\/]`)

func buildPayload(chunk ChunkRecord, inputHash string, pipe PipelineIdentity, storeText bool) (map[string]any, error) {
	payload := map[string]any{
		"chunk_id": chunk.ChunkID, "document_id": chunk.DocumentID, "parent_document_id": chunk.ParentDocumentID,
		"content": chunk.Content, "title": chunk.Metadata.Title, "source": chunk.Metadata.Source,
		"page_id": chunk.Metadata.PageID, "parent_page_id": chunk.Metadata.ParentPageID, "root_page_id": chunk.Metadata.RootPageID,
		"depth": chunk.Metadata.Depth, "ancestor_ids": chunk.Metadata.AncestorIDs, "breadcrumbs": chunk.Metadata.Breadcrumbs,
		"heading_path": chunk.Metadata.HeadingPath, "section_title": chunk.Metadata.SectionTitle,
		"language": chunk.Metadata.Language, "status": chunk.Metadata.Status, "category": chunk.Metadata.Category,
		"version": chunk.Metadata.Version, "date": chunk.Metadata.Date, "content_hash": chunk.ContentHash,
		"embedding_text_hash": chunk.EmbeddingTextHash, "embedding_input_hash": inputHash,
		"embedding_model": pipe.EmbeddingModel, "embedding_model_revision": pipe.EmbeddingModelRevision,
		"embedding_pipeline_version": pipe.EmbeddingPipelineVersion, "embedding_dtype": pipe.EmbeddingDType,
		"embedding_backend": EmbeddingBackend, "point_namespace": pipe.PointNamespace,
		"previous_chunk_id": chunk.PreviousChunkID, "next_chunk_id": chunk.NextChunkID,
		"duplicate_of_chunk_id": chunk.Metadata.DuplicateOfChunkID,
	}
	if chunk.Metadata.DocumentURL != nil {
		payload["document_url"] = *chunk.Metadata.DocumentURL
	}
	if storeText {
		payload["embedding_text"] = chunk.EmbeddingText
	}
	hash, err := stableHash(payload)
	if err != nil {
		return nil, err
	}
	payload["payload_hash"] = hash
	return payload, nil
}

func requiresEmbedding(payload map[string]any, inputHash string, pipe PipelineIdentity) bool {
	return fmt.Sprint(payload["embedding_input_hash"]) != inputHash ||
		fmt.Sprint(payload["embedding_backend"]) != EmbeddingBackend ||
		fmt.Sprint(payload["embedding_model"]) != pipe.EmbeddingModel ||
		fmt.Sprint(payload["embedding_model_revision"]) != pipe.EmbeddingModelRevision ||
		fmt.Sprint(payload["embedding_pipeline_version"]) != pipe.EmbeddingPipelineVersion
}

func newReport(config Config) Report {
	return Report{InputFile: portableRelative(config.InputFile), Collection: config.Collection,
		EmbeddingModel: config.Embedding.ModelID, EmbeddingModelRevision: config.Embedding.ModelRevision,
		EmbeddingPipelineVersion: config.Embedding.PipelineVersion, EmbeddingBackend: EmbeddingBackend,
		BatchSize: config.BatchSize, DryRun: config.DryRun, Errors: []ErrorEntry{}}
}

func writeReport(path string, report Report) error {
	report.ProviderCheckMS = roundMS(report.ProviderCheckMS)
	report.EmbeddingMS = roundMS(report.EmbeddingMS)
	report.QdrantReadMS = roundMS(report.QdrantReadMS)
	report.QdrantWriteMS = roundMS(report.QdrantWriteMS)
	report.TotalMS = roundMS(report.TotalMS)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func normalizeConfig(config *Config) {
	if config.BatchSize <= 0 {
		config.BatchSize = 16
	}
	if config.TEIURL == "" {
		config.TEIURL = "http://127.0.0.1:8080"
	}
	if config.QdrantURL == "" {
		config.QdrantURL = "http://127.0.0.1:6333"
	}
	if config.TEITimeout <= 0 {
		config.TEITimeout = 120 * time.Second
	}
	if config.QdrantTimeout <= 0 {
		config.QdrantTimeout = 30 * time.Second
	}
	if config.ReportFile == "" {
		config.ReportFile = filepath.Join(filepath.Dir(config.InputFile), "index-report."+safeCollectionName(config.Collection)+".json")
	}
}

func safeCollectionName(value string) string {
	return regexp.MustCompile(`[^A-Za-z0-9_.-]+`).ReplaceAllString(value, "_")
}
func elapsedMS(start time.Time) float64 { return float64(time.Since(start).Microseconds()) / 1000 }
func addError(report *Report, line *int, chunkID *string, stage string, err error) {
	report.Errors = append(report.Errors, ErrorEntry{Line: line, ChunkID: chunkID, Stage: stage, Message: err.Error()})
}
func markBatchFailed(report *Report, chunks []preparedChunk, stage string, err error) {
	for _, item := range chunks {
		line, id := item.line, item.chunk.ChunkID
		report.FailedChunks++
		addError(report, &line, &id, stage, err)
	}
}
func pointIDs(chunks []preparedChunk) []string {
	ids := make([]string, len(chunks))
	for i, item := range chunks {
		ids[i] = item.pointID
	}
	return ids
}
func hasForbiddenControl(value string) bool {
	for _, r := range value {
		if (r >= 0 && r <= 8) || r == 11 || r == 12 || (r >= 14 && r <= 31) {
			return true
		}
	}
	return false
}

type vectorCache struct {
	capacity int
	order    []string
	values   map[string][]float64
}

func newVectorCache(capacity int) *vectorCache {
	return &vectorCache{capacity: capacity, values: make(map[string][]float64)}
}
func (cache *vectorCache) Get(key string) []float64 {
	value := cache.values[key]
	if value == nil {
		return nil
	}
	cache.touch(key)
	return value
}
func (cache *vectorCache) Set(key string, value []float64) {
	if _, found := cache.values[key]; found {
		cache.touch(key)
	} else {
		cache.order = append(cache.order, key)
	}
	cache.values[key] = value
	if len(cache.order) > cache.capacity {
		delete(cache.values, cache.order[0])
		cache.order = cache.order[1:]
	}
}
func (cache *vectorCache) touch(key string) {
	for i, item := range cache.order {
		if item == key {
			cache.order = append(append(cache.order[:i], cache.order[i+1:]...), key)
			return
		}
	}
}
