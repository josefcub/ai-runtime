# IMPLEMENTATION.md — Phased Remediation Plan

Generated from `docs/TODO.md` per `docs/TESTS.md` standards.

**Context**: This plan was created for a multi-session remediation effort. Each phase executes in a **separate AI session**. The AI that writes this plan is NOT the AI that executes it — this document is the sole source of truth for phase scope, ordering rationale, and validation criteria. Read it fully before starting any phase.

---

## Goals

1. Close all test coverage gaps identified in `docs/TODO.md`
2. Apply moderate refactoring (SRP, DRY, deduplication) as specified in TODO.md — **do not go beyond what TODO asks**
3. Interleave test additions with code quality fixes within each phase
4. Ensure the codebase compiles, passes the full writing cycle, and functions correctly **after each phase**

## Writing Cycle (from TESTS.md)

Every phase MUST end with:

1. `gofmt` — formatting holds
2. `go vet ./...` — static analysis clean
3. `go test ./...` — all tests pass
4. `go test -race ./...` — race detector finds nothing
5. `go build ./...` - ensures the code builds

If any step fails, the phase is incomplete. Fix before proceeding.

## Functional Verification (Human Owner)

After each phase, the human owner stress-tests the harness by using the agent in the harness to exercise the changed code in ways test cases cannot cover. This step is **not automated** — it happens outside this document. The AI does NOT perform functional verification.

## Phasing Strategy

Phases are ordered by **dependency layer** (leaf packages first) and **risk** (low-risk first). This ensures:

- New/refactored code in lower layers is tested before higher layers depend on it
- Each phase can complete without breaking functionality in unrefactored code
- The codebase remains fully functional between phases (critical for human stress-testing)

**Dependency map** (simplified):

```
main.go  →  worker, webhook, session, config, queue, log, tools
worker   →  agent, session, config, log, callback, queue
agent    →  llm, tools, session, sandbox, channellog, imageutil
tools    →  sandbox, imageutil
webhook  →  queue, session, log
client   →  config, imageutil, session  (standalone binary)
```

---

## Phase 1: Foundation — Zero-Coverage Leaf Packages

**Packages**: `imageutil`, `sandbox`, `session/summary`

**Rationale**: These are leaf packages with zero or minimal test coverage and no downstream dependents. Testing them first provides immediate coverage gains with zero risk of breaking existing functionality. They are also prerequisites for Phase 2 (tools depend on sandbox and imageutil).

**Target coverage gain**: imageutil 0% → 100%, sandbox 59% → 85%+, session/summary 0% → 100%

### 1A. New: `imageutil/imageutil_test.go`

Create a new test file with table-driven tests for `DetectMIME()`:

| Test Case | Input | Expected |
|---|---|---|
| PNG magic detection | `\x89PNG\r\n\x1a\n...` | `"image/png"` |
| JPEG detection | `\xff\xd8\xff...` | `"image/jpeg"` |
| GIF87a detection | `GIF87a...` | `"image/gif"` |
| GIF89a detection | `GIF89a...` | `"image/gif"` |
| WebP detection (wildcard bytes at positions 4-7) | `RIFF\x00\x00\x00\x00WEBP` | `"image/webp"` |
| BMP detection | `BM...` | `"image/bmp"` |
| TIFF little-endian | `\x49\x49\x2a\x00...` | `"image/tiff"` |
| TIFF big-endian | `\x4d\x4d\x00\x2a...` | `"image/tiff"` |
| Empty `[]byte` | `[]byte{}` | `""` |
| Data shorter than any magic | `[]byte{0x00}` | `""` |
| Partial/truncated magic (e.g., PNG header cut short) | `[]byte{0x89, 'P'}` | `""` |
| Unknown/random bytes | `[]byte{0x00, 0x00, 0x00}` | `""` |
| First-match-wins ordering | Data matching multiple patterns | First match in `KnownImageMagic` slice |

**TODO items covered**: All `imageutil/imageutil.go` unchecked items.

### 1B. New: `sandbox/sanitize_test.go`

Create a new test file (or add to existing `sandbox_test.go`) with table-driven tests for `SanitizeFilename()`:

| Test Case | Input | Expected |
|---|---|---|
| Null byte replacement | `foo\x00bar` | `foo_bar` |
| Forward slash replacement | `foo/bar` | `foo_bar` |
| Backslash replacement | `foo\bar` | `foo_bar` |
| `..` replacement | `foo/../bar` | `foo_/_bar` (note: `/` replaced first, then `..`) |
| `*` replacement | `*.txt` | `_.txt` |
| `?` replacement | `file?.txt` | `file_.txt` |
| `<` replacement | `a<b` | `a_b` |
| `>` replacement | `a>b` | `a_b` |
| `\|` replacement | `a\|b` | `a_b` |
| `:` replacement | `a:b` | `a_b` |
| `"` replacement | `a"b` | `a_b` |
| `'` replacement | `a'b` | `a_b` |
| Combined characters | `foo\0../bar*?` | all replaced with `_` |
| No special characters | `normal.txt` | `normal.txt` (unchanged) |

**TODO items covered**: `sandbox/sandbox.go:33-48 SanitizeFilename()` — entire function was untested.

### 1C. Additional: `sandbox/sandbox_test.go` — `ResolvePath()` gap

Add table-driven tests for `ResolvePath()`:

| Test Case | Setup | Expected |
|---|---|---|
| `filepath.Abs(workingDir)` failure path | (requires special setup or acceptance that this is hard to trigger) | `"access denied"` |
| Parent directory symlink resolution when file doesn't exist | Symlinked parent, non-existent child file | Resolves through parent symlink |

Also: Convert existing `TestResolvePath_PrefixWithSeparator` to table-driven format with explicit controlled paths (currently depends on temp dir parent being predictable).

### 1D. New: `session/summary_test.go`

Create a minimal test file verifying `SummaryPrompt`:

