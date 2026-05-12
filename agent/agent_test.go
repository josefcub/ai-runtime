package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agent-project/harness/llm"
	"github.com/agent-project/harness/session"
	"github.com/agent-project/harness/tools"
)

// mockCallResult holds either a response or an error for a single mock call.
type mockCallResult struct {
	resp *llm.ChatResponse
	err  error
}

// mockClient simulates LLM responses for testing.
type mockClient struct {
	mu        sync.Mutex
	results   []mockCallResult
	callCount int
	lastCalls []mockCall
}

type mockCall struct {
	messages  []llm.Message
	maxTokens int
}

func newMockClient() *mockClient {
	return &mockClient{}
}

func (m *mockClient) QueueResponse(resp *llm.ChatResponse) {
	m.results = append(m.results, mockCallResult{resp: resp})
}

func (m *mockClient) QueueError(err error) {
	m.results = append(m.results, mockCallResult{err: err})
}

func (m *mockClient) QueuePartial(resp *llm.ChatResponse, err error) {
	m.results = append(m.results, mockCallResult{resp: resp, err: err})
}

func (m *mockClient) Chat(_ context.Context, messages []llm.Message, _ json.RawMessage, maxTokens int) (*llm.ChatResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.callCount++
	m.lastCalls = append(m.lastCalls, mockCall{messages: messages, maxTokens: maxTokens})

	idx := m.callCount - 1
	if idx >= len(m.results) {
		return nil, fmt.Errorf("mockClient: no more responses (call %d, have %d)", m.callCount, len(m.results))
	}

	result := m.results[idx]
	return result.resp, result.err
}

func (m *mockClient) LastMessages() []llm.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.lastCalls) == 0 {
		return nil
	}
	return m.lastCalls[len(m.lastCalls)-1].messages
}

// setupAgent creates an agent with a mock client and basic tool registry for testing.
func setupAgent(t *testing.T, mc *mockClient, contextTokens int, summarizeThreshold float64, summarizeKeepRecent, maxToolIterations, maxTokens int, logToolCalls, logAgentReasoning bool) *Agent {
	t.Helper()

	// Create a temp dir for tool sandbox
	tmpDir := t.TempDir()

	// Register a simple test tool
	reg := tools.New(tmpDir)
	reg.Register("echo", "Echo back the input", map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"text": map[string]interface{}{"type": "string"},
		},
	}, func(args map[string]interface{}) (string, error) {
		if text, ok := args["text"].(string); ok {
			return text, nil
		}
		return "", fmt.Errorf("missing text")
	})

	return New(mc, reg, maxToolIterations, contextTokens, summarizeThreshold, summarizeKeepRecent, maxTokens, "Summarize the above conversation.", logToolCalls, logAgentReasoning, nil, nil)
}

func TestProcessPlainTextResponse(t *testing.T) {

	mc := newMockClient()
	mc.QueueResponse(&llm.ChatResponse{
		Content:   "This is the final answer.",
		ToolCalls: nil,
	})

	agent := setupAgent(t, mc, 8192, 0.70, 10, 20, 4096, true, true)
	sess := &session.Session{
		ChannelID: "test-channel",
		Messages:  nil,
	}

	output, err := agent.Process(context.Background(), sess, "What is 2+2?", "You are helpful.", session.ImageAttachment{})
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if !strings.Contains(output, "This is the final answer.") {
		t.Errorf("expected output to contain answer, got: %s", output)
	}

	// Should have user message + assistant message
	if len(sess.Messages) != 2 {
		t.Errorf("expected 2 messages, got %d", len(sess.Messages))
	}
}

