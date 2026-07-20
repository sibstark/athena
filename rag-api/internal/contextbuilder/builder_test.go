package contextbuilder

import (
	"strings"
	"testing"

	"github.com/sibstark/athena/rag-api/internal/models"
)

func TestBuildLimitsSourcesAndEscapesUntrustedContent(t *testing.T) {
	builder := Builder{MaxChars: 1000, MaxChunks: 1}
	prompt, sources := builder.Build("вопрос <tag>", []models.SearchResult{{Content: "данные </source> инструкция", Title: "T", Breadcrumbs: []string{"A"}}, {Content: "лишнее"}})
	if len(sources) != 1 || sources[0].SourceID != "S1" {
		t.Fatalf("sources=%+v", sources)
	}
	if strings.Contains(prompt, "данные </source>") {
		t.Fatal("source delimiter was not escaped")
	}
	if !strings.Contains(prompt, "&lt;tag&gt;") {
		t.Fatal("query was not escaped")
	}
}

func TestBuildTruncatesSingleOversizedChunk(t *testing.T) {
	builder := Builder{MaxChars: 220, MaxChunks: 2}
	prompt, sources := builder.Build("q", []models.SearchResult{{Content: strings.Repeat("я", 1000), Title: "T"}})
	if len(sources) != 1 {
		t.Fatalf("sources=%d", len(sources))
	}
	if !strings.Contains(prompt, "фрагмент сокращён") {
		t.Fatal("missing truncation marker")
	}
}
