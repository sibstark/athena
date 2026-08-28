# Архитектура Athena RAG и исходные данные для аудита поиска

## 1. Назначение и границы документа

Этот документ описывает фактическую реализацию RAG в Athena, чтобы независимая LLM могла предложить проверяемые способы улучшить качество поиска. Документ основан на текущем коде репозитория и последнем полном локальном pipeline-run, а не только на README.

Срез системы:

- дата описания: 2026-08-28;
- Git commit: `c6dd618`;
- проанализированный pipeline-run: `run_1787656034365`;
- источник данных: Bitrix24 Knowledge Base 2.0;
- векторная БД: Qdrant;
- embedding runtime: Hugging Face Text Embeddings Inference (TEI);
- генератор ответа: DeepSeek Chat Completions.

В документе нет ключей API, webhook secrets, паролей и содержательных фрагментов закрытой базы знаний. Примеры контрактов ниже обезличены.

## 2. Общая схема

Система состоит из двух независимых контуров: offline-подготовки индекса и online-поиска/генерации ответа.

### 2.1. Offline pipeline

```text
Bitrix24 Knowledge Base
        │
        │ note.collection.list
        │ note.document.tree.list
        │ note.document.get
        ▼
Fetcher (Go)
        │ immutable snapshot
        ▼
data/fetcher/run_<id>
        │
        │ normalize
        ▼
Normalizer (Go, модуль fetcher)
        │ normalized JSONL + document tree
        ▼
data/normalized/normalized_<id>
        │
        │ chunk
        ▼
Chunker (TypeScript/Bun)
        │ chunks JSONL
        ▼
data/chunks/chunks_<id>
        │
        │ passage prefix + embedding
        ▼
Indexer (Go, модуль fetcher) ───────► TEI
        │
        │ vectors + payload + safe prune
        ▼
Qdrant: отдельная collection для каждого workspace
```

Имена стадий связаны одним ID:

```text
run_<id> → normalized_<id> → chunks_<id>
```

Ни один CLI-компонент не запускает следующую стадию автоматически. TEI и Qdrant поднимаются отдельно через Docker Compose.

### 2.2. Online pipeline

```text
Bot или другой доверенный клиент
        │ query + обязательный список collections
        ▼
Go API /v1/search или /v1/answer
        │
        ├──► TEI: query embedding (один раз на запрос)
        │
        ├──► Qdrant collection A ─┐
        ├──► Qdrant collection B ─┼─ параллельный dense search
        └──► Qdrant collection N ─┘
                     │
                     ▼
        merge → global sort → exact dedup → top_k
                     │
          ┌──────────┴──────────┐
          ▼                     ▼
    /v1/search             /v1/answer
    JSON results           context builder
                                │
                                ▼
                             DeepSeek
                                │
                                ▼
                     ответ + проверенные sources
```

API stateless и read-only относительно Qdrant. `/v1/search` не вызывает DeepSeek. `/v1/answer` вызывает DeepSeek только при наличии прошедших threshold результатов.

## 3. Fetcher: получение исходной БЗ

Fetcher — Go-приложение, которое создаёт immutable snapshot доступных workspace Bitrix24.

### 3.1. Используемые методы Bitrix

| Операция | Метод |
|---|---|
| Список workspace | `note.collection.list` |
| Полное дерево workspace | `note.document.tree.list` |
| Содержимое документа | `note.document.get` |

`note.collection.list` читается cursor-пагинацией по 200 workspace. Дерево каждого workspace запрашивается отдельно. Если Bitrix возвращает `truncated=true`, весь fetch-run отклоняется, потому что дерево может быть неполным.

Документы скачиваются с ограниченной конкурентностью, по умолчанию четыре worker. Один документ запрашивается отдельным вызовом API. Клиент выполняет до четырёх попыток при сетевых ошибках, HTTP 429/5xx и временных кодах Bitrix; задержка растёт примерно от 0,5 до 8 секунд и учитывает `Retry-After`.

### 3.2. Результат fetch

Каждый запуск создаёт новый каталог и никогда не перезаписывает предыдущий:

```text
data/fetcher/run_<id>/
├── manifest.json
├── report.json
└── workspace_<workspace_id>/
    ├── assets/
    └── document_<document_id>/
        ├── content.md
        └── document_<child_id>/
            └── content.md
```

`manifest.json` является источником истины для:

- workspace/document ID и иерархии;
- локальных путей;
- URL исходных страниц Bitrix;
- имени будущей Qdrant collection: `workspace_<workspace_id>`.

Каждый `content.md` содержит YAML Front Matter с ID, parent ID, title, timestamps, `workspace_url` и `document_url`, а затем Markdown, полученный от Bitrix.

### 3.3. Assets и отказоустойчивость

Fetcher скачивает изображения и файлы, заменяет удалённые ссылки на относительные локальные пути и дедуплицирует одинаковые asset URL внутри workspace. Размер одного asset ограничен 100 MiB, конкурентность загрузки по умолчанию равна четырём.

Ошибка asset не блокирует snapshot: run получает `complete_with_warnings`, а исходная удалённая ссылка сохраняется. Ошибка получения или проверки отдельного документа в текущем fetch-коде останавливает весь snapshot; failed run публикует только очищенный от секретов `report.json`.

Bitrix не предоставляет отдельный надёжный признак обрезанного `markdown` в `note.document.get`. Поэтому последующие suspicious-эвристики могут только обнаруживать некоторые признаки незавершённого текста, но не доказывают полноту документа.

## 4. Normalizer: подготовка документов

Normalizer является отдельной Go-командой внутри `fetcher`. Он читает только завершённый `run_<id>` и создаёт новый `normalized_<id>`, не изменяя snapshot.

### 4.1. Обработка Markdown

Для каждого документа normalizer:

1. проверяет UTF-8 и разбирает YAML Front Matter;
2. приводит line endings к LF;
3. заменяет запрещённые control characters пробелами;
4. удаляет trailing whitespace вне code blocks;
5. ограничивает длинные последовательности пустых строк;
6. декодирует HTML entities в URL;
7. анализирует Markdown AST, заголовки, ссылки, изображения и semantic text;
8. проверяет соответствие ID/parent/workspace данным manifest;
9. вычисляет SHA-256 нормализованного content.

### 4.2. Классификация и индексируемость

| Тип | Условие | `indexable` |
|---|---|---:|
| `markdown` | обычный содержательный документ | true |
| `short_note` | semantic text короче 200 Unicode-символов | true |
| `external_link` | собственного текста нет, есть внешняя ссылка | false |
| `empty` | нет semantic text и внешних URL | false; документ не публикуется в normalized JSONL |

Короткие документы остаются индексируемыми. Документы-ссылки сохраняются в normalized JSONL для диагностики, но затем пропускаются chunker.

### 4.3. Иерархический контекст

Из manifest строятся:

- `document_id` вида `page_<bitrix_id>`;
- parent/root page ID;
- depth и ancestor IDs;
- breadcrumbs от корня до текущего документа;
- `context_text` как breadcrumbs, соединённые ` → `;
- отдельный `document_tree.json`.

Эта информация позже включается в `embedding_text` и Qdrant payload.

### 4.4. Диагностика качества исходного контента

Normalizer помечает документ `suspicious=true`, если обнаружен хотя бы один признак:

- оглавление ссылается минимум на два отсутствующих заголовка;
- документ заканчивается заголовком;
- документ заканчивается пустым элементом списка;
- code fence не закрыт;
- leaf-документ пуст или короче 50 символов.

Дополнительно проверяются Front Matter, внутренние document links, assets и целостность дерева. Suspicious — только диагностический флаг: он не меняет `indexable`, потому что эвристики могут давать false positive.

Ошибка отдельного документа исключает только этот документ. Валидные документы workspace сохраняются, а статус становится `complete_with_warnings`.

### 4.5. Сокращённый normalized contract

