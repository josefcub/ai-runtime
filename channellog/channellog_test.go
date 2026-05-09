package channellog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLogUser(t *testing.T) {
	dir := t.TempDir()
	logger := New(dir)

	err := logger.LogUser("test-channel", "hello world")
	if err != nil {
		t.Fatalf("LogUser failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "test-channel.log"))
	if err != nil {
		t.Fatalf("read log: %v", err)
	}

	line := strings.TrimSpace(string(data))
	if !strings.Contains(line, `"role":"user"`) {
		t.Errorf("expected role=user, got: %s", line)
	}
	if !strings.Contains(line, `"action":"message"`) {
		t.Errorf("expected action=message, got: %s", line)
	}
	if !strings.Contains(line, `"message":"hello world"`) {
		t.Errorf("expected message content, got: %s", line)
	}
}

func TestLogTool(t *testing.T) {
	dir := t.TempDir()
	logger := New(dir)

	err := logger.LogTool("test-channel", "view")
	if err != nil {
		t.Fatalf("LogTool failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "test-channel.log"))
	if err != nil {
		t.Fatalf("read log: %v", err)
	}

	line := strings.TrimSpace(string(data))
	if !strings.Contains(line, `"role":"assistant"`) {
		t.Errorf("expected role=assistant, got: %s", line)
	}
	if !strings.Contains(line, `"action":"tool"`) {
		t.Errorf("expected action=tool, got: %s", line)
	}
	if !strings.Contains(line, `"tool":"view"`) {
		t.Errorf("expected tool=view, got: %s", line)
	}
}

func TestLogAssistant(t *testing.T) {
	dir := t.TempDir()
	logger := New(dir)

	err := logger.LogAssistant("test-channel", "The answer is 42.")
	if err != nil {
		t.Fatalf("LogAssistant failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "test-channel.log"))
	if err != nil {
		t.Fatalf("read log: %v", err)
	}

	line := strings.TrimSpace(string(data))
	if !strings.Contains(line, `"role":"assistant"`) {
		t.Errorf("expected role=assistant, got: %s", line)
	}
	if !strings.Contains(line, `"action":"message"`) {
		t.Errorf("expected action=message, got: %s", line)
	}
	if !strings.Contains(line, `"message":"The answer is 42."`) {
		t.Errorf("expected message content, got: %s", line)
	}
}

func TestNewlinesEscaped(t *testing.T) {
	dir := t.TempDir()
	logger := New(dir)

	err := logger.LogUser("test", "line one\nline two\nline three")
	if err != nil {
		t.Fatalf("LogUser failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "test.log"))
	if err != nil {
		t.Fatalf("read log: %v", err)
	}

	// Should be exactly one line (JSON escaping handles newlines)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Errorf("expected 1 line, got %d: %q", len(lines), string(data))
	}

	// Verify the escaped newlines are in the JSON
	if !strings.Contains(string(data), `\n`) {
		t.Errorf("expected escaped newlines in JSON, got: %s", string(data))
	}
}

func TestQuotesEscaped(t *testing.T) {
	dir := t.TempDir()
	logger := New(dir)

	err := logger.LogUser("test", `he said "hello"`)
	if err != nil {
		t.Fatalf("LogUser failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "test.log"))
	if err != nil {
		t.Fatalf("read log: %v", err)
	}

	// Verify quotes are escaped in JSON
	if !strings.Contains(string(data), `\"`) {
		t.Errorf("expected escaped quotes in JSON, got: %s", string(data))
	}
}

func TestAppendMultipleEntries(t *testing.T) {
	dir := t.TempDir()
	logger := New(dir)

	logger.LogUser("test", "first message")
	logger.LogAssistant("test", "first response")
	logger.LogTool("test", "echo")
	logger.LogAssistant("test", "second response")

	data, err := os.ReadFile(filepath.Join(dir, "test.log"))
	if err != nil {
		t.Fatalf("read log: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 4 {
		t.Errorf("expected 4 lines, got %d", len(lines))
	}
}

func TestNilLogger(t *testing.T) {
	var logger *Logger

	// Nil logger should not panic
	err := logger.LogUser("test", "hello")
	if err != nil {
		t.Errorf("nil logger should not error, got: %v", err)
	}
}

func TestEmptyDirDisablesLogging(t *testing.T) {
	logger := New("")
	if logger != nil {
		t.Error("expected nil logger for empty dir")
	}

	logger2 := New("   ")
	if logger2 != nil {
		t.Error("expected nil logger for whitespace dir")
	}
}

func TestSanitizeChannelID(t *testing.T) {
	dir := t.TempDir()
	logger := New(dir)

	// Channel IDs with path separators should be sanitized
	err := logger.LogUser("slack/abc/123", "test")
	if err != nil {
		t.Fatalf("LogUser failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "slack_abc_123.log"))
	if err != nil {
		t.Fatalf("read log: %v", err)
	}

	if !strings.Contains(string(data), "test") {
		t.Errorf("expected 'test' in log, got: %s", string(data))
	}
}

func TestDirectoryCreatedAutomatically(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "logs")
	logger := New(dir)

	err := logger.LogUser("test", "hello")
	if err != nil {
		t.Fatalf("LogUser failed: %v", err)
	}

	// Verify directory was created
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Error("expected directory to be created")
	}
}
