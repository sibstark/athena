package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/sibstark/athena/rag-api/internal/models"
	"golang.org/x/crypto/bcrypt"
)

var embeddingUUIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)

type Config struct {
	Embedding         models.EmbeddingContract
	HTTPAddr          string
	HTTPMaxBodyBytes  int64
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
	CORSOrigins       []string
	APIKey            string
	AuthUsername      string
	AuthPasswordHash  string
	AuthSessionSecret string
	AuthSessionTTL    time.Duration
	AuthCookieSecure  bool

	TEIURL     string
	TEITimeout time.Duration

	QdrantURL        string
	QdrantAPIKey     string
	QdrantCollection string
	QdrantTimeout    time.Duration

	DeepSeekBaseURL         string
	DeepSeekAPIKey          string
	DeepSeekModel           string
	DeepSeekTimeout         time.Duration
	DeepSeekMaxOutputTokens int
	DeepSeekThinkingEnabled bool
	DeepSeekMaxConcurrency  int

	DefaultTopK      int
	MaxTopK          int
	ScoreThreshold   float64
	MaxQueryChars    int
	MaxContextChars  int
	MaxContextChunks int
	LogLevel         string
}

func Load() (Config, error) {
	if err := loadRootEnvironment(); err != nil {
		return Config{}, err
	}
	if err := validateEnvironmentSyntax(); err != nil {
		return Config{}, err
	}
	embedding, err := loadEmbeddingContract()
	if err != nil {
		return Config{}, err
	}
	cfg := Config{
		Embedding:               embedding,
		HTTPAddr:                env("HTTP_ADDR", ":8080"),
		HTTPMaxBodyBytes:        int64(envInt("HTTP_MAX_BODY_BYTES", 1<<20)),
		ReadHeaderTimeout:       envDuration("HTTP_READ_HEADER_TIMEOUT", 5*time.Second),
		ReadTimeout:             envDuration("HTTP_READ_TIMEOUT", 15*time.Second),
		WriteTimeout:            envDuration("HTTP_WRITE_TIMEOUT", 90*time.Second),
		IdleTimeout:             envDuration("HTTP_IDLE_TIMEOUT", 120*time.Second),
		ShutdownTimeout:         envDuration("HTTP_SHUTDOWN_TIMEOUT", 15*time.Second),
		CORSOrigins:             splitCSV(os.Getenv("CORS_ALLOWED_ORIGINS")),
		APIKey:                  strings.TrimSpace(os.Getenv("RAG_API_KEY")),
		AuthUsername:            strings.TrimSpace(os.Getenv("AUTH_USERNAME")),
		AuthPasswordHash:        strings.TrimSpace(os.Getenv("AUTH_PASSWORD_HASH")),
		AuthSessionSecret:       strings.TrimSpace(os.Getenv("AUTH_SESSION_SECRET")),
		AuthSessionTTL:          envDuration("AUTH_SESSION_TTL", 24*time.Hour),
		AuthCookieSecure:        envBool("AUTH_COOKIE_SECURE", false),
		TEIURL:                  env("TEI_URL", "http://127.0.0.1:8080"),
		TEITimeout:              envDuration("TEI_TIMEOUT", 10*time.Second),
		QdrantURL:               env("QDRANT_URL", "http://127.0.0.1:6333"),
		QdrantAPIKey:            strings.TrimSpace(os.Getenv("QDRANT_API_KEY")),
		QdrantCollection:        env("QDRANT_COLLECTION", "methodology_e5_small_v1"),
		QdrantTimeout:           envDuration("QDRANT_TIMEOUT", 10*time.Second),
		DeepSeekBaseURL:         env("DEEPSEEK_BASE_URL", "https://api.deepseek.com"),
		DeepSeekAPIKey:          strings.TrimSpace(os.Getenv("DEEPSEEK_API_KEY")),
		DeepSeekModel:           env("DEEPSEEK_MODEL", "deepseek-v4-flash"),
		DeepSeekTimeout:         envDuration("DEEPSEEK_TIMEOUT", 60*time.Second),
		DeepSeekMaxOutputTokens: envInt("DEEPSEEK_MAX_OUTPUT_TOKENS", 1200),
		DeepSeekThinkingEnabled: envBool("DEEPSEEK_THINKING_ENABLED", false),
		DeepSeekMaxConcurrency:  envInt("DEEPSEEK_MAX_CONCURRENCY", 4),
		DefaultTopK:             envInt("RAG_DEFAULT_TOP_K", 8),
		MaxTopK:                 envInt("RAG_MAX_TOP_K", 20),
		ScoreThreshold:          envFloat("RAG_SCORE_THRESHOLD", 0.65),
		MaxQueryChars:           envInt("RAG_MAX_QUERY_CHARS", 4000),
		MaxContextChars:         envInt("RAG_MAX_CONTEXT_CHARS", 30000),
		MaxContextChunks:        envInt("RAG_MAX_CONTEXT_CHUNKS", 8),
		LogLevel:                strings.ToLower(env("LOG_LEVEL", "info")),
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func loadRootEnvironment() error {
	for _, path := range []string{".env", "../.env"} {
		if _, err := os.Stat(path); err == nil {
			if err := godotenv.Load(path); err != nil {
				return fmt.Errorf("load %s: %w", path, err)
			}
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect %s: %w", path, err)
		}
	}
	return nil
}

func loadEmbeddingContract() (models.EmbeddingContract, error) {
	modelID, err := requiredEnv("EMBEDDING_MODEL_ID", false)
	if err != nil {
		return models.EmbeddingContract{}, err
	}
	revision, err := requiredEnv("EMBEDDING_MODEL_REVISION", false)
	if err != nil {
		return models.EmbeddingContract{}, err
	}
	dimension, err := requiredPositiveInt("EMBEDDING_DIMENSION")
	if err != nil {
		return models.EmbeddingContract{}, err
	}
	dtype, err := requiredEnv("EMBEDDING_DTYPE", false)
	if err != nil {
		return models.EmbeddingContract{}, err
	}
	pooling, err := requiredEnv("EMBEDDING_POOLING", false)
	if err != nil {
		return models.EmbeddingContract{}, err
	}
	normalized, err := requiredBool("EMBEDDING_NORMALIZED")
	if err != nil {
		return models.EmbeddingContract{}, err
	}
	documentPrefix, err := requiredEnv("EMBEDDING_DOCUMENT_PREFIX", true)
	if err != nil {
		return models.EmbeddingContract{}, err
	}
	queryPrefix, err := requiredEnv("EMBEDDING_QUERY_PREFIX", false)
	if err != nil {
		return models.EmbeddingContract{}, err
	}
	maxTokens, err := requiredPositiveInt("EMBEDDING_MAX_INPUT_TOKENS")
	if err != nil {
		return models.EmbeddingContract{}, err
	}
	autoTruncate, err := requiredBool("EMBEDDING_AUTO_TRUNCATE")
	if err != nil {
		return models.EmbeddingContract{}, err
	}
	pipelineVersion, err := requiredEnv("EMBEDDING_PIPELINE_VERSION", false)
	if err != nil {
		return models.EmbeddingContract{}, err
	}
	pointNamespace, err := requiredEnv("EMBEDDING_POINT_NAMESPACE", false)
	if err != nil {
		return models.EmbeddingContract{}, err
	}
	contract := models.EmbeddingContract{
		ModelID: modelID, Revision: revision, Dimension: dimension, DType: dtype,
		Pooling: pooling, Normalized: normalized, DocumentPrefix: documentPrefix,
		QueryPrefix: queryPrefix, MaxInputTokens: maxTokens, AutoTruncate: autoTruncate,
		PipelineVersion: pipelineVersion, PointNamespace: pointNamespace,
	}
	if contract.Pooling != "cls" && contract.Pooling != "mean" && contract.Pooling != "last_token" {
		return models.EmbeddingContract{}, fmt.Errorf("EMBEDDING_POOLING is unsupported: %s", contract.Pooling)
	}
	if !embeddingUUIDPattern.MatchString(contract.PointNamespace) {
		return models.EmbeddingContract{}, fmt.Errorf("EMBEDDING_POINT_NAMESPACE must be a UUID")
	}
	return contract, nil
}

func requiredEnv(name string, allowEmpty bool) (string, error) {
	value, exists := os.LookupEnv(name)
	if !exists {
		return "", fmt.Errorf("%s is required", name)
	}
	if !allowEmpty && strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%s must not be empty", name)
	}
	return value, nil
}

func requiredPositiveInt(name string) (int, error) {
	value, err := requiredEnv(name, false)
	if err != nil {
		return 0, err
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return parsed, nil
}

func requiredBool(name string) (bool, error) {
	value, err := requiredEnv(name, false)
	if err != nil {
		return false, err
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%s must be true or false", name)
	}
	return parsed, nil
}

func (c Config) Validate() error {
	var missing []string
	if c.APIKey == "" {
		missing = append(missing, "RAG_API_KEY")
	}
	if c.DeepSeekAPIKey == "" {
		missing = append(missing, "DEEPSEEK_API_KEY")
	}
	if c.AuthUsername == "" {
		missing = append(missing, "AUTH_USERNAME")
	}
	if c.AuthPasswordHash == "" {
		missing = append(missing, "AUTH_PASSWORD_HASH")
	}
	if c.AuthSessionSecret == "" {
		missing = append(missing, "AUTH_SESSION_SECRET")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
	}
	if _, err := bcrypt.Cost([]byte(c.AuthPasswordHash)); err != nil {
		return fmt.Errorf("AUTH_PASSWORD_HASH must be a valid bcrypt hash")
	}
	if len([]byte(c.AuthSessionSecret)) < 32 {
		return fmt.Errorf("AUTH_SESSION_SECRET must be at least 32 bytes")
	}
	for name, raw := range map[string]string{"TEI_URL": c.TEIURL, "QDRANT_URL": c.QdrantURL, "DEEPSEEK_BASE_URL": c.DeepSeekBaseURL} {
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("%s must be a valid HTTP(S) URL", name)
		}
	}
	if c.DefaultTopK < 1 || c.MaxTopK < c.DefaultTopK {
		return fmt.Errorf("RAG top-k limits are invalid")
	}
	if strings.TrimSpace(c.QdrantCollection) == "" || strings.TrimSpace(c.DeepSeekModel) == "" || strings.TrimSpace(c.HTTPAddr) == "" {
		return fmt.Errorf("HTTP_ADDR, QDRANT_COLLECTION and DEEPSEEK_MODEL must not be empty")
	}
	if c.ScoreThreshold < -1 || c.ScoreThreshold > 1 {
		return fmt.Errorf("RAG_SCORE_THRESHOLD must be between -1 and 1")
	}
	if c.MaxQueryChars < 1 || c.MaxContextChars < 1 || c.MaxContextChunks < 1 {
		return fmt.Errorf("RAG character and chunk limits must be positive")
	}
	if c.DeepSeekMaxConcurrency < 1 || c.DeepSeekMaxOutputTokens < 1 {
		return fmt.Errorf("DeepSeek limits must be positive")
	}
	if c.AuthSessionTTL <= 0 {
		return fmt.Errorf("AUTH_SESSION_TTL must be positive")
	}
	for name, duration := range map[string]time.Duration{"TEI_TIMEOUT": c.TEITimeout, "QDRANT_TIMEOUT": c.QdrantTimeout, "DEEPSEEK_TIMEOUT": c.DeepSeekTimeout, "HTTP_READ_TIMEOUT": c.ReadTimeout, "HTTP_WRITE_TIMEOUT": c.WriteTimeout} {
		if duration <= 0 {
			return fmt.Errorf("%s must be positive", name)
		}
	}
	return nil
}

func validateEnvironmentSyntax() error {
	for _, name := range []string{"HTTP_MAX_BODY_BYTES", "DEEPSEEK_MAX_OUTPUT_TOKENS", "DEEPSEEK_MAX_CONCURRENCY", "RAG_DEFAULT_TOP_K", "RAG_MAX_TOP_K", "RAG_MAX_QUERY_CHARS", "RAG_MAX_CONTEXT_CHARS", "RAG_MAX_CONTEXT_CHUNKS"} {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			if _, err := strconv.Atoi(value); err != nil {
				return fmt.Errorf("%s must be an integer", name)
			}
		}
	}
	if value := strings.TrimSpace(os.Getenv("RAG_SCORE_THRESHOLD")); value != "" {
		if _, err := strconv.ParseFloat(value, 64); err != nil {
			return fmt.Errorf("RAG_SCORE_THRESHOLD must be a number")
		}
	}
	if value := strings.TrimSpace(os.Getenv("DEEPSEEK_THINKING_ENABLED")); value != "" {
		if _, err := strconv.ParseBool(value); err != nil {
			return fmt.Errorf("DEEPSEEK_THINKING_ENABLED must be true or false")
		}
	}
	if value := strings.TrimSpace(os.Getenv("AUTH_COOKIE_SECURE")); value != "" {
		if _, err := strconv.ParseBool(value); err != nil {
			return fmt.Errorf("AUTH_COOKIE_SECURE must be true or false")
		}
	}
	for _, name := range []string{"HTTP_READ_HEADER_TIMEOUT", "HTTP_READ_TIMEOUT", "HTTP_WRITE_TIMEOUT", "HTTP_IDLE_TIMEOUT", "HTTP_SHUTDOWN_TIMEOUT", "TEI_TIMEOUT", "QDRANT_TIMEOUT", "DEEPSEEK_TIMEOUT", "AUTH_SESSION_TTL"} {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			if _, err := time.ParseDuration(value); err != nil {
				return fmt.Errorf("%s must be a Go duration", name)
			}
		}
	}
	return nil
}

func env(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
func envInt(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}
func envFloat(name string, fallback float64) float64 {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return fallback
	}
	return parsed
}
func envBool(name string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}
func envDuration(name string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}
func splitCSV(value string) []string {
	var result []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			result = append(result, item)
		}
	}
	return result
}
