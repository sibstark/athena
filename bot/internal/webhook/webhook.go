package webhook

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/sibstark/athena/bot/internal/api"
	"github.com/sibstark/athena/bot/internal/bitrix"
)

type WebhookOptions struct {
	ApplicationToken string
	Collection       string
	TimeoutMs        int
	Context          context.Context
}

type Options struct {
	Api     *api.APIClient
	Bitrix  *bitrix.BitrixClient
	Logger  *slog.Logger
	Options WebhookOptions
}

type BitrixEventMessage struct {
	Text     string `json:"text"`
	IsSystem bool   `json:"isSystem"`
	AuthorId int    `json:"authorId"`
}

type BitrixEventBot struct {
	Id int `json:"id"`
}

type BitrixEventChat struct {
	DialogId string `json:"dialogId"`
}

var defaultFallbackMessage = "Извините, сейчас не удалось получить ответ. Попробуйте повторить запрос позже."

func CreateWebhookHandler(options Options) func(req *http.Request) (statusCode int, status string) {
	logger := options.Logger
	if logger == nil {
		logger = slog.Default()
	}

	return func(req *http.Request) (statusCode int, status string) {
		if req.Method != http.MethodPost {
			return http.StatusMethodNotAllowed, "405 Not Allowed"
		}
		logger.WarnContext(req.Context(), "Hook is working")
		event, err := Parse(req)
		if err != nil {
			logger.WarnContext(req.Context(), "invalid webhook payload", "error", err)
			return http.StatusBadRequest, "400 Bad request"
		}

		if event.ApplicationToken != options.Options.ApplicationToken {
			logger.WarnContext(req.Context(), "webhook authorization failed")
			return http.StatusUnauthorized, "401 Not Authorized"
		}

		if event.Message.AuthorId == event.Bot.Id || event.Message.IsSystem || event.Event != "ONIMBOTV2MESSAGEADD" {
			logger.DebugContext(req.Context(), "webhook event ignored", "event", event.Event)
			return http.StatusOK, "200 Success"
		}

		go func(dialogId string, message string) {
			startedAt := time.Now()
			parent := options.Options.Context
			if parent == nil {
				parent = context.Background()
			}

			ctx, cancel := context.WithTimeout(parent, time.Duration(options.Options.TimeoutMs)*time.Millisecond)
			answer, err := options.Api.Answer(ctx, message)
			cancel()
			if err != nil {
				logger.ErrorContext(parent, "answer request failed", "error", safeErrorMessage(err), "dialog_id", dialogId)
				answer = defaultFallbackMessage
			}

			ctx, cancel = context.WithTimeout(parent, time.Duration(options.Options.TimeoutMs)*time.Millisecond)
			err = options.Bitrix.Send(ctx, dialogId, answer)
			cancel()
			if err != nil {
				logger.ErrorContext(parent, "Bitrix message send failed", "error", safeErrorMessage(err), "dialog_id", dialogId)
				return
			}
			logger.InfoContext(
				parent,
				"webhook message processed",
				"dialog_id", dialogId,
				"duration_ms", time.Since(startedAt).Milliseconds(),
			)
		}(event.Chat.DialogId, event.Message.Text)

		return http.StatusOK, "200 Success"
	}
}

func safeErrorMessage(err error) string {
	var urlError *url.Error
	if errors.As(err, &urlError) {
		if urlError.Err != nil {
			return urlError.Err.Error()
		}
		return "HTTP request failed"
	}
	return err.Error()
}
