package qdrant

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sibstark/athena/rag-api/internal/models"
)

type Observer func(time.Duration, error)

type Client struct {
	baseURL    string
	apiKey     string
	collection string
	http       *http.Client
	observe    Observer
}

type Snapshot struct {
	Status      string
	PointsCount int64
	Dimension   int
	Distance    string
	Metadata    map[string]any
}

type Hit struct {
	ID         string
	Collection string
	Score      float64
	Payload    map[string]any
}

type SearchParams struct {
	Vector         []float64
	Collections    []string
	Limit          int
	ScoreThreshold float64
	Filters        models.Filters
}

func New(baseURL, apiKey, collection string, timeout time.Duration, observer Observer) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, collection: collection, http: &http.Client{Timeout: timeout}, observe: observer}
}

func (c *Client) Collection() string { return c.collection }

func (c *Client) ListCollections(ctx context.Context) ([]string, error) {
	start := time.Now()
	collections, err := c.listCollections(ctx)
	c.notify(time.Since(start), err)
	return collections, err
}

func (c *Client) listCollections(ctx context.Context) ([]string, error) {
	var response struct {
		Result *struct {
			Collections []struct {
				Name string `json:"name"`
			} `json:"collections"`
		} `json:"result"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/collections", nil, &response); err != nil {
		return nil, fmt.Errorf("%w: %v", models.ErrQdrantUnavailable, err)
	}
	if response.Result == nil || response.Result.Collections == nil {
		return nil, fmt.Errorf("%w: Qdrant response has no result.collections array", models.ErrQdrantUnavailable)
	}
	seen := make(map[string]struct{}, len(response.Result.Collections))
	collections := make([]string, 0, len(response.Result.Collections))
	for _, item := range response.Result.Collections {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			return nil, fmt.Errorf("%w: Qdrant returned an empty collection name", models.ErrQdrantUnavailable)
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		collections = append(collections, name)
	}
	sort.Strings(collections)
	return collections, nil
}

func (c *Client) Ready(ctx context.Context) error {
	start := time.Now()
	snapshot, err := c.Snapshot(ctx)
	c.notify(time.Since(start), err)
	if err != nil {
		return err
	}
	if snapshot.Dimension != models.EmbeddingDimension || !strings.EqualFold(snapshot.Distance, "Cosine") {
		return fmt.Errorf("%w: incompatible collection vectors %d/%s", models.ErrQdrantUnavailable, snapshot.Dimension, snapshot.Distance)
	}
	if !strings.EqualFold(snapshot.Status, "green") || snapshot.PointsCount <= 0 {
		return fmt.Errorf("%w: collection is not ready (status=%s, points=%d)", models.ErrQdrantUnavailable, snapshot.Status, snapshot.PointsCount)
	}
	if model, _ := snapshot.Metadata["embedding_model"].(string); model != models.EmbeddingModel {
		return fmt.Errorf("%w: incompatible embedding_model %q", models.ErrQdrantUnavailable, model)
	}
	if revision, _ := snapshot.Metadata["embedding_model_revision"].(string); revision != models.EmbeddingRevision {
		return fmt.Errorf("%w: incompatible embedding revision %q", models.ErrQdrantUnavailable, revision)
	}
	return nil
}

func (c *Client) Snapshot(ctx context.Context) (Snapshot, error) {
	var response struct {
		Result struct {
			Status      string `json:"status"`
			PointsCount int64  `json:"points_count"`
			Config      struct {
				Params struct {
					Vectors struct {
						Size     int    `json:"size"`
						Distance string `json:"distance"`
					} `json:"vectors"`
				} `json:"params"`
				Metadata map[string]any `json:"metadata"`
			} `json:"config"`
		} `json:"result"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/collections/"+url.PathEscape(c.collection), nil, &response); err != nil {
		return Snapshot{}, fmt.Errorf("%w: %v", models.ErrQdrantUnavailable, err)
	}
	return Snapshot{Status: response.Result.Status, PointsCount: response.Result.PointsCount, Dimension: response.Result.Config.Params.Vectors.Size, Distance: response.Result.Config.Params.Vectors.Distance, Metadata: response.Result.Config.Metadata}, nil
}

func (c *Client) Search(ctx context.Context, params SearchParams) ([]Hit, error) {
	start := time.Now()
	hits, err := c.search(ctx, params)
	c.notify(time.Since(start), err)
	return hits, err
}

func (c *Client) search(ctx context.Context, params SearchParams) ([]Hit, error) {
	collections := params.Collections
	if len(collections) == 0 {
		if c.collection == "" {
			return nil, fmt.Errorf("qdrant collection is required")
		}
		collections = []string{c.collection}
	}

	results := make([][]Hit, len(collections))
	errors := make([]error, len(collections))
	searchCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var group sync.WaitGroup
	for index, collection := range collections {
		collection := strings.TrimSpace(collection)
		if collection == "" {
			return nil, fmt.Errorf("qdrant collection must not be empty")
		}
		group.Add(1)
		go func(index int, collection string) {
			defer group.Done()
			results[index], errors[index] = c.searchCollection(searchCtx, collection, params)
			if errors[index] != nil {
				cancel()
			}
		}(index, collection)
	}
	group.Wait()
	for _, err := range errors {
		if err != nil {
			return nil, err
		}
	}

	hits := make([]Hit, 0)
	for _, collectionHits := range results {
		hits = append(hits, collectionHits...)
	}
	return hits, nil
}

func (c *Client) searchCollection(ctx context.Context, collection string, params SearchParams) ([]Hit, error) {
	body := map[string]any{"vector": params.Vector, "limit": params.Limit, "score_threshold": params.ScoreThreshold, "with_payload": true, "with_vector": false}
	if filter := buildFilter(params.Filters); filter != nil {
		body["filter"] = filter
	}
	var response struct {
		Result []struct {
			ID      any            `json:"id"`
			Score   float64        `json:"score"`
			Payload map[string]any `json:"payload"`
		} `json:"result"`
	}
	path := "/collections/" + url.PathEscape(collection) + "/points/search"
	if err := c.doJSON(ctx, http.MethodPost, path, body, &response); err != nil {
		return nil, fmt.Errorf("%w: %v", models.ErrQdrantUnavailable, err)
	}
	hits := make([]Hit, 0, len(response.Result))
	for _, item := range response.Result {
		hits = append(hits, Hit{ID: fmt.Sprint(item.ID), Collection: collection, Score: item.Score, Payload: item.Payload})
	}
	return hits, nil
}

func buildFilter(filters models.Filters) map[string]any {
	values := []struct{ key, value string }{
		{"document_id", filters.DocumentID}, {"parent_document_id", filters.ParentDocumentID},
		{"page_id", filters.PageID}, {"parent_page_id", filters.ParentPageID}, {"root_page_id", filters.RootPageID},
		{"language", filters.Language}, {"category", filters.Category}, {"status", filters.Status},
	}
	must := make([]map[string]any, 0, len(values))
	for _, item := range values {
		if item.value != "" {
			must = append(must, map[string]any{"key": item.key, "match": map[string]any{"value": item.value}})
		}
	}
	if len(must) == 0 {
		return nil
	}
	return map[string]any{"must": must}
}

func (c *Client) doJSON(ctx context.Context, method, path string, body, target any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.apiKey != "" {
		req.Header.Set("api-key", c.apiKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("Qdrant returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(target); err != nil {
		return fmt.Errorf("invalid Qdrant response: %w", err)
	}
	return nil
}

func (c *Client) notify(duration time.Duration, err error) {
	if c.observe != nil {
		c.observe(duration, err)
	}
}
