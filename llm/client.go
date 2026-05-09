package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/agent-project/harness/log"
)

// Client handles communication with OpenAI-compatible LLM APIs.
type Client struct {
	baseURL string
	model   string
	apiKey  string
	timeout time.Duration
	http    *http.Client
	logDir  string
}

// New creates a new LLM client.
// logDir is used to write partial response files when debug logging is enabled.
func New(baseURL, model, apiKey string, timeout time.Duration, logDir string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		apiKey:  apiKey,
		timeout: timeout,
		http: &http.Client{
			Timeout: timeout,
		},
		logDir: logDir,
	}
}

// Message represents a chat message for the LLM API.
type Message struct {
	Role              string     `json:"role"`
	Content           string     `json:"content"`
	ReasoningContent  string     `json:"reasoning_content,omitempty"`
	ToolCalls         []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID        string     `json:"tool_call_id,omitempty"`
}

// ToolCall represents a tool invocation from the LLM.
type ToolCall struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// ChatResponse is the aggregated response from the LLM.
type ChatResponse struct {
	Content            string
	ReasoningContent   string
	ToolCalls          []ToolCall
}

// ErrPartialResponse is returned when the LLM stream is interrupted (e.g. context
// cancellation or timeout) but some content was already received. The Chat() method
// writes the partial content to a debug logfile before returning this error.
var ErrPartialResponse = errors.New("partial LLM response — connection interrupted")

// Chat sends a chat completion request with streaming (SSE) and returns the
// aggregated response including any tool calls.
func (c *Client) Chat(ctx context.Context, messages []Message, toolsJSON json.RawMessage, maxTokens int) (*ChatResponse, error) {
	body := chatRequestBody{
		Model:     c.model,
		Messages:  messages,
		Tools:     toolsJSON,
		MaxTokens: maxTokens,
		Stream:    true,
	}

	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := c.baseURL + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	log.GetGlobal().WithSource("llm").Debug("LLM API request payload", "body", string(data))

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("LLM API error %d: %s", resp.StatusCode, strings.TrimSpace(string(errBody)))
	}

	result, err := parseSSE(ctx, resp.Body)
	if err == ErrPartialResponse && result != nil {
		c.writePartialResponse(result)
		return nil, fmt.Errorf("LLM call interrupted (partial response saved): %w", err)
	}
	return result, err
}

// writePartialResponse writes the partial LLM response to a debug logfile
// when debug logging is enabled. This preserves the work done by the model
// before the connection was interrupted, without contaminating the session file.
func (c *Client) writePartialResponse(resp *ChatResponse) {
	if log.GetGlobal().Level != log.DebugLevel {
		return
	}

	ts := time.Now().Format("20060102-150405")
	name := fmt.Sprintf("partial-%s.log", ts)
	path := filepath.Join(c.logDir, name)

	var buf strings.Builder
	if resp.ReasoningContent != "" {
		buf.WriteString("[Reasoning: ")
		buf.WriteString(resp.ReasoningContent)
		buf.WriteString("]\n")
	}
	if resp.Content != "" {
		buf.WriteString(resp.Content)
	}
	if len(resp.ToolCalls) > 0 {
		for _, tc := range resp.ToolCalls {
			buf.WriteString(fmt.Sprintf("\n[Tool Call: %s args=%s id=%s]\n",
				tc.Function.Name, tc.Function.Arguments, tc.ID))
		}
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		log.GetGlobal().WithSource("llm").Error("failed to create partial response file",
			"file", path, "error", err.Error())
		return
	}
	defer f.Close()

	if _, err := f.WriteString(buf.String()); err != nil {
		log.GetGlobal().WithSource("llm").Error("failed to write partial response",
			"file", path, "error", err.Error())
		return
	}

	log.GetGlobal().WithSource("llm").Debug("partial response saved",
		"file", path,
		"content_bytes", fmt.Sprintf("%d", len(resp.Content)),
		"reasoning_bytes", fmt.Sprintf("%d", len(resp.ReasoningContent)),
	)
}

// --- Internal types ---

type chatRequestBody struct {
	Model     string          `json:"model"`
	Messages  []Message       `json:"messages"`
	Tools     json.RawMessage `json:"tools,omitempty"`
	MaxTokens int             `json:"max_tokens,omitempty"`
	Stream    bool            `json:"stream"`
}

