package bitrix

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

const sendMessageMethod = "imbot.v2.Chat.Message.send"

type Options struct {
	BotId    int
	BotToken string
}

type BitrixClient struct {
	endpoint   string
	botId      int
	botToken   string
	httpClient *http.Client
}

type bitrixMessage struct {
	Message string `json:"message"`
}

type bitrixRequest struct {
	BotId    int           `json:"botId"`
	BotToken string        `json:"botToken"`
	DialogId string        `json:"dialogId"`
	Fields   bitrixMessage `json:"fields"`
}

func New(webhook string, options Options) (*BitrixClient, error) {
	endpoint, err := url.JoinPath(webhook, sendMessageMethod)

	if err != nil {
		return nil, err
	}
	httpClient := http.Client{}
	bc := BitrixClient{
		endpoint:   endpoint,
		botId:      options.BotId,
		botToken:   options.BotToken,
		httpClient: &httpClient,
	}
	return &bc, nil
}

func (c *BitrixClient) Send(ctx context.Context, dialogId string, message string) error {
	body, err := json.Marshal(bitrixRequest{
		BotId:    c.botId,
		BotToken: c.botToken,
		DialogId: dialogId,
		Fields:   bitrixMessage{Message: message},
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", c.endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	setHeaders(req)
	res, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if err := checkStatusCode(res); err != nil {
		return err
	}
	var value any
	if err := json.NewDecoder(res.Body).Decode(&value); err != nil {
		return err
	}
	return nil
}

func setHeaders(req *http.Request) {
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
}

func checkStatusCode(res *http.Response) error {
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("error status code %v", res.StatusCode)
	}
	return nil
}
