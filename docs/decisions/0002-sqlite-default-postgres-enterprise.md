# 0002: SQLite as default database, Postgres for enterprise

Date: 2026-04-18
Status: accepted

## Context

ctx originally required a running Postgres instance with pgvector for all
usage, even local single-user setups. This meant `ctx init` needed Docker or an
existing Postgres, adding friction for personal use and breaking the
single-binary promise.

## Decision

SQLite is the default database backend. Postgres remains available as an option
for team/enterprise deployments.

- SQLite via `modernc.org/sqlite` (pure Go, no CGo) keeps the single-binary
  clean.
- Embeddings are stored as JSON-encoded float32 arrays in TEXT columns.
- Vector search uses brute-force cosine similarity computed in Go. For the
  expected scale (<100k chunks in personal use), this is fast enough.
- A `store.New(ctx, cfg)` factory selects the backend based on config: if
  `database.url` starts with `postgres`, use Postgres; otherwise use SQLite.
- Both backends implement the same `Store` interface and `StoreWithExtras`
  extension.

## Consequences

- `ctx init` works with zero external dependencies (no Docker, no Postgres).
- SQLite tests always run in CI (no database setup needed).
- Postgres tests remain integration tests that skip when no database is
  available.
- For large-scale deployments (>100k chunks, concurrent multi-user access),
  Postgres with pgvector is the recommended backend.
- If SQLite vector search becomes a bottleneck, sqlite-vec can be added later
  without changing the Store interface.
