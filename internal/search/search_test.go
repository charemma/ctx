package search

import (
	"context"
	"testing"
	"time"

	"github.com/charemma/ctx/internal/store"
	"github.com/google/uuid"
	"github.com/pgvector/pgvector-go"
)

// mockEmbedder returns a fixed embedding for any input.
type mockEmbedder struct {
	vec []float32
	dim int
	err error
}

func (m *mockEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	if m.err != nil {
		return nil, m.err
	}
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = m.vec
	}
	return out, nil
}

func (m *mockEmbedder) Dimensions() int { return m.dim }

// mockStore implements only SearchChunks; all other methods panic.
type mockStore struct {
	store.Store // embed interface to satisfy compiler for unused methods
	results     []store.SearchResult
	opts        store.SearchOpts // captured opts from last call
	err         error
}

func (m *mockStore) SearchChunks(_ context.Context, _ pgvector.Vector, opts store.SearchOpts) ([]store.SearchResult, error) {
	m.opts = opts
	if m.err != nil {
		return nil, m.err
	}
	return m.results, nil
}

func TestSearchReturnsResults(t *testing.T) {
	now := time.Now()
	ms := &mockStore{
		results: []store.SearchResult{
			{
				ChunkID:     uuid.New(),
				Content:     "Go is a statically typed language",
				HeadingPath: "## Languages > Go",
				Score:       0.85,
				DocumentID:  uuid.New(),
				SourcePath:  "notes/go.md",
				Category:    "3 Resources",
				Tags:        []string{"go", "programming"},
				LastSynced:  now,
			},
		},
	}
	me := &mockEmbedder{vec: make([]float32, 8), dim: 8}

	engine := New(ms, me)
	results, err := engine.Search(context.Background(), Query{Text: "golang basics"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if r.Score != 0.85 {
		t.Errorf("expected score 0.85, got %f", r.Score)
	}
	if r.SourcePath != "notes/go.md" {
		t.Errorf("expected source path notes/go.md, got %s", r.SourcePath)
	}
	if r.Category != "3 Resources" {
		t.Errorf("expected category '3 Resources', got %s", r.Category)
	}
}

func TestSearchAppliesDefaults(t *testing.T) {
	ms := &mockStore{}
	me := &mockEmbedder{vec: make([]float32, 4), dim: 4}

	engine := New(ms, me)
	_, err := engine.Search(context.Background(), Query{Text: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ms.opts.Limit != 10 {
		t.Errorf("expected default limit 10, got %d", ms.opts.Limit)
	}
	if ms.opts.MinScore != 0.3 {
		t.Errorf("expected default min score 0.3, got %f", ms.opts.MinScore)
	}
}

func TestSearchPassesFilters(t *testing.T) {
	ms := &mockStore{}
	me := &mockEmbedder{vec: make([]float32, 4), dim: 4}

	engine := New(ms, me)
	_, err := engine.Search(context.Background(), Query{
		Text:     "architecture",
		Category: "1 Projects",
		Tags:     []string{"infra", "nix"},
		Limit:    5,
		MinScore: 0.5,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ms.opts.Category != "1 Projects" {
		t.Errorf("expected category '1 Projects', got %q", ms.opts.Category)
	}
	if len(ms.opts.Tags) != 2 || ms.opts.Tags[0] != "infra" {
		t.Errorf("expected tags [infra nix], got %v", ms.opts.Tags)
	}
	if ms.opts.Limit != 5 {
		t.Errorf("expected limit 5, got %d", ms.opts.Limit)
	}
	if ms.opts.MinScore != 0.5 {
		t.Errorf("expected min score 0.5, got %f", ms.opts.MinScore)
	}
}

func TestSearchEmptyResults(t *testing.T) {
	ms := &mockStore{results: nil}
	me := &mockEmbedder{vec: make([]float32, 4), dim: 4}

	engine := New(ms, me)
	results, err := engine.Search(context.Background(), Query{Text: "obscure topic"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestSearchEmptyQueryReturnsError(t *testing.T) {
	engine := New(&mockStore{}, &mockEmbedder{})
	_, err := engine.Search(context.Background(), Query{})
	if err == nil {
		t.Fatal("expected error for empty query text")
	}
}

func TestSearchEmbedderError(t *testing.T) {
	me := &mockEmbedder{err: context.DeadlineExceeded}
	engine := New(&mockStore{}, me)
	_, err := engine.Search(context.Background(), Query{Text: "test"})
	if err == nil {
		t.Fatal("expected error when embedder fails")
	}
}

func TestSearchStoreError(t *testing.T) {
	ms := &mockStore{err: context.DeadlineExceeded}
	me := &mockEmbedder{vec: make([]float32, 4), dim: 4}
	engine := New(ms, me)
	_, err := engine.Search(context.Background(), Query{Text: "test"})
	if err == nil {
		t.Fatal("expected error when store fails")
	}
}
