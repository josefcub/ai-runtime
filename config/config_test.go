package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeTestINI(t *testing.T, content string) string {
	t.Helper()
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.ini")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test ini: %v", err)
	}
	return path
}

func TestLoadFullConfig(t *testing.T) {
	content := `
[server]
host = 0.0.0.0
port = 9090
webhook_path = "/api/hook"

[llm]
endpoint = "http://localhost:8080/v1"
model = "llama-3.1-8b"
api_key = "sk-test-key"
context_tokens = 4096
max_tokens = 2048
timeout = 60
max_tool_iterations = 10
system_prompt = "You are a test assistant."

[queue]
max_depth = 32

[paths]
working_dir = ./sandbox/
log_dir = ./logs/
state_dir = ./state/

[logging]
level = debug
log_tool_calls = true
log_agent_reasoning = true
log_channel_events = true
`
	path := writeTestINI(t, content)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Validate server
	if cfg.Server.Host != "0.0.0.0" {
		t.Errorf("host = %q, want 0.0.0.0", cfg.Server.Host)
	}
	if cfg.Server.Port != 9090 {
		t.Errorf("port = %d, want 9090", cfg.Server.Port)
	}
	if cfg.Server.WebhookPath != "/api/hook" {
		t.Errorf("webhook_path = %q, want /api/hook", cfg.Server.WebhookPath)
	}

	// Validate LLM
	if cfg.LLM.Endpoint != "http://localhost:8080/v1" {
		t.Errorf("endpoint = %q, want http://localhost:8080/v1", cfg.LLM.Endpoint)
	}
	if cfg.LLM.Model != "llama-3.1-8b" {
		t.Errorf("model = %q, want llama-3.1-8b", cfg.LLM.Model)
	}
	if cfg.LLM.APIKey != "sk-test-key" {
		t.Errorf("api_key = %q, want sk-test-key", cfg.LLM.APIKey)
	}
	if cfg.LLM.ContextTokens != 4096 {
		t.Errorf("context_tokens = %d, want 4096", cfg.LLM.ContextTokens)
	}
	if cfg.LLM.MaxTokens != 2048 {
		t.Errorf("max_tokens = %d, want 2048", cfg.LLM.MaxTokens)
	}
	if cfg.LLM.Timeout != 60*time.Second {
		t.Errorf("timeout = %v, want 60s", cfg.LLM.Timeout)
	}
	if cfg.LLM.MaxToolIterations != 10 {
		t.Errorf("max_tool_iterations = %d, want 10", cfg.LLM.MaxToolIterations)
	}
	if cfg.LLM.SystemPrompt != "You are a test assistant." {
		t.Errorf("system_prompt = %q, want You are a test assistant.", cfg.LLM.SystemPrompt)
	}

	// Validate queue
	if cfg.Queue.MaxDepth != 32 {
		t.Errorf("max_depth = %d, want 32", cfg.Queue.MaxDepth)
	}

	// Validate paths
	if cfg.Paths.WorkingDir != "./sandbox/" {
		t.Errorf("working_dir = %q, want ./sandbox/", cfg.Paths.WorkingDir)
	}
	if cfg.Paths.LogDir != "./logs/" {
		t.Errorf("log_dir = %q, want ./logs/", cfg.Paths.LogDir)
	}
	if cfg.Paths.StateDir != "./state/" {
		t.Errorf("state_dir = %q, want ./state/", cfg.Paths.StateDir)
	}

	// Validate logging
	if cfg.Logging.Level != "debug" {
		t.Errorf("level = %q, want debug", cfg.Logging.Level)
	}
	if !cfg.Logging.LogToolCalls {
		t.Error("log_tool_calls should be true")
	}
	if !cfg.Logging.LogAgentReasoning {
		t.Error("log_agent_reasoning should be true")
	}
	if !cfg.Logging.LogChannelEvents {
		t.Error("log_channel_events should be true")
	}

	// Check new summarization defaults
	if cfg.LLM.SummarizeThreshold != 0.70 {
		t.Errorf("summarize_threshold should default to 0.70, got %f", cfg.LLM.SummarizeThreshold)
	}
	if cfg.LLM.SummarizeKeepRecent != 10 {
		t.Errorf("summarize_keep_recent should default to 10, got %d", cfg.LLM.SummarizeKeepRecent)
	}
}

