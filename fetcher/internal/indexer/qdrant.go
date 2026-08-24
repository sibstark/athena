package indexer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type QdrantClient struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

func NewQdrantClient(baseURL, apiKey string, timeout time.Duration) *QdrantClient {
	return &QdrantClient{
		baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey,
		http: &http.Client{Timeout: timeout},
	}
}

func (client *QdrantClient) Ping(ctx context.Context) error {
	var response qdrantResponse[struct {
		Collections []any `json:"collections"`
	}]
	return client.doJSON(ctx, http.MethodGet, "/collections", nil, &response)
}

func (client *QdrantClient) EnsureCollection(ctx context.Context, collection string, dimension int, distance string, pipeline PipelineIdentity, dryRun bool) (CollectionState, error) {
	name := url.PathEscape(collection)
	var existence qdrantResponse[struct {
		Exists bool `json:"exists"`
	}]
	err := client.doJSON(ctx, http.MethodGet, "/collections/"+name+"/exists", nil, &existence)
	if err != nil {
		return CollectionState{}, err
	}
	if !existence.Result.Exists {
		if dryRun {
			return CollectionState{WouldCreate: true}, nil
		}
		var created qdrantResponse[bool]
		if err := client.doJSON(ctx, http.MethodPut, "/collections/"+name, map[string]any{
			"vectors":  map[string]any{"size": dimension, "distance": distance},
			"metadata": pipeline,
		}, &created); err != nil {
			return CollectionState{}, err
		}
		if !created.Result {
			return CollectionState{}, fmt.Errorf("Qdrant did not create collection %s", collection)
		}
		return CollectionState{Created: true}, nil
	}
	info, err := client.collectionInfo(ctx, collection)
	if err != nil {
		return CollectionState{}, err
	}
	var vectors struct {
		Size     int    `json:"size"`
		Distance string `json:"distance"`
	}
	data, _ := json.Marshal(info.Config.Params.Vectors)
	if err := json.Unmarshal(data, &vectors); err != nil || vectors.Size == 0 || vectors.Distance == "" {
		return CollectionState{}, fmt.Errorf("collection %s uses named or unsupported vector configuration", collection)
	}
	if vectors.Size != dimension || !strings.EqualFold(vectors.Distance, distance) {
		return CollectionState{}, fmt.Errorf("collection %s is incompatible: expected %d/%s, got %d/%s", collection, dimension, distance, vectors.Size, vectors.Distance)
	}
	expectedData, _ := json.Marshal(pipeline)
	var expected map[string]any
	_ = json.Unmarshal(expectedData, &expected)
	for key, value := range expected {
		if fmt.Sprint(info.Config.Metadata[key]) != fmt.Sprint(value) {
			return CollectionState{}, fmt.Errorf("collection %s pipeline metadata mismatch for %s: expected %v, got %v; use a new collection", collection, key, value, info.Config.Metadata[key])
		}
	}
	return CollectionState{Exists: true}, nil
}

func (client *QdrantClient) EnsurePayloadIndexes(ctx context.Context, collection string, fields []string, dryRun bool) error {
	if dryRun {
		return nil
	}
	info, err := client.collectionInfo(ctx, collection)
	if err != nil {
		return err
	}
	name := url.PathEscape(collection)
	for _, field := range fields {
		if _, exists := info.PayloadSchema[field]; exists {
			continue
		}
		var response qdrantResponse[any]
		path := "/collections/" + name + "/index?wait=true"
		if err := client.doJSON(ctx, http.MethodPut, path, map[string]any{
			"field_name": field, "field_schema": "keyword",
		}, &response); err != nil {
			return fmt.Errorf("create payload index %s: %w", field, err)
		}
	}
	return nil
}

func (client *QdrantClient) GetPoints(ctx context.Context, collection string, ids []string) ([]StoredPoint, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var response qdrantResponse[[]struct {
		ID      any            `json:"id"`
		Payload map[string]any `json:"payload"`
	}]
	path := "/collections/" + url.PathEscape(collection) + "/points"
	if err := client.doJSON(ctx, http.MethodPost, path, map[string]any{
		"ids": ids, "with_payload": true, "with_vector": false,
	}, &response); err != nil {
		return nil, err
	}
	points := make([]StoredPoint, 0, len(response.Result))
	for _, point := range response.Result {
		points = append(points, StoredPoint{ID: scalarString(point.ID), Payload: point.Payload})
	}
	return points, nil
}

func (client *QdrantClient) FindVector(ctx context.Context, collection string, lookup VectorLookup) ([]float64, error) {
	conditions := append([]map[string]any{{
		"key": "embedding_input_hash", "match": map[string]any{"value": lookup.EmbeddingInputHash},
	}, {
		"key": "embedding_backend", "match": map[string]any{"value": EmbeddingBackend},
	}}, pipelineConditions(lookup.Pipeline)...)
	result, err := client.scroll(ctx, collection, map[string]any{
		"limit": 1, "with_payload": false, "with_vector": true,
		"filter": map[string]any{"must": conditions},
	})
	if err != nil || len(result.Points) == 0 {
		return nil, err
	}
	vector, ok := result.Points[0].Vector.([]any)
	if !ok {
		return nil, nil
	}
	values := make([]float64, len(vector))
	for index, raw := range vector {
		value, ok := raw.(float64)
		if !ok {
			return nil, nil
		}
		values[index] = value
	}
	return values, nil
}

