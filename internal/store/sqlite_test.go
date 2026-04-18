package store

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pgvector/pgvector-go"
)

func setupSQLiteStore(t *testing.T) *SQLiteStore {
	t.Helper()
	ctx := context.Background()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("creating sqlite store: %v", err)
	}

	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("running migrations: %v", err)
	}

	t.Cleanup(func() {
		s.Close()
	})

	return s
}

func TestSQLiteMigrate(t *testing.T) {
	s := setupSQLiteStore(t)
	ctx := context.Background()

	// Running migrate again should be idempotent.
	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("second migrate failed: %v", err)
	}

	applied, pending, err := s.MigrationStatus(ctx)
	if err != nil {
		t.Fatalf("migration status: %v", err)
	}
	if len(applied) == 0 {
		t.Error("expected at least one applied migration")
	}
	if len(pending) != 0 {
		t.Errorf("expected no pending migrations, got %v", pending)
	}
}

func TestSQLitePing(t *testing.T) {
	s := setupSQLiteStore(t)
	if err := s.Ping(context.Background()); err != nil {
		t.Fatalf("ping failed: %v", err)
	}
}

func TestSQLiteSourceCRUD(t *testing.T) {
	s := setupSQLiteStore(t)
	ctx := context.Background()

	src := &Source{
		Provider: "filesystem",
		Name:     "test-vault",
		Location: "/tmp/vault",
		Config:   json.RawMessage(`{"recursive": true}`),
	}

	if err := s.CreateSource(ctx, src); err != nil {
		t.Fatalf("creating source: %v", err)
	}
	if src.ID == uuid.Nil {
		t.Error("expected source ID to be set")
	}

	got, err := s.GetSource(ctx, src.ID)
	if err != nil {
		t.Fatalf("getting source: %v", err)
	}
	if got == nil {
		t.Fatal("expected source, got nil")
	}
	if got.Name != "test-vault" {
		t.Errorf("expected name test-vault, got %s", got.Name)
	}

	gotByName, err := s.GetSourceByName(ctx, "test-vault")
	if err != nil {
		t.Fatalf("getting source by name: %v", err)
	}
	if gotByName == nil || gotByName.ID != src.ID {
		t.Error("GetSourceByName returned wrong source")
	}

	sources, err := s.ListSources(ctx)
	if err != nil {
		t.Fatalf("listing sources: %v", err)
	}
	if len(sources) != 1 {
		t.Errorf("expected 1 source, got %d", len(sources))
	}

	if err := s.DeleteSource(ctx, src.ID); err != nil {
		t.Fatalf("deleting source: %v", err)
	}

	deleted, err := s.GetSource(ctx, src.ID)
	if err != nil {
		t.Fatalf("getting deleted source: %v", err)
	}
	if deleted != nil {
		t.Error("expected nil after delete")
	}
}

func TestSQLiteDocumentUpsertAndQuery(t *testing.T) {
	s := setupSQLiteStore(t)
	ctx := context.Background()

	src := &Source{Provider: "filesystem", Name: "docs-test", Location: "/tmp/docs"}
	if err := s.CreateSource(ctx, src); err != nil {
		t.Fatalf("creating source: %v", err)
	}

	title := "Test Document"
	category := "3 Resources"
	doc := &Document{
		SourceID:     src.ID,
		SourcePath:   "notes/test.md",
		Title:        &title,
		ContentHash:  "abc123",
		ContentType:  "text/markdown",
		ParaCategory: &category,
		Tags:         []string{"go", "testing"},
	}

	if err := s.UpsertDocument(ctx, doc); err != nil {
		t.Fatalf("upserting document: %v", err)
	}

	got, err := s.GetDocumentByPath(ctx, src.ID, "notes/test.md")
	if err != nil {
		t.Fatalf("getting document: %v", err)
	}
	if got == nil {
		t.Fatal("expected document, got nil")
	}
	if *got.Title != "Test Document" {
		t.Errorf("expected title 'Test Document', got %q", *got.Title)
	}
	if got.ContentHash != "abc123" {
		t.Errorf("expected hash abc123, got %s", got.ContentHash)
	}

	// Upsert with new hash should update.
	doc.ContentHash = "def456"
	if err := s.UpsertDocument(ctx, doc); err != nil {
		t.Fatalf("upserting document (update): %v", err)
	}

	updated, err := s.GetDocumentByPath(ctx, src.ID, "notes/test.md")
	if err != nil {
		t.Fatalf("getting updated document: %v", err)
	}
	if updated.ContentHash != "def456" {
		t.Errorf("expected updated hash def456, got %s", updated.ContentHash)
	}

	paths, err := s.ListDocumentPaths(ctx, src.ID)
	if err != nil {
		t.Fatalf("listing paths: %v", err)
	}
	if len(paths) != 1 || paths[0] != "notes/test.md" {
		t.Errorf("unexpected paths: %v", paths)
	}
}

