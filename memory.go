package goagent

import (
	"context"
	"strings"
	"time"
)

// ShortTermMemory manages the active conversation history for an Agent.
// It is used to maintain context across multiple Run calls within a session.
// Implementations must be safe for concurrent use.
type ShortTermMemory interface {
	// Messages returns the messages to include in the next provider request,
	// with the configured read policy applied (e.g. FixedWindow, TokenWindow).
	// The returned slice is a defensive copy; callers may modify it freely.
	Messages(ctx context.Context) ([]Message, error)

	// Append adds msgs to the store in the order provided.
	// Filtering occurs at read time (Messages), never at write time.
	Append(ctx context.Context, msgs ...Message) error
}

// LongTermMemory stores and retrieves messages across sessions by semantic
// relevance. Unlike ShortTermMemory, retrieval is similarity-based, not
// positional — the store may contain thousands of messages but only the most
// relevant ones are surfaced on each Run.
// Implementations must be safe for concurrent use.
type LongTermMemory interface {
	// Store persists msgs for future retrieval.
	Store(ctx context.Context, msgs ...Message) error

	// Retrieve returns the topK messages most semantically similar to the
	// given content, each paired with its similarity score.
	//
	// The full []ContentBlock is passed so that embedder implementations
	// that support vision or documents can build a meaningful query vector
	// even when the prompt contains no text.
	//
	// Score in each ScoredMessage is the cosine similarity in [0.0, 1.0]
	// as computed by the underlying VectorStore.
	//
	// opts are forwarded to the underlying VectorStore.Search call.
	Retrieve(ctx context.Context, query []ContentBlock, topK int, opts ...SearchOption) ([]ScoredMessage, error)
}

// WritePolicy decides what to persist after a completed turn. It is called
// once per Run after the final answer is produced.
//
// Returning nil discards the turn — nothing is written to long-term memory.
// Returning a non-nil slice (even an empty one) stores exactly those messages.
//
// This design lets policies both filter and transform: a policy may return the
// original user+assistant pair unchanged (like StoreAlways), a condensed single
// message (like a summarising judge agent), or any custom set of messages.
//
// prompt is the user Message that opened the turn (may contain images,
// documents, or other multimodal blocks). response is the final assistant
// Message. Both are passed in full so policies can inspect or forward binary
// content without losing it.
type WritePolicy func(prompt, response Message) []Message

// StoreAlways is a WritePolicy that persists every turn as the original
// user+assistant message pair. It is the default when WithLongTermMemory is
// configured without an explicit WritePolicy.
var StoreAlways WritePolicy = func(p, r Message) []Message {
	return []Message{p, r}
}

// MinLength returns a WritePolicy that stores the original user+assistant pair
// only when the combined character count of their text content exceeds n.
// Returns nil (discard) when the combined length is n or fewer characters.
// Useful for filtering out trivial exchanges ("ok", "gracias", "seguí") that
// add noise to the long-term store without carrying durable information.
func MinLength(n int) WritePolicy {
	return func(p, r Message) []Message {
		if len(p.TextContent())+len(r.TextContent()) <= n {
			return nil
		}
		return []Message{p, r}
	}
}

// SearchOptions holds the resolved configuration for a single Search call.
// Fields at their zero value impose no constraint.
// Implementations that do not support a given field must silently ignore it.
type SearchOptions struct {
	// ScoreThreshold, when non-nil, causes Search to discard results whose
	// similarity score is strictly below this value. The score convention
	// follows ScoredMessage.Score for the underlying store.
	ScoreThreshold *float64

	// Filter restricts results to entries whose metadata contains all the
	// key–value pairs in this map. Support is implementation-defined:
	// implementations that do not support metadata filtering silently ignore it.
	Filter map[string]any

	// TokenBudget, when positive, caps the total estimated token cost of the
	// results returned by [LongTermMemory.Retrieve]. Results are evaluated in
	// score-descending order; once a result would exceed the remaining budget,
	// that result and all subsequent ones are dropped.
	// TokenEstimator must be non-nil when TokenBudget > 0; otherwise the
	// option has no effect.
	// This field is consumed by [LongTermMemory] and is invisible to
	// [VectorStore] implementations.
	TokenBudget int

	// TokenEstimator is called once per result with the concatenated text of
	// that message's content blocks to determine its token cost.
	// Provide e.g. est.Measure for any memory/vector.SizeEstimator.
	TokenEstimator func(ctx context.Context, text string) int
}

