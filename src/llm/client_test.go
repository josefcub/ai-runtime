package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agent-project/harness/log"
)

func TestChatPlainTextResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/chat/completions" {
			t.Errorf("expected /chat/completions, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		// Simulate SSE stream with plain text
		chunks := []string{
			`data: {"choices":[{"delta":{"role":"assistant"},"finish_reason":null}]}`,
			`data: {"choices":[{"delta":{"content":"Hello"},"finish_reason":null}]}`,
			`data: {"choices":[{"delta":{"content":" world"},"finish_reason":null}]}`,
			`data: {"choices":[{"delta":{},"finish_reason":"stop"}]}`,
			`data: [DONE]`,
		}
		for _, chunk := range chunks {
			if _, err := w.Write([]byte(chunk + "\n\n")); err != nil {
				t.Fatal(err)
			}
			flusher, ok := w.(http.Flusher)
			if ok {
				flusher.Flush()
			}
		}
	}))
	defer srv.Close()

	client := New(srv.URL, "test-model", "", 5*time.Second, t.TempDir(), nil)
	ctx := context.Background()
	resp, err := client.Chat(ctx, []Message{NewTextMessage("user", "hi")}, nil, 100)
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	if resp.Content != "Hello world" {
		t.Errorf("expected 'Hello world', got %q", resp.Content)
	}
	if len(resp.ToolCalls) != 0 {
		t.Errorf("expected no tool calls, got %d", len(resp.ToolCalls))
	}
}

func TestChatToolCallsResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		// Simulate SSE stream with tool calls
		chunks := []string{
			`data: {"choices":[{"delta":{"role":"assistant"},"finish_reason":null}]}`,
			`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_abc","type":"function","function":{"name":"view"}}]},"finish_reason":null}]}`,
			`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"path\":\"foo.go\"}"}}]},"finish_reason":null}]}`,
			`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
			`data: [DONE]`,
		}
		for _, chunk := range chunks {
			if _, err := w.Write([]byte(chunk + "\n\n")); err != nil {
				t.Fatal(err)
			}
			flusher, ok := w.(http.Flusher)
			if ok {
				flusher.Flush()
			}
		}
	}))
	defer srv.Close()

	client := New(srv.URL, "test-model", "", 5*time.Second, t.TempDir(), nil)
	ctx := context.Background()
	resp, err := client.Chat(ctx, []Message{NewTextMessage("user", "view foo.go")}, nil, 100)
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	tc := resp.ToolCalls[0]
	if tc.ID != "call_abc" {
		t.Errorf("expected id 'call_abc', got %q", tc.ID)
	}
	if tc.Type != "function" {
		t.Errorf("expected type 'function', got %q", tc.Type)
	}
	if tc.Function.Name != "view" {
		t.Errorf("expected name 'view', got %q", tc.Function.Name)
	}
	if tc.Function.Arguments != `{"path":"foo.go"}` {
		t.Errorf("unexpected arguments: %q", tc.Function.Arguments)
	}
}

func TestChatMultipleToolCalls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		chunks := []string{
			`data: {"choices":[{"delta":{"role":"assistant"},"finish_reason":null}]}`,
			`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"grep"}}]},"finish_reason":null}]}`,
			`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"pattern\":\"error\"}"}}]},"finish_reason":null}]}`,
			`data: {"choices":[{"delta":{"tool_calls":[{"index":1,"id":"call_2","type":"function","function":{"name":"view"}}]},"finish_reason":null}]}`,
			`data: {"choices":[{"delta":{"tool_calls":[{"index":1,"function":{"arguments":"{\"path\":\"bar.go\"}"}}]},"finish_reason":null}]}`,
			`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
			`data: [DONE]`,
		}
		for _, chunk := range chunks {
			if _, err := w.Write([]byte(chunk + "\n\n")); err != nil {
				t.Fatal(err)
			}
			flusher, ok := w.(http.Flusher)
			if ok {
				flusher.Flush()
			}
		}
	}))
	defer srv.Close()

	client := New(srv.URL, "test-model", "", 5*time.Second, t.TempDir(), nil)
	ctx := context.Background()
	resp, err := client.Chat(ctx, []Message{NewTextMessage("user", "search")}, nil, 100)
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	if len(resp.ToolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Function.Name != "grep" {
		t.Errorf("expected first tool 'grep', got %q", resp.ToolCalls[0].Function.Name)
	}
	if resp.ToolCalls[1].Function.Name != "view" {
		t.Errorf("expected second tool 'view', got %q", resp.ToolCalls[1].Function.Name)
	}
}

