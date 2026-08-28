package answer

import (
	"net/url"
	"regexp"
)

var citationPattern = regexp.MustCompile(`\[S([1-9][0-9]*)\]`)

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

	return citationPattern.ReplaceAllStringFunc(result.Text, func(citation string) string {
		matches := citationPattern.FindStringSubmatch(citation)
		number := matches[1]
		label := "Источник " + number
		target, exists := sources["S"+number]
		if !exists || !isHTTPURL(target) {
			return label
		}
		return renderer.Link(label, target)
	})
}

func isHTTPURL(value string) bool {
	parsed, err := url.ParseRequestURI(value)
	if err != nil {
		return false
	}
	return (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}
