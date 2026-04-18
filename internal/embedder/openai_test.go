package embedder

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/charemma/ctx/internal/config"
)

func TestOpenAI_Embed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-key" {
			t.Errorf("unexpected authorization header: %s", auth)
		}

		var req openaiRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decoding request: %v", err)
		}

		resp := openaiResponse{}
		for i := range req.Input {
			resp.Data = append(resp.Data, openaiEmbedding{
				Index:     i,
				Embedding: make([]float32, 1536),
			})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	embedder := &OpenAI{
		apiKey:   "test-key",
		model:    "text-embedding-3-small",
		endpoint: server.URL,
		dims:     1536,
		client:   server.Client(),
	}

	texts := []string{"hello", "world"}
	result, err := embedder.Embed(context.Background(), texts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 embeddings, got %d", len(result))
	}
	if len(result[0]) != 1536 {
		t.Errorf("expected 1536 dimensions, got %d", len(result[0]))
	}
}

func TestOpenAI_EmptyInput(t *testing.T) {
	embedder := &OpenAI{
		apiKey:   "test-key",
		model:    "text-embedding-3-small",
		endpoint: "http://unused",
		dims:     1536,
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

func TestOpenAI_BatchSplitting(t *testing.T) {
	var callCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)

		var req openaiRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decoding request: %v", err)
		}

		if len(req.Input) > openaiMaxBatchSize {
			t.Errorf("batch size %d exceeds max %d", len(req.Input), openaiMaxBatchSize)
		}

		resp := openaiResponse{}
		for i := range req.Input {
			resp.Data = append(resp.Data, openaiEmbedding{
				Index:     i,
				Embedding: make([]float32, 1536),
			})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	embedder := &OpenAI{
		apiKey:   "test-key",
		model:    "text-embedding-3-small",
		endpoint: server.URL,
		dims:     1536,
		client:   server.Client(),
	}

	// 150 texts should produce 2 API calls (100 + 50)
	texts := make([]string, 150)
	for i := range texts {
		texts[i] = "text"
	}

	result, err := embedder.Embed(context.Background(), texts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 150 {
		t.Errorf("expected 150 embeddings, got %d", len(result))
	}
	if calls := callCount.Load(); calls != 2 {
		t.Errorf("expected 2 API calls, got %d", calls)
	}
}

func TestOpenAI_RetryOnRateLimit(t *testing.T) {
	var callCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := callCount.Add(1)

		// First call returns 429, second succeeds
		if count == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}

		var req openaiRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decoding request: %v", err)
		}

		resp := openaiResponse{}
		for i := range req.Input {
			resp.Data = append(resp.Data, openaiEmbedding{
				Index:     i,
				Embedding: make([]float32, 1536),
			})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	embedder := &OpenAI{
		apiKey:   "test-key",
		model:    "text-embedding-3-small",
		endpoint: server.URL,
		dims:     1536,
		client:   server.Client(),
	}

	result, err := embedder.Embed(context.Background(), []string{"hello"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Errorf("expected 1 embedding, got %d", len(result))
	}
	if calls := callCount.Load(); calls != 2 {
		t.Errorf("expected 2 API calls (1 retry), got %d", calls)
	}
}

func TestOpenAI_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(openaiResponse{
			Error: &openaiError{
				Message: "invalid api key",
				Type:    "authentication_error",
			},
		})
	}))
	defer server.Close()

	embedder := &OpenAI{
		apiKey:   "bad-key",
		model:    "text-embedding-3-small",
		endpoint: server.URL,
		dims:     1536,
		client:   server.Client(),
	}

	_, err := embedder.Embed(context.Background(), []string{"hello"})
	if err == nil {
		t.Fatal("expected error for unauthorized request")
	}
}

func TestOpenAI_Dimensions(t *testing.T) {
	embedder := &OpenAI{dims: 1536}
	if d := embedder.Dimensions(); d != 1536 {
		t.Errorf("expected 1536, got %d", d)
	}
}

func TestOpenAI_NewRequiresAPIKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	_, err := NewOpenAI(config.EmbeddingConfig{Provider: "openai"})
	if err == nil {
		t.Fatal("expected error when OPENAI_API_KEY is not set")
	}
}
