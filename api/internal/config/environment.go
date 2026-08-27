package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
)

// LoadEnvironment loads the single repository-level .env for local commands.
// Existing process variables keep precedence, including in Docker.
func LoadEnvironment() error {
	workingDirectory, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve working directory: %w", err)
	}
	candidates := []string{filepath.Join(workingDirectory, ".env")}
	if filepath.Base(workingDirectory) == "api" {
		candidates = []string{filepath.Join(workingDirectory, "..", ".env"), filepath.Join(workingDirectory, ".env")}
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			if err := godotenv.Load(candidate); err != nil {
				return fmt.Errorf("load %s: %w", candidate, err)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect %s: %w", candidate, err)
		}
	}
	return nil
}
