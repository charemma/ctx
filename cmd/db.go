package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/charemma/ctx/internal/config"
	"github.com/charemma/ctx/internal/store"
	"github.com/spf13/cobra"
)

var (
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	okStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	warnStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
)

var dbCmd = &cobra.Command{
	Use:   "db",
	Short: "Database management commands",
}

var dbMigrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Run pending database migrations",
	RunE:  runDBMigrate,
}

var dbResetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Drop all tables and re-run migrations",
	RunE:  runDBReset,
}

var dbStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show migration status and table counts",
	RunE:  runDBStatus,
}

var yesFlag bool

func init() {
	dbResetCmd.Flags().BoolVar(&yesFlag, "yes", false, "skip confirmation prompt")

	dbCmd.AddCommand(dbMigrateCmd)
	dbCmd.AddCommand(dbResetCmd)
	dbCmd.AddCommand(dbStatusCmd)
	rootCmd.AddCommand(dbCmd)
}

func openStore(ctx context.Context) (*store.PostgresStore, error) {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}

	s, err := store.NewPostgresStore(ctx, cfg.Database.URL)
	if err != nil {
		return nil, fmt.Errorf("connecting to database: %w", err)
	}
	return s, nil
}

func runDBMigrate(_ *cobra.Command, _ []string) error {
	ctx := context.Background()

	s, err := openStore(ctx)
	if err != nil {
		return err
	}
	defer s.Close()

	_, pending, err := s.MigrationStatus(ctx)
	if err != nil {
		return fmt.Errorf("checking migration status: %w", err)
	}

	if len(pending) == 0 {
		_, _ = fmt.Fprintln(os.Stdout, okStyle.Render("All migrations already applied."))
		return nil
	}

	_, _ = fmt.Fprintf(os.Stdout, "Applying %d pending migration(s)...\n", len(pending))

	if err := s.Migrate(ctx); err != nil {
		return fmt.Errorf("running migrations: %w", err)
	}

	_, _ = fmt.Fprintln(os.Stdout, okStyle.Render("Migrations applied successfully."))
	return nil
}

func runDBReset(_ *cobra.Command, _ []string) error {
	ctx := context.Background()

	if !yesFlag {
		var confirm bool
		err := huh.NewConfirm().
			Title("Drop all tables and re-run migrations?").
			Description("This will delete all data in the database.").
			Affirmative("Yes, reset").
			Negative("Cancel").
			Value(&confirm).
			Run()
		if err != nil {
			return fmt.Errorf("prompt: %w", err)
		}
		if !confirm {
			_, _ = fmt.Fprintln(os.Stdout, "Reset cancelled.")
			return nil
		}
	}

	s, err := openStore(ctx)
	if err != nil {
		return err
	}
	defer s.Close()

	_, _ = fmt.Fprintln(os.Stdout, "Dropping all tables...")
	if err := s.DropAll(ctx); err != nil {
		return fmt.Errorf("dropping tables: %w", err)
	}

	_, _ = fmt.Fprintln(os.Stdout, "Running migrations...")
	if err := s.Migrate(ctx); err != nil {
		return fmt.Errorf("running migrations: %w", err)
	}

	_, _ = fmt.Fprintln(os.Stdout, okStyle.Render("Database reset complete."))
	return nil
}

func runDBStatus(_ *cobra.Command, _ []string) error {
	ctx := context.Background()

	s, err := openStore(ctx)
	if err != nil {
		return err
	}
	defer s.Close()

	applied, pending, err := s.MigrationStatus(ctx)
	if err != nil {
		return fmt.Errorf("checking migration status: %w", err)
	}

	_, _ = fmt.Fprintln(os.Stdout, headerStyle.Render("Migrations"))
	for _, m := range applied {
		_, _ = fmt.Fprintf(os.Stdout, "  %s %s\n", okStyle.Render("applied"), m)
	}
	for _, m := range pending {
		_, _ = fmt.Fprintf(os.Stdout, "  %s %s\n", warnStyle.Render("pending"), m)
	}
	if len(applied) == 0 && len(pending) == 0 {
		_, _ = fmt.Fprintln(os.Stdout, "  No migrations found.")
	}

	counts, err := s.TableCounts(ctx)
	if err != nil {
		return fmt.Errorf("getting table counts: %w", err)
	}

	_, _ = fmt.Fprintln(os.Stdout)
	_, _ = fmt.Fprintln(os.Stdout, headerStyle.Render("Tables"))
	for _, table := range []string{"sources", "documents", "chunks", "sync_state", "ingest_queue"} {
		count := counts[table]
		if count < 0 {
			_, _ = fmt.Fprintf(os.Stdout, "  %-15s %s\n", table, errStyle.Render("not found"))
		} else {
			_, _ = fmt.Fprintf(os.Stdout, "  %-15s %d rows\n", table, count)
		}
	}

	return nil
}
