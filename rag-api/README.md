# Athena RAG API

Отдельный stateless Go-сервис объединяет существующие Rust TEI и Qdrant с внешним DeepSeek Chat Completions API. Сервис не записывает данные в Qdrant и не запускает fetcher, normalizer, chunker или indexer. Он не знает о Bitrix, ролях и вызывающих приложениях: клиент сам передаёт коллекции, в которых нужно искать.

```text
GET  /v1/collections → Qdrant read-only collection list
POST /v1/search → TEI query embedding → Qdrant read-only search
POST /v1/answer → тот же retrieval → ограниченный контекст → DeepSeek → проверка [S1]
```

## Embedding contract

Полный контракт модели задаётся только обязательными `EMBEDDING_*` в корневом `.env`. Перед embedding клиент сверяет `/info`, token limit и параметры коллекции Qdrant с этим контрактом. Значений модели и fallback в Go-коде нет.

## Конфигурация

Скопируйте корневой `.env.example` в `.env` и заполните как минимум:

```env
RAG_API_KEY=<длинный случайный Bearer token>
DEEPSEEK_API_KEY=<ключ DeepSeek>
AUTH_USERNAME=admin
AUTH_PASSWORD_HASH='<bcrypt-хеш>'
AUTH_SESSION_SECRET=<случайный секрет минимум 32 байта>
AUTH_SESSION_TTL=24h
AUTH_COOKIE_SECURE=false
```

Обычный пароль не сохраняется. Создайте bcrypt-хеш интерактивной командой: пароль дважды читается из терминала без echo и никогда не передаётся в argv.

```bash
bun run rag-api:hash-password
```

Создайте независимый session secret минимум из 32 случайных байт:

```bash
openssl rand -base64 32
```

Запишите результаты только в локальный `.env`. Bcrypt-хеш заключите в одинарные кавычки, чтобы символы `$` обрабатывались буквально. Не коммитьте пароль, хеш или session secret.

Основные настройки:

| Переменная | Default |
|---|---|
| `HTTP_ADDR` | code default `:8080`; root `.env.example` uses `:8090` for host run |
| `RAG_API_PORT` | `8090` (host Docker) |
| `AUTH_SESSION_TTL` | `24h` |
| `AUTH_COOKIE_SECURE` | `false` локально; `true` только с HTTPS |
| `TEI_URL` | `http://127.0.0.1:8080` на host |
| `QDRANT_URL` | `http://127.0.0.1:6333` на host |
| `DEEPSEEK_BASE_URL` | `https://api.deepseek.com` |
| `DEEPSEEK_MODEL` | `deepseek-v4-flash` |
| `DEEPSEEK_THINKING_ENABLED` | `false` |
| `DEEPSEEK_MAX_OUTPUT_TOKENS` | `1200` |
| `DEEPSEEK_MAX_CONCURRENCY` | `4` |
| `RAG_DEFAULT_TOP_K` / `RAG_MAX_TOP_K` | `8` / `20` |
| `RAG_SCORE_THRESHOLD` | `0.65` |
| `RAG_MAX_QUERY_CHARS` | `4000` |
| `RAG_MAX_CONTEXT_CHARS` | `30000` |
| `RAG_MAX_CONTEXT_CHUNKS` | `8` |

В Compose внутренние URL автоматически заменяются на `http://embeddings:80` и `http://qdrant:6333`. CORS выключен, пока `CORS_ALLOWED_ORIGINS` не содержит явный CSV-список origins.

## Запуск

```bash
cp .env.example .env
# заполнить RAG_API_KEY, DEEPSEEK_API_KEY и AUTH_*

bun run db:up
bun run tei:up
bun run rag-api:up
bun run rag-api:status
bun run rag-api:logs
```

Локальный запуск без Docker для API:

```bash
bun run rag-api:run
```

Health и metrics не требуют Bearer token:

```bash
curl http://127.0.0.1:8090/health/live
curl http://127.0.0.1:8090/health/ready
curl http://127.0.0.1:8090/metrics
```

## Авторизация пользователя

Браузер использует только подписанную `HttpOnly` cookie `athena_session`. Сессия не сохраняется в БД: cookie содержит имя пользователя и срок действия, кодируется base64url и подписывается HMAC-SHA256. Cookie имеет `SameSite=Strict`, `Path=/`, ограниченный TTL и флаг `Secure`, когда `AUTH_COOKIE_SECURE=true`.

```text
POST /v1/auth/login
GET  /v1/auth/session
POST /v1/auth/logout
```

Пример ручной проверки с cookie jar:

```bash
curl -sS -c /tmp/athena-cookies.txt http://127.0.0.1:8090/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"your-password"}'

curl -sS -b /tmp/athena-cookies.txt http://127.0.0.1:8090/v1/auth/session
curl -sS -b /tmp/athena-cookies.txt -X POST http://127.0.0.1:8090/v1/auth/logout
```

После пяти неудачных входов с одного IP новые попытки блокируются на 15 минут; успешный вход очищает счётчик. Неверный логин и неверный пароль всегда возвращают одинаковую ошибку.

