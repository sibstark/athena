package config

import (
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestLoadRequiresSecrets(t *testing.T) {
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
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	t.Setenv("RAG_API_KEY", "key")
	t.Setenv("DEEPSEEK_API_KEY", "key")
	t.Setenv("RAG_MAX_TOP_K", "many")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "integer") {
		t.Fatalf("error = %v", err)
	}
}
