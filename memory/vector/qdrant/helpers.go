package qdrant

import (
	"encoding/binary"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/qdrant/go-client/qdrant"
)

// qdrantNamespace is the UUID v5 namespace used to derive deterministic point
// IDs from arbitrary string identifiers. Stable across releases — changing it
// would invalidate all existing point IDs.
var qdrantNamespace = uuid.MustParse("6ba7b810-9dad-11d1-80b4-00c04fd430c8")

// stringToPointID derives a deterministic uint64 from an arbitrary string id
// via UUID v5 (SHA-1 over namespace + name). The same string always produces
// the same uint64, so Upsert is idempotent. Collision probability is negligible
// for realistic dataset sizes.
func stringToPointID(id string) uint64 {
	u := uuid.NewSHA1(qdrantNamespace, []byte(id))
	return binary.BigEndian.Uint64(u[:8])
}

// payloadToQdrant converts a map[string]any to the protobuf Value map that
// the Qdrant go-client expects in UpsertPoints.
func payloadToQdrant(m map[string]any) map[string]*qdrant.Value {
	result := make(map[string]*qdrant.Value, len(m))
	for k, v := range m {
		result[k] = anyToValue(v)
	}
	return result
}

// anyToValue converts a Go value to a Qdrant protobuf Value.
// Handles string, float64, bool, map[string]any, []any, and nil.
// Unknown types are JSON-encoded to a string as a fallback.
func anyToValue(v any) *qdrant.Value {
	switch t := v.(type) {
	case string:
		return qdrant.NewValueString(t)
	case float64:
		return qdrant.NewValueDouble(t)
	case bool:
		return qdrant.NewValueBool(t)
	case map[string]any:
		inner := make(map[string]*qdrant.Value, len(t))
		for k, iv := range t {
			inner[k] = anyToValue(iv)
		}
		return qdrant.NewValueStruct(&qdrant.Struct{Fields: inner})
	case []any:
		items := make([]*qdrant.Value, len(t))
		for i, item := range t {
			items[i] = anyToValue(item)
		}
		return qdrant.NewValueList(&qdrant.ListValue{Values: items})
	case nil:
		return qdrant.NewValueNull()
	default:
		b, _ := json.Marshal(v)
		return qdrant.NewValueString(string(b))
	}
}

// filterToConditions converts a map[string]any filter to qdrant Must conditions.
// Each key-value pair becomes an equality condition on "metadata.<key>" in the
// point payload. All conditions are combined with AND (Must).
//
// Supported value types: string, bool, float64 (whole numbers treated as int64),
// int64. Float values with a fractional part are skipped — qdrant has no native
// float equality condition. Unsupported or nil values are silently ignored.
func filterToConditions(filter map[string]any) []*qdrant.Condition {
	if len(filter) == 0 {
		return nil
	}
	conditions := make([]*qdrant.Condition, 0, len(filter))
	for k, v := range filter {
		key := "metadata." + k
		var cond *qdrant.Condition
		switch val := v.(type) {
		case string:
			cond = qdrant.NewMatchKeyword(key, val)
		case bool:
			cond = &qdrant.Condition{
				ConditionOneOf: &qdrant.Condition_Field{
					Field: &qdrant.FieldCondition{
						Key:   key,
						Match: &qdrant.Match{MatchValue: &qdrant.Match_Boolean{Boolean: val}},
					},
				},
			}
		case float64:
			// JSON numbers unmarshal as float64; treat whole numbers as integers.
			if i := int64(val); float64(i) == val {
				cond = &qdrant.Condition{
					ConditionOneOf: &qdrant.Condition_Field{
						Field: &qdrant.FieldCondition{
							Key:   key,
							Match: &qdrant.Match{MatchValue: &qdrant.Match_Integer{Integer: i}},
						},
					},
				}
			}
		case int64:
			cond = &qdrant.Condition{
				ConditionOneOf: &qdrant.Condition_Field{
					Field: &qdrant.FieldCondition{
						Key:   key,
						Match: &qdrant.Match{MatchValue: &qdrant.Match_Integer{Integer: val}},
					},
				},
			}
		}
		if cond != nil {
			conditions = append(conditions, cond)
		}
	}
	return conditions
}

// extractPayload reads "id", "content", and "metadata" keys from a Qdrant
// point payload and returns them as typed Go values. Returns an error if
// "content" is missing.
func extractPayload(payload map[string]*qdrant.Value) (text, id string, meta map[string]any, err error) {
	if v, ok := payload["id"]; ok {
		id = v.GetStringValue()
	}
	contentVal, ok := payload["content"]
	if !ok {
		return "", "", nil, fmt.Errorf("missing 'content' key in payload")
	}
	text = contentVal.GetStringValue()

	if metaVal, ok := payload["metadata"]; ok {
		meta = valueToMap(metaVal)
	}
	return text, id, meta, nil
}

// valueToMap converts a Qdrant Struct Value back to map[string]any.
func valueToMap(v *qdrant.Value) map[string]any {
	s := v.GetStructValue()
	if s == nil {
		return nil
	}
	result := make(map[string]any, len(s.Fields))
	for k, fv := range s.Fields {
		result[k] = valueToAny(fv)
	}
	return result
}

// valueToAny converts a Qdrant protobuf Value back to a Go any.
func valueToAny(v *qdrant.Value) any {
	switch k := v.Kind.(type) {
	case *qdrant.Value_StringValue:
		return k.StringValue
	case *qdrant.Value_DoubleValue:
		return k.DoubleValue
	case *qdrant.Value_BoolValue:
		return k.BoolValue
	case *qdrant.Value_StructValue:
		return valueToMap(v)
	case *qdrant.Value_ListValue:
		items := make([]any, len(k.ListValue.Values))
		for i, item := range k.ListValue.Values {
			items[i] = valueToAny(item)
		}
		return items
	default:
		return nil
	}
}
