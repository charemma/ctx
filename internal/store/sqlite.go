package store

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pgvector/pgvector-go"
	_ "modernc.org/sqlite"
)

//go:embed migrations_sqlite/*.sql
var sqliteMigrationFS embed.FS

// SQLiteStore implements Store using modernc.org/sqlite (pure Go).
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore opens or creates a SQLite database at the given path.
func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("creating database directory: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(wal)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("opening sqlite database: %w", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("pinging sqlite database: %w", err)
	}

	return &SQLiteStore{db: db}, nil
}

func (s *SQLiteStore) Ping(_ context.Context) error {
	return s.db.Ping()
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// Migrate runs all pending SQLite migrations in order.
func (s *SQLiteStore) Migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TEXT NOT NULL
		)
	`)
	if err != nil {
		return fmt.Errorf("creating schema_migrations table: %w", err)
	}

	entries, err := fs.ReadDir(sqliteMigrationFS, "migrations_sqlite")
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
		err := s.db.QueryRowContext(ctx,
			"SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = ?)",
			version,
		).Scan(&exists)
		if err != nil {
			return fmt.Errorf("checking migration %s: %w", version, err)
		}
		if exists {
			continue
		}

		content, err := sqliteMigrationFS.ReadFile("migrations_sqlite/" + version)
		if err != nil {
			return fmt.Errorf("reading migration %s: %w", version, err)
		}

		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("beginning transaction for %s: %w", version, err)
		}

		if _, err := tx.ExecContext(ctx, string(content)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("executing migration %s: %w", version, err)
		}

		if _, err := tx.ExecContext(ctx,
			"INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)",
			version, time.Now().UTC().Format(time.RFC3339),
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("recording migration %s: %w", version, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration %s: %w", version, err)
		}
	}

	return nil
}

// MigrationStatus returns applied and pending migrations.
func (s *SQLiteStore) MigrationStatus(ctx context.Context) (applied []string, pending []string, err error) {
	entries, err := fs.ReadDir(sqliteMigrationFS, "migrations_sqlite")
	if err != nil {
		return nil, nil, fmt.Errorf("reading migration files: %w", err)
	}

	// Check if schema_migrations table exists.
	var tableName string
	err = s.db.QueryRowContext(ctx,
		"SELECT name FROM sqlite_master WHERE type='table' AND name='schema_migrations'",
	).Scan(&tableName)

	tableExists := err == nil
	if err != nil && err != sql.ErrNoRows {
		return nil, nil, fmt.Errorf("checking schema_migrations table: %w", err)
	}

	appliedSet := make(map[string]bool)
	if tableExists {
		rows, err := s.db.QueryContext(ctx, "SELECT version FROM schema_migrations ORDER BY version")
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
func (s *SQLiteStore) TableCounts(ctx context.Context) (map[string]int64, error) {
	tables := []string{"sources", "documents", "chunks", "sync_state", "ingest_queue"}
	counts := make(map[string]int64, len(tables))

	for _, table := range tables {
		var count int64
		err := s.db.QueryRowContext(ctx,
			fmt.Sprintf("SELECT COUNT(*) FROM %s", table),
		).Scan(&count)
		if err != nil {
			counts[table] = -1
			continue
		}
		counts[table] = count
	}

	return counts, nil
}

// DropAll drops all application tables.
func (s *SQLiteStore) DropAll(ctx context.Context) error {
	tables := []string{"ingest_queue", "sync_state", "chunks", "documents", "sources", "schema_migrations"}
	for _, table := range tables {
		if _, err := s.db.ExecContext(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", table)); err != nil {
			return fmt.Errorf("dropping table %s: %w", table, err)
		}
	}
	return nil
}

// --- Sources ---

func (s *SQLiteStore) CreateSource(ctx context.Context, src *Source) error {
	if src.ID == uuid.Nil {
		src.ID = uuid.New()
	}
	now := time.Now()
	src.CreatedAt = now
	src.UpdatedAt = now

	if src.Config == nil {
		src.Config = []byte("{}")
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sources (id, provider, name, location, config, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, src.ID.String(), src.Provider, src.Name, src.Location,
		string(src.Config), src.CreatedAt.UTC().Format(time.RFC3339Nano), src.UpdatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("creating source: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetSource(ctx context.Context, id uuid.UUID) (*Source, error) {
	var src Source
	var idStr, configStr, createdStr, updatedStr string
	err := s.db.QueryRowContext(ctx, `
		SELECT id, provider, name, location, config, created_at, updated_at
		FROM sources WHERE id = ?
	`, id.String()).Scan(&idStr, &src.Provider, &src.Name, &src.Location, &configStr, &createdStr, &updatedStr)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("getting source: %w", err)
	}

	src.ID = uuid.MustParse(idStr)
	src.Config = json.RawMessage(configStr)
	src.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdStr)
	src.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedStr)
	return &src, nil
}

func (s *SQLiteStore) GetSourceByName(ctx context.Context, name string) (*Source, error) {
	var src Source
	var idStr, configStr, createdStr, updatedStr string
	err := s.db.QueryRowContext(ctx, `
		SELECT id, provider, name, location, config, created_at, updated_at
		FROM sources WHERE name = ?
	`, name).Scan(&idStr, &src.Provider, &src.Name, &src.Location, &configStr, &createdStr, &updatedStr)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("getting source by name: %w", err)
	}

	src.ID = uuid.MustParse(idStr)
	src.Config = json.RawMessage(configStr)
	src.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdStr)
	src.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedStr)
	return &src, nil
}

func (s *SQLiteStore) ListSources(ctx context.Context) ([]Source, error) {
	rows, err := s.db.QueryContext(ctx, `
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
		var idStr, configStr, createdStr, updatedStr string
		if err := rows.Scan(&idStr, &src.Provider, &src.Name, &src.Location, &configStr, &createdStr, &updatedStr); err != nil {
			return nil, fmt.Errorf("scanning source: %w", err)
		}
		src.ID = uuid.MustParse(idStr)
		src.Config = json.RawMessage(configStr)
		src.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdStr)
		src.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedStr)
		sources = append(sources, src)
	}
	return sources, rows.Err()
}

