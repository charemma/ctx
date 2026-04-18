package search

import (
	"context"
	"fmt"
	"time"

	"github.com/charemma/ctx/internal/embedder"
	"github.com/charemma/ctx/internal/store"
)

// Query describes what to search for.
type Query struct {
	Text     string
	Category string   // PARA filter (optional)
	Tags     []string // tag filter (optional)
	Limit    int      // default 10
	MinScore float64  // default 0.3
}

// Result holds a single search hit with source attribution.
type Result struct {
	Content     string
	Score       float64
	SourcePath  string
	HeadingPath string
	Category    string
	Tags        []string
	LastSynced  time.Time
}

// Engine performs semantic search over the knowledge store.
type Engine struct {
	store    store.Store
	embedder embedder.Embedder
}

// New creates a search engine backed by the given store and embedder.
func New(st store.Store, emb embedder.Embedder) *Engine {
	return &Engine{store: st, embedder: emb}
}

// Search embeds the query text and finds similar chunks, applying optional
// category and tag filters.
func (e *Engine) Search(ctx context.Context, q Query) ([]Result, error) {
	if q.Text == "" {
		return nil, fmt.Errorf("query text must not be empty")
	}

	limit := q.Limit
	if limit <= 0 {
		limit = 10
	}
	minScore := q.MinScore
	if minScore <= 0 {
		minScore = 0.3
	}

	embeddings, err := e.embedder.Embed(ctx, []string{q.Text})
	if err != nil {
		return nil, fmt.Errorf("embedding query: %w", err)
	}
	if len(embeddings) == 0 {
		return nil, fmt.Errorf("embedder returned no vectors")
	}

	vec := embedder.ToVector(embeddings[0])

	hits, err := e.store.SearchChunks(ctx, vec, store.SearchOpts{
		Category: q.Category,
		Tags:     q.Tags,
		Limit:    limit,
		MinScore: minScore,
	})
	if err != nil {
		return nil, fmt.Errorf("searching store: %w", err)
	}

	results := make([]Result, len(hits))
	for i, h := range hits {
		results[i] = Result{
			Content:     h.Content,
			Score:       h.Score,
			SourcePath:  h.SourcePath,
			HeadingPath: h.HeadingPath,
			Category:    h.Category,
			Tags:        h.Tags,
			LastSynced:  h.LastSynced,
		}
	}
	return results, nil
}
