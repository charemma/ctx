package embedder

import (
	"context"
	"fmt"

	"github.com/charemma/ctx/internal/config"
	"github.com/pgvector/pgvector-go"
)

// Embedder generates vector embeddings from text.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	Dimensions() int
}

// New creates an Embedder based on the given configuration.
func New(cfg config.EmbeddingConfig) (Embedder, error) {
	switch cfg.Provider {
	case "openai":
		return NewOpenAI(cfg)
	case "ollama":
		return NewOllama(cfg)
	default:
		return nil, fmt.Errorf("unknown embedding provider: %q", cfg.Provider)
	}
}

// ToVector converts an embedding slice to a pgvector.Vector.
func ToVector(embedding []float32) pgvector.Vector {
	return pgvector.NewVector(embedding)
}
