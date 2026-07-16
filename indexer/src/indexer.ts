import { createReadStream } from "node:fs";
import { mkdir, writeFile } from "node:fs/promises";
import { dirname, isAbsolute, relative } from "node:path";
import { createInterface } from "node:readline";
import type { ChunkRecord } from "./contracts.js";
import type { EmbeddingProvider } from "./embeddings/embedding-provider.js";
import { assertValidVector } from "./embeddings/embedding-provider.js";
import { TransformersJsE5EmbeddingProvider } from "./embeddings/transformers-e5-provider.js";
import { POINT_ID_NAMESPACE, uuidV5 } from "./shared/deterministic-uuid.js";
import { sha256 } from "./shared/hashing.js";
import { stableJson } from "./shared/stable-json.js";
import { containsForbiddenControlCharacters } from "./text.js";
import { QdrantVectorStore } from "./vector-store/qdrant-vector-store.js";
import type {
  PipelineIdentity,
  PointPayload,
  StoredPoint,
  VectorPoint,
  VectorStore,
} from "./vector-store/vector-store.js";
import type { IndexerConfig } from "./config.js";
import { createLogger, type Logger } from "./logger.js";
import type { IndexerError, IndexerReport } from "./report.js";

const MAX_MODEL_TOKENS = 512;
const PAYLOAD_INDEXES = [
  "document_id",
  "root_page_id",
  "parent_page_id",
  "language",
  "status",
  "content_hash",
  "embedding_input_hash",
] as const;

export interface IndexerDependencies {
  embeddingProvider?: EmbeddingProvider;
  vectorStore?: VectorStore;
  logger?: Logger;
  now?: () => number;
}

interface PreparedChunk {
  line: number;
  pointId: string;
  chunk: ChunkRecord;
  embeddingInput: string;
  embeddingInputHash: string;
  payload: PointPayload;
}

interface EmbeddingCandidate extends PreparedChunk {
  reason: "new" | "changed" | "forced";
}

