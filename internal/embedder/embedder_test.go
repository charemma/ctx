package embedder

import (
	"testing"

	"github.com/charemma/ctx/internal/config"
)

func TestNew_UnknownProvider(t *testing.T) {
	_, err := New(config.EmbeddingConfig{Provider: "unknown"})
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
}

func TestNew_Ollama(t *testing.T) {
	e, err := New(config.EmbeddingConfig{Provider: "ollama"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := e.(*Ollama); !ok {
		t.Error("expected *Ollama type")
	}
}

func TestNew_OpenAI_RequiresKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	_, err := New(config.EmbeddingConfig{Provider: "openai"})
	if err == nil {
		t.Fatal("expected error when OPENAI_API_KEY is not set")
	}
}

func TestToVector(t *testing.T) {
	embedding := []float32{0.1, 0.2, 0.3}
	v := ToVector(embedding)
	slice := v.Slice()
	if len(slice) != 3 {
		t.Fatalf("expected 3 dimensions, got %d", len(slice))
	}
	if slice[0] != 0.1 || slice[1] != 0.2 || slice[2] != 0.3 {
		t.Errorf("unexpected vector values: %v", slice)
	}
}
