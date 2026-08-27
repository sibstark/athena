package models

import (
	"errors"
	"time"
)

var (
	ErrTEIUnavailable    = errors.New("TEI unavailable")
	ErrInvalidEmbedding  = errors.New("invalid embedding")
	ErrQdrantUnavailable = errors.New("Qdrant unavailable")
	ErrLLMUnavailable    = errors.New("LLM unavailable")
	ErrLLMRateLimited    = errors.New("LLM rate limited")
	ErrGenerationInvalid = errors.New("generated answer failed validation")
)

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

type Filters struct {
	DocumentID       string `json:"document_id,omitempty"`
	ParentDocumentID string `json:"parent_document_id,omitempty"`
	PageID           string `json:"page_id,omitempty"`
	ParentPageID     string `json:"parent_page_id,omitempty"`
	RootPageID       string `json:"root_page_id,omitempty"`
	Language         string `json:"language,omitempty"`
	Category         string `json:"category,omitempty"`
	Status           string `json:"status,omitempty"`
}

type SearchRequest struct {
	Query          string   `json:"query"`
	Collections    []string `json:"collections"`
	TopK           *int     `json:"top_k,omitempty"`
	ScoreThreshold *float64 `json:"score_threshold,omitempty"`
	Filters        Filters  `json:"filters,omitempty"`
}

type SearchOptions struct {
	Query          string
	Collections    []string
	TopK           int
	ScoreThreshold float64
	Filters        Filters
}

type SearchResult struct {
	SourceID        string         `json:"source_id"`
	Collection      string         `json:"collection"`
	ChunkID         string         `json:"chunk_id"`
	DocumentID      string         `json:"document_id"`
	Score           float64        `json:"score"`
	Content         string         `json:"content"`
	ContentHash     string         `json:"-"`
	Title           string         `json:"title"`
	Source          string         `json:"source"`
	URL             string         `json:"url,omitempty"`
	Breadcrumbs     []string       `json:"breadcrumbs"`
	HeadingPath     []string       `json:"heading_path"`
	SectionTitle    string         `json:"section_title"`
	PreviousChunkID string         `json:"-"`
	NextChunkID     string         `json:"-"`
	Metadata        map[string]any `json:"-"`
}

type RetrievalInfo struct {
	Collections    []string `json:"collections"`
	EmbeddingModel string   `json:"embedding_model"`
	Returned       int      `json:"returned"`
	DurationMS     int64    `json:"duration_ms"`
}

type SearchResponse struct {
	RequestID string         `json:"request_id"`
	Query     string         `json:"query"`
	Results   []SearchResult `json:"results"`
	Retrieval RetrievalInfo  `json:"retrieval"`
}

type CollectionsResponse struct {
	RequestID   string   `json:"request_id"`
	Collections []string `json:"collections"`
}

type AnswerSource struct {
	SourceID     string   `json:"source_id"`
	Collection   string   `json:"collection"`
	ChunkID      string   `json:"chunk_id"`
	DocumentID   string   `json:"document_id"`
	Score        float64  `json:"score"`
	Title        string   `json:"title"`
	Source       string   `json:"source"`
	URL          string   `json:"url,omitempty"`
	Breadcrumbs  []string `json:"breadcrumbs"`
	HeadingPath  []string `json:"heading_path"`
	SectionTitle string   `json:"section_title"`
}

type GenerationInfo struct {
	Provider     string `json:"provider"`
	Model        string `json:"model"`
	InputTokens  *int   `json:"input_tokens,omitempty"`
	OutputTokens *int   `json:"output_tokens,omitempty"`
	DurationMS   int64  `json:"duration_ms"`
}

type AnswerResponse struct {
	RequestID  string          `json:"request_id"`
	Status     string          `json:"status"`
	Answer     string          `json:"answer"`
	Sources    []AnswerSource  `json:"sources"`
	Generation *GenerationInfo `json:"generation,omitempty"`
}

type LLMResult struct {
	Content      string
	InputTokens  *int
	OutputTokens *int
	Duration     time.Duration
}

type APIError struct {
	RequestID string       `json:"request_id"`
	Error     APIErrorBody `json:"error"`
}

type APIErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (result SearchResult) SourceMetadata() AnswerSource {
	return AnswerSource{
		SourceID: result.SourceID, Collection: result.Collection, ChunkID: result.ChunkID, DocumentID: result.DocumentID,
		Score: result.Score, Title: result.Title, Source: result.Source, URL: result.URL,
		Breadcrumbs: result.Breadcrumbs, HeadingPath: result.HeadingPath, SectionTitle: result.SectionTitle,
	}
}
