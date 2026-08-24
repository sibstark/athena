package normalizer

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
	"golang.org/x/text/unicode/norm"
	"gopkg.in/yaml.v3"
)

type parsedFrontMatter struct {
	Attributes map[string]any
	Markdown   string
}

type controlReplacement struct {
	Character string
	Count     int
}

type markdownLink struct {
	Destination string
	Label       string
}

type markdownAnalysis struct {
	ExternalURLs []string
	Assets       []NormalizedAsset
	Links        []markdownLink
	SemanticText string
	WordCount    int
	HeadingCount int
	Headings     map[string]struct{}
	AnchorLabels map[string]string
	AnchorOrder  []string
}

type parsedDocument struct {
	Attributes          map[string]any
	Content             string
	Version             *string
	Date                *string
	Category            *string
	Status              string
	Language            string
	ContentHash         string
	DocumentType        DocumentType
	Indexable           bool
	Analysis            markdownAnalysis
	ControlReplacements []controlReplacement
	SuspiciousReasons   []string
	MissingTOCSections  []string
}

var (
	externalURLPattern   = regexp.MustCompile(`(?i)https?://[^\s<>"'` + "`" + `]+`)
	wordPattern          = regexp.MustCompile(`[\p{L}\p{N}]+(?:[’'-][\p{L}\p{N}]+)*`)
	headingLinePattern   = regexp.MustCompile(`^#{1,6}(?:[ \t]+.*)?$`)
	emptyListPattern     = regexp.MustCompile(`^(?:[-*+]|\d+[.)])[ \t]*$`)
	fenceStartPattern    = regexp.MustCompile(`^[ \t]{0,3}(` + "`" + `{3,}|~{3,})`)
	htmlReferencePattern = regexp.MustCompile(`(?i)<(img|a)\b[^>]*?\b(?:src|href)=["']([^"']+)["'][^>]*>`)
)

func parseAndNormalizeMarkdown(input []byte, threshold, documentID int, leaf bool) (parsedDocument, error) {
	if !utf8.Valid(input) {
		return parsedDocument{}, errors.New("file is not valid UTF-8")
	}
	value := strings.TrimPrefix(string(input), "\uFEFF")
	frontMatter, err := parseFrontMatter(value)
	if err != nil {
		return parsedDocument{}, err
	}
	sanitized, replacements := sanitizeControlCharacters(frontMatter.Markdown)
	content := normalizeMarkdown(sanitized)
	analysis, err := analyzeMarkdown(content, documentID)
	if err != nil {
		return parsedDocument{}, err
	}
	usefulCharacters := utf8.RuneCountInString(analysis.SemanticText)
	documentType := DocumentMarkdown
	switch {
	case usefulCharacters == 0 && len(analysis.ExternalURLs) == 0:
		documentType = DocumentEmpty
	case usefulCharacters == 0:
		documentType = DocumentExternalLink
	case usefulCharacters < threshold:
		documentType = DocumentShortNote
	}
	reasons, missing := suspiciousReasons(content, documentID, leaf, analysis)
	version, err := optionalScalar(frontMatter.Attributes, "version")
	if err != nil {
		return parsedDocument{}, err
	}
	date, err := optionalScalar(frontMatter.Attributes, "date")
	if err != nil {
		return parsedDocument{}, err
	}
	category, err := optionalScalar(frontMatter.Attributes, "category")
	if err != nil {
		return parsedDocument{}, err
	}
	status, err := optionalScalar(frontMatter.Attributes, "status")
	if err != nil {
		return parsedDocument{}, err
	}
	language, err := optionalScalar(frontMatter.Attributes, "language")
	if err != nil {
		return parsedDocument{}, err
	}
	return parsedDocument{
		Attributes: frontMatter.Attributes, Content: content, Version: version, Date: date,
		Category: category, Status: valueOr(status, "active"), Language: valueOr(language, "ru"),
		ContentHash: sha256Hex(content), DocumentType: documentType,
		Indexable: documentType != DocumentExternalLink && documentType != DocumentEmpty,
		Analysis:  analysis, ControlReplacements: replacements,
		SuspiciousReasons: reasons, MissingTOCSections: missing,
	}, nil
}

