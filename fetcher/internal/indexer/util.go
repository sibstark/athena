package indexer

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"regexp"
	"strings"
)

var knownPrefix = regexp.MustCompile(`^(?:passage|query):\s*`)

func embeddingInput(text string) string {
	return "passage: " + knownPrefix.ReplaceAllString(text, "")
}

func hashText(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func pointID(name string) (string, error) {
	namespace, err := hex.DecodeString(strings.ReplaceAll(PointIDNamespace, "-", ""))
	if err != nil || len(namespace) != 16 {
		return "", fmt.Errorf("invalid point namespace %q", PointIDNamespace)
	}
	h := sha1.New()
	_, _ = h.Write(namespace)
	_, _ = h.Write([]byte(name))
	digest := h.Sum(nil)[:16]
	digest[6] = (digest[6] & 0x0f) | 0x50
	digest[8] = (digest[8] & 0x3f) | 0x80
	hexValue := hex.EncodeToString(digest)
	return fmt.Sprintf("%s-%s-%s-%s-%s", hexValue[:8], hexValue[8:12], hexValue[12:16], hexValue[16:20], hexValue[20:]), nil
}

func stableHash(value map[string]any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return hashText(string(data)), nil
}

func validateVector(vector []float64) error {
	if len(vector) != DefaultDimension {
		return fmt.Errorf("embedding dimension is %d, expected %d", len(vector), DefaultDimension)
	}
	normSquared := 0.0
	for _, value := range vector {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return fmt.Errorf("embedding contains NaN or Infinity")
		}
		normSquared += value * value
	}
	norm := math.Sqrt(normSquared)
	if norm < 0.99 || norm > 1.01 {
		return fmt.Errorf("embedding L2 norm is %.6f, expected approximately 1", norm)
	}
	return nil
}

func portableRelative(path string) string {
	relative, err := filepath.Rel(".", path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(relative)
}

func pipeline(version string) PipelineIdentity {
	return PipelineIdentity{
		EmbeddingModel: DefaultModel, EmbeddingModelRevision: DefaultModelRevision,
		EmbeddingDimension: DefaultDimension, EmbeddingDType: DefaultDType,
		EmbeddingPooling: "mean", EmbeddingNormalized: true, DocumentPrefix: "passage:",
		EmbeddingPipelineVersion: version, PointNamespace: PointIDNamespace,
	}
}

func roundMS(value float64) float64 { return math.Round(value*100) / 100 }
