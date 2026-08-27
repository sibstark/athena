package indexer

import (
	"context"
	"time"
)

const (
	EmbeddingBackend = "tei"
)

var payloadIndexes = []string{
	"document_id", "parent_document_id", "page_id", "parent_page_id", "root_page_id",
	"language", "category", "status", "content_hash", "embedding_input_hash",
	"embedding_backend", "point_namespace", "embedding_model",
	"embedding_model_revision", "embedding_pipeline_version",
}

type Config struct {
	InputFile          string
	ReportFile         string
	Collection         string
	QdrantURL          string
	QdrantAPIKey       string
	QdrantTimeout      time.Duration
	TEIURL             string
	TEITimeout         time.Duration
	BatchSize          int
	Embedding          EmbeddingConfig
	StoreEmbeddingText bool
	DryRun             bool
	ForceReembed       bool
	Prune              bool
	ContinueOnError    bool
	LogLevel           string
	Progress           func(string)
}

type RunConfig struct {
	RunDir             string
	ReportFile         string
	QdrantURL          string
	QdrantAPIKey       string
	QdrantTimeout      time.Duration
	TEIURL             string
	TEITimeout         time.Duration
	BatchSize          int
	Embedding          EmbeddingConfig
	StoreEmbeddingText bool
	DryRun             bool
	ForceReembed       bool
	ContinueOnError    bool
	LogLevel           string
	Progress           func(string)
}

type EmbeddingConfig struct {
	ModelID               string
	ModelRevision         string
	Dimension             int
	DType                 string
	Pooling               string
	Normalized            bool
	DocumentPrefix        string
	QueryPrefix           string
	MaxInputTokens        int
	AutoTruncate          bool
	PipelineVersion       string
	PointNamespace        string
	TokenizerSHA256       string
	TokenizerConfigSHA256 string
	CacheDir              string
	VectorCacheSize       int
}

type ChunkMetadata struct {
	Title              *string  `json:"title"`
	Source             string   `json:"source"`
	DocumentURL        *string  `json:"document_url,omitempty"`
	PageID             *string  `json:"page_id"`
	ParentPageID       *string  `json:"parent_page_id"`
	RootPageID         *string  `json:"root_page_id"`
	Depth              int      `json:"depth"`
	AncestorIDs        []string `json:"ancestor_ids"`
	Breadcrumbs        []string `json:"breadcrumbs"`
	HeadingPath        []string `json:"heading_path"`
	SectionTitle       string   `json:"section_title"`
	Language           string   `json:"language"`
	Status             string   `json:"status"`
	Category           *string  `json:"category"`
	Version            *string  `json:"version"`
	Date               *string  `json:"date"`
	DuplicateOfChunkID *string  `json:"duplicate_of_chunk_id"`
}

type ChunkRecord struct {
	ChunkID           string        `json:"chunk_id"`
	DocumentID        string        `json:"document_id"`
	ParentDocumentID  *string       `json:"parent_document_id"`
	Content           string        `json:"content"`
	EmbeddingText     string        `json:"embedding_text"`
	ContentHash       string        `json:"content_hash"`
	EmbeddingTextHash string        `json:"embedding_text_hash"`
	Metadata          ChunkMetadata `json:"metadata"`
	PreviousChunkID   *string       `json:"previous_chunk_id"`
	NextChunkID       *string       `json:"next_chunk_id"`
}

type PipelineIdentity struct {
	EmbeddingModel           string `json:"embedding_model"`
	EmbeddingModelRevision   string `json:"embedding_model_revision"`
	EmbeddingDimension       int    `json:"embedding_dimension"`
	EmbeddingDType           string `json:"embedding_dtype"`
	EmbeddingPooling         string `json:"embedding_pooling"`
	EmbeddingNormalized      bool   `json:"embedding_normalized"`
	DocumentPrefix           string `json:"document_prefix"`
	EmbeddingPipelineVersion string `json:"embedding_pipeline_version"`
	PointNamespace           string `json:"point_namespace"`
}

type Point struct {
	ID      string
	Vector  []float64
	Payload map[string]any
}

type StoredPoint struct {
	ID      string
	Payload map[string]any
}

type CollectionState struct {
	Exists      bool
	Created     bool
	WouldCreate bool
}

type VectorLookup struct {
	EmbeddingInputHash string
	Pipeline           PipelineIdentity
}

type Provider interface {
	Ready(context.Context) error
	CountTokens(context.Context, string) (int, error)
	EmbedDocuments(context.Context, []string) ([][]float64, error)
}

type Store interface {
	Ping(context.Context) error
	EnsureCollection(context.Context, string, int, string, PipelineIdentity, bool) (CollectionState, error)
	EnsurePayloadIndexes(context.Context, string, []string, bool) error
	GetPoints(context.Context, string, []string) ([]StoredPoint, error)
	FindVector(context.Context, string, VectorLookup) ([]float64, error)
	Upsert(context.Context, string, []Point) error
	UpdatePayload(context.Context, string, []StoredPoint) error
	DeletePoints(context.Context, string, []string) error
	ListManagedPointIDs(context.Context, string, PipelineIdentity) ([]string, error)
}