func (client *QdrantClient) Upsert(ctx context.Context, collection string, points []Point) error {
	if len(points) == 0 {
		return nil
	}
	values := make([]map[string]any, 0, len(points))
	for _, point := range points {
		values = append(values, map[string]any{"id": point.ID, "vector": point.Vector, "payload": point.Payload})
	}
	var response qdrantResponse[any]
	path := "/collections/" + url.PathEscape(collection) + "/points?wait=true"
	return client.doJSON(ctx, http.MethodPut, path, map[string]any{"points": values}, &response)
}

func (client *QdrantClient) UpdatePayload(ctx context.Context, collection string, points []StoredPoint) error {
	path := "/collections/" + url.PathEscape(collection) + "/points/payload?wait=true"
	for _, point := range points {
		var response qdrantResponse[any]
		if err := client.doJSON(ctx, http.MethodPut, path, map[string]any{
			"points": []string{point.ID}, "payload": point.Payload,
		}, &response); err != nil {
			return err
		}
	}
	return nil
}

func (client *QdrantClient) DeletePoints(ctx context.Context, collection string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	var response qdrantResponse[any]
	path := "/collections/" + url.PathEscape(collection) + "/points/delete?wait=true"
	return client.doJSON(ctx, http.MethodPost, path, map[string]any{"points": ids}, &response)
}

func (client *QdrantClient) ListManagedPointIDs(ctx context.Context, collection string, pipeline PipelineIdentity) ([]string, error) {
	conditions := pipelineConditions(pipeline)
	ids := make([]string, 0)
	var offset any
	for {
		body := map[string]any{
			"limit": 256, "with_payload": false, "with_vector": false,
			"filter": map[string]any{"must": conditions},
		}
		if offset != nil {
			body["offset"] = offset
		}
		result, err := client.scroll(ctx, collection, body)
		if err != nil {
			return nil, err
		}
		for _, point := range result.Points {
			ids = append(ids, scalarString(point.ID))
		}
		if result.NextPageOffset == nil {
			break
		}
		offset = result.NextPageOffset
	}
	return ids, nil
}

type collectionInfo struct {
	Config struct {
		Params struct {
			Vectors any `json:"vectors"`
		} `json:"params"`
		Metadata map[string]any `json:"metadata"`
	} `json:"config"`
	PayloadSchema map[string]any `json:"payload_schema"`
}

func (client *QdrantClient) collectionInfo(ctx context.Context, collection string) (collectionInfo, error) {
	var response qdrantResponse[collectionInfo]
	err := client.doJSON(ctx, http.MethodGet, "/collections/"+url.PathEscape(collection), nil, &response)
	return response.Result, err
}

type scrollResult struct {
	Points []struct {
		ID     any `json:"id"`
		Vector any `json:"vector"`
	} `json:"points"`
	NextPageOffset any `json:"next_page_offset"`
}

func (client *QdrantClient) scroll(ctx context.Context, collection string, body map[string]any) (scrollResult, error) {
	var response qdrantResponse[scrollResult]
	path := "/collections/" + url.PathEscape(collection) + "/points/scroll"
	err := client.doJSON(ctx, http.MethodPost, path, body, &response)
	return response.Result, err
}

type qdrantResponse[T any] struct {
	Result T       `json:"result"`
	Status any     `json:"status"`
	Time   float64 `json:"time"`
}

func (client *QdrantClient) doJSON(ctx context.Context, method, path string, body, target any) error {
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
	if client.apiKey != "" {
		req.Header.Set("api-key", client.apiKey)
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
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<20)).Decode(target); err != nil {
		return fmt.Errorf("invalid JSON from %s: %w", path, err)
	}
	return nil
}

func pipelineConditions(pipeline PipelineIdentity) []map[string]any {
	return []map[string]any{
		{"key": "point_namespace", "match": map[string]any{"value": pipeline.PointNamespace}},
		{"key": "embedding_model", "match": map[string]any{"value": pipeline.EmbeddingModel}},
		{"key": "embedding_model_revision", "match": map[string]any{"value": pipeline.EmbeddingModelRevision}},
		{"key": "embedding_pipeline_version", "match": map[string]any{"value": pipeline.EmbeddingPipelineVersion}},
	}
}

func scalarString(value any) string {
	switch item := value.(type) {
	case string:
		return item
	case float64:
		return strconv.FormatFloat(item, 'f', -1, 64)
	default:
		return fmt.Sprint(item)
	}
}
