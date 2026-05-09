package tools

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/agent-project/harness/sandbox"
)

// RegisterFileTools registers all built-in file tools on the given registry.
func RegisterFileTools(reg *Registry) {
	workingDir := reg.workingDir

	// view
	reg.Register("view",
		"View a file, optionally returning a range of lines.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":       map[string]interface{}{"type": "string", "description": "Relative path from working directory."},
				"start_line": map[string]interface{}{"type": "integer", "description": "0-based start line (optional)."},
				"end_line":   map[string]interface{}{"type": "integer", "description": "0-based end line, exclusive (optional)."},
			},
		},
		func(args map[string]interface{}) (string, error) {
			return toolViewFile(workingDir, args)
		})

	// write
	reg.Register("write",
		"Create or overwrite a file in the working directory.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":    map[string]interface{}{"type": "string"},
				"content": map[string]interface{}{"type": "string"},
			},
		},
		func(args map[string]interface{}) (string, error) {
			return toolCreateFile(workingDir, args)
		})

	// append
	reg.Register("append",
		"Appends text to the end of a file in the working directory. Creates the file if it doesn't exist.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":    map[string]interface{}{"type": "string"},
				"content": map[string]interface{}{"type": "string"},
			},
		},
		func(args map[string]interface{}) (string, error) {
			return toolAppendToFile(workingDir, args)
		})

	// ls
	reg.Register("ls",
		"List files in a directory. Defaults to working directory if no path is given.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{"type": "string", "description": "Relative path (optional; defaults to working directory)."},
			},
		},
		func(args map[string]interface{}) (string, error) {
			return toolListFiles(workingDir, args)
		})

	// edit
	reg.Register("edit",
		"Edit a file by replacing the first occurrence of old_text with new_text.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":     map[string]interface{}{"type": "string"},
				"old_text": map[string]interface{}{"type": "string"},
				"new_text": map[string]interface{}{"type": "string"},
			},
		},
		func(args map[string]interface{}) (string, error) {
			return toolEditFile(workingDir, args)
		})

	// grep
	reg.Register("grep",
		"Search for a pattern in files. Supports plain text and regex.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"pattern":   map[string]interface{}{"type": "string"},
				"path":      map[string]interface{}{"type": "string", "description": "File or directory to search (relative)."},
				"use_regex": map[string]interface{}{"type": "boolean", "default": false},
			},
		},
		func(args map[string]interface{}) (string, error) {
			return toolGrep(workingDir, args)
		})

	// glob
	reg.Register("glob",
		"Find files by name pattern (e.g. '*.go', '**/*.txt'). Supports ** for recursive matching.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"pattern": map[string]interface{}{"type": "string", "description": "Glob pattern (e.g. '*.go', '**/*.txt', 'src/**/*.json')."},
				"path":    map[string]interface{}{"type": "string", "description": "Base directory to search in (relative, optional; defaults to working directory)."},
			},
		},
		func(args map[string]interface{}) (string, error) {
			return toolGlob(workingDir, args)
		})
}

func mustPath(args map[string]interface{}) (string, error) {
	p, ok := args["path"].(string)
	if !ok || p == "" {
		return "", fmt.Errorf("path is required")
	}
	return p, nil
}

func mustString(args map[string]interface{}, key string) (string, error) {
	v, ok := args[key]
	if !ok {
		return "", fmt.Errorf("%s is required", key)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string", key)
	}
	return s, nil
}

func optInt(args map[string]interface{}, key string) (*int, error) {
	v, ok := args[key]
	if !ok {
		return nil, nil
	}
	f, ok := v.(float64)
	if !ok {
		return nil, fmt.Errorf("%s must be an integer", key)
	}
	i := int(f)
	return &i, nil
}

