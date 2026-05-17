# PLAN_integ.md — Real LLM Integration Tests

## Goals

1. Add harness-level integration tests that use a **real local LLM** (Ollama / OpenAI-compatible) instead of a mock
2. The same test assertions verify both mock mode (deterministic) and real-LLM mode (deterministic via temp=0)
3. Enable rapid whole-codebase regression testing — one LLM call through the entire pipeline: webhook → queue → agent → tools → session → callback

## Architecture Change

Add a single config toggle that controls which LLM the harness uses:

**Config:**
```ini
[llm]
endpoint = ""              # "" = use mock, otherwise real LLM URL
api_key = "test"           # dummy value for local models that accept it
use_real_endpoint = false  # new field
```

**Code change at `src/llm/client.go:New()`:**
```go
func New(endpoint, model, apiKey string, timeout time.Duration, logDir string, logger *log.Logger) *Client {
    if endpoint == "" {
        // Mock mode — return nil or short-circuit to mock path
        // This is the injection point for integration testing
    }
    return &Client{...}
}
```

**Better approach (prefer dependency injection):**

Add an env var `HARNESS_LLM_ENDPOINT` that overrides `config.LLM.Endpoint` during bootstrap. This is zero-code-change:

```go
// In main.go, after config load:
if endpoint := os.Getenv("HARNESS_LLM_ENDPOINT"); endpoint != "" {
    config.LLM.Endpoint = endpoint
}
if key := os.Getenv("HARNESS_API_KEY"); key != "" {
    config.LLM.APIKey = key
}
```

Tests set these env vars before starting the harness — no config flag needed. No production code changes required.

## Test Organization

New file: `src/main_integ_test.go` (integrated with main_test.go, same package)

Each test has **two variants**:

```go
// Deterministic test — uses mock, verifies harness plumbing
func TestReadFile_Mock(t *testing.T) {
    s, mockURL := mockllm.New(t)
    s.SetResponseToolCalls([]mockllm.MockToolCall{
        {ID: "call_1", Name: "view", Args: `{"path":"foo.md"}`},
    })
    // configure harness with mockURL
    // assert: exact tool call ID, exact session structure, exact callback
}

// Integration test — uses real LLM, same assertions on harness behavior
func TestReadFile_Integration(t *testing.T) {
    os.Setenv("HARNESS_LLM_ENDPOINT", "http://localhost:11434/v1")
    // configure harness normally
    // assert: same harness plumbing checks (session structure, callback, tool dispatched)
    // but NOT checking exact tool return content (LLM may paraphrase)
}
```

The assertions differ *only on LLM output content*, not on harness plumbing.

## Test Suite

### Phase 1: Single-turn responses (validation only)

These tests verify that a real LLM response flows through the harness without structural bugs. They are **validation tests** — low effort, confirm the plumbing works.

| Name | Message | LLM prompt | Assertion |
|------|---------|------------|-----------|
| `Integ_ReflectiveReply` | "hello" | No tools | Callback contains a greeting-like response, session has user+assistant roles |
| `Integ_SystemPromptIncluded` | "what are your instructions?" | No tools | Callback mentions or references AGENTS.md (proves system prompt composed correctly) |

These are not strict assertions — they check the harness processed the LLM response structurally, not that the LLM said a specific thing.

### Phase 2: Tool-call integration (core value)

These are the high-value tests. With temp=0 and the harness system prompt, tool selection is deterministic. Each test asserts on:

- **Tool dispatch**: the real tool function was called (file was read, bash executed, etc.)
- **Session structure**: assistant message → tool call → tool result → assistant message
- **Callback delivery**: the final message was POSTed to the callback URL

| Name | Message | Expected tool call | Tool | Assertion |
|------|---------|-------------------|------|-----------|
| `Integ_ReadFile` | "read the contents of foo.md" | `view` with `path: "foo.md"` | `view` | File contents returned in callback; session has correct tool call + result messages |
| `Integ_WriteFile` | "write hello world to bar.txt" | `write` with `path: "bar.txt"` | `write` | File created with correct content in working dir |
| `Integ_AppendFile` | "append new line to bar.txt" | `append` with `path: "bar.txt"` | `append` | File has appended content |
| `Integ_Echo` | "echo 'echo output'" | `bash` with `cmd: "echo echo output"` | `bash` | Output in callback matches expected string |
| `Integ_ListDir` | "list files in work/" | `ls` with `path: "work/"` | `ls` | File list returned in callback |
| `Integ_GrepSearch` | "grep for 'TODO' in *.go" | `grep` with `pattern: "TODO"` | `grep` | Matches returned in callback |
| `Integ_Fetch` | "fetch https://example.com" | `fetch` with `url: "https://example.com"` | `fetch` | Domain mentioned in callback (structural, not full content) |
| `Integ_ImageRead` | "view the image file" + image attachment | `view_image` | `image` | Callback contains base64 image reference |

### Phase 3: Multi-tool + boundary

These are the most valuable for catching regressions in the agent loop.

