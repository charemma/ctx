# CLAUDE.md

This file provides guidance to Claude Code when working with code in this repository.

## What ctx is

ctx is an AX (AI Experience) platform for enterprise knowledge. It connects scattered company knowledge from sources like Confluence, GitHub, Slack, and Jira into a structured, searchable knowledge store backed by Postgres + pgvector. It delivers context-aware results to the tools people already use -- IDEs (via MCP server), CLI (for CI/CD), chat, email, and more.

The core concept is a bidirectional knowledge store: not just read-only search, but a living store that both humans and AI agents can read from and write to.

## Tech stack

- Go (single binary for CLI, MCP server, and web UI)
- Postgres + pgvector (knowledge store with vector search)
- Cobra (CLI framework)
- Charmbracelet (lipgloss, huh) for terminal UI
- templ + htmx for web UI (planned)

## Development commands

Build and run:
- `just build` -- build binary to `bin/ctx`
- `just dev` -- run via `go run`
- `just dev version` -- quick run with subcommand

Testing and checks:
- `just test` -- run all tests
- `just lint` -- run golangci-lint
- `just fmt` -- format code

Database:
- `just db-up` -- start dev Postgres (docker compose)
- `just db-down` -- stop dev Postgres

## Project structure

```
main.go              -- entry point, version injection via ldflags
cmd/                  -- Cobra commands
  root.go             -- root command, global flags (--config)
  version.go          -- version subcommand
internal/
  config/             -- configuration loading (~/.config/ctx/ctx.yaml)
docs/
  decisions/          -- Architecture Decision Records
```

## Key conventions

- Config file: `~/.config/ctx/ctx.yaml` (overridable via `--config` flag or `CTX_HOME` env var)
- Environment overrides: `CTX_DATABASE_URL`, `CTX_HOME`, `OPENAI_API_KEY`
- Tests use `CTX_HOME` pointed at `t.TempDir()` to isolate state
- Version info injected via ldflags (`-X main.version=...`)