func (s *SQLiteStore) DeleteSource(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM sources WHERE id = ?", id.String())
	if err != nil {
		return fmt.Errorf("deleting source: %w", err)
	}
	return nil
}

// --- Documents ---

func (s *SQLiteStore) UpsertDocument(ctx context.Context, d *Document) error {
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

	tagsJSON, err := json.Marshal(d.Tags)
	if err != nil {
		return fmt.Errorf("marshaling tags: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO documents (id, source_id, source_path, title, content_hash, content_type,
			para_category, tags, metadata, first_synced, last_synced)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (source_id, source_path) DO UPDATE SET
			title = excluded.title,
			content_hash = excluded.content_hash,
			content_type = excluded.content_type,
			para_category = excluded.para_category,
			tags = excluded.tags,
			metadata = excluded.metadata,
			last_synced = excluded.last_synced
	`, d.ID.String(), d.SourceID.String(), d.SourcePath, d.Title, d.ContentHash, d.ContentType,
		d.ParaCategory, string(tagsJSON), string(d.Metadata),
		d.FirstSynced.UTC().Format(time.RFC3339Nano), d.LastSynced.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("upserting document: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetDocumentByPath(ctx context.Context, sourceID uuid.UUID, path string) (*Document, error) {
	var d Document
	var idStr, sourceIDStr, tagsStr, metadataStr, firstStr, lastStr string
	err := s.db.QueryRowContext(ctx, `
		SELECT id, source_id, source_path, title, content_hash, content_type,
			para_category, tags, metadata, first_synced, last_synced
		FROM documents WHERE source_id = ? AND source_path = ?
	`, sourceID.String(), path).Scan(
		&idStr, &sourceIDStr, &d.SourcePath, &d.Title, &d.ContentHash, &d.ContentType,
		&d.ParaCategory, &tagsStr, &metadataStr, &firstStr, &lastStr,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("getting document by path: %w", err)
	}

	d.ID = uuid.MustParse(idStr)
	d.SourceID = uuid.MustParse(sourceIDStr)
	_ = json.Unmarshal([]byte(tagsStr), &d.Tags)
	d.Metadata = json.RawMessage(metadataStr)
	d.FirstSynced, _ = time.Parse(time.RFC3339Nano, firstStr)
	d.LastSynced, _ = time.Parse(time.RFC3339Nano, lastStr)
	return &d, nil
}

func (s *SQLiteStore) ListDocumentPaths(ctx context.Context, sourceID uuid.UUID) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT source_path FROM documents WHERE source_id = ? ORDER BY source_path",
		sourceID.String(),
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

func (s *SQLiteStore) DeleteDocument(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM documents WHERE id = ?", id.String())
	if err != nil {
		return fmt.Errorf("deleting document: %w", err)
	}
	return nil
}

func (s *SQLiteStore) DeleteDocumentsByPaths(ctx context.Context, sourceID uuid.UUID, paths []string) error {
	if len(paths) == 0 {
		return nil
	}

	placeholders := make([]string, len(paths))
	args := make([]any, 0, len(paths)+1)
	args = append(args, sourceID.String())
	for i, p := range paths {
		placeholders[i] = "?"
		args = append(args, p)
	}

	query := fmt.Sprintf(
		"DELETE FROM documents WHERE source_id = ? AND source_path IN (%s)",
		strings.Join(placeholders, ","),
	)

	_, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("deleting documents by paths: %w", err)
	}
	return nil
}

// --- Chunks ---

func (s *SQLiteStore) CreateChunks(ctx context.Context, chunks []Chunk) error {
	if len(chunks) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}

	now := time.Now()
	nowStr := now.UTC().Format(time.RFC3339Nano)

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO chunks (id, document_id, chunk_index, content, heading_path, embedding, token_count, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("preparing chunk insert: %w", err)
	}
	defer stmt.Close()

	for i := range chunks {
		c := &chunks[i]
		if c.ID == uuid.Nil {
			c.ID = uuid.New()
		}
		c.CreatedAt = now
		c.UpdatedAt = now

		var embeddingStr *string
		if c.Embedding != nil {
			s := SerializeEmbedding(c.Embedding.Slice())
			embeddingStr = &s
		}

		if _, err := stmt.ExecContext(ctx,
			c.ID.String(), c.DocumentID.String(), c.ChunkIndex, c.Content, c.HeadingPath,
			embeddingStr, c.TokenCount, nowStr, nowStr,
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("creating chunk: %w", err)
		}
	}

	return tx.Commit()
}

func (s *SQLiteStore) DeleteChunksByDocument(ctx context.Context, docID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM chunks WHERE document_id = ?", docID.String())
	if err != nil {
		return fmt.Errorf("deleting chunks by document: %w", err)
	}
	return nil
}

func (s *SQLiteStore) UpdateChunkEmbedding(ctx context.Context, chunkID uuid.UUID, embedding pgvector.Vector) error {
	embStr := SerializeEmbedding(embedding.Slice())
	_, err := s.db.ExecContext(ctx,
		"UPDATE chunks SET embedding = ?, updated_at = ? WHERE id = ?",
		embStr, time.Now().UTC().Format(time.RFC3339Nano), chunkID.String(),
	)
	if err != nil {
		return fmt.Errorf("updating chunk embedding: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetUnembeddedChunks(ctx context.Context, limit int) ([]Chunk, error) {
	query := `
		SELECT id, document_id, chunk_index, content, heading_path, token_count, created_at, updated_at
		FROM chunks WHERE embedding IS NULL
		ORDER BY created_at
	`
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("getting unembedded chunks: %w", err)
	}
	defer rows.Close()

	var chunks []Chunk
	for rows.Next() {
		var c Chunk
		var idStr, docIDStr, createdStr, updatedStr string
		if err := rows.Scan(&idStr, &docIDStr, &c.ChunkIndex, &c.Content, &c.HeadingPath,
			&c.TokenCount, &createdStr, &updatedStr); err != nil {
			return nil, fmt.Errorf("scanning chunk: %w", err)
		}
		c.ID = uuid.MustParse(idStr)
		c.DocumentID = uuid.MustParse(docIDStr)
		c.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdStr)
		c.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedStr)
		chunks = append(chunks, c)
	}
	return chunks, rows.Err()
}

// SearchChunks performs brute-force cosine similarity search over all embedded chunks.
func (s *SQLiteStore) SearchChunks(ctx context.Context, embedding pgvector.Vector, opts SearchOpts) ([]SearchResult, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 10
	}
	minScore := opts.MinScore
	if minScore <= 0 {
		minScore = 0.3
	}

	queryVec := embedding.Slice()

	// Load all embedded chunks with their document metadata.
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.id, c.content, c.heading_path, c.embedding,
		       d.id, d.source_path, d.para_category, d.tags, d.last_synced
		FROM chunks c
		JOIN documents d ON c.document_id = d.id
		WHERE c.embedding IS NOT NULL
	`)
	if err != nil {
		return nil, fmt.Errorf("searching chunks: %w", err)
	}
	defer rows.Close()

	type candidate struct {
		result SearchResult
		score  float64
	}
	var candidates []candidate

	for rows.Next() {
		var (
			chunkIDStr, docIDStr, embStr, lastSyncedStr string
			headingPath, category                       *string
			tagsStr                                     string
			content                                     string
		)
		var sourcePath string
		if err := rows.Scan(
			&chunkIDStr, &content, &headingPath, &embStr,
			&docIDStr, &sourcePath, &category, &tagsStr, &lastSyncedStr,
		); err != nil {
			return nil, fmt.Errorf("scanning search result: %w", err)
		}

		// Apply category filter.
		if opts.Category != "" {
			if category == nil || *category != opts.Category {
				continue
			}
		}

		// Apply tag filter.
		var tags []string
		_ = json.Unmarshal([]byte(tagsStr), &tags)
		if len(opts.Tags) > 0 {
			if !hasOverlap(tags, opts.Tags) {
				continue
			}
		}

		chunkVec, err := ParseEmbedding(embStr)
		if err != nil {
			continue
		}

		score := CosineSimilarity(queryVec, chunkVec)
		if score < minScore {
			continue
		}

		var r SearchResult
		r.ChunkID = uuid.MustParse(chunkIDStr)
		r.Content = content
		r.DocumentID = uuid.MustParse(docIDStr)
		r.SourcePath = sourcePath
		r.Score = score
		r.Tags = tags
		r.LastSynced, _ = time.Parse(time.RFC3339Nano, lastSyncedStr)

		if headingPath != nil {
			r.HeadingPath = *headingPath
		}
		if category != nil {
			r.Category = *category
		}

		candidates = append(candidates, candidate{result: r, score: score})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating search results: %w", err)
	}

	// Sort by score descending.
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	// Apply limit.
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}

	results := make([]SearchResult, len(candidates))
	for i, c := range candidates {
		results[i] = c.result
	}
	return results, nil
}

