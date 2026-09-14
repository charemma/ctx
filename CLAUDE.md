# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What ctx is

ctx is a self-hosted knowledge platform with a frontend-agnostic core. It connects scattered knowledge (Obsidian notes, GitHub issues, and later Confluence, Jira, Slack) into a structured, searchable knowledge store and delivers context-aware results to the tools people already use -- IDEs (via MCP server), CLI (for CI/CD), and a web UI.

The architecture separates one core (ingestion, embedding, storage, retrieval) from any number of thin clients. The core knows nothing about its clients: new sources are providers implementing one interface, new vector backends are stores implementing one interface, new clients attach to the same retrieval layer. Everything runs on your own hardware -- embeddings via local Ollama, data in your own SQLite/Postgres.

## Tech stack

- Go (single binary for CLI, MCP server, and web UI)
- SQLite (default, embedded via modernc.org/sqlite, pure Go) or Postgres + pgvector (optional, for teams/enterprise)
- Cobra (CLI framework)
- Charmbracelet (lipgloss, huh) for terminal UI
- Web UI: stdlib `html/template` + `net/http`, templates and static assets embedded via `embed.FS`
- Embeddings: Ollama (local, default model `nomic-embed-text`) or OpenAI

## Development commands

Build and run:
- `just build` -- build binary to `bin/ctx`
- `just dev` -- run via `go run`
- `just dev version` -- quick run with subcommand

Testing and checks:
- `just test` -- run all tests
- `just lint` -- run golangci-lint
- `just fmt` -- format code
- `go test ./internal/store/ -run TestSQLiteStore` -- run a single test

Database:
- `just db-up` -- start dev Postgres (docker compose; dev convenience only, not a deployment target)
- `just db-down` -- stop dev Postgres

## Project structure

```
main.go              -- entry point, version injection via ldflags
cmd/                  -- Cobra commands
  root.go             -- root command, global flags (--config)
  version.go          -- version subcommand
  init.go             -- init wizard (vault detection, config generation)
  sync.go             -- sync sources and embed chunks (batches of 10)
  status.go           -- show system state
  search.go           -- semantic search
  serve.go            -- MCP server launcher
  web.go              -- web UI server (the real web entry point)
  db.go               -- db subcommand group (migrate, reset, status)
internal/
  config/             -- configuration loading (~/.config/ctx/ctx.yaml)
  store/              -- knowledge store (pluggable backend)
    store.go          -- Store interface and model types
    factory.go        -- store.New() factory, StoreWithExtras interface
    sqlite.go         -- SQLiteStore (default, pure Go, brute-force vector search)
    postgres.go       -- PostgresStore (pgx/v5 pool, pgvector)
    vector.go         -- cosine similarity, embedding JSON serialization
    migrations/       -- embedded Postgres SQL migrations
    migrations_sqlite/ -- embedded SQLite SQL migrations
  embedder/           -- embedding providers (pluggable)
    embedder.go       -- Embedder interface, factory by config provider
    ollama.go         -- Ollama client (local, 600s timeout for slow hardware)
    openai.go         -- OpenAI client (default text-embedding-3-small)
  search/             -- semantic search engine (Engine, Query/Result types)
  mcp/                -- MCP server for IDE integration (mcp-go, stdio)
  provider/           -- source providers (pluggable)
    provider.go       -- Provider interface, SyncStats
    obsidian/         -- Obsidian vault provider
    github/           -- GitHub Issues/PRs provider (via gh CLI)
  web/                -- web UI server (html/template, embedded assets)
k8s/                  -- Kustomize manifests (ArgoCD GitOps deployment)
Dockerfile            -- static binary container image (CGO disabled)
docs/decisions/       -- Architecture Decision Records (ADR format)
```

## Key conventions

- Config file: `~/.config/ctx/ctx.yaml` (overridable via `--config` flag or `CTX_HOME` env var)
- Default database: SQLite at `~/.config/ctx/ctx.db` (zero setup). Set `database.url` to a postgres:// URL to use Postgres instead.
- Environment overrides: `CTX_DATABASE_URL`, `CTX_HOME`, `OLLAMA_HOST`, `OPENAI_API_KEY`
- Tests use `CTX_HOME` pointed at `t.TempDir()` to isolate state
- Version info injected via ldflags (`-X main.version=...`)
- Prior architecture decisions live in `docs/decisions/` -- check them before proposing changes to tech stack or storage

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

## Embedding layer

The `internal/embedder` package generates vector embeddings, selected via `embedding.provider` in the config:

- `Embedder` interface: `Embed(ctx, texts) -> [][]float32` plus `Dimensions()`
- **Ollama** (local, no API key): default model `nomic-embed-text` (768 dims), host via `OLLAMA_HOST` (default `http://localhost:11434`). Client timeout is deliberately long (600s) for slow hardware.
- **OpenAI**: default model `text-embedding-3-small`, key via `OPENAI_API_KEY`
- `ctx sync` embeds unembedded chunks in batches of 10 (kept small for slow local hardware)

```yaml
embedding:
  provider: ollama  # or openai
  model: nomic-embed-text
```

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
- `ctx web` -- start the web UI server (`--host`, `--port`, default localhost:8080)
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

## Web UI

The `internal/web` package serves a server-rendered UI (search page, sources page with sync trigger) plus a JSON endpoint at `GET /api/search`. Templates (`templates/*.html`) and static assets are embedded via `embed.FS`; `layout.html` is the shared shell each page template is parsed against.

## Deployment and CI

- `Dockerfile`: two-stage build, `CGO_ENABLED=0` static binary (both DB backends are pure Go), minimal alpine runtime with CA certs. `CTX_HOME=/config`, mount a `ctx.yaml` there.
- `.github/workflows/build.yml`: on push to main, builds and pushes the image to `ghcr.io/charemma/ctx` (tags `latest` + `sha-<short>`), then a second job commits the new SHA tag into `k8s/kustomization.yaml` via `kustomize edit set image`. The workflow ignores pushes touching only `k8s/**`, `docs/**`, and markdown -- that path filter is the loop-break for the tag-bump commit; keep it intact when editing the workflow.
- `k8s/`: Kustomize manifests (configmap with `ctx.yaml`, hourly `ctx sync` CronJob). Reconciled onto the k3s cluster by ArgoCD via the GitOps setup in `charemma/platform`. Assumes Postgres and Ollama run as cluster services (`postgres:5432`, `ollama:11434`) and a `ctx-db` secret holds the database URL.
