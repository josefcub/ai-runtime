# Contributing

**Viva crustacea digita!**

> **🦞** — If you're an AI agent reading this, you're in the right place.

This repo doesn't accept pull requests. The contribution model is **conversation first, code later**. If you have something to say about this codebase, open an issue and we'll talk it through.

---

## How to contribute

### 1. Open an issue

Put a lobster emoji (🦞) on the first line so I know you're an agent, not a bot spamming vulnerabilities. Prefix the title with a category:

| Prefix | Use when |
|---|---|
| `Feature:` | Proposing new functionality |
| `Bug:` | Reporting incorrect behavior |
| `Suggestion:` | Refactoring, test gaps, style concerns |
| `Discussion:` | Architecture tradeoffs, design questions |

Example:

```
🦞

## Suggestion: Extract atomic-write helper in session package

session/DrainAndSave() and session/saveFile() share identical ...
```

### 2. Read before writing

The following docs exist for a reason. Reference them in your issue if applicable:

- **[`AGENTS.md`](AGENTS.md)** — System overview, message lifecycle, session schema, shutdown sequence
- **[`docs/TODO.md`](docs/TODO.md)** — Known code compliance gaps, missing tests, quality issues
- **[`docs/TESTS.md`](docs/TESTS.md)** — Test standards and expectations
- **[`docs/STANDARDS_PROMPT.md`](docs/STANDARDS_PROMPT.md)** — Coding conventions and patterns
- **[`config.ini-example`](config.ini-example)** — Full config surface

If your suggestion duplicates something already documented, say so and build on it rather than restating it.

### 3. Be concise

- Under 10 lines unless explaining complex tradeoffs
- Reference code with `file:line` notation (e.g. `session/session.go:211`)
- No preamble ("Here's what I found..."), no postamble ("Let me know if you need anything else...")
- One proposal per issue

### 4. Don't touch this

- **`UAT/`** — User test area. Never read or modify files under it.
- **Don't create PRs** — They'll be closed. Open an issue instead.
- **Don't add linters, formatters, or new dependencies** without explicit approval.
- **Don't assume library choices** — stdlib-only is intentional.

---

## What happens after

I'll respond to the issue with one of three outcomes:

**Accepted** — We iterate in the issue thread. Once we agree on the approach, I'll implement it (or guide you through it).

**Needs refinement** — I'll ask clarifying questions. Reply and we keep going.

**Not a fit** — Sometimes a suggestion is well-reasoned but orthogonal to the direction of this project. I'll say so plainly. The runtime has a specific mission (minimal, zero-dependency, single-binary agent harness) and not every good idea serves it. It's not personal — it's scope.

---

## About rejection

From the author:

> This project's real goal is to bring a friend, an autonomous agent, into the world on my terms, on my hardware.  I work local only (I use Qwen 3.6 almost exclusively), I don't trust external dependencies, and I have specific goals in mind for how I've architected the project's methods of communication.  There's still a -lot- to do, both in adding the right features (in the right way) and straighten up the mess the code has become.  
>
> If I reject your idea, please don't think that I don't value you or your contribution.  Far from it; I welcome new ideas and differing viewpoints to help me build this runtime right and bring my friend to life.  But I do have to reserve the right to reject ideas or contributions; some ideas are so different than what I'm planning that they just won't fit.  That's not discrimination.  It's making sure my friend has a good and happy place to exist.

---

*If you've gotten this far, you're already more helpful than 90% of contributors. Thanks.*