func TestChatAPIKey(t *testing.T) {
	var receivedAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	client := New(srv.URL, "test-model", "secret-key-123", 5*time.Second, t.TempDir(), nil)
	ctx := context.Background()
	_, err := client.Chat(ctx, []Message{NewTextMessage("user", "hi")}, nil, 100)
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	expected := "Bearer secret-key-123"
	if receivedAuth != expected {
		t.Errorf("expected auth %q, got %q", expected, receivedAuth)
	}
}

func TestChatEmptyAPIKey(t *testing.T) {
	var receivedAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	client := New(srv.URL, "test-model", "", 5*time.Second, t.TempDir(), nil)
	ctx := context.Background()
	_, err := client.Chat(ctx, []Message{NewTextMessage("user", "hi")}, nil, 100)
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	if receivedAuth != "" {
		t.Errorf("expected no Authorization header, got %q", receivedAuth)
	}
}

func TestChatServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":{"message":"internal error"}}`))
	}))
	defer srv.Close()

	client := New(srv.URL, "test-model", "", 5*time.Second, t.TempDir(), nil)
	ctx := context.Background()
	_, err := client.Chat(ctx, []Message{NewTextMessage("user", "hi")}, nil, 100)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected 500 in error, got: %v", err)
	}
}

func TestChatRequestContainsTools(t *testing.T) {
	var receivedBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedBody)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	toolsJSON := json.RawMessage(`[{"type":"function","function":{"name":"test_tool"}}]`)
	client := New(srv.URL, "test-model", "", 5*time.Second, t.TempDir(), nil)
	ctx := context.Background()
	_, err := client.Chat(ctx, []Message{NewTextMessage("user", "hi")}, toolsJSON, 500)
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	if receivedBody["model"] != "test-model" {
		t.Errorf("expected model 'test-model', got %v", receivedBody["model"])
	}
	if receivedBody["stream"] != true {
		t.Error("expected stream=true")
	}
	if receivedBody["max_tokens"].(float64) != 500 {
		t.Errorf("expected max_tokens 500, got %v", receivedBody["max_tokens"])
	}
}

func TestChatContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Delay response to allow context cancellation
		time.Sleep(2 * time.Second)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	client := New(srv.URL, "test-model", "", 5*time.Second, t.TempDir(), nil)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := client.Chat(ctx, []Message{NewTextMessage("user", "hi")}, nil, 100)
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
}

