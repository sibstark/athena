package snapshot

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

type archiveLock struct {
	file *os.File
}

func acquireArchiveLock(root string) (*archiveLock, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("create output root: %w", err)
	}
	path := filepath.Join(root, ".fetcher.lock")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open archive lock: %w", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, errors.New("another fetcher run is already active for this output directory")
		}
		return nil, fmt.Errorf("lock output archive: %w", err)
	}
	return &archiveLock{file: file}, nil
}

func (lock *archiveLock) Close() error {
	unlockErr := syscall.Flock(int(lock.file.Fd()), syscall.LOCK_UN)
	closeErr := lock.file.Close()
	return errors.Join(unlockErr, closeErr)
}

func rebuildRegistry(root string) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return fmt.Errorf("read output archive: %w", err)
	}
	runs := make([]RunEntry, 0)
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "run_") {
			continue
		}
		reportPath := filepath.Join(root, entry.Name(), "report.json")
		data, err := os.ReadFile(reportPath)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("read %s: %w", reportPath, err)
		}
		var report Report
		if err := json.Unmarshal(data, &report); err != nil {
			return fmt.Errorf("decode %s: %w", reportPath, err)
		}
		if report.RunID != entry.Name() {
			return fmt.Errorf("run directory %s contains report for %s", entry.Name(), report.RunID)
		}
		run := RunEntry{
			ID:             report.RunID,
			Path:           report.RunID,
			Status:         report.Status,
			StartedAt:      report.StartedAt,
			FinishedAt:     report.FinishedAt,
			ReportPath:     filepath.ToSlash(filepath.Join(report.RunID, "report.json")),
			WorkspaceCount: report.WorkspaceCount,
			DocumentCount:  report.DocumentCount,
		}
		if report.Status != StatusFailed {
			run.ManifestPath = filepath.ToSlash(filepath.Join(report.RunID, "manifest.json"))
		}
		runs = append(runs, run)
	}
	sort.Slice(runs, func(i, j int) bool {
		left, leftErr := time.Parse(time.RFC3339Nano, runs[i].StartedAt)
		right, rightErr := time.Parse(time.RFC3339Nano, runs[j].StartedAt)
		if leftErr == nil && rightErr == nil && !left.Equal(right) {
			return left.Before(right)
		}
		return runs[i].ID < runs[j].ID
	})
	return writeJSONAtomic(filepath.Join(root, "runs.json"), Registry{FormatVersion: FormatVersion, Runs: runs})
}

func writeJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func writeJSONAtomic(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	encoder := json.NewEncoder(temporary)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}
