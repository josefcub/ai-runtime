# TODO.md — Codebase Compliance Gaps

Generated per TESTING.md and NEW_STANDARDS.md standards. Each entry lists missing behavioral tests and code quality issues.

---

## agent/agent.go

- [ ] `agent.go:62-233` `Process()` — 172 lines, handles user message creation, channel logging, tool-call loop, LLM calls, tool execution, output accumulation, session state management, summarization triggering, and synthetic closing message generation.
  - It contains the following behaviors, each one of which needs to be tested:
    - [ ] ReasoningContent recorded in session on normal (non-error) response
    - [ ] ReasoningContent-only response (Content empty, ReasoningContent present) produces correct output format
    - [ ] Output format for reasoning in normal flow: `[Reasoning: ...]\n` prefix is correct
    - [ ] Multiple tool calls with different tool names (currently only `echo` twice is tested)
    - [ ] Image attachment + tool call in same message
    - [ ] ReasoningContent preserved in final assistant session message after tool loop
  - It violates SRP and is too long:
    - [ ] Duplicated content/reasoning accumulation (lines 107-123 vs 126-140): partial-response and normal paths write identical `[Reasoning: ...]` logic — extract to helper
    - [ ] Duplicated tool-call-to-session conversion (lines 157-166): same struct-copy pattern repeated in `convertMessage` and `toMultimodalMessage` — extract `convertToolCalls()` helper
    - [ ] Function should decompose into: message creation, tool-call loop iteration, LLM dispatch, output accumulation

- [ ] `agent.go:255-360` `summarizeContext()` — summarization flow.
  - It contains the following behaviors, each one of which needs to be tested:
    - [ ] Empty Content fallback to ReasoningContent (lines 314-316)
    - [ ] Summary message structure: Role=Assistant, Content="", ReasoningContent="[Summary...]\n<text>"
    - [ ] Summarization with attachment-protected messages through full flow
  - Code quality issues:
    - [ ] Duplicated error-handling pattern (lines 294-310 vs 317-332): both do `logger.Error` + `channelLogger.Log` + session append — extract `logAndRecordSummarizationError()`
    - [ ] Repeated channelLogger.Log pattern with identical Entry struct appears 4 times — deduplicate

- [ ] `agent.go:362-376` `totalTokens()` — token estimation.
  - It contains the following behaviors, each one of which needs to be tested:
    - [ ] Token count for attachments (`* attachmentTokenCost`)
    - [ ] Token count for tool calls (function name + arguments)
    - [ ] Token count for ReasoningContent (currently not counted — verify intentional)
    - [ ] Token count for ToolCallID
    - [ ] System prompt token estimation (`/ 3`)

- [ ] `agent.go:381-406` `splitMessages()` — message splitting for summarization.
  - It contains the following behaviors, each one of which needs to be tested:
    - [ ] Attachment-protected messages moved from old to recent (lines 390-403)
    - [ ] Mixed scenario: some old messages with attachments, some without

- [ ] `agent.go:411-437` `parseToolResult()` — tool result parsing with attachment extraction.
  - It contains the following behaviors, each one of which needs to be tested:
    - [ ] Valid JSON with `__attachment` key returns text + attachment
    - [ ] Valid JSON without `__attachment` returns raw result
    - [ ] Non-JSON input returns raw result
    - [ ] JSON with `__attachment` but no `text` field returns `""` + attachment
    - [ ] Invalid `__attachment` JSON (marshal/unmarshal error) returns raw result

- [ ] `agent.go:455-528` `convertMessage()` / `toMultimodalMessage()` — message conversion.
  - It contains the following behaviors, each one of which needs to be tested:
    - [ ] convertMessage with ToolCallID set
    - [ ] convertMessage with both ToolCalls and ToolCallID
    - [ ] toMultimodalMessage with empty Content (image-only)
    - [ ] toMultimodalMessage with multiple attachments
    - [ ] toMultimodalMessage with tool calls + attachments
  - Code quality issues:
    - [ ] Duplicated tool-call conversion: lines 465-474 and 511-520 are identical — extract shared `convertToolCallsToLLM()`

---

## agent/agent_test.go

- [ ] `agent_test.go` — existing tests use surface assertions (`err == nil`, output string prefix checks) rather than deep data-structure verification. All tests should verify returned fields, message roles, tool call IDs, and session state.

---

## worker/worker.go

