# Athena TEI compatibility tools

`tei/` — отдельный Bun/TypeScript-проект для работы с официальным Rust-сервисом Hugging Face Text Embeddings Inference. TypeScript не выполняет production inference: он вызывает TEI по HTTP, делает read-only поиск в Qdrant и сравнивает результаты с существующим Transformers.js pipeline.

Во всех режимах разрешена только модель:

```text
Xenova/multilingual-e5-small@761b726dd34fb83930e26aab4e9ac3899aa1fa78
```

Подмена на `intfloat/multilingual-e5-small` запрещена.

## Запуск

Официальная документация перечисляет ARM64 image, но на момент проверки тег `cpu-arm64-1.9` отсутствовал в GHCR (`manifest unknown`). Поэтому на Apple Silicon используется официальный amd64 image через Docker-эмуляцию:

```env
TEI_IMAGE=ghcr.io/huggingface/text-embeddings-inference:cpu-1.9@sha256:ad950d30878eceb72aaf32024d26fa2b1d04a75304fa0b4776b49aa1941fea07
TEI_PLATFORM=linux/amd64
```

На x86_64:

```env
TEI_IMAGE=ghcr.io/huggingface/text-embeddings-inference:cpu-1.9@sha256:ad950d30878eceb72aaf32024d26fa2b1d04a75304fa0b4776b49aa1941fea07
TEI_PLATFORM=linux/amd64
```

```bash
cd ..
bun run db:up
bun run tei:up
bun run db:status
bun run tei:status
curl -fsS http://127.0.0.1:8080/health
curl -fsS http://127.0.0.1:8080/info
```

`tei:up` сначала выполняет `tei:prepare`: берёт закреплённый `onnx/model_quantized.onnx` q8 из общего кеша `var/models`, размещает его в TEI Hub cache под ожидаемым именем `onnx/model.onnx`, записывает SHA-256 manifest и пересоздаёт только контейнер `embeddings`. Model ID/revision не меняются.

Кеш TEI хранится в `var/tei`, существующий q8-кеш Transformers.js — в `var/models`. После первого pull получите digest:

```bash
docker image inspect "$TEI_IMAGE" --format '{{index .RepoDigests 0}}'
```

Проверенный digest уже закреплён в Compose, `.env.example` и `tei/image-lock.json`. При обновлении TEI повторите inspect и compatibility test.

## Smoke search

```bash
bun run rag:search:tei -- \
  --query "Что делать, если у ребёнка нет абакуса?" \
  --collection methodology_e5_small_v1 \
  --limit 10
```

Команда добавляет `query: `, проверяет точную модель/revision и выполняет только чтение.

## Compatibility test

```bash
bun run rag:compat:tei -- \
  --chunks data/normalized/mental-arithmetic/chunks.jsonl \
  --collection methodology_e5_small_v1 \
  --queries tei/data/eval/queries.jsonl \
  --sample-size 100 \
  --seed 42 \
  --top-k 10 \
  --batch-size 8 \
  --report reports/tei-compatibility.json \
  --offline
```

Результаты: JSON и Markdown. Команда не экспонирует методы Qdrant для создания, обновления или удаления данных. До и после теста сравниваются status, config, payload schema и points count.

- `PASS_STRICT`: revision и pipeline подтверждены, численные и retrieval-пороги пройдены.
- `PASS`: поиск совместим при допустимых численных отличиях.
- `WARN`: часть сведений неизвестна или проверка неполна.
- `FAIL`: модель, размерность, truncation или качество поиска несовместимы.

При `WARN`/`FAIL` существующая коллекция не меняется. Переиндексация этим проектом не выполняется.

## Почему одна модель обязательна

Документные и query embeddings должны находиться в одном векторном пространстве. Chunker, Go-indexer в `fetcher`, TEI и Go RAG API используют один model ID, revision, pooling, normalization и E5-префиксы. Для другой модели создаётся новая коллекция после отдельного решения.

Go RAG API вызывает `POST /embed` TEI с `query: <вопрос>`, проверяет 384 координаты и выполняет поиск в Qdrant; Go-indexer использует тот же endpoint с `passage: <документ>`.
