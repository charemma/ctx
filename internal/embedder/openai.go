package embedder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"time"

	"github.com/charemma/ctx/internal/config"
)

const (
	openaiDefaultModel    = "text-embedding-3-small"
	openaiDefaultEndpoint = "https://api.openai.com/v1/embeddings"
	openaiDefaultDims     = 1536
	openaiMaxBatchSize    = 100
	maxRetries            = 5
	baseRetryDelay        = 500 * time.Millisecond
)

// OpenAI implements the Embedder interface using the OpenAI embeddings API.
type OpenAI struct {
	apiKey   string
	model    string
	endpoint string
	dims     int
	client   *http.Client
}

// NewOpenAI creates a new OpenAI embedder from config.
// The API key is read from OPENAI_API_KEY if not otherwise available.
func NewOpenAI(cfg config.EmbeddingConfig) (*OpenAI, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY environment variable is required")
	}

	model := cfg.Model
	if model == "" {
		model = openaiDefaultModel
	}

	return &OpenAI{
		apiKey:   apiKey,
		model:    model,
		endpoint: openaiDefaultEndpoint,
		dims:     openaiDefaultDims,
		client:   &http.Client{Timeout: 60 * time.Second},
	}, nil
}

func (o *OpenAI) Dimensions() int {
	return o.dims
}

func (o *OpenAI) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	var result [][]float32
	for start := 0; start < len(texts); start += openaiMaxBatchSize {
		end := start + openaiMaxBatchSize
		if end > len(texts) {
			end = len(texts)
		}
		batch, err := o.embedBatch(ctx, texts[start:end])
		if err != nil {
			return nil, fmt.Errorf("embedding batch [%d:%d]: %w", start, end, err)
		}
		result = append(result, batch...)
	}
	return result, nil
}

type openaiRequest struct {
	Input []string `json:"input"`
	Model string   `json:"model"`
}

type openaiResponse struct {
	Data []openaiEmbedding `json:"data"`
	Error *openaiError      `json:"error,omitempty"`
}

type openaiEmbedding struct {
	Index     int       `json:"index"`
	Embedding []float32 `json:"embedding"`
}

type openaiError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}

func (o *OpenAI) embedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	body, err := json.Marshal(openaiRequest{
		Input: texts,
		Model: o.model,
	})
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	var lastErr error
	for attempt := range maxRetries {
		result, err := o.doRequest(ctx, body)
		if err == nil {
			return result, nil
		}

		if !isRetryable(err) {
			return nil, err
		}

		lastErr = err
		delay := baseRetryDelay * time.Duration(math.Pow(2, float64(attempt)))
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}
	return nil, fmt.Errorf("max retries exceeded: %w", lastErr)
}

func (o *OpenAI) doRequest(ctx context.Context, body []byte) ([][]float32, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+o.apiKey)

	resp, err := o.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, &rateLimitError{status: resp.StatusCode}
	}
	if resp.StatusCode >= http.StatusInternalServerError {
		return nil, &rateLimitError{status: resp.StatusCode}
	}
	if resp.StatusCode != http.StatusOK {
		var apiResp openaiResponse
		if err := json.Unmarshal(respBody, &apiResp); err == nil && apiResp.Error != nil {
			return nil, fmt.Errorf("openai API error (%d): %s", resp.StatusCode, apiResp.Error.Message)
		}
		return nil, fmt.Errorf("openai API error: status %d", resp.StatusCode)
	}

	var apiResp openaiResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	embeddings := make([][]float32, len(apiResp.Data))
	for _, d := range apiResp.Data {
		embeddings[d.Index] = d.Embedding
	}
	return embeddings, nil
}

// rateLimitError signals a retryable error (429 or 5xx).
type rateLimitError struct {
	status int
}

func (e *rateLimitError) Error() string {
	return fmt.Sprintf("retryable HTTP status: %d", e.status)
}

func isRetryable(err error) bool {
	_, ok := err.(*rateLimitError)
	return ok
}
