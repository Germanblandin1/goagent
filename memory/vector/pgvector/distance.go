package pgvector

import "fmt"

// DistanceFunc defines the vector distance operator used for similarity search
// and the corresponding HNSW operator class for index creation.
//
// The DistanceFunc passed to New and the operator class used in Migrate must
// match — a mismatch causes pgvector to skip the index and fall back to a
// sequential scan.
//
// The three built-in values (Cosine, L2, InnerProduct) cover all operators
// supported by pgvector. For custom or future operators use NewDistanceFunc.
type DistanceFunc struct {
	operator string // SQL infix operator: <=>, <->, <#>
	opsClass string // HNSW operator class
}

// NewDistanceFunc constructs a DistanceFunc for a custom or future pgvector
// operator. operator is the SQL infix operator (e.g. "<=>") and opsClass is
// the HNSW operator class (e.g. "vector_cosine_ops").
//
// Use the built-in values (Cosine, L2, InnerProduct) for standard pgvector
// operators.
func NewDistanceFunc(operator, opsClass string) DistanceFunc {
	return DistanceFunc{operator: operator, opsClass: opsClass}
}

var (
	// Cosine uses cosine distance (<=>).
	// Score is 1 - distance, in [0, 1] for normalised vectors.
	// Recommended for most text embedding models.
	// HNSW operator class: vector_cosine_ops.
	Cosine = DistanceFunc{"<=>", "vector_cosine_ops"}

	// L2 uses Euclidean distance (<->).
	// Score is 1 / (1 + distance), in (0, 1].
	// Use when the model produces non-normalised vectors.
	// HNSW operator class: vector_l2_ops.
	L2 = DistanceFunc{"<->", "vector_l2_ops"}

	// InnerProduct uses negative inner product (<#>).
	// Score is the inner product (negated back to positive).
	// Equivalent to cosine similarity for normalised vectors;
	// marginally faster on some hardware.
	// HNSW operator class: vector_ip_ops.
	InnerProduct = DistanceFunc{"<#>", "vector_ip_ops"}
)

// orderExpr returns the expression for ORDER BY.
// col is the vector column name, ph is the query placeholder ($1, $2, etc.).
func (d DistanceFunc) orderExpr(col, ph string) string {
	return fmt.Sprintf("%s %s %s::vector", col, d.operator, ph)
}

// scoreExpr returns the SELECT expression for the similarity score.
// Scores are consistent across operators: higher always means more similar.
//
//	Cosine:       1 - (col <=> ph::vector)           → [0, 1]
//	L2:           1.0 / (1.0 + (col <-> ph::vector)) → (0, 1]
//	InnerProduct: -(col <#> ph::vector)               → positive for normalised vectors
func (d DistanceFunc) scoreExpr(col, ph string) string {
	dist := fmt.Sprintf("%s %s %s::vector", col, d.operator, ph)
	switch d.operator {
	case "<->":
		return fmt.Sprintf("1.0 / (1.0 + (%s))", dist)
	case "<#>":
		return fmt.Sprintf("-(%s)", dist)
	default: // "<=>" and any custom operator via NewDistanceFunc
		return fmt.Sprintf("1 - (%s)", dist)
	}
}
