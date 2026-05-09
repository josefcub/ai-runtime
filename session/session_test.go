package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func tmpDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "session-test-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func TestCreateNewSession(t *testing.T) {
	m := NewManager(tmpDir(t))

	s := m.Get("ch1")
	if s.ChannelID != "ch1" {
		t.Errorf("expected channel ch1, got %s", s.ChannelID)
	}
	if s.CreatedAt.IsZero() {
		t.Error("CreatedAt should be set")
	}
	if s.UpdatedAt.IsZero() {
		t.Error("UpdatedAt should be set")
	}
	if s.Messages != nil {
		t.Error("new session should have nil messages")
	}
}

func TestGetOrCreate(t *testing.T) {
	m := NewManager(tmpDir(t))

	// First call creates
	s1 := m.Get("ch1")
	// Second call returns same session
	s2 := m.Get("ch1")
	if s1 != s2 {
		t.Error("Get should return the same session instance")
	}
}

func TestSaveLoadRoundtrip(t *testing.T) {
	dir := tmpDir(t)
	m := NewManager(dir)

	s := m.Get("ch1")
	s.Messages = []ConversationMessage{
		{Role: RoleUser, Content: "hello"},
		{Role: RoleAssistant, Content: "hi there"},
	}

	if err := m.Save(s); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	// Load in a new manager
	m2 := NewManager(dir)
	if err := m2.LoadAll(); err != nil {
		t.Fatalf("load failed: %v", err)
	}

	s2 := m2.Get("ch1")
	if len(s2.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(s2.Messages))
	}
	if s2.Messages[0].Content != "hello" {
		t.Errorf("expected 'hello', got %q", s2.Messages[0].Content)
	}
	if s2.Messages[1].Content != "hi there" {
		t.Errorf("expected 'hi there', got %q", s2.Messages[1].Content)
	}
}

func TestAtomicWrite(t *testing.T) {
	dir := tmpDir(t)
	m := NewManager(dir)

	s := m.Get("ch1")
	s.Messages = []ConversationMessage{{Role: RoleUser, Content: "test"}}

	if err := m.Save(s); err != nil {
		t.Fatal(err)
	}

	// Verify no .tmp file remains
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("temporary file should not remain: %s", e.Name())
		}
	}
}

func TestSessionJSONFormat(t *testing.T) {
	dir := tmpDir(t)
	m := NewManager(dir)

	s := m.Get("slack:abc123")
	s.Messages = []ConversationMessage{
		{Role: RoleUser, Content: "start"},
		{Role: RoleAssistant, Content: "reply", ToolCalls: []ToolCall{
			{ID: "call_1", Type: "function", Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{Name: "view", Arguments: `{"path":"foo.txt"}`}},
		}},
		{Role: RoleTool, Content: "file contents", ToolCallID: "call_1"},
	}

	if err := m.Save(s); err != nil {
		t.Fatal(err)
	}

	// Read raw JSON and verify structure
	data, err := os.ReadFile(filepath.Join(dir, "slack:abc123.json"))
	if err != nil {
		t.Fatal(err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}

	if raw["channel_id"] != "slack:abc123" {
		t.Errorf("wrong channel_id: %v", raw["channel_id"])
	}
	if _, ok := raw["created_at"]; !ok {
		t.Error("missing created_at")
	}
	if _, ok := raw["updated_at"]; !ok {
		t.Error("missing updated_at")
	}
}

func TestSystemPromptNotStored(t *testing.T) {
	dir := tmpDir(t)
	m := NewManager(dir)

	s := m.Get("ch1")
	s.Messages = []ConversationMessage{{Role: RoleUser, Content: "test"}}

	if err := m.Save(s); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "ch1.json"))
	if err != nil {
		t.Fatal(err)
	}

	// Session file should not contain any system prompt field
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}

	if _, hasSystem := raw["system_prompt"]; hasSystem {
		t.Error("session file should not contain system_prompt")
	}
}

