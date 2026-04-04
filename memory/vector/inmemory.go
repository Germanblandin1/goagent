package vector

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/internal/session"
)

// Compile-time check.
var _ goagent.VectorStore = (*InMemoryStore)(nil)

type entry struct {
	vector []float32
	msg    goagent.Message
}

// InMemoryStore implements goagent.VectorStore using an in-process slice
// protected by sync.RWMutex. Search is O(n) — suitable for development,
// testing, and deployments with fewer than ~10,000 entries.
// For production at larger scale use a dedicated vector database.
type InMemoryStore struct {
	mu      sync.RWMutex
	entries map[string]entry
}

// NewInMemoryStore creates an empty InMemoryStore.
func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{entries: make(map[string]entry)}
}

// Upsert stores or replaces the message and its embedding under id.
// The operation is idempotent: calling Upsert twice with the same id
// replaces the first entry with the second.
// A copy of the vector is made to protect against caller mutation.
func (s *InMemoryStore) Upsert(_ context.Context, id string, vec []float32, msg goagent.Message) error {
	cp := make([]float32, len(vec))
	copy(cp, vec)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[id] = entry{vector: cp, msg: msg}
	return nil
}

// Search returns the topK messages most similar to the query vector,
// ordered by cosine similarity descending, each paired with its cosine
// similarity score. If the context carries a session ID (set via
// WithSessionID), only entries whose id starts with "sessionID:" are
// considered. Returns fewer than topK results when the store contains
// fewer matching entries.
func (s *InMemoryStore) Search(ctx context.Context, query []float32, topK int) ([]goagent.ScoredMessage, error) {
	sessionID, hasSession := session.IDFromContext(ctx)

	s.mu.RLock()
	results := make([]goagent.ScoredMessage, 0, len(s.entries))
	for id, e := range s.entries {
		if hasSession && !strings.HasPrefix(id, sessionID+":") {
			continue
		}
		results = append(results, goagent.ScoredMessage{
			Message: e.msg,
			Score:   CosineSimilarity(query, e.vector),
		})
	}
	s.mu.RUnlock()

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if topK < len(results) {
		results = results[:topK]
	}
	return results, nil
}
