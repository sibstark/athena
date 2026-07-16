# Normalizer

Самостоятельный этап `Markdown → normalized.jsonl + document_tree.json`.

```bash
cd normalizer
bun install
bun run normalize -- \
  --input ../data/exports/mental-arithmetic/content \
  --output ../data/normalized/mental-arithmetic/normalized.jsonl \
  --tree ../data/normalized/mental-arithmetic/document_tree.json \
  --report ../data/normalized/mental-arithmetic/normalization_report.json \
  --force
```