export async function indexChunks(
  config: IndexerConfig,
  dependencies: IndexerDependencies = {},
): Promise<IndexerReport> {
  const startedAt = performance.now();
  const now = dependencies.now ?? (() => performance.now());
  const logger = dependencies.logger ?? createLogger(config.logLevel);
  const provider =
    dependencies.embeddingProvider ??
    new TransformersJsE5EmbeddingProvider({
      modelId: config.modelId,
      revision: config.modelRevision,
      dtype: config.dtype,
      cacheDir: config.cacheDir,
      offline: config.offline,
      dimension: config.dimension,
    });
  const store =
    dependencies.vectorStore ??
    new QdrantVectorStore({
      url: config.qdrantUrl,
      collection: config.collection,
      ...(config.qdrantApiKey ? { apiKey: config.qdrantApiKey } : {}),
    });
  const pipeline = pipelineIdentity(config);
  const report = createReport(config);
  const vectorCache = new LruCache<string, number[]>(config.vectorCacheSize);
  const seenInputHashes = new Set<string>();
  const seenPointIds = new Set<string>();
  let stalePointIds = new Set<string>();
  let collectionAvailable = false;
  let inputHadErrors = false;
  let aborted = false;

  try {
    logger.info(`Connecting to Qdrant at ${redactUrl(config.qdrantUrl)}...`);
    await timed(report, "qdrant_read_ms", now, () => store.ping());
    const state = await timed(report, "qdrant_read_ms", now, () =>
      store.ensureCollection(
        {
          name: config.collection,
          dimension: config.dimension,
          distance: "Cosine",
          pipeline,
        },
        config.dryRun,
      ),
    );
    collectionAvailable = state.exists || state.created;
    logger.info(
      state.created
        ? `Created collection ${config.collection}.`
        : state.wouldCreate
          ? `Dry run: collection ${config.collection} would be created.`
          : `Collection ${config.collection} is compatible.`,
    );
    if (collectionAvailable) {
      await timed(report, "qdrant_write_ms", now, () =>
        store.ensurePayloadIndexes(PAYLOAD_INDEXES, config.dryRun),
      );
    }
    if (collectionAvailable && (config.prune || config.dryRun)) {
      const ids = await timed(report, "qdrant_read_ms", now, () =>
        store.listManagedPointIds(pipeline),
      );
      stalePointIds = new Set(ids);
    }

    let batch: Array<{ line: number; raw: string }> = [];
    const stream = createReadStream(config.inputFile, { encoding: "utf8" });
    const lines = createInterface({ input: stream, crlfDelay: Infinity });
    let lineNumber = 0;
    try {
      for await (const raw of lines) {
        lineNumber += 1;
        if (!raw.trim()) continue;
        report.input_chunks += 1;
        batch.push({ line: lineNumber, raw });
        if (batch.length < config.batchSize) continue;
        const outcome = await processBatch(batch, {
          config,
          provider,
          store,
          pipeline,
          report,
          vectorCache,
          seenInputHashes,
          seenPointIds,
          stalePointIds,
          collectionAvailable,
          logger,
          now,
        });
        inputHadErrors ||= outcome.hadInputErrors;
        batch = [];
        if (outcome.batchFailed && !config.continueOnError) {
          aborted = true;
          break;
        }
      }
      if (!aborted && batch.length > 0) {
        const outcome = await processBatch(batch, {
          config,
          provider,
          store,
          pipeline,
          report,
          vectorCache,
          seenInputHashes,
          seenPointIds,
          stalePointIds,
          collectionAvailable,
          logger,
          now,
        });
        inputHadErrors ||= outcome.hadInputErrors;
        aborted = outcome.batchFailed && !config.continueOnError;
      }
    } catch (error) {
      inputHadErrors = true;
      addError(report, null, null, "input", error);
    } finally {
      lines.close();
      stream.destroy();
    }

    report.stale_chunks_found = stalePointIds.size;
    if (config.prune && stalePointIds.size > 0) {
      if (inputHadErrors || aborted) {
        report.prune_skipped_due_to_errors = true;
        logger.warn("Prune skipped because input or batch errors were detected.");
      } else if (!config.dryRun) {
        for (const ids of partition([...stalePointIds], 128)) {
          try {
            await timed(report, "qdrant_write_ms", now, () => store.deletePoints(ids));
            report.deleted_chunks += ids.length;
          } catch (error) {
            addError(report, null, null, "prune", error);
            report.prune_skipped_due_to_errors = true;
            break;
          }
        }
      }
    }
  } catch (error) {
    addError(report, null, null, "qdrant", error);
  }

  report.model_loaded = provider.loaded;
  report.total_ms = rounded(performance.now() - startedAt);
  await writeReport(config.reportFile, report);
  logger.info(summary(report));
  return report;
}

interface BatchContext {
  config: IndexerConfig;
  provider: EmbeddingProvider;
  store: VectorStore;
  pipeline: PipelineIdentity;
  report: IndexerReport;
  vectorCache: LruCache<string, number[]>;
  seenInputHashes: Set<string>;
  seenPointIds: Set<string>;
  stalePointIds: Set<string>;
  collectionAvailable: boolean;
  logger: Logger;
  now: () => number;
}