func TestSQLiteChunksCRUD(t *testing.T) {
	s := setupSQLiteStore(t)
	ctx := context.Background()

	src := &Source{Provider: "filesystem", Name: "chunks-test", Location: "/tmp/chunks"}
	if err := s.CreateSource(ctx, src); err != nil {
		t.Fatalf("creating source: %v", err)
	}

	doc := &Document{
		SourceID:    src.ID,
		SourcePath:  "chunked.md",
		ContentHash: "hash1",
	}
	if err := s.UpsertDocument(ctx, doc); err != nil {
		t.Fatalf("upserting document: %v", err)
	}

	got, err := s.GetDocumentByPath(ctx, src.ID, "chunked.md")
	if err != nil || got == nil {
		t.Fatalf("getting document: %v", err)
	}

	heading := "## Introduction"
	tokenCount := 42
	chunks := []Chunk{
		{DocumentID: got.ID, ChunkIndex: 0, Content: "First chunk content", HeadingPath: &heading, TokenCount: &tokenCount},
		{DocumentID: got.ID, ChunkIndex: 1, Content: "Second chunk content"},
	}

	if err := s.CreateChunks(ctx, chunks); err != nil {
		t.Fatalf("creating chunks: %v", err)
	}

	unembedded, err := s.GetUnembeddedChunks(ctx, 10)
	if err != nil {
		t.Fatalf("getting unembedded chunks: %v", err)
	}
	if len(unembedded) != 2 {
		t.Fatalf("expected 2 unembedded chunks, got %d", len(unembedded))
	}

	// Update embedding on the first chunk.
	vec := make([]float32, 1536)
	vec[0] = 0.1
	vec[1] = 0.2
	embedding := pgvector.NewVector(vec)

	if err := s.UpdateChunkEmbedding(ctx, unembedded[0].ID, embedding); err != nil {
		t.Fatalf("updating embedding: %v", err)
	}

	stillUnembedded, err := s.GetUnembeddedChunks(ctx, 10)
	if err != nil {
		t.Fatalf("getting unembedded chunks after update: %v", err)
	}
	if len(stillUnembedded) != 1 {
		t.Errorf("expected 1 unembedded chunk, got %d", len(stillUnembedded))
	}

	// Delete chunks by document.
	if err := s.DeleteChunksByDocument(ctx, got.ID); err != nil {
		t.Fatalf("deleting chunks: %v", err)
	}

	afterDelete, err := s.GetUnembeddedChunks(ctx, 10)
	if err != nil {
		t.Fatalf("getting chunks after delete: %v", err)
	}
	if len(afterDelete) != 0 {
		t.Errorf("expected 0 chunks after delete, got %d", len(afterDelete))
	}
}

