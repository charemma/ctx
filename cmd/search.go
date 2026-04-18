package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/charemma/ctx/internal/config"
	"github.com/charemma/ctx/internal/embedder"
	"github.com/charemma/ctx/internal/store"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var (
	searchCategory string
	searchTags     string
	searchLimit    int
)

var scoreHighStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
var scoreMidStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
var scoreLowStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
var pathStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
var headingStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
var snippetStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("7"))

var searchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Semantic search over your knowledge base",
	Args:  cobra.ExactArgs(1),
	RunE:  runSearch,
}

func init() {
	searchCmd.Flags().StringVar(&searchCategory, "category", "", "filter by PARA category (e.g. resources, projects)")
	searchCmd.Flags().StringVar(&searchTags, "tags", "", "filter by tags (comma-separated)")
	searchCmd.Flags().IntVar(&searchLimit, "limit", 5, "maximum number of results")
	rootCmd.AddCommand(searchCmd)
}

func runSearch(_ *cobra.Command, args []string) error {
	ctx := context.Background()
	query := args[0]

	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Create embedder to vectorize the query.
	emb, err := embedder.New(cfg.Embedding)
	if err != nil {
		return fmt.Errorf("creating embedder: %w", err)
	}

	embeddings, err := emb.Embed(ctx, []string{query})
	if err != nil {
		return fmt.Errorf("embedding query: %w", err)
	}
	if len(embeddings) == 0 {
		return fmt.Errorf("embedder returned no results")
	}

	queryVec := embedder.ToVector(embeddings[0])

	// Connect to store and search.
	s, err := store.NewPostgresStore(ctx, cfg.Database.URL)
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}
	defer s.Close()

	opts := store.SearchOpts{
		Category: searchCategory,
		Limit:    searchLimit,
	}
	if searchTags != "" {
		opts.Tags = strings.Split(searchTags, ",")
	}

	results, err := s.SearchChunks(ctx, queryVec, opts)
	if err != nil {
		return fmt.Errorf("searching: %w", err)
	}

	if len(results) == 0 {
		_, _ = fmt.Fprintln(os.Stdout, dimStyle.Render("No results found."))
		return nil
	}

	_, _ = fmt.Fprintf(os.Stdout, "%s %d results for %q\n\n", headerStyle.Render("Search"), len(results), query)

	for i, r := range results {
		// Score with color based on relevance.
		scoreStr := fmt.Sprintf("%.3f", r.Score)
		var styledScore string
		switch {
		case r.Score >= 0.7:
			styledScore = scoreHighStyle.Render(scoreStr)
		case r.Score >= 0.5:
			styledScore = scoreMidStyle.Render(scoreStr)
		default:
			styledScore = scoreLowStyle.Render(scoreStr)
		}

		_, _ = fmt.Fprintf(os.Stdout, "%d. [%s] %s\n", i+1, styledScore, pathStyle.Render(r.SourcePath))

		if r.HeadingPath != "" {
			_, _ = fmt.Fprintf(os.Stdout, "   %s\n", headingStyle.Render(r.HeadingPath))
		}

		// Content snippet (first 200 chars).
		snippet := r.Content
		if len(snippet) > 200 {
			snippet = snippet[:200] + "..."
		}
		snippet = strings.ReplaceAll(snippet, "\n", " ")
		_, _ = fmt.Fprintf(os.Stdout, "   %s\n", snippetStyle.Render(snippet))

		_, _ = fmt.Fprintf(os.Stdout, "   %s\n\n", dimStyle.Render("synced: "+r.LastSynced.Format("2006-01-02 15:04")))
	}

	return nil
}
