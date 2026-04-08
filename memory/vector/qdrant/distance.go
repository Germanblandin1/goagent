package qdrant

import (
	"github.com/qdrant/go-client/qdrant"
)

// DistanceFunc defines the vector distance measure used for similarity search.
// It must match the distance configured in the Qdrant collection — a mismatch
// causes incorrect score normalization.
//
// The three built-in values (Cosine, Euclid, Dot) cover all distances
// supported by Qdrant. For custom measures use NewDistanceFunc.
type DistanceFunc struct {
	distance       qdrant.Distance
	normalizeScore func(float64) float64
}

// NewDistanceFunc constructs a DistanceFunc for a custom or future Qdrant
// distance. d is the Qdrant proto Distance enum value, and normalize converts
// the raw Qdrant score to a similarity value where higher means more similar.
func NewDistanceFunc(d qdrant.Distance, normalize func(float64) float64) DistanceFunc {
	return DistanceFunc{distance: d, normalizeScore: normalize}
}

var (
	// Cosine uses cosine similarity.
	// Qdrant returns a pre-computed similarity value — not a distance —
	// so no inversion is needed. For normalized vectors the range is [0, 1].
	// Recommended for most text embedding models.
	Cosine = DistanceFunc{
		distance:       qdrant.Distance_Cosine,
		normalizeScore: func(s float64) float64 { return s },
	}

	// Euclid uses Euclidean (L2) distance.
	// Qdrant returns the raw non-negative distance value.
	// Score mapping: 1 / (1 + distance) → (0, 1].
	// Use when the model produces non-normalised vectors.
	Euclid = DistanceFunc{
		distance:       qdrant.Distance_Euclid,
		normalizeScore: func(s float64) float64 { return 1.0 / (1.0 + s) },
	}

	// Dot uses dot product similarity.
	// Qdrant returns the raw dot product (positive for normalised vectors).
	// Equivalent to Cosine for unit-normalised vectors; marginally faster on
	// some hardware.
	Dot = DistanceFunc{
		distance:       qdrant.Distance_Dot,
		normalizeScore: func(s float64) float64 { return s },
	}
)