// toolViewFile reads a file and returns its content, optionally limited to a line range.
func toolViewFile(workingDir string, args map[string]interface{}) (string, error) {
	path, err := mustPath(args)
	if err != nil {
		return "", err
	}

	resolved, err := sandbox.ResolvePath(workingDir, path)
	if err != nil {
		return "", err
	}

	startLine, err := optInt(args, "start_line")
	if err != nil {
		return "", err
	}
	endLine, err := optInt(args, "end_line")
	if err != nil {
		return "", err
	}

	data, err := os.ReadFile(resolved)
	if err != nil {
		return "", fmt.Errorf("read file: %v", err)
	}

	lines := strings.Split(string(data), "\n")

	// Handle trailing newline: if file ends with \n, Split produces an extra empty element
	if len(lines) > 0 && lines[len(lines)-1] == "" && strings.HasSuffix(string(data), "\n") {
		lines = lines[:len(lines)-1]
	}

	// Calculate start and end indices relative to the original list
	start := 0
	end := len(lines)
	if startLine != nil {
		s := *startLine
		if s < 0 {
			s = 0
		}
		if s > end {
			s = end
		}
		start = s
	}
	if endLine != nil {
		e := *endLine
		if e < 0 {
			e = 0
		}
		if e > end {
			e = end
		}
		end = e
	}

	if start > end {
		return "", fmt.Errorf("start_line (%d) must be less than or equal to end_line (%d)", start, end)
	}

	return strings.Join(lines[start:end], "\n"), nil
}

// toolCreateFile creates or overwrites a file.
func toolCreateFile(workingDir string, args map[string]interface{}) (string, error) {
	path, err := mustPath(args)
	if err != nil {
		return "", err
	}

	content, err := mustString(args, "content")
	if err != nil {
		return "", err
	}

	resolved, err := sandbox.ResolvePath(workingDir, path)
	if err != nil {
		return "", err
	}

	// Create parent directories as needed
	if err := os.MkdirAll(filepath.Dir(resolved), 0755); err != nil {
		return "", fmt.Errorf("create directories: %v", err)
	}

	if err := os.WriteFile(resolved, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("write file: %v", err)
	}

	return fmt.Sprintf("File created: %s (%d bytes)", path, len(content)), nil
}

// toolAppendToFile appends content to a file.
func toolAppendToFile(workingDir string, args map[string]interface{}) (string, error) {
	path, err := mustPath(args)
	if err != nil {
		return "", err
	}

	content, err := mustString(args, "content")
	if err != nil {
		return "", err
	}

	resolved, err := sandbox.ResolvePath(workingDir, path)
	if err != nil {
		return "", err
	}

	f, err := os.OpenFile(resolved, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return "", fmt.Errorf("open file: %v", err)
	}
	defer f.Close()

	n, err := f.WriteString(content)
	if err != nil {
		return "", fmt.Errorf("write to file: %v", err)
	}

	return fmt.Sprintf("Appended %d bytes to: %s", n, path), nil
}

// toolListFiles lists files and directories in the given path.
func toolListFiles(workingDir string, args map[string]interface{}) (string, error) {
	path, _ := args["path"].(string)
	if path == "" {
		path = "."
	}

	resolved, err := sandbox.ResolvePath(workingDir, path)
	if err != nil {
		return "", err
	}

	entries, err := os.ReadDir(resolved)
	if err != nil {
		return "", fmt.Errorf("list directory: %v", err)
	}

	var results []string
	for _, entry := range entries {
		// Skip hidden files (starting with ".")
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		info := ""
		if entry.IsDir() {
			info = "/"
		}
		results = append(results, entry.Name()+info)
	}

	return strings.Join(results, "\n"), nil
}

// toolEditFile replaces the first occurrence of old_text with new_text in a file.
func toolEditFile(workingDir string, args map[string]interface{}) (string, error) {
	path, err := mustPath(args)
	if err != nil {
		return "", err
	}

	oldText, err := mustString(args, "old_text")
	if err != nil {
		return "", err
	}

	newText, err := mustString(args, "new_text")
	if err != nil {
		return "", err
	}

	resolved, err := sandbox.ResolvePath(workingDir, path)
	if err != nil {
		return "", err
	}

	data, err := os.ReadFile(resolved)
	if err != nil {
		return "", fmt.Errorf("read file: %v", err)
	}

	content := string(data)
	idx := strings.Index(content, oldText)
	if idx == -1 {
		return "", fmt.Errorf("old_text not found in file")
	}

	// Replace only the first occurrence
	content = content[:idx] + newText + content[idx+len(oldText):]

	if err := os.WriteFile(resolved, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("write file: %v", err)
	}

	return fmt.Sprintf("Edited %s: replaced %d bytes with %d bytes", path, len(oldText), len(newText)), nil
}

