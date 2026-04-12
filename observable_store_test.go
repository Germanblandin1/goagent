package goagent_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Germanblandin1/goagent"
)

// ── Minimal VectorStore mocks ────────────────────────────────────────────────

type stubVectorStore struct {
	upsertErr error
	searchErr error
	deleteErr error
	results   []goagent.ScoredMessage
}

func (s *stubVectorStore) Upsert(_ context.Context, _ string, _ []float32, _ goagent.Message) error {
	return s.upsertErr
}

func (s *stubVectorStore) Search(_ context.Context, _ []float32, _ int, _ ...goagent.SearchOption) ([]goagent.ScoredMessage, error) {
	return s.results, s.searchErr
}

func (s *stubVectorStore) Delete(_ context.Context, _ string) error {
	return s.deleteErr
}

// stubBulkVectorStore extends stubVectorStore with BulkVectorStore support.
type stubBulkVectorStore struct {
	stubVectorStore
	bulkUpsertErr error
	bulkDeleteErr error
}

func (s *stubBulkVectorStore) BulkUpsert(_ context.Context, _ []goagent.UpsertEntry) error {
	return s.bulkUpsertErr
}

func (s *stubBulkVectorStore) BulkDelete(_ context.Context, _ []string) error {
	return s.bulkDeleteErr
}

// ── NewObservableStore tests ─────────────────────────────────────────────────

func TestNewObservableStore_CallbacksFire(t *testing.T) {
	t.Parallel()

	inner := &stubVectorStore{
		results: []goagent.ScoredMessage{
			{Message: goagent.UserMessage("hello"), Score: 0.9},
		},
	}

	var upsertCalled, searchCalled, deleteCalled atomic.Bool

	obs := goagent.VectorStoreObserver{
		OnUpsert: func(_ context.Context, id string, dur time.Duration, err error) {
			upsertCalled.Store(true)
			if id != "id1" {
				t.Errorf("OnUpsert id = %q, want id1", id)
			}
			if err != nil {
				t.Errorf("OnUpsert err = %v, want nil", err)
			}
			if dur < 0 {
				t.Errorf("OnUpsert dur = %v, want >= 0", dur)
			}
		},
		OnSearch: func(_ context.Context, topK int, results int, dur time.Duration, err error) {
			searchCalled.Store(true)
			if topK != 3 {
				t.Errorf("OnSearch topK = %d, want 3", topK)
			}
			if results != 1 {
				t.Errorf("OnSearch results = %d, want 1", results)
			}
			if err != nil {
				t.Errorf("OnSearch err = %v, want nil", err)
			}
		},
		OnDelete: func(_ context.Context, id string, dur time.Duration, err error) {
			deleteCalled.Store(true)
			if id != "id1" {
				t.Errorf("OnDelete id = %q, want id1", id)
			}
		},
	}

	store := goagent.NewObservableStore(inner, obs)
	ctx := context.Background()

	if err := store.Upsert(ctx, "id1", []float32{0.1}, goagent.UserMessage("msg")); err != nil {
		t.Fatalf("Upsert error: %v", err)
	}
	if _, err := store.Search(ctx, []float32{0.1}, 3); err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if err := store.Delete(ctx, "id1"); err != nil {
		t.Fatalf("Delete error: %v", err)
	}

	if !upsertCalled.Load() {
		t.Error("OnUpsert was not called")
	}
	if !searchCalled.Load() {
		t.Error("OnSearch was not called")
	}
	if !deleteCalled.Load() {
		t.Error("OnDelete was not called")
	}
}