// --- Sync State ---

func (s *SQLiteStore) GetSyncState(ctx context.Context, sourceID uuid.UUID) (*SyncState, error) {
	var ss SyncState
	var sourceIDStr string
	var lastSyncStr, cursorStr *string
	err := s.db.QueryRowContext(ctx, `
		SELECT source_id, last_sync, docs_total, docs_synced, docs_skipped, cursor, error
		FROM sync_state WHERE source_id = ?
	`, sourceID.String()).Scan(&sourceIDStr, &lastSyncStr, &ss.DocsTotal, &ss.DocsSynced,
		&ss.DocsSkipped, &cursorStr, &ss.Error)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("getting sync state: %w", err)
	}

	ss.SourceID = uuid.MustParse(sourceIDStr)
	if lastSyncStr != nil {
		t, _ := time.Parse(time.RFC3339Nano, *lastSyncStr)
		ss.LastSync = &t
	}
	if cursorStr != nil {
		ss.Cursor = json.RawMessage(*cursorStr)
	}
	return &ss, nil
}

func (s *SQLiteStore) UpdateSyncState(ctx context.Context, ss *SyncState) error {
	if ss.Cursor == nil {
		ss.Cursor = []byte("{}")
	}

	var lastSyncStr *string
	if ss.LastSync != nil {
		s := ss.LastSync.UTC().Format(time.RFC3339Nano)
		lastSyncStr = &s
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sync_state (source_id, last_sync, docs_total, docs_synced, docs_skipped, cursor, error)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (source_id) DO UPDATE SET
			last_sync = excluded.last_sync,
			docs_total = excluded.docs_total,
			docs_synced = excluded.docs_synced,
			docs_skipped = excluded.docs_skipped,
			cursor = excluded.cursor,
			error = excluded.error
	`, ss.SourceID.String(), lastSyncStr, ss.DocsTotal, ss.DocsSynced, ss.DocsSkipped,
		string(ss.Cursor), ss.Error)
	if err != nil {
		return fmt.Errorf("updating sync state: %w", err)
	}
	return nil
}

// hasOverlap returns true if a and b share at least one element.
func hasOverlap(a, b []string) bool {
	set := make(map[string]bool, len(b))
	for _, s := range b {
		set[s] = true
	}
	for _, s := range a {
		if set[s] {
			return true
		}
	}
	return false
}
