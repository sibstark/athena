package snapshot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

const defaultMaxAssetBytes int64 = 100 << 20

type assetKind string

const (
	assetImage assetKind = "image"
	assetFile  assetKind = "file"
)

type assetReference struct {
	literal  string
	absolute string
	kind     assetKind
}

type assetFuture struct {
	done   chan struct{}
	record AssetRecord
}

type assetManager struct {
	root       string
	portal     string
	httpClient *http.Client
	redact     func(string) string
	skip       bool
	maxBytes   int64
	semaphore  chan struct{}
	mu         sync.Mutex
	futures    map[string]*assetFuture
	records    map[string]AssetRecord
}

type assetManagerOptions struct {
	Root        string
	Portal      string
	HTTPClient  *http.Client
	Redact      func(string) string
	Skip        bool
	MaxBytes    int64
	Concurrency int
}

var htmlAssetPattern = regexp.MustCompile(`(?i)<(img|a)\b[^>]*?\b(?:src|href)=["']([^"']+)["'][^>]*>`)

var fileExtensions = map[string]struct{}{
	".7z": {}, ".avi": {}, ".csv": {}, ".doc": {}, ".docx": {}, ".gif": {},
	".jpeg": {}, ".jpg": {}, ".json": {}, ".mov": {}, ".mp3": {}, ".mp4": {},
	".ods": {}, ".odt": {}, ".pdf": {}, ".png": {}, ".ppt": {}, ".pptx": {},
	".rar": {}, ".svg": {}, ".tar": {}, ".txt": {}, ".webm": {}, ".webp": {},
	".xls": {}, ".xlsx": {}, ".xml": {}, ".zip": {},
}

func newAssetManager(options assetManagerOptions) *assetManager {
	client := options.HTTPClient
	if client == nil {
		client = newSafeHTTPClient()
	}
	redact := options.Redact
	if redact == nil {
		redact = func(value string) string { return value }
	}
	maxBytes := options.MaxBytes
	if maxBytes == 0 {
		maxBytes = defaultMaxAssetBytes
	}
	concurrency := options.Concurrency
	if concurrency == 0 {
		concurrency = 4
	}
	return &assetManager{
		root: options.Root, portal: options.Portal, httpClient: client, redact: redact,
		skip: options.Skip, maxBytes: maxBytes, semaphore: make(chan struct{}, concurrency),
		futures: make(map[string]*assetFuture), records: make(map[string]AssetRecord),
	}
}

func (manager *assetManager) processMarkdown(ctx context.Context, workspaceID int, markdown, markdownPath string) string {
	if manager.skip || markdown == "" {
		return markdown
	}
	references := collectAssetReferences(markdown, manager.portal)
	type replacement struct{ from, to string }
	replacements := make(chan replacement, len(references))
	var wait sync.WaitGroup
	for _, reference := range references {
		reference := reference
		wait.Add(1)
		go func() {
			defer wait.Done()
			record := manager.download(ctx, workspaceID, reference)
			if record.Status != "downloaded" || record.LocalPath == "" {
				return
			}
			assetPath := filepath.Join(manager.root, filepath.FromSlash(record.LocalPath))
			relative, err := filepath.Rel(filepath.Dir(markdownPath), assetPath)
			if err != nil {
				return
			}
			relative = filepath.ToSlash(relative)
			if !strings.HasPrefix(relative, ".") {
				relative = "./" + relative
			}
			replacements <- replacement{from: reference.literal, to: relative}
		}()
	}
	wait.Wait()
	close(replacements)
	items := make([]replacement, 0, len(references))
	for replacement := range replacements {
		items = append(items, replacement)
	}
	sort.Slice(items, func(i, j int) bool { return len(items[i].from) > len(items[j].from) })
	result := markdown
	for _, replacement := range items {
		result = strings.ReplaceAll(result, replacement.from, replacement.to)
	}
	return result
}

func (manager *assetManager) recordsList() []AssetRecord {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	result := make([]AssetRecord, 0, len(manager.records))
	for _, record := range manager.records {
		result = append(result, record)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].WorkspaceID != result[j].WorkspaceID {
			return result[i].WorkspaceID < result[j].WorkspaceID
		}
		return result[i].SourceURL < result[j].SourceURL
	})
	return result
}

