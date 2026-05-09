package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestRegistry(t *testing.T) (*Registry, string) {
	t.Helper()
	dir, err := os.MkdirTemp("", "file-tools-test-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	reg := New(dir)
	RegisterFileTools(reg)
	return reg, dir
}

func TestViewFile(t *testing.T) {
	reg, dir := newTestRegistry(t)

	// Create a test file
	content := "line one\nline two\nline three\n"
	path := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := reg.Dispatch("view", `{"path":"test.txt"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lines := strings.Split(result, "\n")
	if len(lines) != 3 {
		t.Errorf("expected 3 lines, got %d: %v", len(lines), lines)
	}

	if lines[0] != "line one" {
		t.Errorf("expected 'line one', got %q", lines[0])
	}
}

func TestViewFile_LineRange(t *testing.T) {
	reg, dir := newTestRegistry(t)

	content := "alpha\nbeta\ngamma\ndelta\n"
	path := filepath.Join(dir, "range.txt")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Get lines 1-3 (beta and gamma)
	result, err := reg.Dispatch("view", `{"path":"range.txt","start_line":1,"end_line":3}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lines := strings.Split(result, "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 lines, got %d: %v", len(lines), lines)
	}
	if lines[0] != "beta" {
		t.Errorf("expected 'beta', got %q", lines[0])
	}
	if lines[1] != "gamma" {
		t.Errorf("expected 'gamma', got %q", lines[1])
	}
}

func TestViewFile_SandboxEscape(t *testing.T) {
	reg, _ := newTestRegistry(t)

	_, err := reg.Dispatch("view", `{"path":"/etc/passwd"}`)
	if err == nil {
		t.Fatal("expected sandbox escape error")
	}
}

func TestViewFile_Traversal(t *testing.T) {
	reg, _ := newTestRegistry(t)

	_, err := reg.Dispatch("view", `{"path":"../../../etc/passwd"}`)
	if err == nil {
		t.Fatal("expected traversal error")
	}
}

func TestCreateFile(t *testing.T) {
	reg, dir := newTestRegistry(t)

	result, err := reg.Dispatch("write", `{"path":"new.txt","content":"hello world"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(result, "File created") {
		t.Errorf("expected 'File created' in result, got: %s", result)
	}

	// Verify file was created
	data, err := os.ReadFile(filepath.Join(dir, "new.txt"))
	if err != nil {
		t.Fatalf("read created file: %v", err)
	}
	if string(data) != "hello world" {
		t.Errorf("expected 'hello world', got %q", string(data))
	}
}

func TestCreateFile_NestedDir(t *testing.T) {
	reg, dir := newTestRegistry(t)

	result, err := reg.Dispatch("write", `{"path":"sub/nested/file.txt","content":"nested content"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(result, "File created") {
		t.Errorf("expected 'File created' in result, got: %s", result)
	}

	// Verify nested file was created
	data, err := os.ReadFile(filepath.Join(dir, "sub", "nested", "file.txt"))
	if err != nil {
		t.Fatalf("read nested file: %v", err)
	}
	if string(data) != "nested content" {
		t.Errorf("expected 'nested content', got %q", string(data))
	}
}

func TestAppendToFile(t *testing.T) {
	reg, dir := newTestRegistry(t)

	// Create initial file
	path := filepath.Join(dir, "append.txt")
	if err := os.WriteFile(path, []byte("initial\n"), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := reg.Dispatch("append", `{"path":"append.txt","content":"appended"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(result, "Appended") {
		t.Errorf("expected 'Appended' in result, got: %s", result)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "initial\nappended" {
		t.Errorf("expected 'initial\\nappended', got %q", string(data))
	}
}

func TestListFiles(t *testing.T) {
	reg, dir := newTestRegistry(t)

	// Create some files
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b"), 0644)
	os.Mkdir(filepath.Join(dir, "subdir"), 0755)

	result, err := reg.Dispatch("ls", `{"path":"."}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lines := strings.Split(result, "\n")
	if len(lines) < 3 {
		t.Errorf("expected at least 3 entries, got %d: %v", len(lines), lines)
	}

	// Check for expected entries
	found := make(map[string]bool)
	for _, line := range lines {
		found[line] = true
	}
	if !found["a.txt"] {
		t.Error("expected 'a.txt' in listing")
	}
	if !found["b.txt"] {
		t.Error("expected 'b.txt' in listing")
	}
	if !found["subdir/"] {
		t.Error("expected 'subdir/' in listing")
	}
}

func TestEditFile(t *testing.T) {
	reg, dir := newTestRegistry(t)

	// Create a test file
	content := "foo bar baz\nqux quux\n"
	path := filepath.Join(dir, "edit.txt")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := reg.Dispatch("edit", `{"path":"edit.txt","old_text":"bar","new_text":"BAR"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(result, "Edited") {
		t.Errorf("expected 'Edited' in result, got: %s", result)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "foo BAR baz\nqux quux\n" {
		t.Errorf("expected 'foo BAR baz\\nqux quux\\n', got %q", string(data))
	}
}

func TestEditFile_NotFound(t *testing.T) {
	reg, dir := newTestRegistry(t)

	path := filepath.Join(dir, "edit2.txt")
	if err := os.WriteFile(path, []byte("hello world"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := reg.Dispatch("edit", `{"path":"edit2.txt","old_text":"not here","new_text":"replacement"}`)
	if err == nil {
		t.Fatal("expected error for text not found")
	}
}

func TestGrep_PlainText(t *testing.T) {
	reg, dir := newTestRegistry(t)

	// Create test file
	content := "hello world\nfoo bar\nhello again\n"
	path := filepath.Join(dir, "search.txt")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := reg.Dispatch("grep", `{"pattern":"hello","path":"search.txt"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lines := strings.Split(result, "\n")
	// Should match 2 lines (line 1 and line 3)
	if len(lines) != 2 {
		t.Errorf("expected 2 matches, got %d: %v", len(lines), lines)
	}

	// First match should be on line 1
	if !strings.Contains(lines[0], "search.txt:1:hello world") {
		t.Errorf("unexpected first match: %s", lines[0])
	}
}

func TestGrep_Regex(t *testing.T) {
	reg, dir := newTestRegistry(t)

	content := "foo123bar\nhello\nfoo456world\n"
	path := filepath.Join(dir, "regex.txt")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := reg.Dispatch("grep", `{"pattern":"foo\\d+","path":"regex.txt","use_regex":true}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lines := strings.Split(result, "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 regex matches, got %d: %v", len(lines), lines)
	}
}

func TestGrep_Directory(t *testing.T) {
	reg, dir := newTestRegistry(t)

	// Create files in a subdirectory
	sub := filepath.Join(dir, "src")
	os.Mkdir(sub, 0755)
	os.WriteFile(filepath.Join(sub, "a.txt"), []byte("needle in haystack\nother line\n"), 0644)
	os.WriteFile(filepath.Join(sub, "b.txt"), []byte("no match here\nneedle again\n"), 0644)

	result, err := reg.Dispatch("grep", `{"pattern":"needle","path":"src"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have 2 matches total (one in each file)
	if !strings.Contains(result, "needle in haystack") {
		t.Error("expected match in a.txt")
	}
	if !strings.Contains(result, "needle again") {
		t.Error("expected match in b.txt")
	}
}

func TestGrep_NoMatches(t *testing.T) {
	reg, dir := newTestRegistry(t)

	path := filepath.Join(dir, "nomatch.txt")
	os.WriteFile(path, []byte("no matches here\n"), 0644)

	result, err := reg.Dispatch("grep", `{"pattern":"xyz","path":"nomatch.txt"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "No matches found." {
		t.Errorf("expected 'No matches found.', got %q", result)
	}
}

func TestViewFile_EmptyFile(t *testing.T) {
	reg, dir := newTestRegistry(t)

	path := filepath.Join(dir, "empty.txt")
	os.WriteFile(path, []byte(""), 0644)

	result, err := reg.Dispatch("view", `{"path":"empty.txt"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty result, got %q", result)
	}
}

func TestCreateFile_SandboxEscape(t *testing.T) {
	reg, _ := newTestRegistry(t)

	_, err := reg.Dispatch("write", `{"path":"/tmp/escape.txt","content":"data"}`)
	if err == nil {
		t.Fatal("expected sandbox escape error")
	}
}

func TestListFiles_SandboxEscape(t *testing.T) {
	reg, _ := newTestRegistry(t)

	_, err := reg.Dispatch("ls", `{"path":"../"}`)
	if err == nil {
		t.Fatal("expected sandbox escape error")
	}
}

func TestGrep_SymlinkNoFollow(t *testing.T) {
	reg, dir := newTestRegistry(t)

	// Create a file outside the sandbox
	outside := filepath.Join(filepath.Dir(dir), "outside-"+filepath.Base(dir))
	os.MkdirAll(outside, 0755)
	defer os.RemoveAll(outside)
	os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("sensitive data\n"), 0644)

	// Create a symlink inside the sandbox pointing outside
	linkDir := filepath.Join(dir, "evil_link")
	if err := os.Symlink(outside, linkDir); err != nil {
		t.Skip("symlinks not supported")
	}

	// Grep on the symlinked directory should NOT follow the symlink
	// (WalkDir skips symlinks, so it won't traverse into it)
	result, err := reg.Dispatch("grep", `{"pattern":"sensitive","path":"evil_link"}`)
	if err != nil {
		// Error is also acceptable (stat may fail on broken symlink)
		return
	}
	// If we got a result, it must not contain the external data
	if strings.Contains(result, "sensitive data") {
		t.Error("grep followed symlink outside sandbox")
	}
}

func TestGrep_InvalidRegex(t *testing.T) {
	reg, dir := newTestRegistry(t)

	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("test"), 0644)

	_, err := reg.Dispatch("grep", `{"pattern":"[invalid","path":"test.txt","use_regex":true}`)
	if err == nil {
		t.Fatal("expected error for invalid regex")
	}
}

func TestGlob_SingleLevel(t *testing.T) {
	reg, dir := newTestRegistry(t)

	os.WriteFile(filepath.Join(dir, "foo.go"), []byte("package foo"), 0644)
	os.WriteFile(filepath.Join(dir, "bar.go"), []byte("package bar"), 0644)
	os.WriteFile(filepath.Join(dir, "baz.txt"), []byte("text"), 0644)
	os.WriteFile(filepath.Join(dir, ".hidden"), []byte("hidden"), 0644)

	result, err := reg.Dispatch("glob", `{"pattern":"*.go"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lines := strings.Split(result, "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 matches, got %d: %v", len(lines), lines)
	}
	if lines[0] != "foo.go" && lines[1] != "foo.go" {
		t.Errorf("expected foo.go in results: %v", lines)
	}
	if lines[0] != "bar.go" && lines[1] != "bar.go" {
		t.Errorf("expected bar.go in results: %v", lines)
	}

	// .hidden should not appear
	if strings.Contains(result, ".hidden") {
		t.Error("hidden file should not be included")
	}
}

func TestGlob_Recursive(t *testing.T) {
	reg, dir := newTestRegistry(t)

	os.MkdirAll(filepath.Join(dir, "src", "pkg"), 0755)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0644)
	os.WriteFile(filepath.Join(dir, "src", "foo.go"), []byte("package foo"), 0644)
	os.WriteFile(filepath.Join(dir, "src", "pkg", "bar.go"), []byte("package bar"), 0644)
	os.WriteFile(filepath.Join(dir, "src", "readme.txt"), []byte("readme"), 0644)

	result, err := reg.Dispatch("glob", `{"pattern":"**/*.go"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lines := strings.Split(result, "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 matches, got %d: %v", len(lines), lines)
	}

	// Check all .go files are present
	found := make(map[string]bool)
	for _, line := range lines {
		found[line] = true
	}
	if !found["main.go"] {
		t.Error("expected main.go in results")
	}
	if !found["src/foo.go"] {
		t.Error("expected src/foo.go in results")
	}
	if !found["src/pkg/bar.go"] {
		t.Error("expected src/pkg/bar.go in results")
	}
}

func TestGlob_WithPath(t *testing.T) {
	reg, dir := newTestRegistry(t)

	os.MkdirAll(filepath.Join(dir, "src", "pkg"), 0755)
	os.WriteFile(filepath.Join(dir, "src", "a.go"), []byte("a"), 0644)
	os.WriteFile(filepath.Join(dir, "src", "pkg", "b.go"), []byte("b"), 0644)
	os.WriteFile(filepath.Join(dir, "src", "pkg", "c.txt"), []byte("c"), 0644)

	result, err := reg.Dispatch("glob", `{"pattern":"*.go","path":"src/pkg"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lines := strings.Split(result, "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 match, got %d: %v", len(lines), lines)
	}
	if lines[0] != "src/pkg/b.go" {
		t.Errorf("expected 'src/pkg/b.go', got %q", lines[0])
	}
}

func TestGlob_NoMatches(t *testing.T) {
	reg, dir := newTestRegistry(t)

	os.WriteFile(filepath.Join(dir, "only.txt"), []byte("text"), 0644)

	result, err := reg.Dispatch("glob", `{"pattern":"*.go"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "No matches found." {
		t.Errorf("expected 'No matches found.', got %q", result)
	}
}

func TestGlob_NotADirectory(t *testing.T) {
	reg, dir := newTestRegistry(t)

	os.WriteFile(filepath.Join(dir, "file.txt"), []byte("text"), 0644)

	_, err := reg.Dispatch("glob", `{"pattern":"*","path":"file.txt"}`)
	if err == nil {
		t.Fatal("expected error when path is not a directory")
	}
}

func TestGlob_MissingPattern(t *testing.T) {
	reg, _ := newTestRegistry(t)

	_, err := reg.Dispatch("glob", `{"path":"."}`)
	if err == nil {
		t.Fatal("expected error for missing pattern")
	}
}
