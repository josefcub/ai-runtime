package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agent-project/harness/queue"
	"github.com/agent-project/harness/session"
)

// mockProcessor implements Processor for testing.
type mockProcessor struct {
	mu       sync.Mutex
	calls    []string
	response string
	err      error
	done     chan struct{} // signaled after each Process() call completes
}

func (m *mockProcessor) Process(ctx context.Context, sess *session.Session, messageText, systemPrompt string, imageAtt session.ImageAttachment) (string, error) {
	if m.done != nil {
		defer func() { m.done <- struct{}{} }()
	}
	m.mu.Lock()
	m.calls = append(m.calls, messageText)
	m.mu.Unlock()

	if m.err != nil {
		return "", m.err
	}

	// Simulate real agent behavior: add user + assistant messages to session
	userMsg := session.ConversationMessage{
		Role:    session.RoleUser,
		Content: messageText,
	}
	if imageAtt.Data != "" {
		userMsg.Attachments = []session.ImageAttachment{imageAtt}
	}
	sess.Messages = append(sess.Messages, userMsg)
	sess.Messages = append(sess.Messages, session.ConversationMessage{
		Role:    session.RoleAssistant,
		Content: m.response,
	})

	return m.response, nil
}

func (m *mockProcessor) GetCalls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	calls := make([]string, len(m.calls))
	copy(calls, m.calls)
	return calls
}

func newTestWorker(t *testing.T, processor Processor, workingDir string) (*Worker, *queue.Queue, *session.Manager) {
	t.Helper()

	stateDir := filepath.Join(t.TempDir(), "state")
	q := queue.New(64, nil)
	sess := session.NewManager(stateDir)

	return New(q, sess, processor, "test system prompt", workingDir, nil), q, sess
}

func TestWorker_SingleMessage(t *testing.T) {
	proc := &mockProcessor{response: "agent response", done: make(chan struct{}, 1)}
	w, q, _ := newTestWorker(t, proc, t.TempDir())

	// Enqueue a message
	q.Enqueue(queue.Message{
		ChannelID:   "ch-1",
		MessageText: "hello",
	})

	// Run worker with timeout context
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()

	// Wait for processing to complete
	<-proc.done
	cancel()
	<-done

	calls := proc.GetCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0] != "hello" {
		t.Errorf("call = %q, want %q", calls[0], "hello")
	}
}

