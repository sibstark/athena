package retrieval

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/sibstark/athena/rag-api/internal/models"
	"github.com/sibstark/athena/rag-api/internal/qdrant"
)

type Embedder interface {
	EmbedQuery(context.Context, string) ([]float64, error)
}
type Searcher interface {
	Search(context.Context, qdrant.SearchParams) ([]qdrant.Hit, error)
	Collection() string
}

type Service struct {
	embedder Embedder
	searcher Searcher
	maxTopK  int
}

func New(embedder Embedder, searcher Searcher, maxTopK int) *Service {
	return &Service{embedder: embedder, searcher: searcher, maxTopK: maxTopK}
}
func (s *Service) Collection() string { return s.searcher.Collection() }

func (s *Service) Search(ctx context.Context, options models.SearchOptions) ([]models.SearchResult, time.Duration, error) {
	start := time.Now()
	vector, err := s.embedder.EmbedQuery(ctx, options.Query)
	if err != nil {
		return nil, time.Since(start), err
	}
	candidateLimit := options.TopK * 3
	if maximum := s.maxTopK * 3; candidateLimit > maximum {
		candidateLimit = maximum
	}
	hits, err := s.searcher.Search(ctx, qdrant.SearchParams{Vector: vector, Collections: options.Collections, Limit: candidateLimit, ScoreThreshold: options.ScoreThreshold, Filters: options.Filters})
	if err != nil {
		return nil, time.Since(start), err
	}
	results := deduplicate(hits, options.TopK)
	for index := range results {
		results[index].SourceID = fmt.Sprintf("S%d", index+1)
	}
	return results, time.Since(start), nil
}

func deduplicate(hits []qdrant.Hit, limit int) []models.SearchResult {
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	seenHashes, seenContent := map[string]struct{}{}, map[string]struct{}{}
	results := make([]models.SearchResult, 0, min(limit, len(hits)))
	for _, hit := range hits {
		result, ok := resultFromHit(hit)
		if !ok {
			continue
		}
		if result.ContentHash != "" {
			if _, exists := seenHashes[result.ContentHash]; exists {
				continue
			}
		}
		normalized := normalizedContentHash(result.Content)
		if _, exists := seenContent[normalized]; exists {
			continue
		}
		if result.ContentHash != "" {
			seenHashes[result.ContentHash] = struct{}{}
		}
		seenContent[normalized] = struct{}{}
		results = append(results, result)
		if len(results) == limit {
			break
		}
	}
	return results
}

func resultFromHit(hit qdrant.Hit) (models.SearchResult, bool) {
	p := hit.Payload
	chunkID, _ := p["chunk_id"].(string)
	documentID, _ := p["document_id"].(string)
	content, _ := p["content"].(string)
	if chunkID == "" || documentID == "" || strings.TrimSpace(content) == "" {
		return models.SearchResult{}, false
	}
	return models.SearchResult{
		Collection: hit.Collection, ChunkID: chunkID, DocumentID: documentID, Score: hit.Score, Content: content,
		ContentHash: stringValue(p, "content_hash"), Title: stringValue(p, "title"), Source: stringValue(p, "source"),
		Breadcrumbs: stringSlice(p["breadcrumbs"]), HeadingPath: stringSlice(p["heading_path"]), SectionTitle: stringValue(p, "section_title"),
		PreviousChunkID: stringValue(p, "previous_chunk_id"), NextChunkID: stringValue(p, "next_chunk_id"), Metadata: p,
	}, true
}

func normalizedContentHash(content string) string {
	normalized := strings.Join(strings.Fields(strings.ReplaceAll(strings.ReplaceAll(content, "\r\n", "\n"), "\r", "\n")), " ")
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:])
}

func stringValue(record map[string]any, key string) string {
	value, _ := record[key].(string)
	return value
}
func stringSlice(value any) []string {
	raw, ok := value.([]any)
	if !ok {
		if typed, ok := value.([]string); ok {
			return append([]string(nil), typed...)
		}
		return []string{}
	}
	result := make([]string, 0, len(raw))
	for _, item := range raw {
		if text, ok := item.(string); ok {
			result = append(result, text)
		}
	}
	return result
}
