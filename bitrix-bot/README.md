# Athena Bitrix24 bot

Stateless adapter for Bitrix24 Chat Bots 2.0. The bot has no database and no conversation history:

```text
Bitrix message → POST /v1/answer → Bitrix reply in the same dialog
```

The bot always sends the current message and one configured collection:

```json
{
  "query": "Что такое ментальная арифметика?",
  "collections": ["methodology_e5_small_v1"]
}
```

The RAG API does not receive Bitrix user IDs, roles, departments, or any other Bitrix context.

## Environment

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

`BITRIX_APPLICATION_TOKEN` validates incoming events. `BITRIX_BOT_TOKEN` authenticates outgoing `imbot.v2.*` calls. Keep both values server-side.

## Registering the bot

Register the bot once with `imbot.v2.Bot.register`:

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

Save the returned `result.bot.id` as `BITRIX_BOT_ID`. The webhook event is `ONIMBOTV2MESSAGEADD`; the handler supports JSON and nested form payloads. It uses `chat.dialogId` for both group and private dialogs.

## Run locally

```bash
bun install
bun test
bun run typecheck
bun run build
BITRIX_APPLICATION_TOKEN=... BITRIX_REST_WEBHOOK_URL=... BITRIX_BOT_ID=456 BITRIX_BOT_TOKEN=... bun start
```

## SSL and Nginx

The bot listens on internal HTTP only. The existing `https-proxy` terminates TLS and routes:

```text
https://devgames.hwschool.online/bitrix/bot/webhook
  → http://bitrix-bot:8080/webhook
```

No certificate is mounted into `bitrix-bot`. The Nginx `proxy_read_timeout` is 90 seconds because this MVP waits for `/v1/answer` and then sends the Bitrix response before returning HTTP 200. If the upstream request exceeds the timeout, Bitrix may deliver the event again; this MVP intentionally has no deduplication storage.

## References

- [Register a Chat Bot 2.0](https://apidocs.bitrix24.ru/api-reference/chat-bots/chat-bots-v2/imbot.v2/bots/bot-register.html)
- [Send a message](https://apidocs.bitrix24.ru/api-reference/chat-bots/chat-bots-v2/imbot.v2/messages/chat-message-send.html)
- [Webhook event mode](https://apidocs.bitrix24.ru/api-reference/chat-bots/chat-bots-v2/imbot.v2/events/event-get.html)
