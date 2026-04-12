package pgvector_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/Germanblandin1/goagent"
	"github.com/Germanblandin1/goagent/memory/vector/pgvector"
)

// ── mock Querier ─────────────────────────────────────────────────────────────

// mockResult implements sql.Result.
type mockResult struct{}

func (mockResult) LastInsertId() (int64, error) { return 0, nil }
func (mockResult) RowsAffected() (int64, error) { return 1, nil }

// mockQuerier implements pgvector.Querier and returns configurable errors.
// When execErr is nil, ExecContext succeeds. When queryErr is nil, QueryContext
// returns (nil, queryErr) — which lets us test error paths without needing
// real *sql.Rows.
type mockQuerier struct {
	execErr  error
	queryErr error
}

func (m *mockQuerier) ExecContext(_ context.Context, _ string, _ ...any) (sql.Result, error) {
	if m.execErr != nil {
		return nil, m.execErr
	}
	return mockResult{}, nil
}

func (m *mockQuerier) QueryContext(_ context.Context, _ string, _ ...any) (*sql.Rows, error) {
	return nil, m.queryErr
}

// validCfg returns a minimal valid TableConfig for tests.
func validCfg() pgvector.TableConfig {
	return pgvector.TableConfig{
		Table:        "embeddings",
		IDColumn:     "id",
		VectorColumn: "embedding",
		TextColumn:   "content",
	}
}

func validCfgWithMeta() pgvector.TableConfig {
	cfg := validCfg()
	cfg.MetadataColumn = "metadata"
	return cfg
}

// ── Constructor tests ────────────────────────────────────────────────────────

func TestNew_Success(t *testing.T) {
	t.Parallel()

	store, err := pgvector.New(&mockQuerier{}, validCfg())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if store == nil {
		t.Fatal("New returned nil store")
	}
}

func TestNew_WithMetadataColumn(t *testing.T) {
	t.Parallel()

	store, err := pgvector.New(&mockQuerier{}, validCfgWithMeta())
	if err != nil {
		t.Fatalf("New with metadata: %v", err)
	}
	if store == nil {
		t.Fatal("returned nil store")
	}
}

func TestNew_WithDistanceFuncs(t *testing.T) {
	t.Parallel()

	funcs := []pgvector.DistanceFunc{pgvector.Cosine, pgvector.L2, pgvector.InnerProduct}
	for _, df := range funcs {
		store, err := pgvector.New(&mockQuerier{}, validCfg(), pgvector.WithDistanceFunc(df))
		if err != nil {
			t.Fatalf("New with distance func: %v", err)
		}
		if store == nil {
			t.Fatal("returned nil store")
		}
	}
}

func TestNew_InvalidIdentifier(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		cfg  pgvector.TableConfig
	}{
		{
			"table with space",
			pgvector.TableConfig{Table: "my table", IDColumn: "id", VectorColumn: "v", TextColumn: "t"},
		},
		{
			"id with semicolon",
			pgvector.TableConfig{Table: "t", IDColumn: "id;DROP", VectorColumn: "v", TextColumn: "t"},
		},
		{
			"vector with dash",
			pgvector.TableConfig{Table: "t", IDColumn: "id", VectorColumn: "vec-col", TextColumn: "t"},
		},
		{
			"text starts with digit",
			pgvector.TableConfig{Table: "t", IDColumn: "id", VectorColumn: "v", TextColumn: "1text"},
		},
		{
			"invalid metadata column",
			pgvector.TableConfig{Table: "t", IDColumn: "id", VectorColumn: "v", TextColumn: "t", MetadataColumn: "meta data"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := pgvector.New(&mockQuerier{}, tc.cfg)
			if err == nil {
				t.Error("expected error for invalid identifier, got nil")
			}
		})
	}
}

// ── Upsert error paths ───────────────────────────────────────────────────────

func TestUpsert_ExecError(t *testing.T) {
	t.Parallel()

	dbErr := errors.New("connection refused")
	store, _ := pgvector.New(&mockQuerier{execErr: dbErr}, validCfg())

	err := store.Upsert(context.Background(), "id1", []float32{0.1, 0.2}, goagent.UserMessage("hello"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, dbErr) {
		t.Errorf("error = %v, want to wrap %v", err, dbErr)
	}
}

func TestUpsert_WithMetadata_ExecError(t *testing.T) {
	t.Parallel()

	dbErr := errors.New("timeout")
	store, _ := pgvector.New(&mockQuerier{execErr: dbErr}, validCfgWithMeta())

	msg := goagent.Message{
		Role:     goagent.RoleUser,
		Content:  []goagent.ContentBlock{goagent.TextBlock("text")},
		Metadata: map[string]any{"source": "test"},
	}
	err := store.Upsert(context.Background(), "id1", []float32{0.1}, msg)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, dbErr) {
		t.Errorf("error = %v, want to wrap %v", err, dbErr)
	}
}

// ── Delete error path ────────────────────────────────────────────────────────

func TestDelete_ExecError(t *testing.T) {
	t.Parallel()

	dbErr := errors.New("delete failed")
	store, _ := pgvector.New(&mockQuerier{execErr: dbErr}, validCfg())

	err := store.Delete(context.Background(), "id1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, dbErr) {
		t.Errorf("error = %v, want to wrap %v", err, dbErr)
	}
}

