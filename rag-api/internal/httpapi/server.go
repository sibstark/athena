package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sibstark/athena/rag-api/internal/config"
	"github.com/sibstark/athena/rag-api/internal/contextbuilder"
	"github.com/sibstark/athena/rag-api/internal/models"
	"github.com/sibstark/athena/rag-api/internal/observability"
)

type Retriever interface {
	Search(context.Context, models.SearchOptions) ([]models.SearchResult, time.Duration, error)
	Collection() string
}
type Generator interface {
	Generate(context.Context, string, string) (models.LLMResult, error)
	Model() string
}
type Readiness interface{ Ready(context.Context) error }

type Server struct {
	cfg                   config.Config
	retriever             Retriever
	generator             Generator
	teiReady, qdrantReady Readiness
	builder               contextbuilder.Builder
	metrics               *observability.Metrics
	logger                *slog.Logger
	handler               http.Handler
}

type requestIDKey struct{}

var citationPattern = regexp.MustCompile(`\[S([1-9][0-9]*)\]`)

func New(cfg config.Config, retriever Retriever, generator Generator, teiReady, qdrantReady Readiness, metrics *observability.Metrics, logger *slog.Logger) *Server {
	s := &Server{cfg: cfg, retriever: retriever, generator: generator, teiReady: teiReady, qdrantReady: qdrantReady, builder: contextbuilder.Builder{MaxChars: cfg.MaxContextChars, MaxChunks: cfg.MaxContextChunks}, metrics: metrics, logger: logger}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", s.live)
	mux.HandleFunc("GET /health/ready", s.ready)
	mux.HandleFunc("GET /metrics", s.prometheus)
	mux.HandleFunc("POST /v1/search", s.search)
	mux.HandleFunc("POST /v1/answer", s.answer)
	s.handler = s.recovery(s.requestID(s.logging(s.cors(s.auth(mux)))))
	return s
}

func (s *Server) Handler() http.Handler { return s.handler }

func (s *Server) live(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), max(s.cfg.TEITimeout, s.cfg.QdrantTimeout))
	defer cancel()
	if err := s.teiReady.Ready(ctx); err != nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "tei_unavailable", "TEI недоступен")
		return
	}
	if err := s.qdrantReady.Ready(ctx); err != nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "qdrant_unavailable", "Qdrant недоступен")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}
func (s *Server) prometheus(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	s.metrics.WritePrometheus(w)
}

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	options, ok := s.decodeSearchRequest(w, r)
	if !ok {
		return
	}
	results, duration, err := s.retriever.Search(r.Context(), options)
	if err != nil {
		s.downstreamError(w, r, err)
		return
	}
	s.metrics.Found(len(results))
	writeJSON(w, http.StatusOK, models.SearchResponse{RequestID: requestID(r.Context()), Query: options.Query, Results: results, Retrieval: models.RetrievalInfo{Collection: s.retriever.Collection(), EmbeddingModel: models.EmbeddingModel, Returned: len(results), DurationMS: duration.Milliseconds()}})
}

