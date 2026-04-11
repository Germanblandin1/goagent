package sqlitevec

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3" // registers the "sqlite3" driver

	"github.com/Germanblandin1/goagent"
)

// validIdentifier matches safe SQLite identifiers: letters, digits, underscores.
// Schema-qualified names (dots) are not supported — SQLite has no schemas.
var validIdentifier = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// TableConfig describes the schema used by this package.
// The vec0 virtual table is inferred by appending "_vec" to Table.
// All fields are required except MetadataColumn.
type TableConfig struct {
	// Table is the regular SQLite table name (e.g. "goagent_embeddings").
	Table string

	// IDColumn is the PRIMARY KEY column of type TEXT in the data table.
	IDColumn string

	// VectorColumn is the vector column defined in the vec0 virtual table
	// (the table named Table+"_vec").
	VectorColumn string

	// TextColumn is the TEXT column in the data table containing the chunk text.
	TextColumn string

	// MetadataColumn is optional. If non-empty, it must be a TEXT column holding
	// a JSON object. Its content is deserialized into Message.Metadata.
	MetadataColumn string
}

// StoreOption configures the behaviour of a Store.
type StoreOption func(*storeOptions)

type storeOptions struct {
	metric DistanceMetric
}

// WithDistanceMetric sets the similarity metric used in Search.
// Default: L2.
func WithDistanceMetric(m DistanceMetric) StoreOption {
	return func(o *storeOptions) { o.metric = m }
}

// Compile-time checks.
var _ goagent.VectorStore = (*Store)(nil)
var _ goagent.BulkVectorStore = (*Store)(nil)

// Store implements goagent.VectorStore over SQLite with the sqlite-vec extension.
// It satisfies the goagent.VectorStore interface directly.
type Store struct {
	db            *sql.DB
	cfg           TableConfig
	metric        DistanceMetric
	upsertMainSQL string
	getRowidSQL   string
	deleteVecSQL  string
	insertVecSQL  string
	searchSQL     string
	deleteSQL     string
}

// Open registers the sqlite-vec extension and opens a SQLite database at dsn.
// It is the recommended way to obtain a *sql.DB for use with this package.
//
// Callers managing their own connection must call Register before sql.Open.
func Open(dsn string) (*sql.DB, error) {
	sqlite_vec.Auto()
	return sql.Open("sqlite3", dsn)
}

// Register enables the sqlite-vec extension for all SQLite connections opened
// after this call. It is idempotent and safe to call multiple times.
// Must be called before sql.Open when not using Open.
func Register() {
	sqlite_vec.Auto()
}

// New creates a Store backed by db using the given TableConfig and options.
// Returns an error if any required field is missing or contains invalid characters.
func New(db *sql.DB, cfg TableConfig, opts ...StoreOption) (*Store, error) {
	required := []struct{ name, value string }{
		{"Table", cfg.Table},
		{"IDColumn", cfg.IDColumn},
		{"VectorColumn", cfg.VectorColumn},
		{"TextColumn", cfg.TextColumn},
	}
	for _, f := range required {
		if f.value == "" {
			return nil, fmt.Errorf("sqlitevec: TableConfig.%s is required", f.name)
		}
		if !validIdentifier.MatchString(f.value) {
			return nil, fmt.Errorf("sqlitevec: TableConfig.%s contains invalid characters: %q", f.name, f.value)
		}
	}
	if cfg.MetadataColumn != "" && !validIdentifier.MatchString(cfg.MetadataColumn) {
		return nil, fmt.Errorf("sqlitevec: TableConfig.MetadataColumn contains invalid characters: %q", cfg.MetadataColumn)
	}

	var o storeOptions
	for _, opt := range opts {
		opt(&o)
	}
	if o.metric == "" {
		o.metric = L2
	}

	vt := cfg.Table + "_vec"
	s := &Store{db: db, cfg: cfg, metric: o.metric}
	s.upsertMainSQL = s.buildUpsertMainSQL()
	s.getRowidSQL = fmt.Sprintf(`SELECT rowid FROM %s WHERE %s = ?`, cfg.Table, cfg.IDColumn)
	s.deleteVecSQL = fmt.Sprintf(`DELETE FROM %s WHERE rowid = ?`, vt)
	s.insertVecSQL = fmt.Sprintf(`INSERT INTO %s(rowid, %s) VALUES (?, ?)`, vt, cfg.VectorColumn)
	s.searchSQL = s.buildSearchSQL(o.metric)
	s.deleteSQL = fmt.Sprintf(`DELETE FROM %s WHERE %s = ?`, cfg.Table, cfg.IDColumn)
	return s, nil
}

