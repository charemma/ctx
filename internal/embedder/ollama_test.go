package embedder

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/charemma/ctx/internal/config"
)

func TestOllama_Embed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		var req ollamaRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decoding request: %v", err)
		}

		if req.Model != "nomic-embed-text" {
			t.Errorf("expected model nomic-embed-text, got %s", req.Model)
		}

		resp := ollamaResponse{
			Embeddings: make([][]float32, len(req.Input)),
		}
		for i := range req.Input {
			resp.Embeddings[i] = make([]float32, 768)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	embedder := &Ollama{
		model:    "nomic-embed-text",
		endpoint: server.URL,
		dims:     768,
		client:   server.Client(),
	}

	texts := []string{"hello", "world", "test"}
	result, err := embedder.Embed(context.Background(), texts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("expected 3 embeddings, got %d", len(result))
	}
	if len(result[0]) != 768 {
		t.Errorf("expected 768 dimensions, got %d", len(result[0]))
	}
}

func TestOllama_EmptyInput(t *testing.T) {
	embedder := &Ollama{
		model:    "nomic-embed-text",
		endpoint: "http://unused",
		dims:     768,
		client:   http.DefaultClient,
	}

	result, err := embedder.Embed(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil result for empty input, got %v", result)
	}
}

func TestOllama_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("model not found"))
	}))
	defer server.Close()

	embedder := &Ollama{
		model:    "nomic-embed-text",
		endpoint: server.URL,
		dims:     768,
		client:   server.Client(),
	}

	_, err := embedder.Embed(context.Background(), []string{"hello"})
	if err == nil {
		t.Fatal("expected error for server error response")
	}
}

func TestOllama_MismatchedEmbeddingCount(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return fewer embeddings than requested
		resp := ollamaResponse{
			Embeddings: [][]float32{make([]float32, 768)},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	embedder := &Ollama{
		model:    "nomic-embed-text",
		endpoint: server.URL,
		dims:     768,
		client:   server.Client(),
	}

	_, err := embedder.Embed(context.Background(), []string{"hello", "world"})
	if err == nil {
		t.Fatal("expected error for mismatched embedding count")
	}
}

func TestOllama_Dimensions(t *testing.T) {
	embedder := &Ollama{dims: 768}
	if d := embedder.Dimensions(); d != 768 {
		t.Errorf("expected 768, got %d", d)
	}
}

func TestOllama_DefaultModel(t *testing.T) {
	o, err := NewOllama(config.EmbeddingConfig{Provider: "ollama"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if o.model != "nomic-embed-text" {
		t.Errorf("expected default model nomic-embed-text, got %s", o.model)
	}
	if o.dims != 768 {
		t.Errorf("expected 768 dims for default model, got %d", o.dims)
	}
}

func TestOllama_CustomModel(t *testing.T) {
	o, err := NewOllama(config.EmbeddingConfig{Provider: "ollama", Model: "mxbai-embed-large"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if o.model != "mxbai-embed-large" {
		t.Errorf("expected model mxbai-embed-large, got %s", o.model)
	}
	if o.dims != 1024 {
		t.Errorf("expected 1024 dims for mxbai-embed-large, got %d", o.dims)
	}
}
