package snapshot

import "github.com/sibstark/athena/fetcher/internal/bitrix"

const FormatVersion = 1

type Status string

const (
	StatusComplete             Status = "complete"
	StatusCompleteWithWarnings Status = "complete_with_warnings"
	StatusFailed               Status = "failed"
)

type Manifest struct {
	FormatVersion int                 `json:"formatVersion"`
	ExportedAt    string              `json:"exportedAt"`
	Source        Source              `json:"source"`
	AssetMode     string              `json:"assetMode"`
	Workspaces    []WorkspaceManifest `json:"workspaces"`
}

type Source struct {
	Provider string `json:"provider"`
	Portal   string `json:"portal"`
	URL      string `json:"url"`
}

type WorkspaceManifest struct {
	ID               int                `json:"id"`
	Name             string             `json:"name"`
	Position         int                `json:"position"`
	PolicyLevel      string             `json:"policyLevel"`
	CreatedBy        int                `json:"createdBy"`
	UpdatedBy        int                `json:"updatedBy"`
	CreatedAt        string             `json:"createdAt"`
	UpdatedAt        string             `json:"updatedAt"`
	Path             string             `json:"path"`
	URL              string             `json:"url"`
	QdrantCollection string             `json:"qdrantCollection"`
	DocumentCount    int                `json:"documentCount"`
	Documents        []DocumentManifest `json:"documents"`
}

type DocumentManifest struct {
	ID          int                `json:"id"`
	WorkspaceID int                `json:"workspaceId"`
	ParentID    *int               `json:"parentId"`
	Title       string             `json:"title"`
	Position    int                `json:"position"`
	CreatedBy   int                `json:"createdBy"`
	UpdatedBy   int                `json:"updatedBy"`
	CreatedAt   string             `json:"createdAt"`
	UpdatedAt   string             `json:"updatedAt"`
	Path        string             `json:"path"`
	ContentPath string             `json:"contentPath"`
	URL         string             `json:"url"`
	Children    []DocumentManifest `json:"children"`
}

type AssetRecord struct {
	WorkspaceID int    `json:"workspaceId"`
	SourceURL   string `json:"sourceUrl"`
	Status      string `json:"status"`
	LocalPath   string `json:"localPath,omitempty"`
	ContentType string `json:"contentType,omitempty"`
	Bytes       int64  `json:"bytes,omitempty"`
	Error       string `json:"error,omitempty"`
}

type Report struct {
	FormatVersion  int          `json:"formatVersion"`
	RunID          string       `json:"runId"`
	Status         Status       `json:"status"`
	StartedAt      string       `json:"startedAt"`
	FinishedAt     string       `json:"finishedAt"`
	DurationMS     int64        `json:"durationMs"`
	WorkspaceCount int          `json:"workspaceCount"`
	DocumentCount  int          `json:"documentCount"`
	Assets         AssetsReport `json:"assets"`
	Error          *Failure     `json:"error,omitempty"`
}

type AssetsReport struct {
	Mode       string        `json:"mode"`
	Downloaded int           `json:"downloaded"`
	Failed     int           `json:"failed"`
	Items      []AssetRecord `json:"items"`
}

type Failure struct {
	Message string `json:"message"`
}

type Registry struct {
	FormatVersion int        `json:"formatVersion"`
	Runs          []RunEntry `json:"runs"`
}

type RunEntry struct {
	ID             string `json:"id"`
	Path           string `json:"path"`
	Status         Status `json:"status"`
	StartedAt      string `json:"startedAt"`
	FinishedAt     string `json:"finishedAt"`
	ManifestPath   string `json:"manifestPath,omitempty"`
	ReportPath     string `json:"reportPath"`
	WorkspaceCount int    `json:"workspaceCount"`
	DocumentCount  int    `json:"documentCount"`
}

type plannedDocument struct {
	Node     bitrix.TreeNode
	Manifest *DocumentManifest
}

type plannedWorkspace struct {
	Workspace bitrix.Workspace
	Manifest  WorkspaceManifest
	Documents []plannedDocument
}

type Summary struct {
	RunID            string
	RunPath          string
	Status           Status
	WorkspaceCount   int
	DocumentCount    int
	AssetsDownloaded int
	AssetsFailed     int
}