// SSE chunk types

type sseChunk struct {
	Choices []sseChoice `json:"choices"`
}

type sseChoice struct {
	Delta        sseDelta `json:"delta"`
	FinishReason *string  `json:"finish_reason"`
}

type sseDelta struct {
	ReasoningContent *string              `json:"reasoning_content"`
	Content          *string              `json:"content"`
	ToolCalls        []sseToolCallDelta   `json:"tool_calls"`
}

type sseToolCallDelta struct {
	Index    int               `json:"index"`
	ID       string            `json:"id"`
	Type     string            `json:"type"`
	Function *sseFunctionDelta `json:"function"`
}

type sseFunctionDelta struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// parseSSE reads an SSE stream and aggregates the response.
// If ctx is cancelled mid-stream, the accumulated content is returned along with
// ErrPartialResponse so the caller can preserve it (e.g. write to a debug logfile).
func parseSSE(ctx context.Context, reader io.Reader) (*ChatResponse, error) {
	scanner := bufio.NewScanner(reader)
	// Increase buffer for large SSE chunks
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	var content strings.Builder
	var reasoningContent strings.Builder

	// Track tool calls by index for incremental accumulation
	type accumTC struct {
		id        string
		toolType  string
		name      string
		arguments strings.Builder
	}
	toolCalls := make(map[int]*accumTC)

	for scanner.Scan() {
		line := scanner.Text()

		if line == "" {
			continue
		}

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")

		if data == "[DONE]" {
			break
		}

		var chunk sseChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		for _, choice := range chunk.Choices {
			// Accumulate reasoning content
			if choice.Delta.ReasoningContent != nil {
				reasoningContent.WriteString(*choice.Delta.ReasoningContent)
			}

			// Accumulate text content
			if choice.Delta.Content != nil {
				content.WriteString(*choice.Delta.Content)
			}

			// Accumulate tool calls incrementally
			for _, tc := range choice.Delta.ToolCalls {
				idx := tc.Index
				existing, ok := toolCalls[idx]
				if !ok {
					existing = &accumTC{}
					toolCalls[idx] = existing
				}

				if existing.id == "" && tc.ID != "" {
					existing.id = tc.ID
				}
				if existing.toolType == "" && tc.Type != "" {
					existing.toolType = tc.Type
				}
				if tc.Function != nil {
					if existing.name == "" && tc.Function.Name != "" {
						existing.name = tc.Function.Name
					}
					existing.arguments.WriteString(tc.Function.Arguments)
				}
			}
		}
	}

	// Build final tool calls in index order
	var finalToolCalls []ToolCall
	for i := 0; i < len(toolCalls); i++ {
		tc := toolCalls[i]
		finalToolCalls = append(finalToolCalls, ToolCall{
			ID:   tc.id,
			Type: tc.toolType,
		})
		finalToolCalls[len(finalToolCalls)-1].Function.Name = tc.name
		finalToolCalls[len(finalToolCalls)-1].Function.Arguments = tc.arguments.String()
	}

	if err := scanner.Err(); err != nil {
		// Check if this is a context cancellation (timeout, client disconnect).
		// If so, return the partial content along with ErrPartialResponse.
		if isContextError(ctx.Err(), err) {
			return &ChatResponse{
				Content:          content.String(),
				ReasoningContent: reasoningContent.String(),
				ToolCalls:        finalToolCalls,
			}, ErrPartialResponse
		}
		return nil, fmt.Errorf("SSE parse error: %w", err)
	}

	return &ChatResponse{
		Content:            content.String(),
		ReasoningContent:   reasoningContent.String(),
		ToolCalls:          finalToolCalls,
	}, nil
}

// isContextError reports whether scanErr is likely caused by ctxErr.
// When a context is cancelled, the underlying HTTP connection is closed,
// which propagates as an I/O error. We check for common wrappers.
func isContextError(ctxErr error, scanErr error) bool {
	if ctxErr == nil {
		return false
	}
	// The scanner error wraps the underlying connection close error,
	// which in turn may reference the context error.
	return errors.Is(scanErr, ctxErr)
}
