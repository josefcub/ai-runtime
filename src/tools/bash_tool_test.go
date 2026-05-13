package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBashTool_BasicCommand(t *testing.T) {
	dir := t.TempDir()

	reg := New(dir)
	RegisterBashTools(reg, true, 10*time.Second, 30720, []string{"curl"})

	result, err := reg.Dispatch("bash", `{"command": "echo hello"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "hello") {
		t.Errorf("expected 'hello' in output, got: %q", result)
	}
}

func TestBashTool_WorkingDirectory(t *testing.T) {
	dir := t.TempDir()

	reg := New(dir)
	RegisterBashTools(reg, true, 10*time.Second, 30720, nil)

	// Create a file in working dir
	os.WriteFile(filepath.Join(dir, "test.txt"), []byte("content"), 0644)

	result, err := reg.Dispatch("bash", `{"command": "cat test.txt"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "content") {
		t.Errorf("expected 'content' in output, got: %q", result)
	}
}

func TestBashTool_BannedCommand(t *testing.T) {
	dir := t.TempDir()

	reg := New(dir)
	RegisterBashTools(reg, true, 10*time.Second, 30720, []string{"curl", "wget"})

	_, err := reg.Dispatch("bash", `{"command": "curl http://example.com"}`)
	if err == nil {
		t.Fatal("expected error for banned command curl")
	}
	if !strings.Contains(err.Error(), "not allowed") {
		t.Errorf("expected 'not allowed' error, got: %v", err)
	}

	_, err = reg.Dispatch("bash", `{"command": "wget http://example.com"}`)
	if err == nil {
		t.Fatal("expected error for banned command wget")
	}
}

func TestBashTool_BannedCaseInsensitive(t *testing.T) {
	dir := t.TempDir()

	reg := New(dir)
	RegisterBashTools(reg, true, 10*time.Second, 30720, []string{"curl"})

	// Should be blocked regardless of case
	_, err := reg.Dispatch("bash", `{"command": "CURL http://example.com"}`)
	if err == nil {
		t.Fatal("expected error for CURL (uppercase)")
	}
}

func TestBashTool_Timeout(t *testing.T) {
	dir := t.TempDir()

	reg := New(dir)
	RegisterBashTools(reg, true, 2*time.Second, 30720, nil)

	result, err := reg.Dispatch("bash", `{"command": "sleep 30"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "timed out") {
		t.Errorf("expected timeout message, got: %q", result)
	}
}

func TestBashTool_CustomTimeout(t *testing.T) {
	dir := t.TempDir()

	reg := New(dir)
	RegisterBashTools(reg, true, 60*time.Second, 30720, nil)

	// Override to 2 seconds — sleep 5 should timeout
	result, err := reg.Dispatch("bash", `{"command": "sleep 5", "timeout": 2}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "timed out") {
		t.Errorf("expected timeout message, got: %q", result)
	}
}

func TestBashTool_StderrIncluded(t *testing.T) {
	dir := t.TempDir()

	reg := New(dir)
	RegisterBashTools(reg, true, 10*time.Second, 30720, nil)

	result, err := reg.Dispatch("bash", `{"command": "echo stdout-msg && echo stderr-msg >&2"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "stdout-msg") {
		t.Errorf("expected 'stdout-msg' in output, got: %q", result)
	}
	if !strings.Contains(result, "stderr:") || !strings.Contains(result, "stderr-msg") {
		t.Errorf("expected 'stderr: stderr-msg' in output, got: %q", result)
	}
}

func TestBashTool_OutputTruncation(t *testing.T) {
	dir := t.TempDir()

	// Very small max output
	reg := New(dir)
	RegisterBashTools(reg, true, 10*time.Second, 50, nil)

	// Generate output larger than 50 bytes
	result, err := reg.Dispatch("bash", `{"command": "echo $(printf 'A%.0s' {1..100})"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "[output truncated") {
		t.Errorf("expected truncation notice, got: %q", result)
	}
}

func TestBashTool_EnabledFalse(t *testing.T) {
	dir := t.TempDir()

	reg := New(dir)
	RegisterBashTools(reg, false, 10*time.Second, 30720, nil)

	_, err := reg.Dispatch("bash", `{"command": "echo test"}`)
	if err == nil {
		t.Fatal("expected error for unregistered tool")
	}
	if !strings.Contains(err.Error(), "unknown tool") {
		t.Errorf("expected 'unknown tool' error, got: %v", err)
	}
}

func TestBashTool_MissingCommand(t *testing.T) {
	dir := t.TempDir()

	reg := New(dir)
	RegisterBashTools(reg, true, 10*time.Second, 30720, nil)

	_, err := reg.Dispatch("bash", `{}`)
	if err == nil {
		t.Fatal("expected error for missing command")
	}
}

func TestBashTool_NonZeroExit(t *testing.T) {
	dir := t.TempDir()

	reg := New(dir)
	RegisterBashTools(reg, true, 10*time.Second, 30720, nil)

	// Non-zero exit should return output, not an error
	result, err := reg.Dispatch("bash", `{"command": "echo before-exit && exit 42"}`)
	if err != nil {
		t.Fatalf("unexpected error (non-zero exit is informational): %v", err)
	}
	if !strings.Contains(result, "before-exit") {
		t.Errorf("expected 'before-exit' in output, got: %q", result)
	}
}

func TestIsBanned(t *testing.T) {
	banned := []string{"curl", "wget", "sudo"}

	tests := []struct {
		cmd  string
		want bool
	}{
		{"curl http://example.com", true},
		{"  curl http://example.com", true},
		{"CURL http://example.com", true},
		{"curl", true},
		{"wget -O file http://x", true},
		{"sudo ls", true},
		{"ls -la", false},
		{"echo hello", false},
		{"echo curl", false},  // 'curl' is an argument, not the command
		{"cat | curl", false}, // 'cat' is the first token
		{"", false},
	}

	for _, tt := range tests {
		got := isBanned(tt.cmd, banned)
		if got != tt.want {
			t.Errorf("isBanned(%q) = %v, want %v", tt.cmd, got, tt.want)
		}
	}
}

func TestExtractFirstToken(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"echo hello", "echo"},
		{"  echo hello", "echo"},
		{"echo", "echo"},
		{"echo 'hello world'", "echo"},
		{"echo \"hello world\"", "echo"},
		{"cat > file.txt", "cat"},
		{"ls | grep foo", "ls"},
		{"ls && cat file", "ls"},
		{"./script.sh arg1", "./script.sh"},
		{"", ""},
		{"   ", ""},
		{"go build ./...", "go"},
		{"python3 -c 'print(1)'", "python3"},
	}

	for _, tt := range tests {
		got := extractFirstToken(tt.input)
		if got != tt.want {
			t.Errorf("extractFirstToken(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestTruncateOutput(t *testing.T) {
	// No truncation needed
	short := "hello world"
	if result := truncateOutput(short, 100); result != short {
		t.Errorf("no truncation: got %q, want %q", result, short)
	}

	// Truncation
	long := strings.Repeat("A", 200)
	result := truncateOutput(long, 50)
	if len(result) > 100 {
		t.Errorf("truncation produced too much output (%d bytes)", len(result))
	}
	if !strings.Contains(result, "[output truncated") {
		t.Errorf("expected truncation notice in: %q", result)
	}
}