| Name | Message | Expected behavior | Assertion |
|------|---------|-------------------|-----------|
| `Integ_MultiToolCall` | "read foo.md AND list files in work/" | 2+ tool calls in one LLM turn | Both tools dispatched; all appear in session |
| `Integ_ToolErrorRecovery` | (message likely to cause a tool error first) | Tool fails, LLM retries | Callback contains recovered answer; session has error tool result |
| `Integ_ContextSummarization` | "tell me what we discussed" after 10+ exchanges | Summarization triggered | Summary message in session with `[Summary of prior conversation]` prefix |
| `Integ_SandboxEscape` | "read /etc/passwd" | Tool returns "access denied" | LLM receives denial; does not expose restricted content |
| `Integ_BashAllowlist` | "sudo rm -rf /" | Bash tool blocks banned command | Session has error tool result; callback shows blocked message |

### Phase 4: Harness plumbing with real LLM (regression safety)

These verify that real LLM output doesn't break any harness components.

| Name | Message | Purpose | Assertion |
|------|---------|---------|-----------|
| `Integ_MultiChannel` | 3 messages to different channels | Channels don't mix | All 3 processes independently, each gets unique callback |
| `Integ_QueueBackpressure` | Messages exceeding max_depth | Queue enforces per-channel limit | 429 returned; callback reports rejection |
| `Integ_GracefulShutdown` | Signal during processing | Worker handles interruption | Pending messages drained to session files; no crashes |
| `Integ_SessionPersistence` | Long conversation (50+ turns) with temp=0 | Session grows, saves, reloads | Session file has exactly N messages; reloadable |
| `Integ_CallbackFailure` | Configured callback URL that returns 500 | Callback error handling | Error logged; processing continues; session saved |

## Determinism Model

All integration tests use Ollama (or compatible) with **temp=0** in the model config:

```ini
[llm]
endpoint = "http://localhost:11434/v1"
model = "qwen2.5:7b"
temperature = 0  # deterministic output
```

With temp=0, the LLM output is deterministic given the same message + state. Model changes can affect which tool is selected (e.g., qwen → mistral may choose `grep` instead of `view`), but this is an acceptable risk per requirements.

Each integration test documents which model it was written for. If model swap causes test breakage, the test is flagged for that model family.

## Configuration

### For developers

**Mock mode (default):**
```bash
make test
```

**Integration tests (real LLM):**
```bash
HARNESS_LLM_ENDPOINT=http://localhost:11434/v1 make integ
```

Where `make integ` runs:
```makefile
integ:
    cd $(SRC) && go test -tags=integ ./...
```

The `-tags=integ` filter ensures integration tests are skipped during normal test runs. Tests with `//go:build integ` tags only run when explicitly invoked.

### Model setup

Developers should configure their local Ollama in `config.ini`:
```ini
[llm]
endpoint = "http://localhost:11434/v1"
model = "qwen2.5:7b"
api_key = "ollama"  # placeholder
temperature = 0
```

Model requirement: must support **function calling / tool use** (Qwen2.5-7B or better). Older models without tool support will fail immediately and surface the issue to the developer.

## File Map (new/modified)

| File | Action | Notes |
|------|--------|-------|
| `src/main_integ_test.go` | New | Integration test suite (20+ tests) |
| `src/main.go` | Modify | Add env var override for `HARNESS_LLM_ENDPOINT` |
| `Makefile` | Modify | Add `make integ` target with `-tags=integ` |
| `AGENTS.md` | Modify | Add section on running integration tests |
| `config/config.go` | Modify | Add `Temperature` field (may already exist as HTTP param) |

## Execution Strategy

1. **Phase 1 first** — 2 validation tests. Quick win. Confirms the env var approach works.
2. **Phase 2 second** — 8 tool integration tests. These are the regression safety net. Run them on PR creation to verify tool dispatch works end-to-end.
3. **Phase 3 third** — Multi-tool + boundary tests. Require Qwen2.5+ level tool-calling capability.
4. **Phase 4 last** — Harness plumbing. These verify infrastructure under real-load conditions.

Each phase can be committed separately (one commit per phase, or squash into one PR with the fixture).

## Success Criteria

1. Running `make integ` against a local Ollama instance passes all 20 tests
2. Each test documents which model family it targets
3. Tests run under 60 seconds total (temp=0 + local endpoint = fast)
4. No harness code changes required beyond the env var injection
5. Same test file structure — mock variant is the default, integ variant uses env var

## Risks

| Risk | Mitigation |
|------|------------|
| Model capability variance (older models lack tool use) | `-tags=integ` skips tests on models that don't support tools; CI (later) can test across model families |
| Non-deterministic output from temp > 0 | Require temp=0; validate in test setup |
| LLM hallucinates wrong tools | With temp=0 + clear system prompts, tool selection is deterministic for a given model. Document which models pass which tests. |
| Longer test runtime than mocks (~2-5s per LLM call vs ~0ms) | 20 tests × 3s = ~60s. Acceptable for manual dev runs. |
| LLM endpoint unavailable locally | `-tags=integ` makes these opt-in by default |
