package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

type Options struct {
	ApiKey     string
	Collection string
	TimeoutMs  int
}

type APIClient struct {
	endpoint   string
	apiKey     string
	collection string
	httpClient *http.Client
}

type answerRequest struct {
	Query      string `json:"query"`
	Collection string `json:"collection"`
}

type answerResponse struct {
	Answer string `json:"answer"`
}

func New(baseUrl string, options Options) (*APIClient, error) {
	endpoint, err := url.JoinPath(baseUrl, "v1/answer")
	if err != nil {
		return nil, fmt.Errorf("Path error faced %w", err)
	}
	client := http.Client{
		Timeout: time.Duration(options.TimeoutMs) * time.Millisecond,
	}
	return &APIClient{
		endpoint:   endpoint,
		apiKey:     options.ApiKey,
		collection: options.Collection,
		httpClient: &client,
	}, nil
}

func (c *APIClient) Answer(ctx context.Context, query string) (string, error) {
	body, err := json.Marshal(answerRequest{
		Query:      query,
		Collection: c.collection,
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", c.endpoint, bytes.NewReader(body))
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
