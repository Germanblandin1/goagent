package pgvector

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"

	"github.com/Germanblandin1/goagent"
)

// Querier is the minimal interface the Store needs from a connection.
// Both *sql.DB and *sql.Tx satisfy it, as does any pgx adapter that wraps them.
type Querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// Note: *Store does not satisfy goagent.VectorStore directly — its Search
// method accepts an optional ...SearchOption parameter which is not part of
// the interface. Wrap it with an adapter if you need to pass *Store where a
// goagent.VectorStore is required.

// validIdentifier matches safe SQL identifiers: letters, digits, underscores,
// and dots (for schema-qualified names like "public.embeddings").
var validIdentifier = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_.]*$`)

// TableConfig describes the schema of the caller's vector table.
// No field has a default — the caller must be explicit.
// All fields are required except MetadataColumn.
type TableConfig struct {
	// Table is the table or view name. May include schema: "public.embeddings".
	Table string

	// IDColumn is the PRIMARY KEY column of type TEXT.
	IDColumn string

	// VectorColumn is the column of type vector(n) where n is the embedding
	// model dimension (e.g. 1536 for text-embedding-3-small, 768 for
	// nomic-embed-text, 1024 for voyage-3).
	VectorColumn string

	// TextColumn is the TEXT column containing the chunk text.
	// Its value is returned as a goagent.ContentBlock of type text in ScoredMessage.
	TextColumn string

	// MetadataColumn is optional. If non-empty, it must be a JSONB column.
	// Its content is deserialized into Message.Metadata of each ScoredMessage.
	MetadataColumn string
}

// StoreOption configures the behaviour of a Store.
type StoreOption func(*storeOptions)

type storeOptions struct {
	distanceFunc DistanceFunc
}

// WithDistanceFunc sets the distance operator used in Search.
// Must match the operator class of the table's vector index.
// Default: Cosine.
func WithDistanceFunc(d DistanceFunc) StoreOption {
	return func(o *storeOptions) { o.distanceFunc = d }
}

// Store implements goagent.VectorStore over PostgreSQL with the pgvector extension.
type Store struct {
	db        Querier
	cfg       TableConfig
	upsertSQL string
	searchSQL string
	deleteSQL string
}

// New creates a Store backed by db using the given TableConfig and options.
// Returns an error if any required field is missing or contains invalid characters.
func New(db Querier, cfg TableConfig, opts ...StoreOption) (*Store, error) {
	required := []struct {
		name  string
		value string
	}{
		{"Table", cfg.Table},
		{"IDColumn", cfg.IDColumn},
		{"VectorColumn", cfg.VectorColumn},
		{"TextColumn", cfg.TextColumn},
	}
	for _, f := range required {
		if f.value == "" {
			return nil, fmt.Errorf("pgvector: TableConfig.%s is required", f.name)
		}
		if !validIdentifier.MatchString(f.value) {
			return nil, fmt.Errorf("pgvector: TableConfig.%s contains invalid characters: %q", f.name, f.value)
		}
	}
	if cfg.MetadataColumn != "" && !validIdentifier.MatchString(cfg.MetadataColumn) {
		return nil, fmt.Errorf("pgvector: TableConfig.MetadataColumn contains invalid characters: %q", cfg.MetadataColumn)
	}

	var o storeOptions
	for _, opt := range opts {
		opt(&o)
	}
	if o.distanceFunc.operator == "" {
		o.distanceFunc = Cosine
	}

	s := &Store{db: db, cfg: cfg}
	s.upsertSQL = s.buildUpsertSQL()
	s.searchSQL = s.buildSearchSQL(o.distanceFunc)
	s.deleteSQL = fmt.Sprintf(`DELETE FROM %s WHERE %s = $1`, cfg.Table, cfg.IDColumn)
	return s, nil
}

func (s *Store) buildUpsertSQL() string {
	cfg := s.cfg
	if cfg.MetadataColumn != "" {
		return fmt.Sprintf(`
INSERT INTO %s (%s, %s, %s, %s)
VALUES ($1, $2::vector, $3, $4)
ON CONFLICT (%s) DO UPDATE
    SET %s = EXCLUDED.%s,
        %s = EXCLUDED.%s,
        %s = EXCLUDED.%s`,
			cfg.Table,
			cfg.IDColumn, cfg.VectorColumn, cfg.TextColumn, cfg.MetadataColumn,
			cfg.IDColumn,
			cfg.VectorColumn, cfg.VectorColumn,
			cfg.TextColumn, cfg.TextColumn,
			cfg.MetadataColumn, cfg.MetadataColumn,
		)
	}
	return fmt.Sprintf(`
