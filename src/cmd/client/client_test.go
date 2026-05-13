package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/agent-project/harness/session"
)

// ---------- httpClient Tests ----------

func TestClient_Send_Success(t *testing.T) {
	var received WebhookRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	u, _ := url.Parse(srv.URL)
	client := &httpClient{host: u.Hostname(), port: mustPort(u), webhookPath: u.Path}

	err := client.Send("test-ch", "hello world", "", session.ImageAttachment{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if received.Channel != "test-ch" {
		t.Errorf("channel = %q, want %q", received.Channel, "test-ch")
	}
	if received.Message != "hello world" {
		t.Errorf("message = %q, want %q", received.Message, "hello world")
	}
	if received.CallbackURL != "" {
		t.Errorf("callback_url = %q, want empty", received.CallbackURL)
	}
}

func TestClient_Send_WithCallback(t *testing.T) {
	var received WebhookRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	u, _ := url.Parse(srv.URL)
	client := &httpClient{host: u.Hostname(), port: mustPort(u), webhookPath: u.Path}

	cbURL := "http://127.0.0.1:9999/callback"
	err := client.Send("ch-1", "test message", cbURL, session.ImageAttachment{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if received.CallbackURL != cbURL {
		t.Errorf("callback_url = %q, want %q", received.CallbackURL, cbURL)
	}
}

func TestClient_Send_Non2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	u, _ := url.Parse(srv.URL)
	client := &httpClient{host: u.Hostname(), port: mustPort(u), webhookPath: u.Path}

	err := client.Send("ch-1", "hello", "", session.ImageAttachment{})
	if err == nil {
		t.Fatal("expected error for 502 response, got nil")
	}
}

func TestClient_Send_ConnectionRefused(t *testing.T) {
	client := &httpClient{host: "127.0.0.1", port: 59999, webhookPath: "/webhook"}
	err := client.Send("ch", "msg", "", session.ImageAttachment{})
	if err == nil {
		t.Fatal("expected error for connection refused, got nil")
	}
}

func TestClient_Send_WithImageAttachment(t *testing.T) {
	var received WebhookRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	u, _ := url.Parse(srv.URL)
	client := &httpClient{host: u.Hostname(), port: mustPort(u), webhookPath: u.Path}

	att := session.ImageAttachment{Data: "iVBORw0KGgo=", MIMEType: "image/png"}
	err := client.Send("test-ch", "what is this image", "", att)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if received.ImageAttachment == nil {
		t.Fatal("expected image_attachment to be sent, got nil")
	}
	if received.ImageAttachment.Data != "iVBORw0KGgo=" {
		t.Errorf("data = %q, want %q", received.ImageAttachment.Data, "iVBORw0KGgo=")
	}
	if received.ImageAttachment.MIMEType != "image/png" {
		t.Errorf("mime_type = %q, want %q", received.ImageAttachment.MIMEType, "image/png")
	}
}

// ---------- Callback Server Tests ----------

func TestCallbackServer_ReceivesCallback(t *testing.T) {
	cb, err := newCallbackServer()
	if err != nil {
		t.Fatalf("failed to create callback server: %v", err)
	}
	defer cb.stop()

	resp := CallbackResponse{Channel: "test-ch", Message: "callback received"}
	body, _ := json.Marshal(resp)

	httpResp, err := http.Post(fmt.Sprintf("http://127.0.0.1:%d/callback", cb.port), "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post failed: %v", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", httpResp.StatusCode)
	}

	select {
	case result := <-cb.ch:
		if result.Channel != "test-ch" {
			t.Errorf("channel = %q, want %q", result.Channel, "test-ch")
		}
		if result.Message != "callback received" {
			t.Errorf("message = %q, want %q", result.Message, "callback received")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for callback")
	}
}

func TestCallbackServer_RejectsNonPOST(t *testing.T) {
	cb, err := newCallbackServer()
	if err != nil {
		t.Fatalf("failed to create callback server: %v", err)
	}
	defer cb.stop()

	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/callback", cb.port))
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusMethodNotAllowed)
	}
}

func TestCallbackServer_RejectsInvalidJSON(t *testing.T) {
	cb, err := newCallbackServer()
	if err != nil {
		t.Fatalf("failed to create callback server: %v", err)
	}
	defer cb.stop()

	resp, err := http.Post(fmt.Sprintf("http://127.0.0.1:%d/callback", cb.port), "application/json", bytes.NewReader([]byte("not json")))
	if err != nil {
		t.Fatalf("post failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

// ---------- Integration Test (full flow) ----------

func TestFullFlow_Callback(t *testing.T) {
	// Mock harness: receives webhook, then calls back
	var mu sync.Mutex
	var webhookReceived WebhookRequest

	harnessSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req WebhookRequest
		json.NewDecoder(r.Body).Decode(&req)
		mu.Lock()
		webhookReceived = req
		mu.Unlock()
		w.WriteHeader(http.StatusOK)

		// Simulate harness processing and calling back
		if req.CallbackURL != "" {
			go func() {
				time.Sleep(50 * time.Millisecond)
				resp := CallbackResponse{
					Channel: req.Channel,
					Message: "processed: " + req.Message,
				}
				body, _ := json.Marshal(resp)
				_, _ = http.Post(req.CallbackURL, "application/json", bytes.NewReader(body))
			}()
		}
	}))
	defer harnessSrv.Close()

	u, _ := url.Parse(harnessSrv.URL)

	// Start callback server
	cb, err := newCallbackServer()
	if err != nil {
		t.Fatalf("failed to create callback server: %v", err)
	}
	defer cb.stop()

	callbackURL := fmt.Sprintf("http://127.0.0.1:%d/callback", cb.port)

	// Send message
	client := &httpClient{host: u.Hostname(), port: mustPort(u), webhookPath: u.Path}
	err = client.Send("integration-ch", "hello harness", callbackURL, session.ImageAttachment{})
	if err != nil {
		t.Fatalf("send failed: %v", err)
	}

	// Wait for callback
	select {
	case result := <-cb.ch:
		if result.Channel != "integration-ch" {
			t.Errorf("channel = %q, want %q", result.Channel, "integration-ch")
		}
		if result.Message != "processed: hello harness" {
			t.Errorf("message = %q, want %q", result.Message, "processed: hello harness")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for callback")
	}

	// Verify harness received the correct request
	mu.Lock()
	if webhookReceived.Channel != "integration-ch" {
		t.Errorf("harness channel = %q, want %q", webhookReceived.Channel, "integration-ch")
	}
	if webhookReceived.Message != "hello harness" {
		t.Errorf("harness message = %q, want %q", webhookReceived.Message, "hello harness")
	}
	mu.Unlock()
}

// ---------- Config Helpers Tests ----------

func TestStrVal(t *testing.T) {
	data := map[string]map[string]string{
		"server": {"host": "0.0.0.0", "port": "9090"},
	}

	if got := strVal(data, "server", "host", "default"); got != "0.0.0.0" {
		t.Errorf("got %q, want %q", got, "0.0.0.0")
	}
	if got := strVal(data, "server", "missing", "default"); got != "default" {
		t.Errorf("got %q, want %q", got, "default")
	}
	if got := strVal(data, "missing", "key", "default"); got != "default" {
		t.Errorf("got %q, want %q", got, "default")
	}
}

func TestIntVal(t *testing.T) {
	data := map[string]map[string]string{
		"server": {"port": "9090"},
	}

	if got := intVal(data, "server", "port", 8080); got != 9090 {
		t.Errorf("got %d, want %d", got, 9090)
	}
	if got := intVal(data, "server", "missing", 8080); got != 8080 {
		t.Errorf("got %d, want %d", got, 8080)
	}
}

// ---------- mustPort helper ----------

func mustPort(u *url.URL) int {
	p, err := strconv.Atoi(u.Port())
	if err != nil {
		return 80
	}
	return p
}
