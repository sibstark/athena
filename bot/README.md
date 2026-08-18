# Athena Bitrix24 bot

Go-адаптер для Bitrix24 Chat Bots 2.0 без собственной базы данных и истории диалога:

```text
Bitrix message → POST /v1/answer → Bitrix reply in the same dialog
```

Бот отправляет в RAG API текущее сообщение и одну настроенную коллекцию:

```json
{
  "query": "Что такое ментальная арифметика?",
  "collections": ["methodology_e5_small_v1"]
}
```

Идентификаторы пользователей, роли, отделы и другой контекст Bitrix в RAG API не передаются.

## Настройка Compose

Корневой `docker-compose.yml` преобразует существующие переменные окружения в имена, которые ожидает Go-приложение:

```env
BITRIX_BOT_PORT=8080
BITRIX_WEBHOOK_PATH=/webhook
BITRIX_APPLICATION_TOKEN=the-application-token-from-webhook-event
BITRIX_REST_WEBHOOK_URL=https://portal.bitrix24.ru/rest/1/webhook-code/
BITRIX_BOT_ID=456
BITRIX_BOT_TOKEN=the-bot-token-used-at-registration
RAG_API_URL=http://rag-api:8080
RAG_API_KEY=internal-rag-token
BITRIX_BOT_COLLECTION=methodology_e5_small_v1
BOT_REQUEST_TIMEOUT_MS=75000
```

`BITRIX_APPLICATION_TOKEN` проверяет входящие события, а `BITRIX_BOT_TOKEN` подписывает исходящие вызовы `imbot.v2.*`. Оба значения должны оставаться только на сервере.

Запуск и диагностика:

```bash
docker compose up -d --build bot
docker compose ps bot
docker compose logs -f bot
docker compose stop bot
```

JSON-логи выводятся в stdout и в почасовые файлы с хранением за семь дней. Файлы сохраняются в именованном volume `bot_logs`.

## Регистрация бота

Однократно зарегистрируйте бота через `imbot.v2.Bot.register`:

```bash
curl -X POST \
  -H 'Content-Type: application/json' \
  -d '{
    "fields": {
      "code": "athena_bot",
      "botToken": "the-bot-token-used-at-registration",
      "properties": {"name": "Athena"},
      "type": "bot",
      "eventMode": "webhook",
      "webhookUrl": "https://devgames.hwschool.online/bitrix/bot/webhook"
    }
  }' \
  'https://portal.bitrix24.ru/rest/1/webhook-code/imbot.v2.Bot.register'
```

Сохраните `result.bot.id` из ответа как `BITRIX_BOT_ID`. Бот обрабатывает событие `ONIMBOTV2MESSAGEADD`, принимает JSON и вложенные form-параметры и отвечает в `chat.dialogId` как для групповых, так и для личных диалогов.

## Локальный запуск

Для запуска без Compose приложение ожидает внутренние имена переменных:

```bash
cd bot
go test ./...
PORT=8080 \
WEBHOOK_PATH=/webhook \
LOG_DIR=./logs \
APPLICATION_TOKEN=... \
REST_WEBHOOK_URL=... \
BOT_ID=456 \
BOT_TOKEN=... \
API_URL=http://127.0.0.1:8090 \
API_KEY=... \
COLLECTION=methodology_e5_small_v1 \
REQUEST_TIMEOUT_MS=75000 \
go run ./cmd
```

Liveness endpoint: `GET http://127.0.0.1:8080/health/live`.

## HTTPS и обработка webhook

TLS завершается в сервисе `https-proxy`, который сохраняет публичный маршрут:

```text
https://devgames.hwschool.online/bitrix/bot/webhook
  → http://bot:8080/webhook
```

После проверки payload и токена бот сразу отвечает Bitrix HTTP 200, а запрос к RAG API и отправку сообщения выполняет асинхронно. При ошибке RAG API бот пытается отправить стандартное сообщение-заглушку. Хранилища дедупликации в сервисе нет.

## Ссылки

- [Register a Chat Bot 2.0](https://apidocs.bitrix24.ru/api-reference/chat-bots/chat-bots-v2/imbot.v2/bots/bot-register.html)
- [Send a message](https://apidocs.bitrix24.ru/api-reference/chat-bots/chat-bots-v2/imbot.v2/messages/chat-message-send.html)
- [Webhook event mode](https://apidocs.bitrix24.ru/api-reference/chat-bots/chat-bots-v2/imbot.v2/events/event-get.html)
