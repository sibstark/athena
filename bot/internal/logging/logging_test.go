package logging

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLoggerWritesJSONToConsoleAndFile(t *testing.T) {
	directory := t.TempDir()
	now := time.Date(2026, time.August, 18, 11, 30, 0, 0, time.UTC)
	var console bytes.Buffer

	logger, err := newLogger(directory, &console, func() time.Time { return now })
	if err != nil {
		t.Fatalf("newLogger() error = %v", err)
	}
	logger.Info("server started", "port", "8080")
	if err := logger.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	path := filepath.Join(directory, filePrefix+now.Format(fileTimeLayout)+".log")
	fileData, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if console.String() != string(fileData) {
		t.Fatalf("console and file output differ\nconsole: %s\nfile: %s", console.String(), fileData)
	}

	var record map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(fileData), &record); err != nil {
		t.Fatalf("log is not valid JSON: %v", err)
	}
	if record["msg"] != "server started" || record["port"] != "8080" || record["level"] != "INFO" {
		t.Fatalf("unexpected log record: %#v", record)
	}
}

func TestHourlyWriterRotatesAndAppends(t *testing.T) {
	directory := t.TempDir()
	now := time.Date(2026, time.August, 18, 11, 30, 0, 0, time.UTC)
	writer, err := newHourlyWriter(directory, filePrefix, defaultRetention, func() time.Time { return now })
	if err != nil {
		t.Fatalf("newHourlyWriter() error = %v", err)
	}

	if _, err := writer.Write([]byte("first\n")); err != nil {
		t.Fatalf("first Write() error = %v", err)
	}
	if _, err := writer.Write([]byte("second\n")); err != nil {
		t.Fatalf("second Write() error = %v", err)
	}
	now = now.Add(time.Hour)
	if _, err := writer.Write([]byte("third\n")); err != nil {
		t.Fatalf("rotating Write() error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	firstPath := filepath.Join(directory, filePrefix+now.Add(-time.Hour).Format(fileTimeLayout)+".log")
	firstData, err := os.ReadFile(firstPath)
	if err != nil {
		t.Fatalf("ReadFile(first) error = %v", err)
	}
	if string(firstData) != "first\nsecond\n" {
		t.Fatalf("first file = %q", firstData)
	}

	secondPath := filepath.Join(directory, filePrefix+now.Format(fileTimeLayout)+".log")
	secondData, err := os.ReadFile(secondPath)
	if err != nil {
		t.Fatalf("ReadFile(second) error = %v", err)
	}
	if string(secondData) != "third\n" {
		t.Fatalf("second file = %q", secondData)
	}
}

func TestHourlyWriterRemovesExpiredFiles(t *testing.T) {
	directory := t.TempDir()
	now := time.Date(2026, time.August, 18, 11, 0, 0, 0, time.UTC)
	oldName := filePrefix + now.Add(-defaultRetention-time.Hour).Format(fileTimeLayout) + ".log"
	recentName := filePrefix + now.Add(-defaultRetention+time.Hour).Format(fileTimeLayout) + ".log"
	foreignName := "unrelated.log"
	for _, name := range []string{oldName, recentName, foreignName} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("log"), 0o640); err != nil {
			t.Fatalf("WriteFile(%q) error = %v", name, err)
		}
	}

	writer, err := newHourlyWriter(directory, filePrefix, defaultRetention, func() time.Time { return now })
	if err != nil {
		t.Fatalf("newHourlyWriter() error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(directory, oldName)); !os.IsNotExist(err) {
		t.Fatalf("expired file still exists, Stat() error = %v", err)
	}
	for _, name := range []string{recentName, foreignName} {
		if _, err := os.Stat(filepath.Join(directory, name)); err != nil {
			t.Fatalf("retained file %q: Stat() error = %v", name, err)
		}
	}
}

func TestLoggerWritesConcurrentlyWithoutCorruptingJSON(t *testing.T) {
	directory := t.TempDir()
	now := time.Date(2026, time.August, 18, 11, 30, 0, 0, time.UTC)
	logger, err := newLogger(directory, &bytes.Buffer{}, func() time.Time { return now })
	if err != nil {
		t.Fatalf("newLogger() error = %v", err)
	}

	const records = 100
	var waitGroup sync.WaitGroup
	for index := range records {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			logger.Info("concurrent record", "index", index)
		}()
	}
	waitGroup.Wait()
	if err := logger.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	path := filepath.Join(directory, filePrefix+now.Format(fileTimeLayout)+".log")
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer file.Close()

	seen := make(map[int]bool, records)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var record map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatalf("invalid JSON line %q: %v", scanner.Text(), err)
		}
		index, ok := record["index"].(float64)
		if !ok {
			t.Fatalf("record has no numeric index: %#v", record)
		}
		seen[int(index)] = true
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(seen) != records {
		t.Fatalf("read %d unique records, want %d", len(seen), records)
	}
}

func TestHourlyWriterUsesExpectedFilename(t *testing.T) {
	now := time.Date(2026, time.August, 18, 11, 0, 0, 0, time.FixedZone("test", 4*60*60))
	writer, err := newHourlyWriter(t.TempDir(), filePrefix, defaultRetention, func() time.Time { return now })
	if err != nil {
		t.Fatalf("newHourlyWriter() error = %v", err)
	}
	defer writer.Close()

	if got, want := writer.filename(writer.currentHour()), "athena-bot-2026-08-18-07.log"; got != want {
		t.Fatalf("filename = %q, want %q", got, want)
	}
}

func BenchmarkHourlyWriter(b *testing.B) {
	directory := b.TempDir()
	now := time.Date(2026, time.August, 18, 11, 0, 0, 0, time.UTC)
	writer, err := newHourlyWriter(directory, filePrefix, defaultRetention, func() time.Time { return now })
	if err != nil {
		b.Fatalf("newHourlyWriter() error = %v", err)
	}
	b.Cleanup(func() { _ = writer.Close() })

	data := []byte(strings.Repeat("x", 128) + "\n")
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := writer.Write(data); err != nil {
			b.Fatal(fmt.Errorf("Write() error = %w", err))
		}
	}
}
