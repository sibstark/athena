package deepseek

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/sibstark/athena/api/internal/models"
)

type Observer func(time.Duration, error)
type RetryObserver func()
type Sleeper func(context.Context, time.Duration) error

type Options struct {
	BaseURL        string
	APIKey         string
	Model          string
	Timeout        time.Duration
	MaxTokens      int
	Thinking       bool
	MaxConcurrency int
	HTTPClient     *http.Client
	Observe        Observer
	OnRetry        RetryObserver
	Sleep          Sleeper
	Jitter         func() float64
}

type Client struct {
	baseURL, apiKey, model string
	maxTokens              int
	thinking               bool
	http                   *http.Client
	timeout                time.Duration
	sem                    chan struct{}
	observe                Observer
	onRetry                RetryObserver
	sleep                  Sleeper
	jitter                 func() float64
}

type chatRequest struct {
	Model    string `json:"model"`
	Thinking struct {
		Type string `json:"type"`
	} `json:"thinking"`
	Messages  []message `json:"messages"`
	MaxTokens int       `json:"max_tokens"`
	Stream    bool      `json:"stream"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     *int `json:"prompt_tokens"`
		CompletionTokens *int `json:"completion_tokens"`
	} `json:"usage"`
}

func New(options Options) *Client {
	httpClient := options.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: options.Timeout}
	}
	sleep := options.Sleep
	if sleep == nil {
		sleep = sleepContext
	}
	jitter := options.Jitter
	if jitter == nil {
		jitter = func() float64 { return rand.Float64() }
	}
	return &Client{baseURL: strings.TrimRight(options.BaseURL, "/"), apiKey: options.APIKey, model: options.Model, maxTokens: options.MaxTokens, thinking: options.Thinking, http: httpClient, timeout: options.Timeout, sem: make(chan struct{}, options.MaxConcurrency), observe: options.Observe, onRetry: options.OnRetry, sleep: sleep, jitter: jitter}
}

func (c *Client) Model() string { return c.model }

func (c *Client) Generate(ctx context.Context, systemPrompt, userPrompt string) (models.LLMResult, error) {
	start := time.Now()
	if c.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}
	select {
	case c.sem <- struct{}{}:
		defer func() { <-c.sem }()
	case <-ctx.Done():
		return models.LLMResult{}, ctx.Err()
	}
	result, err := c.generate(ctx, systemPrompt, userPrompt)
	result.Duration = time.Since(start)
	if c.observe != nil {
		c.observe(result.Duration, err)
	}
	return result, err
}

func (c *Client) generate(ctx context.Context, systemPrompt, userPrompt string) (models.LLMResult, error) {
	reqBody := chatRequest{Model: c.model, Messages: []message{{Role: "system", Content: systemPrompt}, {Role: "user", Content: userPrompt}}, MaxTokens: c.maxTokens, Stream: false}
	if c.thinking {
		reqBody.Thinking.Type = "enabled"
	} else {
		reqBody.Thinking.Type = "disabled"
	}
	data, err := json.Marshal(reqBody)
	if err != nil {
		return models.LLMResult{}, fmt.Errorf("%w: request encoding failed", models.ErrLLMUnavailable)
	}
	var lastStatus int
	for attempt := 0; attempt < 3; attempt++ {
		response, status, retryAfter, requestErr := c.attempt(ctx, data)
		lastStatus = status
		if requestErr == nil {
			if len(response.Choices) == 0 || strings.TrimSpace(response.Choices[0].Message.Content) == "" {
				return models.LLMResult{}, fmt.Errorf("%w: empty completion", models.ErrLLMUnavailable)
			}
			return models.LLMResult{Content: response.Choices[0].Message.Content, InputTokens: response.Usage.PromptTokens, OutputTokens: response.Usage.CompletionTokens}, nil
		}
		if ctx.Err() != nil {
			return models.LLMResult{}, ctx.Err()
		}
		if !retryable(status, requestErr) || attempt == 2 {
			if status == http.StatusTooManyRequests {
				return models.LLMResult{}, fmt.Errorf("%w: request failed after retries", models.ErrLLMRateLimited)
			}
			return models.LLMResult{}, fmt.Errorf("%w: request failed (HTTP %d)", models.ErrLLMUnavailable, status)
		}
		if c.onRetry != nil {
			c.onRetry()
		}
		delay := retryAfter
		if delay <= 0 {
			delay = time.Duration(1<<attempt)*200*time.Millisecond + time.Duration(c.jitter()*100)*time.Millisecond
		}
		if delay > 5*time.Second {
			delay = 5 * time.Second
		}
		if err := c.sleep(ctx, delay); err != nil {
			return models.LLMResult{}, err
		}
	}
	return models.LLMResult{}, fmt.Errorf("%w: request failed (HTTP %d)", models.ErrLLMUnavailable, lastStatus)
}

func (c *Client) attempt(ctx context.Context, data []byte) (chatResponse, int, time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(data))
	if err != nil {
		return chatResponse{}, 0, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return chatResponse{}, 0, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return chatResponse{}, resp.StatusCode, parseRetryAfter(resp.Header.Get("Retry-After")), errors.New("DeepSeek HTTP error")
	}
	var result chatResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&result); err != nil {
		return chatResponse{}, resp.StatusCode, 0, err
	}
	return result, resp.StatusCode, 0, nil
}

func retryable(status int, err error) bool {
	if err == nil {
		return false
	}
	if status == 0 {
		var netErr net.Error
		return errors.As(err, &netErr) || !errors.Is(err, context.Canceled)
	}
	return status == 429 || status == 500 || status == 502 || status == 503 || status == 504
}

func parseRetryAfter(value string) time.Duration {
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	if when, err := http.ParseTime(value); err == nil {
		if delay := time.Until(when); delay > 0 {
			return delay
		}
	}
	return 0
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func ValidateBaseURL(raw string) error { _, err := url.ParseRequestURI(raw); return err }