func TestLoadAllIgnoresNonJSON(t *testing.T) {
	dir := tmpDir(t)
	m := NewManager(dir)

	// Create a non-JSON file
	os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("hello"), 0644)

	// Create a valid session
	s := m.Get("ch1")
	m.Save(s)

	// Load should succeed and only load the session
	m2 := NewManager(dir)
	if err := m2.LoadAll(); err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}

	if m2.Count() != 1 {
		t.Errorf("expected 1 session, got %d", m2.Count())
	}
}

func TestLoadAllEmptyDir(t *testing.T) {
	m := NewManager(tmpDir(t))
	if err := m.LoadAll(); err != nil {
		t.Fatalf("LoadAll on empty dir failed: %v", err)
	}
	if m.Count() != 0 {
		t.Errorf("expected 0 sessions, got %d", m.Count())
	}
}

func TestSaveAll(t *testing.T) {
	dir := tmpDir(t)
	m := NewManager(dir)

	// Create multiple sessions
	for i := 0; i < 3; i++ {
		s := m.Get("ch" + string(rune('a'+i)))
		s.Messages = []ConversationMessage{{Role: RoleUser, Content: "msg"}}
	}

	if err := m.SaveAll(); err != nil {
		t.Fatalf("SaveAll failed: %v", err)
	}

	// Verify all files exist
	for i := 0; i < 3; i++ {
		ch := "ch" + string(rune('a'+i))
		if _, err := os.Stat(filepath.Join(dir, ch+".json")); err != nil {
			t.Errorf("session file missing for %s: %v", ch, err)
		}
	}
}

func TestExists(t *testing.T) {
	m := NewManager(tmpDir(t))

	if m.Exists("ch1") {
		t.Error("ch1 should not exist yet")
	}

	m.Get("ch1")
	if !m.Exists("ch1") {
		t.Error("ch1 should exist after Get")
	}
}

func TestUpdatedAt(t *testing.T) {
	dir := tmpDir(t)
	m := NewManager(dir)

	s := m.Get("ch1")
	original := s.UpdatedAt
	time.Sleep(10 * time.Millisecond)

	if err := m.Save(s); err != nil {
		t.Fatal(err)
	}

	// Load and check UpdatedAt was updated
	m2 := NewManager(dir)
	m2.LoadAll()
	s2 := m2.Get("ch1")

	if s2.UpdatedAt.Equal(original) {
		t.Error("UpdatedAt should be newer after save")
	}
}

func TestSaveNonExistentSession(t *testing.T) {
	dir := tmpDir(t)
	m := NewManager(dir)

	fake := &Session{ChannelID: "nope", Messages: []ConversationMessage{{}}}
	err := m.Save(fake)
	if err == nil {
		t.Error("expected error saving non-existent session")
	}
}

func TestConcurrentAccess(t *testing.T) {
	dir := tmpDir(t)
	m := NewManager(dir)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			ch := "ch" + string(rune('a'+id))
			s := m.Get(ch)
			s.Messages = append(s.Messages, ConversationMessage{
				Role:    RoleUser,
				Content: "concurrent",
			})
			m.Save(s)
		}(i)
	}

	wg.Wait()

	// All sessions should be persisted
	m2 := NewManager(dir)
	if err := m2.LoadAll(); err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}
	if m2.Count() != 10 {
		t.Errorf("expected 10 sessions, got %d", m2.Count())
	}
}

func TestConversationMessageJSON(t *testing.T) {
	msg := ConversationMessage{
		Role:    RoleAssistant,
		Content: "result",
		ToolCalls: []ToolCall{
			{ID: "call_1", Type: "function", Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{Name: "grep", Arguments: `{"pattern":"test"}`}},
		},
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}

	var parsed ConversationMessage
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}

	if parsed.Role != RoleAssistant {
		t.Errorf("expected RoleAssistant, got %s", parsed.Role)
	}
	if len(parsed.ToolCalls) != 1 {
		t.Errorf("expected 1 tool call, got %d", len(parsed.ToolCalls))
	}
}

