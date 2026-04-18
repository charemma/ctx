package cmd

import (
	"fmt"
	"os"

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
	serveCmd.Flags().BoolVar(&serveMCP, "mcp", false, "start MCP server")
	serveCmd.Flags().BoolVar(&serveWeb, "web", false, "start web server")
	rootCmd.AddCommand(serveCmd)
}

func runServe(_ *cobra.Command, _ []string) error {
	if serveMCP {
		_, _ = fmt.Fprintln(os.Stdout, "MCP server not yet implemented.")
		return nil
	}
	if serveWeb {
		_, _ = fmt.Fprintln(os.Stdout, "Web server not yet implemented.")
		return nil
	}

	_, _ = fmt.Fprintln(os.Stderr, "Specify --mcp or --web to choose a server mode.")
	return nil
}