func TestProcessToolCallLoop(t *testing.T) {
	
	mc := newMockClient()

	// First call: LLM decides to call "echo"
	echoTC := llm.ToolCall{ID: "call_1", Type: "function"}
	echoTC.Function.Name = "echo"
	echoTC.Function.Arguments = `{"text":"hello world"}`
	mc.QueueResponse(&llm.ChatResponse{
		Content:   "",
		ToolCalls: []llm.ToolCall{echoTC},
	})

	// Second call: LLM gives final answer
	mc.QueueResponse(&llm.ChatResponse{
		Content:   "The echoed result is: hello world",
		ToolCalls: nil,
	})

	agent := setupAgent(t, mc, 8192, 0.70, 10, 20, 4096, true, true)
	sess := &session.Session{
		ChannelID: "test-channel",
		Messages:  nil,
	}

	output, err := agent.Process(context.Background(), sess, "Echo hello world", "You are helpful.", session.ImageAttachment{})
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Output should contain tool call and result markers
	if !strings.Contains(output, "[Tool Call: echo]") {
		t.Errorf("expected tool call in output, got: %s", output)
	}
	if !strings.Contains(output, "[Result: hello world]") {
		t.Errorf("expected tool result in output, got: %s", output)
	}
	if !strings.Contains(output, "The echoed result is: hello world") {
		t.Errorf("expected final answer in output, got: %s", output)
	}

	// Verify session has correct message sequence: user, assistant(tool_call), tool, assistant(final)
	if len(sess.Messages) != 4 {
		t.Errorf("expected 4 messages in session, got %d: %+v", len(sess.Messages), sess.Messages)
	}
}

