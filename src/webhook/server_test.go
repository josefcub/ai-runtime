package webhook

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agent-project/harness/queue"
	"github.com/agent-project/harness/session"
)

func newTestServer(t *testing.T, maxDepth int) (*Server, *queue.Queue, *session.Manager, *httptest.Server) {
	t.Helper()

	stateDir := filepath.Join(t.TempDir(), "state")
	q := queue.New(maxDepth, nil)
	sess := session.NewManager(stateDir)

	srv := NewServer("127.0.0.1", 0, "/webhook", 1048576, q, sess, true, nil)

	// Create a test HTTP server wrapping the webhook handler
	h := http.NewServeMux()
	h.HandleFunc("/webhook", srv.handleWebhook)

	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)

	return srv, q, sess, ts
}

func TestWebhook_ValidPayload(t *testing.T) {
	_, q, _, ts := newTestServer(t, 64)

	payload := map[string]string{
		"channel":      "slack:abc123",
		"message":      "hello world",
		"callback_url": "http://example.com/callback",
	}
	body, _ := json.Marshal(payload)

	resp, err := http.Post(ts.URL+"/webhook", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusAccepted)
	}

	if q.Len() != 1 {
		t.Errorf("queue len = %d, want 1", q.Len())
	}

	msg, ok := q.Dequeue()
	if !ok {
		t.Fatal("expected message in queue")
	}
	if msg.ChannelID != "slack:abc123" {
		t.Errorf("channel = %q, want %q", msg.ChannelID, "slack:abc123")
	}
	// MessageText is prefixed with "[timestamp] [#channel] "
	if !strings.HasPrefix(msg.MessageText, "[") || !strings.Contains(msg.MessageText, "#slack:abc123") || !strings.HasSuffix(msg.MessageText, "hello world") {
		t.Errorf("message = %q, want prefixed message containing channel and original text", msg.MessageText)
	}
	if msg.CallbackURL != "http://example.com/callback" {
		t.Errorf("callback_url = %q, want %q", msg.CallbackURL, "http://example.com/callback")
	}
}

func TestWebhook_MissingChannel(t *testing.T) {
	_, _, _, ts := newTestServer(t, 64)

	payload := map[string]string{
		"message": "hello",
	}
	body, _ := json.Marshal(payload)

	resp, err := http.Post(ts.URL+"/webhook", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestWebhook_MissingMessage(t *testing.T) {
	_, _, _, ts := newTestServer(t, 64)

	payload := map[string]string{
		"channel": "slack:abc123",
	}
	body, _ := json.Marshal(payload)

	resp, err := http.Post(ts.URL+"/webhook", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestWebhook_InvalidJSON(t *testing.T) {
	_, _, _, ts := newTestServer(t, 64)

	resp, err := http.Post(ts.URL+"/webhook", "application/json", bytes.NewReader([]byte("not json")))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestWebhook_NewChannelCreatesSession(t *testing.T) {
	_, _, sess, ts := newTestServer(t, 64)

	payload := map[string]string{
		"channel": "new-channel",
		"message": "first message",
	}
	body, _ := json.Marshal(payload)

	resp, err := http.Post(ts.URL+"/webhook", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusAccepted)
	}

	if !sess.Exists("new-channel") {
		t.Error("expected session to be created for new channel")
	}
}

func TestWebhook_NoCallbackURL(t *testing.T) {
	_, q, _, ts := newTestServer(t, 64)

	payload := map[string]string{
		"channel": "slack:no-callback",
		"message": "no callback",
	}
	body, _ := json.Marshal(payload)

	resp, err := http.Post(ts.URL+"/webhook", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusAccepted)
	}

	msg, ok := q.Dequeue()
	if !ok {
		t.Fatal("expected message in queue")
	}
	if msg.CallbackURL != "" {
		t.Errorf("callback_url = %q, want empty string", msg.CallbackURL)
	}
}

func TestWebhook_Backpressure(t *testing.T) {
	_, _, _, ts := newTestServer(t, 2)

	// Fill the queue to max depth
	for i := 0; i < 2; i++ {
		payload := map[string]string{
			"channel": "full-channel",
			"message": "message",
		}
		body, _ := json.Marshal(payload)
		resp, err := http.Post(ts.URL+"/webhook", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("post %d: %v", i, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusAccepted {
			t.Errorf("post %d: status = %d, want %d", i, resp.StatusCode, http.StatusAccepted)
		}
	}

	// Next message should be rejected
	payload := map[string]string{
		"channel": "full-channel",
		"message": "should be rejected",
	}
	body, _ := json.Marshal(payload)
	resp, err := http.Post(ts.URL+"/webhook", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusTooManyRequests)
	}
}

func TestWebhook_BackpressureSendsCallback(t *testing.T) {
	var received CallbackPayload
	var callbackMu sync.Mutex
	var callbackReceived atomic.Bool

	callbackServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callbackMu.Lock()
		defer callbackMu.Unlock()
		json.NewDecoder(r.Body).Decode(&received)
		callbackReceived.Store(true)
		w.WriteHeader(http.StatusOK)
	}))
	defer callbackServer.Close()

	_, _, _, ts := newTestServer(t, 1)

	// Fill the queue to max depth
	payload := map[string]string{
		"channel":      "full-cb",
		"message":      "first",
		"callback_url": callbackServer.URL,
	}
	body, _ := json.Marshal(payload)
	resp, err := http.Post(ts.URL+"/webhook", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("first post: status = %d, want %d", resp.StatusCode, http.StatusAccepted)
	}

	// Second message should be rejected and callback sent
	payload["message"] = "should be rejected"
	body, _ = json.Marshal(payload)
	resp, err = http.Post(ts.URL+"/webhook", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusTooManyRequests)
	}

	// Wait for callback to arrive
	for i := 0; i < 100 && !callbackReceived.Load(); i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if !callbackReceived.Load() {
		t.Fatal("callback was not received")
	}

	callbackMu.Lock()
	defer callbackMu.Unlock()

	if received.Channel != "full-cb" {
		t.Errorf("callback channel = %q, want %q", received.Channel, "full-cb")
	}
	if received.Message == "" {
		t.Error("expected rejection message in callback")
	}
}

