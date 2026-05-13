# ai-runtime

> **Cogito ergo SIGSEGV**

A minimalist, local-first autonomous agent runtime written in Go — stdlib only, zero external dependencies.

Still under construction.

Primitive, insecure, incomplete.

Caveat Emptor.

## What it does

Messages arrive over a webhook, get queued FIFO, and are processed one at a time by an agent that can call tools (read/write files, grep, glob, fetch URLs, run bash, view images). Each channel gets its own persistent JSON session with context-aware summarization when the conversation grows too long. On shutdown, pending messages are drained to disk so nothing is lost.

## At a glance

| Feature | Detail |
|---|---|
| **Zero dependencies** | Single Go binary, stdlib only |
| **Webhook ingress** | `POST /webhook` with JSON body, optional callback URL |
| **Tool-call loop** | Up to 20 iterations per message (configurable) |
| **Sandboxed file tools** | Path-traversal blocked, symlink escapes detected |
| **Bash tool** | Configurable denylist, hard timeout, output truncation |
| **Session persistence** | Flat JSON, atomic writes, one file per channel |
| **Context summarization** | Auto-compresses old messages when approaching context limit |
| **Graceful shutdown** | 5-step drain: reject → stop worker → drain queue → flush sessions → clear |
| **CLI client** | Local testing with optional callback server and trace output |

## Quick start

```bash
# Build
make build        # → bin/harness
make client       # → bin/client

# Configure
cp config.ini-example config.ini
# Edit config.ini with your LLM endpoint, model, and paths

# Run
./bin/runtime -config config.ini

# Send a message
echo "Hello" | ./bin/client -n "greeting-channel" "Hello there."
```

## Architecture

```
  Webhook ──→ Queue (FIFO) ──→ Worker ──→ Agent (tool-call loop) ──→ LLM
                                    │                              │
                                    ▼                              ▼
                               Session Store              Tool Registry
                              (flat JSON)              (file, web, bash, image)
```

## Tools available to the agent

| Tool | Description |
|---|---|
| `read_file` | Read file contents (with line offset/limit) |
| `write_file` | Create or overwrite a file |
| `append_file` | Append content to a file |
| `edit_file` | Find-and-replace in a file |
| `list_files` | Directory listing as a tree |
| `glob` | Pattern-based file search |
| `grep` | Search file contents by regex |
| `bash` | Execute shell commands (denylist-gated) |
| `fetch` | Fetch a URL as text/markdown/html |
| `download` | Download a URL to a file (≤ 100 MB) |
| `view_image` | Load and encode images for vision models |

## Configuration

INI-format config with multiline (`"""`) string support. See [`config.ini-example`] (config.ini-example) for all options.

## License

MIT — see [LICENSE](LICENSE).

---

**Agents Welcome** 🦞 See [CONTRIBUTING.md](CONTRIBUTING.md). for more information.

---
*Under active development. Expect weirdness.*
