#!/usr/bin/env bun

const MODEL_ID = required("EMBEDDING_MODEL_ID");
const MODEL_REVISION = required("EMBEDDING_MODEL_REVISION");
const VECTOR_DIMENSION = positiveInteger(
  required("EMBEDDING_DIMENSION"),
  "EMBEDDING_DIMENSION",
);
const QUERY_PREFIX = required("EMBEDDING_QUERY_PREFIX");
const NORMALIZED = requiredBoolean("EMBEDDING_NORMALIZED");
const AUTO_TRUNCATE = requiredBoolean("EMBEDDING_AUTO_TRUNCATE");
const DTYPE = required("EMBEDDING_DTYPE");
const POOLING = required("EMBEDDING_POOLING");
const DOCUMENT_PREFIX = present("EMBEDDING_DOCUMENT_PREFIX");
const MAX_INPUT_TOKENS = positiveInteger(
  required("EMBEDDING_MAX_INPUT_TOKENS"),
  "EMBEDDING_MAX_INPUT_TOKENS",
);
const PIPELINE_VERSION = required("EMBEDDING_PIPELINE_VERSION");
const POINT_NAMESPACE = required("EMBEDDING_POINT_NAMESPACE");

const TEI_URL = (process.env.TEI_URL || "http://127.0.0.1:8080").replace(
  /\/+$/u,
  "",
);
const QDRANT_URL = (process.env.QDRANT_URL || "http://127.0.0.1:6333").replace(
  /\/+$/u,
  "",
);
const COLLECTION = process.env.QDRANT_COLLECTION || "methodology_e5_small_v1";
const LIMIT = positiveInteger(process.env.SEARCH_LIMIT || "5", "SEARCH_LIMIT");
const QDRANT_API_KEY = process.env.QDRANT_API_KEY?.trim();

if (process.argv.includes("--help") || process.argv.includes("-h")) {
  console.log(`Использование:
  bun query.ts "Что делать, если у ребёнка нет абакуса?"
  bun query.ts

Переменные окружения:
  TEI_URL             ${TEI_URL}
  QDRANT_URL          ${QDRANT_URL}
  QDRANT_COLLECTION   ${COLLECTION}
  SEARCH_LIMIT        ${LIMIT}`);
  process.exit(0);
}

const argumentQuery = process.argv.slice(2).join(" ").trim();
const query =
  argumentQuery || globalThis.prompt("Введите вопрос:")?.trim() || "";

if (!query) {
  console.error("Ошибка: вопрос не может быть пустым.");
  process.exit(1);
}

try {
  const [teiInfo, collectionInfo] = await Promise.all([
    requestJson(`${TEI_URL}/info`),
    requestJson(`${QDRANT_URL}/collections/${encodeURIComponent(COLLECTION)}`, {
      headers: qdrantHeaders(),
    }),
  ]);

  validateTei(teiInfo);
  validateCollection(collectionInfo);

  const modelInput = `${QUERY_PREFIX}${query.replace(/^(?:query|passage):\s*/iu, "")}`;
  const embeddings = await requestJson(`${TEI_URL}/embed`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({
      inputs: [modelInput],
      normalize: NORMALIZED,
      truncate: AUTO_TRUNCATE,
    }),
  });

  const vector = extractVector(embeddings);
  const search = await requestJson(
    `${QDRANT_URL}/collections/${encodeURIComponent(COLLECTION)}/points/search`,
    {
      method: "POST",
      headers: { "content-type": "application/json", ...qdrantHeaders() },
      body: JSON.stringify({
        vector,
        limit: LIMIT,
        with_payload: true,
        with_vector: false,
      }),
    },
  );

  const results = extractResults(search);
  console.log(`\nВопрос: ${query}`);
  console.log(`Найдено результатов: ${results.length}\n`);

  if (results.length === 0) {
    console.log("Подходящие материалы не найдены.");
  }

  for (const [index, result] of results.entries()) {
    const payload = asRecord(result.payload);
    const breadcrumbs = Array.isArray(payload.breadcrumbs)
      ? payload.breadcrumbs.map(String).join(" → ")
      : "";
    const content = String(payload.content || "").trim();

    console.log("═".repeat(80));
    console.log(`${index + 1}. ${String(payload.title || "Без названия")}`);
    console.log(`Релевантность: ${Number(result.score).toFixed(6)}`);
    if (breadcrumbs) console.log(`Путь: ${breadcrumbs}`);
    if (payload.section_title)
      console.log(`Раздел: ${String(payload.section_title)}`);
    console.log(`Документ: ${String(payload.document_id || "")}`);
    console.log(`Чанк: ${String(payload.chunk_id || result.id || "")}`);
    console.log();
    console.log(content || "[Текст отсутствует]");
    console.log();
  }
} catch (error) {
  console.error(
    `\nОшибка поиска: ${error instanceof Error ? error.message : String(error)}`,
  );
  console.error(
    "Проверьте сервисы командами: bun run db:status && bun run tei:status",
  );
  process.exitCode = 1;
}

