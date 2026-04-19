# CLAUDE.md

This file provides guidance to Claude Code when working with code in this repository.

## What ctx is

ctx is an AX (AI Experience) platform for enterprise knowledge. It connects scattered company knowledge from sources like Confluence, GitHub, Slack, and Jira into a structured, searchable knowledge store backed by Postgres + pgvector. It delivers context-aware results to the tools people already use -- IDEs (via MCP server), CLI (for CI/CD), chat, email, and more.

The core concept is a bidirectional knowledge store: not just read-only search, but a living store that both humans and AI agents can read from and write to.

## Tech stack

- Go (single binary for CLI, MCP server, and web UI)
- SQLite (default, embedded via modernc.org/sqlite, pure Go) or Postgres + pgvector (optional, for teams/enterprise)
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
  init.go             -- init wizard (vault detection, config generation)
  sync.go             -- sync sources and embed chunks
  status.go           -- show system state
  search.go           -- semantic search
  serve.go            -- MCP and web server launcher
  db.go               -- db subcommand group (migrate, reset, status)
internal/
  config/             -- configuration loading (~/.config/ctx/ctx.yaml)
  store/              -- knowledge store (pluggable backend)
    store.go          -- Store interface and model types
    factory.go        -- store.New() factory, StoreWithExtras interface
    sqlite.go         -- SQLiteStore (default, pure Go, brute-force vector search)
    sqlite_test.go    -- SQLite tests (always run, no external deps)
    postgres.go       -- PostgresStore (pgx/v5 pool, pgvector)
    postgres_test.go  -- Postgres integration tests (requires running Postgres)
    vector.go         -- cosine similarity, embedding JSON serialization
    vector_test.go    -- vector math unit tests
    migrations/       -- embedded Postgres SQL migrations
    migrations_sqlite/ -- embedded SQLite SQL migrations
  search/             -- semantic search engine
    search.go         -- Engine type, Query/Result types, vector similarity search
    search_test.go    -- unit tests with mock store and embedder
  mcp/                -- MCP server for IDE integration
    server.go         -- server setup, stdio transport via mcp-go
    tools.go          -- tool definitions and handlers
    tools_test.go     -- unit tests with mock store and embedder
  provider/           -- source providers (pluggable)
    provider.go       -- Provider interface, SyncStats
    obsidian/         -- Obsidian vault provider
    github/           -- GitHub Issues/PRs provider (via gh CLI)
docs/
  decisions/          -- Architecture Decision Records
```

## Key conventions

- Config file: `~/.config/ctx/ctx.yaml` (overridable via `--config` flag or `CTX_HOME` env var)
- Default database: SQLite at `~/.config/ctx/ctx.db` (zero setup). Set `database.url` to a postgres:// URL to use Postgres instead.
- Environment overrides: `CTX_DATABASE_URL`, `CTX_HOME`, `OPENAI_API_KEY`
- Tests use `CTX_HOME` pointed at `t.TempDir()` to isolate state
- Version info injected via ldflags (`-X main.version=...`)

## Store layer

The `internal/store` package provides the persistence layer with a pluggable backend:

- `Store` interface defines all data operations (sources, documents, chunks, sync state)
- `StoreWithExtras` extends Store with CLI-specific methods (MigrationStatus, TableCounts, DropAll)
- `store.New(ctx, cfg)` factory selects the backend based on config (SQLite by default, Postgres if URL is set)
- **SQLiteStore** (default): pure Go via modernc.org/sqlite, stores embeddings as JSON text, brute-force cosine similarity search in Go. No external dependencies. Tests always run.
- **PostgresStore**: pgx/v5 connection pool, pgvector for vector search. Integration tests require a running Postgres; set `CTX_TEST_DATABASE_URL` or use the default.
- SQL migrations are embedded via `embed.FS` and applied automatically (separate migration sets for each backend)
- Model types are plain structs (Source, Document, Chunk, SyncState, IngestEntry)
- pgvector-go is used for vector embedding types in the Store interface
- Vector math helpers in `vector.go`: CosineSimilarity, ParseEmbedding, SerializeEmbedding

## Providers

Providers sync documents from external sources into the store. Each implements the `Provider` interface (`Sync(ctx, store) -> SyncStats`).

- **Obsidian** (`internal/provider/obsidian/`): walks a local vault directory, parses frontmatter, chunks markdown, detects changes via content hashing
- **GitHub** (`internal/provider/github/`): syncs issues and PRs from a GitHub repo via the `gh` CLI (no token config needed). Supports incremental sync via `updated_at` cursor. Issues/PRs become documents, comments become separate chunks. Labels map to tags, PARA category is always "projects".

GitHub source config example:
```yaml
sources:
  - name: ctx-issues
    type: github
    location: charemma/ctx
    metadata:
      include_prs: "true"
      include_comments: "true"
      state: all  # open, closed, all
```

## Search layer

The `internal/search` package provides semantic search over the knowledge store:

- `Engine` combines a `Store` and an `Embedder` to perform vector similarity search
- `Query` supports text search with optional PARA category and tag filters, configurable limit and minimum score
- The store's `SearchChunks` method runs a single SQL query that joins chunks with documents, applies cosine similarity scoring via pgvector's `<=>` operator, and filters by category/tags
- Unit tests use mock implementations of Store and Embedder -- no database required

## CLI subcommands

- `ctx init` -- interactive setup wizard (vault detection, embedding provider, database). Supports `--yes` for non-interactive mode.
- `ctx sync [source-name]` -- sync configured sources and embed unembedded chunks. Optional source name to sync a single source.
- `ctx status` -- show sources, sync state, document/chunk counts, and embedding stats
- `ctx search "query"` -- semantic search with `--category`, `--tags`, `--limit` filters
- `ctx serve --mcp` -- start MCP server on stdio (for Claude Code, Copilot, etc.)
- `ctx serve --web` -- placeholder for web server
- `ctx db migrate` -- run pending database migrations
- `ctx db reset` -- drop all tables and re-run migrations (interactive confirmation, or `--yes` to skip)
- `ctx db status` -- show applied/pending migrations and table row counts

## MCP server

The MCP server exposes the knowledge store to AI tools via the Model Context Protocol (stdio transport). It uses `github.com/mark3labs/mcp-go` as the SDK.

Available tools:
- `search_knowledge` -- semantic search with optional category, tags, limit, min_score filters
- `find_decisions` -- scoped search in decision records (filters to paths containing "decisions")
- `get_project_context` -- aggregates project notes, decisions, and journal entries
- `search_journal` -- journal search with optional date_from/date_to range
- `list_sources` -- shows configured sources and sync status

Configure in Claude Code (`.claude/mcp.json`):
```json
{
  "mcpServers": {
    "ctx": {
      "command": "ctx",
      "args": ["serve", "--mcp"]
    }
  }
}
```