async function processBatch(
  input: Array<{ line: number; raw: string }>,
  context: BatchContext,
): Promise<{ hadInputErrors: boolean; batchFailed: boolean }> {
  const prepared: PreparedChunk[] = [];
  let hadInputErrors = false;
  for (const item of input) {
    let chunkId: string | null = null;
    try {
      const chunk = parseChunk(item.raw);
      chunkId = chunk.chunk_id;
      const embeddingInput = `passage: ${chunk.embedding_text}`;
      const tokenCount = await context.provider.countTokens(embeddingInput);
      if (tokenCount > MAX_MODEL_TOKENS) {
        throw new Error(
          `Model input has ${tokenCount} tokens; maximum is ${MAX_MODEL_TOKENS}. Re-run chunker with the E5 tokenizer limits.`,
        );
      }
      const embeddingInputHash = sha256(embeddingInput);
      const payload = buildPayload(chunk, embeddingInputHash, context.pipeline, context.config);
      const pointId = uuidV5(chunk.chunk_id);
      if (context.seenPointIds.has(pointId)) {
        throw new Error(`Duplicate chunk_id in input: ${chunk.chunk_id}.`);
      }
      context.seenPointIds.add(pointId);
      context.stalePointIds.delete(pointId);
      prepared.push({
        line: item.line,
        pointId,
        chunk,
        embeddingInput,
        embeddingInputHash,
        payload,
      });
      context.report.valid_chunks += 1;
    } catch (error) {
      hadInputErrors = true;
      context.report.failed_chunks += 1;
      addError(context.report, item.line, chunkId, "validation", error);
    }
  }
  if (prepared.length === 0) return { hadInputErrors, batchFailed: false };

  try {
    const existing = context.collectionAvailable
      ? await timed(context.report, "qdrant_read_ms", context.now, () =>
          context.store.getPoints(prepared.map((item) => item.pointId)),
        )
      : [];
    const existingById = new Map(existing.map((point) => [point.id, point]));
    const embeddingCandidates: EmbeddingCandidate[] = [];
    const payloadUpdates: PreparedChunk[] = [];

    for (const item of prepared) {
      const current = existingById.get(item.pointId);
      if (!current) {
        context.report.new_chunks += 1;
        embeddingCandidates.push({ ...item, reason: "new" });
      } else if (
        context.config.forceReembed ||
        requiresEmbedding(current, item.embeddingInputHash, context.pipeline)
      ) {
        context.report.changed_embeddings += 1;
        embeddingCandidates.push({
          ...item,
          reason: context.config.forceReembed ? "forced" : "changed",
        });
      } else if (current.payload.payload_hash !== item.payload.payload_hash) {
        payloadUpdates.push(item);
      } else {
        context.report.skipped_unchanged += 1;
      }
    }

    if (context.config.dryRun) {
      context.report.payload_only_updates += payloadUpdates.length;
      context.logger.info(
        `Dry-run batch: ${embeddingCandidates.length} embeddings, ${payloadUpdates.length} payload updates.`,
      );
      return { hadInputErrors, batchFailed: false };
    }

    const points = await vectorizeCandidates(embeddingCandidates, context);
    if (points.length > 0) {
      await timed(context.report, "qdrant_write_ms", context.now, () =>
        context.store.upsert(points),
      );
      context.report.embedded_chunks += points.length;
      for (const item of embeddingCandidates) context.seenInputHashes.add(item.embeddingInputHash);
    }
    if (payloadUpdates.length > 0) {
      await timed(context.report, "qdrant_write_ms", context.now, () =>
        context.store.updatePayload(
          payloadUpdates.map((item) => ({ id: item.pointId, payload: item.payload })),
        ),
      );
      context.report.payload_only_updates += payloadUpdates.length;
    }
    context.logger.info(
      `Processed batch: ${points.length} vectors, ${payloadUpdates.length} payload updates.`,
    );
    return { hadInputErrors, batchFailed: false };
  } catch (error) {
    for (const item of prepared) {
      context.report.failed_chunks += 1;
      addError(context.report, item.line, item.chunk.chunk_id, "qdrant", error);
    }
    return { hadInputErrors, batchFailed: true };
  }
}

