package store

import (
	"encoding/json"
	"fmt"
	"math"
)

// CosineSimilarity computes the cosine similarity between two float32 vectors.
// Returns 0 if either vector has zero magnitude.
func CosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}

	var dot, magA, magB float64
	for i := range a {
		ai, bi := float64(a[i]), float64(b[i])
		dot += ai * bi
		magA += ai * ai
		magB += bi * bi
	}

	if magA == 0 || magB == 0 {
		return 0
	}

	return dot / (math.Sqrt(magA) * math.Sqrt(magB))
}

// ParseEmbedding parses a JSON-encoded float32 array from a string.
func ParseEmbedding(s string) ([]float32, error) {
	var v []float32
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return nil, fmt.Errorf("parsing embedding: %w", err)
	}
	return v, nil
}

// SerializeEmbedding converts a float32 slice to a JSON array string.
func SerializeEmbedding(v []float32) string {
	data, _ := json.Marshal(v)
	return string(data)
}
