package tei

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sibstark/athena/rag-api/internal/models"
)

func TestEmbedQueryUsesConfiguredContract(t *testing.T) {
	contract := testContract()
	var embedded string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/info":
			json.NewEncoder(w).Encode(Info{ModelID: contract.ModelID, ModelSHA: contract.Revision, MaxInputLength: contract.MaxInputTokens, AutoTruncate: contract.AutoTruncate, ModelDType: contract.DType})
		case "/tokenize":
			json.NewEncoder(w).Encode([]int{1, 2, 3})
		case "/embed":
			var body struct {
				Inputs    []string `json:"inputs"`
				Normalize bool     `json:"normalize"`
				Truncate  bool     `json:"truncate"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			embedded = body.Inputs[0]
			if !body.Normalize || body.Truncate {
				t.Errorf("unexpected embed flags: %+v", body)
			}
			json.NewEncoder(w).Encode([][]float64{unitVector()})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := New(server.URL, time.Second, contract, nil)
	vector, err := client.EmbedQuery(t.Context(), "query: вопрос")
	if err != nil {
		t.Fatal(err)
	}
	if embedded != "query: вопрос" {
		t.Fatalf("input = %q", embedded)
	}
	if len(vector) != contract.Dimension {
		t.Fatalf("dimension = %d", len(vector))
	}
}

func TestEmbedQueryRejectsWrongDimension(t *testing.T) {
	contract := testContract()
	server := teiServer(t, contract, [][]float64{{1}})
	defer server.Close()
	_, err := New(server.URL, time.Second, contract, nil).EmbedQuery(t.Context(), "текст")
	if err == nil || !strings.Contains(err.Error(), "dimension") {
		t.Fatalf("error = %v", err)
	}
}

func TestEmbedQueryReportsTEIError(t *testing.T) {
	contract := testContract()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/info" {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	_, err := New(server.URL, time.Second, contract, nil).EmbedQuery(t.Context(), "текст")
	if err == nil || !strings.Contains(err.Error(), "TEI unavailable") {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateVectorRejectsInvalidValuesAndNorm(t *testing.T) {
	contract := testContract()
	vector := unitVector()
	vector[1] = math.NaN()
	if ValidateVector(vector, contract) == nil {
		t.Fatal("NaN accepted")
	}
	vector = make([]float64, contract.Dimension)
	vector[0] = 2
	if ValidateVector(vector, contract) == nil {
		t.Fatal("invalid norm accepted")
	}
}

func TestPrefixQueryReplacesKnownPrefix(t *testing.T) {
	contract := testContract()
	for input, want := range map[string]string{"текст": "query: текст", "query: текст": "query: текст", "passage: текст": "query: текст"} {
		if got := PrefixQuery(input, contract.QueryPrefix); got != want {
			t.Errorf("PrefixQuery(%q)=%q", input, got)
		}
	}
}

func teiServer(t *testing.T, contract models.EmbeddingContract, vectors [][]float64) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/info":
			json.NewEncoder(w).Encode(Info{ModelID: contract.ModelID, ModelSHA: contract.Revision, MaxInputLength: contract.MaxInputTokens, AutoTruncate: contract.AutoTruncate, ModelDType: contract.DType})
		case "/tokenize":
			json.NewEncoder(w).Encode([]int{1})
		case "/embed":
			json.NewEncoder(w).Encode(vectors)
		case "/health":
			w.WriteHeader(200)
		default:
			http.NotFound(w, r)
		}
	}))
}
func unitVector() []float64 {
	result := make([]float64, testContract().Dimension)
	result[0] = 1
	return result
}

func testContract() models.EmbeddingContract {
	return models.EmbeddingContract{
		ModelID: "test/model", Revision: "test-revision", Dimension: 8, DType: "float32",
		Pooling: "cls", Normalized: true, DocumentPrefix: "", QueryPrefix: "query: ",
		MaxInputTokens: 8192, AutoTruncate: false, PipelineVersion: "2",
		PointNamespace: "6195ee42-5bb1-4440-93c2-285e67dbcbf2",
	}
}
