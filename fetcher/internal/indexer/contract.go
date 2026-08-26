package indexer

import (
	"encoding/hex"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)

func LoadEmbeddingContract() (EmbeddingContract, error) {
	modelID, err := requiredEnv("EMBEDDING_MODEL_ID", false)
	if err != nil {
		return EmbeddingContract{}, err
	}
	revision, err := requiredEnv("EMBEDDING_MODEL_REVISION", false)
	if err != nil {
		return EmbeddingContract{}, err
	}
	dimension, err := requiredPositiveIntEnv("EMBEDDING_DIMENSION")
	if err != nil {
		return EmbeddingContract{}, err
	}
	dtype, err := requiredEnv("EMBEDDING_DTYPE", false)
	if err != nil {
		return EmbeddingContract{}, err
	}
	pooling, err := requiredEnv("EMBEDDING_POOLING", false)
	if err != nil {
		return EmbeddingContract{}, err
	}
	normalized, err := requiredBoolEnv("EMBEDDING_NORMALIZED")
	if err != nil {
		return EmbeddingContract{}, err
	}
	documentPrefix, err := requiredEnv("EMBEDDING_DOCUMENT_PREFIX", true)
	if err != nil {
		return EmbeddingContract{}, err
	}
	queryPrefix, err := requiredEnv("EMBEDDING_QUERY_PREFIX", false)
	if err != nil {
		return EmbeddingContract{}, err
	}
	maxTokens, err := requiredPositiveIntEnv("EMBEDDING_MAX_INPUT_TOKENS")
	if err != nil {
		return EmbeddingContract{}, err
	}
	autoTruncate, err := requiredBoolEnv("EMBEDDING_AUTO_TRUNCATE")
	if err != nil {
		return EmbeddingContract{}, err
	}
	pipelineVersion, err := requiredEnv("EMBEDDING_PIPELINE_VERSION", false)
	if err != nil {
		return EmbeddingContract{}, err
	}
	pointNamespace, err := requiredEnv("EMBEDDING_POINT_NAMESPACE", false)
	if err != nil {
		return EmbeddingContract{}, err
	}

	contract := EmbeddingContract{
		ModelID: modelID, Revision: revision, Dimension: dimension, DType: dtype,
		Pooling: pooling, Normalized: normalized, DocumentPrefix: documentPrefix,
		QueryPrefix: queryPrefix, MaxInputTokens: maxTokens, AutoTruncate: autoTruncate,
		PipelineVersion: pipelineVersion, PointNamespace: pointNamespace,
	}
	if err := contract.Validate(); err != nil {
		return EmbeddingContract{}, err
	}
	return contract, nil
}

func (contract EmbeddingContract) Validate() error {
	for name, value := range map[string]string{
		"model ID": contract.ModelID, "revision": contract.Revision, "dtype": contract.DType,
		"pooling": contract.Pooling, "query prefix": contract.QueryPrefix,
		"pipeline version": contract.PipelineVersion, "point namespace": contract.PointNamespace,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("embedding %s must not be empty", name)
		}
	}
	if contract.Dimension <= 0 || contract.MaxInputTokens <= 0 {
		return fmt.Errorf("embedding dimension and max input tokens must be positive")
	}
	if contract.Pooling != "cls" && contract.Pooling != "mean" && contract.Pooling != "last_token" {
		return fmt.Errorf("unsupported embedding pooling %q", contract.Pooling)
	}
	if !uuidPattern.MatchString(contract.PointNamespace) {
		return fmt.Errorf("invalid embedding point namespace %q", contract.PointNamespace)
	}
	namespace, err := hex.DecodeString(strings.ReplaceAll(contract.PointNamespace, "-", ""))
	if err != nil || len(namespace) != 16 {
		return fmt.Errorf("invalid embedding point namespace %q", contract.PointNamespace)
	}
	return nil
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

func requiredPositiveIntEnv(name string) (int, error) {
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

func requiredBoolEnv(name string) (bool, error) {
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