type ErrorEntry struct {
	Line    *int    `json:"line"`
	ChunkID *string `json:"chunk_id"`
	Stage   string  `json:"stage"`
	Message string  `json:"message"`
}

type Report struct {
	InputFile                string       `json:"input_file"`
	Collection               string       `json:"collection"`
	EmbeddingModel           string       `json:"embedding_model"`
	EmbeddingModelRevision   string       `json:"embedding_model_revision"`
	EmbeddingPipelineVersion string       `json:"embedding_pipeline_version"`
	EmbeddingBackend         string       `json:"embedding_backend"`
	BatchSize                int          `json:"batch_size"`
	DryRun                   bool         `json:"dry_run"`
	InputChunks              int          `json:"input_chunks"`
	ValidChunks              int          `json:"valid_chunks"`
	NewChunks                int          `json:"new_chunks"`
	ChangedEmbeddings        int          `json:"changed_embeddings"`
	EmbeddedChunks           int          `json:"embedded_chunks"`
	ReusedVectorsInRun       int          `json:"reused_vectors_in_run"`
	PayloadOnlyUpdates       int          `json:"payload_only_updates"`
	SkippedUnchanged         int          `json:"skipped_unchanged"`
	StaleChunksFound         int          `json:"stale_chunks_found"`
	DeletedChunks            int          `json:"deleted_chunks"`
	FailedChunks             int          `json:"failed_chunks"`
	ProviderCheckMS          float64      `json:"provider_check_ms"`
	EmbeddingMS              float64      `json:"embedding_ms"`
	QdrantReadMS             float64      `json:"qdrant_read_ms"`
	QdrantWriteMS            float64      `json:"qdrant_write_ms"`
	TotalMS                  float64      `json:"total_ms"`
	PruneSkippedDueToErrors  bool         `json:"prune_skipped_due_to_errors"`
	Errors                   []ErrorEntry `json:"errors"`
}

type Dependencies struct {
	Provider Provider
	Store    Store
}

type RunWorkspaceReport struct {
	WorkspaceID          int     `json:"workspace_id"`
	WorkspaceName        string  `json:"workspace_name"`
	WorkspacePath        string  `json:"workspace_path"`
	Collection           string  `json:"collection"`
	Status               string  `json:"status"`
	SourceStatus         string  `json:"source_status"`
	SourceDocumentErrors int     `json:"source_document_errors"`
	ChunkReportFile      *string `json:"chunk_report_file"`
	InputFile            *string `json:"input_file"`
	ReportFile           *string `json:"report_file"`
	InputChunks          int     `json:"input_chunks"`
	EmbeddedChunks       int     `json:"embedded_chunks"`
	PayloadOnlyUpdates   int     `json:"payload_only_updates"`
	SkippedUnchanged     int     `json:"skipped_unchanged"`
	StaleChunksFound     int     `json:"stale_chunks_found"`
	DeletedChunks        int     `json:"deleted_chunks"`
	FailedChunks         int     `json:"failed_chunks"`
	Error                *string `json:"error"`
}

type RunReport struct {
	FormatVersion        int                  `json:"format_version"`
	Status               string               `json:"status"`
	RunDir               string               `json:"run_dir"`
	SourceRun            string               `json:"source_run"`
	StartedAt            string               `json:"started_at"`
	FinishedAt           string               `json:"finished_at"`
	DurationMS           int64                `json:"duration_ms"`
	DryRun               bool                 `json:"dry_run"`
	Prune                bool                 `json:"prune"`
	WorkspaceCount       int                  `json:"workspace_count"`
	ProcessedWorkspaces  int                  `json:"processed_workspaces"`
	SuccessfulWorkspaces int                  `json:"successful_workspaces"`
	WarningWorkspaces    int                  `json:"warning_workspaces"`
	FailedWorkspaces     int                  `json:"failed_workspaces"`
	SkippedWorkspaces    int                  `json:"skipped_workspaces"`
	Totals               ReportTotals         `json:"totals"`
	Workspaces           []RunWorkspaceReport `json:"workspaces"`
}

type ReportTotals struct {
	InputChunks        int `json:"input_chunks"`
	ValidChunks        int `json:"valid_chunks"`
	NewChunks          int `json:"new_chunks"`
	ChangedEmbeddings  int `json:"changed_embeddings"`
	EmbeddedChunks     int `json:"embedded_chunks"`
	ReusedVectorsInRun int `json:"reused_vectors_in_run"`
	PayloadOnlyUpdates int `json:"payload_only_updates"`
	SkippedUnchanged   int `json:"skipped_unchanged"`
	StaleChunksFound   int `json:"stale_chunks_found"`
	DeletedChunks      int `json:"deleted_chunks"`
	FailedChunks       int `json:"failed_chunks"`
}
