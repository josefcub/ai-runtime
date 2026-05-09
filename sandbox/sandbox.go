package sandbox

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ResolveWorkingDir canonicalizes the config-provided working directory.
// It resolves to an absolute path and evaluates all symlinks.
// If EvalSymlinks fails (e.g. directory doesn't exist yet), it falls back to filepath.Abs.
func ResolveWorkingDir(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolve working dir: %w", err)
	}

	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		// Fall back to absolute path if symlinks can't be resolved
		return abs, nil
	}

	return resolved, nil
}

// ResolvePath validates a requested path against the sandbox working directory.
// It enforces the five-step process from SYSTEM.md §8.2:
//   1. Reject absolute paths
//   2. Join + absolute
//   3. Resolve symlinks (fallback if file doesn't exist yet)
//   4. Normalize workingDir with Abs (not EvalSymlinks)
//   5. Prefix check with separator
//
// Returns the resolved safe path, or an error if the path escapes the sandbox.
func ResolvePath(workingDir, reqPath string) (string, error) {
	// Step 1: Reject absolute paths
	if filepath.IsAbs(reqPath) {
		return "", fmt.Errorf("access denied")
	}

	// Step 2: Join with workingDir and make absolute
	candidate, err := filepath.Abs(filepath.Join(workingDir, reqPath))
	if err != nil {
		return "", fmt.Errorf("access denied")
	}

	// Step 3: Resolve symlinks in candidate
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		// File may not exist yet (for new file creation).
		// Try resolving the parent directory's symlinks instead.
		parent := filepath.Dir(candidate)
		resolvedParent, parentErr := filepath.EvalSymlinks(parent)
		if parentErr != nil {
			resolved = candidate
		} else {
			resolved = filepath.Join(resolvedParent, filepath.Base(candidate))
		}
	}

	// Step 4: Normalize workingDir
	// Use Abs as the spec says, but also try EvalSymlinks to handle
	// platforms where Abs and EvalSymlinks disagree on parent dirs (e.g. macOS /var vs /private/var).
	normalizedWD, err := filepath.Abs(workingDir)
	if err != nil {
		return "", fmt.Errorf("access denied")
	}
	resolvedWD, wdErr := filepath.EvalSymlinks(normalizedWD)

	// Step 5: Prefix check with separator to prevent false positives
	sep := string(filepath.Separator)

	// Helper to check if resolved path is inside a given working dir
	isInside := func(wd string) bool {
		return resolved == wd || strings.HasPrefix(resolved+sep, wd+sep)
	}

	// Check against the Abs-normalized working dir
	if isInside(normalizedWD) {
		return resolved, nil
	}
	// Also check against the EvalSymlinks-resolved working dir
	if wdErr == nil && isInside(resolvedWD) {
		return resolved, nil
	}

	return "", fmt.Errorf("access denied")
}
