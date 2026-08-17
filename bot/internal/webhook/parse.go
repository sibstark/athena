package webhook

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

type BitrixEvent struct {
	Event            string             `json:"event"`
	ApplicationToken string             `json:"applicationToken"`
	Message          BitrixEventMessage `json:"message"`
	Bot              BitrixEventBot     `json:"bot"`
	Chat             BitrixEventChat    `json:"chat"`
}

type recordValue map[string]any

type missingValue struct{}

const maxBodySize int64 = 1 << 20 // 1 MiB = 1_048_576 bytes

var (
	errBodyTooLarge         = errors.New("Request body is too large")
	errInvalidPayload       = errors.New("Invalid webhook payload")
	errMissingRequiredField = errors.New("Webhook payload does not contain the required Bitrix fields")
	missing                 = missingValue{}
	nestedKeyPartPattern    = regexp.MustCompile(`[^\[\]]+`)
)

func Parse(req *http.Request) (BitrixEvent, error) {
	root, err := readBody(req)
	if err != nil {
		return BitrixEvent{}, err
	}

	data := firstRecord(value(root, "data"), value(root, "DATA"))
	if data == nil {
		data = root
	}
	auth := firstRecord(value(root, "auth"), value(data, "auth"), value(root, "AUTH"))
	if auth == nil {
		auth = recordValue{}
	}
	params := firstRecord(
		value(data, "params"),
		value(data, "PARAMS"),
		value(root, "params"),
		value(root, "PARAMS"),
	)
	if params == nil {
		params = recordValue{}
	}
	message := firstRecord(
		value(root, "message"),
		value(data, "message"),
		value(root, "MESSAGE"),
		value(data, "MESSAGE"),
		value(params, "message"),
		params,
	)
	chat := firstRecord(
		value(root, "chat"),
		value(data, "chat"),
		value(root, "CHAT"),
		value(data, "CHAT"),
	)
	if chat == nil {
		chat = recordValue{}
	}
	bot := firstRecord(
		value(root, "bot"),
		value(data, "bot"),
		value(root, "BOT"),
		value(data, "BOT"),
	)
	if bot == nil {
		bot = recordValue{}
	}

	event, hasEvent := stringValue(value(root, "event"), value(data, "event"), value(root, "EVENT"))
	applicationToken, hasApplicationToken := stringValue(
		value(auth, "application_token"),
		value(auth, "applicationToken"),
		value(auth, "APPLICATION_TOKEN"),
		value(root, "application_token"),
	)
	text, hasText := stringValue(
		value(message, "text"),
		value(message, "message"),
		value(message, "MESSAGE"),
		value(params, "message"),
		value(params, "MESSAGE"),
	)
	authorID, hasAuthorID := numberValue(
		value(message, "authorId"),
		value(message, "author_id"),
		value(message, "AUTHOR_ID"),
		value(params, "authorId"),
		value(params, "FROM_USER_ID"),
	)
	dialogID, hasDialogID := stringValue(
		value(chat, "dialogId"),
		value(chat, "dialog_id"),
		value(chat, "DIALOG_ID"),
		value(message, "dialogId"),
		value(params, "dialogId"),
		value(params, "DIALOG_ID"),
		value(root, "dialogId"),
	)
	botID, hasBotID := numberValue(
		value(bot, "id"),
		value(bot, "botId"),
		value(data, "botId"),
		value(root, "botId"),
		value(params, "BOT_ID"),
	)

	if !hasEvent || !hasApplicationToken || !hasText || !hasAuthorID || !hasDialogID || !hasBotID {
		return BitrixEvent{}, errMissingRequiredField
	}

	return BitrixEvent{
		Event:            event,
		ApplicationToken: applicationToken,
		Message: BitrixEventMessage{
			Text:     text,
			AuthorId: authorID,
			IsSystem: booleanValue(firstValue(
				value(message, "isSystem"),
				value(message, "is_system"),
				value(message, "system"),
				value(message, "SYSTEM"),
				value(params, "SYSTEM"),
			)),
		},
		Chat: BitrixEventChat{DialogId: dialogID},
		Bot:  BitrixEventBot{Id: botID},
	}, nil
}