| Test Case | Assertion |
|---|---|
| `SummaryPrompt` is non-empty | `len(session.SummaryPrompt) > 0` |
| Contains expected section headers | Contains `"Current State"`, `"Files & Changes"`, `"Technical Context"`, `"Strategy & Approach"`, `"Exact Next Steps"` |

**TODO items covered**: `session/summary.go` — no test file existed.

### Phase 1 Validation

```bash
gofmt -d ./imageutil/ ./sandbox/ ./session/
go vet ./...
go test ./...
go test -race ./...
go build ./...
```

---

## Phase 2: Tool Layer — Zero-Coverage Tool Implementations

**Packages**: `tools/image_tool.go`, `tools/web_tools.go`

**Rationale**: These files have zero test coverage and depend only on Phase 1 packages (sandbox, imageutil). Testing them builds confidence in the tool layer before the agent (which calls these tools) is touched in Phase 6.

**Target coverage gain**: tools package from 61% → 80%+

### 2A. New: `tools/image_tool_test.go`

Table-driven tests for `toolViewImage()`:

| Test Case | Setup | Expected |
|---|---|---|
| Missing path argument | `args` without `"path"` | Error |
| Sandbox escape: absolute path | `path: "/etc/passwd"` | `"access denied"` |
| Sandbox escape: path traversal | `path: "../../../etc/passwd"` | `"access denied"` |
| Non-existent file | Valid relative path to missing file | Error containing `"read file"` |
| File too large | File > `imageutil.MaxImageSize` (4MB) | Error containing `"image too large"` |
| Non-image file (unrecognized MIME) | Plain text file | Error containing `"not a recognized image"` |
| Successful PNG load | Valid PNG in working dir | JSON with `__attachment.data` (valid base64), `__attachment.mime_type == "image/png"`, `text` contains path + MIME + KB |
| Successful JPEG load | Valid JPEG in working dir | Same structure, MIME = `"image/jpeg"` |
| KB size rounding up | File of 1025 bytes | KB = 2 (rounded up) |

**Implementation note**: Create test image files programmatically (write magic bytes to temp files). Do not depend on external image assets.

### 2B. New: `tools/web_tools_test.go`

Table-driven tests using `httptest.Server`:

**`validateURL()` tests:**

| Test Case | Input | Expected |
|---|---|---|
| Empty URL | `""` | Error |
| HTTP allowed | `"http://example.com"` | `nil` |
| HTTPS allowed | `"https://example.com"` | `nil` |
| FTP rejected | `"ftp://example.com"` | Error |
| File scheme rejected | `"file:///etc/passwd"` | Error |

**`limitedReader` tests:**

| Test Case | Setup | Expected |
|---|---|---|
| Read under limit | 10 bytes, limit 100 | Full read, no error |
| Read at limit (truncate mode) | Exactly limit bytes | `io.EOF` at boundary |
| Read over limit (non-truncate mode) | Over limit | Error with limit message |

**`toolFetch()` tests (use `httptest.Server`):**

| Test Case | Setup | Expected |
|---|---|---|
| Missing URL error | No `"url"` in args | Error |
| Invalid scheme | `"ftp://..."` | Error |
| 2xx success (text format) | Server returns 200 + text | Trimmed body text |
| 2xx success (markdown format, non-HTML content-type) | Server returns text/plain | Body as-is |
| 2xx success (markdown format, HTML content-type) | Server returns text/html | HTML stripped |
| Non-2xx error | Server returns 404 | Error with HTTP status |
| Redirect limit exceeded | Server redirects 11+ times | Error with redirect count |
| Redirect to non-http scheme | Redirect to `ftp://` | Error |
| Response > 50KB (truncate) | Server returns 51KB+ | Truncated to 50KB silently |

**`stripHTML()` tests:**

| Test Case | Input | Expected |
|---|---|---|
| Basic tag stripping | `"<p>hello</p>"` | `" hello "` |
| Nested tags | `"<div><span>hi</span></div>"` | `" hi "` |
| No tags | `"plain text"` | `"plain text"` |
| Empty string | `""` | `""` |

**`toolDownload()` tests:**

| Test Case | Setup | Expected |
|---|---|---|
| Missing `url` error | No `"url"` in args | Error |
| Missing `file_path` error | No `"file_path"` in args | Error |
| Sandbox escape on destination | `file_path: "/tmp/out"` | `"access denied"` |
| Successful download | Server returns data | `"Downloaded N bytes to path"` + file exists |
| Non-2xx error | Server returns 500 | Error |
| >100MB rejected | Server returns >100MB | Error (via limitedReader) |
| Custom timeout | `timeout: 5` | Uses 5s timeout |
| Redirect limit | 11+ redirects | Error |
| Parent directory creation | `file_path: "subdir/file.txt"` | Creates `subdir/` |
| Partial file cleanup on error | Fails mid-download | File removed |

### 2C. Code Quality: `tools/web_tools.go` — Extract `newHTTPClient()`

**Refactoring**: `toolFetch()` (lines 121-134) and `toolDownload()` (lines 228-241) have identical HTTP client creation logic. Extract to:

```go
func newHTTPClient(timeout time.Duration, checkRedirect func(req *http.Request, via []*http.Request) error) *http.Client {
    return &http.Client{
        Timeout:       timeout,
        CheckRedirect: checkRedirect,
    }
}
```

Both callers still construct their own `checkRedirect` closures (they differ slightly in variable capture), but the client construction is shared. This is a low-risk extraction that doesn't change behavior.

Also fix `toolDownload()` `out.Close()` redundancy: `out.Close()` is called via `defer` on line 263 and explicitly on lines 270 and 275. The explicit call on line 275 after successful copy is redundant (defer handles it). The explicit call on line 270 in the error path is also redundant (defer handles it), but keeping it there is acceptable for clarity since `os.Remove` follows. Remove the redundant `out.Close()` on line 275 only (the success path).

**TODO items covered**: `tools/web_tools.go` all unchecked items + code quality (duplicate HTTP client creation, `out.Close()` redundancy).

### Phase 2 Validation

