package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

var cfgFile string

var rootCmd = &cobra.Command{
	Use:   "ctx",
	Short: "ctx - enterprise knowledge platform",
	Long: `ctx connects scattered company knowledge from Confluence, GitHub, Slack,
and other sources into a structured, searchable knowledge store. It delivers
context-aware results to the tools people already use -- IDEs, chat, email,
and CI/CD pipelines.`,
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: ~/.config/ctx/ctx.yaml)")
}

func Execute() {
	rootCmd.Version = Version
	rootCmd.SetVersionTemplate(fmt.Sprintf("ctx version %s (commit: %s, built: %s)\n", Version, Commit, Date))
	if err := rootCmd.Execute(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