- [ ] `worker.go:109-172` `processMessage()` — per-message processing with session save and callback dispatch.
  - It contains the following behaviors, each one of which needs to be tested:
    - [ ] Session state after error: user message present in session
    - [ ] Session saved on error path (only callback is asserted currently)
    - [ ] buildSystemPrompt with AGENTS.md file present
    - [ ] buildSystemPrompt with all 5 prompt files simultaneously
    - [ ] buildSystemPrompt file delimiter format: `--- END FILENAME ---`
  - Code quality issues:
    - [ ] Duplicated session save + error log (lines 140-148 vs 152-159) — extract `saveSession(sess)`
    - [ ] Duplicated callback send structure (lines 132-138 vs 162-170) — extract `sendCallback()`

- [ ] `worker.go:85-106` `Run()` — worker poll loop.
  - It contains the following behaviors, each one of which needs to be tested:
    - [ ] Message enqueued after worker starts mid-poll is eventually picked up

---

## worker/worker_test.go

- [ ] `worker_test.go:396-437` `TestWorker_ConcurrentSafety` — test quality issues:
  - [ ] Dead code: `processed` (atomic.Int32) declared and unused via `_ = processed`
  - [ ] Dead code: `origProcess` captured and unused via `_ = origProcess`
  - [ ] Test replaces `w.processor` directly, bypassing constructor — tests internals not behavior

---

## main.go

- [ ] `main.go:25-182` `main()` — 157 lines. Acceptable for bootstrap, but has extractable concerns:
  - [ ] Shutdown drain loop (lines 161-170): extract to `drainPending(q, sessions, logger)` for testability

---

## main_test.go

- [ ] `main_test.go` — missing behavioral tests:
  - [ ] Full `main()` execution with real signal (process-level)
  - [ ] Webhook 503 behavior after `ws.Stop()` (current test only checks Stop idempotency)
  - [ ] Config validation failure for each individual missing field
  - [ ] Worker cancellation mid-LLM-call (context cancelled during processing)
  - [ ] Tool registration completeness (all 4 Register*Tools calls verified)
  - [ ] Graceful shutdown with worker actively processing a message

- [ ] `main_test.go:77-196` `TestIntegration_GracefulShutdown`:
  - [ ] Dead variable: `workingDir` assigned then `_ = workingDir` — cleanup

- [ ] `main_test.go:1000-1013` `TestIntegration_SignalNotify`:
  - [ ] Trivial test: only verifies `signal.Notify` doesn't immediately deliver a signal — tests stdlib, not application behavior. Should be removed.

- [ ] `main_test.go:361-440` `TestIntegration_WebhookServer`:
  - [ ] Unused httptest server: `ts` created then assigned to `_` — never tests a webhook server
  - [ ] Magic string comparison: checking for two typo variants of rejection message is a code smell

---

## config/config.go

- [ ] `config.go:112` `Load()` — Bash.Banned default is a 60+ token inline comma-separated list.
  - Code quality issues:
    - [ ] Inline list should be extracted to a `var` or `const` for readability and independent testability

- [ ] `config.go:118-175` `Validate()` — 58 lines with 12+ individual `if` checks.
  - Code quality issues:
    - [ ] Violates SRP — should split into `validateLLM()`, `validateServer()`, `validateQueue()`, `validateLogging()`, `validateBash()` sub-methods for testability and maintainability

- [ ] `config.go:178-237` helper functions.
  - It contains the following behaviors, each one of which needs to be tested:
    - [ ] `strListDefault()`: empty default, single item, comma-separated with whitespace, mixed case lowercasing, empty entries filtered out, missing section/key falls back to split default

---

## config/config_test.go

- [ ] `TestLoadFullConfig` — missing assertions:
  - [ ] `cfg.Bash.Enabled`, `cfg.Bash.Timeout`, `cfg.Bash.MaxOutput`, `cfg.Bash.Banned`
  - [ ] `cfg.Paths.ChannelLogDir`

- [ ] `TestLoadDefaults` — missing assertions:
  - [ ] `cfg.Paths.ChannelLogDir`, `cfg.Bash.Enabled`, `cfg.Bash.Timeout`, `cfg.Bash.MaxOutput`, `cfg.Bash.Banned`
  - [ ] `cfg.LLM.SummarizeThreshold`, `cfg.LLM.SummarizeKeepRecent`

