package bitrix

import "testing"

func TestLinkRenderer(t *testing.T) {
	got := (LinkRenderer{}).Link("Источник 1", "https://example.test/note/document/3515/")
	want := "[url=https://example.test/note/document/3515/]Источник 1[/url]"
	if got != want {
		t.Fatalf("Link() = %q, want %q", got, want)
	}
}
