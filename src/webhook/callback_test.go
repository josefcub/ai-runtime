package webhook

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSendCallback_Success(t *testing.T) {
	var received CallbackPayload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	err := SendCallback("ch-1", "hello world", server.URL, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if received.Channel != "ch-1" {
		t.Errorf("channel = %q, want %q", received.Channel, "ch-1")
	}
	if received.Message != "hello world" {
		t.Errorf("message = %q, want %q", received.Message, "hello world")
	}
}

func TestSendCallback_NetworkError(t *testing.T) {
	// Use an invalid URL to trigger a network error
	err := SendCallback("ch-2", "test", "http://192.0.2.1:99999/callback", nil)
	if err == nil {
		t.Fatal("expected error for network failure, got nil")
	}
	if !strings.Contains(err.Error(), "network error") {
		t.Errorf("error = %q, want substring %q", err.Error(), "network error")
	}
}

func TestSendCallback_Non2xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte("upstream error"))
	}))
	defer server.Close()

	err := SendCallback("ch-3", "test message", server.URL, nil)
	if err == nil {
		t.Fatal("expected error for non-2xx response, got nil")
	}
	if !strings.Contains(err.Error(), "502") {
		t.Errorf("error = %q, want substring %q", err.Error(), "502")
	}
}

func TestSendCallback_EmptyMessage(t *testing.T) {
	var received CallbackPayload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	err := SendCallback("ch-4", "", server.URL, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if received.Message != "" {
		t.Errorf("message = %q, want empty string", received.Message)
	}
}

func TestCallbackPayload_JSON(t *testing.T) {
	p := CallbackPayload{
		Channel: "slack:abc123",
		Message: "response text",
	}

	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded CallbackPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.Channel != p.Channel {
		t.Errorf("channel = %q, want %q", decoded.Channel, p.Channel)
	}
	if decoded.Message != p.Message {
		t.Errorf("message = %q, want %q", decoded.Message, p.Message)
	}
}

func TestSendCallback_Shorthand2xx(t *testing.T) {
	// Test 201 Created response (also success)
	var received CallbackPayload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	err := SendCallback("ch-5", "created", server.URL, nil)
	if err != nil {
		t.Fatalf("unexpected error for 201: %v", err)
	}
	if received.Message != "created" {
		t.Errorf("message = %q, want %q", received.Message, "created")
	}
}
