package mockllm

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/agent-project/harness/agent"
	"github.com/agent-project/harness/llm"
)

var _ agent.ChatClient = (*MockClient)(nil)

type responseItem struct {
	resp      *llm.ChatResponse
	err       error
	isPartial bool
}

type MockClient struct {
	mu           sync.Mutex
	cond         *sync.Cond
	q            []responseItem
	callCount    int
	lastMessages []llm.Message
	lastTools    json.RawMessage
	lastMaxTokens int
}

func NewMockClient() *MockClient {
	mc := &MockClient{}
	mc.cond = sync.NewCond(&mc.mu)
	return mc
}

func (m *MockClient) QueueResp(resp *llm.ChatResponse) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.q = append(m.q, responseItem{resp: resp})
	m.cond.Signal()
}

func (m *MockClient) QueueError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.q = append(m.q, responseItem{err: err})
	m.cond.Signal()
}

func (m *MockClient) QueuePartial(resp *llm.ChatResponse, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.q = append(m.q, responseItem{resp: resp, err: err, isPartial: true})
	m.cond.Signal()
}

func (m *MockClient) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.callCount
}

func (m *MockClient) LastMessages() []llm.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.lastMessages) == 0 {
		return nil
	}
	out := make([]llm.Message, len(m.lastMessages))
	for i, msg := range m.lastMessages {
		out[i] = llm.Message{
			Role:    msg.Role,
			Content: make(json.RawMessage, len(msg.Content)),
		}
		copy(out[i].Content, msg.Content)
	}
	return out
}

func (m *MockClient) LastTools() json.RawMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.lastTools == nil {
		return nil
	}
	return m.lastTools
}

func (m *MockClient) LastMaxTokens() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastMaxTokens
}

func (m *MockClient) Chat(ctx context.Context, messages []llm.Message, toolsJSON json.RawMessage, maxTokens int) (*llm.ChatResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.callCount++
	m.lastMessages = messages
	m.lastTools = toolsJSON
	m.lastMaxTokens = maxTokens

	for len(m.q) == 0 {
		m.cond.Wait()
		if ctx != nil && ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}

	item := m.q[0]
	m.q = m.q[1:]

	if item.isPartial {
		if item.err != nil {
			return item.resp, item.err
		}
		return item.resp, fmt.Errorf("partial response")
	}

	return item.resp, item.err
}
