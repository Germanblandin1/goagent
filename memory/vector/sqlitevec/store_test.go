package sqlitevec_test

import (
	"context"
	"testing"

	"github.com/Germanblandin1/goagent/memory/vector/sqlitevec"
)

// TestNew_MissingRequiredFields and TestMigrate_ZeroDimsReturnsError do not
// require a database — they only validate constructor error paths.

func TestNew_MissingRequiredFields(t *testing.T) {
	cases := []struct {
		name string
		cfg  sqlitevec.TableConfig
	}{
		{"missing Table", sqlitevec.TableConfig{IDColumn: "id", VectorColumn: "emb", TextColumn: "txt"}},
		{"missing IDColumn", sqlitevec.TableConfig{Table: "t", VectorColumn: "emb", TextColumn: "txt"}},
		{"missing VectorColumn", sqlitevec.TableConfig{Table: "t", IDColumn: "id", TextColumn: "txt"}},
		{"missing TextColumn", sqlitevec.TableConfig{Table: "t", IDColumn: "id", VectorColumn: "emb"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := sqlitevec.New(nil, tc.cfg)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

func TestMigrate_ZeroDimsReturnsError(t *testing.T) {
	err := sqlitevec.Migrate(context.Background(), nil, sqlitevec.MigrateConfig{})
	if err == nil {
		t.Fatal("expected error for zero Dims, got nil")
	}
}
