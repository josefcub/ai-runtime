package llm

import (
	"context"
	"encoding/json"
	"fmt"
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

	client := New(srv.URL, "test-model", "", 5*time.Second, t.TempDir())
	ctx := context.Background()
	resp, err := client.Chat(ctx, []Message{{Role: "user", Content: "hi"}}, nil, 100)
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

	client := New(srv.URL, "test-model", "", 5*time.Second, t.TempDir())
	ctx := context.Background()
	resp, err := client.Chat(ctx, []Message{{Role: "user", Content: "view foo.go"}}, nil, 100)
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

	client := New(srv.URL, "test-model", "", 5*time.Second, t.TempDir())
	ctx := context.Background()
	resp, err := client.Chat(ctx, []Message{{Role: "user", Content: "search"}}, nil, 100)
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

	client := New(srv.URL, "test-model", "secret-key-123", 5*time.Second, t.TempDir())
	ctx := context.Background()
	_, err := client.Chat(ctx, []Message{{Role: "user", Content: "hi"}}, nil, 100)
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

	client := New(srv.URL, "test-model", "", 5*time.Second, t.TempDir())
	ctx := context.Background()
	_, err := client.Chat(ctx, []Message{{Role: "user", Content: "hi"}}, nil, 100)
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

	client := New(srv.URL, "test-model", "", 5*time.Second, t.TempDir())
	ctx := context.Background()
	_, err := client.Chat(ctx, []Message{{Role: "user", Content: "hi"}}, nil, 100)
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
	client := New(srv.URL, "test-model", "", 5*time.Second, t.TempDir())
	ctx := context.Background()
	_, err := client.Chat(ctx, []Message{{Role: "user", Content: "hi"}}, toolsJSON, 500)
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

	client := New(srv.URL, "test-model", "", 5*time.Second, t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := client.Chat(ctx, []Message{{Role: "user", Content: "hi"}}, nil, 100)
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
	client := New(srv.URL, "test-model", "", 5*time.Second, logDir)

	ctx, cancel := context.WithCancel(context.Background())

	// Start chat in goroutine
	done := make(chan error, 1)
	go func() {
		_, err := client.Chat(ctx, []Message{{Role: "user", Content: "hi"}}, nil, 100)
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

func TestChatPartialResponseDebugFile(t *testing.T) {
	// Run both scenarios (debug and non-debug) in one test to avoid
	// any global logger state issues.

	// --- Scenario 1: Info level — no file should be written ---
	{
		tmpDir := t.TempDir()
		logger, err := log.New(tmpDir, log.InfoLevel)
		if err != nil {
			t.Fatalf("create logger: %v", err)
		}
		log.SetGlobal(logger)
		defer logger.Close()

		var connected atomic.Bool
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"hello"}}]}`)
			flusher, _ := w.(http.Flusher)
			flusher.Flush()
			connected.Store(true)
			<-r.Context().Done()
		}))
		defer srv.Close()

		logDir := t.TempDir()
		client := New(srv.URL, "test-model", "", 5*time.Second, logDir)

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			_, err := client.Chat(ctx, []Message{{Role: "user", Content: "hi"}}, nil, 100)
			done <- err
		}()

		for i := 0; i < 100 && !connected.Load(); i++ {
			time.Sleep(10 * time.Millisecond)
		}
		time.Sleep(50 * time.Millisecond)
		cancel()
		<-done

		files, _ := os.ReadDir(logDir)
		for _, f := range files {
			if strings.HasPrefix(f.Name(), "partial-") {
				t.Errorf("expected no partial response file at info level, found: %s", f.Name())
			}
		}
	}

	// --- Scenario 2: Debug level — file should be written ---
	{
		tmpDir := t.TempDir()
		logger, err := log.New(tmpDir, log.DebugLevel)
		if err != nil {
			t.Fatalf("create debug logger: %v", err)
		}
		log.SetGlobal(logger)
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
		client := New(srv.URL, "test-model", "", 5*time.Second, logDir)

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			_, err := client.Chat(ctx, []Message{{Role: "user", Content: "hi"}}, nil, 100)
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
		<-done

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
}

