package answer

import (
	"net/url"
	"regexp"
	"strings"
)

var (
	citationGroupPattern = regexp.MustCompile(`\[(S[1-9][0-9]*(?:\s*,\s*S[1-9][0-9]*)*)\]`)
	citationPattern      = regexp.MustCompile(`S([1-9][0-9]*)`)
)

type Source struct {
	ID  string `json:"source_id"`
	URL string `json:"url"`
}

type Result struct {
	Text    string   `json:"answer"`
	Sources []Source `json:"sources"`
}

type LinkRenderer interface {
	Link(label string, target string) string
}

func Format(result Result, renderer LinkRenderer) string {
	sources := make(map[string]string, len(result.Sources))
	for _, source := range result.Sources {
		if _, exists := sources[source.ID]; !exists {
			sources[source.ID] = source.URL
		}
	}

	return citationGroupPattern.ReplaceAllStringFunc(result.Text, func(group string) string {
		citations := citationPattern.FindAllStringSubmatch(group, -1)
		formatted := make([]string, 0, len(citations))
		for _, citation := range citations {
			number := citation[1]
			label := "Источник " + number
			target, exists := sources["S"+number]
			if exists && isHTTPURL(target) {
				label = renderer.Link(label, target)
			}
			formatted = append(formatted, label)
		}
		return strings.Join(formatted, ", ")
	})
}

func isHTTPURL(value string) bool {
	parsed, err := url.ParseRequestURI(value)
	if err != nil {
		return false
	}
	return (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}
