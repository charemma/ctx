package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/charemma/ctx/internal/config"
	"github.com/charemma/ctx/internal/embedder"
	ctxmcp "github.com/charemma/ctx/internal/mcp"
	"github.com/charemma/ctx/internal/search"
	"github.com/charemma/ctx/internal/store"
	"github.com/spf13/cobra"
)

var (
	serveMCP bool
	serveWeb bool
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start a ctx server (MCP or web)",
	RunE:  runServe,
}

func init() {
	serveCmd.Flags().BoolVar(&serveMCP, "mcp", false, "start MCP server (stdio transport)")
	serveCmd.Flags().BoolVar(&serveWeb, "web", false, "start web server")
	rootCmd.AddCommand(serveCmd)
}

func runServe(_ *cobra.Command, _ []string) error {
	if serveMCP {
		return runMCPServer()
	}
	if serveWeb {
		_, _ = fmt.Fprintln(os.Stdout, "Web server not yet implemented.")
		return nil
	}

	_, _ = fmt.Fprintln(os.Stderr, "Specify --mcp or --web to choose a server mode.")
	return nil
}

func runMCPServer() error {
	ctx := context.Background()

	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	st, err := store.NewPostgresStore(ctx, cfg.Database.URL)
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}
	defer st.Close()

	emb, err := embedder.New(cfg.Embedding)
	if err != nil {
		return fmt.Errorf("creating embedder: %w", err)
	}

	engine := search.New(st, emb)

	return ctxmcp.Serve(st, engine)
}
