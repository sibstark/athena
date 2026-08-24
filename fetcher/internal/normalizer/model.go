package normalizer

import "time"

const (
	SourceFormatVersion = 1
	FormatVersion       = 1
)

type Status string

const (
	StatusComplete             Status = "complete"
	StatusCompleteWithWarnings Status = "complete_with_warnings"
	StatusFailed               Status = "failed"
)

type DocumentType string

const (
	DocumentMarkdown     DocumentType = "markdown"
	DocumentExternalLink DocumentType = "external_link"
	DocumentShortNote    DocumentType = "short_note"
	DocumentEmpty        DocumentType = "empty"
)

type SourceManifest struct {
	FormatVersion int               `json:"formatVersion"`
	ExportedAt    string            `json:"exportedAt"`
	Source        Source            `json:"source"`
	AssetMode     string            `json:"assetMode"`
	Workspaces    []SourceWorkspace `json:"workspaces"`
}

type Source struct {
	Provider string `json:"provider"`
	Portal   string `json:"portal"`
	URL      string `json:"url"`
}

type SourceWorkspace struct {
	ID               int              `json:"id"`
	Name             string           `json:"name"`
	Position         int              `json:"position"`
	PolicyLevel      string           `json:"policyLevel"`
	Path             string           `json:"path"`
	URL              string           `json:"url"`
	QdrantCollection string           `json:"qdrantCollection"`
	DocumentCount    int              `json:"documentCount"`
	Documents        []SourceDocument `json:"documents"`
}

type SourceDocument struct {
	ID          int              `json:"id"`
	WorkspaceID int              `json:"workspaceId"`
	ParentID    *int             `json:"parentId"`
	Title       string           `json:"title"`
	Position    int              `json:"position"`
	CreatedBy   int              `json:"createdBy"`
	UpdatedBy   int              `json:"updatedBy"`
	CreatedAt   string           `json:"createdAt"`
	UpdatedAt   string           `json:"updatedAt"`
	Path        string           `json:"path"`
	ContentPath string           `json:"contentPath"`
	URL         string           `json:"url"`
	Children    []SourceDocument `json:"children"`
}

type NormalizedAsset struct {
	Type string `json:"type"`
	Path string `json:"path"`
	Alt  string `json:"alt"`
}

type NormalizedMetadata struct {
	Title              *string           `json:"title"`
	Source             string            `json:"source"`
	PageID             *string           `json:"page_id"`
	ParentPageID       *string           `json:"parent_page_id"`
	RootPageID         *string           `json:"root_page_id"`
	Depth              int               `json:"depth"`
	AncestorIDs        []string          `json:"ancestor_ids"`
	Breadcrumbs        []string          `json:"breadcrumbs"`
	ContextText        string            `json:"context_text"`
	DocumentType       DocumentType      `json:"document_type"`
	Indexable          bool              `json:"indexable"`
	ExternalURLs       []string          `json:"external_urls"`
	Assets             []NormalizedAsset `json:"assets"`
	CharacterCount     int               `json:"character_count"`
	WordCount          int               `json:"word_count"`
	HeadingCount       int               `json:"heading_count"`
	ExternalURLCount   int               `json:"external_url_count"`
	AssetCount         int               `json:"asset_count"`
	DuplicateOf        string            `json:"duplicate_of,omitempty"`
	Version            *string           `json:"version"`
	Date               *string           `json:"date"`
	Category           *string           `json:"category"`
	Status             string            `json:"status"`
	Language           string            `json:"language"`
	WorkspaceID        int               `json:"workspace_id"`
	Position           int               `json:"position"`
	CreatedBy          int               `json:"created_by"`
	UpdatedBy          int               `json:"updated_by"`
	CreatedAt          string            `json:"created_at"`
	UpdatedAt          string            `json:"updated_at"`
	WorkspaceURL       string            `json:"workspace_url"`
	DocumentURL        string            `json:"document_url"`
	Suspicious         bool              `json:"suspicious"`
	SuspiciousReasons  []string          `json:"suspicious_reasons"`
	MissingTOCSections []string          `json:"missing_toc_sections,omitempty"`
}

type NormalizedDocument struct {
	DocumentID       string             `json:"document_id"`
	ParentDocumentID *string            `json:"parent_document_id"`
	Content          string             `json:"content"`
	Metadata         NormalizedMetadata `json:"metadata"`
	ContentHash      string             `json:"content_hash"`
}

type DocumentTreeNode struct {
	NodeID       string  `json:"node_id"`
	ParentNodeID *string `json:"parent_node_id"`
	PageID       *string `json:"page_id"`
	Title        string  `json:"title"`
	Depth        int     `json:"depth"`
	HasDocument  bool    `json:"has_document"`
	Source       *string `json:"source"`
}

type DocumentTree struct {
	Nodes []DocumentTreeNode `json:"nodes"`
}

