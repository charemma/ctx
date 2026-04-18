package store

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// PostgresStore implements Store using pgx/v5 with a connection pool.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore creates a new PostgresStore connected to the given database URL.
func NewPostgresStore(ctx context.Context, databaseURL string) (*PostgresStore, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("connecting to database: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pinging database: %w", err)
	}

	return &PostgresStore{pool: pool}, nil
}

// Ping checks database connectivity.
func (s *PostgresStore) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

// Close releases the connection pool.
func (s *PostgresStore) Close() error {
	s.pool.Close()
	return nil
}

// Migrate runs all pending SQL migrations in order.
func (s *PostgresStore) Migrate(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`)
	if err != nil {
		return fmt.Errorf("creating schema_migrations table: %w", err)
	}

	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return fmt.Errorf("reading migration files: %w", err)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		version := entry.Name()

		var exists bool
		err := s.pool.QueryRow(ctx,
			"SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)",
			version,
		).Scan(&exists)
		if err != nil {
			return fmt.Errorf("checking migration %s: %w", version, err)
		}
		if exists {
			continue
		}

		content, err := migrationFS.ReadFile("migrations/" + version)
		if err != nil {
			return fmt.Errorf("reading migration %s: %w", version, err)
		}

		tx, err := s.pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("beginning transaction for %s: %w", version, err)
		}

		if _, err := tx.Exec(ctx, string(content)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("executing migration %s: %w", version, err)
		}

		if _, err := tx.Exec(ctx,
			"INSERT INTO schema_migrations (version) VALUES ($1)",
			version,
		); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("recording migration %s: %w", version, err)
		}

		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("committing migration %s: %w", version, err)
		}
	}

	return nil
}

// MigrationStatus returns info about applied and pending migrations.
func (s *PostgresStore) MigrationStatus(ctx context.Context) (applied []string, pending []string, err error) {
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return nil, nil, fmt.Errorf("reading migration files: %w", err)
	}

	// Check if schema_migrations table exists.
	var tableExists bool
	err = s.pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM information_schema.tables
			WHERE table_name = 'schema_migrations'
		)
	`).Scan(&tableExists)
	if err != nil {
		return nil, nil, fmt.Errorf("checking schema_migrations table: %w", err)
	}

	appliedSet := make(map[string]bool)
	if tableExists {
		rows, err := s.pool.Query(ctx, "SELECT version FROM schema_migrations ORDER BY version")
		if err != nil {
			return nil, nil, fmt.Errorf("querying schema_migrations: %w", err)
		}
		defer rows.Close()

		for rows.Next() {
			var v string
			if err := rows.Scan(&v); err != nil {
				return nil, nil, fmt.Errorf("scanning migration version: %w", err)
			}
			appliedSet[v] = true
			applied = append(applied, v)
		}
		if err := rows.Err(); err != nil {
			return nil, nil, fmt.Errorf("iterating migration rows: %w", err)
		}
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		if !appliedSet[entry.Name()] {
			pending = append(pending, entry.Name())
		}
	}

	sort.Strings(pending)
	return applied, pending, nil
}

// TableCounts returns row counts for the main tables.
func (s *PostgresStore) TableCounts(ctx context.Context) (map[string]int64, error) {
	tables := []string{"sources", "documents", "chunks", "sync_state", "ingest_queue"}
	counts := make(map[string]int64, len(tables))

	for _, table := range tables {
		var count int64
		// Table names are hardcoded constants, not user input.
		err := s.pool.QueryRow(ctx,
			fmt.Sprintf("SELECT COUNT(*) FROM %s", table),
		).Scan(&count)
		if err != nil {
			// Table may not exist yet if migrations haven't run.
			counts[table] = -1
			continue
		}
		counts[table] = count
	}

	return counts, nil
}

// DropAll drops all application tables. Used for db reset.
func (s *PostgresStore) DropAll(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `
		DROP TABLE IF EXISTS ingest_queue CASCADE;
		DROP TABLE IF EXISTS sync_state CASCADE;
		DROP TABLE IF EXISTS chunks CASCADE;
		DROP TABLE IF EXISTS documents CASCADE;
		DROP TABLE IF EXISTS sources CASCADE;
		DROP TABLE IF EXISTS schema_migrations CASCADE;
	`)
	if err != nil {
		return fmt.Errorf("dropping tables: %w", err)
	}
	return nil
}

// --- Sources ---