```json
{
  "document_id": "page_123",
  "parent_document_id": "page_100",
  "content": "## Правило\n\nОбезличенный текст документа.",
  "metadata": {
    "title": "Название документа",
    "source": "workspace_1/document_100/document_123/content.md",
    "document_url": "https://portal.example/note/document/123/",
    "page_id": "123",
    "parent_page_id": "100",
    "root_page_id": "100",
    "depth": 1,
    "ancestor_ids": ["100"],
    "breadcrumbs": ["Корневой документ", "Название документа"],
    "context_text": "Корневой документ → Название документа",
    "document_type": "markdown",
    "indexable": true,
    "status": "active",
    "language": "ru",
    "suspicious": false,
    "suspicious_reasons": []
  },
  "content_hash": "<sha256>"
}
```

## 5. Chunker: преобразование документов в поисковые единицы

Chunker — TypeScript/Bun-приложение. Он не вычисляет embeddings и не обращается к Hugging Face по сети. Точный tokenizer читается из локального model cache и проверяется по model revision и SHA-256 файлов tokenizer.

### 5.1. Текущие лимиты

| Параметр | Значение | Назначение |
|---|---:|---|
| target tokens | 320 | предпочтительный размер `embedding_text` |
| max tokens | 450 | жёсткий предел chunker для `embedding_text` |
| min tokens | 80 | soft minimum только для отчёта |
| overlap tokens | 50 | overlap полных paragraph units |
| model input limit | 512 | проверяется повторно indexer через TEI |

`min_tokens` не заставляет chunker объединять короткий chunk. Поэтому в индексе могут быть очень короткие самостоятельные секции.

### 5.2. Алгоритм chunking

1. Markdown делится на секции по заголовкам H1–H6 с сохранением полного `heading_path`.
2. Секция разбирается на блоки: heading, paragraph, list item, table, fenced code, HTML и horizontal rule.
3. Слишком большие блоки делятся специализированно:
   - paragraph — по предложениям через `Intl.Segmenter`, затем по inline atoms/символам;
   - table — с повторением header в каждой части;
   - list — по элементам и вложенным строкам;
   - code/HTML — жёстким делением с учётом token limit.
4. Блоки упаковываются до target, но не выше max.
5. В следующий chunk может добавляться suffix предыдущего chunk до 50 токенов. Overlap применяется только к подходящим paragraph units; forced table/code/list parts не overlap-ятся.
6. Для каждого chunk создаются `content` и отдельный `embedding_text`.

### 5.3. Отличие `content` от `embedding_text`

`content` — исходный фрагмент Markdown, который возвращается пользователю и передаётся LLM.

`embedding_text` добавляет контекст:

```text
Путь: Корневой документ → Название документа
Документ: Название документа
Раздел: Раздел → Подраздел

Обезличенный текст фрагмента.
```

Из embedding body удаляется первый заголовок текущей секции, потому что он уже присутствует в `Раздел`. Markdown links заменяются их label, а image links — строкой `Изображение: <alt>`, только если alt непустой.

Таким образом, title может присутствовать одновременно в `Путь` и `Документ`. Префикс `passage: ` пока не входит в `embedding_text`: его добавляет indexer непосредственно перед токенизацией и embedding.

### 5.4. Идентичность и metadata chunk

`chunk_id` стабилен и вычисляется из:

- `document_id`;
- нормализованного heading path;
- content hash;
- номера повторного вхождения идентичного content в той же секции.

Chunk хранит `previous_chunk_id` и `next_chunk_id`, но online retrieval сейчас не использует их для расширения контекста.

Точные дубликаты chunk помечаются `metadata.duplicate_of_chunk_id`, но всё равно публикуются в `chunks.jsonl` и индексируются.

### 5.5. Сокращённый chunk contract

```json
{
  "chunk_id": "chunk_<sha256>",
  "document_id": "page_123",
  "parent_document_id": "page_100",
  "content": "Обезличенный текст фрагмента.",
  "embedding_text": "Путь: ...\nДокумент: ...\nРаздел: ...\n\nОбезличенный текст фрагмента.",
  "content_hash": "<sha256>",
  "embedding_text_hash": "<sha256>",
  "metadata": {
    "title": "Название документа",
    "document_url": "https://portal.example/note/document/123/",
    "breadcrumbs": ["Корневой документ", "Название документа"],
    "heading_path": ["Раздел", "Подраздел"],
    "section_title": "Подраздел",
    "token_count": 287,
    "chunk_index": 2,
    "chunk_count": 5,
    "contains_table": false,
    "contains_code": false,
    "language": "ru",
    "status": "active"
  },
  "previous_chunk_id": "chunk_<previous>",
  "next_chunk_id": "chunk_<next>"
}
```