func TestNewObservableStore_ErrorPropagated(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("store unavailable")
	inner := &stubVectorStore{
		upsertErr: wantErr,
		searchErr: wantErr,
		deleteErr: wantErr,
	}

	var upsertErrGot, searchErrGot, deleteErrGot error

	obs := goagent.VectorStoreObserver{
		OnUpsert: func(_ context.Context, _ string, _ time.Duration, err error) {
			upsertErrGot = err
		},
		OnSearch: func(_ context.Context, _ int, _ int, _ time.Duration, err error) {
			searchErrGot = err
		},
		OnDelete: func(_ context.Context, _ string, _ time.Duration, err error) {
			deleteErrGot = err
		},
	}

	store := goagent.NewObservableStore(inner, obs)
	ctx := context.Background()

	if err := store.Upsert(ctx, "id1", nil, goagent.UserMessage("")); !errors.Is(err, wantErr) {
		t.Errorf("Upsert returned %v, want %v", err, wantErr)
	}
	if !errors.Is(upsertErrGot, wantErr) {
		t.Errorf("OnUpsert received %v, want %v", upsertErrGot, wantErr)
	}

	if _, err := store.Search(ctx, nil, 1); !errors.Is(err, wantErr) {
		t.Errorf("Search returned %v, want %v", err, wantErr)
	}
	if !errors.Is(searchErrGot, wantErr) {
		t.Errorf("OnSearch received %v, want %v", searchErrGot, wantErr)
	}

	if err := store.Delete(ctx, "id1"); !errors.Is(err, wantErr) {
		t.Errorf("Delete returned %v, want %v", err, wantErr)
	}
	if !errors.Is(deleteErrGot, wantErr) {
		t.Errorf("OnDelete received %v, want %v", deleteErrGot, wantErr)
	}
}

func TestNewObservableStore_NilCallbacksNoPanic(t *testing.T) {
	t.Parallel()

	inner := &stubVectorStore{}
	store := goagent.NewObservableStore(inner, goagent.VectorStoreObserver{})
	ctx := context.Background()

	// None of these should panic with a zero-value observer.
	_ = store.Upsert(ctx, "id1", nil, goagent.UserMessage(""))
	_, _ = store.Search(ctx, nil, 1)
	_ = store.Delete(ctx, "id1")
}

func TestNewObservableStore_NotBulkWhenInnerIsNot(t *testing.T) {
	t.Parallel()

	inner := &stubVectorStore{}
	store := goagent.NewObservableStore(inner, goagent.VectorStoreObserver{})

	if _, ok := store.(goagent.BulkVectorStore); ok {
		t.Error("expected store NOT to implement BulkVectorStore when inner does not")
	}
}

func TestNewObservableStore_BulkCallbacksFire(t *testing.T) {
	t.Parallel()

	inner := &stubBulkVectorStore{}

	var bulkUpsertCalled, bulkDeleteCalled atomic.Bool
	var bulkUpsertCount, bulkDeleteCount int

	obs := goagent.VectorStoreObserver{
		OnBulkUpsert: func(_ context.Context, count int, dur time.Duration, err error) {
			bulkUpsertCalled.Store(true)
			bulkUpsertCount = count
			if err != nil {
				t.Errorf("OnBulkUpsert err = %v, want nil", err)
			}
		},
		OnBulkDelete: func(_ context.Context, count int, dur time.Duration, err error) {
			bulkDeleteCalled.Store(true)
			bulkDeleteCount = count
			if err != nil {
				t.Errorf("OnBulkDelete err = %v, want nil", err)
			}
		},
	}

	store := goagent.NewObservableStore(inner, obs)

	bulk, ok := store.(goagent.BulkVectorStore)
	if !ok {
		t.Fatal("expected store to implement BulkVectorStore when inner does")
	}

	ctx := context.Background()

	entries := []goagent.UpsertEntry{
		{ID: "a", Vector: []float32{0.1}, Message: goagent.UserMessage("a")},
		{ID: "b", Vector: []float32{0.2}, Message: goagent.UserMessage("b")},
	}
	if err := bulk.BulkUpsert(ctx, entries); err != nil {
		t.Fatalf("BulkUpsert error: %v", err)
	}
	if !bulkUpsertCalled.Load() {
		t.Error("OnBulkUpsert was not called")
	}
	if bulkUpsertCount != 2 {
		t.Errorf("OnBulkUpsert count = %d, want 2", bulkUpsertCount)
	}

	if err := bulk.BulkDelete(ctx, []string{"a", "b", "c"}); err != nil {
		t.Fatalf("BulkDelete error: %v", err)
	}
	if !bulkDeleteCalled.Load() {
		t.Error("OnBulkDelete was not called")
	}
	if bulkDeleteCount != 3 {
		t.Errorf("OnBulkDelete count = %d, want 3", bulkDeleteCount)
	}
}

