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
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	athenaauth "github.com/sibstark/athena/rag-api/internal/auth"
	"github.com/sibstark/athena/rag-api/internal/config"
	"github.com/sibstark/athena/rag-api/internal/contextbuilder"
	"github.com/sibstark/athena/rag-api/internal/models"
	"github.com/sibstark/athena/rag-api/internal/observability"
)

type Retriever interface {
	Search(context.Context, models.SearchOptions) ([]models.SearchResult, time.Duration, error)
}
type Generator interface {
	Generate(context.Context, string, string) (models.LLMResult, error)
	Model() string
}
type Readiness interface{ Ready(context.Context) error }
type Qdrant interface {
	Readiness
	ListCollections(context.Context) ([]string, error)
}

type Server struct {
	cfg       config.Config
	retriever Retriever
	generator Generator
	teiReady  Readiness
	qdrant    Qdrant
	builder   contextbuilder.Builder
	metrics   *observability.Metrics
	logger    *slog.Logger
	auth      *athenaauth.Manager
	handler   http.Handler
}

type requestIDKey struct{}

var citationPattern = regexp.MustCompile(`\[S([1-9][0-9]*)\]`)

const sessionCookieName = "athena_session"

func New(cfg config.Config, retriever Retriever, generator Generator, teiReady Readiness, qdrant Qdrant, metrics *observability.Metrics, logger *slog.Logger) *Server {
	authManager := athenaauth.New(athenaauth.Config{Username: cfg.AuthUsername, PasswordHash: cfg.AuthPasswordHash, SessionSecret: cfg.AuthSessionSecret, SessionTTL: cfg.AuthSessionTTL}, time.Now)
	s := &Server{cfg: cfg, retriever: retriever, generator: generator, teiReady: teiReady, qdrant: qdrant, builder: contextbuilder.Builder{MaxChars: cfg.MaxContextChars, MaxChunks: cfg.MaxContextChunks}, metrics: metrics, logger: logger, auth: authManager}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", s.live)
	mux.HandleFunc("GET /health/ready", s.ready)
	mux.HandleFunc("GET /metrics", s.prometheus)
	mux.HandleFunc("POST /v1/auth/login", s.login)
	mux.HandleFunc("GET /v1/auth/session", s.session)
	mux.HandleFunc("POST /v1/auth/logout", s.logout)
	mux.HandleFunc("GET /v1/collections", s.collections)
	mux.HandleFunc("POST /v1/search", s.search)
	mux.HandleFunc("POST /v1/answer", s.answer)
	s.handler = s.recovery(s.requestID(s.logging(s.cors(s.authorize(mux)))))
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
	if err := s.qdrant.Ready(ctx); err != nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "qdrant_unavailable", "Qdrant недоступен")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) collections(w http.ResponseWriter, r *http.Request) {
	collections, err := s.qdrant.ListCollections(r.Context())
	if err != nil {
		s.downstreamError(w, r, err)
		return
	}
	if collections == nil {
		collections = []string{}
	}
	writeJSON(w, http.StatusOK, models.CollectionsResponse{RequestID: requestID(r.Context()), Collections: collections})
}
func (s *Server) prometheus(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	s.metrics.WritePrometheus(w)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, s.cfg.HTTPMaxBodyBytes)
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decoder.Decode(&request); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "Некорректный JSON-запрос")
		return
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "В теле должен быть один JSON-объект")
		return
	}

	token, expires, err := s.auth.Login(request.Username, request.Password, clientIP(r))
	switch {
	case errors.Is(err, athenaauth.ErrRateLimited):
		s.writeError(w, r, http.StatusTooManyRequests, "too_many_login_attempts", "Слишком много попыток входа. Попробуйте позже")
		return
	case errors.Is(err, athenaauth.ErrInvalidCredentials):
		s.writeError(w, r, http.StatusUnauthorized, "invalid_credentials", "Неверный логин или пароль")
		return
	case err != nil:
		s.writeError(w, r, http.StatusInternalServerError, "internal_error", "Внутренняя ошибка")
		return
	}

	http.SetCookie(w, s.sessionCookie(token, expires, max(1, int(s.cfg.AuthSessionTTL/time.Second))))
	writeJSON(w, http.StatusOK, map[string]any{"authenticated": true, "username": s.cfg.AuthUsername})
}