func (s *Store) buildUpsertMainSQL() string {
	cfg := s.cfg
	if cfg.MetadataColumn != "" {
		return fmt.Sprintf(
			`INSERT INTO %s(%s, %s, %s) VALUES (?, ?, ?)
ON CONFLICT(%s) DO UPDATE SET
    %s = excluded.%s,
    %s = excluded.%s`,
			cfg.Table, cfg.IDColumn, cfg.TextColumn, cfg.MetadataColumn,
			cfg.IDColumn,
			cfg.TextColumn, cfg.TextColumn,
			cfg.MetadataColumn, cfg.MetadataColumn,
		)
	}
	return fmt.Sprintf(
		`INSERT INTO %s(%s, %s) VALUES (?, ?)
ON CONFLICT(%s) DO UPDATE SET
    %s = excluded.%s`,
		cfg.Table, cfg.IDColumn, cfg.TextColumn,
		cfg.IDColumn,
		cfg.TextColumn, cfg.TextColumn,
	)
}

func (s *Store) buildSearchSQL(metric DistanceMetric) string {
	cfg := s.cfg
	vt := cfg.Table + "_vec"

	var distCol, filterClause, tailClause string
	if metric == Cosine {
		distCol = fmt.Sprintf("vec_distance_cosine(v.%s, ?) AS distance", cfg.VectorColumn)
		filterClause = ""
		tailClause = "ORDER BY distance\nLIMIT ?"
	} else { // L2: indexed KNN via MATCH
		distCol = "v.distance"
		filterClause = fmt.Sprintf("WHERE v.%s MATCH ? AND k = ?", cfg.VectorColumn)
		tailClause = "ORDER BY v.distance"
	}

	var selectCols string
	if cfg.MetadataColumn != "" {
		selectCols = fmt.Sprintf("t.%s, t.%s, %s", cfg.TextColumn, cfg.MetadataColumn, distCol)
	} else {
		selectCols = fmt.Sprintf("t.%s, %s", cfg.TextColumn, distCol)
	}

	return fmt.Sprintf(
		"SELECT %s\nFROM %s v\nJOIN %s t ON t.rowid = v.rowid\n%s\n%s",
		selectCols, vt, cfg.Table, filterClause, tailClause,
	)
}