func TestLoadDefaults(t *testing.T) {
	// Minimal config with only required fields
	content := `
[llm]
endpoint = "http://localhost:8080/v1"
model = "test-model"
`
	path := writeTestINI(t, content)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check defaults
	if cfg.Server.Host != "127.0.0.1" {
		t.Errorf("host should default to 127.0.0.1, got %q", cfg.Server.Host)
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("port should default to 8080, got %d", cfg.Server.Port)
	}
	if cfg.Server.WebhookPath != "/webhook" {
		t.Errorf("webhook_path should default to /webhook, got %q", cfg.Server.WebhookPath)
	}
	if cfg.LLM.ContextTokens != 8192 {
		t.Errorf("context_tokens should default to 8192, got %d", cfg.LLM.ContextTokens)
	}
	if cfg.LLM.MaxTokens != 4096 {
		t.Errorf("max_tokens should default to 4096, got %d", cfg.LLM.MaxTokens)
	}
	if cfg.LLM.Timeout != 120*time.Second {
		t.Errorf("timeout should default to 120s, got %v", cfg.LLM.Timeout)
	}
	if cfg.LLM.MaxToolIterations != 20 {
		t.Errorf("max_tool_iterations should default to 20, got %d", cfg.LLM.MaxToolIterations)
	}
	if cfg.LLM.SystemPrompt != "You are a helpful assistant." {
		t.Errorf("system_prompt should default correctly, got %q", cfg.LLM.SystemPrompt)
	}
	if cfg.Queue.MaxDepth != 64 {
		t.Errorf("max_depth should default to 64, got %d", cfg.Queue.MaxDepth)
	}
	if cfg.Paths.WorkingDir != "./work/" {
		t.Errorf("working_dir should default to ./work/, got %q", cfg.Paths.WorkingDir)
	}
	if cfg.Logging.Level != "info" {
		t.Errorf("level should default to info, got %q", cfg.Logging.Level)
	}
	if !cfg.Logging.LogToolCalls {
		t.Error("log_tool_calls should default to true")
	}
	if !cfg.Logging.LogAgentReasoning {
		t.Error("log_agent_reasoning should default to true")
	}
	if !cfg.Logging.LogChannelEvents {
		t.Error("log_channel_events should default to true")
	}
}

func TestLoadMultilineSystemPrompt(t *testing.T) {
	content := `
[llm]
endpoint = "http://localhost:8080/v1"
model = "test-model"
system_prompt = """
You are a helpful assistant.
Always think step by step.
"""
`
	path := writeTestINI(t, content)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "You are a helpful assistant.\nAlways think step by step."
	if cfg.LLM.SystemPrompt != expected {
		t.Errorf("system_prompt = %q, want %q", cfg.LLM.SystemPrompt, expected)
	}
}

func TestValidateMissingEndpoint(t *testing.T) {
	content := `
[llm]
model = "test-model"
`
	path := writeTestINI(t, content)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}

	err = cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for missing endpoint")
	}
	if !strings.Contains(err.Error(), "endpoint") {
		t.Errorf("expected endpoint in error, got: %v", err)
	}
}

func TestValidateMissingModel(t *testing.T) {
	content := `
[llm]
endpoint = "http://localhost:8080/v1"
`
	path := writeTestINI(t, content)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}

	err = cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for missing model")
	}
	if !strings.Contains(err.Error(), "model") {
		t.Errorf("expected model in error, got: %v", err)
	}
}

func TestValidateMissingBoth(t *testing.T) {
	content := `[server]
host = 127.0.0.1
`
	path := writeTestINI(t, content)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}

	err = cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "endpoint") || !strings.Contains(err.Error(), "model") {
		t.Errorf("expected both endpoint and model in error, got: %v", err)
	}
}

func TestValidateInvalidLogLevel(t *testing.T) {
	content := `
[llm]
endpoint = "http://localhost:8080/v1"
model = "test"

[logging]
level = invalid
`
	path := writeTestINI(t, content)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}

	err = cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for invalid log level")
	}
	if !strings.Contains(err.Error(), "logging.level") {
		t.Errorf("expected logging.level in error, got: %v", err)
	}
}

func TestValidateInvalidPort(t *testing.T) {
	content := `
[llm]
endpoint = "http://localhost:8080/v1"
model = "test"

[server]
port = 99999
`
	path := writeTestINI(t, content)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}

	err = cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for invalid port")
	}
}

func TestValidateNegativeContextTokens(t *testing.T) {
	content := `
[llm]
endpoint = "http://localhost:8080/v1"
model = "test"
context_tokens = -1
`
	path := writeTestINI(t, content)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}

	err = cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for negative context_tokens")
	}
}

func TestValidateNegativeMaxDepth(t *testing.T) {
	content := `
[llm]
endpoint = "http://localhost:8080/v1"
model = "test"

[queue]
max_depth = -1
`
	path := writeTestINI(t, content)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}

	err = cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for negative max_depth")
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load("/nonexistent/config.ini")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestValidateValidConfig(t *testing.T) {
	content := `
[llm]
endpoint = "http://localhost:8080/v1"
model = "test-model"
`
	path := writeTestINI(t, content)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}

	err = cfg.Validate()
	if err != nil {
		t.Fatalf("expected valid config, got error: %v", err)
	}
}

