package store

import (
	"math"
	"testing"
)

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		name string
		a, b []float32
		want float64
	}{
		{
			name: "identical vectors",
			a:    []float32{1, 2, 3},
			b:    []float32{1, 2, 3},
			want: 1.0,
		},
		{
			name: "orthogonal vectors",
			a:    []float32{1, 0, 0},
			b:    []float32{0, 1, 0},
			want: 0.0,
		},
		{
			name: "opposite vectors",
			a:    []float32{1, 0, 0},
			b:    []float32{-1, 0, 0},
			want: -1.0,
		},
		{
			name: "zero vector a",
			a:    []float32{0, 0, 0},
			b:    []float32{1, 2, 3},
			want: 0.0,
		},
		{
			name: "zero vector b",
			a:    []float32{1, 2, 3},
			b:    []float32{0, 0, 0},
			want: 0.0,
		},
		{
			name: "both zero vectors",
			a:    []float32{0, 0, 0},
			b:    []float32{0, 0, 0},
			want: 0.0,
		},
		{
			name: "different lengths",
			a:    []float32{1, 2},
			b:    []float32{1, 2, 3},
			want: 0.0,
		},
		{
			name: "known similarity",
			a:    []float32{1, 0},
			b:    []float32{1, 1},
			want: 1.0 / math.Sqrt(2),
		},
		{
			name: "scaled vectors are identical direction",
			a:    []float32{1, 2, 3},
			b:    []float32{2, 4, 6},
			want: 1.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CosineSimilarity(tt.a, tt.b)
			if math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("CosineSimilarity(%v, %v) = %f, want %f", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestSerializeEmbeddingRoundtrip(t *testing.T) {
	original := []float32{0.1, 0.2, 0.3, -0.5, 1.0}

	serialized := SerializeEmbedding(original)
	parsed, err := ParseEmbedding(serialized)
	if err != nil {
		t.Fatalf("ParseEmbedding: %v", err)
	}

	if len(parsed) != len(original) {
		t.Fatalf("length mismatch: got %d, want %d", len(parsed), len(original))
	}
	for i := range original {
		if parsed[i] != original[i] {
			t.Errorf("index %d: got %f, want %f", i, parsed[i], original[i])
		}
	}
}

func TestParseEmbeddingInvalid(t *testing.T) {
	_, err := ParseEmbedding("not json")
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestSerializeEmbeddingEmpty(t *testing.T) {
	s := SerializeEmbedding([]float32{})
	parsed, err := ParseEmbedding(s)
	if err != nil {
		t.Fatalf("ParseEmbedding: %v", err)
	}
	if len(parsed) != 0 {
		t.Errorf("expected empty slice, got %v", parsed)
	}
}

func TestSerializeEmbeddingNil(t *testing.T) {
	s := SerializeEmbedding(nil)
	if s != "null" {
		t.Errorf("expected 'null', got %q", s)
	}
}
