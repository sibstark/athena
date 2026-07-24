# Athena

Репозиторий состоит из независимых проектов и общей инфраструктуры данных:

```text
puller/      Bitrix24 → Markdown
normalizer/  Markdown → normalized.jsonl + document_tree.json
chunker/     normalized.jsonl → chunks.jsonl
indexer/     chunks.jsonl → local E5 embeddings → Qdrant
tei/         Bun-клиенты → Rust TEI → read-only Qdrant compatibility/search
rag-api/     Go HTTP API → TEI + read-only Qdrant + DeepSeek
web/         React chat UI → same-origin nginx proxy → rag-api
data/        общие входные и выходные данные (не в Git)
var/models/  общий кеш моделей (не в Git)
var/qdrant/  данные Qdrant (не в Git)
var/tei/     кеш модели TEI (не в Git)
```

CLI не вызывают друг друга автоматически. В каждом каталоге свой `package.json`, `bun.lock` и набор зависимостей. `puller` не содержит Docker Compose и кода Qdrant.

Во всех embedding-компонентах закреплена одна модель: `Xenova/multilingual-e5-small@761b726dd34fb83930e26aab4e9ac3899aa1fa78`. Автоматическая подмена модели запрещена.

## Общий Qdrant

Единственный Compose находится в корне и предназначен для общей инфраструктуры этого репозитория. Позже сюда можно добавлять другие сервисы.

```bash
cp .env.example .env
docker compose up -d qdrant
docker compose ps qdrant
docker compose logs -f qdrant
docker compose down
```

Qdrant доступен только на `127.0.0.1:${QDRANT_PORT:-6333}`. Данные и snapshots сохраняются в `var/qdrant/`. Версия образа закреплена через `QDRANT_VERSION`.

## Rust TEI

TEI работает отдельным официальным Rust-контейнером, а `tei/` содержит только Bun/TypeScript HTTP-клиенты и read-only проверки:

```bash
bun run tei:up
bun run tei:status
bun run tei:logs
bun run tei:down
```

Подробности, smoke search и compatibility test: [tei/README.md](tei/README.md).

## Go RAG API

`rag-api/` — отдельный production HTTP-сервис. Он использует прошедший compatibility test TEI для query embeddings, читает существующую коллекцию Qdrant и вызывает DeepSeek только для `/v1/answer` при наличии релевантного контекста.

```bash
bun run rag-api:test
bun run rag-api:up
bun run rag-api:status
bun run rag-api:logs
```

Настройка, endpoint-ы и проверенные curl-примеры: [rag-api/README.md](rag-api/README.md).

## Web-интерфейс

`web/` — отдельный Bun/Vite-проект на React, Tailwind CSS v4 и shadcn/ui. Он показывает интерактивные цитаты, tooltip пути, подробную панель источника и компактные сноски. Браузер авторизуется через подписанную `HttpOnly` cookie; `RAG_API_KEY` используется только внутренними CLI и машинными клиентами.

```bash
bun run web:test
bun run web:build
bun run web:up
bun run web:status
```

После запуска Compose интерфейс доступен на `http://127.0.0.1:${WEB_PORT:-3000}`. Для production требуется внешний аутентифицирующий reverse proxy с TLS; встроенный web nginx отвечает только за статику и связь с `rag-api`. Подробности: [web/README.md](web/README.md).

## Production HTTPS

Для публичного сервера предусмотрен opt-in профиль с `umputun/nginx-le:v1.4.3`. Он публикует только порты `80` и `443`, автоматически получает и обновляет сертификат Let's Encrypt и проксирует запросы во внутренний `web:8080`. Сертификаты сохраняются в `var/nginx-le/ssl/`.

До запуска направьте DNS-запись домена на сервер, откройте входящие TCP-порты `80` и `443` и задайте в серверном `.env`:

```env
LETSENCRYPT=true
LE_FQDN=devgames.hwschool.online
LE_EMAIL=admin@example.com
TZ=UTC
AUTH_COOKIE_SECURE=true
```

Запуск и проверка:

```bash
bun run https:up
bun run https:status
bun run https:logs
curl -I https://devgames.hwschool.online/healthz
```

HTTP автоматически перенаправляется на HTTPS. Порты Qdrant, TEI и `rag-api` остаются привязаны только к `127.0.0.1`; не открывайте их в firewall. Для остановки только TLS-прокси используйте `bun run https:down`. Не удаляйте `var/nginx-le/ssl/`, если хотите сохранить выданные сертификаты.

## Полный ручной workflow

```bash
cd puller
bun run pull -- --collection 17 --out ../data/exports/mental-arithmetic

cd ../normalizer
bun run normalize -- --input ../data/exports/mental-arithmetic/content --output ../data/normalized/mental-arithmetic/normalized.jsonl --tree ../data/normalized/mental-arithmetic/document_tree.json --force

cd ../chunker
bun run chunk -- --input ../data/normalized/mental-arithmetic/normalized.jsonl --tree ../data/normalized/mental-arithmetic/document_tree.json --output ../data/normalized/mental-arithmetic/chunks.jsonl --force

cd ../indexer
bun run rag:index -- --input ../data/normalized/mental-arithmetic/chunks.jsonl --collection methodology_e5_small_v1
```

Установка и тесты выполняются отдельно в каждом проекте: `bun install`, `bun test`, `bun run typecheck`.
