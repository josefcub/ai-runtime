package log

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewLogger(t *testing.T) {
	dir := t.TempDir()

	l, err := New(dir, InfoLevel)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer l.Close()

	if l.file == nil {
		t.Error("expected file to be non-nil")
	}

	// Verify log file was created
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	if len(files) != 1 || files[0].Name() != "harness.log" {
		t.Errorf("expected harness.log file, got %v", files)
	}
}

func TestLoggerInfo(t *testing.T) {
	dir := t.TempDir()

	l, err := New(dir, InfoLevel)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer l.Close()

	l.Info("hello, world", "channel", "test:123")

	data, err := os.ReadFile(filepath.Join(dir, "harness.log"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	line := strings.TrimSpace(string(data))

	// Check level
	if !strings.Contains(line, `level=info`) {
		t.Errorf("expected level=info, got: %s", line)
	}

	// Check message
	if !strings.Contains(line, `msg="hello, world"`) {
		t.Errorf("expected msg=\"hello, world\", got: %s", line)
	}

	// Check key-value (unquoted per spec)
	if !strings.Contains(line, `channel=test:123`) {
		t.Errorf("expected channel=test:123, got: %s", line)
	}

	// Check timestamp is valid (starts with RFC3339)
	ts := strings.Fields(line)[0]
	if _, err := time.Parse(time.RFC3339, ts); err != nil {
		t.Errorf("invalid timestamp %q: %v", ts, err)
	}
}

func TestLoggerLevelsFilter(t *testing.T) {
	dir := t.TempDir()

	l, err := New(dir, WarnLevel)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer l.Close()

	l.Debug("debug message")
	l.Info("info message")
	l.Warn("warn message")
	l.Error("error message")

	data, err := os.ReadFile(filepath.Join(dir, "harness.log"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	lines := strings.TrimSpace(string(data))

	// Debug and Info should be filtered out
	if strings.Contains(lines, "debug message") {
		t.Error("debug message should be filtered out")
	}
	if strings.Contains(lines, "info message") {
		t.Error("info message should be filtered out")
	}

	// Warn and Error should be logged
	if !strings.Contains(lines, "warn message") {
		t.Error("warn message should be logged")
	}
	if !strings.Contains(lines, "error message") {
		t.Error("error message should be logged")
	}
}

func TestLoggerDebugLevel(t *testing.T) {
	dir := t.TempDir()

	l, err := New(dir, DebugLevel)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer l.Close()

	l.Debug("debug message")
	l.Info("info message")
	l.Warn("warn message")
	l.Error("error message")

	data, err := os.ReadFile(filepath.Join(dir, "harness.log"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	lines := strings.TrimSpace(string(data))

	// All levels should be logged
	for _, msg := range []string{"debug message", "info message", "warn message", "error message"} {
		if !strings.Contains(lines, msg) {
			t.Errorf("expected %q to be logged", msg)
		}
	}
}

func TestLoggerErrorLevelOnly(t *testing.T) {
	dir := t.TempDir()

	l, err := New(dir, ErrorLevel)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer l.Close()

	l.Debug("debug message")
	l.Info("info message")
	l.Warn("warn message")
	l.Error("error message")

	data, err := os.ReadFile(filepath.Join(dir, "harness.log"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	lines := strings.TrimSpace(string(data))

	// Only error should be logged
	if strings.Contains(lines, "debug message") {
		t.Error("debug message should be filtered out")
	}
	if strings.Contains(lines, "info message") {
		t.Error("info message should be filtered out")
	}
	if strings.Contains(lines, "warn message") {
		t.Error("warn message should be filtered out")
	}
	if !strings.Contains(lines, "error message") {
		t.Error("error message should be logged")
	}
}

func TestLoggerWithSource(t *testing.T) {
	dir := t.TempDir()

	l, err := New(dir, InfoLevel)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer l.Close()

	srcLogger := l.WithSource("plugin.webhook")
	srcLogger.Info("webhook message received", "channel", "slack:abc123")

	data, err := os.ReadFile(filepath.Join(dir, "harness.log"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	line := strings.TrimSpace(string(data))

	if !strings.Contains(line, `src=plugin.webhook`) {
		t.Errorf("expected src=plugin.webhook, got: %s", line)
	}
}

func TestLoggerMultipleKVs(t *testing.T) {
	dir := t.TempDir()

	l, err := New(dir, InfoLevel)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer l.Close()

	l.Info("test", "key1", "val1", "key2", "val2", "key3", "val3")

	data, err := os.ReadFile(filepath.Join(dir, "harness.log"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	line := strings.TrimSpace(string(data))

	for _, kv := range []string{`key1=val1`, `key2=val2`, `key3=val3`} {
		if !strings.Contains(line, kv) {
			t.Errorf("expected %s in log, got: %s", kv, line)
		}
	}
}

func TestLoggerNoKVs(t *testing.T) {
	dir := t.TempDir()

	l, err := New(dir, InfoLevel)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer l.Close()

	l.Info("simple message")

	data, err := os.ReadFile(filepath.Join(dir, "harness.log"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	line := strings.TrimSpace(string(data))

	expected := `level=info msg="simple message"`
	if !strings.Contains(line, expected) {
		t.Errorf("expected %s, got: %s", expected, line)
	}
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected Level
	}{
		{"debug", DebugLevel},
		{"info", InfoLevel},
		{"warn", WarnLevel},
		{"error", ErrorLevel},
		{"DEBUG", DebugLevel},
		{"INFO", InfoLevel},
		{"unknown", InfoLevel},
		{"", InfoLevel},
	}

	for _, tt := range tests {
		if got := ParseLevel(tt.input); got != tt.expected {
			t.Errorf("ParseLevel(%q) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}

func TestLevelString(t *testing.T) {
	tests := []Level{DebugLevel, InfoLevel, WarnLevel, ErrorLevel}
	expected := []string{"debug", "info", "warn", "error"}

	for i, level := range tests {
		if got := level.String(); got != expected[i] {
			t.Errorf("Level(%d).String() = %q, want %q", i, got, expected[i])
		}
	}
}

func TestLoggerInvalidDir(t *testing.T) {
	// Use an invalid path to test error handling
	_, err := New("/nonexistent/impossible/path/that/cannot/exist", InfoLevel)
	if err == nil {
		t.Error("expected error for invalid directory, got nil")
	}
}

func TestLoggerAppendMode(t *testing.T) {
	dir := t.TempDir()

	l, err := New(dir, InfoLevel)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	l.Info("first message")
	l.Info("second message")
	l.Info("third message")
	l.Close()

	// Re-open the logger (simulating restart)
	l2, err := New(dir, InfoLevel)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer l2.Close()

	l2.Info("fourth message")

	data, err := os.ReadFile(filepath.Join(dir, "harness.log"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 4 {
		t.Errorf("expected 4 log lines, got %d: %v", len(lines), lines)
	}
}


