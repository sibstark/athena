package answer

import "testing"

type testRenderer struct{}

func (testRenderer) Link(label string, target string) string {
	return "<" + label + "|" + target + ">"
}

func TestFormatCitations(t *testing.T) {
	result := Result{
		Text: "Первый факт [S1], второй [S10]. Снова первый [S1].",
		Sources: []Source{
			{ID: "S1", URL: "https://example.test/document/1/"},
			{ID: "S10", URL: "http://example.test/document/10/"},
		},
	}

	got := Format(result, testRenderer{})
	want := "Первый факт <Источник 1|https://example.test/document/1/>, второй <Источник 10|http://example.test/document/10/>. Снова первый <Источник 1|https://example.test/document/1/>."
	if got != want {
		t.Fatalf("Format() = %q, want %q", got, want)
	}
}

func TestFormatUsesPlainLabelWhenSourceCannotBeLinked(t *testing.T) {
	tests := []struct {
		name    string
		sources []Source
	}{
		{name: "source missing"},
		{name: "URL missing", sources: []Source{{ID: "S2"}}},
		{name: "unsupported scheme", sources: []Source{{ID: "S2", URL: "javascript:alert(1)"}}},
		{name: "relative URL", sources: []Source{{ID: "S2", URL: "/note/document/2/"}}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := Format(Result{Text: "Факт [S2].", Sources: test.sources}, testRenderer{})
			if got != "Факт Источник 2." {
				t.Fatalf("Format() = %q", got)
			}
		})
	}
}

func TestFormatLeavesTextWithoutCitationsUnchanged(t *testing.T) {
	const text = "Ответ без источников."
	if got := Format(Result{Text: text}, testRenderer{}); got != text {
		t.Fatalf("Format() = %q, want %q", got, text)
	}
}

func TestFormatDoesNotTreatZeroOrMalformedReferencesAsCitations(t *testing.T) {
	const text = "Ссылки [S0], [S-1], [S] и [s1] невалидны."
	if got := Format(Result{Text: text}, testRenderer{}); got != text {
		t.Fatalf("Format() = %q, want %q", got, text)
	}
}
