# Athena Puller Bitrix24 exporter

TypeScript CLI for exporting a Bitrix24 Knowledge Base 2.0 collection to files that are convenient for later parsing. The default target is **[M] Ментальная арифметика**, collection `17`:

`https://hwschool.bitrix24.ru/note/workspace/17/`

The exporter uses the official REST 3.0 methods `note.collection.list`, `note.document.tree.list`, and `note.document.get`. It does not automate a browser and does not store your Bitrix24 password. A standard `/rest/USER_ID/SECRET/` webhook URL is automatically converted to the REST 3.0 `/rest/api/USER_ID/SECRET/` endpoint.

## Requirements

- Bun 1.3 or newer
- An incoming Bitrix24 webhook created by a user who can view the collection
- The webhook permission for Knowledge Base 2.0 (`note`)

## Setup

1. In Bitrix24, open **Developer resources → Other → Incoming webhook**.
2. Create a webhook for your user and enable the Knowledge Base permission (`note`).
3. Copy `.env.example` to `.env` and replace the placeholder with the generated base URL:

   ```dotenv
   BITRIX_WEBHOOK_URL=https://hwschool.bitrix24.ru/rest/USER_ID/WEBHOOK_SECRET/
   ```

4. Install dependencies:

   ```bash
   bun install
   ```

The `.env` file is ignored by Git. Treat the webhook URL as a password and do not paste it into logs, commits, or screenshots.

## Commands

Check authorization and the `note` permission:

```bash
bun run cli -- auth check
```

List collections visible to the webhook user:

```bash
bun run cli -- collections list
```

Export collection `17`:

```bash
bun run cli -- export
```

Choose another collection or directory:

```bash
bun run cli -- export --collection 17 --out exports/mental-arithmetic
```

An existing export is not overwritten unless `--force` is supplied:

```bash
bun run cli -- export --force
```

## Output

```text
exports/mental-arithmetic/
├── assets/          downloaded images and file attachments
├── content/         Markdown documents preserving the Bitrix tree
├── raw/             original note.document.get responses
├── manifest.json    collection metadata and document tree
└── report.json      counts and asset download errors
```

Normal links to external web pages remain links. Images and links that look like file attachments are downloaded and rewritten to relative local paths. If a protected attachment cannot be downloaded, its original link remains in Markdown and the failure is recorded in `report.json`.

The exporter first writes to a temporary directory. A completed snapshot is moved into place only after all Bitrix documents have been received. Bitrix's `truncated` tree response is treated as a fatal error to prevent an incomplete export from looking successful.

## Normalize Markdown for RAG

Normalization is a separate step and does not change the Bitrix export. It first scans Markdown files and directories to reconstruct the exported page tree, then removes YAML Front Matter from document content and normalizes formatting outside code blocks. The result is ready for later chunking and indexing; this step does not create embeddings.

```bash
bun run cli -- normalize \
  --input exports/mental-arithmetic/content \
  --output normalized/mental-arithmetic/normalized.jsonl \
  --tree normalized/mental-arithmetic/document_tree.json \
  --report normalized/mental-arithmetic/report.json \
  --short-note-threshold 200
```

Use `--force` to replace existing results. When omitted, `document_tree.json` and `report.json` are created next to the JSONL file.

Example JSONL line:

```json
{"document_id":"page_3693","parent_document_id":"page_3663","content":"# Сценарий урока\n\nТекст","metadata":{"title":"МЕ.0.2. 3. Цифра 6","source":"ментальная-арифметика-new--3035/уровень--3499/раздел--3663/урок--3693.md","page_id":"3693","parent_page_id":"3663","root_page_id":"3035","depth":3,"ancestor_ids":["3035","3499","3663"],"breadcrumbs":["Ментальная арифметика New","Уровень","Раздел","МЕ.0.2. 3. Цифра 6"],"context_text":"Ментальная арифметика New → Уровень → Раздел → МЕ.0.2. 3. Цифра 6","document_type":"short_note","indexable":true,"external_urls":[],"assets":[],"character_count":30,"word_count":4,"heading_count":1,"external_url_count":0,"asset_count":0,"version":null,"date":null,"category":null,"status":"active","language":"ru"},"content_hash":"<sha256>"}
```

Page IDs encoded as `--1234` in file and directory names become stable IDs such as `page_1234`; files without a page ID use a hash of their relative source path. `content_hash` depends only on normalized Markdown. Exact duplicates remain in JSONL and receive `metadata.duplicate_of`, preserving their navigation position. Empty documents are omitted, external-link-only documents are marked non-indexable, and per-file errors do not stop the remaining import. `document_tree.json` also contains virtual directory nodes that have no Markdown document.

## Split normalized documents into RAG chunks

Chunking is a separate deterministic stage. It reads normalized documents, validates their links against the document tree, and splits only `indexable: true` Markdown into semantic sections. It does not create embeddings or connect to a vector database.

```bash
bun run cli -- chunk \
  --input normalized/mental-arithmetic/normalized.jsonl \
  --tree normalized/mental-arithmetic/document_tree.json \
  --output normalized/mental-arithmetic/chunks.jsonl \
  --report normalized/mental-arithmetic/chunk_report.json \
  --target-tokens 320 \
  --max-tokens 450 \
  --min-tokens 80 \
  --overlap-tokens 50
```

