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

## Development

```bash
bun run typecheck
bun run test
bun run build
```
