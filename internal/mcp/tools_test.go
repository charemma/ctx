package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/charemma/ctx/internal/search"
	"github.com/charemma/ctx/internal/store"
	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/pgvector/pgvector-go"
)

// mockEmbedder returns a fixed embedding for any input.
type mockEmbedder struct {
	vec []float32
	dim int
}

func (m *mockEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = m.vec
	}
	return out, nil
}

func (m *mockEmbedder) Dimensions() int { return m.dim }

// mockStore implements store.Store partially for testing.
type mockStore struct {
	store.Store
	searchResults []store.SearchResult
	searchOpts    store.SearchOpts
	sources       []store.Source
	syncStates    map[uuid.UUID]*store.SyncState
}

func (m *mockStore) SearchChunks(_ context.Context, _ pgvector.Vector, opts store.SearchOpts) ([]store.SearchResult, error) {
	m.searchOpts = opts
	return m.searchResults, nil
}

func (m *mockStore) ListSources(_ context.Context) ([]store.Source, error) {
	return m.sources, nil
}

func (m *mockStore) GetSyncState(_ context.Context, id uuid.UUID) (*store.SyncState, error) {
	if m.syncStates != nil {
		if s, ok := m.syncStates[id]; ok {
			return s, nil
		}
	}
	return nil, nil
}

func newTestEngine(results []store.SearchResult) *search.Engine {
	ms := &mockStore{searchResults: results}
	me := &mockEmbedder{vec: make([]float32, 8), dim: 8}
	return search.New(ms, me)
}

func callTool(t *testing.T, handler func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error), args map[string]any) *mcp.CallToolResult {
	t.Helper()
	req := mcp.CallToolRequest{}
	req.Params.Arguments = args
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return result
}

func TestSearchKnowledgeRequiresQuery(t *testing.T) {
	engine := newTestEngine(nil)
	handler := handleSearchKnowledge(engine)
	result := callTool(t, handler, map[string]any{})

	if !result.IsError {
		t.Fatal("expected error for missing query")
	}
}

func TestSearchKnowledgeReturnsResults(t *testing.T) {
	now := time.Now()
	results := []store.SearchResult{
		{
			ChunkID:     uuid.New(),
			Content:     "Go is great",
			HeadingPath: "## Go",
			Score:       0.9,
			DocumentID:  uuid.New(),
			SourcePath:  "notes/go.md",
			Category:    "3 Resources",
			Tags:        []string{"go"},
			LastSynced:  now,
		},
	}

	engine := newTestEngine(results)
	handler := handleSearchKnowledge(engine)
	result := callTool(t, handler, map[string]any{"query": "golang"})

	if result.IsError {
		t.Fatal("unexpected error in result")
	}

	var entries []resultEntry
	text := result.Content[0].(mcp.TextContent).Text
	if err := json.Unmarshal([]byte(text), &entries); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].SourcePath != "notes/go.md" {
		t.Errorf("expected source_path 'notes/go.md', got %q", entries[0].SourcePath)
	}
	if entries[0].Score != 0.9 {
		t.Errorf("expected score 0.9, got %f", entries[0].Score)
	}
}

func TestSearchKnowledgePassesFilters(t *testing.T) {
	ms := &mockStore{}
	me := &mockEmbedder{vec: make([]float32, 8), dim: 8}
	engine := search.New(ms, me)
	handler := handleSearchKnowledge(engine)

	callTool(t, handler, map[string]any{
		"query":     "test",
		"category":  "projects",
		"tags":      []any{"infra", "nix"},
		"limit":     float64(5),
		"min_score": float64(0.5),
	})

	if ms.searchOpts.Category != "projects" {
		t.Errorf("expected category 'projects', got %q", ms.searchOpts.Category)
	}
	if len(ms.searchOpts.Tags) != 2 || ms.searchOpts.Tags[0] != "infra" {
		t.Errorf("expected tags [infra nix], got %v", ms.searchOpts.Tags)
	}
	if ms.searchOpts.Limit != 5 {
		t.Errorf("expected limit 5, got %d", ms.searchOpts.Limit)
	}
	if ms.searchOpts.MinScore != 0.5 {
		t.Errorf("expected min_score 0.5, got %f", ms.searchOpts.MinScore)
	}
}

func TestFindDecisionsRequiresTopic(t *testing.T) {
	engine := newTestEngine(nil)
	handler := handleFindDecisions(engine)
	result := callTool(t, handler, map[string]any{})

	if !result.IsError {
		t.Fatal("expected error for missing topic")
	}
}

func TestFindDecisionsFiltersPath(t *testing.T) {
	now := time.Now()
	results := []store.SearchResult{
		{
			ChunkID:    uuid.New(),
			Content:    "Use Go for CLI tools",
			Score:      0.8,
			DocumentID: uuid.New(),
			SourcePath: "2 Areas/Decisions/go-for-cli.md",
			Category:   "areas",
			LastSynced: now,
		},
		{
			ChunkID:    uuid.New(),
			Content:    "Some other area note",
			Score:      0.7,
			DocumentID: uuid.New(),
			SourcePath: "2 Areas/SomeOther/note.md",
			Category:   "areas",
			LastSynced: now,
		},
	}

	engine := newTestEngine(results)
	handler := handleFindDecisions(engine)
	result := callTool(t, handler, map[string]any{"topic": "CLI tools"})

	if result.IsError {
		t.Fatal("unexpected error in result")
	}

	var entries []resultEntry
	text := result.Content[0].(mcp.TextContent).Text
	if err := json.Unmarshal([]byte(text), &entries); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 filtered entry, got %d", len(entries))
	}
	if entries[0].SourcePath != "2 Areas/Decisions/go-for-cli.md" {
		t.Errorf("expected decisions path, got %q", entries[0].SourcePath)
	}
}