// SearchOption configures an individual Search call.
type SearchOption func(*SearchOptions)

// WithScoreThreshold returns a SearchOption that discards results whose
// similarity score is strictly below min.
func WithScoreThreshold(min float64) SearchOption {
	return func(o *SearchOptions) { o.ScoreThreshold = &min }
}

// WithFilter returns a SearchOption that restricts search results to entries
// whose metadata contains all key–value pairs in f.
// Support is implementation-defined; implementations that do not support
// metadata filtering silently ignore this option.
func WithFilter(f map[string]any) SearchOption {
	return func(o *SearchOptions) { o.Filter = f }
}

// WithTokenBudget returns a SearchOption that limits the total token cost of
// results returned by [LongTermMemory.Retrieve]. Results are already ordered
// by descending similarity score; the loop stops as soon as the next result
// would exceed the remaining budget.
//
// estimator is called once per result with the concatenated text content of
// that message. Pass e.g. est.Measure for any memory/vector.SizeEstimator.
// If estimator is nil or budget ≤ 0 the option has no effect.
//
// This option is a no-op when passed directly to [VectorStore.Search];
// it is only applied by [LongTermMemory.Retrieve].
func WithTokenBudget(budget int, estimator func(ctx context.Context, text string) int) SearchOption {
	return func(o *SearchOptions) {
		o.TokenBudget = budget
		o.TokenEstimator = estimator
	}
}

// VectorStore stores (message, embedding) pairs and supports similarity search.
// This module does not ship a VectorStore implementation; the caller must
// supply one (e.g. a pgvector client, a Chroma adapter, or an in-process
// approximate nearest-neighbour store).
type VectorStore interface {
	// Upsert stores or updates a message with its embedding vector.
	// id must be a stable identifier for the message (e.g. content hash).
	Upsert(ctx context.Context, id string, vector []float32, msg Message) error

	// Search returns the topK messages most similar to the given vector,
	// each paired with its similarity score. Score is in [0.0, 1.0] for
	// stores that use cosine similarity with normalised vectors.
	// opts may constrain results (e.g. WithScoreThreshold, WithFilter).
	Search(ctx context.Context, vector []float32, topK int, opts ...SearchOption) ([]ScoredMessage, error)

	// Delete removes the entry with the given id from the store.
	// It is a no-op if id does not exist.
	Delete(ctx context.Context, id string) error
}

// UpsertEntry holds the data for a single [BulkVectorStore.BulkUpsert] element.
type UpsertEntry struct {
	ID      string
	Vector  []float32
	Message Message
}

// BulkVectorStore extends [VectorStore] with batch operations for stores that
// support efficient multi-row writes. Callers type-assert to BulkVectorStore
// and fall back to individual [VectorStore.Upsert] / [VectorStore.Delete]
// calls when the store does not implement it.
//
// Implementations that do implement BulkVectorStore should ensure that a single
// BulkUpsert or BulkDelete call is cheaper than an equivalent number of
// individual Upsert / Delete calls (e.g. by using a database transaction or a
// native batch RPC).
type BulkVectorStore interface {
	VectorStore

	// BulkUpsert stores or updates all entries in a single batch.
	// The operation is idempotent; when entries contains duplicate IDs the
	// last occurrence wins (same behaviour as repeated Upsert calls).
	BulkUpsert(ctx context.Context, entries []UpsertEntry) error

	// BulkDelete removes all entries with the given ids in a single batch.
	// IDs that do not exist are silently ignored.
	BulkDelete(ctx context.Context, ids []string) error
}

// Embedder converts message content into a dense vector representation
// suitable for semantic similarity search. Implementations receive the full
// []ContentBlock so they can handle text, image, and document blocks natively
// — for example, by routing ContentImage blocks to a vision embedding model.
//
// The vector for a given content slice must be consistent across calls
// (deterministic given the same model and input).
//
// Use TextFrom to extract and concatenate all text blocks in implementations
// that only support text.
type Embedder interface {
	Embed(ctx context.Context, content []ContentBlock) ([]float32, error)
}

