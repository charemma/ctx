package store

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charemma/ctx/internal/config"
)

// StoreWithExtras extends Store with methods needed by CLI commands
// (MigrationStatus, TableCounts, DropAll) that are not part of the core interface.
type StoreWithExtras interface {
	Store
	MigrationStatus(ctx context.Context) (applied []string, pending []string, err error)
	TableCounts(ctx context.Context) (map[string]int64, error)
	DropAll(ctx context.Context) error
}

// New creates a Store based on the given database configuration.
// If a Postgres URL is set, it connects to Postgres. Otherwise it uses SQLite.
func New(ctx context.Context, cfg config.DatabaseConfig) (StoreWithExtras, error) {
	if cfg.URL != "" && strings.HasPrefix(cfg.URL, "postgres") {
		return NewPostgresStore(ctx, cfg.URL)
	}

	path := cfg.Path
	if path == "" {
		dir, err := config.ConfigDir()
		if err != nil {
			return nil, fmt.Errorf("determining config directory: %w", err)
		}
		path = filepath.Join(dir, "ctx.db")
	}
	return NewSQLiteStore(path)
}