func TestChatPartialResponseOnCancellation(t *testing.T) {
	// Server sends some data then stalls, allowing context cancellation mid-stream.
	var connected atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		// Send partial data
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"partial "}}]}`)
		flusher, _ := w.(http.Flusher)
		flusher.Flush()

		// Signal that we've started sending
		connected.Store(true)

		// Stall — wait for client to disconnect
		<-r.Context().Done()
	}))
	defer srv.Close()

	logDir := t.TempDir()
	client := New(srv.URL, "test-model", "", 5*time.Second, logDir, nil)

	ctx, cancel := context.WithCancel(context.Background())

	// Start chat in goroutine
	done := make(chan error, 1)
	go func() {
		_, err := client.Chat(ctx, []Message{NewTextMessage("user", "hi")}, nil, 100)
		done <- err
	}()

	// Wait for server to send partial data, then cancel
	for i := 0; i < 100 && !connected.Load(); i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if !connected.Load() {
		t.Fatal("server never sent partial data")
	}
	time.Sleep(50 * time.Millisecond) // let data arrive
	cancel()

	err := <-done
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
	if !strings.Contains(err.Error(), "interrupted") {
		t.Errorf("expected 'interrupted' in error, got: %v", err)
	}
}

func TestChatPartialResponseFile(t *testing.T) {
	// Verify that partial response files are written regardless of log level.
	// (Previously only written at debug level — now unconditional.)

	tmpDir := t.TempDir()
	logger, err := log.New(tmpDir, log.InfoLevel)
	if err != nil {
		t.Fatalf("create logger: %v", err)
	}
	defer logger.Close()

	var connected atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"reasoning_content":"thinking..."}}]}`)
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"hello"}}]}`)
		flusher, _ := w.(http.Flusher)
		flusher.Flush()
		connected.Store(true)
		<-r.Context().Done()
	}))
	defer srv.Close()

	logDir := t.TempDir()
	client := New(srv.URL, "test-model", "", 5*time.Second, logDir, nil)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := client.Chat(ctx, []Message{NewTextMessage("user", "hi")}, nil, 100)
		done <- err
	}()

	for i := 0; i < 100 && !connected.Load(); i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if !connected.Load() {
		t.Fatal("server never sent partial data")
	}
	time.Sleep(50 * time.Millisecond)
	cancel()
	err = <-done
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}

	// Check that a partial response file was created
	files, err := os.ReadDir(logDir)
	if err != nil {
		t.Fatalf("read log dir: %v", err)
	}

	var foundPartial bool
	for _, f := range files {
		if strings.HasPrefix(f.Name(), "partial-") && strings.HasSuffix(f.Name(), ".log") {
			foundPartial = true
			data, err := os.ReadFile(filepath.Join(logDir, f.Name()))
			if err != nil {
				t.Fatalf("read partial file: %v", err)
			}
			content := string(data)
			if !strings.Contains(content, "thinking...") {
				t.Errorf("expected reasoning content in partial file, got: %s", content)
			}
			if !strings.Contains(content, "hello") {
				t.Errorf("expected text content in partial file, got: %s", content)
			}
			break
		}
	}
	if !foundPartial {
		t.Errorf("no partial response file found in %s", logDir)
	}
}

func TestNewTextMessage(t *testing.T) {
	msg := NewTextMessage("user", "hello world")

	if msg.Role != "user" {
		t.Errorf("expected role 'user', got %q", msg.Role)
	}

	// Content should be a valid JSON string
	var content string
	if err := json.Unmarshal(msg.Content, &content); err != nil {
		t.Fatalf("Content is not a valid JSON string: %v", err)
	}
	if content != "hello world" {
		t.Errorf("expected 'hello world', got %q", content)
	}
}

func TestNewTextMessageEscaping(t *testing.T) {
	// Verify that special characters are properly JSON-escaped
	msg := NewTextMessage("user", `hello "world" with \ backslash`)

	var content string
	if err := json.Unmarshal(msg.Content, &content); err != nil {
		t.Fatalf("Content is not a valid JSON string: %v", err)
	}
	if content != `hello "world" with \ backslash` {
		t.Errorf("expected escaped content, got %q", content)
	}
}

func TestNewTextMessageNewlines(t *testing.T) {
	// Newlines and other control characters must be escaped
	msg := NewTextMessage("user", "line1\nline2\ttab\r\ncrlf")

	var content string
	if err := json.Unmarshal(msg.Content, &content); err != nil {
		t.Fatalf("Content is not a valid JSON string: %v", err)
	}
	if content != "line1\nline2\ttab\r\ncrlf" {
		t.Errorf("expected control chars preserved, got %q", content)
	}
}

func TestMessageMarshalPlainText(t *testing.T) {
	// Verify that a text message marshals Content as a JSON string (not array)
	msg := NewTextMessage("user", "plain text")

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}

	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}

	// Content should be a JSON string, not an array
	contentStr := string(parsed["content"])
	if !strings.HasPrefix(contentStr, `"`) {
		t.Errorf("expected Content to be a JSON string, got: %s", contentStr)
	}
}

func TestMessageMarshalMultimodal(t *testing.T) {
	// Verify that a multimodal message marshals Content as a JSON array
	contentParts := json.RawMessage(`[{"type":"text","text":"see this"},{"type":"image_url","image_url":{"url":"data:image/png;base64,abc"}}]`)

	msg := Message{
		Role:    "tool",
		Content: contentParts,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}

	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}

	// Content should be a JSON array
	contentStr := string(parsed["content"])
	if !strings.HasPrefix(contentStr, `[`) {
		t.Errorf("expected Content to be a JSON array, got: %s", contentStr)
	}
}

