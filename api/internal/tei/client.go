package tei

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/sibstark/athena/api/internal/models"
)

type Observer func(time.Duration, error)

type Client struct {
	baseURL   string
	http      *http.Client
	embedding models.EmbeddingConfig
	observe   Observer
	mu        sync.RWMutex
	info      *Info
}

type Info struct {
	ModelID        string `json:"model_id"`
	ModelSHA       string `json:"model_sha"`
	MaxInputLength int    `json:"max_input_length"`
	AutoTruncate   bool   `json:"auto_truncate"`
}

var knownPrefix = regexp.MustCompile(`^(?:passage|query):\s*`)

func New(baseURL string, timeout time.Duration, embedding models.EmbeddingConfig, observer Observer) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), http: &http.Client{Timeout: timeout}, embedding: embedding, observe: observer}
}

func PrefixQuery(text, prefix string) string {
	return prefix + knownPrefix.ReplaceAllString(text, "")
}

func (c *Client) Ready(ctx context.Context) error {
	start := time.Now()
	err := c.ready(ctx)
	c.notify(time.Since(start), err)
	return err
}

func (c *Client) ready(ctx context.Context) error {
	if err := c.health(ctx); err != nil {
		return fmt.Errorf("%w: %v", models.ErrTEIUnavailable, err)
	}
	info, err := c.fetchInfo(ctx)
	if err != nil {
		return fmt.Errorf("%w: %v", models.ErrTEIUnavailable, err)
	}
	if err := c.validateInfo(info); err != nil {
		return fmt.Errorf("%w: %v", models.ErrInvalidEmbedding, err)
	}
	c.mu.Lock()
	c.info = &info
	c.mu.Unlock()
	return nil
}

func (c *Client) EmbedQuery(ctx context.Context, query string) ([]float64, error) {
	start := time.Now()
	vector, err := c.embedQuery(ctx, query)
	c.notify(time.Since(start), err)
	return vector, err
}

func (c *Client) embedQuery(ctx context.Context, query string) ([]float64, error) {
	input := PrefixQuery(query, c.embedding.QueryPrefix)
	info, err := c.cachedOrFetchInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", models.ErrTEIUnavailable, err)
	}
	if err := c.validateInfo(info); err != nil {
		return nil, fmt.Errorf("%w: %v", models.ErrInvalidEmbedding, err)
	}
	count, err := c.countTokens(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("%w: tokenization failed: %v", models.ErrTEIUnavailable, err)
	}
	limit := c.embedding.MaxInputTokens
	if count > limit {
		return nil, fmt.Errorf("%w: query input has %d tokens, maximum is %d", models.ErrInvalidEmbedding, count, limit)
	}

	body := map[string]any{"inputs": []string{input}, "normalize": c.embedding.Normalized, "truncate": c.embedding.AutoTruncate}
	var vectors [][]float64
	if err := c.doJSON(ctx, http.MethodPost, "/embed", body, &vectors); err != nil {
		return nil, fmt.Errorf("%w: %v", models.ErrTEIUnavailable, err)
	}
	if len(vectors) != 1 {
		return nil, fmt.Errorf("%w: TEI returned %d vectors", models.ErrInvalidEmbedding, len(vectors))
	}
	if err := ValidateVector(vectors[0], c.embedding); err != nil {
		return nil, fmt.Errorf("%w: %v", models.ErrInvalidEmbedding, err)
	}
	return vectors[0], nil
}

func (c *Client) health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/health", nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("health returned HTTP %d", resp.StatusCode)
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	return nil
}

func (c *Client) cachedOrFetchInfo(ctx context.Context) (Info, error) {
	c.mu.RLock()
	cached := c.info
	c.mu.RUnlock()
	if cached != nil {
		return *cached, nil
	}
	info, err := c.fetchInfo(ctx)
	if err != nil {
		return Info{}, err
	}
	c.mu.Lock()
	c.info = &info
	c.mu.Unlock()
	return info, nil
}

func (c *Client) fetchInfo(ctx context.Context) (Info, error) {
	var info Info
	if err := c.doJSON(ctx, http.MethodGet, "/info", nil, &info); err != nil {
		return Info{}, err
	}
	return info, nil
}

func (c *Client) countTokens(ctx context.Context, input string) (int, error) {
	var raw any
	if err := c.doJSON(ctx, http.MethodPost, "/tokenize", map[string]any{"inputs": input, "add_special_tokens": true}, &raw); err != nil {
		return 0, err
	}
	return tokenCount(raw)
}

func (c *Client) doJSON(ctx context.Context, method, path string, body any, target any) error {
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
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("%s returned HTTP %d: %s", path, resp.StatusCode, strings.TrimSpace(string(data)))
	}
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 8<<20))
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid JSON from %s: %w", path, err)
	}
	return nil
}

func (c *Client) validateInfo(info Info) error {
	if info.ModelID != c.embedding.ModelID {
		return fmt.Errorf("expected model %s, got %s", c.embedding.ModelID, info.ModelID)
	}
	if info.ModelSHA != c.embedding.ModelRevision {
		return fmt.Errorf("expected model revision %s, got %s", c.embedding.ModelRevision, info.ModelSHA)
	}
	if info.MaxInputLength > 0 && info.MaxInputLength != c.embedding.MaxInputTokens {
		return fmt.Errorf("expected max input length %d, got %d", c.embedding.MaxInputTokens, info.MaxInputLength)
	}
	if info.AutoTruncate != c.embedding.AutoTruncate {
		return fmt.Errorf("expected TEI auto_truncate=%t, got %t", c.embedding.AutoTruncate, info.AutoTruncate)
	}
	return nil
}

func ValidateVector(vector []float64, embedding models.EmbeddingConfig) error {
	if len(vector) != embedding.Dimension {
		return fmt.Errorf("embedding dimension is %d, expected %d", len(vector), embedding.Dimension)
	}
	normSquared := 0.0
	for _, value := range vector {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return errors.New("embedding contains NaN or Infinity")
		}
		normSquared += value * value
	}
	norm := math.Sqrt(normSquared)
	if embedding.Normalized && (norm < 0.99 || norm > 1.01) {
		return fmt.Errorf("embedding L2 norm is %.6f, expected approximately 1", norm)
	}
	return nil
}

func tokenCount(raw any) (int, error) {
	items, ok := raw.([]any)
	if !ok {
		return 0, errors.New("tokenize response is not an array")
	}
	if len(items) == 0 {
		return 0, nil
	}
	if nested, ok := items[0].([]any); ok {
		return len(nested), nil
	}
	return len(items), nil
}

func (c *Client) notify(duration time.Duration, err error) {
	if c.observe != nil {
		c.observe(duration, err)
	}
}
