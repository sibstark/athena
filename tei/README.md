# Athena TEI tools

`tei/` содержит Bun/TypeScript HTTP-клиенты для официального Rust-сервиса Hugging Face Text Embeddings Inference. Production inference выполняет только TEI; инструменты делают smoke search и read-only compatibility-проверки Qdrant.

## Единый embedding contract

Model ID, exact revision, dimension, dtype, pooling, normalization, document/query prefixes, max input, auto-truncate, pipeline version и point namespace обязательны в корневом `.env`. Fallback-модели нет. Текущая конфигурация использует `Snowflake/snowflake-arctic-embed-m-v2.0@95c2741480856aa9666782eb4afe11959938017f`, CLS pooling, 768 координат, пустой document prefix и `query: ` для запросов.

Compose явно закрепляет:

- `ghcr.io/huggingface/text-embeddings-inference:cpu-1.9.3@sha256:c26a226262ad4ff3330fb30b76653c1bb65da2fcf413b92284545a010e0a8a48`;
- `platform: linux/amd64`;
- Qdrant `v1.19.0`.

Переменных `TEI_IMAGE`, `TEI_PLATFORM` и `QDRANT_VERSION` нет.

## Запуск

```bash
cd ..
bun run db:up
bun run tei:up
bun run db:status
bun run tei:status
curl -fsS http://127.0.0.1:8080/health
curl -fsS http://127.0.0.1:8080/info
```

Модель скачивается TEI в `var/tei`. Chunker использует только два tokenizer-файла из отдельного проверяемого кеша; подготовьте их явно:

```bash
bun run chunker:tokenizer
```

## Smoke search

```bash
bun run rag:search:tei -- \
  --query "Что делать, если у ребёнка нет абакуса?" \
  --collection workspace_17 \
  --limit 10
```

Команда добавляет ровно один configured query prefix, проверяет TEI и metadata коллекции, затем выполняет только чтение.

## Compatibility test

```bash
bun run rag:compat:tei -- \
  --chunks data/chunks/chunks_<id>/workspace_17/chunks.jsonl \
  --collection workspace_17 \
  --queries tei/data/eval/queries.jsonl \
  --sample-size 100 \
  --seed 42 \
  --top-k 10 \
  --batch-size 8 \
  --report reports/tei-compatibility.json \
  --offline
```

Инструмент не создаёт, не обновляет и не удаляет точки. `PASS_STRICT` означает точное совпадение revision/pipeline и прохождение численных и retrieval-порогов; `WARN`/`FAIL` не меняют коллекцию.