async function vectorizeCandidates(
  candidates: EmbeddingCandidate[],
  context: BatchContext,
): Promise<VectorPoint[]> {
  if (candidates.length === 0) return [];
  const byHash = new Map<string, EmbeddingCandidate[]>();
  for (const item of candidates) {
    const group = byHash.get(item.embeddingInputHash) ?? [];
    group.push(item);
    byHash.set(item.embeddingInputHash, group);
  }

  const vectors = new Map<string, number[]>();
  const toEmbed: EmbeddingCandidate[] = [];
  for (const [hash, group] of byHash) {
    const force = group.some((item) => item.reason === "forced");
    const canReuse = !force || context.seenInputHashes.has(hash);
    let vector = canReuse ? (context.vectorCache.get(hash) ?? null) : null;
    if (canReuse && !vector && context.collectionAvailable) {
      vector = await timed(context.report, "qdrant_read_ms", context.now, () =>
        context.store.findVector({ embeddingInputHash: hash, pipeline: context.pipeline }),
      );
    }
    if (vector) {
      assertValidVector(vector, context.config.dimension);
      vectors.set(hash, vector);
      context.vectorCache.set(hash, vector);
      context.report.reused_vectors_in_run += group.length;
    } else {
      const first = group[0];
      if (first) toEmbed.push(first);
    }
  }

  if (toEmbed.length > 0) {
    if (!context.provider.loaded) {
      context.logger.info(`Loading embedding model ${context.provider.modelId}...`);
      const started = context.now();
      await context.provider.load();
      context.report.model_load_ms += context.now() - started;
    }
    const started = context.now();
    const computed = await context.provider.embedDocuments(
      toEmbed.map((item) => item.embeddingInput),
    );
    context.report.embedding_ms += context.now() - started;
    for (const [index, item] of toEmbed.entries()) {
      const vector = computed[index];
      if (!vector) throw new Error(`Embedding provider omitted vector for ${item.chunk.chunk_id}.`);
      assertValidVector(vector, context.config.dimension);
      vectors.set(item.embeddingInputHash, vector);
      context.vectorCache.set(item.embeddingInputHash, vector);
      const duplicates = (byHash.get(item.embeddingInputHash)?.length ?? 1) - 1;
      context.report.reused_vectors_in_run += duplicates;
    }
  }

  return candidates.map((item) => {
    const vector = vectors.get(item.embeddingInputHash);
    if (!vector) throw new Error(`No vector available for ${item.chunk.chunk_id}.`);
    return { id: item.pointId, vector, payload: item.payload };
  });
}

function parseChunk(raw: string): ChunkRecord {
  let value: unknown;
  try {
    value = JSON.parse(raw);
  } catch (error) {
    throw new Error(`Invalid JSON: ${error instanceof Error ? error.message : String(error)}`);
  }
  if (!isRecord(value)) throw new Error("JSONL line must be an object.");
  requireString(value, "chunk_id");
  requireString(value, "document_id");
  requireString(value, "content");
  requireString(value, "embedding_text");
  requireString(value, "embedding_text_hash");
  requireString(value, "content_hash");
  if (value.parent_document_id !== null && typeof value.parent_document_id !== "string") {
    throw new Error("parent_document_id must be a string or null.");
  }
  if (value.previous_chunk_id !== null && typeof value.previous_chunk_id !== "string") {
    throw new Error("previous_chunk_id must be a string or null.");
  }
  if (value.next_chunk_id !== null && typeof value.next_chunk_id !== "string") {
    throw new Error("next_chunk_id must be a string or null.");
  }
  if (!isRecord(value.metadata)) throw new Error("metadata must be an object.");
  const metadata = value.metadata;
  requireString(metadata, "source");
  requireString(metadata, "language");
  requireString(metadata, "status");
  requireString(metadata, "section_title");
  for (const field of ["title", "page_id", "parent_page_id", "root_page_id", "category", "version", "date"] as const) {
    if (metadata[field] !== null && typeof metadata[field] !== "string") {
      throw new Error(`metadata.${field} must be a string or null.`);
    }
  }
  if (!Number.isInteger(metadata.depth) || (metadata.depth as number) < 0) {
    throw new Error("metadata.depth must be a non-negative integer.");
  }
  if (!Array.isArray(metadata.breadcrumbs) || !metadata.breadcrumbs.every(isString)) {
    throw new Error("metadata.breadcrumbs must be a string array.");
  }
  if (!Array.isArray(metadata.heading_path) || !metadata.heading_path.every(isString)) {
    throw new Error("metadata.heading_path must be a string array.");
  }
  if (!Array.isArray(metadata.ancestor_ids) || !metadata.ancestor_ids.every(isString)) {
    throw new Error("metadata.ancestor_ids must be a string array.");
  }
  const source = metadata.source as string;
  if (isAbsolute(source) || /^[A-Za-z]:[\\/]/u.test(source)) {
    throw new Error("metadata.source must not be an absolute path.");
  }
  const content = value.content as string;
  const embeddingText = value.embedding_text as string;
  if (!content.trim()) throw new Error("content must not be empty.");
  if (!embeddingText.trim()) throw new Error("embedding_text must not be empty.");
  if (containsForbiddenControlCharacters(content) || containsForbiddenControlCharacters(embeddingText)) {
    throw new Error("Chunk contains forbidden control characters.");
  }
  if (sha256(embeddingText) !== value.embedding_text_hash) {
    throw new Error("embedding_text_hash does not match embedding_text.");
  }
  if (sha256(content) !== value.content_hash) {
    throw new Error("content_hash does not match content.");
  }
  return value as unknown as ChunkRecord;
}