func (manager *assetManager) download(ctx context.Context, workspaceID int, reference assetReference) AssetRecord {
	key := fmt.Sprintf("%d:%s", workspaceID, reference.absolute)
	manager.mu.Lock()
	if existing := manager.futures[key]; existing != nil {
		manager.mu.Unlock()
		select {
		case <-ctx.Done():
			return AssetRecord{WorkspaceID: workspaceID, SourceURL: manager.redact(reference.absolute), Status: "failed", Error: manager.redact(ctx.Err().Error())}
		case <-existing.done:
			return existing.record
		}
	}
	future := &assetFuture{done: make(chan struct{})}
	manager.futures[key] = future
	manager.mu.Unlock()

	select {
	case manager.semaphore <- struct{}{}:
		future.record = manager.downloadNow(ctx, workspaceID, reference)
		<-manager.semaphore
	case <-ctx.Done():
		future.record = AssetRecord{WorkspaceID: workspaceID, SourceURL: manager.redact(reference.absolute), Status: "failed", Error: manager.redact(ctx.Err().Error())}
	}

	manager.mu.Lock()
	manager.records[key] = future.record
	close(future.done)
	manager.mu.Unlock()
	return future.record
}

func (manager *assetManager) downloadNow(ctx context.Context, workspaceID int, reference assetReference) AssetRecord {
	record := AssetRecord{WorkspaceID: workspaceID, SourceURL: manager.redact(reference.absolute), Status: "failed"}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reference.absolute, nil)
	if err != nil {
		record.Error = manager.redact(err.Error())
		return record
	}
	req.Header.Set("Accept", "*/*")
	resp, err := manager.httpClient.Do(req)
	if err != nil {
		record.Error = manager.redact(err.Error())
		return record
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		record.Error = fmt.Sprintf("HTTP %d %s", resp.StatusCode, resp.Status)
		return record
	}
	if resp.ContentLength > manager.maxBytes {
		record.Error = fmt.Sprintf("file is larger than %d bytes", manager.maxBytes)
		return record
	}
	contentType := strings.TrimSpace(strings.Split(resp.Header.Get("Content-Type"), ";")[0])
	if reference.kind == assetImage && contentType != "" && !strings.HasPrefix(contentType, "image/") {
		record.Error = "expected an image, received " + contentType
		return record
	}
	if reference.kind == assetFile && contentType == "text/html" {
		record.Error = "received an HTML page instead of a file"
		return record
	}

	fileName := chooseAssetFileName(reference.absolute, resp.Header, contentType)
	hash := sha256.Sum256([]byte(reference.absolute))
	relativePath := filepath.ToSlash(filepath.Join(fmt.Sprintf("workspace_%d", workspaceID), "assets", hex.EncodeToString(hash[:])[:12]+"-"+fileName))
	absolutePath := filepath.Join(manager.root, filepath.FromSlash(relativePath))
	if err := ensureWithin(manager.root, absolutePath); err != nil {
		record.Error = err.Error()
		return record
	}
	if err := os.MkdirAll(filepath.Dir(absolutePath), 0o755); err != nil {
		record.Error = manager.redact(err.Error())
		return record
	}
	temporary, err := os.CreateTemp(filepath.Dir(absolutePath), ".asset.tmp-")
	if err != nil {
		record.Error = manager.redact(err.Error())
		return record
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	written, copyErr := io.Copy(temporary, io.LimitReader(resp.Body, manager.maxBytes+1))
	closeErr := temporary.Close()
	if copyErr != nil {
		record.Error = manager.redact(copyErr.Error())
		return record
	}
	if closeErr != nil {
		record.Error = manager.redact(closeErr.Error())
		return record
	}
	if written > manager.maxBytes {
		record.Error = fmt.Sprintf("file is larger than %d bytes", manager.maxBytes)
		return record
	}
	if err := os.Rename(temporaryPath, absolutePath); err != nil {
		record.Error = manager.redact(err.Error())
		return record
	}
	record.Status = "downloaded"
	record.LocalPath = relativePath
	record.ContentType = contentType
	record.Bytes = written
	record.Error = ""
	return record
}

func collectAssetReferences(markdown, portal string) []assetReference {
	candidates := make(map[string]assetReference)
	source := []byte(markdown)
	document := goldmark.DefaultParser().Parse(text.NewReader(source))
	_ = ast.Walk(document, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch typed := node.(type) {
		case *ast.Image:
			addAssetReference(candidates, string(typed.Destination), assetImage, portal)
		case *ast.Link:
			addAssetReference(candidates, string(typed.Destination), assetFile, portal)
		}
		return ast.WalkContinue, nil
	})
	for _, match := range htmlAssetPattern.FindAllStringSubmatch(markdown, -1) {
		kind := assetFile
		if strings.EqualFold(match[1], "img") {
			kind = assetImage
		}
		addAssetReference(candidates, match[2], kind, portal)
	}
	result := make([]assetReference, 0, len(candidates))
	for _, reference := range candidates {
		result = append(result, reference)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].absolute < result[j].absolute })
	return result
}