func TestSQLiteDeleteDocumentCascade(t *testing.T) {
	s := setupSQLiteStore(t)
	ctx := context.Background()

	src := &Source{Provider: "filesystem", Name: "cascade-test", Location: "/tmp/cascade"}
	if err := s.CreateSource(ctx, src); err != nil {
		t.Fatalf("creating source: %v", err)
	}

	doc := &Document{SourceID: src.ID, SourcePath: "cascade.md", ContentHash: "h1"}
	if err := s.UpsertDocument(ctx, doc); err != nil {
		t.Fatalf("upserting document: %v", err)
	}

	got, _ := s.GetDocumentByPath(ctx, src.ID, "cascade.md")
	chunks := []Chunk{
		{DocumentID: got.ID, ChunkIndex: 0, Content: "Will be cascaded"},
	}
	if err := s.CreateChunks(ctx, chunks); err != nil {
		t.Fatalf("creating chunks: %v", err)
	}

	// Deleting the document should cascade to chunks.
	if err := s.DeleteDocument(ctx, got.ID); err != nil {
		t.Fatalf("deleting document: %v", err)
	}

	remaining, err := s.GetUnembeddedChunks(ctx, 10)
	if err != nil {
		t.Fatalf("getting chunks after cascade: %v", err)
	}
	if len(remaining) != 0 {
		t.Errorf("expected chunks to be cascaded, got %d", len(remaining))
	}
}

func TestSQLiteSyncState(t *testing.T) {
	s := setupSQLiteStore(t)
	ctx := context.Background()

	src := &Source{Provider: "filesystem", Name: "sync-test", Location: "/tmp/sync"}
	if err := s.CreateSource(ctx, src); err != nil {
		t.Fatalf("creating source: %v", err)
	}

	// Initially no sync state.
	ss, err := s.GetSyncState(ctx, src.ID)
	if err != nil {
		t.Fatalf("getting sync state: %v", err)
	}
	if ss != nil {
		t.Error("expected nil sync state initially")
	}

	now := time.Now().Truncate(time.Second)
	syncErr := "timeout"
	state := &SyncState{
		SourceID:    src.ID,
		LastSync:    &now,
		DocsTotal:   100,
		DocsSynced:  95,
		DocsSkipped: 5,
		Cursor:      json.RawMessage(`{"page": 3}`),
		Error:       &syncErr,
	}

	if err := s.UpdateSyncState(ctx, state); err != nil {
		t.Fatalf("updating sync state: %v", err)
	}

	got, err := s.GetSyncState(ctx, src.ID)
	if err != nil {
		t.Fatalf("getting sync state after update: %v", err)
	}
	if got == nil {
		t.Fatal("expected sync state, got nil")
	}
	if got.DocsTotal != 100 {
		t.Errorf("expected 100 docs total, got %d", got.DocsTotal)
	}
	if *got.Error != "timeout" {
		t.Errorf("expected error 'timeout', got %q", *got.Error)
	}
}

func TestSQLiteDeleteDocumentsByPaths(t *testing.T) {
	s := setupSQLiteStore(t)
	ctx := context.Background()

	src := &Source{Provider: "filesystem", Name: "paths-test", Location: "/tmp/paths"}
	if err := s.CreateSource(ctx, src); err != nil {
		t.Fatalf("creating source: %v", err)
	}

	for _, path := range []string{"a.md", "b.md", "c.md"} {
		doc := &Document{SourceID: src.ID, SourcePath: path, ContentHash: "h"}
		if err := s.UpsertDocument(ctx, doc); err != nil {
			t.Fatalf("upserting %s: %v", path, err)
		}
	}

	if err := s.DeleteDocumentsByPaths(ctx, src.ID, []string{"a.md", "c.md"}); err != nil {
		t.Fatalf("deleting by paths: %v", err)
	}

	paths, err := s.ListDocumentPaths(ctx, src.ID)
	if err != nil {
		t.Fatalf("listing paths: %v", err)
	}
	if len(paths) != 1 || paths[0] != "b.md" {
		t.Errorf("expected only b.md remaining, got %v", paths)
	}
}