func readBody(req *http.Request) (recordValue, error) {
	if req == nil || req.Body == nil {
		return nil, errInvalidPayload
	}

	body, err := io.ReadAll(io.LimitReader(req.Body, maxBodySize+1))
	if err != nil {
		return nil, fmt.Errorf("read webhook payload: %w", err)
	}
	if int64(len(body)) > maxBodySize {
		return nil, errBodyTooLarge
	}

	contentType := strings.ToLower(req.Header.Get("Content-Type"))
	if strings.Contains(contentType, "application/x-www-form-urlencoded") {
		parsed, err := parseFormBody(string(body))
		if err != nil {
			return nil, errInvalidPayload
		}
		return parsed, nil
	}

	var parsed recordValue
	if err := json.Unmarshal(body, &parsed); err != nil || parsed == nil {
		return nil, errInvalidPayload
	}
	return parsed, nil
}

func parseFormBody(body string) (recordValue, error) {
	parsed := recordValue{}
	for _, pair := range strings.Split(body, "&") {
		if pair == "" {
			continue
		}
		key, rawValue, _ := strings.Cut(pair, "=")
		key, err := url.QueryUnescape(key)
		if err != nil {
			return nil, err
		}
		decodedValue, err := url.QueryUnescape(rawValue)
		if err != nil {
			return nil, err
		}
		assignNested(parsed, key, decodedValue)
	}
	return parsed, nil
}

func assignNested(target recordValue, key, assignedValue string) {
	path := nestedKeyPartPattern.FindAllString(key, -1)
	if len(path) == 0 {
		return
	}

	current := target
	for _, part := range path[:len(path)-1] {
		nested, ok := current[part].(recordValue)
		if !ok {
			nested = recordValue{}
			current[part] = nested
		}
		current = nested
	}
	current[path[len(path)-1]] = assignedValue
}

func value(record recordValue, key string) any {
	if result, ok := record[key]; ok {
		return result
	}
	return missing
}

func firstRecord(values ...any) recordValue {
	for _, candidate := range values {
		if result := record(candidate); result != nil {
			return result
		}
	}
	return nil
}

func record(candidate any) recordValue {
	switch typed := candidate.(type) {
	case recordValue:
		return typed
	case map[string]any:
		return recordValue(typed)
	default:
		return nil
	}
}

func stringValue(values ...any) (string, bool) {
	for _, candidate := range values {
		switch typed := candidate.(type) {
		case string:
			if result := strings.TrimSpace(typed); result != "" {
				return result, true
			}
		case float64:
			if !math.IsNaN(typed) && !math.IsInf(typed, 0) {
				return strconv.FormatFloat(typed, 'g', -1, 64), true
			}
		}
	}
	return "", false
}

func numberValue(values ...any) (int, bool) {
	for _, candidate := range values {
		result, ok := number(candidate)
		if !ok || math.IsNaN(result) || math.IsInf(result, 0) || math.Trunc(result) != result || result < 0 || result > float64(math.MaxInt) {
			continue
		}
		return int(result), true
	}
	return 0, false
}

func number(candidate any) (float64, bool) {
	switch typed := candidate.(type) {
	case missingValue:
		return 0, false
	case nil:
		return 0, true
	case float64:
		return typed, true
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return 0, true
		}
		result, err := strconv.ParseFloat(trimmed, 64)
		return result, err == nil
	case bool:
		if typed {
			return 1, true
		}
		return 0, true
	default:
		return 0, false
	}
}

func firstValue(values ...any) any {
	for _, candidate := range values {
		if candidate == nil {
			continue
		}
		if _, isMissing := candidate.(missingValue); isMissing {
			continue
		}
		return candidate
	}
	return nil
}

func booleanValue(candidate any) bool {
	switch typed := candidate.(type) {
	case bool:
		return typed
	case float64:
		return typed != 0
	case string:
		switch strings.ToLower(typed) {
		case "1", "y", "yes", "true":
			return true
		}
	}
	return false
}