```bash
gofmt -d ./tools/
go vet ./...
go test ./...
go test -race ./...
go build ./...
```

---

## Phase 3: Client + Channellog + Logger

**Packages**: `client`, `channellog`, `log`

**Rationale**: `client` is a standalone binary (26% coverage) — changes here don't affect the server. `channellog` and `log` are foundational packages used everywhere but have specific edge-case gaps. Fixing them early improves observability for all subsequent phases.

**Target coverage gain**: client 26% → 60%+, channellog 74% → 90%+, log 92% → 98%+

### 3A. New: `client/client_test.go` additions — String manipulation functions

Add table-driven tests for `stripTraceOutput()` (lines 312-345) and `removeBlocks()` (lines 349-371):

**`stripTraceOutput()` tests:**

| Test Case | Input | Expected |
|---|---|---|
| Reasoning block removal | `"[Reasoning: thought]\noutput"` | `"output"` (reasoning block removed) |
| Tool Call block removal | `"[Tool Call: ls]\n[Result: ...]\noutput"` | `"output"` |
| Result block removal | `"[Result: data]\noutput"` | `"output"` |
| Multiple blocks | Multiple trace blocks + text | Only trace blocks removed |
| No blocks present | `"just text"` | `"just text"` (unchanged) |
| Excess newline collapsing | Multiple blank lines after removal | Collapsed to single newline |
| Empty result / only trace blocks | `"[Reasoning: x]\n[Tool Call: y]"` | Verify behavior (TODO notes: suspicious — returns unchanged if message is ONLY trace blocks) |

**`removeBlocks()` tests:**

| Test Case | Input | Expected |
|---|---|---|
| Leading newline stripping | `"start\n[block]\n\nend"` | `"start\nend"` (extra leading newline stripped) |
| Trailing newline stripping | `"start\n[block]\nend\n"` | `"start\nend"` |
| Multiple blocks | Two+ block occurrences | All removed |
| Unclosed blocks | `"[start"` with no closing delimiter | Behavior verified |
| Empty input | `""` | `""` |

**Code quality fix in `stripTraceOutput()`**: The TODO notes a DRY violation — Reasoning removal loop is nearly identical to `removeBlocks()` but manually inlined. Replace the manual loop with a call to `removeBlocks(result, "[Reasoning: ", "]\n")` (adjust delimiters to match current format).

### 3B. Additions: `client/client_test.go` — `intVal()` and `Send()` edge cases

| Test Case | Input | Expected |
|---|---|---|
| `intVal()` with non-integer value | `"port": "abc"` | Returns default |
| `intVal()` with valid integer | `"port": 8080` | Returns 8080 |
| Zero-value `ImageAttachment` omission | `ImageAttachment{Data: "", MIMEType: ""}` in Send() | Correctly omitted from JSON payload |
| `callbackServer.ServeHTTP()` — `Connection: close` header | Any request to callback server | Response has `Connection: close` header |
| Channel-full callback drop | Buffer full when callback arrives | Callback silently dropped |

### 3C. Refactor: `channellog/channellog.go` — `defer f.Close()`

Replace three explicit `f.Close()` calls on error paths (lines 71, 77, 83) with a single `defer f.Close()` after successful `OpenFile`. This is defensive deduplication — the current code works but repeats the close on every error path.

**TODO items covered**: `channellog.go:43-88 Log()` duplicated `f.Close()` calls.

### 3D. Additions: `channellog/channellog_test.go` — Table-driven tests + deep assertions

Convert existing tests from `strings.Contains` on raw JSON to **parsing JSON into `Entry` struct and asserting fields directly**:

| Test Case | Assertion |
|---|---|
| Auto-generated timestamp | `Log()` with `entry.Timestamp == ""` → output has RFC3339 timestamp in `timestamp` field |
| Explicit timestamp preserved | `Log()` with `entry.Timestamp` set → preserved as-is |
| Multiple channels produce separate files | Two channels → two `.log` files |
| `Log()` called directly with custom Entry | `Action`, `Tool` fields present in JSON |
| `LogTool` omits `message` field | JSON has no `message` key (`omitempty`) |
| `LogUser` omits `tool` field | JSON has no `tool` key (`omitempty`) |
| Each output line is valid JSON | Parse with `json.Unmarshal`, not `strings.Contains` |
| Concurrent writes to same channel | Multiple goroutines → no data corruption |
| Large message handling (>1KB) | No truncation |
| File permissions (0644) | `os.Stat()` shows mode `0644` |
| Empty message content | Valid JSON with `"message":""` |

### 3E. Additions: `log/logger_test.go` — Edge cases

| Test Case | Assertion |
|---|---|
| `Level.String()` — `default` case | `Level(99).String()` returns `"unknown"` |
| `ParseLevel()` — uppercase variants | `"WARN"` → `WarnLevel`, `"ERROR"` → `ErrorLevel` |
| `Logger.Close()` — nil-file safe | Closing a logger with `file == nil` doesn't panic |
| `Logger.Close()` — double-close | Second close is safe (no panic) |
| `Logger.Log()` — odd-length kvs | Orphaned last value handled gracefully (no panic) |
| `Logger.WithSource()` — parent not mutated | Parent logger still has `src == ""` after `WithSource()` call |
| `New()` — log file permissions | File created with `0644` |

**TODO items covered**: All `client/client.go`, `channellog/channellog.go`, `channellog/channellog_test.go`, and `log/logger.go` unchecked items.

### Phase 3 Validation

```bash
gofmt -d ./client/ ./channellog/ ./log/
go vet ./...
go test ./...
go test -race ./...
go build ./...
```

---

## Phase 4: Webhook + Queue

**Packages**: `webhook`, `queue`

**Rationale**: Webhook has 66% coverage with significant validation gaps. Queue is at 100% coverage but has a misleading API (`Enqueue` returns `(string, error)` where error is always `nil`) and a depth-underflow bug. Grouping these together because webhook depends on queue.

