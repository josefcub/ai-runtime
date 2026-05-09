package tools

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/agent-project/harness/sandbox"
)

const (
	// HTTP tool timeouts (no retries).
	httpTimeout = 30 * time.Second

	// fetch: max response body size for text retrieval.
	fetchMaxBytes = 50 * 1024 // 50 KB

	// download: max response body size for file downloads.
	downloadMaxBytes = 100 * 1024 * 1024 // 100 MB
  maxRedirects = 10
)

// RegisterWebTools registers the fetch and download tools on the given registry.
func RegisterWebTools(reg *Registry) {
	workingDir := reg.workingDir

	// fetch
	reg.Register("fetch",
		"Fetch the content of a URL and return it as text, markdown, or html. Only http/https schemes are allowed. Responses larger than 50 KB are truncated.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"url":    map[string]interface{}{"type": "string", "description": "The URL to fetch (http or https only)."},
				"format": map[string]interface{}{"type": "string", "description": "Return format: text, markdown, or html (default: text)."},
			},
		},
		func(args map[string]interface{}) (string, error) {
			return toolFetch(args)
		})

	// download
	reg.Register("download",
		"Download a file from a URL and save it to the working directory. Only http/https schemes are allowed. Files larger than 100 MB are rejected. The destination path is sandboxed.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"url":        map[string]interface{}{"type": "string", "description": "The URL to download from (http or https only)."},
				"file_path":  map[string]interface{}{"type": "string", "description": "Relative path from working directory to save the file."},
				"timeout":    map[string]interface{}{"type": "integer", "description": "Optional timeout in seconds (default 30, max 600)."},
			},
		},
		func(args map[string]interface{}) (string, error) {
			return toolDownload(workingDir, args)
		})
}

// validateURL checks that the URL uses http or https and is otherwise valid.
func validateURL(rawURL string) error {
	if rawURL == "" {
		return fmt.Errorf("url is required")
	}
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		return fmt.Errorf("only http and https schemes are allowed")
	}
	return nil
}

// limitedReader wraps an io.Reader and stops after n bytes.
// If truncate is true, it returns io.EOF at the limit (silent truncation).
// Otherwise it returns an error.
type limitedReader struct {
	R        io.Reader
	n        int64
	left     int64
	limit    int64
	truncate bool
}

func (l *limitedReader) Read(p []byte) (int, error) {
	if l.left <= 0 {
		if l.truncate {
			return 0, io.EOF
		}
		return 0, fmt.Errorf("response exceeds %d byte limit", l.limit)
	}
	if int64(len(p)) > l.left {
		p = p[:l.left]
	}
	n, err := l.R.Read(p)
	l.left -= int64(n)
	// If we hit the limit mid-read, suppress the upstream error when truncating.
	if l.left <= 0 && l.truncate {
		return n, io.EOF
	}
	return n, err
}

// toolFetch retrieves a URL and returns the body as text.
func toolFetch(args map[string]interface{}) (string, error) {
	url, err := mustString(args, "url")
	if err != nil {
		return "", err
	}

	if err := validateURL(url); err != nil {
		return "", err
	}

	format := "text"
	if f, ok := args["format"].(string); ok && f != "" {
		format = f
	}

	// redirectCounter tracks HTTP 3xx redirects (max 10).
	redirectCounter := 0

	client := &http.Client{
		Timeout: httpTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			redirectCounter++
			if redirectCounter > maxRedirects {
				return fmt.Errorf("stopped after %d redirects", maxRedirects)
			}
			// Ensure redirected URL still uses http/https.
			if !strings.HasPrefix(req.URL.Scheme, "http") {
				return fmt.Errorf("redirect to unsupported scheme: %s", req.URL.Scheme)
			}
			return nil
		},
	}

	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("fetch failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("fetch failed: HTTP %d", resp.StatusCode)
	}

	// Limit response size — truncate silently at 50 KB.
	lr := &limitedReader{R: resp.Body, left: fetchMaxBytes, limit: fetchMaxBytes, truncate: true}
	body, err := io.ReadAll(lr)
	if err != nil {
		return "", fmt.Errorf("fetch failed: %v", err)
	}

	text := strings.TrimSpace(string(body))

	// Basic format conversion.
	switch format {
	case "html":
		// Return raw HTML as-is.
		return string(body), nil
	case "markdown":
		// If content-type is text/html, do a minimal strip. Otherwise return as-is.
		ct := resp.Header.Get("Content-Type")
		if strings.Contains(ct, "text/html") {
			text = stripHTML(text)
		}
		return text, nil
	default:
		return text, nil
	}
}

// stripHTML removes HTML tags for a basic markdown conversion.
func stripHTML(html string) string {
	var sb strings.Builder
	inTag := false
	for i := 0; i < len(html); i++ {
		ch := html[i]
		if ch == '<' {
			inTag = true
			continue
		}
		if ch == '>' {
			inTag = false
			sb.WriteString(" ")
			continue
		}
		if !inTag {
			sb.WriteRune(rune(ch))
		}
	}
	return sb.String()
}

// toolDownload fetches a URL and saves the body to a sandboxed file path.
func toolDownload(workingDir string, args map[string]interface{}) (string, error) {
	url, err := mustString(args, "url")
	if err != nil {
		return "", err
	}

	if err := validateURL(url); err != nil {
		return "", err
	}

	filePath, err := mustString(args, "file_path")
	if err != nil {
		return "", err
	}

	// Validate destination is within sandbox before downloading anything.
	resolved, err := sandbox.ResolvePath(workingDir, filePath)
	if err != nil {
		return "", fmt.Errorf("access denied: destination path is outside the working directory")
	}

	// Optional timeout override (capped at 600s).
	timeout := httpTimeout
	if t, ok := args["timeout"].(float64); ok {
		secs := int(t)
		if secs > 0 && secs <= 600 {
			timeout = time.Duration(secs) * time.Second
		}
	}

	// redirectCounter tracks HTTP 3xx redirects (max 10).
	redirectCounter := 0

	client := &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			redirectCounter++
			if redirectCounter > maxRedirects {
				return fmt.Errorf("stopped after %d redirects", maxRedirects)
			}
			// Ensure redirected URL still uses http/https.
			if !strings.HasPrefix(req.URL.Scheme, "http") {
				return fmt.Errorf("redirect to unsupported scheme: %s", req.URL.Scheme)
			}
			return nil
		},
	}

	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("download failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}

	// Create parent directories.
	if err := os.MkdirAll(filepath.Dir(resolved), 0755); err != nil {
		return "", fmt.Errorf("create directories: %v", err)
	}

	// Open destination file.
	out, err := os.OpenFile(resolved, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return "", fmt.Errorf("open destination: %v", err)
	}
	defer out.Close()

	// Stream with size limit.
	lr := &limitedReader{R: resp.Body, left: downloadMaxBytes, limit: downloadMaxBytes}
	written, err := io.Copy(out, lr)
	if err != nil {
		// Clean up partial file.
		out.Close()
		os.Remove(resolved)
		return "", fmt.Errorf("download failed: %v", err)
	}

	if err := out.Close(); err != nil {
		return "", fmt.Errorf("close file: %v", err)
	}

	return fmt.Sprintf("Downloaded %d bytes to %s", written, filePath), nil
}