func parseFrontMatter(input string) (parsedFrontMatter, error) {
	normalized := normalizeLineEndings(input)
	lines := strings.Split(normalized, "\n")
	if len(lines) == 0 || lines[0] != "---" {
		return parsedFrontMatter{Attributes: map[string]any{}, Markdown: normalized}, nil
	}
	closing := -1
	for index := 1; index < len(lines); index++ {
		if lines[index] == "---" || lines[index] == "..." {
			closing = index
			break
		}
	}
	if closing < 0 {
		return parsedFrontMatter{}, errors.New("YAML Front Matter is missing a closing delimiter")
	}
	attributes := map[string]any{}
	yamlSource := strings.Join(lines[1:closing], "\n")
	if strings.TrimSpace(yamlSource) != "" {
		var decoded any
		if err := yaml.Unmarshal([]byte(yamlSource), &decoded); err != nil {
			return parsedFrontMatter{}, fmt.Errorf("invalid YAML Front Matter: %s", singleLine(err.Error()))
		}
		mapping, ok := decoded.(map[string]any)
		if !ok {
			return parsedFrontMatter{}, errors.New("invalid YAML Front Matter: the root value must be an object")
		}
		attributes = mapping
	}
	return parsedFrontMatter{Attributes: attributes, Markdown: strings.Join(lines[closing+1:], "\n")}, nil
}

func normalizeMarkdown(markdown string) string {
	withLF := normalizeLineEndings(markdown)
	lines := strings.Split(withLF, "\n")
	protected := protectedCodeLines(lines)
	output := make([]string, 0, len(lines))
	blankRun := 0
	for index, original := range lines {
		if protected[index] {
			output = append(output, original)
			blankRun = 0
			continue
		}
		line := strings.TrimRight(original, " \t")
		line = decodeEntitiesInExternalURLs(line)
		if line == "" {
			blankRun++
			if blankRun <= 2 {
				output = append(output, "")
			}
			continue
		}
		blankRun = 0
		output = append(output, line)
	}
	for len(output) > 0 && output[0] == "" {
		output = output[1:]
	}
	for len(output) > 0 && output[len(output)-1] == "" {
		output = output[:len(output)-1]
	}
	return strings.Join(output, "\n")
}

func protectedCodeLines(lines []string) []bool {
	protected := make([]bool, len(lines))
	inFence := false
	marker := byte(0)
	markerLength := 0
	for index, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		indent := len(line) - len(trimmed)
		if inFence {
			protected[index] = true
			if indent <= 3 && fenceRun(trimmed, marker) >= markerLength {
				inFence = false
			}
			continue
		}
		match := fenceStartPattern.FindStringSubmatch(line)
		if len(match) > 1 {
			inFence = true
			marker = match[1][0]
			markerLength = len(match[1])
			protected[index] = true
			continue
		}
		if strings.HasPrefix(line, "\t") || strings.HasPrefix(line, "    ") {
			protected[index] = true
		}
	}
	for index := 1; index+1 < len(lines); index++ {
		if strings.TrimSpace(lines[index]) == "" && protected[index-1] && protected[index+1] {
			protected[index] = true
		}
	}
	return protected
}

func fenceRun(value string, marker byte) int {
	count := 0
	for count < len(value) && value[count] == marker {
		count++
	}
	return count
}

