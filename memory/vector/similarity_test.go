package vector_test

import (
	"math"
	"testing"

	"github.com/Germanblandin1/goagent/memory/vector"
)

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		name string
		a, b []float32
		want float64
		tol  float64
	}{
		{
			name: "identical unit vectors",
			a:    []float32{1, 0, 0},
			b:    []float32{1, 0, 0},
			want: 1.0,
			tol:  1e-6,
		},
		{
			name: "orthogonal vectors",
			a:    []float32{1, 0},
			b:    []float32{0, 1},
			want: 0.0,
			tol:  1e-6,
		},
		{
			name: "opposite unit vectors",
			a:    []float32{1, 0},
			b:    []float32{-1, 0},
			want: -1.0,
			tol:  1e-6,
		},
		{
			name: "dimension 1",
			a:    []float32{1},
			b:    []float32{1},
			want: 1.0,
			tol:  1e-6,
		},
		{
			name: "partial similarity",
			a:    vector.Normalize([]float32{1, 1, 0}),
			b:    vector.Normalize([]float32{1, 0, 0}),
			want: 1.0 / math.Sqrt2,
			tol:  1e-5,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := vector.CosineSimilarity(tc.a, tc.b)
			if math.Abs(got-tc.want) > tc.tol {
				t.Errorf("CosineSimilarity = %v, want %v (tol %v)", got, tc.want, tc.tol)
			}
		})
	}
}

func TestCosineSimilarityRaw(t *testing.T) {
	tests := []struct {
		name string
		a, b []float32
		want float64
		tol  float64
	}{
		{
			name: "same as normalized when unit",
			a:    vector.Normalize([]float32{3, 4, 0}),
			b:    vector.Normalize([]float32{3, 4, 0}),
			want: 1.0,
			tol:  1e-5,
		},
		{
			name: "non-unit identical direction",
			a:    []float32{3, 0},
			b:    []float32{7, 0},
			want: 1.0,
			tol:  1e-6,
		},
		{
			name: "zero vector a",
			a:    []float32{0, 0, 0},
			b:    []float32{1, 0, 0},
			want: 0.0,
			tol:  1e-9,
		},
		{
			name: "zero vector b",
			a:    []float32{1, 0, 0},
			b:    []float32{0, 0, 0},
			want: 0.0,
			tol:  1e-9,
		},
		{
			name: "both zero vectors",
			a:    []float32{0, 0},
			b:    []float32{0, 0},
			want: 0.0,
			tol:  1e-9,
		},
		{
			name: "orthogonal non-unit",
			a:    []float32{5, 0},
			b:    []float32{0, 3},
			want: 0.0,
			tol:  1e-6,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := vector.CosineSimilarityRaw(tc.a, tc.b)
			if math.Abs(got-tc.want) > tc.tol {
				t.Errorf("CosineSimilarityRaw = %v, want %v (tol %v)", got, tc.want, tc.tol)
			}
		})
	}
}

func TestNormalize(t *testing.T) {
	t.Run("unit length after normalize", func(t *testing.T) {
		v := []float32{3, 4, 0}
		n := vector.Normalize(v)
		var sum float64
		for _, x := range n {
			sum += float64(x) * float64(x)
		}
		if math.Abs(sum-1.0) > 1e-6 {
			t.Errorf("||Normalize(v)|| = %v, want 1.0", math.Sqrt(sum))
		}
	})

	t.Run("zero vector unchanged", func(t *testing.T) {
		v := []float32{0, 0, 0}
		n := vector.Normalize(v)
		for i, x := range n {
			if x != 0 {
				t.Errorf("Normalize(zero)[%d] = %v, want 0", i, x)
			}
		}
	})

	t.Run("does not modify original", func(t *testing.T) {
		v := []float32{3, 4}
		orig := []float32{3, 4}
		vector.Normalize(v)
		for i := range v {
			if v[i] != orig[i] {
				t.Errorf("input modified at [%d]: got %v, want %v", i, v[i], orig[i])
			}
		}
	})

	t.Run("result is new slice", func(t *testing.T) {
		v := []float32{1, 2, 3}
		n := vector.Normalize(v)
		if &n[0] == &v[0] {
			t.Error("Normalize returned same backing array, expected new slice")
		}
	})
}
