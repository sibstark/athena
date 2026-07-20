package config

import (
	"strings"
	"testing"
)

func TestLoadRequiresSecrets(t *testing.T) {
	t.Setenv("RAG_API_KEY", "")
	t.Setenv("DEEPSEEK_API_KEY", "")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "RAG_API_KEY") || !strings.Contains(err.Error(), "DEEPSEEK_API_KEY") {
		t.Fatalf("error = %v", err)
	}
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
