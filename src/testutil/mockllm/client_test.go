package mockllm

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/agent-project/harness/llm"
)

func TestMockClient_TableDriven(t *testing.T) {
	tests := []struct {
		name           string
		enqueue        func(*MockClient)
		messages       []llm.Message
		tools          json.RawMessage
		maxTokens      int
		wantContent    string
		wantErr        bool
		errContains    string
		wantCallCount  int
		wantLastMsgs   int
		wantLastTools  string
		wantLastTokens int
		multiCalls     int // how many times to call Chat after enqueueing
		wantContent2   string
	}{
		{
			name: "single response",
			enqueue: func(mc *MockClient) {
				mc.QueueResp(&llm.ChatResponse{Content: "hello"})
			},
			wantContent:   "hello",
			wantCallCount: 1,
		},
		{
			name: "multiple responses",
			enqueue: func(mc *MockClient) {
				mc.QueueResp(&llm.ChatResponse{Content: "first"})
				mc.QueueResp(&llm.ChatResponse{Content: "second"})
			},
			wantCallCount: 2,
			multiCalls:    2,
			wantContent:   "first",
			wantContent2:  "second",
		},
		{
			name: "error path",
			enqueue: func(mc *MockClient) {
				mc.QueueError(&llmError{msg: "connection refused"})
			},
			wantErr:       true,
			errContains:   "connection refused",
			wantCallCount: 1,
		},
		{
			name: "partial response",
			enqueue: func(mc *MockClient) {
				mc.QueuePartial(&llm.ChatResponse{Content: "partial"}, &llmError{msg: "partial response"})
			},
			wantErr:       true,
			errContains:   "partial response",
			wantCallCount: 1,
		},
		{
			name: "records last values",
			enqueue: func(mc *MockClient) {
				mc.QueueResp(&llm.ChatResponse{Content: "answer"})
			},
			messages: []llm.Message{
				{Role: "user", Content: json.RawMessage(`"test"`)},
			},
			tools:          json.RawMessage(`[{"type":"function"}]`),
			maxTokens:      2048,
			wantContent:    "answer",
			wantCallCount:  1,
			wantLastMsgs:   1,
			wantLastTools:  `[{"type":"function"}]`,
			wantLastTokens: 2048,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mc := NewMockClient()

			if tt.enqueue != nil {
				tt.enqueue(mc)
			}

			calls := tt.multiCalls
			if calls == 0 {
				calls = 1
			}

			for i := 0; i < calls; i++ {
				resp, err := mc.Chat(context.Background(), tt.messages, tt.tools, tt.maxTokens)

				if tt.wantErr {
					if err == nil {
						t.Fatal("expected error, got nil")
					}
					if tt.errContains != "" && !contains(err.Error(), tt.errContains) {
						t.Errorf("call %d: error %q should contain %q", i+1, err.Error(), tt.errContains)
					}
					return
				}
				if err != nil {
					t.Fatalf("call %d: unexpected error: %v", i+1, err)
				}
				if tt.wantContent != "" && i == 0 && resp.Content != tt.wantContent {
					t.Errorf("call %d: expected content %q, got %q", i+1, tt.wantContent, resp.Content)
				}
				if tt.wantContent2 != "" && i == 1 && resp.Content != tt.wantContent2 {
					t.Errorf("call %d: expected content %q, got %q", i+1, tt.wantContent2, resp.Content)
				}
			}
			if tt.wantCallCount > 0 && mc.CallCount() != tt.wantCallCount {
				t.Errorf("expected call count %d, got %d", tt.wantCallCount, mc.CallCount())
			}
			if tt.wantLastMsgs > 0 {
				last := mc.LastMessages()
				if len(last) != tt.wantLastMsgs {
					t.Errorf("expected %d last messages, got %d", tt.wantLastMsgs, len(last))
				}
			}
			if tt.wantLastTools != "" {
				lastTools := mc.LastTools()
				if string(lastTools) != tt.wantLastTools {
					t.Errorf("expected last tools %q, got %q", tt.wantLastTools, string(lastTools))
				}
			}
			if tt.wantLastTokens > 0 && mc.LastMaxTokens() != tt.wantLastTokens {
				t.Errorf("expected last max tokens %d, got %d", tt.wantLastTokens, mc.LastMaxTokens())
			}
		})
	}
}

func TestMockClient_ChainedResponses(t *testing.T) {
	mc := NewMockClient()
	mc.QueueResp(&llm.ChatResponse{Content: "first"})
	mc.QueueResp(&llm.ChatResponse{Content: "second"})
	mc.QueueResp(&llm.ChatResponse{Content: "third"})

	for i, want := range []string{"first", "second", "third"} {
		resp, err := mc.Chat(context.Background(), nil, nil, 0)
		if err != nil {
			t.Fatalf("call %d: unexpected error: %v", i+1, err)
		}
		if resp.Content != want {
			t.Errorf("call %d: expected %q, got %q", i+1, want, resp.Content)
		}
		if mc.CallCount() != i+1 {
			t.Errorf("call %d: expected call count %d, got %d", i+1, i+1, mc.CallCount())
		}
	}
}

func TestMockClient_BlockOnEmptyQueue(t *testing.T) {
	mc := NewMockClient()
	done := make(chan struct{})

	go func() {
		mc.Chat(context.Background(), nil, nil, 0)
		done <- struct{}{}
	}()

	select {
	case <-done:
		t.Fatal("expected Chat to block on empty queue")
	case <-time.After(50 * time.Millisecond):
		// Chat is blocked as expected
	}
}

func TestMockClient_UnblocksAfterQueue(t *testing.T) {
	mc := NewMockClient()
	done := make(chan *llm.ChatResponse, 1)

	go func() {
		resp, _ := mc.Chat(context.Background(), nil, nil, 0)
		done <- resp
	}()

	time.Sleep(50 * time.Millisecond)
	mc.QueueResp(&llm.ChatResponse{Content: "unblocked"})

	select {
	case resp := <-done:
		if resp.Content != "unblocked" {
			t.Errorf("expected 'unblocked', got %q", resp.Content)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for mock client to unblock")
	}
}

type llmError struct {
	msg string
}

func (e *llmError) Error() string { return e.msg }

func TestMockClient_LastMessagesIsCopy(t *testing.T) {
	mc := NewMockClient()
	mc.QueueResp(&llm.ChatResponse{Content: "hello"})
	mc.Chat(context.Background(), []llm.Message{
		{Role: "user", Content: json.RawMessage(`"test"`)},
	}, nil, 0)

	last := mc.LastMessages()
	origLen := len(last)
	origContent := string(last[0].Content)
	last[0].Content = json.RawMessage(`"mutated"`)

	after := mc.LastMessages()
	if len(after) != origLen {
		t.Fatalf("mutation changed len: %d -> %d", origLen, len(after))
	}
	if string(after[0].Content) != origContent {
		t.Errorf("LastMessages did not return a copy: Content changed from %q to %q", origContent, string(after[0].Content))
	}

	overcap := append(last, llm.Message{Role: "system", Content: json.RawMessage(`"extra"`)})
	after2 := mc.LastMessages()
	if len(after2) != 1 {
		t.Errorf("LastMessages did not return a copy: len is now %d (over-cap append leaked)", len(after2))
	}
	if len(overcap) != 2 {
		t.Fatalf("over-cap append itself failed: len %d != 2", len(overcap))
	}
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
