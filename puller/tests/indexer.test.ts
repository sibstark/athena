import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import type { ChunkRecord } from "../src/chunker.js";
import type { EmbeddingProvider } from "../src/embeddings/embedding-provider.js";
import {
  DEFAULT_COLLECTION,
  DEFAULT_DIMENSION,
  DEFAULT_DTYPE,
  DEFAULT_MODEL,
  DEFAULT_MODEL_REVISION,
  DEFAULT_PIPELINE_VERSION,
  type IndexerConfig,
} from "../src/indexer/config.js";
import { indexChunks } from "../src/indexer/indexer.js";
import type { Logger } from "../src/indexer/logger.js";
import { uuidV5 } from "../src/shared/deterministic-uuid.js";
import { sha256 } from "../src/shared/hashing.js";
import type {
  CollectionConfig,
  CollectionState,
  PayloadUpdate,
  PipelineIdentity,
  StoredPoint,
  VectorLookup,
  VectorPoint,
  VectorStore,
} from "../src/vector-store/vector-store.js";

const temporaryDirectories: string[] = [];

afterEach(async () => {
  await Promise.all(temporaryDirectories.splice(0).map((path) => rm(path, { recursive: true })));
});

describe("Qdrant indexer", () => {
  it("embeds and upserts a new chunk", async () => {
    const result = await run([chunk("one")]);
    expect(result.report).toMatchObject({ new_chunks: 1, embedded_chunks: 1 });
    expect(result.provider.embedCalls).toBe(1);
  });

  it("does not embed a fully unchanged chunk", async () => {
    const store = new MockStore();
    await run([chunk("one")], { store });
    const second = await run([chunk("one")], { store });
    expect(second.provider.embedCalls).toBe(0);
  });

  it("reports zero embeddings on an unchanged second run", async () => {
    const store = new MockStore();
    await run([chunk("one")], { store });
    const second = await run([chunk("one")], { store });
    expect(second.report).toMatchObject({ embedded_chunks: 0, payload_only_updates: 0, skipped_unchanged: 1 });
  });

  it("re-embeds when embedding_text_hash and text change", async () => {
    const store = new MockStore();
    const original = chunk("one");
    await run([original], { store });
    const changed = { ...original, embedding_text: "different", embedding_text_hash: sha256("different") };
    const result = await run([changed], { store });
    expect(result.report.changed_embeddings).toBe(1);
  });

  it("re-embeds when the stored embedding_input_hash changes", async () => {
    const store = new MockStore();
    const item = chunk("one");
    await run([item], { store });
    store.points.get(uuidV5(item.chunk_id))!.payload.embedding_input_hash = "stale";
    const result = await run([item], { store });
    expect(result.report.changed_embeddings).toBe(1);
  });

  it("updates payload without the model when only content changes", async () => {
    const store = new MockStore();
    const original = chunk("one");
    await run([original], { store });
    const changed = { ...original, content: "new rendered content", content_hash: sha256("new rendered content") };
    const result = await run([changed], { store });
    expect(result.report).toMatchObject({ payload_only_updates: 1, embedded_chunks: 0 });
    expect(result.provider.loadCalls).toBe(0);
  });

  it("re-embeds a point carrying another model id", async () => {
    const store = await indexedStore();
    firstPayload(store).embedding_model = "other/model";
    const result = await run([chunk("one")], { store });
    expect(result.report.changed_embeddings).toBe(1);
  });

  it("re-embeds a point carrying another model revision", async () => {
    const store = await indexedStore();
    firstPayload(store).embedding_model_revision = "old";
    const result = await run([chunk("one")], { store });
    expect(result.report.changed_embeddings).toBe(1);
  });

  it("re-embeds a point carrying another pipeline version", async () => {
    const store = await indexedStore();
    firstPayload(store).embedding_pipeline_version = "0";
    const result = await run([chunk("one")], { store });
    expect(result.report.changed_embeddings).toBe(1);
  });

  it("force-reembed recalculates unchanged chunks", async () => {
    const store = await indexedStore();
    const result = await run([chunk("one")], { store, config: { forceReembed: true } });
    expect(result.report.changed_embeddings).toBe(1);
    expect(result.provider.embedCalls).toBe(1);
  });

  it("force-reembed still computes duplicate inputs once per run", async () => {
    const store = new MockStore();
    const first = chunk("same", "chunk_force_a");
    const second = chunk("same", "chunk_force_b");
    await run([first, second], { store });
    const result = await run([first, second], {
      store,
      config: { forceReembed: true, batchSize: 1 },
    });
    expect(result.provider.embedCalls).toBe(1);
  });

  it("loads the model at most once for multiple batches", async () => {
    const result = await run([chunk("one"), chunk("two")], { config: { batchSize: 1 } });
    expect(result.provider.loadCalls).toBe(1);
  });

  it("does not load the model when no embeddings are needed", async () => {
    const store = await indexedStore();
    const result = await run([chunk("one")], { store });
    expect(result.provider.loaded).toBe(false);
  });

  it("computes identical embedding inputs once", async () => {
    const first = chunk("same", "chunk_first");
    const second = chunk("same", "chunk_second");
    const result = await run([first, second]);
    expect(result.provider.embeddedInputs).toHaveLength(1);
    expect(result.report.reused_vectors_in_run).toBe(1);
  });

  it("rejects vectors with an invalid dimension", async () => {
    const provider = new MockProvider();
    provider.vectorDimension = 2;
    const result = await run([chunk("one")], { provider });
    expect(result.report.failed_chunks).toBe(1);
  });

  it("rejects NaN vectors", async () => {
    const provider = new MockProvider();
    provider.invalidValue = Number.NaN;
    const result = await run([chunk("one")], { provider });
    expect(result.report.failed_chunks).toBe(1);
  });

  it("rejects Infinity vectors", async () => {
    const provider = new MockProvider();
    provider.invalidValue = Number.POSITIVE_INFINITY;
    const result = await run([chunk("one")], { provider });
    expect(result.report.failed_chunks).toBe(1);
  });

  it("uses the passage prefix for documents", async () => {
    const result = await run([chunk("one")]);
    expect(result.provider.embeddedInputs[0]).toBe("passage: one");
  });

  it("does not silently truncate inputs above 512 tokens", async () => {
    const provider = new MockProvider();
    provider.tokenCount = 513;
    const result = await run([chunk("one")], { provider });
    expect(result.report).toMatchObject({ failed_chunks: 1, embedded_chunks: 0 });
  });

  it("creates a stable deterministic UUID", () => {
    expect(uuidV5("chunk_example")).toBe(uuidV5("chunk_example"));
    expect(uuidV5("chunk_example")).toMatch(/^[0-9a-f-]{36}$/u);
  });

  it("reports a damaged JSONL line and continues", async () => {
    const result = await runRaw(["{bad", JSON.stringify(chunk("one"))]);
    expect(result.report).toMatchObject({ input_chunks: 2, valid_chunks: 1, embedded_chunks: 1, failed_chunks: 1 });
  });

  it("keeps a previously written batch after a later batch fails", async () => {
    const store = new MockStore();
    store.failUpsertAfter = 1;
    const result = await run([chunk("one"), chunk("two")], { store, config: { batchSize: 1 } });
    expect(store.points.size).toBe(1);
    expect(result.report.failed_chunks).toBeGreaterThan(0);
  });

  it("does not recreate an incompatible collection", async () => {
    const store = new MockStore();
    store.ensureError = new Error("incompatible collection");
    const result = await run([chunk("one")], { store });
    expect(result.report.errors[0]?.message).toContain("incompatible");
    expect(store.points.size).toBe(0);
  });

  it("dry-run performs no writes and does not load the model", async () => {
    const result = await run([chunk("one")], { config: { dryRun: true } });
    expect(result.store.upsertCalls).toBe(0);
    expect(result.provider.loadCalls).toBe(0);
    expect(result.report.new_chunks).toBe(1);
  });

  it("does not delete stale points without prune", async () => {
    const store = await indexedStore();
    await run([], { store });
    expect(store.points.size).toBe(1);
  });

  it("prune deletes managed points absent from input", async () => {
    const store = await indexedStore();
    const result = await run([], { store, config: { prune: true } });
    expect(result.report.deleted_chunks).toBe(1);
    expect(store.points.size).toBe(0);
  });

  it("input errors prevent prune", async () => {
    const store = await indexedStore();
    const result = await runRaw(["bad"], { store, config: { prune: true } });
    expect(result.report.prune_skipped_due_to_errors).toBe(true);
    expect(store.points.size).toBe(1);
  });

  it("payload-only update preserves the old vector", async () => {
    const store = await indexedStore();
    const before = [...store.points.values()][0]!.vector;
    const changed = { ...chunk("one"), content: "changed", content_hash: sha256("changed") };
    await run([changed], { store });
    expect([...store.points.values()][0]!.vector).toBe(before);
  });

  it("does not remove duplicate content in different chunks", async () => {
    const result = await run([chunk("same", "chunk_a"), chunk("same", "chunk_b")]);
    expect(result.store.points.size).toBe(2);
  });

  it("rejects duplicate chunk IDs in one input stream", async () => {
    const item = chunk("same", "chunk_duplicate_id");
    const result = await run([item, item]);
    expect(result.report).toMatchObject({ valid_chunks: 1, failed_chunks: 1 });
    expect(result.store.points.size).toBe(1);
  });

  it("rejects absolute source paths", async () => {
    const item = chunk("one");
    item.metadata.source = "/Users/private/document.md";
    const result = await run([item]);
    expect(result.report.failed_chunks).toBe(1);
  });

  it("does not log credentials embedded in the Qdrant URL", async () => {
    const messages: string[] = [];
    const logger = captureLogger(messages);
    await run([chunk("one")], {
      logger,
      config: { qdrantUrl: "http://secret:password@127.0.0.1:6333" },
    });
    expect(messages.join("\n")).not.toContain("secret");
    expect(messages.join("\n")).not.toContain("password");
  });

  it("is idempotent across repeated runs", async () => {
    const store = new MockStore();
    await run([chunk("one"), chunk("two")], { store });
    const second = await run([chunk("one"), chunk("two")], { store });
    expect(second.report).toMatchObject({ embedded_chunks: 0, skipped_unchanged: 2, failed_chunks: 0 });
  });
});