Ошибка одного normalized document не блокирует остальные документы workspace. Такой workspace публикуется как `complete_with_warnings`, а ошибочный документ отсутствует в `chunks.jsonl`.

## 6. Indexer, TEI и Qdrant

Indexer — Go-модуль внутри `fetcher`. В run-режиме он последовательно обрабатывает все successful workspace из `chunks_manifest.json`. Failed workspace не изменяет соответствующую Qdrant collection.

### 6.1. Общий embedding contract

Indexer, chunker и API обязаны использовать один контракт из корневого `.env`. Значений модели в коде нет.

Текущий контракт:

| Поле | Значение |
|---|---|
| model | `Xenova/multilingual-e5-small` |
| revision | `761b726dd34fb83930e26aab4e9ac3899aa1fa78` |
| dimension | 384 |
| dtype | `q8` |
| pooling | `mean` |
| normalized | true |
| document prefix | `passage: ` |
| query prefix | `query: ` |
| maximum model input | 512 tokens |
| TEI auto truncate | false |
| pipeline version | `1` |
| vector distance | Cosine |

Tokenizer также закреплён SHA-256:

- `tokenizer.json`: `0b44a9d7b51c3c62626640cda0e2c2f70fdacdc25bbbd68038369d14ebdf4c39`;
- `tokenizer_config.json`: `a1d6bc8734a6f635dc158508bef000f8e2e5a759c7d92f984b2c86e5ff53425b`.

Перед началом записи indexer проверяет `/health` и `/info` TEI: model ID, revision, input limit и auto truncate. Каждый итоговый input отдельно проходит `/tokenize`; input длиннее 512 токенов считается ошибкой и не обрезается автоматически.

### 6.2. Embedding и повторные прогоны

Для документа indexer формирует:

```text
passage: <embedding_text>
```

По умолчанию JSONL обрабатывается batch-ами по 16 строк. TEI-вызовы дополнительно ограничиваются возможностями, объявленными самим сервером, максимум 32 input за запрос.

Indexer избегает лишнего embedding:

- неизменный point пропускается;
- изменение только metadata выполняется как payload-only update;
- одинаковый `embedding_input_hash` переиспользует vector внутри run;
- vector может быть найден в существующей collection по input hash и полному pipeline identity;
- смена input/model/revision/pipeline/backend требует нового embedding.

Point ID — детерминированный UUID, построенный из `chunk_id` и общего namespace. Поэтому один и тот же chunk получает тот же ID при повторном запуске.

### 6.3. Коллекции Qdrant

Каждый Bitrix workspace имеет отдельную collection:

```text
workspace_1
workspace_7
workspace_17
...
```

Indexer создаёт collection с 384-dimensional Cosine vectors и записывает embedding pipeline identity в metadata collection. Существующая collection принимается только при полном совпадении dimension, distance и pipeline metadata.

Основные payload indexes создаются для document/page/root IDs, language/category/status, content hash, embedding hash/backend/model/revision/pipeline и point namespace.

Сокращённый Qdrant payload:

```json
{
  "chunk_id": "chunk_<sha256>",
  "document_id": "page_123",
  "content": "Обезличенный текст фрагмента.",
  "title": "Название документа",
  "source": "workspace_1/document_100/document_123/content.md",
  "document_url": "https://portal.example/note/document/123/",
  "breadcrumbs": ["Корневой документ", "Название документа"],
  "heading_path": ["Раздел", "Подраздел"],
  "section_title": "Подраздел",
  "previous_chunk_id": "chunk_<previous>",
  "next_chunk_id": "chunk_<next>",
  "duplicate_of_chunk_id": null,
  "content_hash": "<sha256>",
  "embedding_input_hash": "<sha256>",
  "embedding_model": "Xenova/multilingual-e5-small",
  "embedding_model_revision": "761b726dd34fb83930e26aab4e9ac3899aa1fa78",
  "embedding_backend": "tei",
  "embedding_pipeline_version": "1"
}
```

