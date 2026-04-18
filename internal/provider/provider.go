package provider

import (
	"context"

	"github.com/charemma/ctx/internal/store"
)

// Provider syncs documents from an external source into the knowledge store.
type Provider interface {
	Name() string
	Type() string
	Sync(ctx context.Context, st store.Store) (*SyncStats, error)
}

// SyncStats holds counters for a sync run.
type SyncStats struct {
	FilesFound    int
	FilesNew      int
	FilesUpdated  int
	FilesSkipped  int // unchanged (same hash)
	FilesDeleted  int
	ChunksCreated int
}
