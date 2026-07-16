import { mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { QdrantClient } from "@qdrant/js-client-rest";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import type { ChunkRecord } from "../src/chunker.js";
import type { EmbeddingProvider } from "../src/embeddings/embedding-provider.js";
import {
  DEFAULT_DIMENSION,
  DEFAULT_DTYPE,
  DEFAULT_MODEL,
  DEFAULT_MODEL_REVISION,
  DEFAULT_PIPELINE_VERSION,
  type IndexerConfig,
} from "../src/indexer/config.js";
import { indexChunks } from "../src/indexer/indexer.js";
import { sha256 } from "../src/shared/hashing.js";
import { QdrantVectorStore } from "../src/vector-store/qdrant-vector-store.js";

const enabled = process.env.RUN_QDRANT_INTEGRATION === "1";
const qdrantUrl = process.env.QDRANT_URL ?? "http://127.0.0.1:6333";
const collection = process.env.QDRANT_INTEGRATION_COLLECTION ?? "athena_indexer_integration";
let directory = "";

describe.skipIf(!enabled)("Qdrant integration", () => {
  beforeAll(async () => {
    directory = await mkdtemp(join(tmpdir(), "athena-qdrant-integration-"));
    await deleteCollection();
  });

  afterAll(async () => {
    await deleteCollection();
    if (directory) await rm(directory, { recursive: true, force: true });
  });

  it("runs incremental, payload-only, and changed-input updates against real Qdrant", async () => {
    const firstProvider = new IntegrationProvider();
    const first = await run(chunk("Первый текст", "Исходный content"), firstProvider, "first");
    expect(first).toMatchObject({ embedded_chunks: 1, new_chunks: 1, failed_chunks: 0 });

    const secondProvider = new IntegrationProvider();
    const second = await run(chunk("Первый текст", "Исходный content"), secondProvider, "second");
    expect(second).toMatchObject({ embedded_chunks: 0, skipped_unchanged: 1 });
    expect(secondProvider.loadCalls).toBe(0);

    const metadataProvider = new IntegrationProvider();
    const metadata = await run(chunk("Первый текст", "Изменённый content"), metadataProvider, "metadata");
    expect(metadata).toMatchObject({ payload_only_updates: 1, embedded_chunks: 0 });
    expect(metadataProvider.loadCalls).toBe(0);

    const changedProvider = new IntegrationProvider();
    const changed = await run(chunk("Изменённый embedding text", "Изменённый content"), changedProvider, "changed");
    expect(changed).toMatchObject({ changed_embeddings: 1, embedded_chunks: 1 });
    expect(changedProvider.loadCalls).toBe(1);
  });
});

async function run(item: ChunkRecord, provider: IntegrationProvider, suffix: string) {
  const inputFile = join(directory, `${suffix}.jsonl`);
  await writeFile(inputFile, `${JSON.stringify(item)}\n`, "utf8");
  const config: IndexerConfig = {
    inputFile,
    reportFile: join(directory, `${suffix}-report.json`),
    collection,
    qdrantUrl,
    ...(process.env.QDRANT_API_KEY ? { qdrantApiKey: process.env.QDRANT_API_KEY } : {}),
    batchSize: 4,
    modelId: DEFAULT_MODEL,
    modelRevision: DEFAULT_MODEL_REVISION,
    dtype: DEFAULT_DTYPE,
    dimension: DEFAULT_DIMENSION,
    pipelineVersion: DEFAULT_PIPELINE_VERSION,
    cacheDir: join(directory, "models"),
    vectorCacheSize: 4,
    storeEmbeddingText: false,
    dryRun: false,
    forceReembed: false,
    prune: false,
    offline: true,
    continueOnError: false,
    logLevel: "error",
  };
  const store = new QdrantVectorStore({
    url: qdrantUrl,
    collection,
    ...(process.env.QDRANT_API_KEY ? { apiKey: process.env.QDRANT_API_KEY } : {}),
  });
  return indexChunks(config, { embeddingProvider: provider, vectorStore: store });
}

class IntegrationProvider implements EmbeddingProvider {
  readonly modelId = DEFAULT_MODEL;
  readonly revision = DEFAULT_MODEL_REVISION;
  readonly dimension = DEFAULT_DIMENSION;
  readonly dtype = DEFAULT_DTYPE;
  loaded = false;
  loadCalls = 0;
  async loadTokenizer(): Promise<void> {}
  async load(): Promise<void> {
    this.loaded = true;
    this.loadCalls += 1;
  }
  async countTokens(): Promise<number> {
    return 20;
  }
  async embedDocuments(texts: string[]): Promise<number[][]> {
    return texts.map((text) => {
      const seed = Number.parseInt(sha256(text).slice(0, 8), 16) / 0xffffffff;
      return Array.from({ length: DEFAULT_DIMENSION }, (_, index) => seed + index / 100_000);
    });
  }
}

function chunk(embeddingText: string, content: string): ChunkRecord {
  return {
    chunk_id: "chunk_integration_stable",
    document_id: "page_integration",
    parent_document_id: null,
    content,
    embedding_text: embeddingText,
    content_hash: sha256(content),
    embedding_text_hash: sha256(embeddingText),
    metadata: {
      title: "Integration",
      source: "integration/document.md",
      page_id: null,
      parent_page_id: null,
      root_page_id: null,
      depth: 0,
      ancestor_ids: [],
      breadcrumbs: ["Integration"],
      heading_path: [],
      section_title: "Преамбула",
      heading_level: 0,
      section_part: 1,
      section_parts_total: 1,
      chunk_index: 0,
      chunk_count: 1,
      token_count: 10,
      character_count: content.length,
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

async function deleteCollection(): Promise<void> {
  const client = new QdrantClient({
    url: qdrantUrl,
    ...(process.env.QDRANT_API_KEY ? { apiKey: process.env.QDRANT_API_KEY } : {}),
  });
  if ((await client.collectionExists(collection)).exists) {
    await client.deleteCollection(collection);
  }
}