### 6.4. Safe prune

Перед синхронизацией indexer получает ID всех points, принадлежащих текущему pipeline identity. Встреченные во входном JSONL ID удаляются из набора stale. Оставшиеся ID удаляются только после полностью успешной обработки workspace.

Следствия:

- удалённый или больше не индексируемый документ исчезает из Qdrant;
- ошибочный документ из `complete_with_warnings` также исчезает после успешного prune;
- ошибка indexer/TEI/Qdrant запрещает prune данного workspace;
- пустой, но successful workspace очищает свою collection;
- failed workspace оставляет старую collection без изменений.

## 7. API: online retrieval

API — отдельный stateless Go-сервис. Он не запускает offline pipeline, не изменяет Qdrant и не знает бизнес-смысл Bitrix workspace.

Основные методы:

| Метод | Назначение |
|---|---|
| `GET /v1/collections` | список collections с `points_count > 0` |
| `POST /v1/search` | embedding запроса и возврат найденных chunks |
| `POST /v1/answer` | тот же retrieval, затем ответ DeepSeek с источниками |

Поле `collections` обязательно. API не выбирает collection автоматически и не использует это поле как ACL.

### 7.1. Текущие retrieval-параметры

| Параметр | Значение |
|---|---:|
| default `top_k` | 8 |
| maximum `top_k` | 20 |
| score threshold | 0.65 |
| maximum query length | 4 000 Unicode-символов |
| maximum context chunks | 8 |
| maximum context length | 30 000 Unicode-символов |

Клиент может переопределить `top_k` и `score_threshold` в пределах API-валидации.

### 7.2. Query embedding

Для каждого пользовательского запроса TEI вызывается один раз, независимо от количества collections:

```text
query: <пользовательский запрос>
```

Если вход уже начинается с `query:` или `passage:`, этот известный prefix сначала удаляется. API сверяет TEI model/revision/input limit/auto truncate, токенизирует query и отказывается от запроса длиннее model limit. Vector должен иметь dimension 384 и L2 norm примерно 1.

Query rewrite, expansion, decomposition и history-aware reformulation отсутствуют.

### 7.3. Поиск по collections

Для запрошенного `top_k=K` API вычисляет candidate limit:

```text
candidate_limit = min(K × 3, RAG_MAX_TOP_K × 3)
```

Один и тот же query vector параллельно отправляется в каждую collection. Каждая collection может вернуть до `candidate_limit` hits выше общего threshold. При `top_k=8` и 17 collections теоретически может быть получено до `17 × 24 = 408` кандидатов перед объединением.

До каждого поиска API проверяет collection:

- status строго `green`;
- `points_count > 0`;
- dimension 384 и Cosine;
- полное совпадение embedding metadata.

Если хотя бы одна переданная collection не существует, пуста, имеет `yellow/grey` status или несовместимый pipeline, весь multi-collection request завершается ошибкой `qdrant_unavailable`. Частичного результата нет.

`GET /v1/collections` проверяет только `points_count > 0`, но не применяет полную проверку readiness/compatibility. Поэтому collection теоретически может присутствовать в списке и затем сделать search недоступным.

### 7.4. Merge, сортировка и дедупликация

Все hits из всех collections:

1. объединяются в один список;
2. сортируются по raw Qdrant score по убыванию;
3. фильтруются, если отсутствуют `chunk_id`, `document_id` или content;
4. дедуплицируются сначала по `content_hash`, затем по SHA-256 текста после схлопывания whitespace;
5. обрезаются до `top_k`;
6. получают последовательные IDs `S1`, `S2`, ...

Нет:

- нормализации score между collections;
- квот или балансировки по workspace/document;
- reciprocal rank fusion;
- MMR/diversity;
- semantic reranker;
- повышения веса title, breadcrumbs или exact lexical match;
- автоматического присоединения previous/next chunk;
- группировки нескольких chunks одного документа.

Флаг `duplicate_of_chunk_id` не используется retrieval-кодом. При этом exact content deduplication удаляет часть одинаковых chunks уже после Qdrant search.

