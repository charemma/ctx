package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/pgvector/pgvector-go"
)

// Store defines the interface for the knowledge store.
type Store interface {
	// Sources
	CreateSource(ctx context.Context, s *Source) error
	GetSource(ctx context.Context, id uuid.UUID) (*Source, error)
	GetSourceByName(ctx context.Context, name string) (*Source, error)
	ListSources(ctx context.Context) ([]Source, error)
	DeleteSource(ctx context.Context, id uuid.UUID) error

	// Documents
	UpsertDocument(ctx context.Context, d *Document) error
	GetDocumentByPath(ctx context.Context, sourceID uuid.UUID, path string) (*Document, error)
	ListDocumentPaths(ctx context.Context, sourceID uuid.UUID) ([]string, error)
	DeleteDocument(ctx context.Context, id uuid.UUID) error
	DeleteDocumentsByPaths(ctx context.Context, sourceID uuid.UUID, paths []string) error

	// Chunks
	CreateChunks(ctx context.Context, chunks []Chunk) error
	DeleteChunksByDocument(ctx context.Context, docID uuid.UUID) error
	UpdateChunkEmbedding(ctx context.Context, chunkID uuid.UUID, embedding pgvector.Vector) error
	GetUnembeddedChunks(ctx context.Context, limit int) ([]Chunk, error)

	// Sync State
	GetSyncState(ctx context.Context, sourceID uuid.UUID) (*SyncState, error)
	UpdateSyncState(ctx context.Context, s *SyncState) error

	// Migrations
	Migrate(ctx context.Context) error

	// Health
	Ping(ctx context.Context) error
	Close() error
}

// Source represents a configured knowledge provider instance.
type Source struct {
	ID        uuid.UUID       `json:"id"`
	Provider  string          `json:"provider"`
	Name      string          `json:"name"`
	Location  string          `json:"location"`
	Config    json.RawMessage `json:"config"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// Document represents a file or page from a source.
type Document struct {
	ID           uuid.UUID       `json:"id"`
	SourceID     uuid.UUID       `json:"source_id"`
	SourcePath   string          `json:"source_path"`
	Title        *string         `json:"title"`
	ContentHash  string          `json:"content_hash"`
	ContentType  string          `json:"content_type"`
	ParaCategory *string         `json:"para_category"`
	Tags         []string        `json:"tags"`
	Metadata     json.RawMessage `json:"metadata"`
	FirstSynced  time.Time       `json:"first_synced"`
	LastSynced   time.Time       `json:"last_synced"`
}

// Chunk represents an embedded text segment of a document.
type Chunk struct {
	ID          uuid.UUID        `json:"id"`
	DocumentID  uuid.UUID        `json:"document_id"`
	ChunkIndex  int              `json:"chunk_index"`
	Content     string           `json:"content"`
	HeadingPath *string          `json:"heading_path"`
	Embedding   *pgvector.Vector `json:"embedding,omitempty"`
	TokenCount  *int             `json:"token_count"`
	CreatedAt   time.Time        `json:"created_at"`
	UpdatedAt   time.Time        `json:"updated_at"`
}

// SyncState tracks per-source sync progress and cursors.
type SyncState struct {
	SourceID    uuid.UUID       `json:"source_id"`
	LastSync    *time.Time      `json:"last_sync"`
	DocsTotal   int             `json:"docs_total"`
	DocsSynced  int             `json:"docs_synced"`
	DocsSkipped int             `json:"docs_skipped"`
	Cursor      json.RawMessage `json:"cursor"`
	Error       *string         `json:"error"`
}

// IngestEntry represents a pending write-back from an agent.
type IngestEntry struct {
	ID          uuid.UUID       `json:"id"`
	AgentName   string          `json:"agent_name"`
	Content     string          `json:"content"`
	ContentType string          `json:"content_type"`
	SourceRef   *string         `json:"source_ref"`
	Metadata    json.RawMessage `json:"metadata"`
	Status      string          `json:"status"`
	ReviewedBy  *string         `json:"reviewed_by"`
	ReviewedAt  *time.Time      `json:"reviewed_at"`
	ReviewNote  *string         `json:"review_note"`
	CreatedAt   time.Time       `json:"created_at"`
}
