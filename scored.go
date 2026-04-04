package goagent

// ScoredMessage combines a Message with the similarity score calculated by
// the VectorStore at retrieval time.
//
// Score is in [0.0, 1.0] for stores that use cosine similarity with
// normalised vectors (most modern text embedders produce unit vectors).
type ScoredMessage struct {
	Message Message
	Score   float64
}