// Upsert stores or updates the message and its embedding vector under id.
// The operation is idempotent: calling Upsert twice with the same id replaces
// the first entry. Only text content and optional metadata from msg are
// persisted — Role and ToolCalls are discarded.
func (s *Store) Upsert(ctx context.Context, id string, vec []float32, msg goagent.Message) error {
	text := goagent.TextFrom(msg.Content)
	vecBlob := serializeVec(vec)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlitevec: upsert: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if s.cfg.MetadataColumn != "" {
		metaJSON, merr := metadataToJSON(msg.Metadata)
		if merr != nil {
			return fmt.Errorf("sqlitevec: upsert: %w", merr)
		}
		if _, err := tx.ExecContext(ctx, s.upsertMainSQL, id, text, metaJSON); err != nil {
			return fmt.Errorf("sqlitevec: upsert: %w", err)
		}
	} else {
		if _, err := tx.ExecContext(ctx, s.upsertMainSQL, id, text); err != nil {
			return fmt.Errorf("sqlitevec: upsert: %w", err)
		}
	}

	var rowid int64
	if err := tx.QueryRowContext(ctx, s.getRowidSQL, id).Scan(&rowid); err != nil {
		return fmt.Errorf("sqlitevec: upsert: get rowid: %w", err)
	}

	// Delete the existing vec entry (no-op for new rows) before inserting the
	// updated vector. vec0 virtual tables do not support INSERT OR REPLACE.
	if _, err := tx.ExecContext(ctx, s.deleteVecSQL, rowid); err != nil {
		return fmt.Errorf("sqlitevec: upsert: delete vec: %w", err)
	}
	if _, err := tx.ExecContext(ctx, s.insertVecSQL, rowid, vecBlob); err != nil {
		return fmt.Errorf("sqlitevec: upsert: insert vec: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sqlitevec: upsert: %w", err)
	}
	return nil
}

// Search returns the topK messages most similar to vec, ordered by similarity
// descending. Each returned Message has RoleDocument so it is never forwarded
// to a provider.
//
// L2 search uses sqlite-vec's indexed KNN (MATCH … AND k = ?).
// Cosine search uses vec_distance_cosine and performs a full scan.
//
// WithScoreThreshold and WithFilter are both applied post-query in Go.
// topK is applied by the database first, so fewer than topK results may be
// returned when either option is active. WithFilter requires MetadataColumn
// to be set; silently ignored otherwise. All key-value pairs in the filter
// must match (AND semantics); values are compared with reflect.DeepEqual.
func (s *Store) Search(ctx context.Context, vec []float32, topK int, opts ...goagent.SearchOption) ([]goagent.ScoredMessage, error) {
	cfg := &goagent.SearchOptions{}
	for _, o := range opts {
		o(cfg)
	}

	vecBlob := serializeVec(vec)
	rows, err := s.db.QueryContext(ctx, s.searchSQL, vecBlob, topK)
	if err != nil {
		return nil, fmt.Errorf("sqlitevec: search: %w", err)
	}
	defer rows.Close()

	var results []goagent.ScoredMessage
	for rows.Next() {
		var text string
		var distance float64

		if s.cfg.MetadataColumn != "" {
			var metaStr string
			if err := rows.Scan(&text, &metaStr, &distance); err != nil {
				return nil, fmt.Errorf("sqlitevec: search: %w", err)
			}
			meta, merr := jsonToMetadata(metaStr)
			if merr != nil {
				return nil, fmt.Errorf("sqlitevec: search: %w", merr)
			}
			results = append(results, goagent.ScoredMessage{
				Score: s.scoreFromDistance(distance),
				Message: goagent.Message{
					Role:     goagent.RoleDocument,
					Content:  []goagent.ContentBlock{goagent.TextBlock(text)},
					Metadata: meta,
				},
			})
		} else {
			if err := rows.Scan(&text, &distance); err != nil {
				return nil, fmt.Errorf("sqlitevec: search: %w", err)
			}
			results = append(results, goagent.ScoredMessage{
				Score: s.scoreFromDistance(distance),
				Message: goagent.Message{
					Role:    goagent.RoleDocument,
					Content: []goagent.ContentBlock{goagent.TextBlock(text)},
				},
			})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlitevec: search: %w", err)
	}

	if cfg.ScoreThreshold != nil {
		threshold := *cfg.ScoreThreshold
		filtered := results[:0]
		for _, r := range results {
			if r.Score >= threshold {
				filtered = append(filtered, r)
			}
		}
		results = filtered
	}

	if len(cfg.Filter) > 0 && s.cfg.MetadataColumn != "" {
		results = filterByMetadata(results, cfg.Filter)
	}

	return results, nil
}

// scoreFromDistance converts a raw distance to a similarity score where
// higher always means more similar.
//
//	L2:     1.0 / (1.0 + distance)  → (0, 1]
//	Cosine: 1.0 - distance          → [0, 1] for unit-normalised vectors
func (s *Store) scoreFromDistance(distance float64) float64 {
	if s.metric == Cosine {
		return 1.0 - distance
	}
	return 1.0 / (1.0 + distance)
}

// Delete removes the entry with the given id from both the data table and the
// vec0 virtual table. It is a no-op if id does not exist.
func (s *Store) Delete(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlitevec: delete: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var rowid int64
	if err := tx.QueryRowContext(ctx, s.getRowidSQL, id).Scan(&rowid); err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return fmt.Errorf("sqlitevec: delete: %w", err)
	}

	if _, err := tx.ExecContext(ctx, s.deleteVecSQL, rowid); err != nil {
		return fmt.Errorf("sqlitevec: delete: vec: %w", err)
	}
	if _, err := tx.ExecContext(ctx, s.deleteSQL, id); err != nil {
		return fmt.Errorf("sqlitevec: delete: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sqlitevec: delete: %w", err)
	}
	return nil
}