class MockProvider implements EmbeddingProvider {
  readonly modelId = DEFAULT_MODEL;
  readonly revision = DEFAULT_MODEL_REVISION;
  readonly dimension = DEFAULT_DIMENSION;
  readonly dtype = DEFAULT_DTYPE;
  loaded = false;
  loadCalls = 0;
  embedCalls = 0;
  embeddedInputs: string[] = [];
  tokenCount = 10;
  vectorDimension = DEFAULT_DIMENSION;
  invalidValue: number | null = null;

  async loadTokenizer(): Promise<void> {}
  async load(): Promise<void> {
    this.loadCalls += 1;
    this.loaded = true;
  }
  async countTokens(): Promise<number> {
    return this.tokenCount;
  }
  async embedDocuments(texts: string[]): Promise<number[][]> {
    this.embedCalls += 1;
    this.embeddedInputs.push(...texts);
    return texts.map((_, index) => {
      const vector = Array.from({ length: this.vectorDimension }, () => (index + 1) / 100);
      if (this.invalidValue !== null && vector.length > 0) vector[0] = this.invalidValue;
      return vector;
    });
  }
}

class MockStore implements VectorStore {
  points = new Map<string, VectorPoint>();
  upsertCalls = 0;
  deleteCalls = 0;
  failUpsertAfter: number | null = null;
  ensureError: Error | null = null;
  collectionExists = true;

