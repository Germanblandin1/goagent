package vector

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"sync"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/internal/session"
)

// Compile-time checks.
var _ goagent.VectorStore = (*InMemoryStore)(nil)
var _ goagent.BulkVectorStore = (*InMemoryStore)(nil)

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
//
// WithScoreThreshold is applied before topK truncation.
// WithFilter matches against each message's Metadata map using deep equality:
// every key-value pair in the filter must be present in Metadata.
// Messages without metadata are excluded when a filter is active.
func (s *InMemoryStore) Search(ctx context.Context, query []float32, topK int, opts ...goagent.SearchOption) ([]goagent.ScoredMessage, error) {
	cfg := &goagent.SearchOptions{}
	for _, o := range opts {
		o(cfg)
	}

	sessionID, hasSession := session.IDFromContext(ctx)

	s.mu.RLock()
	results := make([]goagent.ScoredMessage, 0, len(s.entries))
	for id, e := range s.entries {
		if hasSession && !strings.HasPrefix(id, sessionID+":") {
			continue
		}
		score := CosineSimilarity(query, e.vector)
		if cfg.ScoreThreshold != nil && score < *cfg.ScoreThreshold {
			continue
		}
		results = append(results, goagent.ScoredMessage{
			Message: e.msg,
			Score:   score,
		})
	}
	s.mu.RUnlock()

	if len(cfg.Filter) > 0 {
		filtered := results[:0]
		for _, r := range results {
			if matchesFilter(r.Message.Metadata, cfg.Filter) {
				filtered = append(filtered, r)
			}
		}
		results = filtered
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if topK < len(results) {
		results = results[:topK]
	}
	return results, nil
}

// matchesFilter reports whether meta contains all key-value pairs in filter.
// Uses reflect.DeepEqual for value comparison to correctly handle nested types.
func matchesFilter(meta, filter map[string]any) bool {
	for k, want := range filter {
		got, ok := meta[k]
		if !ok || !reflect.DeepEqual(got, want) {
			return false
		}
	}
	return true
}

// Delete removes the entry with the given id from the store.
// It is a no-op if id does not exist.
func (s *InMemoryStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, id)
	return nil
}

// BulkUpsert stores or replaces all entries under their respective ids in a
// single lock acquisition. A copy of each vector is made to protect against
// caller mutation.
func (s *InMemoryStore) BulkUpsert(_ context.Context, entries []goagent.UpsertEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range entries {
		cp := make([]float32, len(e.Vector))
		copy(cp, e.Vector)
		s.entries[e.ID] = entry{vector: cp, msg: e.Message}
	}
	return nil
}

// BulkDelete removes all entries with the given ids in a single lock
// acquisition. IDs that do not exist are silently ignored.
func (s *InMemoryStore) BulkDelete(_ context.Context, ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range ids {
		delete(s.entries, id)
	}
	return nil
}