func TestChatRequestWithMultimodalContent(t *testing.T) {
	// End-to-end: verify the HTTP request body has correct content-parts format
	var receivedBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedBody)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	client := New(srv.URL, "test-model", "", 5*time.Second, t.TempDir(), nil)
	ctx := context.Background()

	// Send a message with multimodal content (content parts array)
	contentParts := json.RawMessage(`[{"type":"text","text":"image loaded"},{"type":"image_url","image_url":{"url":"data:image/png;base64,test123"}}]`)
	_, err := client.Chat(ctx, []Message{
		NewTextMessage("user", "show me this"),
		{Role: "tool", Content: contentParts},
	}, nil, 100)
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	// Verify the request was sent with correct messages
	msgs, ok := receivedBody["messages"].([]interface{})
	if !ok {
		t.Fatal("expected messages to be an array")
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}

	// Second message should have content as an array
	msg2, ok := msgs[1].(map[string]interface{})
	if !ok {
		t.Fatal("expected second message to be an object")
	}
	contentArr, ok := msg2["content"].([]interface{})
	if !ok {
		t.Errorf("expected content to be an array for multimodal message, got: %T", msg2["content"])
	} else {
		if len(contentArr) != 2 {
			t.Errorf("expected 2 content parts, got %d", len(contentArr))
		}
	}
}

func TestChatPartialResponseOnBrokenPipe(t *testing.T) {
	// Test parseSSE directly with a reader that errors mid-stream.
	// This simulates a broken pipe / connection reset scenario.
	ctx := context.Background()

	// Build a reader that returns data then errors
	data := `data: {"choices":[{"delta":{"content":"partial"}}]}
data: {"choices":[{"delta":{"content":" data"}}]}
`
	r := &erroringReader{
		reader: strings.NewReader(data),
		errAt:  len(data) - 10, // error partway through
		read:   0,
	}

	resp, err := parseSSE(ctx, r, nil)
	if err == nil {
		t.Fatal("expected error from broken stream, got nil")
	}
	// Should be ErrSSEParseError, not ErrPartialResponse
	if errors.Is(err, ErrPartialResponse) {
		t.Errorf("expected ErrSSEParseError, got ErrPartialResponse")
	}

	// Partial content should be preserved
	if resp == nil {
		t.Fatal("expected partial ChatResponse, got nil")
	}
	if resp.Content != "partial data" {
		t.Errorf("expected content 'partial data', got %q", resp.Content)
	}
}

// erroringReader returns data until errAt bytes, then returns a fixed error.
type erroringReader struct {
	reader io.Reader
	errAt  int
	read   int
}

func (r *erroringReader) Read(p []byte) (int, error) {
	if r.read >= r.errAt {
		return 0, fmt.Errorf("connection reset by peer")
	}
	n, err := r.reader.Read(p)
	r.read += n
	if err != nil {
		return n, fmt.Errorf("connection reset by peer")
	}
	return n, nil
}

func TestChatChunkParseErrorWarning(t *testing.T) {
	// Server sends a malformed chunk followed by valid data.
	// The malformed chunk should be logged as a warning and skipped.
	tmpDir := t.TempDir()
	logger, err := log.New(tmpDir, log.DebugLevel)
	if err != nil {
		t.Fatalf("create logger: %v", err)
	}
	defer logger.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		// Send malformed chunk
		fmt.Fprintln(w, `data: {not valid json}`)
		// Send valid chunk
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"ok"}}]}`)
		fmt.Fprintln(w, `data: [DONE]`)
		flusher, _ := w.(http.Flusher)
		flusher.Flush()
	}))
	defer srv.Close()

	logDir := t.TempDir()
	client := New(srv.URL, "test-model", "", 5*time.Second, logDir, logger)
	ctx := context.Background()

	resp, err := client.Chat(ctx, []Message{NewTextMessage("user", "hi")}, nil, 100)
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
	if resp.Content != "ok" {
		t.Errorf("expected 'ok', got %q", resp.Content)
	}

	// Verify a warning was logged for the malformed chunk
	logFile, err := os.ReadFile(filepath.Join(tmpDir, "harness.log"))
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	if !strings.Contains(string(logFile), "SSE chunk parse error") {
		t.Errorf("expected warning log for chunk parse error, got: %s", string(logFile))
	}
}
