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
  db.go               -- db subcommand group (migrate, reset, status)
internal/
  config/             -- configuration loading (~/.config/ctx/ctx.yaml)
  store/              -- knowledge store (Postgres + pgvector)
    store.go          -- Store interface and model types
    postgres.go       -- PostgresStore implementation (pgx/v5 pool)
    postgres_test.go  -- integration tests (requires running Postgres)
    migrations/       -- embedded SQL migrations (embed.FS)
docs/
  decisions/          -- Architecture Decision Records
```

## Key conventions

- Config file: `~/.config/ctx/ctx.yaml` (overridable via `--config` flag or `CTX_HOME` env var)
- Environment overrides: `CTX_DATABASE_URL`, `CTX_HOME`, `OPENAI_API_KEY`
- Tests use `CTX_HOME` pointed at `t.TempDir()` to isolate state
- Version info injected via ldflags (`-X main.version=...`)

## Store layer

The `internal/store` package provides the persistence layer:

- `Store` interface defines all data operations (sources, documents, chunks, sync state)
- `PostgresStore` implements Store using pgx/v5 connection pool
- SQL migrations are embedded via `embed.FS` and applied automatically
- Migration runner creates a `schema_migrations` table to track applied versions
- Store integration tests require a running Postgres with pgvector; set `CTX_TEST_DATABASE_URL` or use the default `postgres://ctx:ctx@localhost:5432/ctx?sslmode=disable`
- Model types are plain structs (Source, Document, Chunk, SyncState, IngestEntry)
- pgvector-go is used for vector embedding types

## CLI subcommands

- `ctx db migrate` -- run pending database migrations
- `ctx db reset` -- drop all tables and re-run migrations (interactive confirmation, or `--yes` to skip)
- `ctx db status` -- show applied/pending migrations and table row counts