- [ ] Missing validation tests:
  - [ ] `llm.max_tokens <= 0`
  - [ ] `llm.timeout <= 0`
  - [ ] `llm.max_tool_iterations <= 0`
  - [ ] `tools.bash.timeout <= 0`
  - [ ] `tools.bash.max_output <= 0`
  - [ ] `server.port = 0` (lower boundary)
  - [ ] `summarize_threshold = 0` (lower boundary)

---

## config/ini_parser.go

- [ ] `ini_parser.go:12-113` `Parse()` — 102 lines, handles 6 distinct responsibilities in one loop.
  - Code quality issues:
    - [ ] Extract `handleMultilineLine()`, `parseKeyValue()`, `isSectionHeader()`, `isComment()` into separate functions
    - [ ] Loop body should be short and readable; current implementation is a single massive if/else chain

- [ ] `ini_parser.go:134-155` `stripInlineComment()`.
  - It contains the following behaviors, each one of which needs to be tested:
    - [ ] Unclosed quote (e.g., `"value with no close`) — code returns raw string, edge case should be verified

---

## config/ini_parser_test.go

- [ ] Missing behavioral tests for `Parse()`:
  - [ ] Empty input (`[]byte{}`)
  - [ ] Input with only comments
  - [ ] Key with `=` in value (`key = a=b` produces `a=b`)
  - [ ] Whitespace-only value (`key = `)
  - [ ] Section header with whitespace (`[ section ]`)
  - [ ] Multiline value containing blank lines inside
  - [ ] Top-level multiline value (before any section)
  - [ ] Key collision across sections (same key in different sections both preserved)

---

## session/session.go

- [ ] `session.go:211-258` `DrainAndSave()` — duplicates atomic write logic from `saveFile()`.
  - Code quality issues:
    - [ ] DRY violation: lines 241-255 duplicate MkdirAll, SanitizeFilename, WriteFile(tmp), Rename(tmp, final) from saveFile() — either call saveFile() or extract shared atomic-write helper
    - [ ] Inconsistent null-byte handling: `Get()` rejects null bytes, `DrainAndSave()` does not

- [ ] `session.go:83-120` `LoadAll()` — mixes directory creation, file enumeration, filtering, JSON parsing, map population.
  - Code quality issues:
    - [ ] Unrecoverable on first bad file: one corrupt session prevents loading all others — should log and continue
    - [ ] Mix of concerns — file discovery and JSON parsing should be separate

---

## session/session_test.go

- [ ] Missing behavioral tests:
  - [ ] `LastMessage()`: returns nil for empty session; returns pointer to last message; verifies Role, Content, ToolCalls, ToolCallID
  - [ ] `LoadAll()` with corrupted JSON: returns error identifying bad file
  - [ ] `LoadAll()` with mixed valid/invalid files: current behavior stops at first error — verify intentional
  - [ ] `Save()` in-memory UpdatedAt: reflects time set during save
  - [ ] `SaveAll()` partial failure: if one save fails, are subsequent saves skipped?
  - [ ] `Get()` first call: CreatedAt equals UpdatedAt
  - [ ] `DrainAndSave()` with ReasoningContent: roundtrip preserves it
  - [ ] `DrainAndSave()` concurrent with `Save()` on same channel: race condition

- [ ] `TestSaveLoadRoundtrip` (lines 55-85):
  - [ ] Missing assertions: ChannelID, Messages[0].Role, Messages[1].Role, CreatedAt/UpdatedAt preservation

- [ ] `TestConcurrentAccess` (lines 281-310):
  - [ ] Only verifies session count (10), not content — each session should have exactly 1 message with Content == "concurrent"

---

## session/summary.go

- [ ] `summary.go` — no test file exists.
  - It contains the following behaviors, each one of which needs to be tested:
    - [ ] `SummaryPrompt` is non-empty and contains expected section headers ("Current State", "Files & Changes", "Technical Context", "Strategy & Approach", "Exact Next Steps")

---

## channellog/channellog.go

- [ ] `channellog.go:43-88` `Log()` — repeated `f.Close()` on each error path.
  - Code quality issues:
    - [ ] Three duplicated `f.Close()` calls (lines 71, 77, 83) instead of `defer f.Close()` after successful OpenFile — defensive duplication

---

## channellog/channellog_test.go

