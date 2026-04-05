package pgvector_test

import (
	"context"
	"testing"

	"github.com/Germanblandin1/goagent/memory/vector/pgvector"
)

// TestNew_MissingRequiredFields and TestMigrate_ZeroDimsReturnsError do not
// require a database — they only validate constructor error paths.

func TestNew_MissingRequiredFields(t *testing.T) {
	cases := []struct {
		name string
		cfg  pgvector.TableConfig
	}{
		{"missing Table", pgvector.TableConfig{IDColumn: "id", VectorColumn: "emb", TextColumn: "txt"}},
		{"missing IDColumn", pgvector.TableConfig{Table: "t", VectorColumn: "emb", TextColumn: "txt"}},
		{"missing VectorColumn", pgvector.TableConfig{Table: "t", IDColumn: "id", TextColumn: "txt"}},
		{"missing TextColumn", pgvector.TableConfig{Table: "t", IDColumn: "id", VectorColumn: "emb"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := pgvector.New(nil, tc.cfg)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

func TestMigrate_ZeroDimsReturnsError(t *testing.T) {
	err := pgvector.Migrate(context.Background(), nil, pgvector.MigrateConfig{})
	if err == nil {
		t.Fatal("expected error for zero Dims, got nil")
	}
}