func TestStrDefault(t *testing.T) {
	data := map[string]map[string]string{
		"section": {"key": "value"},
	}

	if strDefault(data, "section", "key", "default") != "value" {
		t.Error("expected existing value")
	}
	if strDefault(data, "section", "missing", "default") != "default" {
		t.Error("expected default for missing key")
	}
	if strDefault(data, "missing", "key", "default") != "default" {
		t.Error("expected default for missing section")
	}
}

func TestIntDefault(t *testing.T) {
	data := map[string]map[string]string{
		"section": {"key": "42", "bad": "not-a-number"},
	}

	if intDefault(data, "section", "key", 0) != 42 {
		t.Error("expected 42")
	}
	if intDefault(data, "section", "bad", 10) != 10 {
		t.Error("expected default for invalid number")
	}
	if intDefault(data, "section", "missing", 10) != 10 {
		t.Error("expected default for missing key")
	}
}

func TestBoolDefault(t *testing.T) {
	data := map[string]map[string]string{
		"section": {"a": "true", "b": "false", "c": "invalid"},
	}

	if !boolDefault(data, "section", "a", false) {
		t.Error("expected true for a")
	}
	if boolDefault(data, "section", "b", true) {
		t.Error("expected false for b")
	}
	if boolDefault(data, "section", "c", true) != true {
		t.Error("expected default for invalid bool")
	}
	if boolDefault(data, "section", "missing", false) {
		t.Error("expected default for missing key")
	}
}

func TestFloatDefault(t *testing.T) {
	data := map[string]map[string]string{
		"section": {"a": "0.5", "b": "1.25", "c": "not-a-float"},
	}

	if floatDefault(data, "section", "a", 0.0) != 0.5 {
		t.Error("expected 0.5 for a")
	}
	if floatDefault(data, "section", "b", 0.0) != 1.25 {
		t.Error("expected 1.25 for b")
	}
	if floatDefault(data, "section", "c", 2.0) != 2.0 {
		t.Error("expected default for invalid float")
	}
	if floatDefault(data, "section", "missing", 3.0) != 3.0 {
		t.Error("expected default for missing key")
	}
}

func TestValidateInvalidSummarizeThreshold(t *testing.T) {
	content := `
[llm]
endpoint = "http://localhost:8080/v1"
model = "test"
summarize_threshold = 1.5
`
	path := writeTestINI(t, content)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}

	err = cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for summarize_threshold > 1")
	}
	if !strings.Contains(err.Error(), "summarize_threshold") {
		t.Errorf("expected summarize_threshold in error, got: %v", err)
	}
}

func TestValidateNegativeSummarizeKeepRecent(t *testing.T) {
	content := `
[llm]
endpoint = "http://localhost:8080/v1"
model = "test"
summarize_keep_recent = -1
`
	path := writeTestINI(t, content)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}

	err = cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for negative summarize_keep_recent")
	}
	if !strings.Contains(err.Error(), "summarize_keep_recent") {
		t.Errorf("expected summarize_keep_recent in error, got: %v", err)
	}
}

func TestLoadMaxBodyBytes(t *testing.T) {
	content := `
[llm]
endpoint = "http://localhost:8080/v1"
model = "test"

[server]
max_body_bytes = 2097152
`
	path := writeTestINI(t, content)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}

	if cfg.Server.MaxBodyBytes != 2097152 {
		t.Errorf("max_body_bytes = %d, want 2097152", cfg.Server.MaxBodyBytes)
	}

	err = cfg.Validate()
	if err != nil {
		t.Fatalf("expected valid config, got: %v", err)
	}
}

func TestLoadMaxBodyBytesDefault(t *testing.T) {
	content := `
[llm]
endpoint = "http://localhost:8080/v1"
model = "test"
`
	path := writeTestINI(t, content)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}

	// Default should be 1MB
	if cfg.Server.MaxBodyBytes != 1048576 {
		t.Errorf("max_body_bytes should default to 1048576, got %d", cfg.Server.MaxBodyBytes)
	}
}

func TestValidateNegativeMaxBodyBytes(t *testing.T) {
	content := `
[llm]
endpoint = "http://localhost:8080/v1"
model = "test"

[server]
max_body_bytes = -1
`
	path := writeTestINI(t, content)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}

	err = cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for negative max_body_bytes")
	}
	if !strings.Contains(err.Error(), "max_body_bytes") {
		t.Errorf("expected max_body_bytes in error, got: %v", err)
	}
}
