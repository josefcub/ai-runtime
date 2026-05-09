# System Overview
The Agent Harness is a minimal autonomous agent system written in Go (stdlib only, zero external dependencies). It receives messages via webhook, queues them FIFO, and processes them through an LLM tool-call loop. Each channel gets an isolated session with persistent state.

## Key design principles:

  * Single Go binary, no external dependencies
  * Custom INI config parser with multiline (""") support
  * FIFO message queue with per-channel backpressure
  * Flat JSON session persistence (one file per channel)
  * Per-channel conversation logging (channellog)
  * Sandboxed filesystem tools (path-traversal blocked)
  * OpenAI-compatible LLM API with SSE streaming
  * CLI client for local testing (client/client.go)
