#!/usr/bin/env bun

const EMBEDDING = {
  modelId: requiredEnv("EMBEDDING_MODEL_ID"),
  revision: requiredEnv("EMBEDDING_MODEL_REVISION"),
  dimension: positiveInteger(requiredEnv("EMBEDDING_DIMENSION"), "EMBEDDING_DIMENSION"),
  dtype: requiredEnv("EMBEDDING_DTYPE"),
  pooling: requiredEnv("EMBEDDING_POOLING"),
  normalized: requiredBoolean("EMBEDDING_NORMALIZED"),
  documentPrefix: requiredEnv("EMBEDDING_DOCUMENT_PREFIX", true),
  queryPrefix: requiredEnv("EMBEDDING_QUERY_PREFIX", true),
  maxInputTokens: positiveInteger(requiredEnv("EMBEDDING_MAX_INPUT_TOKENS"), "EMBEDDING_MAX_INPUT_TOKENS"),
  autoTruncate: requiredBoolean("EMBEDDING_AUTO_TRUNCATE"),
  pipelineVersion: requiredEnv("EMBEDDING_PIPELINE_VERSION"),
  pointNamespace: requiredEnv("EMBEDDING_POINT_NAMESPACE"),
  tokenizerSHA256: requiredEnv("EMBEDDING_TOKENIZER_SHA256"),
  tokenizerConfigSHA256: requiredEnv("EMBEDDING_TOKENIZER_CONFIG_SHA256"),
  cacheDir: requiredEnv("EMBEDDING_CACHE_DIR"),
  vectorCacheSize: positiveInteger(requiredEnv("EMBEDDING_VECTOR_CACHE_SIZE"), "EMBEDDING_VECTOR_CACHE_SIZE"),
};
if (EMBEDDING.queryPrefix.trim() === "") throw new Error("EMBEDDING_QUERY_PREFIX обязателен.");

const TEI_URL = (process.env.TEI_URL || "http://127.0.0.1:8080").replace(/\/+$/u, "");
const QDRANT_URL = (process.env.QDRANT_URL || "http://127.0.0.1:6333").replace(/\/+$/u, "");
const COLLECTION = requiredEnv("QDRANT_COLLECTION");
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
const query = argumentQuery || globalThis.prompt("Введите вопрос:")?.trim() || "";

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

  const modelInput = `${EMBEDDING.queryPrefix}${query.replace(/^(?:query|passage):\s*/iu, "")}`;
  const embeddings = await requestJson(`${TEI_URL}/embed`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({
      inputs: [modelInput],
      normalize: EMBEDDING.normalized,
      truncate: EMBEDDING.autoTruncate,
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
    if (payload.section_title) console.log(`Раздел: ${String(payload.section_title)}`);
    console.log(`Документ: ${String(payload.document_id || "")}`);
    console.log(`Чанк: ${String(payload.chunk_id || result.id || "")}`);
    console.log();
    console.log(content || "[Текст отсутствует]");
    console.log();
  }
} catch (error) {
  console.error(`\nОшибка поиска: ${error instanceof Error ? error.message : String(error)}`);
  console.error("Проверьте сервисы командами: bun run db:status && bun run tei:status");
  process.exitCode = 1;
}

async function requestJson(url: string, init: RequestInit = {}): Promise<unknown> {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), 30_000);
  try {
    const response = await fetch(url, { ...init, signal: controller.signal });
    if (!response.ok) {
      const body = (await response.text()).slice(0, 500);
      throw new Error(`${url} вернул HTTP ${response.status}${body ? `: ${body}` : ""}`);
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
  if (info.model_id !== EMBEDDING.modelId || info.model_sha !== EMBEDDING.revision) {
    throw new Error(
      `TEI использует ${String(info.model_id)}@${String(info.model_sha)}, ожидалась ${EMBEDDING.modelId}@${EMBEDDING.revision}`,
    );
  }
  if (info.auto_truncate !== EMBEDDING.autoTruncate || info.max_input_length !== EMBEDDING.maxInputTokens) {
    throw new Error("TEI не соответствует embedding-контракту из .env.");
  }
}

function validateCollection(value: unknown): void {
  const response = asRecord(value);
  const result = asRecord(response.result);
  const config = asRecord(result.config);
  const params = asRecord(config.params);
  const vectors = asRecord(params.vectors);
  const metadata = asRecord(config.metadata);

  if (vectors.size !== EMBEDDING.dimension || String(vectors.distance).toLowerCase() !== "cosine") {
    throw new Error(`Коллекция ${COLLECTION} имеет несовместимую конфигурацию vectors.`);
  }
  const expectedMetadata: Record<string, unknown> = {
    embedding_model: EMBEDDING.modelId,
    embedding_model_revision: EMBEDDING.revision,
    embedding_dimension: EMBEDDING.dimension,
    embedding_dtype: EMBEDDING.dtype,
    embedding_pooling: EMBEDDING.pooling,
    embedding_normalized: EMBEDDING.normalized,
    document_prefix: EMBEDDING.documentPrefix,
    embedding_pipeline_version: EMBEDDING.pipelineVersion,
    point_namespace: EMBEDDING.pointNamespace,
  };
  if (Object.entries(expectedMetadata).some(([key, expected]) => String(metadata[key]) !== String(expected))) {
    throw new Error(`Коллекция ${COLLECTION} создана другой embedding-моделью.`);
  }
}

function extractVector(value: unknown): number[] {
  if (!Array.isArray(value) || !Array.isArray(value[0])) {
    throw new Error("TEI вернул embeddings в неожиданном формате.");
  }
  const vector = value[0];
  if (
    vector.length !== EMBEDDING.dimension
    || !vector.every((item) => typeof item === "number" && Number.isFinite(item))
  ) {
    throw new Error(`TEI вернул некорректный vector: ожидалось ${EMBEDDING.dimension} конечных чисел.`);
  }
  return vector as number[];
}

function extractResults(value: unknown): Array<Record<string, unknown>> {
  const response = asRecord(value);
  if (!Array.isArray(response.result)) {
    throw new Error("Qdrant вернул результаты в неожиданном формате.");
  }
  return response.result.filter(
    (item): item is Record<string, unknown> => item !== null && typeof item === "object" && !Array.isArray(item),
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

function requiredEnv(name: string, preserveWhitespace = false): string {
  if (!(name in process.env)) throw new Error(`${name} обязателен.`);
  const value = process.env[name] ?? "";
  if (!preserveWhitespace && value.trim() === "") throw new Error(`${name} обязателен.`);
  return preserveWhitespace ? value : value.trim();
}

function requiredBoolean(name: string): boolean {
  const value = requiredEnv(name);
  if (value === "true") return true;
  if (value === "false") return false;
  throw new Error(`${name} должен быть true или false.`);
}