func analyzeMarkdown(markdown string, documentID int) (markdownAnalysis, error) {
	result := markdownAnalysis{
		ExternalURLs: []string{}, Assets: []NormalizedAsset{}, Links: []markdownLink{},
		Headings: map[string]struct{}{}, AnchorLabels: map[string]string{}, AnchorOrder: []string{},
	}
	if markdown == "" {
		return result, nil
	}
	source := []byte(markdown)
	document := goldmark.DefaultParser().Parse(text.NewReader(source))
	seenURLs := map[string]struct{}{}
	semantic := make([]string, 0)
	addURL := func(raw string) {
		cleaned := cleanExternalURL(raw)
		if cleaned == "" {
			return
		}
		if _, exists := seenURLs[cleaned]; exists {
			return
		}
		seenURLs[cleaned] = struct{}{}
		result.ExternalURLs = append(result.ExternalURLs, cleaned)
	}
	if err := ast.Walk(document, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch typed := node.(type) {
		case *ast.Heading:
			result.HeadingCount++
			label := nodeText(typed, source)
			if normalized := normalizeIdentity(label); normalized != "" {
				result.Headings[normalized] = struct{}{}
			}
		case *ast.Image:
			path, err := normalizeAssetPath(string(typed.Destination))
			if err != nil {
				return ast.WalkStop, err
			}
			result.Assets = append(result.Assets, NormalizedAsset{Type: "image", Path: path, Alt: nodeText(typed, source)})
			return ast.WalkSkipChildren, nil
		case *ast.Link:
			destination := html.UnescapeString(strings.TrimSpace(string(typed.Destination)))
			label := nodeText(typed, source)
			result.Links = append(result.Links, markdownLink{Destination: destination, Label: label})
			addURL(destination)
			if isSameDocumentAnchor(destination, documentID) {
				normalized := normalizeIdentity(label)
				if normalized != "" {
					if _, exists := result.AnchorLabels[normalized]; !exists {
						result.AnchorLabels[normalized] = label
						result.AnchorOrder = append(result.AnchorOrder, normalized)
					}
				}
			}
		case *ast.AutoLink:
			addURL(string(typed.URL(source)))
			return ast.WalkSkipChildren, nil
		case *ast.Text:
			value := string(typed.Segment.Value(source))
			for _, raw := range externalURLPattern.FindAllString(value, -1) {
				addURL(raw)
			}
			semantic = append(semantic, externalURLPattern.ReplaceAllString(value, " "))
		case *ast.CodeSpan:
			semantic = append(semantic, string(typed.Text(source)))
			return ast.WalkSkipChildren, nil
		case *ast.FencedCodeBlock:
			semantic = append(semantic, linesValue(typed.Lines(), source))
			return ast.WalkSkipChildren, nil
		case *ast.CodeBlock:
			semantic = append(semantic, linesValue(typed.Lines(), source))
			return ast.WalkSkipChildren, nil
		case *ast.RawHTML:
			value := rawHTMLValue(typed, source)
			for _, raw := range externalURLPattern.FindAllString(value, -1) {
				addURL(raw)
			}
			withoutURLs := externalURLPattern.ReplaceAllString(value, " ")
			semantic = append(semantic, stripHTMLTags(withoutURLs))
		}
		return ast.WalkContinue, nil
	}); err != nil {
		return markdownAnalysis{}, err
	}
	for _, match := range htmlReferencePattern.FindAllStringSubmatch(markdown, -1) {
		destination := html.UnescapeString(strings.TrimSpace(match[2]))
		if strings.EqualFold(match[1], "img") {
			path, err := normalizeAssetPath(destination)
			if err != nil {
				return markdownAnalysis{}, err
			}
			result.Assets = append(result.Assets, NormalizedAsset{Type: "image", Path: path, Alt: ""})
		} else {
			result.Links = append(result.Links, markdownLink{Destination: destination})
			addURL(destination)
		}
	}
	result.SemanticText = strings.Join(strings.Fields(strings.Join(semantic, " ")), " ")
	result.WordCount = len(wordPattern.FindAllString(result.SemanticText, -1))
	return result, nil
}

func suspiciousReasons(markdown string, documentID int, leaf bool, analysis markdownAnalysis) ([]string, []string) {
	reasons := make([]string, 0)
	missing := make([]string, 0)
	if len(analysis.AnchorLabels) >= 2 {
		for _, key := range analysis.AnchorOrder {
			if _, exists := analysis.Headings[key]; !exists {
				missing = append(missing, analysis.AnchorLabels[key])
			}
		}
		if len(missing) > 0 {
			reasons = append(reasons, "missing_toc_sections")
		}
	}
	trimmed := strings.TrimRight(markdown, "\n\r \t")
	lastLine := trimmed
	if index := strings.LastIndex(trimmed, "\n"); index >= 0 {
		lastLine = trimmed[index+1:]
	}
	lastLine = strings.TrimSpace(lastLine)
	if headingLinePattern.MatchString(lastLine) {
		reasons = append(reasons, "ends_with_heading")
	}
	if emptyListPattern.MatchString(lastLine) {
		reasons = append(reasons, "ends_with_empty_list_item")
	}
	if unclosedFence(markdown, '`') || unclosedFence(markdown, '~') {
		reasons = append(reasons, "unclosed_fenced_code_block")
	}
	characters := utf8.RuneCountInString(markdown)
	if leaf && characters == 0 {
		reasons = append(reasons, "empty_leaf")
	} else if leaf && characters < 50 {
		reasons = append(reasons, "very_short_leaf")
	}
	_ = documentID
	return reasons, missing
}

func unclosedFence(markdown string, marker byte) bool {
	count := 0
	for _, line := range strings.Split(markdown, "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if len(line)-len(trimmed) <= 3 && fenceRun(trimmed, marker) >= 3 {
			count++
		}
	}
	return count%2 != 0
}

func sanitizeControlCharacters(value string) (string, []controlReplacement) {
	counts := map[rune]int{}
	var builder strings.Builder
	for _, character := range value {
		if (character >= 0 && character <= 8) || character == 11 || character == 12 || (character >= 14 && character <= 31) {
			counts[character]++
			builder.WriteByte(' ')
			continue
		}
		builder.WriteRune(character)
	}
	characters := make([]int, 0, len(counts))
	for character := range counts {
		characters = append(characters, int(character))
	}
	sort.Ints(characters)
	replacements := make([]controlReplacement, 0, len(characters))
	for _, character := range characters {
		replacements = append(replacements, controlReplacement{
			Character: fmt.Sprintf("U+%04X", character), Count: counts[rune(character)],
		})
	}
	return builder.String(), replacements
}

