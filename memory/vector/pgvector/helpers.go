package pgvector

import (
	"encoding/json"
	"fmt"
	"strings"
)

// float32SliceToLiteral converts []float32 to the pgvector literal format: "[v1,v2,v3]".
// pgvector accepts this format directly via $1::vector.
func float32SliceToLiteral(v []float32) string {
	if len(v) == 0 {
		return "[]"
	}
	var b strings.Builder
	b.WriteByte('[')
	for i, f := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "%g", f)
	}
	b.WriteByte(']')
	return b.String()
}

// metadataToJSON serializes map[string]any to a JSON string for a JSONB column.
// Returns "{}" if the map is nil or empty.
func metadataToJSON(m map[string]any) (string, error) {
	if len(m) == 0 {
		return "{}", nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// jsonToMetadata deserializes a JSONB string column value to map[string]any.
// Returns nil if the string is empty or "{}".
func jsonToMetadata(s string) (map[string]any, error) {
	if s == "" || s == "{}" {
		return nil, nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return nil, err
	}
	return m, nil
}