func TestToolMessageJSON(t *testing.T) {
	msg := ConversationMessage{
		Role:       RoleTool,
		Content:    "file found",
		ToolCallID: "call_abc",
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}

	var parsed ConversationMessage
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}

	if parsed.ToolCallID != "call_abc" {
		t.Errorf("expected tool_call_id=call_abc, got %s", parsed.ToolCallID)
	}
}

func TestLoadMultipleSessions(t *testing.T) {
	dir := tmpDir(t)
	m := NewManager(dir)

	// Create and save sessions
	channels := []string{"slack:a", "slack:b", "telegram:c"}
	for _, ch := range channels {
		s := m.Get(ch)
		s.Messages = []ConversationMessage{{Role: RoleUser, Content: "hello"}}
		m.Save(s)
	}

	// Load in fresh manager
	m2 := NewManager(dir)
	if err := m2.LoadAll(); err != nil {
		t.Fatal(err)
	}

	if m2.Count() != 3 {
		t.Errorf("expected 3 sessions, got %d", m2.Count())
	}
}

func TestDrainAndSave_NewChannel(t *testing.T) {
	dir := tmpDir(t)
	m := NewManager(dir)

	err := m.DrainAndSave("drain:ch1", "shutdown message")
	if err != nil {
		t.Fatalf("DrainAndSave failed: %v", err)
	}

	// Verify via load
	m2 := NewManager(dir)
	if err := m2.LoadAll(); err != nil {
		t.Fatal(err)
	}
	s := m2.Get("drain:ch1")
	if len(s.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(s.Messages))
	}
	if s.Messages[0].Role != RoleUser {
		t.Errorf("expected RoleUser, got %s", s.Messages[0].Role)
	}
	if s.Messages[0].Content != "shutdown message" {
		t.Errorf("expected 'shutdown message', got %q", s.Messages[0].Content)
	}
}

func TestDrainAndSave_AppendExisting(t *testing.T) {
	dir := tmpDir(t)
	m := NewManager(dir)

	// Create session with existing message
	s := m.Get("drain:ch2")
	s.Messages = []ConversationMessage{{Role: RoleUser, Content: "original"}}
	m.Save(s)

	// Drain append
	err := m.DrainAndSave("drain:ch2", "pending message")
	if err != nil {
		t.Fatalf("DrainAndSave failed: %v", err)
	}

	// Verify both messages exist
	m2 := NewManager(dir)
	if err := m2.LoadAll(); err != nil {
		t.Fatal(err)
	}
	s2 := m2.Get("drain:ch2")
	if len(s2.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(s2.Messages))
	}
	if s2.Messages[0].Content != "original" {
		t.Errorf("first message mismatch: %q", s2.Messages[0].Content)
	}
	if s2.Messages[1].Content != "pending message" {
		t.Errorf("second message mismatch: %q", s2.Messages[1].Content)
	}
}

func TestDrainAndSave_AtomicWrite(t *testing.T) {
	dir := tmpDir(t)
	m := NewManager(dir)

	m.DrainAndSave("drain:ch3", "test")

	// No .tmp file should remain
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("temporary file should not remain: %s", e.Name())
		}
	}
}

func TestDrainAndSave_MultipleChannels(t *testing.T) {
	dir := tmpDir(t)
	m := NewManager(dir)

	m.DrainAndSave("a", "msg for a")
	m.DrainAndSave("b", "msg for b")
	m.DrainAndSave("a", "second for a")

	m2 := NewManager(dir)
	m2.LoadAll()

	sA := m2.Get("a")
	sB := m2.Get("b")

	if len(sA.Messages) != 2 {
		t.Errorf("expected 2 messages for a, got %d", len(sA.Messages))
	}
	if len(sB.Messages) != 1 {
		t.Errorf("expected 1 message for b, got %d", len(sB.Messages))
	}
}

