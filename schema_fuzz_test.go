package goagent_test

import (
	"encoding/json"
	"testing"

	"github.com/Germanblandin1/goagent"
)

// FuzzSchemaFrom verifies that SchemaFrom never panics for any field value
// combination and always returns a valid JSON Schema map with type "object".
//
// Run with: go test -fuzz=FuzzSchemaFrom -fuzztime=60s .
func FuzzSchemaFrom(f *testing.F) {
	// Seed corpus: representative field value combinations.
	f.Add("", 0, false, 0.0)
	f.Add("hello", 42, true, 3.14)
	f.Add("utf8: 日本語", -1, false, -9999.99)
	f.Add("tab\there", 0, true, 1e38)
	f.Add(`"quoted"`, 0, false, 0.0)

	f.Fuzz(func(t *testing.T, s string, n int, b bool, fl float64) {
		type S struct {
			Name  string  `json:"name"`
			Count int     `json:"count,omitempty"`
			Flag  bool    `json:"flag"`
			Score float64 `json:"score"`
		}

		defer func() {
			if r := recover(); r != nil {
				t.Errorf("SchemaFrom panicked with s=%q n=%d b=%v fl=%v: %v", s, n, b, fl, r)
			}
		}()

		schema := goagent.SchemaFrom(S{Name: s, Count: n, Flag: b, Score: fl})

		if schema == nil {
			t.Fatal("SchemaFrom returned nil")
		}
		if schema["type"] != "object" {
			t.Fatalf("schema[type] = %v, want 'object'", schema["type"])
		}

		// Must be JSON-serializable — the schema is always sent to the model.
		if _, err := json.Marshal(schema); err != nil {
			t.Errorf("schema is not JSON-serializable: %v", err)
		}
	})
}
