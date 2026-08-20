package bitrix

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const maxResponseBytes = 16 << 20

type SleepFunc func(context.Context, time.Duration) error

type ClientOptions struct {
	HTTPClient  *http.Client
	Sleep       SleepFunc
	MaxAttempts int
	Timeout     time.Duration
}

type Client struct {
	baseURL     *url.URL
	portal      string
	secret      string
	httpClient  *http.Client
	sleep       SleepFunc
	maxAttempts int
	timeout     time.Duration
}

type APIError struct {
	Method  string
	Code    string
	Message string
	Status  int
}

func (e *APIError) Error() string {
	if e.Status > 0 {
		return fmt.Sprintf("%s failed: %s (%s, HTTP %d)", e.Method, e.Message, e.Code, e.Status)
	}
	return fmt.Sprintf("%s failed: %s (%s)", e.Method, e.Message, e.Code)
}

type envelope struct {
	Result           json.RawMessage `json:"result"`
	Error            json.RawMessage `json:"error"`
	ErrorDescription string          `json:"error_description"`
}

type v3Error struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	Validation []struct {
		Field   string `json:"field"`
		Message string `json:"message"`
	} `json:"validation"`
}

func NewClient(webhookURL string, options ClientOptions) (*Client, error) {
	parsed, err := url.Parse(strings.TrimSpace(webhookURL))
	if err != nil || parsed.Scheme == "" || parsed.Hostname() == "" {
		return nil, errors.New("BITRIX_WEBHOOK_URL must be a valid URL")
	}
	if parsed.Scheme != "https" && parsed.Hostname() != "localhost" && parsed.Hostname() != "127.0.0.1" {
		return nil, errors.New("BITRIX_WEBHOOK_URL must use HTTPS")
	}
	parts := strings.FieldsFunc(parsed.Path, func(r rune) bool { return r == '/' })
	if len(parts) < 3 || parts[0] != "rest" {
		return nil, errors.New("BITRIX_WEBHOOK_URL must look like https://portal.bitrix24.ru/rest/USER_ID/WEBHOOK_SECRET/")
	}
	offset := 1
	if parts[1] == "api" {
		offset = 2
	}
	if len(parts) != offset+2 || parts[offset] == "" || parts[offset+1] == "" {
		return nil, errors.New("BITRIX_WEBHOOK_URL must contain a user ID and webhook secret")
	}
	base, err := url.Parse(fmt.Sprintf("%s://%s/rest/api/%s/%s/", parsed.Scheme, parsed.Host, url.PathEscape(parts[offset]), url.PathEscape(parts[offset+1])))
	if err != nil {
		return nil, fmt.Errorf("construct Bitrix API URL: %w", err)
	}
	client := options.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 35 * time.Second}
	}
	sleep := options.Sleep
	if sleep == nil {
		sleep = func(ctx context.Context, delay time.Duration) error {
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				return nil
			}
		}
	}
	maxAttempts := options.MaxAttempts
	if maxAttempts == 0 {
		maxAttempts = 4
	}
	if maxAttempts < 1 {
		return nil, errors.New("max attempts must be positive")
	}
	timeout := options.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	return &Client{baseURL: base, portal: parsed.Scheme + "://" + parsed.Host, secret: parts[offset+1], httpClient: client, sleep: sleep, maxAttempts: maxAttempts, timeout: timeout}, nil
}

func (c *Client) PortalOrigin() string { return c.portal }

func (c *Client) Redact(value string) string {
	if c.secret == "" {
		return value
	}
	return strings.ReplaceAll(value, c.secret, "[REDACTED]")
}

func (c *Client) Call(ctx context.Context, method string, params any, result any) error {
	payload, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("encode %s request: %w", method, err)
	}
	endpoint := c.baseURL.ResolveReference(&url.URL{Path: method})

	for attempt := 1; attempt <= c.maxAttempts; attempt++ {
		requestCtx, cancel := context.WithTimeout(ctx, c.timeout)
		req, requestErr := http.NewRequestWithContext(requestCtx, http.MethodPost, endpoint.String(), bytes.NewReader(payload))
		if requestErr != nil {
			cancel()
			return fmt.Errorf("create %s request: %w", method, requestErr)
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Type", "application/json")
		resp, requestErr := c.httpClient.Do(req)
		if requestErr != nil {
			cancel()
			if attempt < c.maxAttempts && isRetryableNetworkError(requestErr) {
				if err := c.sleep(ctx, retryDelay(attempt, "")); err != nil {
					return err
				}
				continue
			}
			return &APIError{Method: method, Code: "NETWORK_ERROR", Message: c.Redact(requestErr.Error())}
		}

		body, readErr := readLimited(resp.Body, maxResponseBytes)
		resp.Body.Close()
		cancel()
		if readErr != nil {
			return &APIError{Method: method, Code: "INVALID_RESPONSE", Message: c.Redact(readErr.Error()), Status: resp.StatusCode}
		}

		var decoded envelope
		if err := json.Unmarshal(body, &decoded); err != nil {
			if attempt < c.maxAttempts && (resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500) {
				if err := c.sleep(ctx, retryDelay(attempt, resp.Header.Get("Retry-After"))); err != nil {
					return err
				}
				continue
			}
			return &APIError{Method: method, Code: "INVALID_RESPONSE", Message: "Bitrix returned non-JSON response", Status: resp.StatusCode}
		}

		apiErr := decodeAPIError(method, resp.StatusCode, decoded)
		if apiErr != nil {
			apiErr.Message = c.Redact(apiErr.Message)
			if attempt < c.maxAttempts && isRetryableAPIError(apiErr) {
				if err := c.sleep(ctx, retryDelay(attempt, resp.Header.Get("Retry-After"))); err != nil {
					return err
				}
				continue
			}
			return apiErr
		}
		if len(decoded.Result) == 0 || bytes.Equal(decoded.Result, []byte("null")) {
			return &APIError{Method: method, Code: "INVALID_RESPONSE", Message: "Bitrix returned no result", Status: resp.StatusCode}
		}
		if err := json.Unmarshal(decoded.Result, result); err != nil {
			return &APIError{Method: method, Code: "INVALID_RESPONSE", Message: fmt.Sprintf("decode result: %v", err), Status: resp.StatusCode}
		}
		return nil
	}
	return &APIError{Method: method, Code: "RETRY_EXHAUSTED", Message: "retry attempts exhausted"}
}