Use `--force` to replace existing results. When `--report` is omitted, `chunk_report.json` is created next to `chunks.jsonl`.

The chunker prefers headings, horizontal rules, paragraphs, list items, and sentences as boundaries. Fenced code blocks and individual list items are never split. Large tables are divided by rows with the header repeated in each part. Overlap is added only when one section must be split and consists of complete paragraphs or sentences.

Each JSONL line contains the original Markdown fragment in `content`, a separate contextual `embedding_text`, stable hashes and IDs, inherited tree metadata, heading context, neighboring chunk IDs, URLs, and assets. Exact duplicate chunks are retained and marked with `metadata.duplicate_of_chunk_id`.

By default, token limits are calculated with the pinned `Xenova/multilingual-e5-small` tokenizer. This leaves room for the `passage:` prefix and model special tokens under E5's 512-token limit. The tokenizer is cached under `var/models`. Use `--offline` after the first download, or explicitly select the deterministic fallback with `--tokenizer unicode-heuristic-v1`.

Example JSONL line:

```json
{"chunk_id":"chunk_<sha256>","document_id":"page_3693","parent_document_id":"page_3663","content":"## **4. Правильная постановка рук**\n\nТекст раздела","embedding_text":"Путь: Ментальная арифметика New → Ментальная арифметика для 6–8 лет → М.1. Простой счет → МЕ.0.2. 3. Цифра 6\nДокумент: МЕ.0.2. 3. Цифра 6\nРаздел: Сценарий урока → 4. Правильная постановка рук\n\nТекст раздела","content_hash":"<sha256>","embedding_text_hash":"<sha256>","metadata":{"title":"МЕ.0.2. 3. Цифра 6","source":"ментальная-арифметика-new--3035/.../ме.0.2.-3.цифра-6--3693.md","page_id":"3693","parent_page_id":"3663","root_page_id":"3035","depth":3,"ancestor_ids":["3035","3499","3663"],"breadcrumbs":["Ментальная арифметика New","Ментальная арифметика для 6–8 лет","М.1. Простой счет","МЕ.0.2. 3. Цифра 6"],"heading_path":["Сценарий урока","4. Правильная постановка рук"],"section_title":"4. Правильная постановка рук","heading_level":2,"section_part":1,"section_parts_total":1,"chunk_index":4,"chunk_count":12,"token_count":542,"character_count":2134,"contains_table":false,"contains_code":false,"oversized":false,"assets":[],"external_urls":[],"language":"ru","category":null,"version":null,"date":null,"status":"active"},"previous_chunk_id":"chunk_<sha256>","next_chunk_id":"chunk_<sha256>"}
```

## Index chunks in local Qdrant

The indexer is a fourth independent CLI stage. It streams `chunks.jsonl`, creates local multilingual E5 embeddings through Transformers.js, and writes them to Qdrant. It does not invoke the exporter, normalizer, or chunker.

Copy the Qdrant and embedding settings from `.env.example`, then start the pinned Qdrant container:

```bash
bun run db:up
bun run db:status
bun run db:logs
```

Qdrant listens only on `127.0.0.1`. Persistent storage and snapshots are kept under the ignored `var/qdrant/` directory. Stop it with `bun run db:down`.

The first online run downloads the pinned model and tokenizer into `var/models`:

```bash
bun run rag:index -- \
  --input normalized/mental-arithmetic/chunks.jsonl \
  --collection methodology_e5_small_v1 \
  --batch-size 16
```

Run the same command again for incremental indexing. Unchanged chunks skip both the model and Qdrant writes. Useful modes:

```bash
# Validate input and show the diff without loading the ONNX model or writing data
bun run rag:index -- --input normalized/mental-arithmetic/chunks.jsonl --dry-run

# Use only files already present in var/models
bun run rag:index -- --input normalized/mental-arithmetic/chunks.jsonl --offline

# Recalculate all vectors
bun run rag:index -- --input normalized/mental-arithmetic/chunks.jsonl --force-reembed

# Delete managed Qdrant points no longer present in chunks.jsonl
bun run rag:index -- --input normalized/mental-arithmetic/chunks.jsonl --prune
```

`--prune` is disabled automatically if any input line is invalid. Without `--prune`, the indexer never deletes points. Reports are written next to the input as `index-report.<collection>.json` unless `--report` is supplied. Set `STORE_EMBEDDING_TEXT=true` only for debugging; `content` is sufficient for later answers.

The model uses `passage: ` for documents. A future query tool must use the same model and revision with the `query: ` prefix; vectors from different models are not comparable. To change the model, revision, dimension, or pipeline version, choose a new collection instead of reusing an existing one.

To move Qdrant to a server, copy its persistent storage or restore a snapshot, expose it behind TLS/authentication, then set `QDRANT_URL` and `QDRANT_API_KEY` on the CLI host. Never commit the API key.

Full local workflow:

```bash
bun run db:up
bun run cli -- export
bun run cli -- normalize --input exports/mental-arithmetic/content --output normalized/mental-arithmetic/normalized.jsonl
bun run cli -- chunk --input normalized/mental-arithmetic/normalized.jsonl --tree normalized/mental-arithmetic/document_tree.json --output normalized/mental-arithmetic/chunks.jsonl
bun run rag:index -- --input normalized/mental-arithmetic/chunks.jsonl
```

## Development

```bash
bun run typecheck
bun run test
bun run test:indexer:integration
bun run build
```