func (s *PostgresStore) CreateSource(ctx context.Context, src *Source) error {
	if src.ID == uuid.Nil {
		src.ID = uuid.New()
	}
	now := time.Now()
	src.CreatedAt = now
	src.UpdatedAt = now

	if src.Config == nil {
		src.Config = []byte("{}")
	}

	_, err := s.pool.Exec(ctx, `
		INSERT INTO sources (id, provider, name, location, config, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, src.ID, src.Provider, src.Name, src.Location, src.Config, src.CreatedAt, src.UpdatedAt)
	if err != nil {
		return fmt.Errorf("creating source: %w", err)
	}
	return nil
}

func (s *PostgresStore) GetSource(ctx context.Context, id uuid.UUID) (*Source, error) {
	var src Source
	err := s.pool.QueryRow(ctx, `
		SELECT id, provider, name, location, config, created_at, updated_at
		FROM sources WHERE id = $1
	`, id).Scan(&src.ID, &src.Provider, &src.Name, &src.Location, &src.Config, &src.CreatedAt, &src.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("getting source: %w", err)
	}
	return &src, nil
}

func (s *PostgresStore) GetSourceByName(ctx context.Context, name string) (*Source, error) {
	var src Source
	err := s.pool.QueryRow(ctx, `
		SELECT id, provider, name, location, config, created_at, updated_at
		FROM sources WHERE name = $1
	`, name).Scan(&src.ID, &src.Provider, &src.Name, &src.Location, &src.Config, &src.CreatedAt, &src.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("getting source by name: %w", err)
	}
	return &src, nil
}

func (s *PostgresStore) ListSources(ctx context.Context) ([]Source, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, provider, name, location, config, created_at, updated_at
		FROM sources ORDER BY name
	`)
	if err != nil {
		return nil, fmt.Errorf("listing sources: %w", err)
	}
	defer rows.Close()

	var sources []Source
	for rows.Next() {
		var src Source
		if err := rows.Scan(&src.ID, &src.Provider, &src.Name, &src.Location, &src.Config, &src.CreatedAt, &src.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning source: %w", err)
		}
		sources = append(sources, src)
	}
	return sources, rows.Err()
}

func (s *PostgresStore) DeleteSource(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, "DELETE FROM sources WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("deleting source: %w", err)
	}
	return nil
}

// --- Documents ---

func (s *PostgresStore) UpsertDocument(ctx context.Context, d *Document) error {
	if d.ID == uuid.Nil {
		d.ID = uuid.New()
	}
	now := time.Now()
	d.LastSynced = now
	if d.FirstSynced.IsZero() {
		d.FirstSynced = now
	}

	if d.Tags == nil {
		d.Tags = []string{}
	}
	if d.Metadata == nil {
		d.Metadata = []byte("{}")
	}
	if d.ContentType == "" {
		d.ContentType = "text/markdown"
	}

	_, err := s.pool.Exec(ctx, `
		INSERT INTO documents (id, source_id, source_path, title, content_hash, content_type,
			para_category, tags, metadata, first_synced, last_synced)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (source_id, source_path) DO UPDATE SET
			title = EXCLUDED.title,
			content_hash = EXCLUDED.content_hash,
			content_type = EXCLUDED.content_type,
			para_category = EXCLUDED.para_category,
			tags = EXCLUDED.tags,
			metadata = EXCLUDED.metadata,
			last_synced = EXCLUDED.last_synced
	`, d.ID, d.SourceID, d.SourcePath, d.Title, d.ContentHash, d.ContentType,
		d.ParaCategory, d.Tags, d.Metadata, d.FirstSynced, d.LastSynced)
	if err != nil {
		return fmt.Errorf("upserting document: %w", err)
	}
	return nil
}

func (s *PostgresStore) GetDocumentByPath(ctx context.Context, sourceID uuid.UUID, path string) (*Document, error) {
	var d Document
	err := s.pool.QueryRow(ctx, `
		SELECT id, source_id, source_path, title, content_hash, content_type,
			para_category, tags, metadata, first_synced, last_synced
		FROM documents WHERE source_id = $1 AND source_path = $2
	`, sourceID, path).Scan(
		&d.ID, &d.SourceID, &d.SourcePath, &d.Title, &d.ContentHash, &d.ContentType,
		&d.ParaCategory, &d.Tags, &d.Metadata, &d.FirstSynced, &d.LastSynced,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("getting document by path: %w", err)
	}
	return &d, nil
}

func (s *PostgresStore) ListDocumentPaths(ctx context.Context, sourceID uuid.UUID) ([]string, error) {
	rows, err := s.pool.Query(ctx,
		"SELECT source_path FROM documents WHERE source_id = $1 ORDER BY source_path",
		sourceID,
	)
	if err != nil {
		return nil, fmt.Errorf("listing document paths: %w", err)
	}
	defer rows.Close()

	var paths []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, fmt.Errorf("scanning document path: %w", err)
		}
		paths = append(paths, p)
	}
	return paths, rows.Err()
}

