package config

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// Parse reads INI data and returns a map of section -> key -> value.
// Top-level keys (before any section) are placed in the "" (empty) section.
func Parse(data []byte) (map[string]map[string]string, error) {
	result := map[string]map[string]string{}

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	// Handle potentially large multiline values
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	currentSection := ""
	inMultiline := false
	multilineKey := ""
	multilineBuf := []string{}

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		// Handle multiline values
		if inMultiline {
			// Check for closing """
			if strings.HasSuffix(trimmed, `"""`) {
				// Append content before closing """
				content := strings.TrimSuffix(trimmed, `"""`)
				if content != "" {
					multilineBuf = append(multilineBuf, content)
				}
				val := strings.Join(multilineBuf, "\n")
				getSection(result, currentSection)[multilineKey] = val
				inMultiline = false
				multilineKey = ""
				multilineBuf = nil
				continue
			}
			multilineBuf = append(multilineBuf, trimmed)
			continue
		}

		// Skip blank lines
		if trimmed == "" {
			continue
		}

		// Skip comment lines
		if trimmed[0] == '#' || trimmed[0] == ';' {
			continue
		}

		// Check for section header
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			currentSection = trimmed[1 : len(trimmed)-1]
			continue
		}

		// Parse key = value
		idx := strings.Index(trimmed, "=")
		if idx < 0 {
			continue
		}

		key := strings.TrimSpace(trimmed[:idx])
		rawValue := trimmed[idx+1:]

		// Check for multiline opening """
		valueStr := strings.TrimSpace(rawValue)
		if strings.HasPrefix(valueStr, `"""`) {
			after := strings.TrimPrefix(valueStr, `"""`)
			afterTrimmed := strings.TrimSpace(after)
			// Check if closing """ is on the same line
			if strings.HasSuffix(afterTrimmed, `"""`) {
				// Single-line triple-quoted value
				val := strings.TrimSuffix(afterTrimmed, `"""`)
				getSection(result, currentSection)[key] = val
			} else {
				// Start multiline
				inMultiline = true
				multilineKey = key
				if strings.TrimSpace(after) != "" {
					multilineBuf = []string{after}
				} else {
					multilineBuf = []string{}
				}
			}
			continue
		}

		// Strip inline comments for non-quoted values
		valueStr = stripInlineComment(valueStr)
		// Strip surrounding quotes if present
		valueStr = stripQuotes(valueStr)

		getSection(result, currentSection)[key] = valueStr
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan error: %w", err)
	}

	if inMultiline {
		return nil, fmt.Errorf("unclosed multiline value (missing closing \"\"\")")
	}

	return result, nil
}

// ParseFile reads and parses an INI file.
func ParseFile(path string) (map[string]map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return Parse(data)
}

// getSection returns the map for a section, creating it if needed.
func getSection(result map[string]map[string]string, section string) map[string]string {
	if _, ok := result[section]; !ok {
		result[section] = map[string]string{}
	}
	return result[section]
}

// stripInlineComment removes inline comments (# or ;) from a value string.
// It respects quoted strings, not stripping comments inside quotes.
func stripInlineComment(s string) string {
	s = strings.TrimSpace(s)

	// If value starts with a quote, find the closing quote and keep only that portion
	if len(s) >= 1 && s[0] == '"' {
		for i := 1; i < len(s); i++ {
			if s[i] == '"' {
				return s[:i+1]
			}
		}
		// No closing quote found, return as-is
		return s
	}

	// Find first unquoted comment character
	for i, c := range s {
		if c == '#' || c == ';' {
			return strings.TrimSpace(s[:i])
		}
	}
	return s
}

// stripQuotes removes surrounding double quotes from a string.
func stripQuotes(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}
