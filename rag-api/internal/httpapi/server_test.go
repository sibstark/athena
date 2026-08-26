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
	options []models.SearchOptions
}

func (f *fakeRetriever) Search(ctx context.Context, options models.SearchOptions) ([]models.SearchResult, time.Duration, error) {
	f.calls.Add(1)
	f.options = append(f.options, options)
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

type qdrantStub struct {
	readyErr    error
	listErr     error
	collections []string
	listCalls   atomic.Int32
}

func (q *qdrantStub) Ready(context.Context) error { return q.readyErr }
func (q *qdrantStub) ListCollections(context.Context) ([]string, error) {
	q.listCalls.Add(1)
	return append([]string(nil), q.collections...), q.listErr
}

func TestSearchRequiresAuthAndNeverCallsGenerator(t *testing.T) {
	retriever := &fakeRetriever{results: sampleResults()}
	generator := &fakeGenerator{}
	handler := testServer(retriever, generator).Handler()
	unauthorized := perform(handler, "/v1/search", `{"query":"q"}`, "")
	if unauthorized.Code != 401 {
		t.Fatalf("status=%d", unauthorized.Code)
	}
	response := perform(handler, "/v1/search", `{"query":"q","collections":["workspace_17"],"filters":{"root_page_id":"3035"}}`, "key")
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

func TestCollectionsRequiresAuthAndReturnsNamesWithoutUsingSearchOrGenerator(t *testing.T) {
	retriever := &fakeRetriever{}
	generator := &fakeGenerator{}
	server := testServer(retriever, generator)
	qdrant := &qdrantStub{collections: []string{"workspace_1", "workspace_7"}}
	server.qdrant = qdrant

	unauthorized := httptest.NewRequest(http.MethodGet, "/v1/collections", nil)
	unauthorizedResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(unauthorizedResponse, unauthorized)
	if unauthorizedResponse.Code != http.StatusUnauthorized || qdrant.listCalls.Load() != 0 {
		t.Fatalf("status=%d calls=%d", unauthorizedResponse.Code, qdrant.listCalls.Load())
	}

	authorized := httptest.NewRequest(http.MethodGet, "/v1/collections", nil)
	authorized.Header.Set("Authorization", "Bearer key")
	authorizedResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(authorizedResponse, authorized)
	if authorizedResponse.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", authorizedResponse.Code, authorizedResponse.Body.String())
	}
	var response models.CollectionsResponse
	if err := json.NewDecoder(authorizedResponse.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.RequestID == "" || len(response.Collections) != 2 || response.Collections[0] != "workspace_1" || response.Collections[1] != "workspace_7" {
		t.Fatalf("response=%+v", response)
	}
	if retriever.calls.Load() != 0 || generator.calls.Load() != 0 || qdrant.listCalls.Load() != 1 {
		t.Fatalf("retriever=%d generator=%d qdrant=%d", retriever.calls.Load(), generator.calls.Load(), qdrant.listCalls.Load())
	}
}

func TestCollectionsAcceptsSessionCookie(t *testing.T) {
	server := testServer(&fakeRetriever{}, &fakeGenerator{})
	qdrant := &qdrantStub{collections: []string{}}
	server.qdrant = qdrant
	login := loginRequest(server.Handler(), "admin", "password", "192.0.2.20:1234")

	request := httptest.NewRequest(http.MethodGet, "/v1/collections", nil)
	request.AddCookie(login.Result().Cookies()[0])
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"collections":[]`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestCollectionsMapsQdrantFailureToServiceUnavailable(t *testing.T) {
	server := testServer(&fakeRetriever{}, &fakeGenerator{})
	server.qdrant = &qdrantStub{listErr: models.ErrQdrantUnavailable}
	request := httptest.NewRequest(http.MethodGet, "/v1/collections", nil)
	request.Header.Set("Authorization", "Bearer key")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), `"code":"qdrant_unavailable"`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
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

	authorized := httptest.NewRequest(http.MethodPost, "/v1/answer", strings.NewReader(`{"query":"q","collections":["methodology_e5_small_v1"]}`))
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
	response := perform(testServer(&fakeRetriever{}, generator).Handler(), "/v1/answer", `{"query":"q","collections":["methodology_e5_small_v1"]}`, "key")
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
	response := perform(testServer(&fakeRetriever{results: sampleResults()}, generator).Handler(), "/v1/answer", `{"query":"Как провести урок?","collections":["methodology_e5_small_v1"]}`, "key")
	if response.Code != 200 {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var result models.AnswerResponse
	json.NewDecoder(response.Body).Decode(&result)
	if len(result.Sources) != 1 || result.Sources[0].ChunkID != "chunk_1" || result.Sources[0].Collection != "methodology_e5_small_v1" || result.Sources[0].URL != "https://hwschool.bitrix24.ru/note/document/1/" {
		t.Fatalf("sources=%+v", result.Sources)
	}
	if strings.Contains(generator.prompts[0], "Второй") == false {
		t.Fatal("selected context missing")
	}
}

func TestAnswerPassesCollectionsToRetriever(t *testing.T) {
	retriever := &fakeRetriever{}
	response := perform(testServer(retriever, &fakeGenerator{}).Handler(), "/v1/answer", `{"query":"q","collections":["accounting_e5_small_v1"]}`, "key")
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if len(retriever.options) != 1 || len(retriever.options[0].Collections) != 1 || retriever.options[0].Collections[0] != "accounting_e5_small_v1" {
		t.Fatalf("options=%+v", retriever.options)
	}
}

func TestAnswerRejectsEmptyCollectionName(t *testing.T) {
	response := perform(testServer(&fakeRetriever{}, &fakeGenerator{}).Handler(), "/v1/answer", `{"query":"q","collections":[""]}`, "key")
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "collections") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestSearchAndAnswerRequireNonEmptyCollections(t *testing.T) {
	for _, path := range []string{"/v1/search", "/v1/answer"} {
		for _, body := range []string{
			`{"query":"q"}`,
			`{"query":"q","collections":null}`,
			`{"query":"q","collections":[]}`,
		} {
			retriever := &fakeRetriever{}
			generator := &fakeGenerator{}
			response := perform(testServer(retriever, generator).Handler(), path, body, "key")
			if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"invalid_request"`) || !strings.Contains(response.Body.String(), "collections") {
				t.Fatalf("path=%s body=%s status=%d response=%s", path, body, response.Code, response.Body.String())
			}
			if retriever.calls.Load() != 0 || generator.calls.Load() != 0 {
				t.Fatalf("path=%s body=%s retriever=%d generator=%d", path, body, retriever.calls.Load(), generator.calls.Load())
			}
		}
	}
}

