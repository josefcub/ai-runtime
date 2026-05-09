package channellog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Logger writes per-channel JSONL conversation logs to a configurable directory.
// Each channel gets its own file: `<channel_id>.log`.
// Safe for single-writer-per-channel (one worker per channel).
type Logger struct {
	dir string
}

// Entry is a single JSONL log record.
type Entry struct {
	Timestamp string `json:"timestamp"`
	Role      string `json:"role"`
	Action    string `json:"action"` // "message" or "tool"
	Message   string `json:"message,omitempty"`
	Tool      string `json:"tool,omitempty"`
}

// New creates a Logger that writes to the given directory.
// Returns nil if dir is empty (logging disabled).
func New(dir string) *Logger {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil
	}
	return &Logger{dir: dir}
}

// Log writes a single JSONL entry to the channel's log file.
// The file is opened in append mode, written, and closed each call.
// This keeps the log rotatable and viewable with tail -f.
func (l *Logger) Log(channelID string, entry Entry) error {
	if l == nil {
		return nil
	}

	// Ensure directory exists
	if err := os.MkdirAll(l.dir, 0755); err != nil {
		return fmt.Errorf("create channel log dir: %w", err)
	}

	// Sanitize channel ID for use as filename
	safeID := strings.ReplaceAll(channelID, "/", "_")
	safeID = strings.ReplaceAll(safeID, "\\", "_")
	safeID = strings.ReplaceAll(safeID, "..", "_")
	logPath := filepath.Join(l.dir, safeID+".log")

	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open channel log: %w", err)
	}

	// Use the provided timestamp if set, otherwise current time
	if entry.Timestamp == "" {
		entry.Timestamp = time.Now().UTC().Format(time.RFC3339)
	}

	// Marshal to JSON — encoding/json handles all escaping (newlines, quotes, etc.)
	// so each entry is guaranteed to be a single line.
	data, err := json.Marshal(entry)
	if err != nil {
		f.Close()
		return fmt.Errorf("marshal log entry: %w", err)
	}

	_, err = f.Write(data)
	if err != nil {
		f.Close()
		return fmt.Errorf("write log entry: %w", err)
	}

	_, err = f.WriteString("\n")
	if err != nil {
		f.Close()
		return fmt.Errorf("write newline: %w", err)
	}

	return f.Close()
}

// LogUser logs a user message.
func (l *Logger) LogUser(channelID, message string) error {
	return l.Log(channelID, Entry{
		Role:    "user",
		Action:  "message",
		Message: message,
	})
}

// LogTool logs a tool call (after execution).
func (l *Logger) LogTool(channelID, toolName string) error {
	return l.Log(channelID, Entry{
		Role: "assistant",
		Action: "tool",
		Tool: toolName,
	})
}

// LogAssistant logs an assistant text response.
func (l *Logger) LogAssistant(channelID, message string) error {
	return l.Log(channelID, Entry{
		Role:    "assistant",
		Action:  "message",
		Message: message,
	})
}