## Коллекции Qdrant

Защищённый метод возвращает отсортированный список всех коллекций Qdrant. Дополнительные запросы статистики к отдельным коллекциям не выполняются.

```bash
curl -sS http://127.0.0.1:8090/v1/collections \
  -H "Authorization: Bearer ${RAG_API_KEY}"
```

```json
{
  "request_id": "8a4dd23d-9c88-44a6-9896-46829a01bc76",
  "collections": ["workspace_1", "workspace_7", "workspace_41"]
}
```

Пустой Qdrant возвращает `"collections": []`. Метод доступен также с действующей пользовательской session cookie, не вызывает TEI или DeepSeek и ничего не записывает в Qdrant.

## Поиск

```bash
curl -sS http://127.0.0.1:8090/v1/search \
  -H "Authorization: Bearer ${RAG_API_KEY}" \
  -H 'Content-Type: application/json' \
  -d '{
    "query": "Как провести урок по цифре 5?",
    "collections": ["methodology_e5_small_v1"],
    "top_k": 8,
    "score_threshold": 0.65,
    "filters": {
      "root_page_id": "3035",
      "language": "ru",
      "status": "active"
    }
  }'
```

Поддерживаются только реальные плоские payload-поля Qdrant: `document_id`, `parent_document_id`, `page_id`, `parent_page_id`, `root_page_id`, `language`, `category`, `status`. Вложенные `metadata.*` не принимаются. Поле `collections` выбирает коллекции Qdrant; оно не является ACL и не содержит контекста пользователя.

`collections` обязательно для `/v1/search` и `/v1/answer` и должно содержать хотя бы одно непустое имя. Если поле отсутствует, равно `null` или передано как `[]`, API возвращает `400 invalid_request` и не вызывает TEI, Qdrant или DeepSeek. Получить доступные имена можно через `GET /v1/collections`.

## Ответ с источниками

```bash
curl -sS http://127.0.0.1:8090/v1/answer \
  -H "Authorization: Bearer ${RAG_API_KEY}" \
  -H 'Content-Type: application/json' \
  -d '{"query":"Как провести урок по цифре 5?","collections":["methodology_e5_small_v1"],"top_k":8}'
```

`/v1/answer` отправляет во внешний DeepSeek API вопрос и только выбранные после threshold/deduplication чанки. Если контекста нет, DeepSeek не вызывается. Цитаты проверяются; metadata источников всегда берётся из Qdrant, а не из текста модели. Если payload содержит `document_url`, `/v1/search` и `/v1/answer` возвращают его как поле `url` соответствующего источника.

### Поиск по нескольким коллекциям

`collections` может содержать несколько имён:

```json
{
  "query": "Какие правила начисления зарплаты?",
  "collections": [
    "methodology_e5_small_v1",
    "salary_e5_small_v1"
  ]
}
```

TEI вызывается один раз. Затем API параллельно выполняет поиск в каждой переданной коллекции, объединяет результаты, удаляет дубли и формирует общий контекст. API не выбирает коллекции автоматически: вызывающий клиент обязан явно передать весь список для конкретного запроса.

## Тесты и сборка

```bash
bun run rag-api:test
bun run rag-api:build

bun run rag-api:test:integration  # реальные TEI + Qdrant
bun run rag-api:test:deepseek     # платный opt-in DeepSeek test
```

Compatibility test перед эксплуатацией:

```bash
bun run rag:compat:tei -- \
  --chunks data/normalized/mental-arithmetic/chunks.jsonl \
  --collection methodology_e5_small_v1 \
  --queries data/eval/queries.jsonl \
  --sample-size 100
```

Начальный threshold `0.65` не является универсальным. Подбирайте его на реальных вопросах: измеряйте долю `insufficient_context`, релевантность top-K и ошибки ответов, затем меняйте `RAG_SCORE_THRESHOLD` без переиндексации.

## Безопасность

- `/v1/search` и `/v1/answer` принимают либо пользовательскую session cookie, либо `RAG_API_KEY` для внутренних CLI и машинных клиентов.
- Выбор коллекций является входным параметром доверенного клиента; API не определяет права пользователя и не интерпретирует роли.
- Auth endpoints, healthcheck и metrics не требуют Bearer token.
- Пароли хранятся только как bcrypt-хеши; session cookie подписывается HMAC-SHA256 и проверяется constant-time сравнением.
- Секреты, вопросы, контекст, ответы и vectors не пишутся в штатные логи.
- Compose публикует API, Qdrant и TEI только на `127.0.0.1`.
- Qdrant-клиент сервиса не имеет методов записи.
- Docker runtime — `scratch`, binary запускается как UID/GID `65532`, builder image закреплён digest.
- Streaming, история диалога и tool calling в первой версии отсутствуют.

На сервере обязательно используйте HTTPS и `AUTH_COOKIE_SECURE=true`. Не публикуйте наружу порты TEI, Qdrant или внутренний порт `rag-api`; Compose по умолчанию привязывает host-порты только к `127.0.0.1`.
