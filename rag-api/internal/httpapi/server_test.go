package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	athenaauth "github.com/sibstark/athena/rag-api/internal/auth"
	"github.com/sibstark/athena/rag-api/internal/config"
	"github.com/sibstark/athena/rag-api/internal/models"
	"github.com/sibstark/athena/rag-api/internal/observability"
	"golang.org/x/crypto/bcrypt"
)

type fakeRetriever struct {
	results []models.SearchResult
	err     error
	calls   atomic.Int32
	wait    bool
}

func (f *fakeRetriever) Search(ctx context.Context, _ models.SearchOptions) ([]models.SearchResult, time.Duration, error) {
	f.calls.Add(1)
	if f.wait {
		<-ctx.Done()
		return nil, 0, ctx.Err()
	}
	return f.results, time.Millisecond, f.err
}
func (f *fakeRetriever) Collection() string { return "methodology_e5_small_v1" }

type fakeGenerator struct {
	results []models.LLMResult
	err     error
	calls   atomic.Int32
	prompts []string
}

func (f *fakeGenerator) Generate(_ context.Context, _ string, user string) (models.LLMResult, error) {
	index := int(f.calls.Add(1)) - 1
	f.prompts = append(f.prompts, user)
	if f.err != nil {
		return models.LLMResult{}, f.err
	}
	if index >= len(f.results) {
		return models.LLMResult{}, f.err
	}
	return f.results[index], nil
}
func (f *fakeGenerator) Model() string { return "deepseek-v4-flash" }

type readyStub struct{ err error }

func (r readyStub) Ready(context.Context) error { return r.err }

func TestSearchRequiresAuthAndNeverCallsGenerator(t *testing.T) {
	retriever := &fakeRetriever{results: sampleResults()}
	generator := &fakeGenerator{}
	handler := testServer(retriever, generator).Handler()
	unauthorized := perform(handler, "/v1/search", `{"query":"q"}`, "")
	if unauthorized.Code != 401 {
		t.Fatalf("status=%d", unauthorized.Code)
	}
	response := perform(handler, "/v1/search", `{"query":"q","filters":{"root_page_id":"3035"}}`, "key")
	if response.Code != 200 {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if generator.calls.Load() != 0 {
		t.Fatal("search called generator")
	}
}

func TestHealthMetricsAndAuthEndpointsDoNotRequireBearer(t *testing.T) {
	handler := testServer(&fakeRetriever{}, &fakeGenerator{}).Handler()
	for _, path := range []string{"/health/live", "/health/ready", "/metrics"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", path, response.Code, response.Body.String())
		}
	}

	sessionRequest := httptest.NewRequest(http.MethodGet, "/v1/auth/session", nil)
	sessionResponse := httptest.NewRecorder()
	handler.ServeHTTP(sessionResponse, sessionRequest)
	if sessionResponse.Code != http.StatusUnauthorized || strings.Contains(sessionResponse.Body.String(), "Bearer") {
		t.Fatalf("session status=%d body=%s", sessionResponse.Code, sessionResponse.Body.String())
	}
}

