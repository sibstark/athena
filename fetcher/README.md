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

## Normalization

The Go normalizer is a separate command inside this project. It reads a finalized immutable `run_*` through its `manifest.json` and writes derived data to a different directory; source runs are never modified.

Normalize every workspace:

```bash
go run ./cmd/fetcher normalize \
  --run ../data/fetcher/run_1787220682160 \
  --out ../data/normalized/run_1787220682160
```

Normalize selected workspaces and atomically replace an existing result:

```bash
go run ./cmd/fetcher normalize \
  --run ../data/fetcher/run_1787220682160 \
  --out ../data/normalized/run_1787220682160 \
  --include 17,27 \
  --short-note-threshold 200 \
  --force
```

`--include` and `--exclude` have the same repeatable, comma-separated semantics as `fetch`. The output directory must not contain the source run or be located inside it.

```text
data/normalized/run_1787220682160/
├── normalization_manifest.json
├── normalization_report.json
├── workspace_17/
│   ├── normalized.jsonl
│   ├── document_tree.json
│   └── normalization_report.json
└── workspace_27/
    └── ...
```

The source manifest, rather than directory names, is authoritative for Bitrix IDs, parents, ordering, paths, and Qdrant collection names. `normalized.jsonl` and `document_tree.json` retain the contract consumed by the existing `chunker`. Additional metadata contains Bitrix timestamps/URLs and `suspicious` diagnostics.

Normalization preserves partial results. Invalid documents are skipped and recorded in the workspace report; an invalid workspace does not block other workspaces. Such a result has `complete_with_warnings` and a successful exit status. Global source/output errors and a run where no selected workspace can be processed return a non-zero exit status.

Suspicious-content heuristics report missing TOC sections, terminal headings or empty list markers, unclosed code fences, and empty/very short leaf documents. They do not change `indexable`, because these checks can have false positives. Integrity diagnostics also cover Front Matter mismatches, missing local assets, broken internal document links, and invalid trees.

Pass one workspace to the chunker:

```bash
cd ../chunker
bun run chunk -- \
  --input ../data/normalized/run_1787220682160/workspace_17/normalized.jsonl \
  --tree ../data/normalized/run_1787220682160/workspace_17/document_tree.json \
  --output ../data/normalized/run_1787220682160/workspace_17/chunks.jsonl \
  --force
```

После создания `chunks.jsonl` для всех выбранных workspace синхронизируйте весь run с Qdrant одной командой:

```bash
cd ../fetcher
go run ./cmd/fetcher index \
  --run ../data/normalized/run_1787220682160
```

Go-indexer использует `qdrantCollection` из `normalization_manifest.json` и автоматически удаляет устаревшие точки только после успешной обработки соответствующего workspace. Workspace обрабатываются последовательно; ошибка одного не блокирует остальные, но итоговая команда возвращает ненулевой код.

Для одной коллекции доступен отдельный режим:

```bash
go run ./cmd/fetcher index \
  --input ../data/normalized/accounting/chunks.jsonl \
  --collection accounting_e5_small_v1 \
  --prune
```

Перед любой записью indexer проверяет `/health` и `/info` внешнего TEI, точные model ID/revision и запрет truncation. Qdrant и TEI команда не запускает. Настройки читаются из `TEI_URL`, `TEI_TIMEOUT`, `QDRANT_URL`, `QDRANT_API_KEY`, `QDRANT_TIMEOUT`, `QDRANT_COLLECTION`, `INDEXER_BATCH_SIZE`, `EMBEDDING_PIPELINE_VERSION`, `EMBEDDING_VECTOR_CACHE_SIZE`, `STORE_EMBEDDING_TEXT` и `LOG_LEVEL`.

Payload новых точек содержит `embedding_backend: "tei"`. Существующие точки Transformers.js без этого поля автоматически вычисляются заново; их vectors не используются как кеш. UUID точек и остальные параметры embedding pipeline остаются совместимыми.

Полезные режимы: `--dry-run`, `--force-reembed`, `--continue-on-error`; `--prune` применяется вручную только с `--input`, а с `--run` всегда включён безопасно.

## Verification

```bash
go test ./...
go test -race ./...
go vet ./...
go build ./cmd/fetcher
```