func TestWebhook_ShutdownReturns503(t *testing.T) {
	srv, _, _, _ := newTestServer(t, 64)

	// Trigger shutdown state
	srv.shutting.Store(true)

	// Create test server with the handler
	h := http.NewServeMux()
	h.HandleFunc("/webhook", srv.handleWebhook)
	ts := httptest.NewServer(h)
	defer ts.Close()

	payload := map[string]string{
		"channel": "ch-1",
		"message": "test",
	}
	body, _ := json.Marshal(payload)

	resp, err := http.Post(ts.URL+"/webhook", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}
}

func TestWebhook_MethodNotAllowed(t *testing.T) {
	_, _, _, ts := newTestServer(t, 64)

	resp, err := http.Get(ts.URL + "/webhook")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusMethodNotAllowed)
	}
}

func TestWebhook_SessionCreated(t *testing.T) {
	_, _, sess, ts := newTestServer(t, 64)

	// Send a message to create a session
	payload := map[string]string{
		"channel": "persist-ch",
		"message": "test message",
	}
	body, _ := json.Marshal(payload)

	resp, err := http.Post(ts.URL+"/webhook", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusAccepted)
	}

	// Verify session exists
	if !sess.Exists("persist-ch") {
		t.Error("expected session to exist after webhook")
	}
}

