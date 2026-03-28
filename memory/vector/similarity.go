package vector

import "math"

// CosineSimilarity computes the cosine similarity between two normalized vectors.
// Both vectors must have the same length. For unit-length vectors this is
// equivalent to the dot product — O(n), no sqrt required.
// Returns values in the range [-1, 1].
func CosineSimilarity(a, b []float32) float64 {
	var dot float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
	}
	return dot
}

// CosineSimilarityRaw computes cosine similarity for vectors that are NOT
// unit-length. It computes norms in the same pass as the dot product.
// Returns 0 if either vector is the zero vector.
func CosineSimilarityRaw(a, b []float32) float64 {
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

// Normalize scales v to unit length (L2 norm = 1.0).
// Returns a new slice — the input is not modified.
// If v is the zero vector, returns v unchanged (a zero vector has no direction).
// Most modern embedding models (nomic-embed-text, voyage-3) already return
// unit-length vectors; call Normalize only when the model does not.
func Normalize(v []float32) []float32 {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	if sum == 0 {
		return v
	}
	norm := float32(math.Sqrt(sum))
	result := make([]float32, len(v))
	for i, x := range v {
		result[i] = x / norm
	}
	return result
}