func (c *Client) ListWorkspaces(ctx context.Context) ([]Workspace, error) {
	var workspaces []Workspace
	var cursor *Cursor
	for {
		pagination := map[string]any{"limit": 200}
		if cursor != nil {
			pagination["afterCursor"] = cursor
		}
		var page WorkspaceListResult
		if err := c.Call(ctx, "note.collection.list", map[string]any{"pagination": pagination}, &page); err != nil {
			return nil, err
		}
		workspaces = append(workspaces, page.Items...)
		if page.NextCursor == nil {
			return workspaces, nil
		}
		cursor = page.NextCursor
	}
}

func (c *Client) GetDocumentTree(ctx context.Context, workspaceID int) (DocumentTreeResult, error) {
	var result DocumentTreeResult
	err := c.Call(ctx, "note.document.tree.list", map[string]any{"collectionId": workspaceID}, &result)
	return result, err
}

func (c *Client) GetDocument(ctx context.Context, documentID int) (Document, error) {
	var result DocumentGetResult
	err := c.Call(ctx, "note.document.get", map[string]any{"id": documentID}, &result)
	return result.Item, err
}

func decodeAPIError(method string, status int, decoded envelope) *APIError {
	if len(decoded.Error) == 0 && status >= 200 && status < 300 {
		return nil
	}
	code := fmt.Sprintf("HTTP_%d", status)
	message := strings.TrimSpace(decoded.ErrorDescription)
	if len(decoded.Error) > 0 {
		var legacy string
		if json.Unmarshal(decoded.Error, &legacy) == nil && legacy != "" {
			code = legacy
		}
		var modern v3Error
		if json.Unmarshal(decoded.Error, &modern) == nil && modern.Code != "" {
			code = modern.Code
			parts := make([]string, 0, 1+len(modern.Validation))
			if modern.Message != "" {
				parts = append(parts, modern.Message)
			}
			for _, validation := range modern.Validation {
				if validation.Field != "" {
					parts = append(parts, validation.Field+": "+validation.Message)
				} else if validation.Message != "" {
					parts = append(parts, validation.Message)
				}
			}
			if len(parts) > 0 {
				message = strings.Join(parts, " — ")
			}
		}
	}
	if message == "" {
		message = http.StatusText(status)
	}
	return &APIError{Method: method, Code: code, Message: message, Status: status}
}

func isRetryableAPIError(err *APIError) bool {
	if err.Status == http.StatusTooManyRequests || err.Status >= 500 {
		return true
	}
	switch strings.ToUpper(err.Code) {
	case "QUERY_LIMIT_EXCEEDED", "OPERATION_TIME_LIMIT", "OVERLOAD_LIMIT":
		return true
	default:
		return false
	}
}

func isRetryableNetworkError(err error) bool {
	if errors.Is(err, context.Canceled) {
		return false
	}
	var netErr net.Error
	return errors.As(err, &netErr) || errors.Is(err, io.EOF)
}

func retryDelay(attempt int, retryAfter string) time.Duration {
	if seconds, err := strconv.ParseFloat(strings.TrimSpace(retryAfter), 64); err == nil && seconds >= 0 {
		return time.Duration(seconds * float64(time.Second))
	}
	if when, err := http.ParseTime(retryAfter); err == nil {
		if delay := time.Until(when); delay > 0 {
			return delay
		}
	}
	delay := 500 * time.Millisecond * time.Duration(1<<(attempt-1))
	if delay > 8*time.Second {
		return 8 * time.Second
	}
	return delay
}

func readLimited(reader io.Reader, max int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, fmt.Errorf("response exceeds %d bytes", max)
	}
	return data, nil
}
