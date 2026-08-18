package logging

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	filePrefix       = "athena-bot-"
	fileTimeLayout   = "2006-01-02-15"
	defaultRetention = 7 * 24 * time.Hour
)

type Logger struct {
	*slog.Logger
	fileWriter *hourlyWriter
}

func New(directory string) (*Logger, error) {
	return newLogger(directory, os.Stdout, time.Now)
}

func newLogger(directory string, console io.Writer, now func() time.Time) (*Logger, error) {
	fileWriter, err := newHourlyWriter(directory, filePrefix, defaultRetention, now)
	if err != nil {
		return nil, err
	}

	output := io.MultiWriter(console, fileWriter)
	handler := slog.NewJSONHandler(output, &slog.HandlerOptions{Level: slog.LevelInfo})
	return &Logger{
		Logger:     slog.New(handler),
		fileWriter: fileWriter,
	}, nil
}

func (l *Logger) Close() error {
	if l == nil || l.fileWriter == nil {
		return nil
	}
	return l.fileWriter.Close()
}

type hourlyWriter struct {
	mu        sync.Mutex
	directory string
	prefix    string
	retention time.Duration
	now       func() time.Time
	file      *os.File
	hour      time.Time
	closed    bool
}

func newHourlyWriter(directory, prefix string, retention time.Duration, now func() time.Time) (*hourlyWriter, error) {
	if strings.TrimSpace(directory) == "" {
		return nil, fmt.Errorf("log directory is empty")
	}
	if now == nil {
		now = time.Now
	}
	if err := os.MkdirAll(directory, 0o750); err != nil {
		return nil, fmt.Errorf("create log directory: %w", err)
	}

	writer := &hourlyWriter{
		directory: directory,
		prefix:    prefix,
		retention: retention,
		now:       now,
	}
	if err := writer.rotateLocked(writer.currentHour()); err != nil {
		_ = writer.Close()
		return nil, err
	}
	return writer, nil
}

func (w *hourlyWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return 0, os.ErrClosed
	}

	hour := w.currentHour()
	var rotationErr error
	if w.file == nil || !hour.Equal(w.hour) {
		rotationErr = w.rotateLocked(hour)
		if rotationErr != nil && (w.file == nil || !hour.Equal(w.hour)) {
			return 0, rotationErr
		}
	}
	written, writeErr := w.file.Write(data)
	return written, errors.Join(rotationErr, writeErr)
}

func (w *hourlyWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return nil
	}
	w.closed = true
	if w.file == nil {
		return nil
	}
	return w.file.Close()
}

func (w *hourlyWriter) currentHour() time.Time {
	return w.now().UTC().Truncate(time.Hour)
}

func (w *hourlyWriter) rotateLocked(hour time.Time) error {
	path := filepath.Join(w.directory, w.filename(hour))
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640)
	if err != nil {
		return fmt.Errorf("open hourly log file: %w", err)
	}

	previous := w.file
	w.file = file
	w.hour = hour
	if previous != nil {
		if err := previous.Close(); err != nil {
			return fmt.Errorf("close previous log file: %w", err)
		}
	}
	if err := w.removeExpiredLocked(hour); err != nil {
		return err
	}
	return nil
}

func (w *hourlyWriter) removeExpiredLocked(currentHour time.Time) error {
	if w.retention <= 0 {
		return nil
	}

	entries, err := os.ReadDir(w.directory)
	if err != nil {
		return fmt.Errorf("read log directory: %w", err)
	}
	cutoff := currentHour.Add(-w.retention)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		hour, ok := w.hourFromFilename(entry.Name())
		if !ok || !hour.Before(cutoff) {
			continue
		}
		if err := os.Remove(filepath.Join(w.directory, entry.Name())); err != nil {
			return fmt.Errorf("remove expired log file %q: %w", entry.Name(), err)
		}
	}
	return nil
}

func (w *hourlyWriter) filename(hour time.Time) string {
	return w.prefix + hour.Format(fileTimeLayout) + ".log"
}

func (w *hourlyWriter) hourFromFilename(name string) (time.Time, bool) {
	if !strings.HasPrefix(name, w.prefix) || !strings.HasSuffix(name, ".log") {
		return time.Time{}, false
	}
	value := strings.TrimSuffix(strings.TrimPrefix(name, w.prefix), ".log")
	hour, err := time.ParseInLocation(fileTimeLayout, value, time.UTC)
	return hour, err == nil
}