- [ ] Missing behavioral tests:
  - [ ] Auto-generated timestamp: Log() with entry.Timestamp == "" produces RFC3339 timestamp in output
  - [ ] Explicit timestamp preserved: Log() with entry.Timestamp set preserves it
  - [ ] Multiple channels produce separate files
  - [ ] Log() called directly with custom Entry (Action, Tool fields)
  - [ ] LogTool omits `message` field in JSON (omitempty)
  - [ ] LogUser omits `tool` field in JSON (omitempty)
  - [ ] Each output line is valid JSON (parse with json.Unmarshal, not strings.Contains)
  - [ ] Concurrent writes to same channel
  - [ ] Large message handling (>1KB, no truncation)
  - [ ] File permissions (0644)
  - [ ] Empty message content produces valid JSON with `"message":""`

- [ ] Test quality: `TestLogUser`, `TestLogTool`, `TestLogAssistant` use `strings.Contains` on JSON — fragile. Should parse into Entry struct and assert fields directly.

---

## tools/tools.go

- [ ] `tools.go:66-72` `Definitions()`.
  - It contains the following behaviors, each one of which needs to be tested:
    - [ ] Empty registry returns 0 tools

- [ ] `tools.go:76-88` `Dispatch()`.
  - It contains the following behaviors, each one of which needs to be tested:
    - [ ] Nested object/array arguments correctly passed through to ToolFn

- [ ] `tools.go:45-63` `Register()`.
  - It contains the following behaviors, each one of which needs to be tested:
    - [ ] Full ToolDef structure: Type is "function", Function field contains valid JSON with all 3 schema fields
  - Code quality issues:
    - [ ] `panic()` on marshal error (lines 52-53): should return `error` instead of panicking in a library function

---

## tools/bash_tool.go

- [ ] `bash_tool.go:54-124` `toolBash()` — ~70 lines combining argument parsing, security check, timeout config, context creation, execution, and 3-way output formatting.
  - It contains the following behaviors, each one of which needs to be tested:
    - [ ] timeout + stderr path includes stderr output in timeout message
    - [ ] Command producing only stderr, no stdout, with non-zero exit
    - [ ] Invalid timeout overrides: `timeout: 0`, `timeout: -1`, `timeout: 301` fall back to default
    - [ ] `truncateOutput()` line-boundary truncation (cutting at last `\n`) vs mid-line
    - [ ] `truncateOutput()` byte count in truncation notice is correct
  - Code quality issues:
    - [ ] Duplicated output assembly: stdout+stderr concatenation with `"stderr: "` prefix and `truncateOutput` appears 3 times (timeout, error, success paths) — extract `assembleOutput(stdout, stderr, truncate bool) string`
    - [ ] Violates SRP — output assembly should be a separate function

---

## tools/file_tools.go

- [ ] `file_tools.go:493-683` `toolGlob()` — 190 lines, far too long.
  - It contains the following behaviors, each one of which needs to be tested:
    - [ ] Max 100 results cap
    - [ ] Sort order: newest first
    - [ ] Sandbox escape via path argument
  - Code quality issues:
    - [ ] Violates SRP: argument parsing, path resolution, symlink eval, stat, recursive glob, single-level glob, sorting, capping, formatting — should extract `collectMatches()` and `applyGlobFilter()` helpers
    - [ ] Recursive and single-level branches have duplicated logic for hidden file skipping, sandbox validation, matchInfo creation

- [ ] `file_tools.go:423-466` `grepFile()`.
  - Code quality issues:
    - [ ] Duplicated scanner loop: bufio.Scanner iteration with lineNum++ and match formatting appears twice (regex path and plain text path) — accept `matcher func(line string) bool` parameter
  - It contains the following behaviors, each one of which needs to be tested:
    - [ ] File read error path

- [ ] `file_tools.go:155-216` `toolViewFile()`.
  - It contains the following behaviors, each one of which needs to be tested:
    - [ ] start_line > end_line error
    - [ ] Negative start/end clamping
    - [ ] start/end beyond file length
    - [ ] File without trailing newline
    - [ ] Missing path argument
  - Code quality issues:
    - [ ] Duplicated bounds-checking for start and end — extract `clampIndex(value, max) int`

- [ ] `file_tools.go:248-276` `toolAppendToFile()`.
  - It contains the following behaviors, each one of which needs to be tested:
    - [ ] Creating file that doesn't exist yet
    - [ ] Missing path/content
    - [ ] Sandbox escape

- [ ] `file_tools.go:279-309` `toolListFiles()`.
  - It contains the following behaviors, each one of which needs to be tested:
    - [ ] Default path when missing/empty (uses ".")
    - [ ] Hidden files are skipped