func TestWebhook_ArrivalTime(t *testing.T) {
	_, q, _, ts := newTestServer(t, 64)

	payload := map[string]string{
		"channel": "time-ch",
		"message": "time test",
	}
	body, _ := json.Marshal(payload)

	before := time.Now()
	resp, err := http.Post(ts.URL+"/webhook", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	resp.Body.Close()
	after := time.Now()

	msg, ok := q.Dequeue()
	if !ok {
		t.Fatal("expected message in queue")
	}

	if msg.ArrivalTime.Before(before) || msg.ArrivalTime.After(after) {
		t.Errorf("arrival time %v not between %v and %v", msg.ArrivalTime, before, after)
	}
}

// Helper for formatting channel names in tests
func channelName(i int) string {
	return fmt.Sprintf("channel-%d", i)
}

func TestWebhook_DifferentChannelsNoBackpressure(t *testing.T) {
	_, q, _, ts := newTestServer(t, 1)

	// Each channel has its own depth limit
	for i := 0; i < 3; i++ {
		payload := map[string]string{
			"channel": channelName(i),
			"message": "test",
		}
		body, _ := json.Marshal(payload)
		resp, err := http.Post(ts.URL+"/webhook", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("post %d: %v", i, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusAccepted {
			t.Errorf("%s: status = %d, want %d", channelName(i), resp.StatusCode, http.StatusAccepted)
		}
	}

	if q.Len() != 3 {
		t.Errorf("queue len = %d, want 3", q.Len())
	}
}

func TestWebhook_InvalidCallbackURL(t *testing.T) {
	_, _, _, ts := newTestServer(t, 64)

	testCases := []struct {
		name        string
		callbackURL string
	}{
		{"empty_scheme", "://not-a-url"},
		{"file_scheme", "file:///etc/passwd"},
		{"gopher_scheme", "gopher://example.com"},
		{"javascript_scheme", "javascript:alert(1)"},
		{"not_a_url", "not-a-url"},
		{"ftp_scheme", "ftp://example.com/file"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			payload := map[string]string{
				"channel":      "test-ch",
				"message":      "test",
				"callback_url": tc.callbackURL,
			}
			body, _ := json.Marshal(payload)

			resp, err := http.Post(ts.URL+"/webhook", "application/json", bytes.NewReader(body))
			if err != nil {
				t.Fatalf("post: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("status = %d, want %d for callback_url %q", resp.StatusCode, http.StatusBadRequest, tc.callbackURL)
			}
		})
	}
}

func TestWebhook_ValidCallbackURLSchemes(t *testing.T) {
	_, _, _, ts := newTestServer(t, 64)

	for _, scheme := range []string{"http", "https"} {
		t.Run(scheme, func(t *testing.T) {
			payload := map[string]string{
				"channel":      "test-ch",
				"message":      "test",
				"callback_url": scheme + "://example.com/callback",
			}
			body, _ := json.Marshal(payload)

			resp, err := http.Post(ts.URL+"/webhook", "application/json", bytes.NewReader(body))
			if err != nil {
				t.Fatalf("post: %v", err)
			}
			defer resp.Body.Close()

			// May be 202 or 429 (backpressure) — not 400
			if resp.StatusCode == http.StatusBadRequest {
				t.Errorf("status = 400, valid %s URL should not be rejected", scheme)
			}
		})
	}
}

func TestWebhook_ChannelIDTooLong(t *testing.T) {
	_, _, _, ts := newTestServer(t, 64)

	// 255 chars — exceeds the 254 limit
	longChannel := strings.Repeat("a", 255)
	payload := map[string]string{
		"channel": longChannel,
		"message": "test",
	}
	body, _ := json.Marshal(payload)

	resp, err := http.Post(ts.URL+"/webhook", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestWebhook_ChannelIDAtLimit(t *testing.T) {
	_, _, _, ts := newTestServer(t, 64)

	// Exactly 254 chars — should be accepted
	exactChannel := strings.Repeat("a", 254)
	payload := map[string]string{
		"channel": exactChannel,
		"message": "test",
	}
	body, _ := json.Marshal(payload)

	resp, err := http.Post(ts.URL+"/webhook", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	// Should be 202 Accepted (or 429 if backpressured, but not 400)
	if resp.StatusCode == http.StatusBadRequest {
		t.Errorf("status = 400, 254-char channel ID should be accepted")
	}
}

func TestWebhook_BodyTooLarge(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "state")
	q := queue.New(64, nil)
	sess := session.NewManager(stateDir)

	// Create server with 100-byte limit
	srv := NewServer("127.0.0.1", 0, "/webhook", 100, q, sess, true, nil)

	h := http.NewServeMux()
	h.HandleFunc("/webhook", srv.handleWebhook)
	ts := httptest.NewServer(h)
	defer ts.Close()

	// Build a payload that exceeds 100 bytes
	payload := map[string]string{
		"channel": "test-ch",
		"message": strings.Repeat("x", 200), // body will be > 100 bytes
	}
	body, _ := json.Marshal(payload)

	resp, err := http.Post(ts.URL+"/webhook", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	// MaxBytesReader causes the JSON decoder to return an error,
	// which we treat as "invalid JSON" (400)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestWebhook_ImageAttachmentPassthrough(t *testing.T) {
	_, q, _, ts := newTestServer(t, 64)

	payload := map[string]interface{}{
		"channel": "test-ch",
		"message": "what is this image",
		"image_attachment": map[string]string{
			"data":      "iVBORw0KGgo=",
			"mime_type": "image/png",
		},
	}
	body, _ := json.Marshal(payload)

	resp, err := http.Post(ts.URL+"/webhook", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusAccepted)
	}

	// Check the queued message has the image attachment
	msg, ok := q.Dequeue()
	if !ok {
		t.Fatal("expected message in queue")
	}
	if msg.ImageAttachment.Data != "iVBORw0KGgo=" {
		t.Errorf("data = %q, want %q", msg.ImageAttachment.Data, "iVBORw0KGgo=")
	}
	if msg.ImageAttachment.MIMEType != "image/png" {
		t.Errorf("mime_type = %q, want %q", msg.ImageAttachment.MIMEType, "image/png")
	}
}
