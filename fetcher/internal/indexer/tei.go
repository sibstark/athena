package indexer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

type TEIClient struct {
	baseURL  string
	http     *http.Client
	contract EmbeddingContract
	mu       sync.RWMutex
	info     *teiInfo
}

type teiInfo struct {
	ModelID            string `json:"model_id"`
	ModelSHA           string `json:"model_sha"`
	MaxInputLength     int    `json:"max_input_length"`
	AutoTruncate       bool   `json:"auto_truncate"`
	ModelDType         string `json:"model_dtype"`
	MaxBatchRequests   int    `json:"max_batch_requests"`
	MaxClientBatchSize int    `json:"max_client_batch_size"`
}

func NewTEIClient(baseURL string, timeout time.Duration, contract EmbeddingContract) *TEIClient {
	return &TEIClient{baseURL: strings.TrimRight(baseURL, "/"), http: &http.Client{Timeout: timeout}, contract: contract}
}

func (client *TEIClient) Ready(ctx context.Context) error {
	if err := client.health(ctx); err != nil {
		return fmt.Errorf("TEI health check: %w", err)
	}
	var info teiInfo
	if err := client.doJSON(ctx, http.MethodGet, "/info", nil, &info); err != nil {
		return fmt.Errorf("TEI info: %w", err)
	}
	if info.ModelID != client.contract.ModelID {
		return fmt.Errorf("TEI model mismatch: expected %s, got %s", client.contract.ModelID, info.ModelID)
	}
	if info.ModelSHA != client.contract.Revision {
		return fmt.Errorf("TEI revision mismatch: expected %s, got %s", client.contract.Revision, info.ModelSHA)
	}
	if info.ModelDType != "" && info.ModelDType != client.contract.DType {
		return fmt.Errorf("TEI dtype mismatch: expected %s, got %s", client.contract.DType, info.ModelDType)
	}
	if info.AutoTruncate != client.contract.AutoTruncate {
		return fmt.Errorf("TEI auto_truncate mismatch: expected %t, got %t", client.contract.AutoTruncate, info.AutoTruncate)
	}
	if info.MaxInputLength != client.contract.MaxInputTokens {
		return fmt.Errorf("TEI max input length mismatch: expected %d, got %d", client.contract.MaxInputTokens, info.MaxInputLength)
	}
	client.mu.Lock()
	client.info = &info
	client.mu.Unlock()
	return nil
}

func (client *TEIClient) CountTokens(ctx context.Context, input string) (int, error) {
	var raw any
	if err := client.doJSON(ctx, http.MethodPost, "/tokenize", map[string]any{
		"inputs": input, "add_special_tokens": true,
	}, &raw); err != nil {
		return 0, err
	}
	items, ok := raw.([]any)
	if !ok {
		return 0, fmt.Errorf("tokenize response is not an array")
	}
	if len(items) > 0 {
		if nested, ok := items[0].([]any); ok {
			return len(nested), nil
		}
	}
	return len(items), nil
}

func (client *TEIClient) EmbedDocuments(ctx context.Context, inputs []string) ([][]float64, error) {
	if len(inputs) == 0 {
		return nil, nil
	}
	client.mu.RLock()
	info := client.info
	client.mu.RUnlock()
	limit := 32
	if info != nil {
		for _, candidate := range []int{info.MaxBatchRequests, info.MaxClientBatchSize} {
			if candidate > 0 && candidate < limit {
				limit = candidate
			}
		}
	}
	result := make([][]float64, 0, len(inputs))
	for start := 0; start < len(inputs); start += limit {
		end := start + limit
		if end > len(inputs) {
			end = len(inputs)
		}
		var vectors [][]float64
		if err := client.doJSON(ctx, http.MethodPost, "/embed", map[string]any{
			"inputs": inputs[start:end], "normalize": client.contract.Normalized, "truncate": client.contract.AutoTruncate,
		}, &vectors); err != nil {
			return nil, err
		}
		if len(vectors) != end-start {
			return nil, fmt.Errorf("TEI returned %d vectors for %d inputs", len(vectors), end-start)
		}
		for _, vector := range vectors {
			if err := validateVector(vector, client.contract); err != nil {
				return nil, err
			}
		}
		result = append(result, vectors...)
	}
	return result, nil
}

func (client *TEIClient) health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, client.baseURL+"/health", nil)
	if err != nil {
		return err
	}
	response, err := client.http.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", response.StatusCode)
	}
	return nil
}

func (client *TEIClient) doJSON(ctx context.Context, method, path string, body, target any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, client.baseURL+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := client.http.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("%s returned HTTP %d: %s", path, response.StatusCode, strings.TrimSpace(string(data)))
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 32<<20)).Decode(target); err != nil {
		return fmt.Errorf("invalid JSON from %s: %w", path, err)
	}
	return nil
}