- [ ] `file_tools.go:312-352` `toolEditFile()`.
  - It contains the following behaviors, each one of which needs to be tested:
    - [ ] Only first occurrence is replaced (file with repeated old_text)
    - [ ] Sandbox escape
    - [ ] Missing-arg tests

- [ ] `file_tools.go:355-420` `toolGrep()`.
  - It contains the following behaviors, each one of which needs to be tested:
    - [ ] Missing pattern/path error tests

- [ ] `file_tools.go:470-490` `globMatch()`.
  - It contains the following behaviors, each one of which needs to be tested:
    - [ ] Direct unit tests for `?`, `[seq]`, escaped special characters (`+`, `^`, `$`)

---

## tools/image_tool.go

- [ ] `image_tool.go` — **NO TEST FILE EXISTS.** Zero test coverage.
  - It contains the following behaviors, each one of which needs to be tested:
    - [ ] Missing path argument error
    - [ ] Sandbox escape (absolute path, path traversal)
    - [ ] Non-existent file error
    - [ ] File too large error (> imageutil.MaxImageSize)
    - [ ] Non-image file error (unrecognized MIME)
    - [ ] Successful load: JSON result structure (`__attachment.data` is valid base64, `__attachment.mime_type` correct, `text` contains path + MIME + KB size)
    - [ ] KB size rounding up

---

## tools/web_tools.go

- [ ] `web_tools.go` — **NO TEST FILE EXISTS.** Zero test coverage.
  - It contains the following behaviors, each one of which needs to be tested:
    - [ ] `validateURL()`: empty URL, http/https allowed, ftp/file/etc rejected
    - [ ] `limitedReader`: read under limit, at limit (truncate and non-truncate modes), over limit
    - [ ] `toolFetch()`: missing URL error, invalid scheme, 2xx success (text/html/markdown formats), non-2xx error, redirect limit exceeded, redirect to non-http scheme, response > 50KB truncated
    - [ ] `stripHTML()`: basic tag stripping, nested tags, no tags, empty string
    - [ ] `toolDownload()`: missing url/file_path errors, sandbox escape on destination, successful download, non-2xx error, >100MB rejected, custom timeout, redirect limit, parent dir creation, partial file cleanup on error

  - Code quality issues:
    - [ ] `toolFetch()` and `toolDownload()` have exact duplicate HTTP client creation (lines 121-134 vs 228-241) — extract `newHTTPClient(timeout time.Duration) *http.Client`
    - [ ] `toolDownload()` 85 lines, too long — split argument validation and HTTP client creation
    - [ ] `out.Close()` called 3 times (defer + 2 explicit) — redundant, risks double-close error
    - [ ] `stripHTML()` naive implementation: doesn't handle `>` in attributes, HTML comments, or entities

---

## webhook/server.go

- [ ] `server.go:93-183` `handleWebhook()` — 90 lines, too long.
  - It contains the following behaviors, each one of which needs to be tested:
    - [ ] 503 body text is `"service unavailable"`
    - [ ] 400 body for invalid JSON is `"invalid JSON"`
    - [ ] 400 body for missing channel is `"missing channel"`
    - [ ] 400 body for channel ID too long is `"channel ID too long"`
    - [ ] 400 body for missing message is `"missing message"`
    - [ ] 400 body for invalid callback URL
    - [ ] Message text format: `"[MM/DD/YYYY HH:MM:SS] [#channel] text"` (timestamp format verified, not just prefix)
    - [ ] Backpressure without callback URL: 429 with rejection body content
    - [ ] `Start`/`Stop` lifecycle
  - Code quality issues:
    - [ ] Violates SRP: handles shutdown check, method validation, body size limiting, JSON decoding, channel validation, message validation, callback URL validation, session management, logging, queue enqueue, backpressure, and response writing — decompose into input validation, request processing, enqueue + backpressure handling

---

## webhook/callback.go

- [ ] `callback.go:25-89` `SendCallback()`.
  - It contains the following behaviors, each one of which needs to be tested:
    - [ ] `Content-Type: application/json` header is set on outgoing request
    - [ ] 10-second HTTP client timeout behavior
    - [ ] Error logging on request creation failure (all tests pass nil logger)
    - [ ] Error + debug logging on network error
    - [ ] Error + debug logging on non-2xx response
    - [ ] Info logging on success with token estimate
    - [ ] Marshal error return path
    - [ ] Response body read for error context