### 7.5. Фильтры

API поддерживает exact match только по плоским payload-полям:

- `document_id`;
- `parent_document_id`;
- `page_id`;
- `parent_page_id`;
- `root_page_id`;
- `language`;
- `category`;
- `status`.

Фильтры по title, breadcrumbs, workspace name, timestamps, suspicious status и nested metadata отсутствуют.

### 7.6. Обезличенный `/v1/search`

Запрос:

```json
{
  "query": "Как выполнить внутреннюю процедуру?",
  "collections": ["workspace_1", "workspace_17"],
  "top_k": 8,
  "score_threshold": 0.65,
  "filters": {
    "language": "ru",
    "status": "active"
  }
}
```

Сокращённый ответ:

```json
{
  "query": "Как выполнить внутреннюю процедуру?",
  "results": [
    {
      "source_id": "S1",
      "collection": "workspace_17",
      "chunk_id": "chunk_<sha256>",
      "document_id": "page_123",
      "score": 0.84,
      "content": "Обезличенный найденный фрагмент.",
      "title": "Название документа",
      "url": "https://portal.example/note/document/123/",
      "breadcrumbs": ["Раздел БЗ", "Название документа"],
      "heading_path": ["Инструкция"],
      "section_title": "Инструкция"
    }
  ],
  "retrieval": {
    "collections": ["workspace_1", "workspace_17"],
    "embedding_model": "Xenova/multilingual-e5-small",
    "returned": 1,
    "duration_ms": 100
  }
}
```

## 8. Формирование ответа `/v1/answer`

`/v1/answer` использует тот же retrieval. Если результатов нет, API возвращает `insufficient_context` и не вызывает DeepSeek.

### 8.1. Context builder

Context builder последовательно берёт уже отсортированные результаты, максимум восемь chunks и 30 000 символов. Для каждого источника в prompt передаются:

- `S<N>`;
- title;
- breadcrumbs;
- heading path/section;
- Qdrant score;
- content.

Если первый source один превышает весь лимит, его content обрезается с пометкой `[фрагмент сокращён]`. Более поздний source, который не помещается, пропускается; builder продолжает проверять следующие результаты.

DeepSeek получает только выбранные chunks и системное правило отвечать исключительно по ним. Каждый существенный факт должен иметь citation `[S1]`, `[S2]` и т. д.

Текущая генерация:

| Параметр | Значение |
|---|---|
| model | `deepseek-v4-flash` |
| thinking | disabled |
| maximum output | 1 200 tokens |
| maximum concurrent generations | 4 |
| retries | до 3 для временных network/429/5xx ошибок |

### 8.2. Проверка citations

API проверяет, что ответ содержит хотя бы одну standalone citation формата `[S<N>]` и что каждый найденный ID существует в переданном context. При ошибке выполняется одна дополнительная repair-generation. Если повторная проверка не проходит, API возвращает `generation_validation_failed`.

Metadata источников берётся из Qdrant, а не из ответа LLM. В `sources` попадают только источники, citations которых распознаны валидатором.

Валидатор API распознаёт только standalone `[S1]`. Групповой вариант `[S1, S2]` целиком не распознаётся. Bot умеет визуально преобразовывать как отдельные, так и групповые citations, но это не расширяет API validation.

Сокращённый ответ:

```json
{
  "status": "ok",
  "answer": "Обезличенный ответ на основании документа [S1].",
  "sources": [
    {
      "source_id": "S1",
      "collection": "workspace_17",
      "chunk_id": "chunk_<sha256>",
      "document_id": "page_123",
      "score": 0.84,
      "title": "Название документа",
      "source": "workspace_17/document_123/content.md",
      "url": "https://portal.example/note/document/123/",
      "breadcrumbs": ["Раздел БЗ", "Название документа"],
      "heading_path": ["Инструкция"],
      "section_title": "Инструкция"
    }
  ],
  "generation": {
    "provider": "deepseek",
    "model": "deepseek-v4-flash",
    "input_tokens": 1000,
    "output_tokens": 200,
    "duration_ms": 3000
  }
}
```

## 9. Bot и выбор collections

Bitrix bot — stateless transport adapter:

1. получает текущее сообщение пользователя;
2. вызывает `GET /v1/collections`;
3. передаёт все возвращённые непустые collections в `/v1/answer`;
4. отправляет ответ reply-сообщением в тот же Bitrix dialog;
5. преобразует `[S1]` и `[S1, S2]` в кликабельные `Источник 1`, `Источник 2`.

Bot не хранит историю диалога, не выбирает workspace по роли пользователя и не выполняет query routing. Следовательно, обычный запрос может одновременно искать по всем непустым workspace, а query embedding не учитывает предыдущие сообщения.

## 10. Фактические показатели последнего полного run

Источник метрик:

- `data/fetcher/run_1787656034365/report.json`;
- `data/normalized/normalized_1787656034365/normalization_report.json`;
- `data/chunks/chunks_1787656034365/chunks_report.json`;
- per-workspace `chunk_report.json`;
- `data/chunks/chunks_1787656034365/index-report.json`.

### 10.1. Сводка

| Этап/показатель | Значение |
|---|---:|
| workspace в snapshot | 17 |
| документов в Bitrix tree | 3 199 |
| normalized документов | 2 575 |
| suspicious normalized документов | 546 |
| normalization errors | 0 |
| normalization warnings | 577 |
| успешно загруженных assets | 34 143 |
| failed assets | 883 |
| документов, обработанных chunker | 2 539 |
| документов, пропущенных chunker | 35 |
| документов с chunking error | 1 |
| созданных chunks | 39 675 |
| chunks ниже soft minimum 80 | 3 436 |
| chunks выше max 450 | 0 |
| средний размер `embedding_text` | ≈262,10 tokens |
| chunks с duplicate marker | 3 538 |
| chunks с таблицами | 387 |
| chunks с code | 19 |
| chunks с изображениями | 14 364 |
| успешно проиндексированных points | 39 675 |
| vectors, переиспользованных внутри initial run | 34 |
| failed index chunks | 0 |

Разница между 3 199 исходными и 2 575 normalized документами в основном обусловлена пустыми документами, которые normalizer учитывает в отчёте, но не публикует в JSONL. Из 2 575 входов chunker исключил 35 non-indexable/empty документов и один документ с ошибкой.

### 10.2. Неравномерность данных

Workspace сильно отличаются по размеру. Например, одна collection содержит десятки тысяч chunks, тогда как несколько workspace пусты или содержат единицы points. При текущем алгоритме каждая непустая collection получает одинаковый candidate limit, а после этого raw scores всех workspace сравниваются напрямую.

Это важно для анализа collection fusion: количество документов и тематическая плотность workspace различаются, но retrieval не использует размер collection, приоритет workspace или квоты.

## 11. Подтверждённые ограничения, влияющие на качество поиска

Ниже перечислены свойства текущей системы, а не утверждения о том, что каждое из них обязательно является дефектом.

### 11.1. Подготовка данных

- Fetcher зависит от полноты Markdown, возвращаемого Bitrix, но API не даёт надёжного content-truncated flag.
- 546 документов имеют suspicious-признаки и всё равно могут индексироваться.
- 883 assets не скачались; binary content в любом случае не проходит OCR или text extraction.
- Изображения дают поисковый текст только через alt; пустой alt не добавляет семантики.
- External-link-only документы не индексируются, содержимое внешних страниц не загружается.
- Exact duplicate documents/chunks помечаются, но не удаляются до Qdrant.

### 11.2. Chunking

- Общие параметры `320/450/80/50` применяются ко всем типам документов и workspace.
- Soft minimum не участвует в принятии решений, поэтому 3 436 chunks короче 80 токенов.
- Context prefix повторяет breadcrumbs/title/section в каждом chunk и может занимать существенную часть token budget у глубоко вложенных документов.
- Overlap применяется только к полным paragraph units и не гарантирует фиксированные 50 токенов.
- Section-aware chunking ориентируется на структуру Markdown; слабая или ошибочная разметка напрямую влияет на границы.
- Соседство chunks сохраняется, но не используется online.

### 11.3. Retrieval