// toolGrep searches for a pattern in files or directories.
func toolGrep(workingDir string, args map[string]interface{}) (string, error) {
	pattern, err := mustString(args, "pattern")
	if err != nil {
		return "", err
	}

	path, err := mustPath(args)
	if err != nil {
		return "", err
	}

	useRegex := false
	if v, ok := args["use_regex"]; ok {
		if b, ok := v.(bool); ok {
			useRegex = b
		}
	}

	resolved, err := sandbox.ResolvePath(workingDir, path)
	if err != nil {
		return "", err
	}

	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("stat: %v", err)
	}

	var results []string

	if info.IsDir() {
		// Walk directory without following symlinks.
		// filepath.WalkDir does not follow symlinks, preventing
		// symlink-based escapes during directory traversal.
		err := filepath.WalkDir(resolved, func(fp string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}

			matches, err := grepFile(fp, pattern, useRegex, workingDir)
			if err != nil {
				return nil // Skip files we can't read
			}
			results = append(results, matches...)
			return nil
		})
		if err != nil {
			return "", fmt.Errorf("walk directory: %v", err)
		}
	} else {
		matches, err := grepFile(resolved, pattern, useRegex, workingDir)
		if err != nil {
			return "", err
		}
		results = append(results, matches...)
	}

	if len(results) == 0 {
		return "No matches found.", nil
	}

	return strings.Join(results, "\n"), nil
}

// grepFile searches for a pattern in a single file and returns matched lines.
func grepFile(filePath, pattern string, useRegex bool, workingDir string) ([]string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var matches []string

	// Get relative path from workingDir
	relPath, err := filepath.Rel(workingDir, filePath)
	if err != nil {
		relPath = filePath
	}

	if useRegex {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid regex %q: %v", pattern, err)
		}

		scanner := bufio.NewScanner(f)
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			line := scanner.Text()
			if re.MatchString(line) {
				matches = append(matches, fmt.Sprintf("%s:%d:%s", relPath, lineNum, line))
			}
		}
	} else {
		scanner := bufio.NewScanner(f)
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			line := scanner.Text()
			if strings.Contains(line, pattern) {
				matches = append(matches, fmt.Sprintf("%s:%d:%s", relPath, lineNum, line))
			}
		}
	}

	return matches, nil
}

// globMatch checks if a filename matches a simple glob pattern (no path separators).
// Handles *, ?, and [seq] character classes.
func globMatch(pattern, name string) bool {
	// Convert glob pattern to regex: * -> [^/]*, ? -> [^/], [abc] stays, escape dots
	re := "^"
	for i := 0; i < len(pattern); i++ {
		ch := pattern[i]
		switch ch {
		case '*':
			re += "[^/]*"
		case '?':
			re += "[^/]"
		case '[', ']', '.', '+', '^', '$', '{', '}', '(', ')', '|', '\\':
			re += "\\" + string(ch)
		default:
			re += string(ch)
		}
	}
	re += "$"

	matched, err := regexp.MatchString(re, name)
	return matched && err == nil
}