// BatchEmbedder is an optional extension of Embedder that converts multiple
// content slices to vectors in a single API call. Store uses it when available
// to collapse N×K individual Embed calls into one round trip — a significant
// win for remote embedding APIs (OpenAI, Voyage, Cohere) where HTTP overhead
// dominates latency.
//
// Implementations must return a slice of exactly len(inputs) vectors.
// A nil vector at index i signals that the input had no embeddable content
// (equivalent to a single Embed returning ErrNoEmbeddeableContent for that slot).
// A non-nil error aborts the entire batch.
//
// This mirrors the BulkVectorStore pattern: declare the interface, and
// longTermMemory.Store will detect it at runtime via a type assertion.
type BatchEmbedder interface {
	Embedder
	BatchEmbed(ctx context.Context, inputs [][]ContentBlock) ([][]float32, error)
}

// VectorStoreObserver holds optional callbacks fired after each [VectorStore]
// operation completes. All fields are optional; nil callbacks are silently
// skipped. Callbacks are invoked synchronously after the operation returns, so
// heavy work (e.g. writing to an external metrics system) should spawn a
// goroutine.
//
// Use [NewObservableStore] to wrap a [VectorStore] with these callbacks.
// Use [MergeVectorStoreObservers] to compose multiple observers.
type VectorStoreObserver struct {
	// OnUpsert is called after every [VectorStore.Upsert] with the entry id,
	// elapsed duration, and any error returned by the inner store.
	OnUpsert func(ctx context.Context, id string, dur time.Duration, err error)

	// OnSearch is called after every [VectorStore.Search] with the requested
	// topK, the number of results actually returned, elapsed duration, and
	// any error.
	OnSearch func(ctx context.Context, topK int, results int, dur time.Duration, err error)

	// OnDelete is called after every [VectorStore.Delete] with the entry id,
	// elapsed duration, and any error.
	OnDelete func(ctx context.Context, id string, dur time.Duration, err error)

	// OnBulkUpsert is called after every [BulkVectorStore.BulkUpsert] with the
	// number of entries in the batch, elapsed duration, and any error.
	// It is only invoked when the inner store implements [BulkVectorStore].
	OnBulkUpsert func(ctx context.Context, count int, dur time.Duration, err error)

	// OnBulkDelete is called after every [BulkVectorStore.BulkDelete] with the
	// number of ids in the batch, elapsed duration, and any error.
	// It is only invoked when the inner store implements [BulkVectorStore].
	OnBulkDelete func(ctx context.Context, count int, dur time.Duration, err error)
}

// NewObservableStore wraps inner with the provided observer callbacks.
// Every [VectorStore] method fires the corresponding callback after the inner
// call returns, passing the elapsed duration and any error.
//
// If inner also implements [BulkVectorStore], the returned value implements it
// too: [BulkVectorStore.BulkUpsert] and [BulkVectorStore.BulkDelete] each fire
// their respective callbacks. The RAG pipeline detects [BulkVectorStore] via a
// type assertion, so the wrapper is transparent to it.
func NewObservableStore(inner VectorStore, obs VectorStoreObserver) VectorStore {
	base := &observableStore{inner: inner, obs: obs}
	if bulk, ok := inner.(BulkVectorStore); ok {
		return &observableBulkStore{observableStore: base, bulk: bulk}
	}
	return base
}