func (s *Server) session(w http.ResponseWriter, r *http.Request) {
	username, ok := s.sessionUsername(r)
	if !ok {
		s.writeError(w, r, http.StatusUnauthorized, "unauthorized", "Требуется авторизация")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"authenticated": true, "username": username})
}

func (s *Server) logout(w http.ResponseWriter, _ *http.Request) {
	http.SetCookie(w, s.sessionCookie("", time.Unix(1, 0), -1))
	writeJSON(w, http.StatusOK, map[string]any{"authenticated": false})
}

func (s *Server) sessionCookie(value string, expires time.Time, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     sessionCookieName,
		Value:    value,
		Path:     "/",
		Expires:  expires.UTC(),
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   s.cfg.AuthCookieSecure,
		SameSite: http.SameSiteStrictMode,
	}
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
	writeJSON(w, http.StatusOK, models.SearchResponse{RequestID: requestID(r.Context()), Query: options.Query, Results: results, Retrieval: models.RetrievalInfo{Collections: append([]string(nil), options.Collections...), EmbeddingModel: s.cfg.Embedding.ModelID, Returned: len(results), DurationMS: duration.Milliseconds()}})
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
		writeJSON(w, http.StatusOK, models.AnswerResponse{RequestID: requestID(r.Context()), Status: "insufficient_context", Answer: "В переданных коллекциях не удалось найти достаточную информацию для ответа.", Sources: []models.AnswerSource{}})
		return
	}
	prompt, selected := s.builder.Build(options.Query, results)
	if len(selected) == 0 {
		s.metrics.Insufficient()
		writeJSON(w, http.StatusOK, models.AnswerResponse{RequestID: requestID(r.Context()), Status: "insufficient_context", Answer: "В переданных коллекциях не удалось найти достаточную информацию для ответа.", Sources: []models.AnswerSource{}})
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
	if len(request.Collections) == 0 {
		s.writeError(w, r, http.StatusBadRequest, "invalid_request", "Поле collections обязательно и должно содержать хотя бы одну коллекцию")
		return models.SearchOptions{}, false
	}
	collections := make([]string, 0, len(request.Collections))
	seenCollections := make(map[string]struct{}, len(request.Collections))
	for _, collection := range request.Collections {
		collection = strings.TrimSpace(collection)
		if collection == "" {
			s.writeError(w, r, http.StatusBadRequest, "invalid_request", "collections не должны содержать пустые значения")
			return models.SearchOptions{}, false
		}
		if _, exists := seenCollections[collection]; exists {
			continue
		}
		seenCollections[collection] = struct{}{}
		collections = append(collections, collection)
	}
	return models.SearchOptions{Query: query, Collections: collections, TopK: topK, ScoreThreshold: threshold, Filters: request.Filters}, true
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

func (s *Server) authorize(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isProtectedEndpoint(r) || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		if s.validBearer(r.Header.Get("Authorization")) {
			next.ServeHTTP(w, r)
			return
		}
		if _, ok := s.sessionUsername(r); !ok {
			s.writeError(w, r, http.StatusUnauthorized, "unauthorized", "Требуется авторизация")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) validBearer(authorization string) bool {
	if !strings.HasPrefix(authorization, "Bearer ") {
		return false
	}
	provided := strings.TrimSpace(strings.TrimPrefix(authorization, "Bearer "))
	expectedHash := sha256.Sum256([]byte(s.cfg.APIKey))
	providedHash := sha256.Sum256([]byte(provided))
	return subtle.ConstantTimeCompare(expectedHash[:], providedHash[:]) == 1
}

func (s *Server) sessionUsername(r *http.Request) (string, bool) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return "", false
	}
	return s.auth.Validate(cookie.Value)
}

func isProtectedEndpoint(r *http.Request) bool {
	return (r.Method == http.MethodPost && (r.URL.Path == "/v1/search" || r.URL.Path == "/v1/answer")) ||
		(r.Method == http.MethodGet && r.URL.Path == "/v1/collections")
}

func clientIP(r *http.Request) string {
	if value := strings.TrimSpace(r.Header.Get("X-Real-IP")); net.ParseIP(value) != nil {
		return value
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && net.ParseIP(host) != nil {
		return host
	}
	return r.RemoteAddr
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
			w.Header().Set("Access-Control-Allow-Credentials", "true")
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
	case "/v1/search", "/v1/answer", "/v1/auth/login", "/v1/auth/session", "/v1/auth/logout", "/health/live", "/health/ready", "/metrics":
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