INSERT INTO %s (%s, %s, %s)
VALUES ($1, $2::vector, $3)
ON CONFLICT (%s) DO UPDATE
    SET %s = EXCLUDED.%s,
        %s = EXCLUDED.%s`,
		cfg.Table,
		cfg.IDColumn, cfg.VectorColumn, cfg.TextColumn,
		cfg.IDColumn,
		cfg.VectorColumn, cfg.VectorColumn,
		cfg.TextColumn, cfg.TextColumn,
	)
}

func (s *Store) buildSearchSQL(df DistanceFunc) string {
	cfg := s.cfg
	orderE := df.orderExpr(cfg.VectorColumn, "$1")
	scoreE := df.scoreExpr(cfg.VectorColumn, "$1")

	if cfg.MetadataColumn != "" {
		return fmt.Sprintf(`
SELECT %s, %s, %s AS score
FROM %s
ORDER BY %s
LIMIT $2`,
			cfg.TextColumn, cfg.MetadataColumn, scoreE,
			cfg.Table,
			orderE,
		)
	}
	return fmt.Sprintf(`
SELECT %s, %s AS score
FROM %s
ORDER BY %s
LIMIT $2`,
		cfg.TextColumn, scoreE,
		cfg.Table,
		orderE,
	)
}

// Upsert stores or updates the message and its embedding vector under id.
// The operation is idempotent: calling Upsert twice with the same id replaces
// the first entry with the second. Only the text content and metadata from msg
// are persisted — Role and ToolCalls are not stored.
func (s *Store) Upsert(ctx context.Context, id string, vec []float32, msg goagent.Message) error {
	text := goagent.TextFrom(msg.Content)
	vecLit := float32SliceToLiteral(vec)

	var err error
	if s.cfg.MetadataColumn != "" {
		metaJSON, merr := metadataToJSON(msg.Metadata)
		if merr != nil {
			return fmt.Errorf("pgvector: upsert: %w", merr)
		}
		_, err = s.db.ExecContext(ctx, s.upsertSQL, id, vecLit, text, metaJSON)
	} else {
		_, err = s.db.ExecContext(ctx, s.upsertSQL, id, vecLit, text)
	}
	if err != nil {
		return fmt.Errorf("pgvector: upsert: %w", err)
	}
	return nil
}

// Search returns the topK messages most similar to vec, ordered by similarity
// descending. Each returned Message has RoleDocument so it is never forwarded
// to a provider.
//
// opts is reserved for future use (score threshold, metadata filters, etc.).
// Passing no options is equivalent to the default behaviour.
func (s *Store) Search(ctx context.Context, vec []float32, topK int, opts ...SearchOption) ([]goagent.ScoredMessage, error) {
	cfg := &searchConfig{}
	for _, o := range opts {
		o(cfg)
	}

	vecLit := float32SliceToLiteral(vec)
	rows, err := s.db.QueryContext(ctx, s.searchSQL, vecLit, topK)
	if err != nil {
		return nil, fmt.Errorf("pgvector: search: %w", err)
	}
	defer rows.Close()

	var results []goagent.ScoredMessage
	for rows.Next() {
		var text string
		var score float64

		if s.cfg.MetadataColumn != "" {
			var metaStr string
			if err := rows.Scan(&text, &metaStr, &score); err != nil {
				return nil, fmt.Errorf("pgvector: search: %w", err)
			}
			meta, merr := jsonToMetadata(metaStr)
			if merr != nil {
				return nil, fmt.Errorf("pgvector: search: %w", merr)
			}
			results = append(results, goagent.ScoredMessage{
				Score: score,
				Message: goagent.Message{
					Role:     goagent.RoleDocument,
					Content:  []goagent.ContentBlock{goagent.TextBlock(text)},
					Metadata: meta,
				},
			})
		} else {
			if err := rows.Scan(&text, &score); err != nil {
				return nil, fmt.Errorf("pgvector: search: %w", err)
			}
			results = append(results, goagent.ScoredMessage{
				Score: score,
				Message: goagent.Message{
					Role:    goagent.RoleDocument,
					Content: []goagent.ContentBlock{goagent.TextBlock(text)},
				},
			})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pgvector: search: %w", err)
	}
	return results, nil
}

// Delete removes the entry with the given id from the store.
// It is a no-op if id does not exist.
func (s *Store) Delete(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, s.deleteSQL, id)
	if err != nil {
		return fmt.Errorf("pgvector: delete: %w", err)
	}
	return nil
}