func (s *Server) answer(w http.ResponseWriter, r *http.Request) {
	options, ok := s.decodeSearchRequest(w, r)
	if !ok {
		return
	}
	results, _, err := s.retriever.Search(r.Context(), options)
	if err != nil {
		s.downstreamError(w, r, err)
		return
	}
	s.metrics.Found(len(results))
	if len(results) == 0 {
		s.metrics.Insufficient()
		writeJSON(w, http.StatusOK, models.AnswerResponse{RequestID: requestID(r.Context()), Status: "insufficient_context", Answer: "В загруженных методических материалах не удалось найти достаточную информацию для ответа.", Sources: []models.AnswerSource{}})
		return
	}
	prompt, selected := s.builder.Build(options.Query, results)
	if len(selected) == 0 {
		s.metrics.Insufficient()
		writeJSON(w, http.StatusOK, models.AnswerResponse{RequestID: requestID(r.Context()), Status: "insufficient_context", Answer: "В загруженных методических материалах не удалось найти достаточную информацию для ответа.", Sources: []models.AnswerSource{}})
		return
	}
	result, err := s.generator.Generate(r.Context(), contextbuilder.SystemPrompt, prompt)
	if err != nil {
		s.downstreamError(w, r, err)
		return
	}
	totalDuration := result.Duration
	inputTokens, outputTokens := copyInt(result.InputTokens), copyInt(result.OutputTokens)
	valid, mentioned := validateCitations(result.Content, selected)
	if !valid {
		repaired, repairErr := s.generator.Generate(r.Context(), contextbuilder.SystemPrompt, contextbuilder.RepairPrompt(prompt, result.Content))
		if repairErr != nil {
			s.downstreamError(w, r, repairErr)
			return
		}
		totalDuration += repaired.Duration
		addTokens(&inputTokens, repaired.InputTokens)
		addTokens(&outputTokens, repaired.OutputTokens)
		result = repaired
		valid, mentioned = validateCitations(result.Content, selected)
	}
	if !valid {
		s.writeError(w, r, http.StatusBadGateway, "generation_validation_failed", "Ответ модели не прошёл проверку цитат")
		return
	}
	s.metrics.Tokens(inputTokens, outputTokens)
	sources := make([]models.AnswerSource, 0, len(mentioned))
	for _, source := range selected {
		if mentioned[source.SourceID] {
			sources = append(sources, source.SourceMetadata())
		}
	}
	writeJSON(w, http.StatusOK, models.AnswerResponse{RequestID: requestID(r.Context()), Status: "ok", Answer: result.Content, Sources: sources, Generation: &models.GenerationInfo{Provider: "deepseek", Model: s.generator.Model(), InputTokens: inputTokens, OutputTokens: outputTokens, DurationMS: totalDuration.Milliseconds()}})
}

func (s *Server) decodeSearchRequest(w http.ResponseWriter, r *http.Request) (models.SearchOptions, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, s.cfg.HTTPMaxBodyBytes)
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request models.SearchRequest
	if err := decoder.Decode(&request); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			s.writeError(w, r, http.StatusRequestEntityTooLarge, "invalid_request", "Слишком большое тело запроса")
			return models.SearchOptions{}, false
		}
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "Некорректный JSON-запрос")
		return models.SearchOptions{}, false
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "В теле должен быть один JSON-объект")
		return models.SearchOptions{}, false
	}
	query := strings.TrimSpace(request.Query)
	if query == "" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "Поле query обязательно")
		return models.SearchOptions{}, false
	}
	if utf8.RuneCountInString(query) > s.cfg.MaxQueryChars {
		s.writeError(w, r, http.StatusRequestEntityTooLarge, "query_too_long", "Слишком длинный запрос")
		return models.SearchOptions{}, false
	}
	topK := s.cfg.DefaultTopK
	if request.TopK != nil {
		topK = *request.TopK
	}
	if topK < 1 || topK > s.cfg.MaxTopK {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", fmt.Sprintf("top_k должен быть от 1 до %d", s.cfg.MaxTopK))
		return models.SearchOptions{}, false
	}
	threshold := s.cfg.ScoreThreshold
	if request.ScoreThreshold != nil {
		threshold = *request.ScoreThreshold
	}
	if threshold < -1 || threshold > 1 {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "score_threshold должен быть от -1 до 1")
		return models.SearchOptions{}, false
	}
	return models.SearchOptions{Query: query, TopK: topK, ScoreThreshold: threshold, Filters: request.Filters}, true
}

func (s *Server) downstreamError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, models.ErrInvalidEmbedding):
		s.writeError(w, r, http.StatusBadGateway, "invalid_embedding", "TEI вернул некорректный embedding")
	case errors.Is(err, models.ErrTEIUnavailable):
		s.writeError(w, r, http.StatusServiceUnavailable, "tei_unavailable", "TEI недоступен")
	case errors.Is(err, models.ErrQdrantUnavailable):
		s.writeError(w, r, http.StatusServiceUnavailable, "qdrant_unavailable", "Qdrant недоступен")
	case errors.Is(err, models.ErrLLMRateLimited):
		s.writeError(w, r, http.StatusTooManyRequests, "llm_rate_limited", "DeepSeek временно ограничил запросы")
	case errors.Is(err, models.ErrLLMUnavailable):
		s.writeError(w, r, http.StatusServiceUnavailable, "llm_unavailable", "Не удалось сформировать ответ")
	case errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded):
		return
	default:
		s.logger.Error("unhandled request error", "request_id", requestID(r.Context()), "error", err.Error())
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", "Внутренняя ошибка")
	}
}