func TestSQLiteSearchChunks(t *testing.T) {
	s := setupSQLiteStore(t)
	ctx := context.Background()

	src := &Source{Provider: "filesystem", Name: "search-test", Location: "/tmp/search"}
	if err := s.CreateSource(ctx, src); err != nil {
		t.Fatalf("creating source: %v", err)
	}

	category := "3 Resources"
	doc := &Document{
		SourceID:     src.ID,
		SourcePath:   "notes/go.md",
		ContentHash:  "h1",
		ParaCategory: &category,
		Tags:         []string{"go", "programming"},
	}
	if err := s.UpsertDocument(ctx, doc); err != nil {
		t.Fatalf("upserting document: %v", err)
	}
	got, _ := s.GetDocumentByPath(ctx, src.ID, "notes/go.md")

	heading := "## Introduction"
	chunks := []Chunk{
		{DocumentID: got.ID, ChunkIndex: 0, Content: "Go is a statically typed language", HeadingPath: &heading},
		{DocumentID: got.ID, ChunkIndex: 1, Content: "Go was designed at Google"},
	}
	if err := s.CreateChunks(ctx, chunks); err != nil {
		t.Fatalf("creating chunks: %v", err)
	}

	unembedded, err := s.GetUnembeddedChunks(ctx, 10)
	if err != nil {
		t.Fatalf("getting unembedded: %v", err)
	}

	// Give both chunks similar embeddings.
	dims := 1536
	for _, c := range unembedded {
		vec := make([]float32, dims)
		vec[0] = 0.5
		vec[1] = 0.3
		if err := s.UpdateChunkEmbedding(ctx, c.ID, pgvector.NewVector(vec)); err != nil {
			t.Fatalf("updating embedding: %v", err)
		}
	}

	// Search with a similar vector.
	queryVec := make([]float32, dims)
	queryVec[0] = 0.5
	queryVec[1] = 0.3
	queryEmbedding := pgvector.NewVector(queryVec)

	t.Run("no filters", func(t *testing.T) {
		results, err := s.SearchChunks(ctx, queryEmbedding, SearchOpts{})
		if err != nil {
			t.Fatalf("searching: %v", err)
		}
		if len(results) != 2 {
			t.Fatalf("expected 2 results, got %d", len(results))
		}
		for _, r := range results {
			if r.Score < 0.3 {
				t.Errorf("score %f below default min", r.Score)
			}
			if r.SourcePath != "notes/go.md" {
				t.Errorf("unexpected source path: %s", r.SourcePath)
			}
			if r.Category != "3 Resources" {
				t.Errorf("unexpected category: %s", r.Category)
			}
		}
	})

	t.Run("category filter", func(t *testing.T) {
		results, err := s.SearchChunks(ctx, queryEmbedding, SearchOpts{Category: "1 Projects"})
		if err != nil {
			t.Fatalf("searching: %v", err)
		}
		if len(results) != 0 {
			t.Errorf("expected 0 results for non-matching category, got %d", len(results))
		}
	})

	t.Run("tag filter", func(t *testing.T) {
		results, err := s.SearchChunks(ctx, queryEmbedding, SearchOpts{Tags: []string{"go"}})
		if err != nil {
			t.Fatalf("searching: %v", err)
		}
		if len(results) != 2 {
			t.Errorf("expected 2 results for matching tag, got %d", len(results))
		}

		results, err = s.SearchChunks(ctx, queryEmbedding, SearchOpts{Tags: []string{"nonexistent"}})
		if err != nil {
			t.Fatalf("searching: %v", err)
		}
		if len(results) != 0 {
			t.Errorf("expected 0 results for non-matching tag, got %d", len(results))
		}
	})

	t.Run("limit", func(t *testing.T) {
		results, err := s.SearchChunks(ctx, queryEmbedding, SearchOpts{Limit: 1})
		if err != nil {
			t.Fatalf("searching: %v", err)
		}
		if len(results) != 1 {
			t.Errorf("expected 1 result with limit 1, got %d", len(results))
		}
	})

	t.Run("high min score", func(t *testing.T) {
		results, err := s.SearchChunks(ctx, queryEmbedding, SearchOpts{MinScore: 0.9999})
		if err != nil {
			t.Fatalf("searching: %v", err)
		}
		// With identical vectors the score should be 1.0.
		for _, r := range results {
			if r.Score < 0.9999 {
				t.Errorf("score %f should be >= 0.9999", r.Score)
			}
		}
	})
}