async function requestJson(
  url: string,
  init: RequestInit = {},
): Promise<unknown> {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), 30_000);
  try {
    const response = await fetch(url, { ...init, signal: controller.signal });
    if (!response.ok) {
      const body = (await response.text()).slice(0, 500);
      throw new Error(
        `${url} вернул HTTP ${response.status}${body ? `: ${body}` : ""}`,
      );
    }
    return await response.json();
  } catch (error) {
    if (controller.signal.aborted) throw new Error(`Тайм-аут запроса к ${url}`);
    throw error;
  } finally {
    clearTimeout(timer);
  }
}

function validateTei(value: unknown): void {
  const info = asRecord(value);
  if (info.model_id !== MODEL_ID || info.model_sha !== MODEL_REVISION) {
    throw new Error(
      `TEI использует ${String(info.model_id)}@${String(info.model_sha)}, ожидалась ${MODEL_ID}@${MODEL_REVISION}`,
    );
  }
  if (info.auto_truncate !== AUTO_TRUNCATE) {
    throw new Error(
      `TEI использует auto_truncate=${String(info.auto_truncate)}, ожидалось ${AUTO_TRUNCATE}.`,
    );
  }
  if (
    info.model_dtype !== DTYPE ||
    info.max_input_length !== MAX_INPUT_TOKENS
  ) {
    throw new Error(`TEI использует несовместимые dtype/max_input_length.`);
  }
}

function validateCollection(value: unknown): void {
  const response = asRecord(value);
  const result = asRecord(response.result);
  const config = asRecord(result.config);
  const params = asRecord(config.params);
  const vectors = asRecord(params.vectors);
  const metadata = asRecord(config.metadata);

  if (
    vectors.size !== VECTOR_DIMENSION ||
    String(vectors.distance).toLowerCase() !== "cosine"
  ) {
    throw new Error(
      `Коллекция ${COLLECTION} имеет несовместимую конфигурацию vectors.`,
    );
  }
  if (
    metadata.embedding_model !== MODEL_ID ||
    metadata.embedding_model_revision !== MODEL_REVISION
  ) {
    throw new Error(
      `Коллекция ${COLLECTION} создана другой embedding-моделью.`,
    );
  }
  const expectedMetadata: Record<string, unknown> = {
    embedding_dimension: VECTOR_DIMENSION,
    embedding_dtype: DTYPE,
    embedding_pooling: POOLING,
    embedding_normalized: NORMALIZED,
    document_prefix: DOCUMENT_PREFIX,
    query_prefix: QUERY_PREFIX,
    embedding_max_input_tokens: MAX_INPUT_TOKENS,
    embedding_auto_truncate: AUTO_TRUNCATE,
    embedding_pipeline_version: PIPELINE_VERSION,
    point_namespace: POINT_NAMESPACE,
  };
  for (const [key, expected] of Object.entries(expectedMetadata)) {
    if (metadata[key] !== expected)
      throw new Error(
        `Коллекция ${COLLECTION} имеет несовместимое поле ${key}.`,
      );
  }
}

function extractVector(value: unknown): number[] {
  if (!Array.isArray(value) || !Array.isArray(value[0])) {
    throw new Error("TEI вернул embeddings в неожиданном формате.");
  }
  const vector = value[0];
  if (
    vector.length !== VECTOR_DIMENSION ||
    !vector.every((item) => typeof item === "number" && Number.isFinite(item))
  ) {
    throw new Error(
      `TEI вернул некорректный vector: ожидалось ${VECTOR_DIMENSION} конечных чисел.`,
    );
  }
  return vector as number[];
}

function extractResults(value: unknown): Array<Record<string, unknown>> {
  const response = asRecord(value);
  if (!Array.isArray(response.result)) {
    throw new Error("Qdrant вернул результаты в неожиданном формате.");
  }
  return response.result.filter(
    (item): item is Record<string, unknown> =>
      item !== null && typeof item === "object" && !Array.isArray(item),
  );
}

function qdrantHeaders(): Record<string, string> {
  return QDRANT_API_KEY ? { "api-key": QDRANT_API_KEY } : {};
}

function asRecord(value: unknown): Record<string, unknown> {
  if (value === null || typeof value !== "object" || Array.isArray(value)) {
    throw new Error("Сервис вернул неожиданный формат данных.");
  }
  return value as Record<string, unknown>;
}

function positiveInteger(value: string, name: string): number {
  const parsed = Number(value);
  if (!Number.isInteger(parsed) || parsed <= 0) {
    throw new Error(`${name} должен быть положительным целым числом.`);
  }
  return parsed;
}

function required(name: string): string {
  const value = process.env[name];
  if (!value?.trim()) throw new Error(`${name} не задан в корневом .env.`);
  return value;
}

function present(name: string): string {
  if (!Object.hasOwn(process.env, name))
    throw new Error(`${name} не задан в корневом .env.`);
  return process.env[name] ?? "";
}

function requiredBoolean(name: string): boolean {
  const value = required(name);
  if (value !== "true" && value !== "false")
    throw new Error(`${name} должен быть true или false.`);
  return value === "true";
}
