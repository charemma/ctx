package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charemma/ctx/internal/config"
	"github.com/charemma/ctx/internal/store"
	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize ctx configuration",
	Long:  "Interactive setup wizard that detects your environment and creates a configuration file.",
	RunE:  runInit,
}

func init() {
	initCmd.Flags().BoolVar(&yesFlag, "yes", false, "use defaults without prompting")
	rootCmd.AddCommand(initCmd)
}

func runInit(_ *cobra.Command, _ []string) error {
	cfg := config.DefaultConfig()

	vaultPath := detectObsidianVault()
	embeddingProvider := "openai"
	dbMode := "docker"
	dbURL := "postgres://ctx:ctx@localhost:5432/ctx?sslmode=disable"

	if vaultPath != "" {
		cfg.Sources = []config.SourceConfig{
			{Name: "obsidian", Type: "obsidian", Location: vaultPath},
		}
	}

	if !yesFlag {
		if err := runInitWizard(cfg, &vaultPath, &embeddingProvider, &dbMode, &dbURL); err != nil {
			return err
		}
	}

	// Apply wizard results to config.
	cfg.Database.URL = dbURL
	cfg.Embedding.Provider = embeddingProvider
	if embeddingProvider == "ollama" {
		cfg.Embedding.Model = "nomic-embed-text"
	}
	if vaultPath != "" {
		cfg.Sources = []config.SourceConfig{
			{Name: "obsidian", Type: "obsidian", Location: vaultPath},
		}
	}

	// Check OpenAI API key if using OpenAI.
	if cfg.Embedding.Provider == "openai" && os.Getenv("OPENAI_API_KEY") == "" {
		_, _ = fmt.Fprintln(os.Stderr, warnStyle.Render("OPENAI_API_KEY is not set. Embedding will fail until it is configured."))
	}

	// Start Docker if requested.
	if dbMode == "docker" {
		if err := startDockerDB(); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "%s %v\n", warnStyle.Render("Could not start Docker database:"), err)
			_, _ = fmt.Fprintln(os.Stderr, "You can start it manually with: docker compose up -d")
		}
	}

	// Write config file.
	if err := config.Save(cfg, cfgFile); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	cfgPath := cfgFile
	if cfgPath == "" {
		dir, _ := config.ConfigDir()
		cfgPath = filepath.Join(dir, "ctx.yaml")
	}
	_, _ = fmt.Fprintf(os.Stdout, "Config written to %s\n", cfgPath)

	// Run migrations.
	ctx := context.Background()
	s, err := store.NewPostgresStore(ctx, cfg.Database.URL)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "%s %v\n", warnStyle.Render("Could not connect to database:"), err)
		_, _ = fmt.Fprintln(os.Stderr, "Run 'ctx db migrate' after the database is available.")
		return nil
	}
	defer s.Close()

	_, _ = fmt.Fprintln(os.Stdout, "Running database migrations...")
	if err := s.Migrate(ctx); err != nil {
		return fmt.Errorf("running migrations: %w", err)
	}
	_, _ = fmt.Fprintln(os.Stdout, okStyle.Render("Migrations applied successfully."))

	_, _ = fmt.Fprintln(os.Stdout)
	_, _ = fmt.Fprintln(os.Stdout, headerStyle.Render("Setup complete. Next steps:"))
	_, _ = fmt.Fprintln(os.Stdout, "  ctx sync     -- sync your knowledge sources")
	_, _ = fmt.Fprintln(os.Stdout, "  ctx status   -- check current state")
	_, _ = fmt.Fprintln(os.Stdout, "  ctx search   -- search your knowledge base")

	return nil
}

