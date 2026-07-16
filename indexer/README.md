# Indexer

Самостоятельный этап `chunks.jsonl → локальные E5 embeddings → Qdrant`.

```bash
cd indexer
cp .env.example .env
bun install
bun run rag:index -- \
  --input ../data/normalized/mental-arithmetic/chunks.jsonl \
  --collection methodology_e5_small_v1
```

При первом запуске Transformers.js скачивает закреплённую ревизию `Xenova/multilingual-e5-small` в `../var/models`. `--offline` запрещает скачивание, `--dry-run` не загружает модель и не пишет в Qdrant, `--force-reembed` пересчитывает векторы, `--prune` удаляет только отсутствующие управляемые точки.

Документы индексируются с префиксом `passage:`. Будущие запросы должны использовать ту же модель и префикс `query:`. Для другой embedding-модели создавайте новую коллекцию, чтобы не смешивать несовместимые векторы.