// BulkUpsert stores or updates all entries within a single SQLite transaction,
// reducing round-trips compared to N individual Upsert calls. Each entry
// follows the same multi-step upsert logic as [Store.Upsert].
func (s *Store) BulkUpsert(ctx context.Context, entries []goagent.UpsertEntry) error {
	if len(entries) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlitevec: bulk upsert: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	for _, e := range entries {
		text := goagent.TextFrom(e.Message.Content)
		vecBlob := serializeVec(e.Vector)

		if s.cfg.MetadataColumn != "" {
			metaJSON, merr := metadataToJSON(e.Message.Metadata)
			if merr != nil {
				return fmt.Errorf("sqlitevec: bulk upsert: %w", merr)
			}
			if _, err := tx.ExecContext(ctx, s.upsertMainSQL, e.ID, text, metaJSON); err != nil {
				return fmt.Errorf("sqlitevec: bulk upsert: %w", err)
			}
		} else {
			if _, err := tx.ExecContext(ctx, s.upsertMainSQL, e.ID, text); err != nil {
				return fmt.Errorf("sqlitevec: bulk upsert: %w", err)
			}
		}

		var rowid int64
		if err := tx.QueryRowContext(ctx, s.getRowidSQL, e.ID).Scan(&rowid); err != nil {
			return fmt.Errorf("sqlitevec: bulk upsert: get rowid: %w", err)
		}

		if _, err := tx.ExecContext(ctx, s.deleteVecSQL, rowid); err != nil {
			return fmt.Errorf("sqlitevec: bulk upsert: delete vec: %w", err)
		}
		if _, err := tx.ExecContext(ctx, s.insertVecSQL, rowid, vecBlob); err != nil {
			return fmt.Errorf("sqlitevec: bulk upsert: insert vec: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sqlitevec: bulk upsert: %w", err)
	}
	return nil
}

// BulkDelete removes all entries with the given ids within a single SQLite
// transaction. IDs that do not exist are silently ignored.
func (s *Store) BulkDelete(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlitevec: bulk delete: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	for _, id := range ids {
		var rowid int64
		if err := tx.QueryRowContext(ctx, s.getRowidSQL, id).Scan(&rowid); err != nil {
			if err == sql.ErrNoRows {
				continue
			}
			return fmt.Errorf("sqlitevec: bulk delete: %w", err)
		}
		if _, err := tx.ExecContext(ctx, s.deleteVecSQL, rowid); err != nil {
			return fmt.Errorf("sqlitevec: bulk delete: vec: %w", err)
		}
		if _, err := tx.ExecContext(ctx, s.deleteSQL, id); err != nil {
			return fmt.Errorf("sqlitevec: bulk delete: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sqlitevec: bulk delete: %w", err)
	}
	return nil
}