func addAssetReference(target map[string]assetReference, literal string, kind assetKind, portal string) {
	literal = strings.TrimSpace(strings.ReplaceAll(literal, "&amp;", "&"))
	if literal == "" || strings.HasPrefix(literal, "#") {
		return
	}
	lower := strings.ToLower(literal)
	for _, prefix := range []string{"data:", "mailto:", "tel:", "javascript:"} {
		if strings.HasPrefix(lower, prefix) {
			return
		}
	}
	base, err := url.Parse(portal)
	if err != nil {
		return
	}
	parsed, err := url.Parse(literal)
	if err != nil {
		return
	}
	absolute := base.ResolveReference(parsed)
	if absolute.Scheme != "http" && absolute.Scheme != "https" {
		return
	}
	if kind == assetFile && !isLikelyFile(absolute) {
		return
	}
	target[string(kind)+":"+absolute.String()] = assetReference{literal: literal, absolute: absolute.String(), kind: kind}
}

func isLikelyFile(value *url.URL) bool {
	if _, ok := fileExtensions[strings.ToLower(filepath.Ext(value.Path))]; ok {
		return true
	}
	path := strings.ToLower(value.Path)
	return strings.Contains(path, "/download/") || strings.Contains(path, "/showfile/") ||
		strings.Contains(path, "/file/") || strings.Contains(path, "/disk/") ||
		strings.Contains(path, "/uf/") || value.Query().Has("download")
}

func chooseAssetFileName(rawURL string, headers http.Header, contentType string) string {
	name := ""
	if _, params, err := mime.ParseMediaType(headers.Get("Content-Disposition")); err == nil {
		name = params["filename"]
	}
	if name == "" {
		if parsed, err := url.Parse(rawURL); err == nil {
			name = filepath.Base(parsed.Path)
		}
	}
	name = sanitizeFileName(name)
	if filepath.Ext(name) == "" {
		name += extensionForContentType(contentType)
	}
	return name
}

func sanitizeFileName(value string) string {
	value = strings.TrimSpace(value)
	var builder strings.Builder
	lastDash := false
	for _, r := range value {
		invalid := unicode.IsControl(r) || strings.ContainsRune(`<>:"/\|?*`, r) || unicode.IsSpace(r)
		if invalid {
			if !lastDash && builder.Len() > 0 {
				builder.WriteByte('-')
				lastDash = true
			}
			continue
		}
		builder.WriteRune(r)
		lastDash = false
		if builder.Len() >= 120 {
			break
		}
	}
	result := strings.Trim(builder.String(), "-. ")
	if result == "" {
		return "asset"
	}
	return result
}

func extensionForContentType(contentType string) string {
	switch contentType {
	case "application/json":
		return ".json"
	case "application/pdf":
		return ".pdf"
	case "application/zip":
		return ".zip"
	case "image/gif":
		return ".gif"
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/svg+xml":
		return ".svg"
	case "image/webp":
		return ".webp"
	case "text/plain":
		return ".txt"
	default:
		return ""
	}
}

func newSafeHTTPClient() *http.Client {
	dialer := &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, err
			}
			if len(addresses) == 0 {
				return nil, errors.New("asset host resolved to no addresses")
			}
			for _, address := range addresses {
				if !isPublicIP(address.IP) {
					return nil, fmt.Errorf("asset host resolves to a non-public address: %s", address.IP)
				}
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(addresses[0].IP.String(), port))
		},
		ForceAttemptHTTP2:     true,
		ResponseHeaderTimeout: 30 * time.Second,
		IdleConnTimeout:       60 * time.Second,
	}
	return &http.Client{
		Transport: transport,
		Timeout:   45 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("too many asset redirects")
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return errors.New("asset redirect uses an unsupported scheme")
			}
			return nil
		},
	}
}

func isPublicIP(ip net.IP) bool {
	if ip == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return false
	}
	_, carrierNAT, _ := net.ParseCIDR("100.64.0.0/10")
	return !carrierNAT.Contains(ip)
}
