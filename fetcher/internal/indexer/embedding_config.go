package indexer

import (
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
)

func LoadEmbeddingConfig() (EmbeddingConfig, error) {
	requiredString := func(name string, allowEmpty bool) (string, error) {
		value, found := os.LookupEnv(name)
		if !found || (!allowEmpty && strings.TrimSpace(value) == "") {
			return "", fmt.Errorf("%s is required", name)
		}
		if allowEmpty {
			return value, nil
		}
		return strings.TrimSpace(value), nil
	}
	modelID, err := requiredString("EMBEDDING_MODEL_ID", false)
	if err != nil {
		return EmbeddingConfig{}, err
	}
	revision, err := requiredString("EMBEDDING_MODEL_REVISION", false)
	if err != nil {
		return EmbeddingConfig{}, err
	}
	dtype, err := requiredString("EMBEDDING_DTYPE", false)
	if err != nil {
		return EmbeddingConfig{}, err
	}
	pooling, err := requiredString("EMBEDDING_POOLING", false)
	if err != nil {
		return EmbeddingConfig{}, err
	}
	documentPrefix, err := requiredString("EMBEDDING_DOCUMENT_PREFIX", true)
	if err != nil {
		return EmbeddingConfig{}, err
	}
	queryPrefix, err := requiredString("EMBEDDING_QUERY_PREFIX", true)
	if err != nil {
		return EmbeddingConfig{}, err
	}
	pipelineVersion, err := requiredString("EMBEDDING_PIPELINE_VERSION", false)
	if err != nil {
		return EmbeddingConfig{}, err
	}
	pointNamespace, err := requiredString("EMBEDDING_POINT_NAMESPACE", false)
	if err != nil {
		return EmbeddingConfig{}, err
	}
	tokenizerSHA, err := requiredString("EMBEDDING_TOKENIZER_SHA256", false)
	if err != nil {
		return EmbeddingConfig{}, err
	}
	tokenizerConfigSHA, err := requiredString("EMBEDDING_TOKENIZER_CONFIG_SHA256", false)
	if err != nil {
		return EmbeddingConfig{}, err
	}
	cacheDir, err := requiredString("EMBEDDING_CACHE_DIR", false)
	if err != nil {
		return EmbeddingConfig{}, err
	}
	dimension, err := requiredPositiveInt("EMBEDDING_DIMENSION")
	if err != nil {
		return EmbeddingConfig{}, err
	}
	maxTokens, err := requiredPositiveInt("EMBEDDING_MAX_INPUT_TOKENS")
	if err != nil {
		return EmbeddingConfig{}, err
	}
	cacheSize, err := requiredPositiveInt("EMBEDDING_VECTOR_CACHE_SIZE")
	if err != nil {
		return EmbeddingConfig{}, err
	}
	normalized, err := requiredEmbeddingBool("EMBEDDING_NORMALIZED")
	if err != nil {
		return EmbeddingConfig{}, err
	}
	autoTruncate, err := requiredEmbeddingBool("EMBEDDING_AUTO_TRUNCATE")
	if err != nil {
		return EmbeddingConfig{}, err
	}
	config := EmbeddingConfig{
		ModelID: modelID, ModelRevision: revision, Dimension: dimension, DType: dtype,
		Pooling: pooling, Normalized: normalized, DocumentPrefix: documentPrefix, QueryPrefix: queryPrefix,
		MaxInputTokens: maxTokens, AutoTruncate: autoTruncate, PipelineVersion: pipelineVersion,
		PointNamespace: pointNamespace, TokenizerSHA256: tokenizerSHA,
		TokenizerConfigSHA256: tokenizerConfigSHA, CacheDir: cacheDir, VectorCacheSize: cacheSize,
	}
	if err := config.Validate(); err != nil {
		return EmbeddingConfig{}, err
	}
	return config, nil
}

func (config EmbeddingConfig) Validate() error {
	if config.ModelID == "" || config.ModelRevision == "" || config.Dimension <= 0 || config.DType == "" || config.Pooling == "" || strings.TrimSpace(config.QueryPrefix) == "" || config.MaxInputTokens <= 0 || config.PipelineVersion == "" || config.CacheDir == "" || config.VectorCacheSize <= 0 {
		return fmt.Errorf("embedding configuration is incomplete")
	}
	namespace, err := hex.DecodeString(strings.ReplaceAll(config.PointNamespace, "-", ""))
	if err != nil || len(namespace) != 16 {
		return fmt.Errorf("EMBEDDING_POINT_NAMESPACE must be a UUID")
	}
	for name, value := range map[string]string{"EMBEDDING_TOKENIZER_SHA256": config.TokenizerSHA256, "EMBEDDING_TOKENIZER_CONFIG_SHA256": config.TokenizerConfigSHA256} {
		if len(value) != 64 {
			return fmt.Errorf("%s must be a lowercase SHA-256", name)
		}
		if _, err := hex.DecodeString(value); err != nil || value != strings.ToLower(value) {
			return fmt.Errorf("%s must be a lowercase SHA-256", name)
		}
	}
	return nil
}

func requiredPositiveInt(name string) (int, error) {
	value, found := os.LookupEnv(name)
	if !found || strings.TrimSpace(value) == "" {
		return 0, fmt.Errorf("%s is required", name)
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return parsed, nil
}

func requiredEmbeddingBool(name string) (bool, error) {
	value, found := os.LookupEnv(name)
	if !found || strings.TrimSpace(value) == "" {
		return false, fmt.Errorf("%s is required", name)
	}
	parsed, err := strconv.ParseBool(strings.TrimSpace(value))
	if err != nil {
		return false, fmt.Errorf("%s must be true or false", name)
	}
	return parsed, nil
}
