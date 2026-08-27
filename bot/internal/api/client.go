package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

type Options struct {
	ApiKey string
}

type APIClient struct {
	answerEndpoint      string
	collectionsEndpoint string
	apiKey              string
	httpClient          *http.Client
}

type answerRequest struct {
	Query       string   `json:"query"`
	Collections []string `json:"collections"`
}

type answerResponse struct {
	Answer string `json:"answer"`
}

type collectionsResponse struct {
	Collections []string `json:"collections"`
}

func New(baseUrl string, options Options) (*APIClient, error) {
	answerEndpoint, err := url.JoinPath(baseUrl, "v1/answer")
	if err != nil {
		return nil, fmt.Errorf("Path error faced %w", err)
	}
	collectionsEndpoint, err := url.JoinPath(baseUrl, "v1/collections")
	if err != nil {
		return nil, fmt.Errorf("Path error faced %w", err)
	}
	client := http.Client{}
	return &APIClient{
		answerEndpoint:      answerEndpoint,
		collectionsEndpoint: collectionsEndpoint,
		apiKey:              options.ApiKey,
		httpClient:          &client,
	}, nil
}

func (c *APIClient) Answer(ctx context.Context, query string) (string, error) {
	collections, err := c.collections(ctx)
	if err != nil {
		return "", err
	}
	body, err := json.Marshal(answerRequest{
		Query:       query,
		Collections: collections,
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", c.answerEndpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	setHeaders(req, c.apiKey)
	res, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if err := checkStatusCode(res); err != nil {
		return "", err
	}
	var answer answerResponse
	err = json.NewDecoder(res.Body).Decode(&answer)
	if err != nil {
		return "", fmt.Errorf("Error decoding response: %w", err)
	}
	if answer.Answer == "" {
		return "", fmt.Errorf("empty answer")
	}
	return answer.Answer, nil
}

func (c *APIClient) collections(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.collectionsEndpoint, nil)
	if err != nil {
		return nil, err
	}
	setHeaders(req, c.apiKey)
	res, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if err := checkStatusCode(res); err != nil {
		return nil, err
	}
	var response collectionsResponse
	if err := json.NewDecoder(res.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("Error decoding collections response: %w", err)
	}
	if len(response.Collections) == 0 {
		return nil, fmt.Errorf("no collections available")
	}
	return response.Collections, nil
}

func setHeaders(req *http.Request, apiKey string) {
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", apiKey))
}

func checkStatusCode(res *http.Response) error {
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("error status code %v", res.StatusCode)
	}
	return nil
}
