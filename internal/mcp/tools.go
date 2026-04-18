package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/charemma/ctx/internal/search"
	"github.com/charemma/ctx/internal/store"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func registerTools(srv *server.MCPServer, st store.Store, engine *search.Engine) {
	srv.AddTool(searchKnowledgeTool(), handleSearchKnowledge(engine))
	srv.AddTool(findDecisionsTool(), handleFindDecisions(engine))
	srv.AddTool(getProjectContextTool(), handleGetProjectContext(engine))
	srv.AddTool(searchJournalTool(), handleSearchJournal(engine))
	srv.AddTool(listSourcesTool(), handleListSources(st))
}

// resultEntry is the JSON structure returned for each search hit.
type resultEntry struct {
	Content     string   `json:"content"`
	Score       float64  `json:"score"`
	SourcePath  string   `json:"source_path"`
	HeadingPath string   `json:"heading_path,omitempty"`
	Category    string   `json:"category,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	LastSynced  string   `json:"last_synced"`
}

func resultsToEntries(results []search.Result) []resultEntry {
	entries := make([]resultEntry, len(results))
	for i, r := range results {
		entries[i] = resultEntry{
			Content:     r.Content,
			Score:       r.Score,
			SourcePath:  r.SourcePath,
			HeadingPath: r.HeadingPath,
			Category:    r.Category,
			Tags:        r.Tags,
			LastSynced:  r.LastSynced.Format(time.RFC3339),
		}
	}
	return entries
}

func marshalResults(entries any) (*mcp.CallToolResult, error) {
	data, err := json.Marshal(entries)
	if err != nil {
		return nil, fmt.Errorf("marshaling results: %w", err)
	}
	return mcp.NewToolResultText(string(data)), nil
}

// search_knowledge

func searchKnowledgeTool() mcp.Tool {
	return mcp.NewTool("search_knowledge",
		mcp.WithDescription("Semantic search across the knowledge store with optional filters"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithString("query", mcp.Required(), mcp.Description("Search query text")),
		mcp.WithString("category", mcp.Description("PARA category filter (e.g. '1 Projects', '2 Areas', '3 Resources', 'journal')")),
		mcp.WithArray("tags", mcp.Description("Filter by tags"), mcp.WithStringItems()),
		mcp.WithNumber("limit", mcp.Description("Max results to return (default 10)")),
		mcp.WithNumber("min_score", mcp.Description("Minimum similarity score 0-1 (default 0.3)")),
	)
}

func handleSearchKnowledge(engine *search.Engine) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()

		query, _ := args["query"].(string)
		if query == "" {
			return mcp.NewToolResultError("query parameter is required"), nil
		}

		q := search.Query{Text: query}

		if cat, ok := args["category"].(string); ok {
			q.Category = cat
		}
		if tagsRaw, ok := args["tags"].([]any); ok {
			for _, t := range tagsRaw {
				if s, ok := t.(string); ok {
					q.Tags = append(q.Tags, s)
				}
			}
		}
		if limit, ok := args["limit"].(float64); ok && limit > 0 {
			q.Limit = int(limit)
		}
		if minScore, ok := args["min_score"].(float64); ok && minScore > 0 {
			q.MinScore = minScore
		}

		results, err := engine.Search(ctx, q)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("search failed", err), nil
		}

		return marshalResults(resultsToEntries(results))
	}
}

// find_decisions

func findDecisionsTool() mcp.Tool {
	return mcp.NewTool("find_decisions",
		mcp.WithDescription("Search decision records (Areas/Decisions, docs/decisions)"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithString("topic", mcp.Required(), mcp.Description("Decision topic to search for")),
		mcp.WithNumber("limit", mcp.Description("Max results to return (default 10)")),
	)
}

func handleFindDecisions(engine *search.Engine) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()

		topic, _ := args["topic"].(string)
		if topic == "" {
			return mcp.NewToolResultError("topic parameter is required"), nil
		}

		limit := 10
		if l, ok := args["limit"].(float64); ok && l > 0 {
			limit = int(l)
		}

		// Search in areas category (where Decisions/ lives in PARA)
		results, err := engine.Search(ctx, search.Query{
			Text:     topic,
			Category: "areas",
			Limit:    limit,
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("search failed", err), nil
		}

		// Filter to only include results with "decisions" in the path
		var filtered []search.Result
		for _, r := range results {
			lower := strings.ToLower(r.SourcePath)
			if strings.Contains(lower, "decisions") {
				filtered = append(filtered, r)
			}
		}

		return marshalResults(resultsToEntries(filtered))
	}
}

// get_project_context

func getProjectContextTool() mcp.Tool {
	return mcp.NewTool("get_project_context",
		mcp.WithDescription("Aggregate project notes, decisions, and journal entries for a project"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithString("project", mcp.Required(), mcp.Description("Project name to search for")),
		mcp.WithBoolean("include_journal", mcp.Description("Include journal entries (default true)")),
	)
}

type projectContext struct {
	Project   string        `json:"project"`
	Notes     []resultEntry `json:"notes"`
	Decisions []resultEntry `json:"decisions"`
	Journal   []resultEntry `json:"journal,omitempty"`
}

func handleGetProjectContext(engine *search.Engine) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()

		project, _ := args["project"].(string)
		if project == "" {
			return mcp.NewToolResultError("project parameter is required"), nil
		}

		includeJournal := true
		if v, ok := args["include_journal"].(bool); ok {
			includeJournal = v
		}

		// Search project notes
		notes, err := engine.Search(ctx, search.Query{
			Text:     project,
			Category: "projects",
			Limit:    10,
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("searching project notes failed", err), nil
		}

		// Search decisions related to the project
		decisions, err := engine.Search(ctx, search.Query{
			Text:     project,
			Category: "areas",
			Limit:    5,
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("searching decisions failed", err), nil
		}

		// Filter decisions to those containing "decisions" in the path
		var filteredDecisions []search.Result
		for _, r := range decisions {
			lower := strings.ToLower(r.SourcePath)
			if strings.Contains(lower, "decisions") {
				filteredDecisions = append(filteredDecisions, r)
			}
		}

		result := projectContext{
			Project:   project,
			Notes:     resultsToEntries(notes),
			Decisions: resultsToEntries(filteredDecisions),
		}

		if includeJournal {
			journal, err := engine.Search(ctx, search.Query{
				Text:     project,
				Category: "journal",
				Limit:    10,
			})
			if err != nil {
				return mcp.NewToolResultErrorFromErr("searching journal failed", err), nil
			}
			result.Journal = resultsToEntries(journal)
		}

		return marshalResults(result)
	}
}

// search_journal

func searchJournalTool() mcp.Tool {
	return mcp.NewTool("search_journal",
		mcp.WithDescription("Search journal entries with optional date range"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithString("query", mcp.Required(), mcp.Description("Search query text")),
		mcp.WithString("date_from", mcp.Description("Start date filter (YYYY-MM-DD)")),
		mcp.WithString("date_to", mcp.Description("End date filter (YYYY-MM-DD)")),
		mcp.WithNumber("limit", mcp.Description("Max results to return (default 10)")),
	)
}

func handleSearchJournal(engine *search.Engine) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()

		query, _ := args["query"].(string)
		if query == "" {
			return mcp.NewToolResultError("query parameter is required"), nil
		}

		limit := 10
		if l, ok := args["limit"].(float64); ok && l > 0 {
			limit = int(l)
		}

		results, err := engine.Search(ctx, search.Query{
			Text:     query,
			Category: "journal",
			Limit:    limit,
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("search failed", err), nil
		}

		// Apply date filters if provided
		dateFrom, _ := args["date_from"].(string)
		dateTo, _ := args["date_to"].(string)

		if dateFrom != "" || dateTo != "" {
			results = filterByDate(results, dateFrom, dateTo)
		}

		return marshalResults(resultsToEntries(results))
	}
}

func filterByDate(results []search.Result, from, to string) []search.Result {
	var fromTime, toTime time.Time
	var hasFrom, hasTo bool

	if from != "" {
		if t, err := time.Parse("2006-01-02", from); err == nil {
			fromTime = t
			hasFrom = true
		}
	}
	if to != "" {
		if t, err := time.Parse("2006-01-02", to); err == nil {
			// Include the entire "to" day
			toTime = t.Add(24*time.Hour - time.Nanosecond)
			hasTo = true
		}
	}

	var filtered []search.Result
	for _, r := range results {
		if hasFrom && r.LastSynced.Before(fromTime) {
			continue
		}
		if hasTo && r.LastSynced.After(toTime) {
			continue
		}
		filtered = append(filtered, r)
	}
	return filtered
}

// list_sources

func listSourcesTool() mcp.Tool {
	return mcp.NewTool("list_sources",
		mcp.WithDescription("Show configured knowledge sources and their sync status"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
	)
}

type sourceEntry struct {
	Name       string  `json:"name"`
	Provider   string  `json:"provider"`
	Location   string  `json:"location"`
	LastSync   *string `json:"last_sync,omitempty"`
	DocsTotal  int     `json:"docs_total"`
	DocsSynced int     `json:"docs_synced"`
	Error      *string `json:"error,omitempty"`
}

func handleListSources(st store.Store) server.ToolHandlerFunc {
	return func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sources, err := st.ListSources(ctx)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("listing sources failed", err), nil
		}

		entries := make([]sourceEntry, 0, len(sources))
		for _, src := range sources {
			entry := sourceEntry{
				Name:     src.Name,
				Provider: src.Provider,
				Location: src.Location,
			}

			state, err := st.GetSyncState(ctx, src.ID)
			if err == nil && state != nil {
				if state.LastSync != nil {
					ts := state.LastSync.Format(time.RFC3339)
					entry.LastSync = &ts
				}
				entry.DocsTotal = state.DocsTotal
				entry.DocsSynced = state.DocsSynced
				entry.Error = state.Error
			}

			entries = append(entries, entry)
		}

		return marshalResults(entries)
	}
}