  async ping(): Promise<void> {}
  async ensureCollection(_config: CollectionConfig, dryRun: boolean): Promise<CollectionState> {
    if (this.ensureError) throw this.ensureError;
    if (!this.collectionExists) {
      if (!dryRun) this.collectionExists = true;
      return { exists: false, created: !dryRun, wouldCreate: dryRun };
    }
    return { exists: true, created: false, wouldCreate: false };
  }
  async ensurePayloadIndexes(): Promise<void> {}
  async getPoints(ids: string[]): Promise<StoredPoint[]> {
    return ids.flatMap((id) => {
      const point = this.points.get(id);
      return point ? [{ id, payload: point.payload }] : [];
    });
  }
  async findVector(lookup: VectorLookup): Promise<number[] | null> {
    for (const point of this.points.values()) {
      if (
        point.payload.embedding_input_hash === lookup.embeddingInputHash &&
        point.payload.embedding_model === lookup.pipeline.embedding_model &&
        point.payload.embedding_model_revision === lookup.pipeline.embedding_model_revision &&
        point.payload.embedding_pipeline_version === lookup.pipeline.embedding_pipeline_version
      ) return point.vector;
    }
    return null;
  }
  async upsert(points: VectorPoint[]): Promise<void> {
    this.upsertCalls += 1;
    if (this.failUpsertAfter !== null && this.upsertCalls > this.failUpsertAfter) {
      throw new Error("simulated upsert failure");
    }
    for (const point of points) this.points.set(point.id, point);
  }
  async updatePayload(points: PayloadUpdate[]): Promise<void> {
    for (const update of points) {
      const point = this.points.get(update.id);
      if (point) this.points.set(update.id, { ...point, payload: update.payload });
    }
  }
  async deletePoints(ids: string[]): Promise<void> {
    this.deleteCalls += 1;
    for (const id of ids) this.points.delete(id);
  }
  async listManagedPointIds(_pipeline: PipelineIdentity): Promise<string[]> {
    return [...this.points.keys()];
  }
}

