import { describe, expect, it } from "vitest";
import { runCompatibilityTest } from "../src/compatibility.js";
import type { ChunkRecord, CollectionSnapshot, SearchHit } from "../src/contracts.js";
import { E5_MODEL_CONTRACT } from "../src/model-contract.js";
import type { EmbeddingProvider } from "../src/providers/embedding-provider.js";
import type { TeiE5EmbeddingProvider } from "../src/providers/tei-e5-provider.js";
import type { ReadOnlyQdrantStore } from "../src/qdrant/read-only-qdrant-store.js";

const unit = (axis = 0) => Array.from({ length: E5_MODEL_CONTRACT.dimension }, (_, index) => index === axis ? 1 : 0);

describe("compatibility verdict", () => {
  it("returns PASS_STRICT for the exact configured artifact and compatible retrieval", async () => {
    const report = await runCompatibilityTest(setup(unit(), unit()));
    expect(report.pipeline_comparison).toMatchObject({ compatible: true, exact_match: true });
    expect(report.verdict).toBe("PASS_STRICT");
    expect(report.collection_unchanged).toBe(true);
    expect(JSON.stringify(report)).not.toContain("0,0,0,0,0,0,0,0,0,0");
  });

  it("returns FAIL for low paired cosine", async () => {
    const report = await runCompatibilityTest(setup(unit(), unit(1)));
    expect(report.verdict).toBe("FAIL");
    expect(report.collection_unchanged).toBe(true);
  });

  it("returns FAIL when the collection changes", async () => {
    const options = setup(unit(), unit());
    let call = 0;
    const qdrant = options.qdrant as unknown as FakeQdrant;
    qdrant.snapshot = async () => ({ ...snapshot(), pointsCount: call++ === 0 ? 1 : 2 });
    const report = await runCompatibilityTest(options);
    expect(report.verdict).toBe("FAIL");
    expect(report.errors.join(" ")).toContain("collection changed");
  });

  it("dry-run performs no embedding calls", async () => {
    const options = setup(unit(), unit());
    let embeddings = 0;
    const tei = options.tei as unknown as FakeTei;
    tei.embedDocuments = async () => { embeddings += 1; return [unit()]; };
    const report = await runCompatibilityTest({ ...options, dryRun: true });
    expect(report.verdict).toBe("WARN");
    expect(embeddings).toBe(0);
  });
});

function setup(stored: number[], generated: number[]) {
  const chunks: ChunkRecord[] = [{
    chunk_id: "chunk_1",
    document_id: "page_1",
    parent_document_id: null,
    content: "Текст",
    embedding_text: "Путь: Курс\n\nТекст",
    embedding_text_hash: "hash",
    metadata: { title: "Документ", token_count: 10, heading_path: [] },
  }];
  const queryVector = unit();
  return {
    chunks,
    queries: [{ id: "q1", query: "Вопрос", expected_document_ids: ["page_1"] }],
    qdrant: new FakeQdrant(stored) as unknown as ReadOnlyQdrantStore,
    tei: new FakeTei(generated, queryVector) as unknown as TeiE5EmbeddingProvider,
    reference: new FakeReference(queryVector),
    topK: 10,
    modelArtifact: {
      model_id: E5_MODEL_CONTRACT.modelId,
      revision: E5_MODEL_CONTRACT.revision,
      source_artifact: "onnx/model_quantized.onnx",
      served_artifact: "onnx/model.onnx",
      dtype: "q8",
      sha256: "a".repeat(64),
    },
  };
}

class FakeTei {
  constructor(private readonly documentVector: number[], private readonly queryVector = unit()) {}
  async getInfo() { return { model_id: E5_MODEL_CONTRACT.modelId, model_sha: E5_MODEL_CONTRACT.revision, model_dtype: "test-dtype", max_input_length: E5_MODEL_CONTRACT.maxInputTokens, auto_truncate: E5_MODEL_CONTRACT.autoTruncate, version: "test-version", backend: "test-backend" }; }
  async embedDocuments(texts: string[]) { return texts.map(() => this.documentVector); }
  async embedQueries(texts: string[]) { return texts.map(() => this.queryVector); }
}

class FakeReference implements EmbeddingProvider {
  readonly modelId = E5_MODEL_CONTRACT.modelId;
  readonly revision = E5_MODEL_CONTRACT.revision;
  readonly dimension = E5_MODEL_CONTRACT.dimension;
  constructor(private readonly vector: number[]) {}
  async embedDocuments(texts: string[]) { return texts.map(() => this.vector); }
  async embedQueries(texts: string[]) { return texts.map(() => this.vector); }
}

class FakeQdrant {
  constructor(private readonly vector: number[]) {}
  async snapshot(): Promise<CollectionSnapshot> { return snapshot(); }
  async getPointsByChunkIds() { return new Map([["chunk_1", { id: "id", payload: { chunk_id: "chunk_1", document_id: "page_1" }, vector: this.vector }]]); }
  async search(): Promise<SearchHit[]> { return [{ id: "id", score: 1, payload: { chunk_id: "chunk_1", document_id: "page_1" } }]; }
}

function snapshot(): CollectionSnapshot {
  return {
    name: "methodology_e5_small_v1",
    status: "green",
    pointsCount: 1,
    dimension: E5_MODEL_CONTRACT.dimension,
    distance: "Cosine",
    metadata: {
      embedding_model: E5_MODEL_CONTRACT.modelId,
      embedding_model_revision: E5_MODEL_CONTRACT.revision,
      embedding_pooling: "mean",
      embedding_normalized: true,
      embedding_dtype: "q8",
      document_prefix: "passage:",
    },
    payloadSchema: {},
  };
}
