package mcp

import (
	"github.com/charemma/ctx/internal/search"
	"github.com/charemma/ctx/internal/store"
	"github.com/mark3labs/mcp-go/server"
)

// Serve creates an MCP server with all tools registered and serves on stdio.
func Serve(st store.Store, engine *search.Engine) error {
	srv := server.NewMCPServer(
		"ctx",
		"0.1.0",
		server.WithToolCapabilities(true),
	)

	registerTools(srv, st, engine)

	return server.ServeStdio(srv)
}
