package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/agent-project/harness/sandbox"
)

// MessageRole is the role of a message in the conversation.
type MessageRole string

const (
	RoleUser     MessageRole = "user"
	RoleAssistant MessageRole = "assistant"
	RoleTool     MessageRole = "tool"
)

// ConversationMessage is a single message in the conversation history.
type ConversationMessage struct {
	Role              MessageRole `json:"role"`
	Content           string      `json:"content"`
	ReasoningContent  string      `json:"reasoning_content,omitempty"`
	ToolCalls         []ToolCall  `json:"tool_calls,omitempty"`
	ToolCallID        string      `json:"tool_call_id,omitempty"`
}

// ToolCall represents a tool invocation request from the LLM.
type ToolCall struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// Session holds the conversation state for a single channel.
type Session struct {
	ChannelID string              `json:"channel_id"`
	Messages  []ConversationMessage `json:"messages"`
	CreatedAt time.Time          `json:"created_at"`
	UpdatedAt time.Time          `json:"updated_at"`
}

// Manager handles loading and saving session state files.
type Manager struct {
	mu       sync.Mutex
	stateDir string
	sessions map[string]*Session
}

// NewManager creates a new session manager for the given state directory.
func NewManager(stateDir string) *Manager {
	return &Manager{
		stateDir: stateDir,
		sessions: make(map[string]*Session),
	}
}

// LoadAll reads all session files from the state directory.
func (m *Manager) LoadAll() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := os.MkdirAll(m.stateDir, 0755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}

	entries, err := os.ReadDir(m.stateDir)
	if err != nil {
		return fmt.Errorf("read state dir: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if filepath.Ext(name) != ".json" {
			continue
		}

		path := filepath.Join(m.stateDir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read session %s: %w", name, err)
		}

		var s Session
		if err := json.Unmarshal(data, &s); err != nil {
			return fmt.Errorf("parse session %s: %w", name, err)
		}

		m.sessions[s.ChannelID] = &s
	}

	return nil
}

// Get returns the session for the given channel, creating a new one if it doesn't exist.
// Returns nil if the channel ID contains a null byte (rejected for safety).
func (m *Manager) Get(channelID string) *Session {
	if strings.Contains(channelID, "\x00") {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.sessions[channelID]
	if !ok {
		now := time.Now()
		s = &Session{
			ChannelID: channelID,
			Messages:  nil,
			CreatedAt: now,
			UpdatedAt: now,
		}
		m.sessions[channelID] = s
	}

	return s
}

// saveFile writes a session to disk atomically. Caller must NOT hold m.mu.
func (m *Manager) saveFile(s *Session) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}

	if err := os.MkdirAll(m.stateDir, 0755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}

	safeName := sandbox.SanitizeFilename(s.ChannelID)
	tmpPath := filepath.Join(m.stateDir, safeName+".json.tmp")
	finalPath := filepath.Join(m.stateDir, safeName+".json")

	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("write tmp file: %w", err)
	}

	if err := os.Rename(tmpPath, finalPath); err != nil {
		return fmt.Errorf("rename to final: %w", err)
	}

	return nil
}

// Save persists a session to disk atomically (write to .tmp, then rename).
// The passed session is used as the source of truth; it is synced back into
// the internal map before persistence.
func (m *Manager) Save(s *Session) error {
	m.mu.Lock()
	if _, ok := m.sessions[s.ChannelID]; !ok {
		m.mu.Unlock()
		return fmt.Errorf("session %s not found", s.ChannelID)
	}
	m.sessions[s.ChannelID] = s
	s.UpdatedAt = time.Now()
	m.mu.Unlock()

	return m.saveFile(s)
}

// SaveAll persists all in-memory sessions to disk.
func (m *Manager) SaveAll() error {
	m.mu.Lock()
	snapshot := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		snapshot = append(snapshot, s)
	}
	m.mu.Unlock()

	for _, s := range snapshot {
		s.UpdatedAt = time.Now()
		if err := m.saveFile(s); err != nil {
			return err
		}
	}

	return nil
}

// DrainAndSave appends a pending inbound message to the session for the given
// channel (creating it if needed), then atomically persists the session to disk.
// This is used during graceful shutdown to flush queued messages without calling the LLM.
func (m *Manager) DrainAndSave(channelID, messageText string) error {
	m.mu.Lock()
	s, ok := m.sessions[channelID]
	if !ok {
		now := time.Now()
		s = &Session{
			ChannelID: channelID,
			Messages:  nil,
			CreatedAt: now,
			UpdatedAt: now,
		}
		m.sessions[channelID] = s
	}
	s.Messages = append(s.Messages, ConversationMessage{
		Role:    RoleUser,
		Content: messageText,
	})
	s.UpdatedAt = time.Now()

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		m.mu.Unlock()
		return fmt.Errorf("marshal session: %w", err)
	}
	m.mu.Unlock()

	if err := os.MkdirAll(m.stateDir, 0755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}

	safeName := sandbox.SanitizeFilename(channelID)
	tmpPath := filepath.Join(m.stateDir, safeName+".json.tmp")
	finalPath := filepath.Join(m.stateDir, safeName+".json")

	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("write tmp file: %w", err)
	}

	if err := os.Rename(tmpPath, finalPath); err != nil {
		return fmt.Errorf("rename to final: %w", err)
	}

	return nil
}

// LastMessage returns the last message in the session, or nil if empty.
func (s *Session) LastMessage() *ConversationMessage {
	if len(s.Messages) == 0 {
		return nil
	}
	return &s.Messages[len(s.Messages)-1]
}

// Exists checks if a session exists for the given channel.
func (m *Manager) Exists(channelID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.sessions[channelID]
	return ok
}

// Count returns the number of loaded sessions.
func (m *Manager) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sessions)
}
