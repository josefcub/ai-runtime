package log

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Level represents a log level.
type Level int

const (
	// DebugLevel is the most verbose level.
	DebugLevel Level = iota
	// InfoLevel is for informational messages.
	InfoLevel
	// WarnLevel is for warning messages.
	WarnLevel
	// ErrorLevel is the least verbose level.
	ErrorLevel
)

// String returns the string representation of a log level.
func (l Level) String() string {
	switch l {
	case DebugLevel:
		return "debug"
	case InfoLevel:
		return "info"
	case WarnLevel:
		return "warn"
	case ErrorLevel:
		return "error"
	default:
		return "unknown"
	}
}

// ParseLevel parses a string into a Level. Returns InfoLevel if the string is not recognized.
func ParseLevel(s string) Level {
	switch strings.ToLower(s) {
	case "debug":
		return DebugLevel
	case "info":
		return InfoLevel
	case "warn":
		return WarnLevel
	case "error":
		return ErrorLevel
	default:
		return InfoLevel
	}
}

// Logger provides structured syslog-friendly logging.
type Logger struct {
	mu   sync.Mutex
	file *os.File
	Level
	src string
}

// New creates a Logger that writes to a file in logDir.
func New(logDir string, level Level) (*Logger, error) {
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, fmt.Errorf("create log dir: %w", err)
	}

	f, err := os.OpenFile(filepath.Join(logDir, "harness.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("open log file: %w", err)
	}

	return &Logger{
		file:  f,
		Level: level,
	}, nil
}

// Close closes the underlying log file.
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		return l.file.Close()
	}
	return nil
}

// Log writes a structured log entry at the given level.
//
// Format: TIMESTAMP level=LEVEL msg="..." key=value ... src=SOURCE
//
// kvs are alternating key/value pairs.
func (l *Logger) Log(level Level, msg string, kvs ...string) {
	if level < l.Level {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	ts := time.Now().Format(time.RFC3339)
	fmt.Fprintf(l.file, "%s level=%s msg=%q", ts, level.String(), msg)

	for i := 0; i+1 < len(kvs); i += 2 {
		fmt.Fprintf(l.file, " %s=%s", kvs[i], kvs[i+1])
	}

	if l.src != "" {
		fmt.Fprintf(l.file, " src=%s", l.src)
	}

	fmt.Fprintln(l.file)
}

// Debug logs a debug-level message.
func (l *Logger) Debug(msg string, kvs ...string) {
	l.Log(DebugLevel, msg, kvs...)
}

// Info logs an info-level message.
func (l *Logger) Info(msg string, kvs ...string) {
	l.Log(InfoLevel, msg, kvs...)
}

// Warn logs a warn-level message.
func (l *Logger) Warn(msg string, kvs ...string) {
	l.Log(WarnLevel, msg, kvs...)
}

// Error logs an error-level message.
func (l *Logger) Error(msg string, kvs ...string) {
	l.Log(ErrorLevel, msg, kvs...)
}

// WithSource returns a copy of the Logger with the given source tag.
func (l *Logger) WithSource(src string) *Logger {
	return &Logger{
		file:  l.file,
		Level: l.Level,
		src:   src,
	}
}
