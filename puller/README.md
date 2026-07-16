# Puller

Самостоятельный CLI для чтения «Базы знаний 2.0» Bitrix24 и экспорта коллекции в Markdown. Он не запускает normalizer, chunker, indexer и не управляет Qdrant.

```bash
cd puller
cp .env.example .env
bun install
bun run cli -- auth check
bun run cli -- collections list
bun run pull -- --collection 17 --out ../data/exports/mental-arithmetic
```

Входящий webhook Bitrix24 должен иметь право `note`. Секрет хранится только в `puller/.env`.