func TestGetProjectContextRequiresProject(t *testing.T) {
	engine := newTestEngine(nil)
	handler := handleGetProjectContext(engine)
	result := callTool(t, handler, map[string]any{})

	if !result.IsError {
		t.Fatal("expected error for missing project")
	}
}

func TestGetProjectContextAggregates(t *testing.T) {
	engine := newTestEngine(nil)
	handler := handleGetProjectContext(engine)
	result := callTool(t, handler, map[string]any{
		"project":         "ctx",
		"include_journal": true,
	})

	if result.IsError {
		t.Fatal("unexpected error in result")
	}

	var ctx projectContext
	text := result.Content[0].(mcp.TextContent).Text
	if err := json.Unmarshal([]byte(text), &ctx); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	if ctx.Project != "ctx" {
		t.Errorf("expected project 'ctx', got %q", ctx.Project)
	}
}

func TestGetProjectContextExcludesJournal(t *testing.T) {
	engine := newTestEngine(nil)
	handler := handleGetProjectContext(engine)
	result := callTool(t, handler, map[string]any{
		"project":         "ctx",
		"include_journal": false,
	})

	if result.IsError {
		t.Fatal("unexpected error")
	}

	var ctx projectContext
	text := result.Content[0].(mcp.TextContent).Text
	if err := json.Unmarshal([]byte(text), &ctx); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if ctx.Journal != nil {
		t.Errorf("expected nil journal when include_journal=false, got %v", ctx.Journal)
	}
}

func TestSearchJournalRequiresQuery(t *testing.T) {
	engine := newTestEngine(nil)
	handler := handleSearchJournal(engine)
	result := callTool(t, handler, map[string]any{})

	if !result.IsError {
		t.Fatal("expected error for missing query")
	}
}

func TestSearchJournalDateFilter(t *testing.T) {
	t1 := time.Date(2025, 3, 15, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2025, 4, 10, 10, 0, 0, 0, time.UTC)

	results := []store.SearchResult{
		{
			ChunkID:    uuid.New(),
			Content:    "old entry",
			Score:      0.8,
			DocumentID: uuid.New(),
			SourcePath: "journal/2025-03-15.md",
			Category:   "journal",
			LastSynced: t1,
		},
		{
			ChunkID:    uuid.New(),
			Content:    "new entry",
			Score:      0.7,
			DocumentID: uuid.New(),
			SourcePath: "journal/2025-04-10.md",
			Category:   "journal",
			LastSynced: t2,
		},
	}

	engine := newTestEngine(results)
	handler := handleSearchJournal(engine)
	result := callTool(t, handler, map[string]any{
		"query":     "meeting",
		"date_from": "2025-04-01",
	})

	if result.IsError {
		t.Fatal("unexpected error")
	}

	var entries []resultEntry
	text := result.Content[0].(mcp.TextContent).Text
	if err := json.Unmarshal([]byte(text), &entries); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry after date filter, got %d", len(entries))
	}
	if entries[0].SourcePath != "journal/2025-04-10.md" {
		t.Errorf("expected newer entry, got %q", entries[0].SourcePath)
	}
}

func TestListSources(t *testing.T) {
	srcID := uuid.New()
	now := time.Now()

	ms := &mockStore{
		sources: []store.Source{
			{
				ID:       srcID,
				Name:     "notes",
				Provider: "obsidian",
				Location: "/home/user/notes",
			},
		},
		syncStates: map[uuid.UUID]*store.SyncState{
			srcID: {
				SourceID:   srcID,
				LastSync:   &now,
				DocsTotal:  42,
				DocsSynced: 40,
			},
		},
	}

	handler := handleListSources(ms)
	result := callTool(t, handler, nil)

	if result.IsError {
		t.Fatal("unexpected error")
	}

	var entries []sourceEntry
	text := result.Content[0].(mcp.TextContent).Text
	if err := json.Unmarshal([]byte(text), &entries); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 source, got %d", len(entries))
	}
	if entries[0].Name != "notes" {
		t.Errorf("expected name 'notes', got %q", entries[0].Name)
	}
	if entries[0].DocsTotal != 42 {
		t.Errorf("expected 42 docs_total, got %d", entries[0].DocsTotal)
	}
	if entries[0].LastSync == nil {
		t.Error("expected last_sync to be set")
	}
}

func TestFilterByDateRange(t *testing.T) {
	results := []search.Result{
		{Content: "a", LastSynced: time.Date(2025, 1, 10, 0, 0, 0, 0, time.UTC)},
		{Content: "b", LastSynced: time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC)},
		{Content: "c", LastSynced: time.Date(2025, 3, 20, 0, 0, 0, 0, time.UTC)},
	}

	filtered := filterByDate(results, "2025-02-01", "2025-03-01")
	if len(filtered) != 1 {
		t.Fatalf("expected 1 result in range, got %d", len(filtered))
	}
	if filtered[0].Content != "b" {
		t.Errorf("expected content 'b', got %q", filtered[0].Content)
	}
}

func TestToolDefinitions(t *testing.T) {
	tools := []struct {
		name string
		tool mcp.Tool
	}{
		{"search_knowledge", searchKnowledgeTool()},
		{"find_decisions", findDecisionsTool()},
		{"get_project_context", getProjectContextTool()},
		{"search_journal", searchJournalTool()},
		{"list_sources", listSourcesTool()},
	}

	for _, tc := range tools {
		t.Run(tc.name, func(t *testing.T) {
			if tc.tool.Name != tc.name {
				t.Errorf("expected name %q, got %q", tc.name, tc.tool.Name)
			}
			if tc.tool.Description == "" {
				t.Error("expected non-empty description")
			}
		})
	}
}