function buildPayload(
  chunk: ChunkRecord,
  embeddingInputHash: string,
  pipeline: PipelineIdentity,
  config: IndexerConfig,
): PointPayload {
  const metadata = chunk.metadata;
  const payload: PointPayload = {
    chunk_id: chunk.chunk_id,
    document_id: chunk.document_id,
    parent_document_id: chunk.parent_document_id,
    content: chunk.content,
    title: metadata.title,
    source: metadata.source,
    page_id: metadata.page_id,
    parent_page_id: metadata.parent_page_id,
    root_page_id: metadata.root_page_id,
    depth: metadata.depth,
    ancestor_ids: metadata.ancestor_ids,
    breadcrumbs: metadata.breadcrumbs,
    heading_path: metadata.heading_path,
    section_title: metadata.section_title,
    language: metadata.language,
    status: metadata.status,
    category: metadata.category,
    version: metadata.version,
    date: metadata.date,
    content_hash: chunk.content_hash,
    embedding_text_hash: chunk.embedding_text_hash,
    embedding_input_hash: embeddingInputHash,
    embedding_model: pipeline.embedding_model,
    embedding_model_revision: pipeline.embedding_model_revision,
    embedding_pipeline_version: pipeline.embedding_pipeline_version,
    embedding_dtype: pipeline.embedding_dtype,
    point_namespace: pipeline.point_namespace,
    previous_chunk_id: chunk.previous_chunk_id,
    next_chunk_id: chunk.next_chunk_id,
    duplicate_of_chunk_id: metadata.duplicate_of_chunk_id ?? null,
    ...(config.storeEmbeddingText ? { embedding_text: chunk.embedding_text } : {}),
  };
  return { ...payload, payload_hash: sha256(stableJson(payload)) };
}

function requiresEmbedding(
  existing: StoredPoint,
  inputHash: string,
  pipeline: PipelineIdentity,
): boolean {
  const payload = existing.payload;
  return (
    payload.embedding_input_hash !== inputHash ||
    payload.embedding_model !== pipeline.embedding_model ||
    payload.embedding_model_revision !== pipeline.embedding_model_revision ||
    payload.embedding_pipeline_version !== pipeline.embedding_pipeline_version
  );
}

function pipelineIdentity(config: IndexerConfig): PipelineIdentity {
  return {
    embedding_model: config.modelId,
    embedding_model_revision: config.modelRevision,
    embedding_dimension: config.dimension,
    embedding_dtype: config.dtype,
    embedding_pooling: "mean",
    embedding_normalized: true,
    document_prefix: "passage:",
    embedding_pipeline_version: config.pipelineVersion,
    point_namespace: POINT_ID_NAMESPACE,
  };
}