func TestSearchDeduplicatesCollectionsAndReportsEffectiveList(t *testing.T) {
	retriever := &fakeRetriever{}
	response := perform(testServer(retriever, &fakeGenerator{}).Handler(), "/v1/search", `{"query":"q","collections":["workspace_17"," workspace_17 ","workspace_25"]}`, "key")
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if len(retriever.options) != 1 || len(retriever.options[0].Collections) != 2 || retriever.options[0].Collections[0] != "workspace_17" || retriever.options[0].Collections[1] != "workspace_25" {
		t.Fatalf("options=%+v", retriever.options)
	}
	var result models.SearchResponse
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if len(result.Retrieval.Collections) != 2 || result.Retrieval.Collections[0] != "workspace_17" || result.Retrieval.Collections[1] != "workspace_25" {
		t.Fatalf("retrieval=%+v", result.Retrieval)
	}
}

func TestAnswerRepairsInvalidCitationOnce(t *testing.T) {
	generator := &fakeGenerator{results: []models.LLMResult{{Content: "Ответ [S99]."}, {Content: "Исправленный ответ [S2]."}}}
	response := perform(testServer(&fakeRetriever{results: sampleResults()}, generator).Handler(), "/v1/answer", `{"query":"q","collections":["methodology_e5_small_v1"]}`, "key")
	if response.Code != 200 || generator.calls.Load() != 2 {
		t.Fatalf("status=%d calls=%d body=%s", response.Code, generator.calls.Load(), response.Body.String())
	}
	if strings.Contains(response.Body.String(), "S99") {
		t.Fatal("invented source returned")
	}
}

func TestAnswerFailsAfterOneBadRepair(t *testing.T) {
	generator := &fakeGenerator{results: []models.LLMResult{{Content: "Без цитат"}, {Content: "Снова [S42]"}}}
	response := perform(testServer(&fakeRetriever{results: sampleResults()}, generator).Handler(), "/v1/answer", `{"query":"q","collections":["methodology_e5_small_v1"]}`, "key")
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
	request := httptest.NewRequest(http.MethodPost, "/v1/search", bytes.NewBufferString(`{"query":"q","collections":["methodology_e5_small_v1"]}`)).WithContext(ctx)
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
	return New(cfg, retriever, generator, readyStub{}, &qdrantStub{}, observability.NewMetrics(), slog.New(slog.NewTextHandler(io.Discard, nil)))
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
	return []models.SearchResult{{SourceID: "S1", Collection: "methodology_e5_small_v1", ChunkID: "chunk_1", DocumentID: "page_1", Score: .9, Content: "Первый", Title: "Документ 1", URL: "https://hwschool.bitrix24.ru/note/document/1/", Breadcrumbs: []string{"Root"}}, {SourceID: "S2", Collection: "accounting_e5_small_v1", ChunkID: "chunk_2", DocumentID: "page_2", Score: .8, Content: "Второй", Title: "Документ 2", Breadcrumbs: []string{"Root"}}}
}