func TestSQLiteSearchChunksRanking(t *testing.T) {
	s := setupSQLiteStore(t)
	ctx := context.Background()

	src := &Source{Provider: "filesystem", Name: "rank-test", Location: "/tmp/rank"}
	if err := s.CreateSource(ctx, src); err != nil {
		t.Fatalf("creating source: %v", err)
	}

	doc := &Document{SourceID: src.ID, SourcePath: "rank.md", ContentHash: "h1"}
	if err := s.UpsertDocument(ctx, doc); err != nil {
		t.Fatalf("upserting document: %v", err)
	}
	got, _ := s.GetDocumentByPath(ctx, src.ID, "rank.md")

	chunks := []Chunk{
		{DocumentID: got.ID, ChunkIndex: 0, Content: "close match"},
		{DocumentID: got.ID, ChunkIndex: 1, Content: "far match"},
	}
	if err := s.CreateChunks(ctx, chunks); err != nil {
		t.Fatalf("creating chunks: %v", err)
	}

	unembedded, err := s.GetUnembeddedChunks(ctx, 10)
	if err != nil {
		t.Fatalf("getting unembedded: %v", err)
	}

	// Give chunk 0 a vector close to the query, chunk 1 a different one.
	closeVec := make([]float32, 8)
	closeVec[0] = 1.0
	closeVec[1] = 0.0
	if err := s.UpdateChunkEmbedding(ctx, unembedded[0].ID, pgvector.NewVector(closeVec)); err != nil {
		t.Fatalf("updating embedding: %v", err)
	}

	farVec := make([]float32, 8)
	farVec[0] = 0.0
	farVec[1] = 1.0
	if err := s.UpdateChunkEmbedding(ctx, unembedded[1].ID, pgvector.NewVector(farVec)); err != nil {
		t.Fatalf("updating embedding: %v", err)
	}

	queryVec := make([]float32, 8)
	queryVec[0] = 1.0
	queryVec[1] = 0.1
	queryEmbedding := pgvector.NewVector(queryVec)

	results, err := s.SearchChunks(ctx, queryEmbedding, SearchOpts{MinScore: 0.01})
	if err != nil {
		t.Fatalf("searching: %v", err)
	}
	if len(results) < 2 {
		t.Fatalf("expected at least 2 results, got %d", len(results))
	}

	// First result should be "close match" (higher cosine similarity).
	if results[0].Content != "close match" {
		t.Errorf("expected 'close match' first, got %q", results[0].Content)
	}
	if results[0].Score <= results[1].Score {
		t.Errorf("expected first result score %f > second %f", results[0].Score, results[1].Score)
	}
}

func TestSQLiteTableCounts(t *testing.T) {
	s := setupSQLiteStore(t)
	ctx := context.Background()

	counts, err := s.TableCounts(ctx)
	if err != nil {
		t.Fatalf("getting table counts: %v", err)
	}

	for _, table := range []string{"sources", "documents", "chunks", "sync_state", "ingest_queue"} {
		count, ok := counts[table]
		if !ok {
			t.Errorf("missing count for table %s", table)
		}
		if count != 0 {
			t.Errorf("expected 0 rows in %s, got %d", table, count)
		}
	}
}

func TestSQLiteDropAll(t *testing.T) {
	s := setupSQLiteStore(t)
	ctx := context.Background()

	// Create some data.
	src := &Source{Provider: "fs", Name: "drop-test", Location: "/tmp"}
	if err := s.CreateSource(ctx, src); err != nil {
		t.Fatalf("creating source: %v", err)
	}

	if err := s.DropAll(ctx); err != nil {
		t.Fatalf("drop all: %v", err)
	}

	// Re-migrate should work.
	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("migrate after drop: %v", err)
	}

	sources, err := s.ListSources(ctx)
	if err != nil {
		t.Fatalf("listing sources after reset: %v", err)
	}
	if len(sources) != 0 {
		t.Errorf("expected 0 sources after reset, got %d", len(sources))
	}
}