func (s *PostgresStore) DeleteDocument(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, "DELETE FROM documents WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("deleting document: %w", err)
	}
	return nil
}

func (s *PostgresStore) DeleteDocumentsByPaths(ctx context.Context, sourceID uuid.UUID, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	_, err := s.pool.Exec(ctx,
		"DELETE FROM documents WHERE source_id = $1 AND source_path = ANY($2)",
		sourceID, paths,
	)
	if err != nil {
		return fmt.Errorf("deleting documents by paths: %w", err)
	}
	return nil
}

// --- Chunks ---

func (s *PostgresStore) CreateChunks(ctx context.Context, chunks []Chunk) error {
	if len(chunks) == 0 {
		return nil
	}

	batch := &pgx.Batch{}
	now := time.Now()
	for i := range chunks {
		c := &chunks[i]
		if c.ID == uuid.Nil {
			c.ID = uuid.New()
		}
		c.CreatedAt = now
		c.UpdatedAt = now

		batch.Queue(`
			INSERT INTO chunks (id, document_id, chunk_index, content, heading_path, embedding, token_count, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		`, c.ID, c.DocumentID, c.ChunkIndex, c.Content, c.HeadingPath, c.Embedding, c.TokenCount, c.CreatedAt, c.UpdatedAt)
	}

	br := s.pool.SendBatch(ctx, batch)
	defer br.Close()

	for range chunks {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("creating chunk: %w", err)
		}
	}
	return nil
}

func (s *PostgresStore) DeleteChunksByDocument(ctx context.Context, docID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, "DELETE FROM chunks WHERE document_id = $1", docID)
	if err != nil {
		return fmt.Errorf("deleting chunks by document: %w", err)
	}
	return nil
}

func (s *PostgresStore) UpdateChunkEmbedding(ctx context.Context, chunkID uuid.UUID, embedding pgvector.Vector) error {
	_, err := s.pool.Exec(ctx,
		"UPDATE chunks SET embedding = $1, updated_at = $2 WHERE id = $3",
		embedding, time.Now(), chunkID,
	)
	if err != nil {
		return fmt.Errorf("updating chunk embedding: %w", err)
	}
	return nil
}

func (s *PostgresStore) GetUnembeddedChunks(ctx context.Context, limit int) ([]Chunk, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, document_id, chunk_index, content, heading_path, token_count, created_at, updated_at
		FROM chunks WHERE embedding IS NULL
		ORDER BY created_at
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("getting unembedded chunks: %w", err)
	}
	defer rows.Close()

	var chunks []Chunk
	for rows.Next() {
		var c Chunk
		if err := rows.Scan(&c.ID, &c.DocumentID, &c.ChunkIndex, &c.Content, &c.HeadingPath,
			&c.TokenCount, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning chunk: %w", err)
		}
		chunks = append(chunks, c)
	}
	return chunks, rows.Err()
}

// --- Sync State ---

func (s *PostgresStore) GetSyncState(ctx context.Context, sourceID uuid.UUID) (*SyncState, error) {
	var ss SyncState
	err := s.pool.QueryRow(ctx, `
		SELECT source_id, last_sync, docs_total, docs_synced, docs_skipped, cursor, error
		FROM sync_state WHERE source_id = $1
	`, sourceID).Scan(&ss.SourceID, &ss.LastSync, &ss.DocsTotal, &ss.DocsSynced,
		&ss.DocsSkipped, &ss.Cursor, &ss.Error)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("getting sync state: %w", err)
	}
	return &ss, nil
}

func (s *PostgresStore) UpdateSyncState(ctx context.Context, ss *SyncState) error {
	if ss.Cursor == nil {
		ss.Cursor = []byte("{}")
	}

	_, err := s.pool.Exec(ctx, `
		INSERT INTO sync_state (source_id, last_sync, docs_total, docs_synced, docs_skipped, cursor, error)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (source_id) DO UPDATE SET
			last_sync = EXCLUDED.last_sync,
			docs_total = EXCLUDED.docs_total,
			docs_synced = EXCLUDED.docs_synced,
			docs_skipped = EXCLUDED.docs_skipped,
			cursor = EXCLUDED.cursor,
			error = EXCLUDED.error
	`, ss.SourceID, ss.LastSync, ss.DocsTotal, ss.DocsSynced, ss.DocsSkipped, ss.Cursor, ss.Error)
	if err != nil {
		return fmt.Errorf("updating sync state: %w", err)
	}
	return nil
}