// toolGlob finds files matching a glob pattern within the sandbox.
func toolGlob(workingDir string, args map[string]interface{}) (string, error) {
	pattern, err := mustString(args, "pattern")
	if err != nil {
		return "", err
	}

	searchPath, _ := args["path"].(string)
	if searchPath == "" {
		searchPath = "."
	}

	resolved, err := sandbox.ResolvePath(workingDir, searchPath)
	if err != nil {
		return "", err
	}

	// Resolve symlinks in workingDir for consistent path comparisons (macOS /var ↔ /private/var)
	resolvedWD, wdErr := filepath.EvalSymlinks(workingDir)
	if wdErr != nil {
		resolvedWD = workingDir
	}

	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("stat: %v", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("path must be a directory, got file: %s", searchPath)
	}

	// Check if pattern uses recursive glob (**). If so, walk the directory tree.
	// Otherwise, use filepath.Glob for a single-level match.
	useRecursive := strings.Contains(pattern, "**")

	type matchInfo struct {
		relPath string
		modTime time.Time
	}
	var matches []matchInfo

	if useRecursive {
		// Split the pattern: the part before ** is the fixed prefix, after is the suffix pattern.
		// e.g. "**/*.go" => prefix="", suffix="*.go"
		//      "src/**/*.go" => prefix="src", suffix="*.go"
		parts := strings.SplitN(pattern, "**/", 2)
		var prefixPattern string
		var suffixPattern string

		if len(parts) == 1 {
			// Pattern like "**" or "**/" — match everything recursively
			suffixPattern = parts[0]
			if strings.HasSuffix(suffixPattern, "/") {
				suffixPattern = ""
			} else if suffixPattern != "**" {
				suffixPattern = ""
			}
			prefixPattern = ""
		} else {
			prefixPattern = strings.TrimSuffix(parts[0], "/")
			suffixPattern = parts[1]
			if strings.HasPrefix(suffixPattern, "/") {
				suffixPattern = strings.TrimPrefix(suffixPattern, "/")
			}
		}

		err := filepath.WalkDir(resolved, func(fp string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}

			// Skip hidden files
			if strings.HasPrefix(d.Name(), ".") {
				return nil
			}

			// Compute the relative path from resolved
			relFromBase, err := filepath.Rel(resolved, fp)
			if err != nil {
				return nil
			}

			// Validate the path is within sandbox
			if _, err := sandbox.ResolvePath(workingDir, filepath.Join(searchPath, relFromBase)); err != nil {
				return nil
			}

			// Get just the filename for pattern matching
			name := d.Name()

			// If there's a suffix pattern, match against filename
			if suffixPattern != "" {
				if !globMatch(suffixPattern, name) {
					return nil
				}
			}

			// If there's a prefix, verify the file is under that prefix
			if prefixPattern != "" {
				// The prefix should match at the start of the relative path
				if !strings.HasPrefix(relFromBase+string(filepath.Separator), prefixPattern+string(filepath.Separator)) && relFromBase != prefixPattern {
					return nil
				}
			}

			info, err := d.Info()
			if err != nil {
				return nil
			}

			// Compute full relative path from working directory
			fullRel, err := filepath.Rel(resolvedWD, fp)
			if err != nil {
				fullRel = relFromBase
			}

			matches = append(matches, matchInfo{
				relPath: fullRel,
				modTime: info.ModTime(),
			})

			return nil
		})
		if err != nil {
			return "", fmt.Errorf("walk directory: %v", err)
		}
	} else {
		// Single-level glob — match in the resolved directory only
		entries, err := os.ReadDir(resolved)
		if err != nil {
			return "", fmt.Errorf("list directory: %v", err)
		}

		for _, entry := range entries {
			// Skip hidden files
			if strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			// Skip directories (glob only returns files)
			if entry.IsDir() {
				continue
			}

			if !globMatch(pattern, entry.Name()) {
				continue
			}

			// Validate sandbox
			fullRel := entry.Name()
			if searchPath != "." {
				fullRel = filepath.Join(searchPath, entry.Name())
			}
			if _, err := sandbox.ResolvePath(workingDir, fullRel); err != nil {
				continue
			}

			info, err := entry.Info()
			if err != nil {
				continue
			}

			matches = append(matches, matchInfo{
				relPath: fullRel,
				modTime: info.ModTime(),
			})
		}
	}

	// Sort by modification time (newest first)
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].modTime.After(matches[j].modTime)
	})

	// Cap at 100 results
	if len(matches) > 100 {
		matches = matches[:100]
	}

	if len(matches) == 0 {
		return "No matches found.", nil
	}

	var result []string
	for _, m := range matches {
		result = append(result, m.relPath)
	}

	return strings.Join(result, "\n"), nil
}
