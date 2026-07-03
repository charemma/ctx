# ctx

A self-hosted knowledge platform with a frontend-agnostic core. It connects
scattered knowledge (notes, issues, docs) into a structured, searchable store
and delivers it to the tools people already use through a single retrieval
core with multiple clients.

Everything runs on your own hardware. Embeddings are computed locally with
Ollama, vectors and documents live in your own Postgres, and nothing leaves
your infrastructure.

## The idea

Most knowledge tools glue a single UI to a single backend. ctx separates the
two: one core that owns ingestion, embedding, storage, and retrieval, and any
number of thin clients that attach to it.

```
          sources                      core                       clients
   ------------------------   ------------------------    ------------------------
    Obsidian vault  ------>                                  MCP server  (IDE, chat)
    GitHub issues   ------>     providers -> store            CLI        (CI/CD, ops)
    (pluggable)     ------>     embedder  -> search  ----->   web UI     (later)
```

The core knows nothing about its clients. A new client (a web UI, another
agent) attaches to the same retrieval layer without touching ingestion or
storage. A new source (Confluence, Jira, Slack) is a provider that implements
one interface. A new vector backend is a store that implements one interface.
Extension happens at the edges, the core stays put.

## What is built today

- Single Go binary: CLI and MCP server in one artifact
- Pluggable store: SQLite by default (pure Go, zero setup) or Postgres +
  pgvector for teams
- Pluggable embedder: Ollama (local) or OpenAI
- Providers: Obsidian vault (PARA-aware markdown chunking) and GitHub
  issues/PRs
- Semantic search with category and tag filters, configurable limit and
  minimum score
- MCP server exposing five tools (`search_knowledge`, `find_decisions`,
  `get_project_context`, `search_journal`, `list_sources`)
- Embedded SQL migrations, applied automatically per backend
- Test coverage across store, search, embedder, provider, and MCP layers

## Running locally

```sh
just build            # build binary to bin/ctx
just test             # run all tests
just db-up            # start dev Postgres + pgvector (docker compose)
just dev search "..." # run via go run
```

Configuration lives at `~/.config/ctx/ctx.yaml` (override with `--config` or
`CTX_HOME`). Environment overrides: `CTX_DATABASE_URL`, `OLLAMA_HOST`,
`OPENAI_API_KEY`. For local Ollama embeddings, run `ollama serve` and
`ollama pull nomic-embed-text`, then set `embedding.provider: ollama` in the
config.

The bundled `docker-compose.yaml` is a dev convenience for Postgres, not a
deployment target.

## MCP integration

ctx speaks the Model Context Protocol so IDEs and agents can query the store
directly, over stdio transport:

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

## Deployment

CI builds a container image (see `Dockerfile`) and pushes it to `ghcr.io` on
every push to `main`. The `k8s/` manifests are reconciled onto a k3s cluster
by ArgoCD, following the GitOps setup in
[charemma/platform](https://github.com/charemma/platform). Ingest runs as a
scheduled `ctx sync` job against Postgres and Ollama running as cluster
services.

## Roadmap

The architecture is built to grow into a full retrieval service. Planned next
steps include:

* An HTTP retrieval API so clients reach the core over the network
* Answer synthesis on top of retrieval using local generation models via Ollama
* A bundled web UI as the first client beyond MCP and the CLI
* Per-namespace isolation for separate teams or projects
* More providers: Confluence, Jira, Slack

## Design decisions

Architecture Decision Records live in [`docs/decisions`](docs/decisions).

## License

See [LICENSE](LICENSE).
