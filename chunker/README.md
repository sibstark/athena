# Chunker

Самостоятельный этап `normalized.jsonl → chunks.jsonl`. Токенизатор E5 загружается в общий кеш `../var/models`; embedding-векторы здесь не вычисляются.

```bash
cd chunker
cp .env.example .env
bun install
bun run chunk -- \
  --input ../data/normalized/mental-arithmetic/normalized.jsonl \
  --tree ../data/normalized/mental-arithmetic/document_tree.json \
  --output ../data/normalized/mental-arithmetic/chunks.jsonl \
  --target-tokens 320 --max-tokens 450 --min-tokens 80 --overlap-tokens 50 \
  --force
```

Для тестов без загрузки модели можно передать `--tokenizer unicode-heuristic-v1`. Для запрета сети используйте `--offline`.