- Используется только один dense embedding на исходный query.
- Нет lexical/BM25/hybrid retrieval, query expansion и reranking.
- Threshold `0.65` глобален для всех тем, workspace и типов запросов.
- Каждая collection возвращает до одинакового `top_k × 3`, независимо от размера и релевантности workspace.
- Raw Cosine scores из collections объединяются напрямую.
- Exact dedup выполняется после получения кандидатов и может уменьшить итоговое количество результатов без повторного добора из Qdrant.
- Нет document diversity, workspace diversity и ограничения числа chunks от одного документа.
- Один проблемный workspace делает недоступным весь multi-collection request.
- API не выбирает релевантные collections: bot обычно передаёт все непустые.

### 11.4. Context и generation

- DeepSeek видит максимум восемь результатов после retrieval/deduplication.
- Context builder не объединяет найденный chunk с соседними chunks или другими секциями документа.
- Ответ не использует conversation history.
- Citation validation проверяет наличие разрешённых ссылок, но не entailment: она не доказывает, что конкретный факт действительно подтверждается указанным chunk.
- Grouped citation синтаксис bot и API обрабатывают по-разному.

### 11.5. Evaluation и наблюдаемость

- В репозитории нет заполненного production relevance dataset с вопросами, ожидаемыми документами и оценками ответа.
- TEI compatibility tooling умеет сравнивать vectors и retrieval между embedding implementations, но не заменяет предметную оценку качества.
- Prometheus metrics агрегируют request status, dependency latency, число найденных chunks, insufficient context, retries и token usage.
- В штатных логах намеренно нет query, context и ответов. Поэтому из production metrics нельзя восстановить precision/recall, причины нерелевантных результатов или проблемные классы вопросов.

## 12. Вопросы для внешнего аудита

При предложении улучшений необходимо отделять гипотезы от доказанных проблем и описывать способ измерения эффекта. Полезно ответить на следующие вопросы:

1. Какой минимальный evaluation dataset нужен для этой БЗ: типы вопросов, expected documents/chunks, negative examples и метрики retrieval/answer quality?
2. Следует ли менять структуру `embedding_text`: повторение path/title, вес заголовков, включение parent context и обработку коротких секций?
3. Какие chunk size/overlap стратегии стоит сравнить отдельно для инструкций, таблиц, FAQ, code и коротких заметок?
4. Нужен ли lexical или hybrid retrieval для точных терминов, названий внутренних процессов, аббревиатур и ошибок в запросах?
5. Какой reranker применим после multi-collection candidate retrieval и сколько кандидатов ему нужно передавать?
6. Как объединять результаты разных workspace: raw score, score calibration, RRF, collection routing, per-collection quota или иная стратегия?
7. Нужны ли diversity/MMR, document grouping и автоматическое добавление adjacent chunks?
8. Как калибровать threshold: глобально, по collection, по классу query или динамически?
9. Следует ли исключать duplicate/suspicious content до поиска и как это повлияет на recall?
10. Какие форматы assets требуют OCR/text extraction и как связать извлечённый текст с исходным документом?
11. Как сделать multi-collection search устойчивым к одной неготовой collection без скрытия инфраструктурных ошибок?
12. Какие online/offline метрики и безопасное логирование нужны для итеративных A/B или shadow экспериментов?

Любое предлагаемое изменение модели, prefix, pooling, normalization или dimension требует нового совместимого embedding contract и переиндексации в новые либо очищенные Qdrant collections. Изменения только `top_k`, threshold, deduplication, context selection или reranking могут проверяться без пересчёта существующих vectors.

## 13. Источники фактов в коде

Основные реализации, по которым сверялось описание:

- `fetcher/internal/snapshot/` — Bitrix snapshot;
- `fetcher/internal/normalizer/` — normalization;
- `chunker/src/chunker.ts` и `chunker/src/run.ts` — chunking;
- `fetcher/internal/indexer/` — TEI indexing и Qdrant sync;
- `api/internal/tei/` — query embeddings;
- `api/internal/qdrant/` и `api/internal/retrieval/` — online search;
- `api/internal/contextbuilder/` и `api/internal/httpapi/` — prompt, answer и citations;
- `bot/internal/` — Bitrix transport adapter.
