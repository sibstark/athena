package indexer

import (
	"strings"
	"testing"
)

func TestLoadEmbeddingConfigRequiresEveryValueAndPreservesPrefixes(t *testing.T) {
	setEmbeddingEnvironment(t)
	config, err := LoadEmbeddingConfig()
	if err != nil {
		t.Fatal(err)
	}
	if config.DocumentPrefix != "document: " || config.QueryPrefix != "query: " {
		t.Fatalf("prefixes were changed: %q / %q", config.DocumentPrefix, config.QueryPrefix)
	}
	t.Setenv("EMBEDDING_MODEL_ID", "")
	if _, err := LoadEmbeddingConfig(); err == nil || !strings.Contains(err.Error(), "EMBEDDING_MODEL_ID") {
		t.Fatalf("missing model ID error = %v", err)
	}
}

func setEmbeddingEnvironment(t *testing.T) {
	t.Helper()
	config := testEmbeddingConfig()
	values := map[string]string{
		"EMBEDDING_MODEL_ID": config.ModelID, "EMBEDDING_MODEL_REVISION": config.ModelRevision,
		"EMBEDDING_DIMENSION": "4", "EMBEDDING_DTYPE": config.DType, "EMBEDDING_POOLING": config.Pooling,
		"EMBEDDING_NORMALIZED": "true", "EMBEDDING_DOCUMENT_PREFIX": config.DocumentPrefix,
		"EMBEDDING_QUERY_PREFIX": config.QueryPrefix, "EMBEDDING_MAX_INPUT_TOKENS": "32",
		"EMBEDDING_AUTO_TRUNCATE": "false", "EMBEDDING_PIPELINE_VERSION": config.PipelineVersion,
		"EMBEDDING_POINT_NAMESPACE": config.PointNamespace, "EMBEDDING_TOKENIZER_SHA256": config.TokenizerSHA256,
		"EMBEDDING_TOKENIZER_CONFIG_SHA256": config.TokenizerConfigSHA256, "EMBEDDING_CACHE_DIR": config.CacheDir,
		"EMBEDDING_VECTOR_CACHE_SIZE": "8",
	}
	for name, value := range values {
		t.Setenv(name, value)
	}
}
