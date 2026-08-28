package webhook

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/sibstark/athena/bot/internal/answer"
	"github.com/sibstark/athena/bot/internal/api"
	"github.com/sibstark/athena/bot/internal/bitrix"
)

type WebhookOptions struct {
	ApplicationToken string
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
	Id       int    `json:"id"`
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

	return func(req *http.Request) (statusCode int, status string) {
		if req.Method != http.MethodPost {
			return http.StatusMethodNotAllowed, "405 Not Allowed"
		}
		event, err := Parse(req)
		logger.WarnContext(req.Context(), "Hook parsed", "event", event)
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

		go reply(options, event.Message.Id, event.Chat.DialogId, event.Message.Text)

		return http.StatusOK, "200 Success"
	}
}

func reply(options Options, messageId int, dialogId string, message string) {
	logger := options.Logger
	startedAt := time.Now()
	parent := options.Options.Context
	if parent == nil {
		parent = context.Background()
	}

	ctx, cancel := context.WithTimeout(parent, time.Duration(options.Options.TimeoutMs)*time.Millisecond)
	result, err := options.Api.Answer(ctx, message)
	cancel()
	messageText := ""
	if err != nil {
		logger.ErrorContext(parent, "answer request failed", "error", safeErrorMessage(err), "dialog_id", dialogId)
		messageText = defaultFallbackMessage
	} else {
		messageText = answer.Format(result, bitrix.LinkRenderer{})
	}

	ctx, cancel = context.WithTimeout(parent, time.Duration(options.Options.TimeoutMs)*time.Millisecond)
	err = options.Bitrix.SendReply(ctx, messageId, dialogId, messageText)
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