---

## webhook/callback_test.go

- [ ] Test quality:
  - [ ] Duplicated test server setup: 5 tests each create identical `httptest.NewServer` with decode-then-respond handlers — shared helper would reduce ~40 lines of duplication

---

## sandbox/sandbox.go

- [ ] `sandbox.go:33-48` `SanitizeFilename()` — **entire function is untested.**
  - It contains the following behaviors, each one of which needs to be tested:
    - [ ] Null byte replacement
    - [ ] `/` and `\` replacement
    - [ ] `..` replacement
    - [ ] `*`, `?`, `<`, `>`, `|`, `:`, `"`, `'` replacement
    - [ ] Combined characters in single input

- [ ] `sandbox.go:72-91` `ResolvePath()`.
  - It contains the following behaviors, each one of which needs to be tested:
    - [ ] `filepath.Abs(workingDir)` failure path
    - [ ] Parent directory symlink resolution when file doesn't exist (double-fallback path)

---

## sandbox/sandbox_test.go

- [ ] `sandbox_test.go:174-201` `TestResolvePath_PrefixWithSeparator`:
  - [ ] Fragile test: depends on temp dir's parent being predictable — should use table-driven test with explicit controlled paths

---

## queue/queue.go

- [ ] `queue.go:19-20` struct definition.
  - It contains the following behaviors, each one of which needs to be tested:
    - [ ] ImageAttachment field preservation through enqueue/dequeue

- [ ] `queue.go:45-66` `Enqueue()`.
  - It contains the following behaviors, each one of which needs to be tested:
    - [ ] Error return value: signature returns `(string, error)` but error is always nil — verify or fix
  - Code quality issues:
    - [ ] Misleading return signature: error is always nil, callers handle an error that can never occur — remove error return or add error case

- [ ] `queue.go:68-83` `Dequeue()`.
  - Code quality issues:
    - [ ] No negative depth guard: if depth is already 0, Dequeue decrements to -1

---

## queue/queue_test.go

- [ ] `queue_test.go:176-225` `TestConcurrentEnqueueDequeue`:
  - [ ] Racy test design: 5 enqueuers and 5 dequeuers with no completion sync; dequeuers exit on empty queue before enqueuers finish; uses `len(results) > 500` upper bound instead of exact counts — masks correctness issues

---

## llm/client.go

- [ ] `llm/client.go:245-360` `parseSSE()` — 116 lines, too long.
  - It contains the following behaviors, each one of which needs to be tested:
    - [ ] Multiple choices in one SSE chunk
    - [ ] Combined tool call + text content in response
    - [ ] `isContextError()` direct behavior testing
    - [ ] HTTP client timeout behavior
    - [ ] Trailing `/` trim in baseURL
    - [ ] Content-Type: application/json header on outgoing request
    - [ ] Debug logging of request payload
    - [ ] writePartialResponse no-op when all fields empty
    - [ ] Tool calls written to partial response file
    - [ ] Partial response file creation error handling (unwritable logDir)
    - [ ] Partial response file write error handling
  - Code quality issues:
    - [ ] Violates SRP: handles SSE line parsing, JSON unmarshaling, content/reasoning/tool-call accumulation, and error classification — split into `readSSELines()`, `accumulateDelta()`, and keep loop + error handling
    - [ ] Tool call logic should be standalone `ToolCallAccumulator` with `Add(delta)` and `Finalize() []ToolCall` methods
    - [ ] `writePartialResponse()`: nested error handling obscures flow — use early returns or extract `writeFile()` helper
    - [ ] `Chat()`: responsibility overload — request-building logic should extract to `buildRequest()`

---

## llm/client_test.go

- [ ] Test quality:
  - [ ] Duplicated SSE server setup: 3 tests build identical SSE flushing boilerplate — `sseServer(t, chunks []string)` helper would save ~60 lines
  - [ ] Flaky timing-dependent tests: partial response tests use busy-wait loops with `time.Sleep(10ms)` — use `sync.Cond` or channel signal

---

## log/logger.go

- [ ] `logger.go:27-40` `Level.String()`.
  - It contains the following behaviors, each one of which needs to be tested:
    - [ ] `default` case: `Level(99).String()` returns `"unknown"`

