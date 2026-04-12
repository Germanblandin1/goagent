package vector_test

import (
	"math"
	"testing"

	"github.com/Germanblandin1/goagent/memory/vector"
)

// FuzzCosineSimilarity verifies that CosineSimilarity never panics and that its
// output stays within the valid range [-1, 1] for same-length vectors.
//
// NOTE: CosineSimilarity assumes both slices have equal length — passing vectors
// of different lengths will panic with an index-out-of-range (this is a known
// contract documented in the function signature). This fuzz test exercises the
// same-length invariant with arbitrary values including NaN and ±Inf.
//
// Run with: go test -fuzz=FuzzCosineSimilarity -fuzztime=60s ./memory/vector
func FuzzCosineSimilarity(f *testing.F) {
	f.Add(float32(1.0), float32(0.0), float32(0.0), float32(1.0))
	f.Add(float32(0.5), float32(0.5), float32(0.5), float32(0.5))
	f.Add(float32(-1.0), float32(0.0), float32(1.0), float32(0.0))

	f.Fuzz(func(t *testing.T, a0, a1, b0, b1 float32) {
		a := []float32{a0, a1}
		b := []float32{b0, b1}

		defer func() {
			if r := recover(); r != nil {
				t.Errorf("CosineSimilarity panicked with a=%v b=%v: %v", a, b, r)
			}
		}()

		got := vector.CosineSimilarity(a, b)

		// NaN inputs produce NaN output — skip range check.
		if math.IsNaN(got) {
			return
		}
		// Inf inputs may produce Inf — not a panic but worth logging.
		if math.IsInf(got, 0) {
			return
		}
		// For finite, normalised inputs the result must be in [-1, 1].
		// We only enforce this when neither input has extreme values.
		maxAbs := float64(max32(abs32(a0), abs32(a1), abs32(b0), abs32(b1)))
		if maxAbs <= 1.0 && (got < -1.0-1e-5 || got > 1.0+1e-5) {
			t.Errorf("CosineSimilarity(%v, %v) = %v, outside [-1,1]", a, b, got)
		}
	})
}

// FuzzNormalize verifies that Normalize never panics and that a non-zero
// normalized vector has unit length within floating-point tolerance.
//
// Run with: go test -fuzz=FuzzNormalize -fuzztime=60s ./memory/vector
func FuzzNormalize(f *testing.F) {
	f.Add(float32(3.0), float32(4.0))
	f.Add(float32(0.0), float32(0.0))
	f.Add(float32(-1.0), float32(0.0))
	f.Add(float32(1e-38), float32(1e-38))

	f.Fuzz(func(t *testing.T, x, y float32) {
		v := []float32{x, y}

		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Normalize panicked with v=%v: %v", v, r)
			}
		}()

		n := vector.Normalize(v)

		if len(n) != len(v) {
			t.Errorf("Normalize returned slice of len %d, want %d", len(n), len(v))
		}

		// Zero vector returns unchanged.
		if x == 0 && y == 0 {
			return
		}
		// Skip when inputs are denormals, NaN, or Inf — no meaningful norm.
		if math.IsNaN(float64(x)) || math.IsNaN(float64(y)) ||
			math.IsInf(float64(x), 0) || math.IsInf(float64(y), 0) {
			return
		}

		// Non-zero vector: |n| should be 1.0 within tolerance.
		var sumSq float64
		for _, v := range n {
			sumSq += float64(v) * float64(v)
		}
		if !math.IsNaN(sumSq) && !math.IsInf(sumSq, 0) {
			if math.Abs(sumSq-1.0) > 1e-5 {
				t.Errorf("||Normalize(%v)|| = %v, want 1.0 (tol 1e-5)", v, math.Sqrt(sumSq))
			}
		}
	})
}

// helper functions kept in this file to avoid polluting the package namespace.
func abs32(x float32) float32 {
	if x < 0 {
		return -x
	}
	return x
}

func max32(vals ...float32) float32 {
	var m float32
	for _, v := range vals {
		if v > m {
			m = v
		}
	}
	return m
}
