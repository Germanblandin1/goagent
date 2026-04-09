package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/Germanblandin1/goagent"
)

// fileLongTermMemory implements goagent.LongTermMemory by persisting messages
// as JSON Lines in a local file.
//
// # Persistence
//
// Each message is written as a single JSON object on its own line (JSON Lines
// format). The file is opened in append mode on every Store call so it is safe
// to stop and restart the process at any point without losing history.
//
// # Retrieval strategy
//
// Retrieve returns ALL stored messages on every call, ignoring topK and query.
// The agent therefore receives the complete file history as long-term context
// on each run.
//
// Trade-offs to be aware of:
//   - As the file grows the context window may be exhausted. A model with a
//     128k-token window supports roughly 50,000 words of history before
//     truncation kicks in at the provider level.
//   - No relevance filtering is applied; all past exchanges are always included.
//     This maximises recall at the cost of precision.
//   - For large histories consider replacing Retrieve with a similarity-based
//     strategy (e.g. Levenshtein, TF-IDF, or a vector store).
//
// # Multimodal content and file size
//
// Go's encoding/json serializes []byte fields as base64-encoded strings.
// This means that every image or document stored in a ContentBlock inflates
// the file by roughly 33 %: a 5 MB JPEG becomes ~6.7 MB of base64 text on
// disk. Because loadAll reads and decodes the entire file on every Retrieve
// call, the I/O cost grows linearly with the number of stored messages:
//
//	10 × 5 MB images  → ~67 MB read from disk on every Run
//	100 × 5 MB images → ~670 MB read from disk on every Run
//
// This implementation is intentionally simple and suited to text-only
// conversations. Do not use it as a long-term store for multimodal history.
// For workloads that include images or documents, store the binary payloads
// externally (e.g. on disk or object storage) and keep only a reference URI
// in the JSON record, or switch to a database-backed store that handles
// BLOBs efficiently.
//
// # Write gating
//
// fileLongTermMemory does not decide what to store — that responsibility belongs
// to the WritePolicy passed via goagent.WithWritePolicy. In this example the
// policy is implemented by a stateless judge agent (see judge.go) that evaluates
// each turn and returns a structured JSON verdict before the framework calls Store.
type fileLongTermMemory struct {
	mu   sync.RWMutex
	path string
}

func newFileLongTermMemory(path string) *fileLongTermMemory {
	return &fileLongTermMemory{path: path}
}

// Store appends msgs to the file, one JSON object per line.
// It is only called when the WritePolicy has already approved the turn.
func (m *fileLongTermMemory) Store(_ context.Context, msgs ...goagent.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	f, err := os.OpenFile(m.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("filemem: open %s: %w", m.path, err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	for _, msg := range msgs {
		if err := enc.Encode(msg); err != nil {
			return fmt.Errorf("filemem: encode message: %w", err)
		}
	}
	return nil
}

// Retrieve returns all persisted messages regardless of query or topK.
// Scores are always 0.0 — this store does not compute similarity.
// See the type-level doc comment for an explanation of this design choice.
func (m *fileLongTermMemory) Retrieve(_ context.Context, _ []goagent.ContentBlock, _ int, _ ...goagent.SearchOption) ([]goagent.ScoredMessage, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	msgs, err := m.loadAll()
	if err != nil {
		return nil, err
	}
	scored := make([]goagent.ScoredMessage, len(msgs))
	for i, msg := range msgs {
		scored[i] = goagent.ScoredMessage{Message: msg}
	}
	return scored, nil
}

// loadAll reads every JSON Line from the file and returns the messages.
// Returns nil, nil when the file does not exist yet.
// Must be called with at least a read lock held.
func (m *fileLongTermMemory) loadAll() ([]goagent.Message, error) {
	f, err := os.Open(m.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("filemem: open %s: %w", m.path, err)
	}
	defer f.Close()

	var msgs []goagent.Message
	dec := json.NewDecoder(f)
	for dec.More() {
		var msg goagent.Message
		if err := dec.Decode(&msg); err != nil {
			return nil, fmt.Errorf("filemem: decode message: %w", err)
		}
		msgs = append(msgs, msg)
	}
	return msgs, nil
}