**Target coverage gain**: webhook 66% → 85%+, queue stays at 100% (defensive fixes only)

### 4A. New: `webhook/server_test.go` additions — Validation response body tests

Add table-driven tests verifying exact HTTP response bodies from `handleWebhook()`:

| Test Case | Request | Status | Body |
|---|---|---|---|
| 503 on shutdown | POST after `Stop()` | 503 | `"service unavailable"` |
| 405 on non-POST | GET request | 405 | (empty) |
| 400 invalid JSON | `{"channel": "x"` | 400 | `"invalid JSON"` |
| 400 missing channel | `{"message": "hi"}` | 400 | `"missing channel"` |
| 400 channel too long | channel > 254 chars | 400 | `"channel ID too long"` |
| 400 missing message | `{"channel": "x"}` | 400 | `"missing message"` |
| 400 invalid callback URL | `callback_url: "ftp://..."` | 400 | `"invalid callback_url (must be http or https)"` |
| Message text format | Valid message | 200 | `"[MM/DD/YYYY HH:MM:SS] [#channel] text"` — verify timestamp format |
| 429 backpressure without callback | Queue full, no `callback_url` | 429 | Rejection body content |
| `Start`/`Stop` lifecycle | Start, then Stop | — | No error on Stop, server shuts down |

**Code quality**: Clean up existing `TestIntegration_WebhookServer` dead code (unused `httptest` server assigned to `_`).

### 4B. New: `webhook/callback_test.go` additions — Deep assertions + logging

| Test Case | Assertion |
|---|---|
| `Content-Type: application/json` header | Outgoing request has correct header |
| 10-second HTTP client timeout | Server that takes >10s → timeout error |
| Error logging on request creation failure | `SendCallback` with logger → error logged |
| Error + debug logging on network error | Unreachable URL → error + debug logged |
| Error + debug logging on non-2xx response | Server returns 500 → error + debug logged |
| Info logging on success | 200 response → info logged with token estimate |
| Marshal error return path | (edge case — only triggers with non-JSON-safe strings) |
| Response body read for error context | Error message includes response body |

**Code quality**: Extract shared `httptest.NewServer` setup helper to reduce ~40 lines of duplication across 5 tests.

### 4C. Fix: `queue/queue.go` — Misleading return signature + depth underflow

