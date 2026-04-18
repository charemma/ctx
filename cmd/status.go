package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var dimStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show current state of sources, documents, and embeddings",
	RunE:  runStatus,
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

func runStatus(_ *cobra.Command, _ []string) error {
	ctx := context.Background()

	s, err := openStore(ctx)
	if err != nil {
		return err
	}
	defer s.Close()

	// Database connectivity.
	if err := s.Ping(ctx); err != nil {
		_, _ = fmt.Fprintf(os.Stdout, "%s %s\n", headerStyle.Render("Database"), errStyle.Render("disconnected"))
		return fmt.Errorf("database ping: %w", err)
	}
	_, _ = fmt.Fprintf(os.Stdout, "%s %s\n", headerStyle.Render("Database"), okStyle.Render("connected"))

	// Sources.
	sources, err := s.ListSources(ctx)
	if err != nil {
		return fmt.Errorf("listing sources: %w", err)
	}

	_, _ = fmt.Fprintln(os.Stdout)
	_, _ = fmt.Fprintln(os.Stdout, headerStyle.Render("Sources"))
	if len(sources) == 0 {
		_, _ = fmt.Fprintln(os.Stdout, dimStyle.Render("  No sources configured."))
	}

	for _, src := range sources {
		_, _ = fmt.Fprintf(os.Stdout, "  %-20s %s (%s)\n", src.Name, src.Provider, src.Location)

		// Sync state.
		ss, err := s.GetSyncState(ctx, src.ID)
		if err != nil {
			_, _ = fmt.Fprintf(os.Stdout, "    Last sync: %s\n", dimStyle.Render("unknown"))
			continue
		}
		if ss == nil || ss.LastSync == nil {
			_, _ = fmt.Fprintf(os.Stdout, "    Last sync: %s\n", dimStyle.Render("never"))
		} else {
			_, _ = fmt.Fprintf(os.Stdout, "    Last sync: %s\n", ss.LastSync.Format("2006-01-02 15:04:05"))
			_, _ = fmt.Fprintf(os.Stdout, "    Docs:      %d total, %d synced, %d skipped\n",
				ss.DocsTotal, ss.DocsSynced, ss.DocsSkipped)
		}
		if ss != nil && ss.Error != nil {
			_, _ = fmt.Fprintf(os.Stdout, "    Error:     %s\n", errStyle.Render(*ss.Error))
		}
	}

	// Table counts.
	counts, err := s.TableCounts(ctx)
	if err != nil {
		return fmt.Errorf("getting table counts: %w", err)
	}

	_, _ = fmt.Fprintln(os.Stdout)
	_, _ = fmt.Fprintln(os.Stdout, headerStyle.Render("Store"))
	docCount := counts["documents"]
	chunkCount := counts["chunks"]
	if docCount >= 0 {
		_, _ = fmt.Fprintf(os.Stdout, "  Documents: %d\n", docCount)
	}
	if chunkCount >= 0 {
		_, _ = fmt.Fprintf(os.Stdout, "  Chunks:    %d\n", chunkCount)
	}

	// Embedding stats.
	unembedded, err := s.GetUnembeddedChunks(ctx, 0)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stdout, "  Embeddings: %s\n", warnStyle.Render("could not check"))
	} else {
		pending := len(unembedded)
		embedded := int(chunkCount) - pending
		if embedded < 0 {
			embedded = 0
		}
		_, _ = fmt.Fprintf(os.Stdout, "  Embedded:  %d\n", embedded)
		if pending > 0 {
			_, _ = fmt.Fprintf(os.Stdout, "  Pending:   %s\n", warnStyle.Render(fmt.Sprintf("%d", pending)))
		} else {
			_, _ = fmt.Fprintf(os.Stdout, "  Pending:   %s\n", okStyle.Render("0"))
		}
	}

	return nil
}
