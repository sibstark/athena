# Chunker

TypeScript-этап `normalized_<id> → chunks_<id>`. Он последовательно обрабатывает все workspace из `normalization_manifest.json`, не вычисляет embedding-векторы и не обращается к Hugging Face.

```bash
cd chunker
bun install
bun run chunk -- \
  --run ../data/normalized/normalized_1787220682160 \
  --out ../data/chunks/chunks_1787220682160 \
  --tokenizer-cache ../var/models
```

Имена обязаны образовывать одну цепочку: `run_<id> → normalized_<id> → chunks_<id>`. В `chunks_<id>` создаются `chunks_manifest.json`, общий `chunks_report.json` и отдельные `workspace_<id>/chunks.jsonl` с `chunk_report.json`.

Chunker использует только закреплённый `Xenova/multilingual-e5-small@761b726dd34fb83930e26aab4e9ac3899aa1fa78`. `tokenizer.json` и `tokenizer_config.json` читаются напрямую из общего кеша и проверяются по SHA-256; сетевого fallback нет. Путь задаётся через `--tokenizer-cache`, `EMBEDDING_CACHE_DIR` или по умолчанию `../var/models`.

Каждый `embedding_text` проверяется тем же токенизатором и не может превышать `maxTokens` (по умолчанию 450). Длинные атомарные значения, строки таблиц, code- и HTML-блоки принудительно делятся, поэтому indexer не получает входы длиннее лимита embedding-модели.

Если отдельный workspace повреждён, остальные продолжают обрабатываться. Его диагностический отчёт сохраняется без частичного `chunks.jsonl`, общий run публикуется со статусом `complete_with_errors`, а команда завершается ненулевым кодом. Пустой успешный workspace получает пустой `chunks.jsonl`, чтобы следующий index удалил устаревшие точки коллекции.

Для ручной проверки одного workspace сохраняется совместимый режим:

```bash
bun run chunk -- \
  --input ../data/normalized/normalized_1787220682160/workspace_17/normalized.jsonl \
  --tree ../data/normalized/normalized_1787220682160/workspace_17/document_tree.json \
  --output /tmp/workspace_17/chunks.jsonl \
  --report /tmp/workspace_17/chunk_report.json \
  --tokenizer-cache ../var/models \
  --force
```

Проверка:

```bash
bun run typecheck
bun run test
```
