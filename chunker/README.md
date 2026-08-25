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

Ошибка отдельного документа не блокирует workspace: документ исключается, валидные чанки публикуются, а workspace и общий run получают `complete_with_warnings`. Подробности остаются в `error_documents` файла `chunk_report.json`, команда завершается с нулевым кодом. Если не удалось обработать ни одного документа либо повреждены артефакты workspace целиком, `chunks.jsonl` не публикуется, статус остаётся `failed`, а команда возвращает ненулевой код. Пустой успешный workspace получает пустой `chunks.jsonl`, чтобы следующий index удалил устаревшие точки коллекции.

`chunks_manifest.json` версии 2 различает `complete`, `complete_with_warnings` и `failed`. Ошибочный документ отсутствует в JSONL, поэтому штатный prune indexer удаляет и его старые точки из Qdrant.

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
