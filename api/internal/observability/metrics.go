package observability

import (
	"fmt"
	"io"
	"sort"
	"sync"
	"time"
)

var durationBuckets = []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}

type histogram struct {
	Count   uint64
	Sum     float64
	Buckets []uint64
}

type Metrics struct {
	mu           sync.Mutex
	requests     map[string]uint64
	errors       map[string]uint64
	durations    map[string]*histogram
	foundChunks  uint64
	insufficient uint64
	retries      uint64
	inputTokens  uint64
	outputTokens uint64
}

func NewMetrics() *Metrics {
	return &Metrics{requests: map[string]uint64{}, errors: map[string]uint64{}, durations: map[string]*histogram{}}
}

func (m *Metrics) Request(endpoint, status string) {
	m.mu.Lock()
	m.requests[endpoint+"\x00"+status]++
	m.mu.Unlock()
}
func (m *Metrics) Error(code string) { m.mu.Lock(); m.errors[code]++; m.mu.Unlock() }
func (m *Metrics) Found(count int)   { m.mu.Lock(); m.foundChunks += uint64(count); m.mu.Unlock() }
func (m *Metrics) Insufficient()     { m.mu.Lock(); m.insufficient++; m.mu.Unlock() }
func (m *Metrics) Retry()            { m.mu.Lock(); m.retries++; m.mu.Unlock() }
func (m *Metrics) Tokens(input, output *int) {
	m.mu.Lock()
	if input != nil && *input > 0 {
		m.inputTokens += uint64(*input)
	}
	if output != nil && *output > 0 {
		m.outputTokens += uint64(*output)
	}
	m.mu.Unlock()
}
func (m *Metrics) Observe(service string, duration time.Duration, _ error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	h := m.durations[service]
	if h == nil {
		h = &histogram{Buckets: make([]uint64, len(durationBuckets))}
		m.durations[service] = h
	}
	seconds := duration.Seconds()
	h.Count++
	h.Sum += seconds
	for i, b := range durationBuckets {
		if seconds <= b {
			h.Buckets[i]++
		}
	}
}

func (m *Metrics) WritePrometheus(w io.Writer) {
	m.mu.Lock()
	defer m.mu.Unlock()
	fmt.Fprintln(w, "# HELP api_requests_total HTTP requests by endpoint and status.")
	fmt.Fprintln(w, "# TYPE api_requests_total counter")
	for _, key := range sortedKeys(m.requests) {
		endpoint, status := splitKey(key)
		fmt.Fprintf(w, "api_requests_total{endpoint=%q,status=%q} %d\n", endpoint, status, m.requests[key])
	}
	fmt.Fprintln(w, "# HELP api_errors_total Errors by stable API code.")
	fmt.Fprintln(w, "# TYPE api_errors_total counter")
	for _, code := range sortedKeys(m.errors) {
		fmt.Fprintf(w, "api_errors_total{code=%q} %d\n", code, m.errors[code])
	}
	fmt.Fprintln(w, "# HELP api_dependency_duration_seconds Downstream request latency.")
	fmt.Fprintln(w, "# TYPE api_dependency_duration_seconds histogram")
	for _, service := range sortedKeys(m.durations) {
		h := m.durations[service]
		for i, b := range durationBuckets {
			fmt.Fprintf(w, "api_dependency_duration_seconds_bucket{service=%q,le=%q} %d\n", service, fmt.Sprint(b), h.Buckets[i])
		}
		fmt.Fprintf(w, "api_dependency_duration_seconds_bucket{service=%q,le=\"+Inf\"} %d\n", service, h.Count)
		fmt.Fprintf(w, "api_dependency_duration_seconds_sum{service=%q} %g\n", service, h.Sum)
		fmt.Fprintf(w, "api_dependency_duration_seconds_count{service=%q} %d\n", service, h.Count)
	}
	fmt.Fprintf(w, "# TYPE api_retrieved_chunks_total counter\napi_retrieved_chunks_total %d\n", m.foundChunks)
	fmt.Fprintf(w, "# TYPE api_insufficient_context_total counter\napi_insufficient_context_total %d\n", m.insufficient)
	fmt.Fprintf(w, "# TYPE api_deepseek_retries_total counter\napi_deepseek_retries_total %d\n", m.retries)
	fmt.Fprintf(w, "# TYPE api_deepseek_input_tokens_total counter\napi_deepseek_input_tokens_total %d\n", m.inputTokens)
	fmt.Fprintf(w, "# TYPE api_deepseek_output_tokens_total counter\napi_deepseek_output_tokens_total %d\n", m.outputTokens)
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
func splitKey(value string) (string, string) {
	for i := range value {
		if value[i] == 0 {
			return value[:i], value[i+1:]
		}
	}
	return value, ""
}