func TestNewObservableStore_BulkBaseMethodsStillWork(t *testing.T) {
	t.Parallel()

	// When wrapped as bulk, the base Upsert/Search/Delete must still work.
	inner := &stubBulkVectorStore{
		stubVectorStore: stubVectorStore{
			results: []goagent.ScoredMessage{{Score: 0.8}},
		},
	}

	var searchCalled atomic.Bool
	obs := goagent.VectorStoreObserver{
		OnSearch: func(_ context.Context, _ int, _ int, _ time.Duration, _ error) {
			searchCalled.Store(true)
		},
	}

	store := goagent.NewObservableStore(inner, obs)
	results, err := store.Search(context.Background(), nil, 1)
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("Search results = %d, want 1", len(results))
	}
	if !searchCalled.Load() {
		t.Error("OnSearch was not called via base method on bulk store")
	}
}

// ── MergeVectorStoreObservers tests ─────────────────────────────────────────

func TestMergeVectorStoreObservers_BothCalledInOrder(t *testing.T) {
	t.Parallel()

	var order []string

	obs1 := goagent.VectorStoreObserver{
		OnSearch: func(_ context.Context, _ int, _ int, _ time.Duration, _ error) {
			order = append(order, "obs1")
		},
	}
	obs2 := goagent.VectorStoreObserver{
		OnSearch: func(_ context.Context, _ int, _ int, _ time.Duration, _ error) {
			order = append(order, "obs2")
		},
	}

	merged := goagent.MergeVectorStoreObservers(obs1, obs2)
	inner := &stubVectorStore{}
	store := goagent.NewObservableStore(inner, merged)

	_, _ = store.Search(context.Background(), nil, 1)

	if len(order) != 2 || order[0] != "obs1" || order[1] != "obs2" {
		t.Errorf("call order = %v, want [obs1 obs2]", order)
	}
}

func TestMergeVectorStoreObservers_Empty(t *testing.T) {
	t.Parallel()

	merged := goagent.MergeVectorStoreObservers()
	// All fields should be nil — using it should not panic.
	store := goagent.NewObservableStore(&stubVectorStore{}, merged)
	_ = store.Upsert(context.Background(), "x", nil, goagent.UserMessage(""))
}

func TestMergeVectorStoreObservers_Single(t *testing.T) {
	t.Parallel()

	var called atomic.Bool
	obs := goagent.VectorStoreObserver{
		OnDelete: func(_ context.Context, _ string, _ time.Duration, _ error) {
			called.Store(true)
		},
	}

	merged := goagent.MergeVectorStoreObservers(obs)
	store := goagent.NewObservableStore(&stubVectorStore{}, merged)
	_ = store.Delete(context.Background(), "id1")

	if !called.Load() {
		t.Error("callback not called after MergeVectorStoreObservers with single observer")
	}
}

func TestMergeVectorStoreObservers_NilFieldsIgnored(t *testing.T) {
	t.Parallel()

	// obs1 has OnSearch; obs2 does not. Merge should work without panic.
	var searchCalled atomic.Bool
	obs1 := goagent.VectorStoreObserver{
		OnSearch: func(_ context.Context, _ int, _ int, _ time.Duration, _ error) {
			searchCalled.Store(true)
		},
	}
	obs2 := goagent.VectorStoreObserver{} // all nil

	merged := goagent.MergeVectorStoreObservers(obs1, obs2)
	store := goagent.NewObservableStore(&stubVectorStore{}, merged)
	_, _ = store.Search(context.Background(), nil, 1)

	if !searchCalled.Load() {
		t.Error("OnSearch was not called")
	}
}
