package sqlitevec

import (
	"encoding/binary"
	"encoding/json"
	"math"
	"reflect"

	"github.com/Germanblandin1/goagent"
)

// serializeVec encodes []float32 to the little-endian binary format expected
// by sqlite-vec's vec0 virtual table and vec_distance_* SQL functions.
func serializeVec(v []float32) []byte {
	b := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(f))
	}
	return b
}

// metadataToJSON serializes map[string]any to a JSON string for a TEXT column.
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

// jsonToMetadata deserializes a JSON TEXT column value to map[string]any.
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

// filterByMetadata removes results whose metadata does not match all key-value
// pairs in filter. Matching is deep-equal: every key in filter must be present
// in the message's Metadata with the same value. Results without metadata are
// always excluded when filter is non-empty.
func filterByMetadata(results []goagent.ScoredMessage, filter map[string]any) []goagent.ScoredMessage {
	if len(filter) == 0 {
		return results
	}
	out := results[:0]
	for _, r := range results {
		if matchesFilter(r.Message.Metadata, filter) {
			out = append(out, r)
		}
	}
	return out
}

// matchesFilter reports whether meta contains all key-value pairs in filter.
// Uses reflect.DeepEqual for value comparison to correctly handle all types
// produced by JSON deserialization (string, float64, bool, nested maps, slices).
func matchesFilter(meta, filter map[string]any) bool {
	for k, want := range filter {
		got, ok := meta[k]
		if !ok || !reflect.DeepEqual(got, want) {
			return false
		}
	}
	return true
}