func optionalScalar(attributes map[string]any, field string) (*string, error) {
	value, exists := attributes[field]
	if !exists || value == nil {
		return nil, nil
	}
	var result string
	switch typed := value.(type) {
	case string:
		result = strings.TrimSpace(typed)
	case int:
		result = strconv.Itoa(typed)
	case int64:
		result = strconv.FormatInt(typed, 10)
	case uint64:
		result = strconv.FormatUint(typed, 10)
	case float64:
		result = strconv.FormatFloat(typed, 'g', -1, 64)
	case bool:
		result = strconv.FormatBool(typed)
	case time.Time:
		result = typed.Format(time.RFC3339Nano)
	default:
		return nil, fmt.Errorf("YAML field %q must be a scalar value or null", field)
	}
	if result == "" {
		return nil, nil
	}
	return &result, nil
}

func scalarForComparison(attributes map[string]any, field string) (string, bool) {
	value, err := optionalScalar(attributes, field)
	if err != nil || value == nil {
		return "", false
	}
	return *value, true
}

func normalizeAssetPath(value string) (string, error) {
	decoded := strings.ReplaceAll(html.UnescapeString(strings.TrimSpace(value)), "\\", "/")
	if decoded == "" {
		return "", errors.New("a Markdown image has an empty path")
	}
	if isLocalAbsolutePath(decoded) {
		return "", fmt.Errorf("absolute image paths are not allowed: %s", decoded)
	}
	return decoded, nil
}

func isLocalAbsolutePath(value string) bool {
	lower := strings.ToLower(value)
	return strings.HasPrefix(value, "/") || strings.HasPrefix(lower, "file://") ||
		(len(value) >= 3 && unicode.IsLetter(rune(value[0])) && value[1] == ':' && (value[2] == '/' || value[2] == '\\'))
}

func cleanExternalURL(raw string) string {
	value := html.UnescapeString(strings.TrimSpace(raw))
	value = strings.TrimRight(value, ".,;:!?")
	for strings.HasSuffix(value, ")") && strings.Count(value, ")") > strings.Count(value, "(") {
		value = strings.TrimSuffix(value, ")")
	}
	value = strings.TrimRight(value, "]")
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return ""
	}
	return value
}

func decodeEntitiesInExternalURLs(line string) string {
	return externalURLPattern.ReplaceAllStringFunc(line, html.UnescapeString)
}

func normalizeIdentity(value string) string {
	value = norm.NFKC.String(strings.ToLower(value))
	var builder strings.Builder
	space := false
	for _, character := range value {
		if unicode.IsLetter(character) || unicode.IsNumber(character) {
			if space && builder.Len() > 0 {
				builder.WriteByte(' ')
			}
			builder.WriteRune(character)
			space = false
		} else {
			space = true
		}
	}
	return strings.TrimSpace(builder.String())
}

func isSameDocumentAnchor(destination string, documentID int) bool {
	if strings.HasPrefix(destination, "#") && len(destination) > 1 {
		return true
	}
	parsed, err := url.Parse(destination)
	if err != nil || parsed.Fragment == "" {
		return false
	}
	expected := fmt.Sprintf("/note/document/%d/", documentID)
	return parsed.Path == expected
}

func nodeText(node ast.Node, source []byte) string {
	var builder strings.Builder
	_ = ast.Walk(node, func(current ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering || current == node {
			return ast.WalkContinue, nil
		}
		switch typed := current.(type) {
		case *ast.Text:
			builder.Write(typed.Segment.Value(source))
		case *ast.CodeSpan:
			builder.Write(typed.Text(source))
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})
	return builder.String()
}

func linesValue(lines *text.Segments, source []byte) string {
	var builder strings.Builder
	for index := 0; index < lines.Len(); index++ {
		segment := lines.At(index)
		builder.Write(segment.Value(source))
	}
	return builder.String()
}

func rawHTMLValue(node *ast.RawHTML, source []byte) string {
	var builder strings.Builder
	for index := 0; index < node.Segments.Len(); index++ {
		segment := node.Segments.At(index)
		builder.Write(segment.Value(source))
	}
	return builder.String()
}

func stripHTMLTags(value string) string {
	var builder strings.Builder
	inTag := false
	for _, character := range value {
		switch character {
		case '<':
			inTag = true
			builder.WriteByte(' ')
		case '>':
			inTag = false
			builder.WriteByte(' ')
		default:
			if !inTag {
				builder.WriteRune(character)
			}
		}
	}
	return builder.String()
}

func valueOr(value *string, fallback string) string {
	if value == nil {
		return fallback
	}
	return *value
}

func normalizeLineEndings(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "\r\n", "\n"), "\r", "\n")
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func singleLine(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
