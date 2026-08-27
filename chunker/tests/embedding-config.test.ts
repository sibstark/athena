import { describe, expect, it } from "vitest";
import { loadEmbeddingConfig } from "../src/embedding-config.js";

function environment(): NodeJS.ProcessEnv {
  return {
    EMBEDDING_MODEL_ID: "test-model",
    EMBEDDING_MODEL_REVISION: "test-revision",
    EMBEDDING_DIMENSION: "4",
    EMBEDDING_DTYPE: "test-dtype",
    EMBEDDING_POOLING: "test-pooling",
    EMBEDDING_NORMALIZED: "true",
    EMBEDDING_DOCUMENT_PREFIX: "document: ",
    EMBEDDING_QUERY_PREFIX: "query: ",
    EMBEDDING_MAX_INPUT_TOKENS: "32",
    EMBEDDING_AUTO_TRUNCATE: "false",
    EMBEDDING_PIPELINE_VERSION: "test-pipeline",
    EMBEDDING_POINT_NAMESPACE: "12345678-1234-1234-1234-123456789abc",
    EMBEDDING_TOKENIZER_SHA256: "a".repeat(64),
    EMBEDDING_TOKENIZER_CONFIG_SHA256: "b".repeat(64),
    EMBEDDING_CACHE_DIR: "test-cache",
    EMBEDDING_VECTOR_CACHE_SIZE: "8",
  };
}

describe("embedding configuration", () => {
  it("requires the complete contract and preserves prefix whitespace", () => {
    const config = loadEmbeddingConfig(environment());
    expect(config.documentPrefix).toBe("document: ");
    expect(config.queryPrefix).toBe("query: ");
    expect(() => loadEmbeddingConfig({ ...environment(), EMBEDDING_MODEL_ID: "" })).toThrow("EMBEDDING_MODEL_ID");
  });
});