type Issue struct {
	Source     string `json:"source"`
	DocumentID string `json:"document_id,omitempty"`
	Code       string `json:"code,omitempty"`
	Message    string `json:"message"`
}

type ControlCharacterWarning struct {
	Source     string `json:"source"`
	DocumentID string `json:"document_id"`
	Character  string `json:"character"`
	Count      int    `json:"count"`
}

type WorkspaceReport struct {
	FormatVersion                int                       `json:"format_version"`
	Status                       Status                    `json:"status"`
	WorkspaceID                  int                       `json:"workspace_id"`
	WorkspaceName                string                    `json:"workspace_name"`
	FoundFiles                   int                       `json:"found_files"`
	TreeNodes                    int                       `json:"tree_nodes"`
	VirtualNodes                 int                       `json:"virtual_nodes"`
	ProcessedDocuments           int                       `json:"processed_documents"`
	SkippedDocuments             int                       `json:"skipped_documents"`
	RootDocuments                int                       `json:"root_documents"`
	MaxDepth                     int                       `json:"max_depth"`
	EmptyFiles                   int                       `json:"empty_files"`
	ShortDocuments               int                       `json:"short_documents"`
	ExternalLinkDocuments        int                       `json:"external_link_documents"`
	Images                       int                       `json:"images"`
	ExternalLinks                int                       `json:"external_links"`
	DuplicateFiles               int                       `json:"duplicate_files"`
	ExactDuplicates              int                       `json:"exact_duplicates"`
	FilesWithoutPageID           int                       `json:"files_without_page_id"`
	FilesWithoutTitle            int                       `json:"files_without_title"`
	SuspiciousDocuments          int                       `json:"suspicious_documents"`
	MissingAssets                int                       `json:"missing_assets"`
	BrokenInternalLinks          int                       `json:"broken_internal_links"`
	FrontMatterMismatches        int                       `json:"front_matter_mismatches"`
	TreeErrors                   int                       `json:"tree_errors"`
	ProcessingErrors             int                       `json:"processing_errors"`
	Errors                       int                       `json:"errors"`
	IntegrityWarnings            int                       `json:"integrity_warnings"`
	ControlCharacterReplacements int                       `json:"control_character_replacements"`
	ControlCharacterWarnings     []ControlCharacterWarning `json:"control_character_warnings"`
	SuspiciousFiles              []Issue                   `json:"suspicious_files"`
	IntegrityWarningFiles        []Issue                   `json:"integrity_warning_files"`
	TreeErrorFiles               []Issue                   `json:"tree_error_files"`
	ErrorFiles                   []Issue                   `json:"error_files"`
}

type WorkspaceOutput struct {
	ID                  int    `json:"id"`
	Name                string `json:"name"`
	Path                string `json:"path"`
	QdrantCollection    string `json:"qdrantCollection"`
	NormalizedPath      string `json:"normalizedPath,omitempty"`
	TreePath            string `json:"treePath,omitempty"`
	ReportPath          string `json:"reportPath"`
	Status              Status `json:"status"`
	ProcessedDocuments  int    `json:"processedDocuments"`
	SuspiciousDocuments int    `json:"suspiciousDocuments"`
	Errors              int    `json:"errors"`
	Warnings            int    `json:"warnings"`
}

type Manifest struct {
	FormatVersion int               `json:"formatVersion"`
	GeneratedAt   string            `json:"generatedAt"`
	SourceRun     string            `json:"sourceRun"`
	Source        Source            `json:"source"`
	Workspaces    []WorkspaceOutput `json:"workspaces"`
}

type Report struct {
	FormatVersion       int               `json:"formatVersion"`
	Status              Status            `json:"status"`
	SourceRun           string            `json:"sourceRun"`
	StartedAt           string            `json:"startedAt"`
	FinishedAt          string            `json:"finishedAt"`
	DurationMS          int64             `json:"durationMs"`
	WorkspaceCount      int               `json:"workspaceCount"`
	ProcessedWorkspaces int               `json:"processedWorkspaces"`
	DocumentCount       int               `json:"documentCount"`
	ProcessedDocuments  int               `json:"processedDocuments"`
	SuspiciousDocuments int               `json:"suspiciousDocuments"`
	Errors              int               `json:"errors"`
	Warnings            int               `json:"warnings"`
	Workspaces          []WorkspaceOutput `json:"workspaces"`
}

type Options struct {
	RunDir             string
	OutputDir          string
	Include            map[int]struct{}
	Exclude            map[int]struct{}
	ShortNoteThreshold int
	Force              bool
	Now                func() time.Time
	Progress           func(string)
}

type Result struct {
	OutputDir           string
	Status              Status
	WorkspaceCount      int
	ProcessedDocuments  int
	SuspiciousDocuments int
	Errors              int
	Warnings            int
}