func runInitWizard(cfg *config.Config, vaultPath *string, embeddingProvider *string, dbMode *string, dbURL *string) error {
	// Step 1: Vault path.
	vaultInput := *vaultPath
	err := huh.NewInput().
		Title("Obsidian vault path").
		Description("Path to your Obsidian vault (leave empty to skip)").
		Value(&vaultInput).
		Run()
	if err != nil {
		return fmt.Errorf("vault prompt: %w", err)
	}
	*vaultPath = strings.TrimSpace(vaultInput)

	// Expand ~ in vault path.
	if strings.HasPrefix(*vaultPath, "~/") {
		home, _ := os.UserHomeDir()
		*vaultPath = filepath.Join(home, (*vaultPath)[2:])
	}

	// Validate vault path if provided.
	if *vaultPath != "" {
		if _, err := os.Stat(filepath.Join(*vaultPath, ".obsidian")); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "%s No .obsidian directory found at %s\n", warnStyle.Render("Warning:"), *vaultPath)
		}
	}

	// Step 2: Embedding provider.
	err = huh.NewSelect[string]().
		Title("Embedding provider").
		Options(
			huh.NewOption("OpenAI (text-embedding-3-small)", "openai"),
			huh.NewOption("Ollama (nomic-embed-text, local)", "ollama"),
		).
		Value(embeddingProvider).
		Run()
	if err != nil {
		return fmt.Errorf("embedding prompt: %w", err)
	}

	// Step 3: Database.
	err = huh.NewSelect[string]().
		Title("Database setup").
		Options(
			huh.NewOption("Docker (auto-start pgvector container)", "docker"),
			huh.NewOption("Custom URL (existing Postgres)", "custom"),
		).
		Value(dbMode).
		Run()
	if err != nil {
		return fmt.Errorf("database prompt: %w", err)
	}

	if *dbMode == "custom" {
		err = huh.NewInput().
			Title("Database URL").
			Description("PostgreSQL connection string").
			Value(dbURL).
			Run()
		if err != nil {
			return fmt.Errorf("database URL prompt: %w", err)
		}
	}

	return nil
}

// detectObsidianVault looks for an Obsidian vault in common locations.
func detectObsidianVault() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	candidates := []string{
		filepath.Join(home, "Documents", "Notes"),
		filepath.Join(home, "Obsidian"),
		filepath.Join(home, "obsidian"),
	}

	for _, dir := range candidates {
		if _, err := os.Stat(filepath.Join(dir, ".obsidian")); err == nil {
			return dir
		}
	}
	return ""
}

// startDockerDB attempts to start the database using docker compose.
func startDockerDB() error {
	// Look for docker-compose.yaml in the project directory or config directory.
	composePaths := []string{}

	// Check config directory for a generated compose file.
	cfgDir, err := config.ConfigDir()
	if err == nil {
		composePaths = append(composePaths, filepath.Join(cfgDir, "docker-compose.yaml"))
	}

	// Find an existing compose file or generate one.
	var composePath string
	for _, p := range composePaths {
		if _, err := os.Stat(p); err == nil {
			composePath = p
			break
		}
	}

	if composePath == "" {
		// Generate a docker-compose.yaml in the config directory.
		if cfgDir == "" {
			return fmt.Errorf("cannot determine config directory")
		}
		composePath = filepath.Join(cfgDir, "docker-compose.yaml")
		if err := os.MkdirAll(cfgDir, 0o755); err != nil {
			return fmt.Errorf("creating config directory: %w", err)
		}
		composeContent := `services:
  postgres:
    image: pgvector/pgvector:pg16
    ports:
      - "5432:5432"
    environment:
      POSTGRES_DB: ctx
      POSTGRES_USER: ctx
      POSTGRES_PASSWORD: ctx
    volumes:
      - pgdata:/var/lib/postgresql/data

volumes:
  pgdata:
`
		if err := os.WriteFile(composePath, []byte(composeContent), 0o644); err != nil {
			return fmt.Errorf("writing docker-compose.yaml: %w", err)
		}
	}

	_, _ = fmt.Fprintln(os.Stdout, "Starting database container...")
	cmd := exec.Command("docker", "compose", "-f", composePath, "up", "-d", "postgres")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
