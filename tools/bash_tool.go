package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// RegisterBashTools registers the bash tool on the given registry.
// If enabled is false, the tool is not registered.
func RegisterBashTools(reg *Registry, enabled bool, timeout time.Duration, maxOutput int, banned []string) {
	if !enabled {
		return
	}

	workingDir := reg.workingDir
	cfg := bashConfig{
		Timeout:   timeout,
		MaxOutput: maxOutput,
		Banned:    banned,
	}

	reg.Register("bash",
		"Execute a shell command. stdin is closed (no interactive tools). Command runs in the working directory. Banned commands are rejected. Non-zero exit codes return output, not an error.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"command": map[string]interface{}{
					"type":        "string",
					"description": "Shell command to execute.",
				},
				"timeout": map[string]interface{}{
					"type":        "integer",
					"description": "Optional timeout override in seconds (max 300).",
				},
			},
		},
		func(args map[string]interface{}) (string, error) {
			return toolBash(workingDir, cfg, args)
		})
}

// bashConfig holds runtime settings for the bash tool (internal, not exported).
type bashConfig struct {
	Timeout   time.Duration
	MaxOutput int
	Banned    []string
}

// toolBash executes a shell command and returns stdout+stderr.
func toolBash(workingDir string, cfg bashConfig, args map[string]interface{}) (string, error) {
	command, err := mustString(args, "command")
	if err != nil {
		return "", err
	}

	// Check if the first token (command name) is banned.
	if isBanned(command, cfg.Banned) {
		return "", fmt.Errorf("command is not allowed")
	}

	// Determine timeout.
	timeout := cfg.Timeout
	if t, ok := args["timeout"].(float64); ok {
		secs := int(t)
		if secs > 0 && secs <= 300 {
			timeout = time.Duration(secs) * time.Second
		}
	}

	// Create context with timeout.
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Execute the command.
	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	cmd.Dir = workingDir
	cmd.Stdin = nil

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// Check if it was a timeout.
		if ctx.Err() == context.DeadlineExceeded {
			msg := fmt.Sprintf("Command timed out after %s.", cfg.Timeout)
			if stderr.Len() > 0 {
				msg += fmt.Sprintf("\nstderr: %s", truncateOutput(stderr.String(), cfg.MaxOutput/2))
			}
			return msg, nil
		}

		// Exit error (non-zero exit code) — still return output.
		result := ""
		if stdout.Len() > 0 {
			result = truncateOutput(stdout.String(), cfg.MaxOutput)
		}
		if stderr.Len() > 0 {
			if result != "" {
				result += "\n"
			}
			result += "stderr: " + truncateOutput(stderr.String(), cfg.MaxOutput/2)
		}
		if result == "" {
			result = err.Error()
		}
		// Return the output but don't error — non-zero exit is informational.
		return result, nil
	}

	output := stdout.String()
	if stderr.Len() > 0 {
		if output != "" {
			output += "\n"
		}
		output += "stderr: " + stderr.String()
	}

	return truncateOutput(output, cfg.MaxOutput), nil
}

// isBanned checks if the first token of the command is in the banned list.
func isBanned(command string, banned []string) bool {
	// Extract the first token (before any spaces, pipes, redirects, etc.).
	// We handle common shell metacharacters as separators.
	firstToken := extractFirstToken(command)
	firstToken = strings.ToLower(strings.TrimSpace(firstToken))

	for _, b := range banned {
		if firstToken == b {
			return true
		}
	}
	return false
}

// extractFirstToken extracts the first word from a shell command,
// stopping at shell metacharacters or whitespace.
func extractFirstToken(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return ""
	}

	// Skip leading shell metacharacters like # (comments)
	// and find the first actual token.
	start := 0
	for start < len(command) {
		ch := command[start]
		if ch != ' ' && ch != '\t' {
			break
		}
		start++
	}
	if start >= len(command) {
		return ""
	}

	// Read the first token until we hit a separator.
	end := start
	for end < len(command) {
		ch := command[end]
		// These are shell metacharacters that typically separate the command from args.
		if ch == ' ' || ch == '\t' || ch == '|' || ch == '>' || ch == '<' ||
			ch == '&' || ch == ';' || ch == '(' || ch == ')' || ch == '$' ||
			ch == '"' || ch == '\'' || ch == '`' || ch == '\\' || ch == '\n' || ch == '#' {
			break
		}
		end++
	}

	return command[start:end]
}

// truncateOutput truncates output to max bytes and appends a notice if truncated.
func truncateOutput(output string, maxBytes int) string {
	// Fast path: no truncation needed.
	if len(output) <= maxBytes {
		return output
	}

	// Truncate, trying to cut at a line boundary.
	truncated := output[:maxBytes]
	lastNewline := strings.LastIndexByte(truncated, '\n')
	if lastNewline > maxBytes/2 {
		// Don't cut too close to the start — if there's a reasonable line break, use it.
		truncated = truncated[:lastNewline]
	}

	return truncated + fmt.Sprintf("\n\n... [output truncated, %d total bytes] ", len(output))
}
