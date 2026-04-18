package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/charemma/ctx/internal/config"
	"github.com/charemma/ctx/internal/embedder"
	"github.com/charemma/ctx/internal/provider/obsidian"
	"github.com/charemma/ctx/internal/store"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var syncCmd = &cobra.Command{
	Use:   "sync [source-name]",
	Short: "Sync knowledge sources and embed chunks",
	Long:  "Sync documents from configured sources into the knowledge store, then generate embeddings for new chunks.",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runSync,
}

func init() {
	rootCmd.AddCommand(syncCmd)
}

func runSync(_ *cobra.Command, args []string) error {
	ctx := context.Background()

	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if len(cfg.Sources) == 0 {
		_, _ = fmt.Fprintln(os.Stderr, warnStyle.Render("No sources configured. Run 'ctx init' first."))
		return nil
	}

	s, err := store.New(ctx, cfg.Database)
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}
	defer s.Close()

	// Filter sources if a name was provided.
	sources := cfg.Sources
	if len(args) > 0 {
		name := args[0]
		var filtered []config.SourceConfig
		for _, src := range sources {
			if src.Name == name {
				filtered = append(filtered, src)
			}
		}
		if len(filtered) == 0 {
			return fmt.Errorf("source %q not found in config", name)
		}
		sources = filtered
	}

	// Sync each source.
	for _, srcCfg := range sources {
		if err := syncSource(ctx, s, srcCfg); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "%s syncing %s: %v\n", errStyle.Render("Error"), srcCfg.Name, err)
			continue
		}
	}

	// Embed unembedded chunks.
	if err := embedChunks(ctx, s, cfg.Embedding); err != nil {
		return fmt.Errorf("embedding chunks: %w", err)
	}

	return nil
}

func syncSource(ctx context.Context, s store.Store, srcCfg config.SourceConfig) error {
	_, _ = fmt.Fprintf(os.Stdout, "%s %s (%s)\n", headerStyle.Render("Syncing"), srcCfg.Name, srcCfg.Location)

	// Ensure source record exists in DB.
	existing, err := s.GetSourceByName(ctx, srcCfg.Name)
	if err != nil {
		return fmt.Errorf("checking source: %w", err)
	}
	if existing == nil {
		src := &store.Source{
			Provider: srcCfg.Type,
			Name:     srcCfg.Name,
			Location: srcCfg.Location,
		}
		if err := s.CreateSource(ctx, src); err != nil {
			return fmt.Errorf("creating source: %w", err)
		}
	}

	// Create and run the provider.
	switch srcCfg.Type {
	case "obsidian":
		return syncObsidian(ctx, s, srcCfg)
	default:
		return fmt.Errorf("unsupported source type: %q", srcCfg.Type)
	}
}

func syncObsidian(ctx context.Context, s store.Store, srcCfg config.SourceConfig) error {
	op := obsidian.New(srcCfg.Name, srcCfg.Location, nil)
	stats, err := op.Sync(ctx, s)
	if err != nil {
		return fmt.Errorf("sync failed: %w", err)
	}

	// Update sync state.
	sourceID, err := getSourceID(ctx, s, srcCfg.Name)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "%s getting source ID: %v\n", warnStyle.Render("Warning:"), err)
	} else {
		now := time.Now()
		syncState := &store.SyncState{
			SourceID:    sourceID,
			LastSync:    &now,
			DocsTotal:   stats.FilesFound,
			DocsSynced:  stats.FilesNew + stats.FilesUpdated,
			DocsSkipped: stats.FilesSkipped,
		}
		if err := s.UpdateSyncState(ctx, syncState); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "%s updating sync state: %v\n", warnStyle.Render("Warning:"), err)
		}
	}

	// Print summary.
	_, _ = fmt.Fprintf(os.Stdout, "  Files found:    %d\n", stats.FilesFound)
	_, _ = fmt.Fprintf(os.Stdout, "  New:            %d\n", stats.FilesNew)
	_, _ = fmt.Fprintf(os.Stdout, "  Updated:        %d\n", stats.FilesUpdated)
	_, _ = fmt.Fprintf(os.Stdout, "  Skipped:        %d\n", stats.FilesSkipped)
	_, _ = fmt.Fprintf(os.Stdout, "  Deleted:        %d\n", stats.FilesDeleted)
	_, _ = fmt.Fprintf(os.Stdout, "  Chunks created: %d\n", stats.ChunksCreated)

	return nil
}

func getSourceID(ctx context.Context, s store.Store, name string) (uuid.UUID, error) {
	src, err := s.GetSourceByName(ctx, name)
	if err != nil {
		return uuid.Nil, err
	}
	if src == nil {
		return uuid.Nil, fmt.Errorf("source %q not found", name)
	}
	return src.ID, nil
}

func embedChunks(ctx context.Context, s store.Store, embCfg config.EmbeddingConfig) error {
	chunks, err := s.GetUnembeddedChunks(ctx, 0)
	if err != nil {
		return fmt.Errorf("getting unembedded chunks: %w", err)
	}

	if len(chunks) == 0 {
		_, _ = fmt.Fprintln(os.Stdout, okStyle.Render("All chunks are embedded."))
		return nil
	}

	_, _ = fmt.Fprintf(os.Stdout, "\n%s %d chunks\n", headerStyle.Render("Embedding"), len(chunks))

	emb, err := embedder.New(embCfg)
	if err != nil {
		return fmt.Errorf("creating embedder: %w", err)
	}

	// Process in batches of 100.
	batchSize := 100
	for i := 0; i < len(chunks); i += batchSize {
		end := i + batchSize
		if end > len(chunks) {
			end = len(chunks)
		}
		batch := chunks[i:end]

		texts := make([]string, len(batch))
		for j, c := range batch {
			texts[j] = c.Content
		}

		embeddings, err := emb.Embed(ctx, texts)
		if err != nil {
			return fmt.Errorf("embedding batch %d-%d: %w", i, end, err)
		}

		for j, vec := range embeddings {
			if err := s.UpdateChunkEmbedding(ctx, batch[j].ID, embedder.ToVector(vec)); err != nil {
				return fmt.Errorf("updating embedding for chunk %s: %w", batch[j].ID, err)
			}
		}

		_, _ = fmt.Fprintf(os.Stdout, "  Embedded %d/%d chunks\n", end, len(chunks))
	}

	_, _ = fmt.Fprintln(os.Stdout, okStyle.Render("Embedding complete."))
	return nil
}