interface RunOptions {
  store?: MockStore;
  provider?: MockProvider;
  logger?: Logger;
  config?: Partial<IndexerConfig>;
}

async function run(chunks: ChunkRecord[], options: RunOptions = {}) {
  return runRaw(chunks.map((item) => JSON.stringify(item)), options);
}

async function runRaw(lines: string[], options: RunOptions = {}) {
  const directory = await mkdtemp(join(tmpdir(), "athena-indexer-"));
  temporaryDirectories.push(directory);
  const inputFile = join(directory, "chunks.jsonl");
  const reportFile = join(directory, "report.json");
  await writeFile(inputFile, `${lines.join("\n")}${lines.length ? "\n" : ""}`, "utf8");
  const store = options.store ?? new MockStore();
  const provider = options.provider ?? new MockProvider();
  const config: IndexerConfig = {
    inputFile,
    reportFile,
    collection: DEFAULT_COLLECTION,
    qdrantUrl: "http://127.0.0.1:6333",
    batchSize: 16,
    modelId: DEFAULT_MODEL,
    modelRevision: DEFAULT_MODEL_REVISION,
    dtype: DEFAULT_DTYPE,
    dimension: DEFAULT_DIMENSION,
    pipelineVersion: DEFAULT_PIPELINE_VERSION,
    cacheDir: join(directory, "models"),
    vectorCacheSize: 2,
    storeEmbeddingText: false,
    dryRun: false,
    forceReembed: false,
    prune: false,
    offline: true,
    continueOnError: false,
    logLevel: "error",
    ...options.config,
  };
  const report = await indexChunks(config, {
    vectorStore: store,
    embeddingProvider: provider,
    ...(options.logger ? { logger: options.logger } : {}),
  });
  expect(JSON.parse(await readFile(reportFile, "utf8"))).toMatchObject({ collection: DEFAULT_COLLECTION });
  return { report, store, provider };
}

function chunk(text: string, id = `chunk_${sha256(text)}`): ChunkRecord {
  return {
    chunk_id: id,
    document_id: "page_1",
    parent_document_id: null,
    content: `# Title\n\n${text}`,
    embedding_text: text,
    content_hash: sha256(`# Title\n\n${text}`),
    embedding_text_hash: sha256(text),
    metadata: {
      title: "Title",
      source: "documents/title--1.md",
      page_id: "1",
      parent_page_id: null,
      root_page_id: "1",
      depth: 0,
      ancestor_ids: [],
      breadcrumbs: ["Title"],
      heading_path: ["Title"],
      section_title: "Title",
      heading_level: 1,
      section_part: 1,
      section_parts_total: 1,
      chunk_index: 0,
      chunk_count: 1,
      token_count: 1,
      character_count: text.length,
      contains_table: false,
      contains_code: false,
      oversized: false,
      assets: [],
      external_urls: [],
      language: "ru",
      category: null,
      version: null,
      date: null,
      status: "active",
    },
    previous_chunk_id: null,
    next_chunk_id: null,
  };
}

async function indexedStore(): Promise<MockStore> {
  const store = new MockStore();
  await run([chunk("one")], { store });
  return store;
}

function firstPayload(store: MockStore): Record<string, unknown> {
  return [...store.points.values()][0]!.payload;
}

function captureLogger(messages: string[]): Logger {
  return {
    error: (message) => messages.push(message),
    warn: (message) => messages.push(message),
    info: (message) => messages.push(message),
    debug: (message) => messages.push(message),
  };
}
