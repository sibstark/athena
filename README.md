# Athena

Репозиторий состоит из четырёх независимых Bun/TypeScript CLI и общей инфраструктуры данных:

```text
puller/      Bitrix24 → Markdown
normalizer/  Markdown → normalized.jsonl + document_tree.json
chunker/     normalized.jsonl → chunks.jsonl
indexer/     chunks.jsonl → local E5 embeddings → Qdrant
data/        общие входные и выходные данные (не в Git)
var/models/  общий кеш моделей (не в Git)
var/qdrant/  данные Qdrant (не в Git)
```

CLI не вызывают друг друга автоматически. В каждом каталоге свой `package.json`, `bun.lock` и набор зависимостей. `puller` не содержит Docker Compose и кода Qdrant.

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
