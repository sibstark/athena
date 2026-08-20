# Fetcher

Read-only exporter for Bitrix24 Knowledge Base 2.0. The project is written in Go 1.26.4 and stores every invocation as a separate immutable run.

## Setup

The incoming Bitrix24 webhook must have the `note` scope.

```bash
cd fetcher
cp .env.example .env
go mod download
go run ./cmd/fetcher auth check
go run ./cmd/fetcher workspaces list
```

`BITRIX_WEBHOOK_URL` may also be supplied directly through the environment. The webhook secret is never written to manifests, reports, or progress messages.

## Fetching

Export every workspace, including empty workspaces:

```bash
go run ./cmd/fetcher fetch --out ../data/fetcher
```

Export selected workspaces without downloading binary assets:

```bash
go run ./cmd/fetcher fetch \
  --out ../data/fetcher \
  --include 27,17 \
  --skip-assets
```

`--include` and `--exclude` accept comma-separated values and may be repeated. Unknown IDs and IDs present in both options are errors. Existing runs are never replaced, so there is no `--force` option.

## Archive layout

```text
data/fetcher/
├── runs.json
├── run_1787227200123/
│   ├── manifest.json
│   ├── report.json
│   └── workspace_27/
│       ├── assets/
│       └── document_4869/
│           ├── content.md
│           └── document_2983/
│               └── content.md
└── run_1787227900456/
    └── ...
```

Run IDs use Unix time in milliseconds. `runs.json` is rebuilt from finalized `run_*` directories after every invocation, so an orphaned run is recovered automatically. Concurrent writes to the same archive are rejected.

A successful `manifest.json` contains the complete recursive structure, relative local paths, `qdrantCollection: "workspace_<id>"`, and absolute Bitrix URLs for every workspace and document. Each `content.md` has YAML front matter with stable IDs, timestamps, `workspace_url`, and `document_url`.

If a workspace tree or document fails, the run directory contains only a redacted `report.json` with `status: "failed"`. Asset failures preserve the original remote URL and produce `complete_with_warnings`. Use `--skip-assets` when only the text snapshot is needed.

## Bitrix API findings

The official Bitrix MCP documentation describes the following Knowledge Base 2.0 REST 3.0 surface:

| Area | Available operations |
|---|---|
| `note.collection.*` | add, update, get, list, delete, archive, field get/list |
| `note.document.*` | add, update, get, delete, archive, tree list, search list, document/tree/search field get/list |
| `note.file.*` | add, get, delete, field get/list |

Fetcher only calls `note.collection.list`, `note.document.tree.list`, and `note.document.get`.

Important API constraints:

- `note.collection.list` uses cursor pagination with a maximum page size of 200.
- `note.document.tree.list` excludes documents archived through the API.
- A tree is marked `truncated=true` above Bitrix's internal limit of 5000 nodes. Fetcher rejects such a snapshot.
- `note.document.search.list` requires a search phrase and cannot enumerate a complete workspace.
- Tests against this portal confirmed that legacy `batch` reports `note.*` as unknown and REST 3.0 does not expose `rest.batch`. Fetcher therefore uses individual document requests with bounded concurrency and retry/backoff.

## Verification

```bash
go test ./...
go test -race ./...
go vet ./...
go build ./cmd/fetcher
```