// MergeVectorStoreObservers returns a [VectorStoreObserver] that calls each
// provided observer in order. Nil-field observers in the input are silently
// ignored; when no input has a callback for a field, that field remains nil,
// preserving zero-value semantics.
//
//	obs := goagent.MergeVectorStoreObservers(logObserver, otelObserver)
//	store := goagent.NewObservableStore(rawStore, obs)
func MergeVectorStoreObservers(observers ...VectorStoreObserver) VectorStoreObserver {
	if len(observers) == 0 {
		return VectorStoreObserver{}
	}
	if len(observers) == 1 {
		return observers[0]
	}

	anyObsHas := func(check func(*VectorStoreObserver) bool) bool {
		for i := range observers {
			if check(&observers[i]) {
				return true
			}
		}
		return false
	}

	var merged VectorStoreObserver

	if anyObsHas(func(o *VectorStoreObserver) bool { return o.OnUpsert != nil }) {
		merged.OnUpsert = func(ctx context.Context, id string, dur time.Duration, err error) {
			for i := range observers {
				if fn := observers[i].OnUpsert; fn != nil {
					fn(ctx, id, dur, err)
				}
			}
		}
	}

	if anyObsHas(func(o *VectorStoreObserver) bool { return o.OnSearch != nil }) {
		merged.OnSearch = func(ctx context.Context, topK int, results int, dur time.Duration, err error) {
			for i := range observers {
				if fn := observers[i].OnSearch; fn != nil {
					fn(ctx, topK, results, dur, err)
				}
			}
		}
	}

	if anyObsHas(func(o *VectorStoreObserver) bool { return o.OnDelete != nil }) {
		merged.OnDelete = func(ctx context.Context, id string, dur time.Duration, err error) {
			for i := range observers {
				if fn := observers[i].OnDelete; fn != nil {
					fn(ctx, id, dur, err)
				}
			}
		}
	}

	if anyObsHas(func(o *VectorStoreObserver) bool { return o.OnBulkUpsert != nil }) {
		merged.OnBulkUpsert = func(ctx context.Context, count int, dur time.Duration, err error) {
			for i := range observers {
				if fn := observers[i].OnBulkUpsert; fn != nil {
					fn(ctx, count, dur, err)
				}
			}
		}
	}

	if anyObsHas(func(o *VectorStoreObserver) bool { return o.OnBulkDelete != nil }) {
		merged.OnBulkDelete = func(ctx context.Context, count int, dur time.Duration, err error) {
			for i := range observers {
				if fn := observers[i].OnBulkDelete; fn != nil {
					fn(ctx, count, dur, err)
				}
			}
		}
	}

	return merged
}

// observableStore wraps a [VectorStore] and fires [VectorStoreObserver]
// callbacks after each operation.
type observableStore struct {
	inner VectorStore
	obs   VectorStoreObserver
}

func (s *observableStore) Upsert(ctx context.Context, id string, vector []float32, msg Message) error {
	start := time.Now()
	err := s.inner.Upsert(ctx, id, vector, msg)
	if fn := s.obs.OnUpsert; fn != nil {
		fn(ctx, id, time.Since(start), err)
	}
	return err
}

func (s *observableStore) Search(ctx context.Context, vector []float32, topK int, opts ...SearchOption) ([]ScoredMessage, error) {
	start := time.Now()
	results, err := s.inner.Search(ctx, vector, topK, opts...)
	if fn := s.obs.OnSearch; fn != nil {
		fn(ctx, topK, len(results), time.Since(start), err)
	}
	return results, err
}

func (s *observableStore) Delete(ctx context.Context, id string) error {
	start := time.Now()
	err := s.inner.Delete(ctx, id)
	if fn := s.obs.OnDelete; fn != nil {
		fn(ctx, id, time.Since(start), err)
	}
	return err
}

// observableBulkStore extends [observableStore] with [BulkVectorStore] support.
type observableBulkStore struct {
	*observableStore
	bulk BulkVectorStore
}

func (s *observableBulkStore) BulkUpsert(ctx context.Context, entries []UpsertEntry) error {
	start := time.Now()
	err := s.bulk.BulkUpsert(ctx, entries)
	if fn := s.obs.OnBulkUpsert; fn != nil {
		fn(ctx, len(entries), time.Since(start), err)
	}
	return err
}

func (s *observableBulkStore) BulkDelete(ctx context.Context, ids []string) error {
	start := time.Now()
	err := s.bulk.BulkDelete(ctx, ids)
	if fn := s.obs.OnBulkDelete; fn != nil {
		fn(ctx, len(ids), time.Since(start), err)
	}
	return err
}

// TextFrom extracts and concatenates the text from a slice of ContentBlocks.
// Non-text blocks are ignored. Adjacent text values are separated by a space.
//
// Intended as a convenience for Embedder implementations that only handle
// text:
//
//	func (e *myEmbedder) Embed(ctx context.Context, content []goagent.ContentBlock) ([]float32, error) {
//	    return e.client.Embed(ctx, goagent.TextFrom(content))
//	}
func TextFrom(blocks []ContentBlock) string {
	var b strings.Builder
	for _, block := range blocks {
		if block.Type == ContentText {
			if b.Len() > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(block.Text)
		}
	}
	return b.String()
}
