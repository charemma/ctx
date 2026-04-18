CREATE TABLE IF NOT EXISTS sources (
    id          TEXT PRIMARY KEY,
    provider    TEXT NOT NULL,
    name        TEXT NOT NULL,
    location    TEXT NOT NULL,
    config      TEXT NOT NULL DEFAULT '{}',
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS documents (
    id              TEXT PRIMARY KEY,
    source_id       TEXT NOT NULL REFERENCES sources(id) ON DELETE CASCADE,
    source_path     TEXT NOT NULL,
    title           TEXT,
    content_hash    TEXT NOT NULL,
    content_type    TEXT NOT NULL DEFAULT 'text/markdown',
    para_category   TEXT,
    tags            TEXT NOT NULL DEFAULT '[]',
    metadata        TEXT NOT NULL DEFAULT '{}',
    first_synced    TEXT NOT NULL,
    last_synced     TEXT NOT NULL,
    UNIQUE(source_id, source_path)
);

CREATE TABLE IF NOT EXISTS chunks (
    id              TEXT PRIMARY KEY,
    document_id     TEXT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    chunk_index     INTEGER NOT NULL,
    content         TEXT NOT NULL,
    heading_path    TEXT,
    embedding       TEXT,
    token_count     INTEGER,
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_documents_para ON documents(para_category);
CREATE INDEX IF NOT EXISTS idx_documents_source ON documents(source_id);
CREATE INDEX IF NOT EXISTS idx_chunks_document ON chunks(document_id);
CREATE INDEX IF NOT EXISTS idx_chunks_embedding_not_null ON chunks(id) WHERE embedding IS NOT NULL;

CREATE TABLE IF NOT EXISTS sync_state (
    source_id       TEXT PRIMARY KEY REFERENCES sources(id) ON DELETE CASCADE,
    last_sync       TEXT,
    docs_total      INTEGER NOT NULL DEFAULT 0,
    docs_synced     INTEGER NOT NULL DEFAULT 0,
    docs_skipped    INTEGER NOT NULL DEFAULT 0,
    cursor          TEXT NOT NULL DEFAULT '{}',
    error           TEXT
);

CREATE TABLE IF NOT EXISTS ingest_queue (
    id              TEXT PRIMARY KEY,
    agent_name      TEXT NOT NULL,
    content         TEXT NOT NULL,
    content_type    TEXT NOT NULL DEFAULT 'text/markdown',
    source_ref      TEXT,
    metadata        TEXT NOT NULL DEFAULT '{}',
    status          TEXT NOT NULL DEFAULT 'pending',
    reviewed_by     TEXT,
    reviewed_at     TEXT,
    review_note     TEXT,
    created_at      TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_ingest_status ON ingest_queue(status);
