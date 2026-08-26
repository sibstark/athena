package config

import (
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestLoadRequiresSecrets(t *testing.T) {
	setEmbeddingEnvironment(t)
	t.Setenv("RAG_API_KEY", "")
	t.Setenv("DEEPSEEK_API_KEY", "")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "RAG_API_KEY") || !strings.Contains(err.Error(), "DEEPSEEK_API_KEY") {
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
	hash, err := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("RAG_API_KEY", "key")
	t.Setenv("DEEPSEEK_API_KEY", "key")
	t.Setenv("AUTH_USERNAME", "admin")
	t.Setenv("AUTH_PASSWORD_HASH", string(hash))
	t.Setenv("AUTH_SESSION_SECRET", strings.Repeat("s", 32))
	setEmbeddingEnvironment(t)
}

func setEmbeddingEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("EMBEDDING_MODEL_ID", "test/model")
	t.Setenv("EMBEDDING_MODEL_REVISION", "test-revision")
	t.Setenv("EMBEDDING_DIMENSION", "8")
	t.Setenv("EMBEDDING_DTYPE", "float32")
	t.Setenv("EMBEDDING_POOLING", "cls")
	t.Setenv("EMBEDDING_NORMALIZED", "true")
	t.Setenv("EMBEDDING_DOCUMENT_PREFIX", "")
	t.Setenv("EMBEDDING_QUERY_PREFIX", "query: ")
	t.Setenv("EMBEDDING_MAX_INPUT_TOKENS", "8192")
	t.Setenv("EMBEDDING_AUTO_TRUNCATE", "false")
	t.Setenv("EMBEDDING_PIPELINE_VERSION", "2")
	t.Setenv("EMBEDDING_POINT_NAMESPACE", "6195ee42-5bb1-4440-93c2-285e67dbcbf2")
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("RAG_MAX_TOP_K", "many")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "integer") {
		t.Fatalf("error = %v", err)
	}
}