func TestSessionLoopTimeout(t *testing.T) {
	done := make(chan struct{})
	go func() {
		dir := tmpDir(t)
		m := NewManager(dir)
		for i := 0; i < 10000; i++ {
			s := m.Get("ch")
			s.Messages = append(s.Messages, ConversationMessage{
				Role:    RoleUser,
				Content: "x",
			})
		}
		close(done)
	}()

	select {
	case <-done:
		// OK
	case <-time.After(2 * time.Second):
		t.Fatal("session operations exceeded 2s timeout")
	}
}

func TestSanitizeChannelID(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"ch1", "ch1"},
		{"slack:abc123", "slack:abc123"},            // colons preserved
		{"slack:/some/path", "slack:_some_path"},     // forward slashes
		{"foo\\bar\\baz", "foo_bar_baz"},             // backslashes
		{"a*b?c\"d<e>f|g", "a_b_c_d_e_f_g"},         // Windows reserved
		{"a/b/c", "a_b_c"},                           // nested slashes
		{"../escape", ".._escape"},                  // parent traversal — / removed so .. is harmless
		{"foo\x00bar", "foo_bar"},                    // null byte
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := sanitizeChannelID(tt.input)
			if result != tt.expected {
				t.Errorf("sanitizeChannelID(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestSaveWithPathSeparators(t *testing.T) {
	dir := tmpDir(t)
	m := NewManager(dir)

	// Channel ID with path separators — should sanitize on disk
	s := m.Get("a/b/c")
	if s == nil {
		t.Fatal("Get returned nil for path-separator channel ID")
	}
	s.Messages = []ConversationMessage{{Role: RoleUser, Content: "test"}}

	if err := m.Save(s); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	// File should be flat (no subdirectories), with sanitized name
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	found := false
	for _, e := range entries {
		if e.IsDir() {
			t.Errorf("unexpected subdirectory created: %s", e.Name())
		}
		if e.Name() == "a_b_c.json" {
			found = true
		}
	}
	if !found {
		t.Error("expected sanitized file a_b_c.json not found")
	}
}

func TestSaveLoadWithSpecialChars(t *testing.T) {
	dir := tmpDir(t)
	m := NewManager(dir)

	// Save session with slashes and backslashes in channel ID
	s := m.Get("foo/bar\\baz")
	s.Messages = []ConversationMessage{{Role: RoleUser, Content: "hello"}}
	if err := m.Save(s); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	// Load in fresh manager — channel ID from JSON body should be preserved
	m2 := NewManager(dir)
	if err := m2.LoadAll(); err != nil {
		t.Fatalf("load failed: %v", err)
	}

	// Get by the ORIGINAL channel ID
	s2 := m2.Get("foo/bar\\baz")
	if len(s2.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(s2.Messages))
	}
	if s2.ChannelID != "foo/bar\\baz" {
		t.Errorf("channel ID not preserved: got %q", s2.ChannelID)
	}
}

func TestGetRejectsNullByte(t *testing.T) {
	m := NewManager(tmpDir(t))

	s := m.Get("foo\x00bar")
	if s != nil {
		t.Error("Get should return nil for channel ID with null byte")
	}
}

func TestDrainAndSaveWithSlashes(t *testing.T) {
	dir := tmpDir(t)
	m := NewManager(dir)

	err := m.DrainAndSave("a/b/c", "shutdown msg")
	if err != nil {
		t.Fatalf("DrainAndSave failed: %v", err)
	}

	// No subdirectories should be created
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() {
			t.Errorf("unexpected subdirectory: %s", e.Name())
		}
	}

	// File should exist with sanitized name
	if _, err := os.Stat(filepath.Join(dir, "a_b_c.json")); err != nil {
		t.Fatalf("expected a_b_c.json, got error: %v", err)
	}
}
