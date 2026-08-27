const values: Record<string, string> = {
  EMBEDDING_MODEL_ID: "test-model",
  EMBEDDING_MODEL_REVISION: "test-revision",
  EMBEDDING_DIMENSION: "4",
  EMBEDDING_DTYPE: "q8",
  EMBEDDING_POOLING: "mean",
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

for (const [name, value] of Object.entries(values)) process.env[name] = value;