func TestLoginSuccessAndCookieFlags(t *testing.T) {
	server := testServer(&fakeRetriever{}, &fakeGenerator{})
	response := loginRequest(server.Handler(), "admin", "password", "192.0.2.10:1234")
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"authenticated":true`) || !strings.Contains(response.Body.String(), `"username":"admin"`) {
		t.Fatalf("body=%s", response.Body.String())
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies=%v", cookies)
	}
	cookie := cookies[0]
	if cookie.Name != sessionCookieName || cookie.Value == "" || !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" || cookie.MaxAge != int((24*time.Hour)/time.Second) {
		t.Fatalf("cookie=%+v", cookie)
	}

	sessionRequest := httptest.NewRequest(http.MethodGet, "/v1/auth/session", nil)
	sessionRequest.AddCookie(cookie)
	sessionResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(sessionResponse, sessionRequest)
	if sessionResponse.Code != http.StatusOK || !strings.Contains(sessionResponse.Body.String(), `"username":"admin"`) {
		t.Fatalf("session status=%d body=%s", sessionResponse.Code, sessionResponse.Body.String())
	}
}

func TestInvalidUsernameAndPasswordReturnSameError(t *testing.T) {
	server := testServer(&fakeRetriever{}, &fakeGenerator{})
	wrongUsername := loginRequest(server.Handler(), "other", "password", "192.0.2.11:1234")
	wrongPassword := loginRequest(server.Handler(), "admin", "wrong", "192.0.2.12:1234")
	if wrongUsername.Code != http.StatusUnauthorized || wrongPassword.Code != http.StatusUnauthorized {
		t.Fatalf("statuses=%d,%d", wrongUsername.Code, wrongPassword.Code)
	}
	for _, response := range []*httptest.ResponseRecorder{wrongUsername, wrongPassword} {
		var payload models.APIError
		if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.Error.Code != "invalid_credentials" || payload.Error.Message != "Неверный логин или пароль" {
			t.Fatalf("error=%+v", payload.Error)
		}
	}
}

func TestAnswerAcceptsSessionCookieAndRejectsTampering(t *testing.T) {
	server := testServer(&fakeRetriever{}, &fakeGenerator{})
	login := loginRequest(server.Handler(), "admin", "password", "192.0.2.13:1234")
	cookie := login.Result().Cookies()[0]

	authorized := httptest.NewRequest(http.MethodPost, "/v1/answer", strings.NewReader(`{"query":"q"}`))
	authorized.AddCookie(cookie)
	authorizedResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(authorizedResponse, authorized)
	if authorizedResponse.Code != http.StatusOK {
		t.Fatalf("authorized status=%d body=%s", authorizedResponse.Code, authorizedResponse.Body.String())
	}

	cookie.Value = "A" + cookie.Value[1:]
	tampered := httptest.NewRequest(http.MethodPost, "/v1/answer", strings.NewReader(`{"query":"q"}`))
	tampered.AddCookie(cookie)
	tamperedResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(tamperedResponse, tampered)
	if tamperedResponse.Code != http.StatusUnauthorized {
		t.Fatalf("tampered status=%d", tamperedResponse.Code)
	}
}

func TestAnswerRejectsExpiredSessionCookie(t *testing.T) {
	server := testServer(&fakeRetriever{}, &fakeGenerator{})
	current := time.Unix(1_700_000_000, 0)
	server.auth = athenaauth.New(athenaauth.Config{
		Username:      server.cfg.AuthUsername,
		PasswordHash:  server.cfg.AuthPasswordHash,
		SessionSecret: server.cfg.AuthSessionSecret,
		SessionTTL:    server.cfg.AuthSessionTTL,
	}, func() time.Time { return current })
	token, _, err := server.auth.Login("admin", "password", "192.0.2.15")
	if err != nil {
		t.Fatal(err)
	}
	current = current.Add(25 * time.Hour)

	request := httptest.NewRequest(http.MethodPost, "/v1/answer", strings.NewReader(`{"query":"q"}`))
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestLogoutClearsCookie(t *testing.T) {
	server := testServer(&fakeRetriever{}, &fakeGenerator{})
	request := httptest.NewRequest(http.MethodPost, "/v1/auth/logout", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d", response.Code)
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != sessionCookieName || cookies[0].Value != "" || cookies[0].MaxAge != -1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookies=%+v", cookies)
	}
}

func TestLoginRateLimit(t *testing.T) {
	server := testServer(&fakeRetriever{}, &fakeGenerator{})
	for attempt := 0; attempt < 5; attempt++ {
		response := loginRequest(server.Handler(), "admin", "wrong", "192.0.2.14:1234")
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d status=%d", attempt, response.Code)
		}
	}
	limited := loginRequest(server.Handler(), "admin", "password", "192.0.2.14:1234")
	if limited.Code != http.StatusTooManyRequests || !strings.Contains(limited.Body.String(), "too_many_login_attempts") {
		t.Fatalf("status=%d body=%s", limited.Code, limited.Body.String())
	}
}

func TestAnswerSkipsGeneratorWithoutContext(t *testing.T) {
	generator := &fakeGenerator{}
	response := perform(testServer(&fakeRetriever{}, generator).Handler(), "/v1/answer", `{"query":"q"}`, "key")
	if response.Code != 200 {
		t.Fatalf("status=%d", response.Code)
	}
	if generator.calls.Load() != 0 {
		t.Fatal("generator called")
	}
	if !strings.Contains(response.Body.String(), "insufficient_context") {
		t.Fatalf("body=%s", response.Body.String())
	}
}

func TestAnswerReturnsOnlyMentionedQdrantSources(t *testing.T) {
	generator := &fakeGenerator{results: []models.LLMResult{{Content: "Шаг из первого источника [S1]."}}}
	response := perform(testServer(&fakeRetriever{results: sampleResults()}, generator).Handler(), "/v1/answer", `{"query":"Как провести урок?"}`, "key")
	if response.Code != 200 {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var result models.AnswerResponse
	json.NewDecoder(response.Body).Decode(&result)
	if len(result.Sources) != 1 || result.Sources[0].ChunkID != "chunk_1" {
		t.Fatalf("sources=%+v", result.Sources)
	}
	if strings.Contains(generator.prompts[0], "Второй") == false {
		t.Fatal("selected context missing")
	}
}

func TestAnswerRepairsInvalidCitationOnce(t *testing.T) {
	generator := &fakeGenerator{results: []models.LLMResult{{Content: "Ответ [S99]."}, {Content: "Исправленный ответ [S2]."}}}
	response := perform(testServer(&fakeRetriever{results: sampleResults()}, generator).Handler(), "/v1/answer", `{"query":"q"}`, "key")
	if response.Code != 200 || generator.calls.Load() != 2 {
		t.Fatalf("status=%d calls=%d body=%s", response.Code, generator.calls.Load(), response.Body.String())
	}
	if strings.Contains(response.Body.String(), "S99") {
		t.Fatal("invented source returned")
	}
}

func TestAnswerFailsAfterOneBadRepair(t *testing.T) {
	generator := &fakeGenerator{results: []models.LLMResult{{Content: "Без цитат"}, {Content: "Снова [S42]"}}}
	response := perform(testServer(&fakeRetriever{results: sampleResults()}, generator).Handler(), "/v1/answer", `{"query":"q"}`, "key")
	if response.Code != 502 || !strings.Contains(response.Body.String(), "generation_validation_failed") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestUnknownMetadataFilterIsRejected(t *testing.T) {
	response := perform(testServer(&fakeRetriever{}, &fakeGenerator{}).Handler(), "/v1/search", `{"query":"q","filters":{"metadata":{"page_id":"1"}}}`, "key")
	if response.Code != 400 {
		t.Fatalf("status=%d", response.Code)
	}
}

func TestClientCancellationReachesRetriever(t *testing.T) {
	retriever := &fakeRetriever{wait: true}
	handler := testServer(retriever, &fakeGenerator{}).Handler()
	ctx, cancel := context.WithCancel(context.Background())
	request := httptest.NewRequest(http.MethodPost, "/v1/search", bytes.NewBufferString(`{"query":"q"}`)).WithContext(ctx)
	request.Header.Set("Authorization", "Bearer key")
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { handler.ServeHTTP(response, request); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("request did not cancel")
	}
}

func testServer(retriever *fakeRetriever, generator *fakeGenerator) *Server {
	hash, err := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.MinCost)
	if err != nil {
		panic(err)
	}
	cfg := config.Config{APIKey: "key", AuthUsername: "admin", AuthPasswordHash: string(hash), AuthSessionSecret: strings.Repeat("s", 32), AuthSessionTTL: 24 * time.Hour, AuthCookieSecure: true, HTTPMaxBodyBytes: 1 << 20, DefaultTopK: 8, MaxTopK: 20, ScoreThreshold: .65, MaxQueryChars: 4000, MaxContextChars: 30000, MaxContextChunks: 8, TEITimeout: time.Second, QdrantTimeout: time.Second}
	return New(cfg, retriever, generator, readyStub{}, readyStub{}, observability.NewMetrics(), slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func loginRequest(handler http.Handler, username, password, remoteAddr string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	request := httptest.NewRequest(http.MethodPost, "/v1/auth/login", bytes.NewReader(body))
	request.RemoteAddr = remoteAddr
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
func perform(handler http.Handler, path, body, key string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	if key != "" {
		request.Header.Set("Authorization", "Bearer "+key)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
func sampleResults() []models.SearchResult {
	return []models.SearchResult{{SourceID: "S1", ChunkID: "chunk_1", DocumentID: "page_1", Score: .9, Content: "Первый", Title: "Документ 1", Breadcrumbs: []string{"Root"}}, {SourceID: "S2", ChunkID: "chunk_2", DocumentID: "page_2", Score: .8, Content: "Второй", Title: "Документ 2", Breadcrumbs: []string{"Root"}}}
}