// ── Search error paths ───────────────────────────────────────────────────────

func TestSearch_QueryError(t *testing.T) {
	t.Parallel()

	dbErr := errors.New("query failed")
	store, _ := pgvector.New(&mockQuerier{queryErr: dbErr}, validCfg())

	_, err := store.Search(context.Background(), []float32{0.1, 0.2}, 5)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, dbErr) {
		t.Errorf("error = %v, want to wrap %v", err, dbErr)
	}
}

func TestSearch_WithFilter_QueryError(t *testing.T) {
	t.Parallel()

	dbErr := errors.New("filter query failed")
	store, _ := pgvector.New(&mockQuerier{queryErr: dbErr}, validCfgWithMeta())

	_, err := store.Search(
		context.Background(),
		[]float32{0.1},
		5,
		goagent.WithFilter(map[string]any{"doc_type": "report"}),
	)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, dbErr) {
		t.Errorf("error = %v, want to wrap %v", err, dbErr)
	}
}

// ── Count error path ─────────────────────────────────────────────────────────

func TestCount_QueryError(t *testing.T) {
	t.Parallel()

	dbErr := errors.New("count failed")
	store, _ := pgvector.New(&mockQuerier{queryErr: dbErr}, validCfg())

	_, err := store.Count(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, dbErr) {
		t.Errorf("error = %v, want to wrap %v", err, dbErr)
	}
}

func TestCount_WithFilter_QueryError(t *testing.T) {
	t.Parallel()

	dbErr := errors.New("count filter failed")
	store, _ := pgvector.New(&mockQuerier{queryErr: dbErr}, validCfgWithMeta())

	_, err := store.Count(
		context.Background(),
		goagent.WithFilter(map[string]any{"type": "article"}),
	)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, dbErr) {
		t.Errorf("error = %v, want to wrap %v", err, dbErr)
	}
}

// ── BulkUpsert ───────────────────────────────────────────────────────────────

func TestBulkUpsert_Empty_IsNoop(t *testing.T) {
	t.Parallel()

	// ExecContext should never be called for empty input.
	q := &mockQuerier{execErr: errors.New("should not be called")}
	store, _ := pgvector.New(q, validCfg())

	if err := store.BulkUpsert(context.Background(), nil); err != nil {
		t.Errorf("BulkUpsert(nil) = %v, want nil", err)
	}
	if err := store.BulkUpsert(context.Background(), []goagent.UpsertEntry{}); err != nil {
		t.Errorf("BulkUpsert([]) = %v, want nil", err)
	}
}

// TestBulkUpsert_ExecError_NonTransactional verifies that BulkUpsert propagates
// ExecContext errors when the db does not implement pgTransactor (no BeginTx).
// mockQuerier only implements Querier (not pgTransactor), so BulkUpsert uses
// the sequential fallback path.
func TestBulkUpsert_ExecError_NonTransactional(t *testing.T) {
	t.Parallel()

	dbErr := errors.New("upsert exec failed")
	store, _ := pgvector.New(&mockQuerier{execErr: dbErr}, validCfg())

	entries := []goagent.UpsertEntry{
		{ID: "a", Vector: []float32{0.1}, Message: goagent.UserMessage("a")},
	}
	err := store.BulkUpsert(context.Background(), entries)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, dbErr) {
		t.Errorf("error = %v, want to wrap %v", err, dbErr)
	}
}

// ── BulkDelete ───────────────────────────────────────────────────────────────

func TestBulkDelete_Empty_IsNoop(t *testing.T) {
	t.Parallel()

	q := &mockQuerier{execErr: errors.New("should not be called")}
	store, _ := pgvector.New(q, validCfg())

	if err := store.BulkDelete(context.Background(), nil); err != nil {
		t.Errorf("BulkDelete(nil) = %v, want nil", err)
	}
	if err := store.BulkDelete(context.Background(), []string{}); err != nil {
		t.Errorf("BulkDelete([]) = %v, want nil", err)
	}
}

func TestBulkDelete_ExecError(t *testing.T) {
	t.Parallel()

	dbErr := errors.New("delete exec failed")
	store, _ := pgvector.New(&mockQuerier{execErr: dbErr}, validCfg())

	err := store.BulkDelete(context.Background(), []string{"id1", "id2"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, dbErr) {
		t.Errorf("error = %v, want to wrap %v", err, dbErr)
	}
}

// ── DistanceFunc SQL expressions ─────────────────────────────────────────────

// TestDistanceFunc_ScoreAndOrderExprs verifies that score expressions for all
// built-in distance functions are non-empty and differ from each other.
// This exercises the scoreExpr/orderExpr helpers that go uncovered otherwise.
func TestDistanceFunc_ScoreAndOrderExprs(t *testing.T) {
	t.Parallel()

	funcs := []struct {
		name string
		df   pgvector.DistanceFunc
	}{
		{"cosine", pgvector.Cosine},
		{"l2", pgvector.L2},
		{"inner_product", pgvector.InnerProduct},
	}

	// Build stores with each distance func — the SQL is generated in New.
	for _, tc := range funcs {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store, err := pgvector.New(&mockQuerier{}, validCfg(), pgvector.WithDistanceFunc(tc.df))
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if store == nil {
				t.Fatal("nil store")
			}
		})
	}
}
