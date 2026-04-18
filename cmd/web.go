package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/charemma/ctx/internal/config"
	"github.com/charemma/ctx/internal/embedder"
	"github.com/charemma/ctx/internal/search"
	"github.com/charemma/ctx/internal/store"
	"github.com/charemma/ctx/internal/web"
	"github.com/spf13/cobra"
)

var (
	webPort string
	webHost string
)

var webCmd = &cobra.Command{
	Use:   "web",
	Short: "Start the web UI server",
	RunE:  runWeb,
}

func init() {
	webCmd.Flags().StringVar(&webPort, "port", "8080", "port to listen on")
	webCmd.Flags().StringVar(&webHost, "host", "localhost", "host to bind to")
	rootCmd.AddCommand(webCmd)
}

func runWeb(_ *cobra.Command, _ []string) error {
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

	srv, err := web.New(engine, st, cfg)
	if err != nil {
		return fmt.Errorf("creating web server: %w", err)
	}

	addr := webHost + ":" + webPort
	_, _ = fmt.Fprintf(os.Stdout, "ctx web UI: http://%s\n", addr)

	return srv.ListenAndServe(addr)
}
