# AGENTS.md — Agent Harness Documentation for AI Assistants

> **IMPORTANT: `UAT/` is the user's test area. Do NOT read, modify, or reference any files under `UAT/` unless explicitly instructed to do so. It contains test artifacts that will confuse agents.**

---

## 1. System Overview

The Agent Harness is a minimal autonomous agent system written in Go (stdlib only, zero external dependencies). It receives messages via webhook, queues them FIFO, and processes them through an LLM tool-call loop. Each channel gets an isolated session with persistent state.

**Key design principles:**
- Single Go binary, no external dependencies
- Custom INI config parser with multiline (`"""`) support
- FIFO message queue with per-channel backpressure
- Flat JSON session persistence (one file per channel)
- Per-channel conversation logging (`channellog`)
- Sandboxed filesystem tools (path-traversal blocked)
- OpenAI-compatible LLM API with SSE streaming
- CLI client for local testing (`src/cmd/client/client.go`)

---

## 2. Message Lifecycle

1. **Inbound**: `POST /webhook` with `{channel, message, callback_url?}` → validated (JSON, required fields, `callback_url` must be http/https, channel ID ≤ 254 chars, body ≤ `server.max_body_bytes` default 1MB) → enqueued
2. **Dequeue**: Worker polls queue, pops oldest message
3. **Process**: Agent runs tool-call loop, appends user message to session, iterates up to `max_tool_iterations`
4. **Save**: Session saved atomically after processing completes (also saved on error to persist the user message)
5. **Callback**: Aggregated output POSTed to `callback_url` (if provided in original message)
6. **On Error**: If agent fails, error message sent as callback instead; session is saved to preserve user message; if callback fails, logged at `error` level

---

## 3. Sandbox

File tools are sandboxed to `paths.working_dir`. `sandbox.ResolvePath()` blocks absolute paths, `..` traversal, and symlink escapes. On failure, the tool returns `"access denied"`.

The `bash` tool is gated by `bash.enabled` in the INI (not registered if false). When enabled, it bypasses the sandbox — mitigated by a configurable command denylist (only checks the **first token** of the command, not piped/redirected commands), hard timeout, no stdin, and output truncation.

**Source**: `src/sandbox/sandbox.go` `ResolvePath()`

---

## 4. Session Persistence

Each channel gets a `<state_dir>/<channel_id>.json` file.

**Schema**:
```json
{
  "channel_id": "slack:abc123",
  "messages": [
    {"role": "user", "content": "..."},
    {"role": "assistant", "content": "...", "reasoning_content": "...", "tool_calls": [...]},
    {"role": "tool", "content": "...", "tool_call_id": "..."}
  ],
  "created_at": "ISO-8601",
  "updated_at": "ISO-8601"
}
```

- **Atomic writes**: write to `.tmp` file, then `os.Rename()` to final path
- **System prompt**: NOT stored in session — composed at request time from the INI `llm.system_prompt` plus any workspace markdown files found in `paths.working_dir` (`AGENTS.md`, `SOUL.md`, `IDENTITY.md`, `USER.md`, `MEMORY.md`). Each file is wrapped with `--- FILENAME ---` delimiters.
- **Context summarization**: when total estimated tokens (characters/4) exceed `context_tokens × summarize_threshold` (threshold is a fraction, e.g. `0.8` = 80%), the oldest messages (excluding the last `summarize_keep_recent`) are compressed into a single assistant message via a dedicated LLM call. The summary is stored in `reasoning_content` prefixed with `"[Summary of prior conversation]\n"`. The summary prompt is embedded at compile time from `src/session/summary.md`. Summarization failures are fatal.

**Source**: `src/session/session.go`, `src/worker/worker.go` `buildSystemPrompt()`

---

## 5. Testing

- Test files co-located with source (e.g. `config_test.go` alongside `config.go`)
- Loop-based tests (agent tool-call loop, worker queue processing) have a 2-second max runtime guard
- Run tests with: `go test ./...`

---

## 6. Channel Logging

The `channellog` package writes per-channel conversation logs to `paths.channel_log_dir`. User messages, assistant responses, tool calls, and system events (e.g. summarization) are logged separately. Enabled via `logging.log_channel_events`.

**Source**: `src/channellog/channellog.go`

---

## 7. Graceful Shutdown

On `SIGTERM` or `SIGINT`, the harness performs a 5-step shutdown:

1. **Stop webhook server** — rejects new messages with 503
2. **Cancel worker context** — stops the processing loop
3. **Drain queue** — pending messages are appended to session files without calling the LLM (prevents message loss)
4. **Flush sessions** — all in-memory sessions are atomically written to disk
5. **Clear queue** — resets pending counters

**Source**: `src/main.go` (graceful shutdown section)

---

## 8. CLI Client

A standalone CLI client (`src/cmd/client/client.go`) sends messages to the harness webhook. By default it spins up a local callback server and waits for the response. Options:
- `-nc` — fire-and-forget (no callback)
- `-cb <url>` — use an external callback URL
- `-n <channel>` — set the channel ID (default: `cli`)
- `-t` — show reasoning and tool calls in output

**Source**: `src/cmd/client/client.go`

---

## 9. Webhook Input Validation

The webhook handler (`src/webhook/server.go`) validates all inbound requests:

| Check | Status Code | Response |
|---|---|---|
| Non-POST method | 405 | (empty) |
| Shutting down | 503 | `service unavailable` |
| Malformed JSON / body exceeds `max_body_bytes` | 400 | `invalid JSON` |
| Missing `channel` | 400 | `missing channel` |
| Channel ID > 254 chars | 400 | `channel ID too long` |
| Missing `message` | 400 | `missing message` |
| Invalid `callback_url` (non-http/https scheme) | 400 | `invalid callback_url (must be http or https)` |
| Queue full (per-channel) | 429 | rejection message + optional callback |

**Config**: `server.max_body_bytes` (optional, default `1048576` = 1MB). Applied via `http.MaxBytesReader`.

**Source**: `src/webhook/server.go` `handleWebhook()`
