package sqlitevec

import (
	"encoding/binary"
	"encoding/json"
	"math"
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