func (s *Server) writeError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	s.metrics.Error(code)
	writeJSON(w, status, models.APIError{RequestID: requestID(r.Context()), Error: models.APIErrorBody{Code: code, Message: message}})
}

func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/v1/") || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		authorization := r.Header.Get("Authorization")
		if !strings.HasPrefix(authorization, "Bearer ") {
			s.writeError(w, r, http.StatusUnauthorized, "unauthorized", "Требуется корректный Bearer token")
			return
		}
		provided := strings.TrimSpace(strings.TrimPrefix(authorization, "Bearer "))
		expectedHash := sha256.Sum256([]byte(s.cfg.APIKey))
		providedHash := sha256.Sum256([]byte(provided))
		if subtle.ConstantTimeCompare(expectedHash[:], providedHash[:]) != 1 {
			s.writeError(w, r, http.StatusUnauthorized, "unauthorized", "Требуется корректный Bearer token")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) cors(next http.Handler) http.Handler {
	allowed := map[string]bool{}
	for _, origin := range s.cfg.CORSOrigins {
		allowed[origin] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && allowed[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		}
		if r.Method == http.MethodOptions {
			if origin == "" || !allowed[origin] {
				http.Error(w, "origin not allowed", http.StatusForbidden)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := newRequestID()
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id)))
	})
}
func (s *Server) logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: 200}
		next.ServeHTTP(rec, r)
		endpoint := endpointName(r.URL.Path)
		s.metrics.Request(endpoint, strconv.Itoa(rec.status))
		s.logger.Info("http_request", "request_id", requestID(r.Context()), "method", r.Method, "path", endpoint, "status", rec.status, "duration_ms", time.Since(start).Milliseconds())
	})
}
func (s *Server) recovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				s.logger.Error("panic recovered", "request_id", requestID(r.Context()), "panic", fmt.Sprint(recovered))
				s.writeError(w, r, http.StatusInternalServerError, "internal_error", "Внутренняя ошибка")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(status int) {
	if r.wroteHeader {
		return
	}
	r.wroteHeader = true
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func requestID(ctx context.Context) string {
	value, _ := ctx.Value(requestIDKey{}).(string)
	return value
}
func newRequestID() string {
	data := make([]byte, 16)
	if _, err := rand.Read(data); err != nil {
		return hex.EncodeToString([]byte(strconv.FormatInt(time.Now().UnixNano(), 10)))
	}
	data[6] = (data[6] & 0x0f) | 0x40
	data[8] = (data[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", data[0:4], data[4:6], data[6:8], data[8:10], data[10:16])
}
func endpointName(path string) string {
	switch path {
	case "/v1/search", "/v1/answer", "/health/live", "/health/ready", "/metrics":
		return path
	default:
		return "other"
	}
}
func validateCitations(answer string, sources []models.SearchResult) (bool, map[string]bool) {
	allowed := map[string]bool{}
	for _, source := range sources {
		allowed[source.SourceID] = true
	}
	matches := citationPattern.FindAllStringSubmatch(answer, -1)
	mentioned := map[string]bool{}
	if len(matches) == 0 {
		return false, mentioned
	}
	for _, match := range matches {
		id := "S" + match[1]
		if !allowed[id] {
			return false, mentioned
		}
		mentioned[id] = true
	}
	return len(mentioned) > 0, mentioned
}
func copyInt(value *int) *int {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
func addTokens(total **int, value *int) {
	if value == nil {
		return
	}
	if *total == nil {
		copy := *value
		*total = &copy
	} else {
		**total += *value
	}
}