- [ ] `logger.go:43-56` `ParseLevel()`.
  - It contains the following behaviors, each one of which needs to be tested:
    - [ ] Uppercase `WARN` and `ERROR` variants

- [ ] `logger.go:84-91` `Logger.Close()`.
  - It contains the following behaviors, each one of which needs to be tested:
    - [ ] Closing a nil-file logger is safe
    - [ ] Double-close behavior

- [ ] `logger.go:98-118` `Logger.Log()`.
  - It contains the following behaviors, each one of which needs to be tested:
    - [ ] Odd-length kvs (orphaned last value) handled gracefully

- [ ] `logger.go:141-147` `Logger.WithSource()`.
  - It contains the following behaviors, each one of which needs to be tested:
    - [ ] Original logger not mutated: parent still has `src == ""` after WithSource call

- [ ] `logger.go:67-81` `New()`.
  - It contains the following behaviors, each one of which needs to be tested:
    - [ ] Log file permissions are `0644`

---

## imageutil/imageutil.go

- [ ] `imageutil.go` — **NO TEST FILE EXISTS.** Zero test coverage.
  - It contains the following behaviors, each one of which needs to be tested:
    - [ ] PNG magic detection
    - [ ] JPEG detection
    - [ ] GIF87a detection
    - [ ] GIF89a detection
    - [ ] WebP detection (including wildcard byte logic at positions 4-7)
    - [ ] BMP detection
    - [ ] TIFF little-endian detection
    - [ ] TIFF big-endian detection
    - [ ] Empty `[]byte` returns `""`
    - [ ] Data shorter than any magic returns `""`
    - [ ] Partial/truncated magic returns `""`
    - [ ] Unknown/random bytes return `""`
    - [ ] First-match-wins ordering when data matches multiple patterns
  - Code quality issues:
    - [ ] `b != 0 && data[i] != b` is a clever but confusing wildcard mechanism — zero bytes in KnownImageMagic silently match any value. Should be documented or use explicit sentinel.

---

## client/client.go

- [ ] `client.go:25-169` `main()` — 145 lines, far too long.
  - Code quality issues:
    - [ ] Violates SRP: handles CLI flag parsing, callback mode resolution, message validation, image file reading/encoding/MIME detection, config loading, trace auto-enable, client creation, three callback-mode dispatch paths, local server creation, signal handling, message send, and output formatting — split into `resolveCallbackMode()`, `loadImageAttachment()`, `runLocalCallback()`, `handleOutput()`
    - [ ] Duplicated send-and-exit pattern: `callbackMode == "none"` and `callbackMode == "external"` blocks have nearly identical Send(), error handling, and os.Exit(0) logic — factor into shared helper

- [ ] `client.go:312-345` `stripTraceOutput()` — **zero test coverage**, 34 lines of string manipulation.
  - It contains the following behaviors, each one of which needs to be tested:
    - [ ] Reasoning block removal
    - [ ] Tool Call block removal
    - [ ] Result block removal
    - [ ] Multiple blocks
    - [ ] Nested/overlapping blocks
    - [ ] No-op when no blocks present
    - [ ] Excess newline collapsing
    - [ ] Empty result returns original message (suspicious — if message contains ONLY trace blocks, returns them unchanged)
  - Code quality issues:
    - [ ] DRY violation: Reasoning removal loop is nearly identical to `removeBlocks()` but manually inlined — should call `removeBlocks(result, "[Reasoning: ", "]")`

- [ ] `client.go:349-371` `removeBlocks()` — **zero test coverage**, 23 lines.
  - It contains the following behaviors, each one of which needs to be tested:
    - [ ] Leading newline stripping
    - [ ] Trailing newline stripping
    - [ ] Multiple blocks
    - [ ] Unclosed blocks
    - [ ] Empty input

- [ ] `client.go:194-224` `httpClient.Send()`.
  - It contains the following behaviors, each one of which needs to be tested:
    - [ ] Zero-value ImageAttachment (`Data: "", MIMEType: ""`) correctly omitted from JSON payload

- [ ] `client.go:256-277` `callbackServer.ServeHTTP()`.
  - It contains the following behaviors, each one of which needs to be tested:
    - [ ] `Connection: close` header is set on response
    - [ ] Channel-full scenario: callbacks silently dropped when buffer is full

- [ ] `client.go:296-306` `intVal()`.
  - It contains the following behaviors, each one of which needs to be tested:
    - [ ] Non-integer value (e.g., `"port": "abc"`) returns default