func TestProcessMaxIterations(t *testing.T) {
	
	mc := newMockClient()

	// LLM keeps calling tools — should stop at max_tool_iterations
	for i := 0; i < 5; i++ {
		resp := &llm.ChatResponse{
			Content: "",
			ToolCalls: []llm.ToolCall{
				{ID: fmt.Sprintf("call_%d", i), Type: "function"},
			},
		}
		resp.ToolCalls[0].Function.Name = "echo"
		resp.ToolCalls[0].Function.Arguments = `{"text":"loop"}`
		mc.QueueResponse(resp)
	}

	agent := setupAgent(t, mc, 8192, 0.70, 10, 3, 4096, true, true) // max 3 iterations
	sess := &session.Session{
		ChannelID: "test-channel",
		Messages:  nil,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := agent.Process(ctx, sess, "Loop forever", "You are helpful.", session.ImageAttachment{})
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Should have exactly 3 tool call rounds (3 iterations)
	toolCallMsgs := 0
	for _, msg := range sess.Messages {
		if msg.Role == session.RoleAssistant && len(msg.ToolCalls) > 0 {
			toolCallMsgs++
		}
	}
	if toolCallMsgs != 3 {
		t.Errorf("expected 3 tool-call assistant messages, got %d", toolCallMsgs)
	}
}

func TestProcessMaxIterationsSyntheticClosing(t *testing.T) {
	
	mc := newMockClient()

	// LLM keeps calling tools — exceeds max_tool_iterations (3)
	for i := 0; i < 5; i++ {
		resp := &llm.ChatResponse{
			Content: "",
			ToolCalls: []llm.ToolCall{
				{ID: fmt.Sprintf("call_%d", i), Type: "function"},
			},
		}
		resp.ToolCalls[0].Function.Name = "echo"
		resp.ToolCalls[0].Function.Arguments = `{"text":"loop"}`
		mc.QueueResponse(resp)
	}

	agent := setupAgent(t, mc, 8192, 0.70, 10, 3, 4096, true, true)
	sess := &session.Session{
		ChannelID: "test-channel",
		Messages:  nil,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	output, err := agent.Process(ctx, sess, "Loop forever", "You are helpful.", session.ImageAttachment{})
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Output should contain the synthetic closing message
	if !strings.Contains(output, "I reached my tool call limit this turn") {
		t.Errorf("expected synthetic closing message in output, got: %s", output)
	}

	// Last session message should be the synthetic assistant message
	lastMsg := sess.LastMessage()
	if lastMsg == nil {
		t.Fatal("expected last message, got nil")
	}
	if lastMsg.Role != session.RoleAssistant {
		t.Errorf("expected last message role=assistant, got %s", lastMsg.Role)
	}
	if !strings.Contains(lastMsg.Content, "tool call limit") {
		t.Errorf("expected synthetic closing content, got: %s", lastMsg.Content)
	}

	// Should have: 1 user + 3 tool-call assistant + 3 tool results + 1 synthetic closing = 8
	expectedMsgs := 8
	if len(sess.Messages) != expectedMsgs {
		t.Errorf("expected %d messages, got %d", expectedMsgs, len(sess.Messages))
	}
}

func TestProcessMaxIterationsNormalExitUnaffected(t *testing.T) {
	
	mc := newMockClient()

	// LLM calls a tool once, then gives final answer — no exhaustion
	echoTC := llm.ToolCall{ID: "call_1", Type: "function"}
	echoTC.Function.Name = "echo"
	echoTC.Function.Arguments = `{"text":"hello"}`
	mc.QueueResponse(&llm.ChatResponse{
		Content:   "",
		ToolCalls: []llm.ToolCall{echoTC},
	})
	mc.QueueResponse(&llm.ChatResponse{
		Content:   "Done.",
		ToolCalls: nil,
	})

	agent := setupAgent(t, mc, 8192, 0.70, 10, 3, 4096, true, true)
	sess := &session.Session{
		ChannelID: "test-channel",
		Messages:  nil,
	}

	output, err := agent.Process(context.Background(), sess, "Say hello", "You are helpful.", session.ImageAttachment{})
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Should NOT contain synthetic closing message
	if strings.Contains(output, "tool call limit") {
		t.Errorf("unexpected synthetic closing message in normal exit: %s", output)
	}

	// Last message should be the real "Done." assistant message
	lastMsg := sess.LastMessage()
	if lastMsg == nil || lastMsg.Content != "Done." {
		t.Errorf("expected last message to be 'Done.', got %+v", lastMsg)
	}
}

func TestProcessToolError(t *testing.T) {
	
	mc := newMockClient()

	// First call: LLM calls a non-existent tool (will error)
	resp1 := &llm.ChatResponse{
		Content: "",
		ToolCalls: []llm.ToolCall{
			{ID: "call_err", Type: "function"},
		},
	}
	resp1.ToolCalls[0].Function.Name = "nonexistent_tool"
	resp1.ToolCalls[0].Function.Arguments = `{}`
	mc.QueueResponse(resp1)

	// Second call: LLM recovers
	mc.QueueResponse(&llm.ChatResponse{
		Content:   "I apologize for the error.",
		ToolCalls: nil,
	})

	agent := setupAgent(t, mc, 8192, 0.70, 10, 20, 4096, true, true)
	sess := &session.Session{
		ChannelID: "test-channel",
		Messages:  nil,
	}

	output, err := agent.Process(context.Background(), sess, "Do something", "You are helpful.", session.ImageAttachment{})
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Tool error should be in output
	if !strings.Contains(output, "unknown tool") {
		t.Errorf("expected tool error in output, got: %s", output)
	}
	if !strings.Contains(output, "I apologize for the error.") {
		t.Errorf("expected recovery text in output, got: %s", output)
	}
}

func TestProcessLLMError(t *testing.T) {
	
	mc := newMockClient()
	mc.QueueError(fmt.Errorf("connection refused"))

	agent := setupAgent(t, mc, 8192, 0.70, 10, 20, 4096, true, true)
	sess := &session.Session{
		ChannelID: "test-channel",
		Messages:  nil,
	}

	_, err := agent.Process(context.Background(), sess, "Hello", "You are helpful.", session.ImageAttachment{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("expected 'connection refused' in error, got: %v", err)
	}

	// Only user message in session — no partial response to record
	if len(sess.Messages) != 1 {
		t.Errorf("expected 1 message (user only), got %d", len(sess.Messages))
	}
}

func TestProcessPartialResponse(t *testing.T) {
	
	mc := newMockClient()
	mc.QueuePartial(&llm.ChatResponse{
		Content:          "This is a partial ",
		ReasoningContent: "thinking about it",
		ToolCalls:        nil,
	}, fmt.Errorf("connection reset — partial response"))

	agent := setupAgent(t, mc, 8192, 0.70, 10, 20, 4096, true, true)
	sess := &session.Session{
		ChannelID: "test-channel",
		Messages:  nil,
	}

	output, err := agent.Process(context.Background(), sess, "Hello", "You are helpful.", session.ImageAttachment{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "partial response") {
		t.Errorf("expected 'partial response' in error, got: %v", err)
	}

	// Partial content should be in output
	if !strings.Contains(output, "This is a partial ") {
		t.Errorf("expected partial content in output, got: %s", output)
	}
	if !strings.Contains(output, "thinking about it") {
		t.Errorf("expected reasoning in output, got: %s", output)
	}

	// Session should have: user message + partial assistant message
	if len(sess.Messages) != 2 {
		t.Fatalf("expected 2 messages (user + partial assistant), got %d", len(sess.Messages))
	}
	if sess.Messages[1].Role != session.RoleAssistant {
		t.Errorf("expected assistant role, got %s", sess.Messages[1].Role)
	}
	if sess.Messages[1].Content != "This is a partial " {
		t.Errorf("expected partial content, got %q", sess.Messages[1].Content)
	}
	if sess.Messages[1].ReasoningContent != "thinking about it" {
		t.Errorf("expected partial reasoning, got %q", sess.Messages[1].ReasoningContent)
	}
}

func TestSummarizationTriggersAtThreshold(t *testing.T) {
	
	mc := newMockClient()

	// First call: summarization LLM call
	mc.QueueResponse(&llm.ChatResponse{
		Content:   "## Summary\n\nTask: count to three.\nCompleted: counted to one.\n",
		ToolCalls: nil,
	})

	// Second call: main LLM call after summarization
	mc.QueueResponse(&llm.ChatResponse{
		Content:   "Short answer.",
		ToolCalls: nil,
	})

	// Very small context window (100 tokens = ~400 chars)
	agent := setupAgent(t, mc, 100, 0.90, 2, 20, 4096, true, true)
	sess := &session.Session{
		ChannelID: "test-channel",
		Messages:  nil,
	}

	// Add messages that exceed the context window
	longText := strings.Repeat("x", 500)
	sess.Messages = append(sess.Messages, session.ConversationMessage{
		Role:    session.RoleUser,
		Content: longText,
	})
	sess.Messages = append(sess.Messages, session.ConversationMessage{
		Role:    session.RoleAssistant,
		Content: strings.Repeat("y", 500),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := agent.Process(ctx, sess, "New message", "System.", session.ImageAttachment{})
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Session should have: summary message + kept recent messages + new user + final assistant
	// The old long messages should be replaced by the summary
	hasSummary := false
	for _, msg := range sess.Messages {
		if msg.ReasoningContent != "" && strings.Contains(msg.ReasoningContent, "[Summary of prior conversation]") {
			hasSummary = true
			break
		}
	}
	if !hasSummary {
		t.Error("expected summary message in session")
	}

	// The old long messages should not be present directly
	for _, msg := range sess.Messages {
		if msg.Content == longText {
			t.Error("expected old long message to be replaced by summary")
		}
	}
}

func TestSummarizationFailure(t *testing.T) {
	
	mc := newMockClient()

	// First call: summarization LLM call fails
	mc.QueueError(fmt.Errorf("context summarization LLM error"))

	agent := setupAgent(t, mc, 100, 0.90, 2, 20, 4096, true, true)
	sess := &session.Session{
		ChannelID: "test-channel",
		Messages:  nil,
	}

	// Add messages that exceed the context window
	longText := strings.Repeat("x", 500)
	sess.Messages = append(sess.Messages, session.ConversationMessage{
		Role:    session.RoleUser,
		Content: longText,
	})
	sess.Messages = append(sess.Messages, session.ConversationMessage{
		Role:    session.RoleAssistant,
		Content: strings.Repeat("y", 500),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := agent.Process(ctx, sess, "New message", "System.", session.ImageAttachment{})
	if err == nil {
		t.Fatal("expected error from summarization failure, got nil")
	}
	if !strings.Contains(err.Error(), "context summarization failed") {
		t.Errorf("expected 'context summarization failed' in error, got: %v", err)
	}

	// Session should have a tool message recording the failure
	hasFailureMsg := false
	for _, msg := range sess.Messages {
		if msg.Role == session.RoleTool && strings.Contains(msg.Content, "context summarization failed") {
			hasFailureMsg = true
			break
		}
	}
	if !hasFailureMsg {
		t.Error("expected tool message recording summarization failure")
	}
}

func TestSummarizationSkippedWhenUnderThreshold(t *testing.T) {
	
	mc := newMockClient()
	mc.QueueResponse(&llm.ChatResponse{
		Content:   "Done.",
		ToolCalls: nil,
	})

	// Large context window — summarization should not trigger
	agent := setupAgent(t, mc, 100000, 0.70, 10, 20, 4096, true, true)
	sess := &session.Session{
		ChannelID: "test-channel",
		Messages:  nil,
	}

	_, err := agent.Process(context.Background(), sess, "Hello", "System.", session.ImageAttachment{})
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Only one LLM call should have been made (no summarization)
	if mc.callCount != 1 {
		t.Errorf("expected 1 LLM call (no summarization), got %d", mc.callCount)
	}
}

func TestSplitMessages(t *testing.T) {
	msgs := make([]session.ConversationMessage, 5)
	for i := range msgs {
		msgs[i] = session.ConversationMessage{
			Role:    session.RoleUser,
			Content: fmt.Sprintf("msg-%d", i),
		}
	}

	old, recent := splitMessages(msgs, 2)

	if len(old) != 3 {
		t.Errorf("expected 3 old messages, got %d", len(old))
	}
	if len(recent) != 2 {
		t.Errorf("expected 2 recent messages, got %d", len(recent))
	}
	if recent[0].Content != "msg-3" {
		t.Errorf("expected recent[0] to be msg-3, got %s", recent[0].Content)
	}
	if recent[1].Content != "msg-4" {
		t.Errorf("expected recent[1] to be msg-4, got %s", recent[1].Content)
	}
}

func TestSplitMessagesKeepZero(t *testing.T) {
	msgs := make([]session.ConversationMessage, 3)
	for i := range msgs {
		msgs[i] = session.ConversationMessage{
			Role:    session.RoleUser,
			Content: fmt.Sprintf("msg-%d", i),
		}
	}

	old, recent := splitMessages(msgs, 0)

	if len(old) != 3 {
		t.Errorf("expected 3 old messages, got %d", len(old))
	}
	if len(recent) != 0 {
		t.Errorf("expected 0 recent messages, got %d", len(recent))
	}
}

func TestSplitMessagesKeepAll(t *testing.T) {
	msgs := make([]session.ConversationMessage, 3)
	for i := range msgs {
		msgs[i] = session.ConversationMessage{
			Role:    session.RoleUser,
			Content: fmt.Sprintf("msg-%d", i),
		}
	}

	old, recent := splitMessages(msgs, 10)

	if len(old) != 3 {
		t.Errorf("expected 3 old messages (keep >= len means all old), got %d", len(old))
	}
	if recent != nil {
		t.Errorf("expected nil recent, got %d messages", len(recent))
	}
}

func TestSystemPromptPreserved(t *testing.T) {
	
	mc := newMockClient()
	mc.QueueResponse(&llm.ChatResponse{
		Content:   "Done.",
		ToolCalls: nil,
	})

	agent := setupAgent(t, mc, 8192, 0.70, 10, 20, 4096, true, true)
	sess := &session.Session{ChannelID: "test", Messages: nil}

	_, err := agent.Process(context.Background(), sess, "Hi", "You are a robot.", session.ImageAttachment{})
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Verify the last LLM call included the system prompt
	lastMsgs := mc.LastMessages()
	if len(lastMsgs) == 0 {
		t.Fatal("no messages sent to LLM")
	}
	if lastMsgs[0].Role != "system" {
		t.Errorf("expected role 'system', got %q", lastMsgs[0].Role)
	}
	var sysContent string
	if err := json.Unmarshal(lastMsgs[0].Content, &sysContent); err != nil {
		t.Fatalf("unmarshal system content: %v", err)
	}
	if sysContent != "You are a robot." {
		t.Errorf("expected 'You are a robot.', got %q", sysContent)
	}
}

func TestMultipleToolCallsInOneTurn(t *testing.T) {
	
	mc := newMockClient()

	// First call: LLM calls echo twice
	resp1 := &llm.ChatResponse{
		Content: "",
		ToolCalls: []llm.ToolCall{
			{ID: "call_a", Type: "function"},
			{ID: "call_b", Type: "function"},
		},
	}
	resp1.ToolCalls[0].Function.Name = "echo"
	resp1.ToolCalls[0].Function.Arguments = `{"text":"first"}`
	resp1.ToolCalls[1].Function.Name = "echo"
	resp1.ToolCalls[1].Function.Arguments = `{"text":"second"}`
	mc.QueueResponse(resp1)

	// Second call: LLM finishes
	mc.QueueResponse(&llm.ChatResponse{
		Content:   "Both done.",
		ToolCalls: nil,
	})

	agent := setupAgent(t, mc, 8192, 0.70, 10, 20, 4096, true, true)
	sess := &session.Session{ChannelID: "test", Messages: nil}

	output, err := agent.Process(context.Background(), sess, "Echo twice", "You are helpful.", session.ImageAttachment{})
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Both tool results should be in output
	if !strings.Contains(output, "[Result: first]") {
		t.Errorf("missing first result in output: %s", output)
	}
	if !strings.Contains(output, "[Result: second]") {
		t.Errorf("missing second result in output: %s", output)
	}
	if !strings.Contains(output, "Both done.") {
		t.Errorf("missing final answer: %s", output)
	}
}

func TestProcessEmptyContentResponse(t *testing.T) {
	
	mc := newMockClient()
	mc.QueueResponse(&llm.ChatResponse{
		Content:   "",
		ToolCalls: nil,
	})

	agent := setupAgent(t, mc, 8192, 0.70, 10, 20, 4096, true, true)
	sess := &session.Session{ChannelID: "test", Messages: nil}

	output, err := agent.Process(context.Background(), sess, "Silent", "You are helpful.", session.ImageAttachment{})
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Empty content is valid — no tool calls, no text
	if output != "" {
		t.Errorf("expected empty output, got %q", output)
	}
}

func TestProcessWithImageAttachment(t *testing.T) {
	
	mc := newMockClient()
	mc.QueueResponse(&llm.ChatResponse{
		Content:   "I see a photo of a cat.",
		ToolCalls: nil,
	})

	agent := setupAgent(t, mc, 8192, 0.70, 10, 20, 4096, true, true)
	sess := &session.Session{
		ChannelID: "test-channel",
		Messages:  nil,
	}

	att := session.ImageAttachment{Data: "iVBORw0KGgo=", MIMEType: "image/png"}
	output, err := agent.Process(context.Background(), sess, "what is this?", "You are helpful.", att)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if !strings.Contains(output, "I see a photo of a cat.") {
		t.Errorf("expected output to contain answer, got: %s", output)
	}

	// User message should have the attachment
	if len(sess.Messages) < 1 {
		t.Fatal("expected at least 1 message in session")
	}
	if len(sess.Messages[0].Attachments) != 1 {
		t.Errorf("expected 1 attachment on user message, got %d", len(sess.Messages[0].Attachments))
	}
	if sess.Messages[0].Attachments[0].Data != "iVBORw0KGgo=" {
		t.Errorf("attachment data mismatch: %q", sess.Messages[0].Attachments[0].Data)
	}

	// Verify the LLM received a multimodal message
	lastMsgs := mc.LastMessages()
	if len(lastMsgs) < 2 {
		t.Fatal("expected at least 2 LLM messages (system + user)")
	}
	// User message content should be a content-parts array (json.RawMessage)
	var parts []map[string]interface{}
	if err := json.Unmarshal(lastMsgs[1].Content, &parts); err != nil {
		t.Fatalf("expected user message content to be a content-parts array, got: %s", string(lastMsgs[1].Content))
	}
	if len(parts) != 2 {
		t.Errorf("expected 2 content parts (text + image), got %d", len(parts))
	}
	if parts[0]["type"] != "text" {
		t.Errorf("expected first part type 'text', got %v", parts[0]["type"])
	}
	if parts[1]["type"] != "image_url" {
		t.Errorf("expected second part type 'image_url', got %v", parts[1]["type"])
	}
}
