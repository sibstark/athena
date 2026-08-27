package config

import (
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestLoadRequiresSecrets(t *testing.T) {
	setEmbeddingEnvironment(t)
	t.Setenv("API_KEY", "")
	t.Setenv("DEEPSEEK_API_KEY", "")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "API_KEY") || !strings.Contains(err.Error(), "DEEPSEEK_API_KEY") {
		t.Fatalf("error = %v", err)
	}
}

func TestLoadValidatesAuthenticationConfiguration(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("AUTH_SESSION_SECRET", "short")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "at least 32 bytes") {
		t.Fatalf("error = %v", err)
	}

	t.Setenv("AUTH_SESSION_SECRET", strings.Repeat("s", 32))
	t.Setenv("AUTH_PASSWORD_HASH", "not-bcrypt")
	_, err = Load()
	if err == nil || !strings.Contains(err.Error(), "bcrypt") {
		t.Fatalf("error = %v", err)
	}
}

func setRequiredEnvironment(t *testing.T) {
	t.Helper()
	setEmbeddingEnvironment(t)
	hash, err := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("API_KEY", "key")
	t.Setenv("DEEPSEEK_API_KEY", "key")
	t.Setenv("AUTH_USERNAME", "admin")
	t.Setenv("AUTH_PASSWORD_HASH", string(hash))
	t.Setenv("AUTH_SESSION_SECRET", strings.Repeat("s", 32))
}

func setEmbeddingEnvironment(t *testing.T) {
	t.Helper()
	values := map[string]string{
		"EMBEDDING_MODEL_ID": "test-model", "EMBEDDING_MODEL_REVISION": "test-revision",
		"EMBEDDING_DIMENSION": "4", "EMBEDDING_DTYPE": "test-dtype", "EMBEDDING_POOLING": "test-pooling",
		"EMBEDDING_NORMALIZED": "true", "EMBEDDING_DOCUMENT_PREFIX": "document: ", "EMBEDDING_QUERY_PREFIX": "query: ",
		"EMBEDDING_MAX_INPUT_TOKENS": "32", "EMBEDDING_AUTO_TRUNCATE": "false",
		"EMBEDDING_PIPELINE_VERSION": "test-pipeline", "EMBEDDING_POINT_NAMESPACE": "12345678-1234-1234-1234-123456789abc",
		"EMBEDDING_TOKENIZER_SHA256": strings.Repeat("a", 64), "EMBEDDING_TOKENIZER_CONFIG_SHA256": strings.Repeat("b", 64),
		"EMBEDDING_CACHE_DIR": "test-cache", "EMBEDDING_VECTOR_CACHE_SIZE": "8",
	}
	for name, value := range values {
		t.Setenv(name, value)
	}
}

func TestLoadRequiresEmbeddingContract(t *testing.T) {
	setRequiredEnvironment(t)
	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Embedding.DocumentPrefix != "document: " || loaded.Embedding.QueryPrefix != "query: " {
		t.Fatalf("prefixes were changed: %q / %q", loaded.Embedding.DocumentPrefix, loaded.Embedding.QueryPrefix)
	}
	t.Setenv("EMBEDDING_MODEL_ID", "")
	_, err = Load()
	if err == nil || !strings.Contains(err.Error(), "EMBEDDING_MODEL_ID") {
		t.Fatalf("error = %v", err)
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	t.Setenv("API_KEY", "key")
	t.Setenv("DEEPSEEK_API_KEY", "key")
	t.Setenv("RAG_MAX_TOP_K", "many")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "integer") {
		t.Fatalf("error = %v", err)
	}
}