function createReport(config: IndexerConfig): IndexerReport {
  return {
    input_file: toPortablePath(relative(process.cwd(), config.inputFile) || "."),
    collection: config.collection,
    embedding_model: config.modelId,
    embedding_model_revision: config.modelRevision,
    embedding_pipeline_version: config.pipelineVersion,
    batch_size: config.batchSize,
    dry_run: config.dryRun,
    input_chunks: 0,
    valid_chunks: 0,
    new_chunks: 0,
    changed_embeddings: 0,
    embedded_chunks: 0,
    reused_vectors_in_run: 0,
    payload_only_updates: 0,
    skipped_unchanged: 0,
    stale_chunks_found: 0,
    deleted_chunks: 0,
    failed_chunks: 0,
    model_loaded: false,
    model_load_ms: 0,
    embedding_ms: 0,
    qdrant_read_ms: 0,
    qdrant_write_ms: 0,
    total_ms: 0,
    prune_skipped_due_to_errors: false,
    errors: [],
  };
}

function addError(
  report: IndexerReport,
  line: number | null,
  chunkId: string | null,
  stage: IndexerError["stage"],
  error: unknown,
): void {
  report.errors.push({
    line,
    chunk_id: chunkId,
    stage,
    message: error instanceof Error ? error.message : String(error),
  });
}

async function timed<T>(
  report: IndexerReport,
  field: "qdrant_read_ms" | "qdrant_write_ms",
  now: () => number,
  operation: () => Promise<T>,
): Promise<T> {
  const started = now();
  try {
    return await operation();
  } finally {
    report[field] += now() - started;
  }
}

async function writeReport(path: string, report: IndexerReport): Promise<void> {
  for (const field of [
    "model_load_ms",
    "embedding_ms",
    "qdrant_read_ms",
    "qdrant_write_ms",
    "total_ms",
  ] as const) {
    report[field] = rounded(report[field]);
  }
  await mkdir(dirname(path), { recursive: true });
  await writeFile(path, `${JSON.stringify(report, null, 2)}\n`, "utf8");
}

function summary(report: IndexerReport): string {
  return [
    `Indexing ${report.dry_run ? "dry-run " : ""}complete:`,
    `${report.valid_chunks}/${report.input_chunks} valid`,
    `${report.embedded_chunks} embedded`,
    `${report.payload_only_updates} payload-only`,
    `${report.skipped_unchanged} unchanged`,
    `${report.failed_chunks} failed`,
  ].join(" ");
}

class LruCache<K, V> {
  private readonly values = new Map<K, V>();
  constructor(private readonly capacity: number) {}

  get(key: K): V | undefined {
    const value = this.values.get(key);
    if (value === undefined) return undefined;
    this.values.delete(key);
    this.values.set(key, value);
    return value;
  }

  set(key: K, value: V): void {
    this.values.delete(key);
    this.values.set(key, value);
    while (this.values.size > this.capacity) {
      const oldest = this.values.keys().next().value as K | undefined;
      if (oldest === undefined) break;
      this.values.delete(oldest);
    }
  }
}

function partition<T>(values: T[], size: number): T[][] {
  const output: T[][] = [];
  for (let index = 0; index < values.length; index += size) {
    output.push(values.slice(index, index + size));
  }
  return output;
}

function requireString(record: Record<string, unknown>, key: string): void {
  if (typeof record[key] !== "string" || !(record[key] as string).trim()) {
    throw new Error(`${key} must be a non-empty string.`);
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function isString(value: unknown): value is string {
  return typeof value === "string";
}

function redactUrl(value: string): string {
  try {
    const url = new URL(value);
    url.username = "";
    url.password = "";
    return url.toString().replace(/\/$/u, "");
  } catch {
    return "[invalid Qdrant URL]";
  }
}

function toPortablePath(value: string): string {
  return value.replaceAll("\\", "/");
}

function rounded(value: number): number {
  return Math.round(value * 100) / 100;
}
