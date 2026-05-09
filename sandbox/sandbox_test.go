package sandbox

import (
	"os"
	"path/filepath"
	"testing"
)

func tempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "sandbox-test-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func TestResolveWorkingDir(t *testing.T) {
	dir := tempDir(t)

	// Subdirectory
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0755); err != nil {
		t.Fatal(err)
	}

	resolved, err := ResolveWorkingDir(sub)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should be absolute
	if !filepath.IsAbs(resolved) {
		t.Error("expected absolute path")
	}

	// Relative path should become absolute
	resolved2, err := ResolveWorkingDir("./sandbox")
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(resolved2) {
		t.Error("expected absolute path from relative input")
	}
}

func TestResolveWorkingDir_NonExistent(t *testing.T) {
	// Non-existent directory should fall back to Abs (no EvalSymlinks)
	resolved, err := ResolveWorkingDir("/tmp/does-not-exist-12345")
	if err != nil {
		t.Fatalf("expected fallback to Abs, got error: %v", err)
	}
	if resolved != "/tmp/does-not-exist-12345" {
		t.Errorf("expected /tmp/does-not-exist-12345, got %s", resolved)
	}
}

func TestResolveWorkingDir_Symlink(t *testing.T) {
	dir := tempDir(t)
	realDir := filepath.Join(dir, "real")
	linkDir := filepath.Join(dir, "link")

	if err := os.Mkdir(realDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realDir, linkDir); err != nil {
		// Skip on systems that don't support symlinks
		t.Skip("symlinks not supported")
	}

	resolved, err := ResolveWorkingDir(linkDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should resolve to the real directory, not the symlink
	if resolved == linkDir {
		t.Error("expected symlink to be resolved to real path")
	}
}

func TestResolvePath_ValidRelative(t *testing.T) {
	wd := tempDir(t)

	resolved, err := ResolvePath(wd, "foo.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resolvedWD, _ := filepath.EvalSymlinks(wd)
	expected := filepath.Join(resolvedWD, "foo.txt")
	if resolved != expected {
		t.Errorf("expected %s, got %s", expected, resolved)
	}
}

func TestResolvePath_ValidSubdir(t *testing.T) {
	wd := tempDir(t)
	sub := filepath.Join(wd, "sub")
	if err := os.Mkdir(sub, 0755); err != nil {
		t.Fatal(err)
	}

	resolved, err := ResolvePath(wd, "sub/bar.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resolvedWD, _ := filepath.EvalSymlinks(wd)
	expected := filepath.Join(resolvedWD, "sub", "bar.txt")
	if resolved != expected {
		t.Errorf("expected %s, got %s", expected, resolved)
	}
}

func TestResolvePath_RejectAbsolute(t *testing.T) {
	wd := tempDir(t)

	_, err := ResolvePath(wd, "/etc/passwd")
	if err == nil {
		t.Fatal("expected error for absolute path")
	}
	if err.Error() != "access denied" {
		t.Errorf("expected 'access denied', got %v", err)
	}
}

func TestResolvePath_RejectTraversal(t *testing.T) {
	wd := tempDir(t)

	_, err := ResolvePath(wd, "../escape.txt")
	if err == nil {
		t.Fatal("expected error for .. traversal")
	}
	if err.Error() != "access denied" {
		t.Errorf("expected 'access denied', got %v", err)
	}
}

func TestResolvePath_RejectDeepTraversal(t *testing.T) {
	wd := tempDir(t)

	_, err := ResolvePath(wd, "sub/../../escape.txt")
	if err == nil {
		t.Fatal("expected error for deep .. traversal")
	}
	if err.Error() != "access denied" {
		t.Errorf("expected 'access denied', got %v", err)
	}
}

func TestResolvePath_SymlinkEscape(t *testing.T) {
	wd := tempDir(t)
	outside := filepath.Join(filepath.Dir(wd), "outside-"+filepath.Base(wd))
	if err := os.Mkdir(outside, 0755); err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(outside)

	// Create symlink inside working dir pointing outside
	linkPath := filepath.Join(wd, "evil_link")
	if err := os.Symlink(outside, linkPath); err != nil {
		t.Skip("symlinks not supported")
	}

	_, err := ResolvePath(wd, "evil_link/secret.txt")
	if err == nil {
		t.Fatal("expected error for symlink escape")
	}
	if err.Error() != "access denied" {
		t.Errorf("expected 'access denied', got %v", err)
	}
}

func TestResolvePath_PrefixWithSeparator(t *testing.T) {
	// Create directories: /tmp/sandbox-test-X and /tmp/sandbox-test-Xdir
	// to test that "sandbox-test-X" doesn't match "sandbox-test-Xdir"
	parent := filepath.Dir(tempDir(t))
	wd := filepath.Join(parent, "sandbox-test-prefix-wd")
	other := filepath.Join(parent, "sandbox-test-prefix-wddir")
	if err := os.Mkdir(wd, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(other, 0755); err != nil {
		t.Fatal(err)
	}
	defer func() {
		os.RemoveAll(wd)
		os.RemoveAll(other)
	}()

	// Symlink from wd to other directory
	linkPath := filepath.Join(wd, "prefix_link")
	if err := os.Symlink(other, linkPath); err != nil {
		t.Skip("symlinks not supported")
	}

	_, err := ResolvePath(wd, "prefix_link/file.txt")
	if err == nil {
		t.Fatal("expected error — wddir is not inside wd despite prefix match")
	}
}

func TestResolvePath_CurrentDir(t *testing.T) {
	wd := tempDir(t)

	resolved, err := ResolvePath(wd, ".")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The resolved path should be inside the working directory.
	// On macOS, EvalSymlinks may resolve /var to /private/var, so we check
	// that the resolved path starts with the canonical working dir.
	resolvedWD, _ := filepath.EvalSymlinks(wd)
	if resolved != resolvedWD {
		t.Errorf("expected %s, got %s", resolvedWD, resolved)
	}
}

func TestResolvePath_NestedSubdirs(t *testing.T) {
	wd := tempDir(t)
	nested := filepath.Join(wd, "a", "b", "c")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatal(err)
	}

	resolved, err := ResolvePath(wd, "a/b/c/deep.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resolvedWD, _ := filepath.EvalSymlinks(wd)
	expected := filepath.Join(resolvedWD, "a", "b", "c", "deep.txt")
	if resolved != expected {
		t.Errorf("expected %s, got %s", expected, resolved)
	}
}