**Fix 1**: `Enqueue()` returns `(string, error)` but error is always `nil`. Either:
- Remove error return: `func (q *Queue) Enqueue(msg Message) string`
- OR document why error is there (if it's for future use)

Recommended: Remove the error return. Update all call sites (check `webhook/server.go:170` and any others).

**Fix 2**: `Dequeue()` decrements `depth[msg.ChannelID]` with no guard against negative values. If Dequeue is called on a message whose channel depth is already 0 (could happen if depth gets out of sync), it goes to -1. Add guard:

```go
if q.depth[msg.ChannelID] > 0 {
    q.depth[msg.ChannelID]--
}
```

### 4D. Fix: `queue/queue_test.go` — Racy concurrent test

Replace `TestConcurrentEnqueueDequeue` with a properly synchronized version:
- Use exact counts (not `len(results) > 500`)
- Sync enqueuers and dequeuers with `sync.WaitGroup`
- Verify exact message count matches

### Phase 4 Validation

```bash
gofmt -d ./webhook/ ./queue/
go vet ./...
go test ./...
go test -race ./...
go build ./...
```

---

## Phase 5: Session + Config + INI Parser

**Packages**: `session`, `config`, `config/ini_parser`

**Rationale**: These are mid-level packages. Session has DRY violations (`DrainAndSave` duplicates `saveFile` logic) and edge-case gaps. Config has a 60+ token inline list and a 58-line `Validate()` that should be split. INI parser has a monolithic loop that should be decomposed.

**Target coverage gain**: session 82% → 92%+, config 95% → 98%+, ini_parser (already 95% via config tests, will improve)

### 5A. Fix: `session/session.go` — DRY violation in `DrainAndSave()`

`DrainAndSave()` (lines 211-258) duplicates the atomic write logic from `saveFile()` (lines 148-171): `MkdirAll`, `SanitizeFilename`, `WriteFile(tmp)`, `Rename(tmp, final)`.

**Fix**: After appending the message and updating timestamps, call `m.saveFile(s)` instead of duplicating the logic. This requires ensuring `saveFile` is callable without holding `m.mu` (it already is — the comment on line 147 says "Caller must NOT hold m.mu").

Updated flow:
```go
func (m *Manager) DrainAndSave(...) error {
    m.mu.Lock()
    // ... append message, update timestamps ...
    s := m.sessions[channelID] // get reference
    m.mu.Unlock()

    return m.saveFile(s)
}
```

Also address the inconsistent null-byte handling: `Get()` rejects null bytes in channelID, `DrainAndSave()` does not. Add the same null-byte check to `DrainAndSave()`.

### 5B. Fix: `session/session.go` — `LoadAll()` unrecoverable on first bad file

Current behavior: one corrupt session file prevents loading ALL others.

**Fix**: Log the error and continue instead of returning immediately. Change the error return to accumulate errors (or use a logger parameter if available). Since `LoadAll()` currently returns a single error, change it to log individual file errors and continue, only returning an error if the directory itself is unreadable.

If adding a logger parameter is too invasive, use `fmt.Fprintf(os.Stderr, ...)` for now, or change the signature to accept an optional logger.

### 5C. Additions: `session/session_test.go`

| Test Case | Assertion |
|---|---|
| `LastMessage()` — nil for empty session | Returns `nil` |
| `LastMessage()` — pointer to last message | Returns `*ConversationMessage` with correct Role, Content, ToolCalls, ToolCallID |
| `LoadAll()` with corrupted JSON | Returns error identifying bad file |
| `LoadAll()` with mixed valid/invalid files | After fix: continues past bad files, loads good ones |
| `Save()` — in-memory UpdatedAt | UpdatedAt reflects time set during save |
| `SaveAll()` — partial failure | If one save fails, subsequent saves are skipped (verify current behavior is intentional) |
| `Get()` first call — CreatedAt equals UpdatedAt | Both set to same time |
| `DrainAndSave()` with ReasoningContent | Roundtrip preserves it |
| `DrainAndSave()` concurrent with `Save()` on same channel | Race detector clean |
| `TestSaveLoadRoundtrip` — missing assertions | Verify ChannelID, Messages[0].Role, Messages[1].Role, CreatedAt/UpdatedAt preservation |
| `TestConcurrentAccess` — content verification | Each session has exactly 1 message with Content == `"concurrent"` |

### 5D. Fix: `config/config.go` — Extract bash banned list

Line 112 has a 60+ token inline comma-separated list. Extract to a `const`:

```go
const defaultBannedCommands = "curl,wget,ssh,scp,..."
```

Then use: `cfg.Bash.Banned = strListDefault(data, "tools.bash", "banned", defaultBannedCommands)`

### 5E. Refactor: `config/config.go` — Split `Validate()`

Split the 58-line `Validate()` (lines 118-175) into sub-methods:

```go
func (c *Config) Validate() error {
    var errors []string
    errors = append(errors, c.validateLLM()...)
    errors = append(errors, c.validateServer()...)
    errors = append(errors, c.validateQueue()...)
    errors = append(errors, c.validateLogging()...)
    errors = append(errors, c.validateBash()...)
    // ...
}

func (c *Config) validateLLM() []string { ... }
func (c *Config) validateServer() []string { ... }
func (c *Config) validateQueue() []string { ... }
func (c *Config) validateLogging() []string { ... }
func (c *Config) validateBash() []string { ... }
```

This makes each validation group independently testable.

### 5F. Additions: `config/config_test.go`

| Test Case | Assertion |
|---|---|
| `TestLoadFullConfig` — missing assertions | Verify `cfg.Bash.Enabled`, `Timeout`, `MaxOutput`, `Banned`, `cfg.Paths.ChannelLogDir` |
| `TestLoadDefaults` — missing assertions | Verify `cfg.Paths.ChannelLogDir`, `cfg.Bash.*`, `cfg.LLM.SummarizeThreshold`, `cfg.LLM.SummarizeKeepRecent` |
| Validation: `llm.max_tokens <= 0` | Error returned |
| Validation: `llm.timeout <= 0` | Error returned |
| Validation: `llm.max_tool_iterations <= 0` | Error returned |
| Validation: `tools.bash.timeout <= 0` | Error returned |
| Validation: `tools.bash.max_output <= 0` | Error returned |
| Validation: `server.port = 0` | Error returned |
| Validation: `summarize_threshold = 0` | Error returned |
| `strListDefault()` — empty default | Empty slice |
| `strListDefault()` — single item | One-item slice |
| `strListDefault()` — comma-separated with whitespace | Trimmed items |
| `strListDefault()` — mixed case lowercasing | All lowercased |
| `strListDefault()` — empty entries filtered | No empty strings in result |
| `strListDefault()` — missing section/key falls back to split default | Default is used |

### 5G. Refactor: `config/ini_parser.go` — Decompose `Parse()` loop

Extract the massive if/else chain in `Parse()` (lines 12-113) into named helper functions:

```go
func handleMultilineLine(...) (handled bool, ...) { ... }
func parseKeyValue(trimmed string) (key, value string, ok bool) { ... }
func isSectionHeader(trimmed string) (section string, ok bool) { ... }
func isComment(trimmed string) bool { ... }
```

The main loop becomes:
```go
for scanner.Scan() {
    if inMultiline {
        // handled by handleMultilineLine
    }
    if isBlank(trimmed) { continue }
    if isComment(trimmed) { continue }
    if section, ok := isSectionHeader(trimmed); ok {
        currentSection = section; continue
    }
    // ... key/value parsing ...
}
```

This is readability-only — behavior must not change.

### 5H. Additions: `config/ini_parser_test.go` — Missing edge cases

| Test Case | Input | Expected |
|---|---|---|
| Empty input | `[]byte{}` | Empty map, no error |
| Only comments | `# comment\n; comment` | Empty map, no error |
| Key with `=` in value | `key = a=b` | Value is `"a=b"` |
| Whitespace-only value | `key = ` | Value is `""` |
| Section header with whitespace | `[ section ]` | Section is `"section"` (trimmed) |
| Multiline value with blank lines inside | `"""  \nline1\n\nline2\n"""` | Preserves blank line |
| Top-level multiline value (before any section) | `key = """..."""` before `[section]` | In `""` section |
| Key collision across sections | Same key in `[a]` and `[b]` | Both preserved in different sections |
| `stripInlineComment()` — unclosed quote | `"value with no close` | Returns raw string |

### Phase 5 Validation

```bash
gofmt -d ./session/ ./config/
go vet ./...
go test ./...
go test -race ./...
go build ./...
```

---

## Phase 6: Agent Core — High-Risk Refactoring + Tests

**Packages**: `agent`

**Rationale**: This is the highest-risk phase. `agent.Process()` is 172 lines with duplicated logic, and `summarizeContext()` has repeated error-handling patterns. All lower-level dependencies (tools, sandbox, session, llm, channellog, imageutil) are now tested from prior phases, providing a safety net.

**Target coverage gain**: agent 77% → 90%+

### 6A. Code Quality: `agent/agent.go` — Extract helpers from `Process()`

**Deduplication 1**: Reasoning + content accumulation (lines 107-123 for partial response, lines 126-140 for normal response) have identical `[Reasoning: ...]\n` logic. Extract:

```go
func accumulateOutput(output *strings.Builder, resp *llm.ChatResponse) {
    if resp.ReasoningContent != "" {
        output.WriteString("[Reasoning: " + resp.ReasoningContent + "]\n")
    }
    if resp.Content != "" {
        output.WriteString(resp.Content)
    }
}
```

**Deduplication 2**: Tool-call-to-session conversion (lines 157-166) duplicates the same struct-copy pattern found in `convertMessage` and `toMultimodalMessage`. Extract:

```go
func convertToolCalls(respToolCalls []llm.ToolCall) []session.ToolCall {
    var sessTCs []session.ToolCall
    for _, tc := range respToolCalls {
        stc := session.ToolCall{
            ID:   tc.ID,
            Type: tc.Type,
        }
        stc.Function.Name = tc.Function.Name
        stc.Function.Arguments = tc.Function.Arguments
        sessTCs = append(sessTCs, stc)
    }
    return sessTCs
}
```

**Decomposition**: `Process()` should decompose into logical sub-functions:
- Message creation (lines 63-70) — already compact
- Tool-call loop iteration (lines 78-215) — the main loop stays, but calls extracted helpers
- LLM dispatch (lines 99-124) — stays inline but uses `accumulateOutput()`
- Output accumulation — uses `accumulateOutput()`

**Do NOT split `Process()` into multiple exported methods** — it's a single logical operation. The refactoring is about extracting *private helpers* to reduce line count and duplication, not changing the public API.

### 6B. Code Quality: `agent/agent.go` — `summarizeContext()` deduplication

**Deduplication 1**: Error-handling pattern (lines 294-310 vs 317-332) does `logger.Error` + `channelLogger.Log` + session append in both branches. Extract:

```go
func (a *Agent) logAndRecordSummarizationError(sess *session.Session, errMsg string) error {
    if a.logger != nil {
        a.logger.Error(errMsg)
    }
    _ = a.channelLogger.Log(sess.ChannelID, channellog.Entry{
        Role:    "system",
        Action:  "tool",
        Tool:    "session_summary",
        Message: errMsg,
    })
    sess.Messages = append(sess.Messages, session.ConversationMessage{
        Role:    session.RoleTool,
        Content: errMsg,
    })
    return fmt.Errorf("%s", errMsg) // wrap as needed
}
```

**Deduplication 2**: The repeated `channelLogger.Log` pattern with identical Entry struct appears 4 times. The helper above covers 2 of them; the remaining 2 (start log on line 265, and the skip log on line 277) are distinct messages and can stay inline.

### 6C. Code Quality: `agent/agent.go` — `convertMessage()` / `toMultimodalMessage()` deduplication

Lines 465-474 and 511-520 have identical tool-call conversion. Use the `convertToolCalls()` helper from 6A.

### 6D. New: `agent/agent_test.go` — Deep assertions + missing behaviors

Convert existing tests to use deep data-structure verification (not surface assertions like `err == nil`). Add tests for all missing behaviors:

**`Process()` behavior tests:**

| Test Case | Assertion |
|---|---|
| ReasoningContent recorded in session on normal response | Session message has `ReasoningContent` set |
| ReasoningContent-only response (Content empty, ReasoningContent present) | Output format: `"[Reasoning: ...]\n"` only, no content after |
| Output format for reasoning in normal flow | `"[Reasoning: ...]\n"` prefix is correct |
| Multiple tool calls with different tool names | Not just `echo` twice — use `read_file` + `write_file` etc. |
| Image attachment + tool call in same message | Session message has `Attachments` field set + tool call processed |
| ReasoningContent preserved in final assistant session message after tool loop | Last assistant message in session has `ReasoningContent` |
| Max tool iterations — synthetic closing message | Session ends with assistant message, not tool message |

**`summarizeContext()` tests:**

| Test Case | Assertion |
|---|---|
| Empty Content fallback to ReasoningContent | Summary uses ReasoningContent when Content is empty |
| Summary message structure | Role=Assistant, Content="", ReasoningContent=`"[Summary of prior conversation]\n<text>"` |
| Summarization with attachment-protected messages | Attachments preserved through summarization |

**`totalTokens()` tests:**

| Test Case | Assertion |
|---|---|
| Token count for attachments | `* attachmentTokenCost` multiplier applied |
| Token count for tool calls | Function name + arguments counted |
| Token count for ReasoningContent | Verify whether intentional to not count it (add comment if so) |
| Token count for ToolCallID | Counted or not — verify intentional |
| System prompt token estimation | `/ 3` divisor applied |

**`splitMessages()` tests:**

| Test Case | Assertion |
|---|---|
| Attachment-protected messages moved from old to recent | Messages with attachments stay in recent set |
| Mixed scenario | Some old messages with attachments, some without — correct split |

**`parseToolResult()` tests:**

| Test Case | Input | Expected |
|---|---|---|
| Valid JSON with `__attachment` key | `{"__attachment": {...}, "text": "hello"}` | text=`"hello"`, attachment extracted |
| Valid JSON without `__attachment` | `{"result": "ok"}` | raw result returned |
| Non-JSON input | `"plain text"` | raw result returned |
| JSON with `__attachment` but no `text` | `{"__attachment": {...}}` | text=`""` + attachment |
| Invalid `__attachment` JSON | Marshal/unmarshal error | raw result returned |

**`convertMessage()` / `toMultimodalMessage()` tests:**

| Test Case | Assertion |
|---|---|
| `convertMessage` with ToolCallID set | ToolCallID preserved in LLM message |
| `convertMessage` with both ToolCalls and ToolCallID | Both handled correctly |
| `toMultimodalMessage` with empty Content (image-only) | Image attachment present, content empty |
| `toMultimodalMessage` with multiple attachments | All attachments in output |
| `toMultimodalMessage` with tool calls + attachments | Both preserved |

### Phase 6 Validation

```bash
gofmt -d ./agent/
go vet ./...
go test ./...
go test -race ./...
go build ./...
```

---

## Phase 7: Worker + Main

**Packages**: `worker`, `main`

**Rationale**: Worker depends on agent (now refactored in Phase 6) and session. Main is the bootstrap entry point. Both have dead code, trivial tests, and extractable concerns.

**Target coverage gain**: worker 78% → 88%+, main (integration tests only)

### 7A. Fix: `worker/worker_test.go` — `go vet` error + dead code

**Critical**: `worker/worker_test.go:432` — `assignment copies lock value to _: sync/atomic.Int32 contains sync/atomic.noCopy`. This is a `go vet` error that must be fixed.

In `TestWorker_ConcurrentSafety` (lines 396-437):
- Remove `var processed atomic.Int32` and `_ = processed` (dead code)
- Remove `origProcess := proc` and `_ = origProcess` (dead code)
- The test replaces `w.processor` directly, bypassing the constructor — this tests internals, not behavior. Accept as-is for now (TODO notes it but doesn't mandate a fix), just clean up dead variables.

### 7B. Code Quality: `worker/worker.go` — Extract `saveSession()` and `sendCallback()`

**Deduplication 1**: Session save + error log (lines 140-148 vs 152-159) is duplicated. Extract:

```go
func (w *Worker) saveSession(sess *session.Session) {
    if err := w.sessions.Save(sess); err != nil {
        w.logger.Error("save session failed", "error", err.Error())
    }
}
```

**Deduplication 2**: Callback send structure (lines 132-138 vs 162-170) is duplicated. Extract:

```go
func (w *Worker) sendCallback(channelID, output, callbackURL string) {
    if callbackURL != "" {
        _ = webhook.SendCallback(channelID, output, callbackURL, w.logger)
    }
}
```

### 7C. Additions: `worker/worker_test.go` — Missing behaviors

| Test Case | Assertion |
|---|---|
| Session state after error: user message present | Session contains user message even when processing fails |
| Session saved on error path | Verify session file exists after error |
| `buildSystemPrompt` with AGENTS.md present | System prompt includes AGENTS.md content |
| `buildSystemPrompt` with all 5 prompt files | All 5 files included (AGENTS.md, SOUL.md, IDENTITY.md, USER.md, MEMORY.md) |
| `buildSystemPrompt` file delimiter format | `"--- END FILENAME ---"` delimiters present |
| Message enqueued after worker starts mid-poll | Eventually picked up by worker |

### 7D. Fix: `main_test.go` — Dead code + trivial tests

| Fix | Details |
|---|---|
| Dead variable `workingDir` | `main_test.go:77-196` — `workingDir` assigned then `_ = workingDir`. Remove both. |
| Trivial test removal | `TestIntegration_SignalNotify` (lines 1000-1013) — tests stdlib `signal.Notify`, not application behavior. Remove entirely. |
| Unused httptest server | `TestIntegration_WebhookServer` (lines 361-440) — `ts` created then assigned to `_`. Remove the unused server or use it to actually test webhook behavior. |
| Magic string comparison | Checking for two typo variants of rejection message. Fix to check for the correct string. |

### 7E. Additions: `main_test.go` — Missing behavioral tests

| Test Case | Assertion |
|---|---|
| Webhook 503 behavior after `ws.Stop()` | POST to webhook returns 503 |
| Config validation failure for each individual missing field | Each required field triggers appropriate error |
| Worker cancellation mid-LLM-call | Context cancelled during processing — graceful handling |
| Tool registration completeness | All 4 `Register*Tools` calls verified |
| Graceful shutdown with worker actively processing message | Worker finishes or cancels cleanly |

### 7F. Code Quality: `main.go` — Extract drain loop

Extract shutdown drain loop (lines 161-170) to `drainPending(q, sessions, logger)` for testability. This is a small extraction that doesn't change behavior.

### Phase 7 Validation

```bash
gofmt -d . ./worker/
go vet ./...
go test ./...
go test -race ./...
go build ./...
```

---

## Phase 8: LLM Client + Final Polish

**Packages**: `llm`, remaining TODO items

**Rationale**: LLM client has 87% coverage but a 116-line `parseSSE()` that violates SRP. This is a complex refactor (tool call accumulation logic) that should be last since it touches the core chat loop.

**Target coverage gain**: llm 87% → 95%+

### 8A. Refactor: `llm/client.go` — `parseSSE()` decomposition

`parseSSE()` (lines 245-360) handles SSE line parsing, JSON unmarshaling, content/reasoning/tool-call accumulation, and error classification. Split into:

1. **`readSSELines(reader io.Reader) <-chan string`** — Reads raw SSE lines, filters for `data: ` prefix, handles `[DONE]`
2. **`accumulateDelta(choice sseChoice, content *strings.Builder, reasoning *strings.Builder, toolCalls map[int]*accumTC)`** — Processes a single delta choice
3. **Main loop** — Orchestrates reading and accumulation

The tool call logic should become a standalone helper type:

```go
type ToolCallAccumulator struct {
    calls map[int]*accumTC
}

func (a *ToolCallAccumulator) Add(choice sseChoice) { ... }
func (a *ToolCallAccumulator) Finalize() []ToolCall { ... }
```

### 8B. Additional: `llm/client.go` — `Chat()` request-building extraction

Extract request-building logic from `Chat()` into `buildRequest(messages []Message, toolsJSON json.RawMessage, maxTokens int) (*http.Request, error)`.

### 8C. Additions: `llm/client_test.go` — Missing behaviors

| Test Case | Assertion |
|---|---|
| Multiple choices in one SSE chunk | All choices accumulated |
| Combined tool call + text content in response | Both present in ChatResponse |
| `isContextError()` direct behavior testing | Returns true for context errors, false for others |
| HTTP client timeout behavior | Timeout triggers error |
| Trailing `/` trim in baseURL | `http://host/` normalized to `http://host` |
| `Content-Type: application/json` header on outgoing request | Header is set |
| Debug logging of request payload | Payload logged at debug level |
| `writePartialResponse` — no-op when all fields empty | No file written |
| Tool calls written to partial response file | File contains tool call data |
| Partial response file creation error handling | Unwritable logDir handled gracefully |
| Partial response file write error handling | Write error handled gracefully |

### 8D. Code Quality: `llm/client_test.go` — Shared SSE server helper

Extract duplicated SSE server setup from 3 tests into `sseServer(t *testing.T, chunks []string) *httptest.Server` to save ~60 lines of boilerplate.

Replace flaky timing-dependent tests (busy-wait loops with `time.Sleep(10ms)`) with `sync.Cond` or channel signals for deterministic partial response testing.

### 8E. Remaining TODO items

Address any remaining unchecked boxes from TODO.md not covered in Phases 1-8:

- `tools/tools.go` — `panic()` on marshal error should return `error` instead (change `Register` signature)
- `tools/file_tools.go` — any remaining unchecked tests from TODO
- `tools/bash_tool.go` — any remaining unchecked tests from TODO

### Phase 8 Validation

```bash
gofmt -d ./llm/ ./tools/
go vet ./...
go test ./...
go test -race ./...
go build ./...
```

---

## Execution Notes for Future Sessions

### General Rules

1. **Read the entire phase section before starting.** Do not skip to the end.
2. **Read each production file before editing it.** Verify exact whitespace and indentation.
3. **Use table-driven tests** for new tests and when converting existing tests where it makes sense.
4. **Deep assertions**: Parse JSON into structs, assert individual fields. Do not use `strings.Contains` on JSON.
5. **Moderate refactoring only**: Do exactly what TODO.md asks. Do not improve architecture beyond what's specified.
6. **Run the full writing cycle after completing each phase**, not after each individual change.
7. **If a refactor breaks tests, fix the tests before proceeding.** The refactor must be behavior-preserving.
8. **Ask questions** If you have any questions, you should ask those questions and then stop and allow me to take a turn. If you're not sure if you have a question or not, that's a sign you have a question, and you should stop and ask.

### Risk Assessment by Phase

| Phase | Risk | Reason |
|---|---|---|
| 1 | **Low** | New test files only, no production changes |
| 2 | **Low** | New test files + small extraction |
| 3 | **Low-Medium** | `client` is standalone; channellog defer fix is trivial |
| 4 | **Medium** | Queue API change (Enqueue return type) touches webhook |
| 5 | **Medium** | Session DrainAndSave refactor, config Validate split |
| 6 | **High** | Agent.Process() is the core loop; behavior must be preserved exactly |
| 7 | **Medium** | Worker extraction + dead code cleanup |
| 8 | **High** | parseSSE() is complex; tool call accumulation must be preserved exactly |

### If a Phase Fails Validation

1. Check `go vet` output for new issues
2. Check `go test` output for specific failures
3. Check `go test -race` for data races
4. If a refactor changed behavior, revert the refactor and keep only the test additions
5. Document the failure in this file under a "## Phase N Issues" subheading

### Cross-Phase Dependencies

- **Phase 2 depends on Phase 1**: tools tests use sandbox and imageutil
- **Phase 6 depends on Phase 2**: agent tests exercise tools
- **Phase 7 depends on Phase 6**: worker uses refactored agent
- **Phase 8 depends on Phase 6**: llm tests may use agent-level patterns
- **Phase 4 (Queue Enqueue API change)**: If you change the return signature, you MUST update `webhook/server.go:170` in the same phase

### Files Modified Per Phase (Summary)

| Phase | New Files | Modified Files |
|---|---|---|
| 1 | `imageutil/imageutil_test.go`, `sandbox/sanitize_test.go`, `session/summary_test.go` | `sandbox/sandbox_test.go` |
| 2 | `tools/image_tool_test.go`, `tools/web_tools_test.go` | `tools/web_tools.go` |
| 3 | (additions to existing) | `client/client_test.go`, `client/client.go`, `channellog/channellog.go`, `channellog/channellog_test.go`, `log/logger_test.go` |
| 4 | (additions to existing) | `webhook/server_test.go`, `webhook/callback_test.go`, `queue/queue.go`, `queue/queue_test.go` |
| 5 | (additions to existing) | `session/session.go`, `session/session_test.go`, `config/config.go`, `config/config_test.go`, `config/ini_parser.go`, `config/ini_parser_test.go` |
| 6 | (additions to existing) | `agent/agent.go`, `agent/agent_test.go` |
| 7 | (additions to existing) | `worker/worker.go`, `worker/worker_test.go`, `main.go`, `main_test.go` |
| 8 | (additions to existing) | `llm/client.go`, `llm/client_test.go`, `tools/tools.go`, `tools/file_tools.go`, `tools/bash_tool.go` |

---

## Baseline Metrics (Pre-Remediation)

| Package | Coverage | Notes |
|---|---|---|
| `imageutil` | 0.0% | No test file |
| `client` | 26.3% | Minimal coverage |
| `sandbox` | 59.1% | SanitizeFilename untested |
| `tools` | 61.0% | image_tool.go and web_tools.go at 0% |
| `webhook` | 66.3% | Validation gaps |
| `channellog` | 74.2% | Surface assertions only |
| `agent` | 77.3% | Missing deep assertions |
| `worker` | 78.7% | go vet error |
| `session` | 82.4% | DRY violation |
| `llm` | 87.3% | parseSSE() too long |
| `log` | 92.3% | Edge cases missing |
| `config` | 95.3% | Missing assertions |
| `queue` | 100.0% | API issues |
| **Total** | **65.8%** | — |

## Target Metrics (Post-Remediation)

| Package | Target Coverage | Notes |
|---|---|---|
| `imageutil` | 100% | Full coverage |
| `client` | 60%+ | String manipulation + edge cases |
| `sandbox` | 85%+ | SanitizeFilename + ResolvePath gaps |
| `tools` | 80%+ | image_tool + web_tools covered |
| `webhook` | 85%+ | Full validation test suite |
| `channellog` | 90%+ | Deep assertions |
| `agent` | 90%+ | All behaviors tested |
| `worker` | 88%+ | Missing behaviors covered |
| `session` | 92%+ | Edge cases + DRY fix |
| `llm` | 95%+ | parseSSE tests + refactor |
| `log` | 98%+ | Edge cases |
| `config` | 98%+ | All assertions + refactor |
| `queue` | 100% | Defensive fixes |
| **Total** | **~85%+** | — |
