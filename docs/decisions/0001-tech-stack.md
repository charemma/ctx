# ADR 0001: Tech Stack -- Go + Postgres + pgvector

## Status

Accepted

## Date

2026-04-18

## Context

ctx needs to serve multiple interfaces from a single codebase: a CLI for CI/CD integration, an MCP server for IDE integration, and a web UI for configuration and management. The knowledge store requires vector search capabilities for semantic retrieval (RAG).

Key requirements:
- Single binary distribution (CLI + MCP server must be easy to install)
- Vector similarity search for embeddings
- Web UI for configuration and template management
- Integration with enterprise sources (Confluence, Slack, GitHub, Jira)

## Decision

**Language: Go**

- Single binary compilation -- critical for CLI and MCP server distribution
- Proven patterns from ikno CLI (cobra, charmbracelet, config management)
- MCP SDK available (github.com/mark3labs/mcp-go)
- Web UI via templ + htmx (server-rendered, minimal JS)
- Strong concurrency model for sync engine and pipeline workers

**Database: Postgres + pgvector**

- pgvector provides vector similarity search without a separate vector database
- Relational model for metadata, source lineage, sync state
- Single database for both structured data and embeddings
- Mature ecosystem, easy to operate, well-understood scaling path

**Embedding: OpenAI text-embedding-3-small (default)**

- High quality embeddings at low cost
- Provider-agnostic design allows swapping to Ollama or other providers
- 1536-dimensional vectors, good balance of quality and storage

## Alternatives considered

**Python:** Richer AI/ML ecosystem (langchain, llama-index), but harder to distribute as a single binary. Deployment complexity increases with CLI + MCP server requirements.

**TypeScript:** Could unify web UI and backend, but weaker for CLI tooling and systems work. Less natural for MCP server and concurrent sync operations.

**Separate vector database (Qdrant, ChromaDB):** Additional operational complexity. pgvector is sufficient for the expected scale and avoids a second data store to manage.

## Consequences

- AI/ML ecosystem in Go is thinner -- mitigated by the fact that development happens with AI agents, and LLM interactions are HTTP API calls regardless of language
- Web UI uses templ + htmx instead of a JS framework -- keeps the single-binary advantage but limits client-side interactivity
- pgvector performance may need monitoring at scale -- can migrate to a dedicated vector DB later if needed
