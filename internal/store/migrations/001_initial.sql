CREATE EXTENSION IF NOT EXISTS vector;
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

CREATE TABLE sources (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    provider    TEXT NOT NULL,
    name        TEXT NOT NULL,
    location    TEXT NOT NULL,
    config      JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE documents (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    source_id       UUID NOT NULL REFERENCES sources(id) ON DELETE CASCADE,
    source_path     TEXT NOT NULL,
    title           TEXT,
    content_hash    TEXT NOT NULL,
    content_type    TEXT NOT NULL DEFAULT 'text/markdown',
    para_category   TEXT,
    tags            TEXT[] DEFAULT '{}',
    metadata        JSONB NOT NULL DEFAULT '{}',
    first_synced    TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_synced     TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(source_id, source_path)
);

CREATE TABLE chunks (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    document_id     UUID NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    chunk_index     INTEGER NOT NULL,
    content         TEXT NOT NULL,
    heading_path    TEXT,
    embedding       vector(1536),
    token_count     INTEGER,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_chunks_embedding ON chunks
    USING ivfflat (embedding vector_cosine_ops) WITH (lists = 100);

CREATE INDEX idx_documents_para ON documents(para_category);
CREATE INDEX idx_documents_tags ON documents USING gin(tags);
CREATE INDEX idx_documents_source ON documents(source_id);
CREATE INDEX idx_chunks_document ON chunks(document_id);

CREATE TABLE sync_state (
    source_id       UUID PRIMARY KEY REFERENCES sources(id) ON DELETE CASCADE,
    last_sync       TIMESTAMPTZ,
    docs_total      INTEGER NOT NULL DEFAULT 0,
    docs_synced     INTEGER NOT NULL DEFAULT 0,
    docs_skipped    INTEGER NOT NULL DEFAULT 0,
    cursor          JSONB NOT NULL DEFAULT '{}',
    error           TEXT
);

CREATE TABLE ingest_queue (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    agent_name      TEXT NOT NULL,
    content         TEXT NOT NULL,
    content_type    TEXT NOT NULL DEFAULT 'text/markdown',
    source_ref      TEXT,
    metadata        JSONB NOT NULL DEFAULT '{}',
    status          TEXT NOT NULL DEFAULT 'pending',
    reviewed_by     TEXT,
    reviewed_at     TIMESTAMPTZ,
    review_note     TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_ingest_status ON ingest_queue(status);