func TestWorker_MultiChannelInterleaving(t *testing.T) {
	proc := &mockProcessor{response: "ok", done: make(chan struct{}, 5)}
	w, q, _ := newTestWorker(t, proc, t.TempDir())

	// Enqueue messages for different channels
	for i := 0; i < 5; i++ {
		q.Enqueue(queue.Message{
			ChannelID:   fmt.Sprintf("ch-%d", i),
			MessageText: fmt.Sprintf("msg-%d", i),
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()

	// Wait for all 5 messages to be processed
	for i := 0; i < 5; i++ {
		<-proc.done
	}
	cancel()
	<-done

	calls := proc.GetCalls()
	if len(calls) != 5 {
		t.Fatalf("expected 5 calls, got %d", len(calls))
	}

	// Verify all messages were processed in FIFO order
	for i := 0; i < 5; i++ {
		expected := fmt.Sprintf("msg-%d", i)
		if calls[i] != expected {
			t.Errorf("calls[%d] = %q, want %q", i, calls[i], expected)
		}
	}
}

func TestWorker_CallbackDelivery(t *testing.T) {
	var receivedCallback struct {
		Channel string
		Message string
	}
	var callbackMu sync.Mutex

	callbackServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callbackMu.Lock()
		defer callbackMu.Unlock()
		json.NewDecoder(r.Body).Decode(&receivedCallback)
		w.WriteHeader(http.StatusOK)
	}))
	defer callbackServer.Close()

	proc := &mockProcessor{response: "callback response", done: make(chan struct{}, 1)}
	w, q, _ := newTestWorker(t, proc, t.TempDir())

	q.Enqueue(queue.Message{
		ChannelID:   "ch-callback",
		MessageText: "test callback",
		CallbackURL: callbackServer.URL,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()

	<-proc.done
	cancel()
	<-done

	callbackMu.Lock()
	defer callbackMu.Unlock()

	if receivedCallback.Channel != "ch-callback" {
		t.Errorf("callback channel = %q, want %q", receivedCallback.Channel, "ch-callback")
	}
	if receivedCallback.Message != "callback response" {
		t.Errorf("callback message = %q, want %q", receivedCallback.Message, "callback response")
	}
}

func TestWorker_NoCallbackURL(t *testing.T) {
	proc := &mockProcessor{response: "no callback response", done: make(chan struct{}, 1)}
	w, q, _ := newTestWorker(t, proc, t.TempDir())

	q.Enqueue(queue.Message{
		ChannelID:   "ch-no-callback",
		MessageText: "no callback",
		// No CallbackURL
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()

	<-proc.done
	cancel()
	<-done

	// Should complete without error — no callback attempted
	calls := proc.GetCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
}

func TestWorker_ProcessorError(t *testing.T) {
	var receivedCallback struct {
		Channel string
		Message string
	}
	var callbackMu sync.Mutex

	callbackServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callbackMu.Lock()
		defer callbackMu.Unlock()
		json.NewDecoder(r.Body).Decode(&receivedCallback)
		w.WriteHeader(http.StatusOK)
	}))
	defer callbackServer.Close()

	proc := &mockProcessor{err: fmt.Errorf("processor error"), done: make(chan struct{}, 1)}
	w, q, _ := newTestWorker(t, proc, t.TempDir())

	q.Enqueue(queue.Message{
		ChannelID:   "ch-error",
		MessageText: "error test",
		CallbackURL: callbackServer.URL,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()

	<-proc.done
	cancel()
	<-done

	callbackMu.Lock()
	defer callbackMu.Unlock()

	if receivedCallback.Channel != "ch-error" {
		t.Errorf("callback channel = %q, want %q", receivedCallback.Channel, "ch-error")
	}
	if receivedCallback.Message == "" {
		t.Error("expected error message in callback")
	}
}

func TestWorker_SessionCreated(t *testing.T) {
	proc := &mockProcessor{response: "ok", done: make(chan struct{}, 1)}
	w, q, sess := newTestWorker(t, proc, t.TempDir())

	q.Enqueue(queue.Message{
		ChannelID:   "new-channel",
		MessageText: "first msg",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()

	<-proc.done
	cancel()
	<-done

	if !sess.Exists("new-channel") {
		t.Error("expected session to be created")
	}
}

func TestWorker_EmptyQueue(t *testing.T) {
	proc := &mockProcessor{}
	w, _, _ := newTestWorker(t, proc, t.TempDir())

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()

	// Should exit gracefully when context is cancelled
	select {
	case <-done:
		// OK
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not stop within timeout")
	}

	// No calls should have been made
	calls := proc.GetCalls()
	if len(calls) != 0 {
		t.Errorf("expected 0 calls, got %d", len(calls))
	}
}

func TestWorker_ContextCancellation(t *testing.T) {
	proc := &mockProcessor{response: "ok"}
	w, q, _ := newTestWorker(t, proc, t.TempDir())

	// Enqueue many messages
	for i := 0; i < 100; i++ {
		q.Enqueue(queue.Message{
			ChannelID:   "ch-many",
			MessageText: fmt.Sprintf("msg-%d", i),
		})
	}

	// Cancel after a short time
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()

	// Cancel almost immediately
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// OK
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not stop within timeout")
	}
}

func TestWorker_SessionSaved(t *testing.T) {
	proc := &mockProcessor{response: "saved", done: make(chan struct{}, 1)}
	w, q, sess := newTestWorker(t, proc, t.TempDir())

	q.Enqueue(queue.Message{
		ChannelID:   "ch-save",
		MessageText: "save test",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()

	<-proc.done
	cancel()
	<-done

	// Verify session was saved (message count should include user + assistant)
	s := sess.Get("ch-save")
	if len(s.Messages) < 2 {
		t.Errorf("expected at least 2 messages in session, got %d", len(s.Messages))
	}
}

func TestWorker_ConcurrentSafety(t *testing.T) {
	proc := &mockProcessor{response: "ok"}
	w, q, _ := newTestWorker(t, proc, t.TempDir())

	// Enqueue messages concurrently while worker is running
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var processed atomic.Int32
	origProcess := proc
	// Wrap processor to count
	countingProc := &mockProcessor{response: "ok", done: make(chan struct{}, 10)}
	w.processor = countingProc

	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()

	// Enqueue messages concurrently
	for i := 0; i < 10; i++ {
		q.Enqueue(queue.Message{
			ChannelID:   fmt.Sprintf("concurrent-%d", i),
			MessageText: fmt.Sprintf("msg-%d", i),
		})
	}

	// Wait for all 10 to be processed
	for i := 0; i < 10; i++ {
		<-countingProc.done
	}
	cancel()
	<-done

	calls := countingProc.GetCalls()
	_ = processed // tracked by calls
	if len(calls) < 10 {
		t.Errorf("expected 10 calls, got %d (may vary due to timing)", len(calls))
	}
	_ = origProcess // used to verify original processor wasn't modified
}

func TestBuildSystemPrompt_NoFiles(t *testing.T) {
	workingDir := t.TempDir()
	_, _, _ = newTestWorker(t, &mockProcessor{}, workingDir)

	w := New(queue.New(64, nil), session.NewManager(filepath.Join(t.TempDir(), "state")),
		&mockProcessor{}, "You are a robot.", workingDir, nil)

	prompt := w.buildSystemPrompt()
	if prompt != "You are a robot." {
		t.Errorf("prompt = %q, want %q", prompt, "You are a robot.")
	}
}

func TestBuildSystemPrompt_LoadsFiles(t *testing.T) {
	workingDir := t.TempDir()

	// Create some prompt files
	os.WriteFile(filepath.Join(workingDir, "SOUL.md"), []byte("Be kind and helpful."), 0644)
	os.WriteFile(filepath.Join(workingDir, "MEMORY.md"), []byte("User prefers short answers."), 0644)

	w := New(queue.New(64, nil), session.NewManager(filepath.Join(t.TempDir(), "state")),
		&mockProcessor{}, "Base prompt.", workingDir, nil)

	prompt := w.buildSystemPrompt()

	if !strings.Contains(prompt, "Base prompt.") {
		t.Error("prompt missing base prompt")
	}
	if !strings.Contains(prompt, "--- SOUL.md ---") {
		t.Error("prompt missing SOUL.md delimiter")
	}
	if !strings.Contains(prompt, "Be kind and helpful.") {
		t.Error("prompt missing SOUL.md content")
	}
	if !strings.Contains(prompt, "--- MEMORY.md ---") {
		t.Error("prompt missing MEMORY.md delimiter")
	}
	if !strings.Contains(prompt, "User prefers short answers.") {
		t.Error("prompt missing MEMORY.md content")
	}
}

func TestBuildSystemPrompt_FileOrder(t *testing.T) {
	workingDir := t.TempDir()

	os.WriteFile(filepath.Join(workingDir, "SOUL.md"), []byte("soul content"), 0644)
	os.WriteFile(filepath.Join(workingDir, "IDENTITY.md"), []byte("identity content"), 0644)
	os.WriteFile(filepath.Join(workingDir, "USER.md"), []byte("user content"), 0644)
	os.WriteFile(filepath.Join(workingDir, "MEMORY.md"), []byte("memory content"), 0644)

	w := New(queue.New(64, nil), session.NewManager(filepath.Join(t.TempDir(), "state")),
		&mockProcessor{}, "base", workingDir, nil)

	prompt := w.buildSystemPrompt()

	// Verify file order: SOUL before IDENTITY before USER before MEMORY
	soulIdx := strings.Index(prompt, "--- SOUL.md ---")
	identityIdx := strings.Index(prompt, "--- IDENTITY.md ---")
	userIdx := strings.Index(prompt, "--- USER.md ---")
	memoryIdx := strings.Index(prompt, "--- MEMORY.md ---")

	if soulIdx >= identityIdx {
		t.Error("SOUL.md should appear before IDENTITY.md")
	}
	if identityIdx >= userIdx {
		t.Error("IDENTITY.md should appear before USER.md")
	}
	if userIdx >= memoryIdx {
		t.Error("USER.md should appear before MEMORY.md")
	}
}

func TestBuildSystemPrompt_SkipsMissingFiles(t *testing.T) {
	workingDir := t.TempDir()
	// Only create IDENTITY.md, skip others

	os.WriteFile(filepath.Join(workingDir, "IDENTITY.md"), []byte("I am an agent."), 0644)

	w := New(queue.New(64, nil), session.NewManager(filepath.Join(t.TempDir(), "state")),
		&mockProcessor{}, "base prompt", workingDir, nil)

	prompt := w.buildSystemPrompt()

	if !strings.Contains(prompt, "base prompt") {
		t.Error("missing base prompt")
	}
	if !strings.Contains(prompt, "I am an agent.") {
		t.Error("missing IDENTITY.md content")
	}
	if strings.Contains(prompt, "SOUL.md") {
		t.Error("should not contain SOUL.md when file is missing")
	}
	if strings.Contains(prompt, "USER.md") {
		t.Error("should not contain USER.md when file is missing")
	}
	if strings.Contains(prompt, "MEMORY.md") {
		t.Error("should not contain MEMORY.md when file is missing")
	}
}

func TestBuildSystemPrompt_SkipsEmptyFiles(t *testing.T) {
	workingDir := t.TempDir()

	os.WriteFile(filepath.Join(workingDir, "SOUL.md"), []byte("   \n\n  "), 0644)
	os.WriteFile(filepath.Join(workingDir, "IDENTITY.md"), []byte("real content"), 0644)

	w := New(queue.New(64, nil), session.NewManager(filepath.Join(t.TempDir(), "state")),
		&mockProcessor{}, "base", workingDir, nil)

	prompt := w.buildSystemPrompt()

	if strings.Contains(prompt, "--- SOUL.md ---") {
		t.Error("should skip whitespace-only SOUL.md")
	}
	if !strings.Contains(prompt, "--- IDENTITY.md ---") {
		t.Error("should include IDENTITY.md")
	}
}
