package webhook

import (
	"errors"
	"fmt"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestParseJSONEvent(t *testing.T) {
	body := `{
		"event":"ONIMBOTV2MESSAGEADD",
		"auth":{"application_token":"app-secret"},
		"data":{
			"message":{"id":789,"text":"Что такое ментальная арифметика?","authorId":42},
			"chat":{"dialogId":"chat17"},
			"bot":{"id":7}
		}
	}`
	req := httptest.NewRequest("POST", "/webhook", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	event, err := Parse(req)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if event.Event != "ONIMBOTV2MESSAGEADD" {
		t.Errorf("Event = %q", event.Event)
	}
	if event.ApplicationToken != "app-secret" {
		t.Errorf("ApplicationToken = %q", event.ApplicationToken)
	}
	if event.Message.Id != 789 {
		t.Errorf("Message.Id = %d", event.Message.Id)
	}
	if event.Message.Text != "Что такое ментальная арифметика?" {
		t.Errorf("Message.Text = %q", event.Message.Text)
	}
	if event.Message.AuthorId != 42 {
		t.Errorf("Message.AuthorId = %d", event.Message.AuthorId)
	}
	if event.Message.IsSystem {
		t.Error("Message.IsSystem = true")
	}
	if event.Chat.DialogId != "chat17" {
		t.Errorf("Chat.DialogId = %q", event.Chat.DialogId)
	}
	if event.Bot.Id != 7 {
		t.Errorf("Bot.Id = %d", event.Bot.Id)
	}
}

func TestParseNestedFormEvent(t *testing.T) {
	form := url.Values{
		"event":                   {"ONIMBOTV2MESSAGEADD"},
		"auth[application_token]": {"app-secret"},
		"message[id]":             {"789"},
		"message[text]":           {"Привет"},
		"message[authorId]":       {"42"},
		"message[isSystem]":       {"N"},
		"chat[dialogId]":          {"42"},
		"bot[id]":                 {"7"},
	}
	req := httptest.NewRequest("POST", "/webhook", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")

	event, err := Parse(req)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if event.Message.Text != "Привет" {
		t.Errorf("Message.Text = %q", event.Message.Text)
	}
	if event.Message.Id != 789 {
		t.Errorf("Message.Id = %d", event.Message.Id)
	}
	if event.Message.AuthorId != 42 {
		t.Errorf("Message.AuthorId = %d", event.Message.AuthorId)
	}
	if event.Message.IsSystem {
		t.Error("Message.IsSystem = true")
	}
	if event.Chat.DialogId != "42" {
		t.Errorf("Chat.DialogId = %q", event.Chat.DialogId)
	}
}

func TestParsePayloadSizeLimit(t *testing.T) {
	base := `{"event":"ONIMBOTV2MESSAGEADD","auth":{"application_token":"token"},"message":{"id":3,"text":"text","authorId":1},"chat":{"dialogId":"chat"},"bot":{"id":2},"padding":""}`
	exactBody := base[:len(base)-2] + strings.Repeat("a", int(maxBodySize)-len(base)) + `"}`
	if int64(len(exactBody)) != maxBodySize {
		t.Fatalf("test body size = %d", len(exactBody))
	}

	t.Run("accepts exactly one MiB", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/webhook", strings.NewReader(exactBody))
		if _, err := Parse(req); err != nil {
			t.Fatalf("Parse() error = %v", err)
		}
	})

	t.Run("rejects more than one MiB", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/webhook", strings.NewReader(exactBody+" "))
		_, err := Parse(req)
		if !errors.Is(err, errBodyTooLarge) {
			t.Fatalf("Parse() error = %v, want %v", err, errBodyTooLarge)
		}
	})
}

func TestParseRejectsInvalidPayload(t *testing.T) {
	tests := []struct {
		name string
		body string
		want error
	}{
		{name: "invalid JSON", body: `{`, want: errInvalidPayload},
		{name: "array root", body: `[]`, want: errInvalidPayload},
		{name: "missing required fields", body: `{}`, want: errMissingRequiredField},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/webhook", strings.NewReader(test.body))
			_, err := Parse(req)
			if !errors.Is(err, test.want) {
				t.Fatalf("Parse() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestParseAlternativeFieldsAndValues(t *testing.T) {
	for _, systemValue := range []string{"1", "y", "yes", "true"} {
		t.Run(fmt.Sprintf("system_%s", systemValue), func(t *testing.T) {
			body := fmt.Sprintf(`{
				"EVENT":" ONIMBOTV2MESSAGEADD ",
				"AUTH":{"APPLICATION_TOKEN":" token "},
				"DATA":{"PARAMS":{
					"MESSAGE_ID":"789",
					"MESSAGE":" hello ",
					"FROM_USER_ID":"42",
					"DIALOG_ID":17,
					"BOT_ID":"7",
					"SYSTEM":%q
				}}
			}`, systemValue)
			req := httptest.NewRequest("POST", "/webhook", strings.NewReader(body))

			event, err := Parse(req)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if event.Event != "ONIMBOTV2MESSAGEADD" || event.ApplicationToken != "token" || event.Message.Text != "hello" {
				t.Fatalf("unexpected normalized event: %+v", event)
			}
			if event.Message.Id != 789 || event.Message.AuthorId != 42 || event.Chat.DialogId != "17" || event.Bot.Id != 7 {
				t.Fatalf("unexpected normalized IDs: %+v", event)
			}
			if !event.Message.IsSystem {
				t.Errorf("Message.IsSystem = false for %q", systemValue)
			}
		})
	}
}
