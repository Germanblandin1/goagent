package rag

// EvalCase represents an evaluation case for retrieval metrics.
// Retrieved holds the document IDs returned by the pipeline, ordered by
// descending relevance. Relevant holds the ground-truth relevant IDs — order
// does not matter. IDs must be consistent: use SearchResult.Source as the ID.
type EvalCase struct {
	Query     string
	Retrieved []string // ordered by descending relevance
	Relevant  []string // ground-truth IDs — order does not matter
}

// PrecisionAtK calculates the fraction of the top-K retrieved documents that
// are relevant according to the ground truth.
//
// Answers: "How clean is what I retrieve?"
// High precision → low noise in the context that reaches the LLM.
//
// If k > len(retrieved), len(retrieved) is used.
// Returns 0 if retrieved is empty or k <= 0.
//
// Example:
//
//	retrieved := []string{"doc1", "doc2", "doc3", "doc4", "doc9"}
//	relevant  := []string{"doc1", "doc3", "doc7"}
//
//	PrecisionAtK(retrieved, relevant, 5) // → 0.40  (2 hits out of 5)
//	PrecisionAtK(retrieved, relevant, 3) // → 0.67  (2 hits out of 3)
func PrecisionAtK(retrieved, relevant []string, k int) float64 {
	if k <= 0 || len(retrieved) == 0 {
		return 0
	}
	if k > len(retrieved) {
		k = len(retrieved)
	}
	retrieved = retrieved[:k]

	relevantSet := make(map[string]struct{}, len(relevant))
	for _, id := range relevant {
		relevantSet[id] = struct{}{}
	}

	hits := 0
	for _, id := range retrieved {
		if _, ok := relevantSet[id]; ok {
			hits++
		}
	}
	return float64(hits) / float64(k)
}

// RecallAtK calculates the fraction of all relevant documents that were
// retrieved within the top-K results.
//
// Answers: "How much am I missing?"
// Low recall → relevant information the LLM will never see.
//
// Returns 0 if relevant is empty.
//
// Example:
//
//	retrieved := []string{"doc1", "doc2", "doc3", "doc4", "doc9"}
//	relevant  := []string{"doc1", "doc3", "doc7"}
//
//	RecallAtK(retrieved, relevant, 5) // → 0.67  (2 out of 3 relevant)
//	RecallAtK(retrieved, relevant, 1) // → 0.33  (1 out of 3 relevant)
func RecallAtK(retrieved, relevant []string, k int) float64 {
	if len(relevant) == 0 {
		return 0
	}
	if k > len(retrieved) {
		k = len(retrieved)
	}
	if k <= 0 {
		return 0
	}
	retrieved = retrieved[:k]

	relevantSet := make(map[string]struct{}, len(relevant))
	for _, id := range relevant {
		relevantSet[id] = struct{}{}
	}

	hits := 0
	for _, id := range retrieved {
		if _, ok := relevantSet[id]; ok {
			hits++
		}
	}
	return float64(hits) / float64(len(relevant))
}

// ReciprocalRank returns 1/position of the first relevant document in retrieved.
// Returns 0 if no relevant document appears in retrieved.
// Position is 1-based: the first element has position 1.
//
// Answers: "Does the most useful result appear near the top?"
//
// Example:
//
//	retrieved := []string{"doc2", "doc5", "doc1"}
//	relevant  := []string{"doc1", "doc3"}
//
//	ReciprocalRank(retrieved, relevant) // → 0.33  (doc1 is at position 3)
func ReciprocalRank(retrieved, relevant []string) float64 {
	relevantSet := make(map[string]struct{}, len(relevant))
	for _, id := range relevant {
		relevantSet[id] = struct{}{}
	}
	for i, id := range retrieved {
		if _, ok := relevantSet[id]; ok {
			return 1.0 / float64(i+1)
		}
	}
	return 0
}

// MRR calculates the Mean Reciprocal Rank across multiple evaluation cases.
// It is the average of ReciprocalRank for each case.
//
// Returns 0 if cases is empty.
//
// Interpretation:
//
//	MRR = 1.00 → the first result is always relevant
//	MRR = 0.50 → the first relevant result is at position 2 on average
//	MRR = 0.33 → the first relevant result is at position 3 on average
func MRR(cases []EvalCase) float64 {
	if len(cases) == 0 {
		return 0
	}
	sum := 0.0
	for _, c := range cases {
		sum += ReciprocalRank(c.Retrieved, c.Relevant)
	}
	return sum / float64(len(cases))
}
