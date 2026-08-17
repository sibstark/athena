package webhook

import (
	"context"
	"net/http"
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
	return func(req *http.Request) (statusCode int, status string) {
		if req.Method != http.MethodPost {
			return http.StatusMethodNotAllowed, "405 Not Allowed"
		}
		event, err := Parse(req)
		if err != nil {
			return http.StatusBadRequest, "400 Bad request"
		}

		if event.ApplicationToken != options.Options.ApplicationToken {
			return http.StatusUnauthorized, "401 Not Authorized"
		}

		if event.Message.AuthorId == event.Bot.Id || event.Message.IsSystem || event.Event != "ONIMBOTV2MESSAGEADD" {
			return http.StatusOK, "200 Success"
		}

		go func(dialogId string, message string) {
			ctx, cancel := context.WithTimeout(options.Options.Context, time.Duration(options.Options.TimeoutMs)*time.Millisecond)
			answer, err := options.Api.Answer(ctx, message)
			cancel()
			if err != nil {
				// log err
				answer = defaultFallbackMessage
			}
			ctx, cancel = context.WithTimeout(options.Options.Context, time.Duration(options.Options.TimeoutMs)*time.Millisecond)
			err = options.Bitrix.Send(ctx, dialogId, answer)
			if err != nil {
				// log err
			}
			defer cancel()
		}(event.Chat.DialogId, event.Message.Text)

		return http.StatusOK, "200 Success"
	}
}
