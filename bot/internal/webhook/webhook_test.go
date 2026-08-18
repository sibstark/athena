package webhook

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestCreateWebhookHandlerLogsInvalidPayload(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	handler := CreateWebhookHandler(Options{Logger: logger})
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader("{"))

	statusCode, _ := handler(req)
	if statusCode != http.StatusBadRequest {
		t.Fatalf("statusCode = %d, want %d", statusCode, http.StatusBadRequest)
	}
	if !strings.Contains(output.String(), `"msg":"invalid webhook payload"`) {
		t.Fatalf("log does not contain invalid payload event: %s", output.String())
	}
}

func TestCreateWebhookHandlerDoesNotLogSecretsOrMessage(t *testing.T) {
	const (
		actualToken   = "actual-secret-token"
		expectedToken = "expected-secret-token"
		message       = "private user message"
	)
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	handler := CreateWebhookHandler(Options{
		Logger: logger,
		Options: WebhookOptions{
			ApplicationToken: expectedToken,
		},
	})
	body := fmt.Sprintf(`{
		"event":"ONIMBOTV2MESSAGEADD",
		"auth":{"application_token":%q},
		"message":{"text":%q,"authorId":42},
		"chat":{"dialogId":"chat17"},
		"bot":{"id":7}
	}`, actualToken, message)
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(body))

	statusCode, _ := handler(req)
	if statusCode != http.StatusUnauthorized {
		t.Fatalf("statusCode = %d, want %d", statusCode, http.StatusUnauthorized)
	}
	for _, secret := range []string{actualToken, expectedToken, message} {
		if strings.Contains(output.String(), secret) {
			t.Fatalf("log contains sensitive value %q: %s", secret, output.String())
		}
	}
}

func TestCreateWebhookHandlerUsesDefaultLogger(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	handler := CreateWebhookHandler(Options{})
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader("{"))
	handler(req)

	if !strings.Contains(output.String(), `"msg":"invalid webhook payload"`) {
		t.Fatalf("default logger did not receive event: %s", output.String())
	}
}

func TestSafeErrorMessageRemovesRequestURL(t *testing.T) {
	err := &url.Error{
		Op:  "POST",
		URL: "https://example.test/rest/secret-token/imbot.send",
		Err: errors.New("connection refused"),
	}

	message := safeErrorMessage(err)
	if message != "connection refused" {
		t.Fatalf("safeErrorMessage() = %q", message)
	}
	if strings.Contains(message, "secret-token") {
		t.Fatalf("safeErrorMessage() leaked URL: %q", message)
	}
}
