package embedder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/charemma/ctx/internal/config"
)

const (
	ollamaDefaultModel    = "nomic-embed-text"
	ollamaDefaultEndpoint = "http://localhost:11434/api/embed"
	ollamaDefaultDims     = 768
)

// modelDimensions maps known Ollama embedding models to their output dimensions.
var modelDimensions = map[string]int{
	"nomic-embed-text": 768,
	"mxbai-embed-large": 1024,
	"all-minilm":        384,
}

// Ollama implements the Embedder interface using the Ollama embeddings API.
type Ollama struct {
	model    string
	endpoint string
	dims     int
	client   *http.Client
}

// NewOllama creates a new Ollama embedder from config.
func NewOllama(cfg config.EmbeddingConfig) (*Ollama, error) {
	model := cfg.Model
	if model == "" {
		model = ollamaDefaultModel
	}

	endpoint := ollamaDefaultEndpoint
	if host := os.Getenv("OLLAMA_HOST"); host != "" {
		endpoint = host + "/api/embed"
	}

	dims := ollamaDefaultDims
	if d, ok := modelDimensions[model]; ok {
		dims = d
	}

	return &Ollama{
		model:    model,
		endpoint: endpoint,
		dims:     dims,
		client:   &http.Client{Timeout: 600 * time.Second},
	}, nil
}

func (o *Ollama) Dimensions() int {
	return o.dims
}

func (o *Ollama) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	body, err := json.Marshal(ollamaRequest{
		Model: o.model,
		Input: texts,
	})
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending request to ollama: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama API error: status %d: %s", resp.StatusCode, string(respBody))
	}

	var apiResp ollamaResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	if len(apiResp.Embeddings) != len(texts) {
		return nil, fmt.Errorf("expected %d embeddings, got %d", len(texts), len(apiResp.Embeddings))
	}

	return apiResp.Embeddings, nil
}

type ollamaRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type ollamaResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
}
