package webhook

import (
	"context"
	"encoding/json"
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

type BitrixEvent struct {
	Event            string             `json:"event"`
	ApplicationToken string             `json:"applicationToken"`
	Message          BitrixEventMessage `json:"message"`
	Bot              BitrixEventBot     `json:"bot"`
	Chat             BitrixEventChat    `json:"chat"`
}

var defaultFallbackMessage = "Извините, сейчас не удалось получить ответ. Попробуйте повторить запрос позже."

func CreateWebhookHandler(options Options) func(req *http.Request) http.Response {
	return func(req *http.Request) http.Response {
		if req.Method != http.MethodPost {
			return http.Response{
				StatusCode: http.StatusMethodNotAllowed,
				Status:     "405 Not Allowed",
			}
		}
		var event BitrixEvent
		if err := json.NewDecoder(req.Body).Decode(&event); err != nil {
			return http.Response{
				StatusCode: http.StatusBadRequest,
				Status:     "400 Bad request",
			}
		}

		if event.ApplicationToken != options.Options.ApplicationToken {
			return http.Response{
				StatusCode: http.StatusUnauthorized,
				Status:     "401 Not Authorized",
			}
		}

		if event.Message.AuthorId == event.Bot.Id || event.Message.IsSystem || event.Event != "ONIMBOTV2MESSAGEADD" {
			return http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 Success",
			}
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

		return http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 Success",
		}
	}
}
