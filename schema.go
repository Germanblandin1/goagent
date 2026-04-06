package goagent

import (
	"reflect"
	"strings"
)

// Schema is a JSON Schema object represented as a plain map. It is
// interchangeable with map[string]any and is the type expected by
// [ToolDefinition.Parameters].
type Schema = map[string]any

// SchemaFrom derives a JSON Schema from a struct value using reflection.
// Pass an empty struct literal so the caller does not need to import reflect:
//
//	schema := goagent.SchemaFrom(struct {
//	    Query string `json:"query" jsonschema_description:"The search query"`
//	    TopK  int    `json:"top_k,omitempty"`
//	}{})
//
// # Supported tags
//
//   - json:"name"             → property name in the schema; "-" skips the field
//   - json:"name,omitempty"   → field is optional (not added to "required")
//   - jsonschema_description:"text" → adds a "description" key to the property
//   - jsonschema_enum:"a,b,c" → adds an "enum" key with the comma-separated values
//
// # Type mapping
//
//	string              → "string"
//	int / int8…int64 /
//	uint / uint8…uint64 → "integer"
//	float32 / float64   → "number"
//	bool                → "boolean"
//	[]T / [n]T          → "array"
//	anything else       → "string"  (conservative fallback)
//
// Note: slice and array fields map to {"type": "array"} without an "items"
// property — the element type is not reflected. If the model needs to know
// the element type, build the schema manually or augment the returned map
// after calling SchemaFrom.
//
// Pointer arguments are dereferenced before inspection. For non-struct types
// (after pointer dereferencing), SchemaFrom returns {"type":"object"} without
// panicking.
func SchemaFrom(v any) Schema {
	t := reflect.TypeOf(v)
	if t == nil {
		return Schema{"type": "object"}
	}
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return Schema{"type": "object"}
	}

	properties := make(map[string]any)
	var required []string

	for i := range t.NumField() {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}

		jsonTag := field.Tag.Get("json")
		if jsonTag == "-" {
			continue
		}

		parts := strings.SplitN(jsonTag, ",", 2)
		fieldName := parts[0]
		if fieldName == "" {
			fieldName = field.Name
		}

		optional := len(parts) > 1 && strings.Contains(parts[1], "omitempty")

		prop := map[string]any{
			"type": jsonSchemaType(field.Type),
		}
		if desc := field.Tag.Get("jsonschema_description"); desc != "" {
			prop["description"] = desc
		}
		if enum := field.Tag.Get("jsonschema_enum"); enum != "" {
			prop["enum"] = strings.Split(enum, ",")
		}

		properties[fieldName] = prop
		if !optional {
			required = append(required, fieldName)
		}
	}

	schema := Schema{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

// jsonSchemaType maps a Go reflect.Type to a JSON Schema type string.
func jsonSchemaType(t reflect.Type) string {
	switch t.Kind() {
	case reflect.String:
		return "string"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "integer"
	case reflect.Float32, reflect.Float64:
		return "number"
	case reflect.Bool:
		return "boolean"
	case reflect.Slice, reflect.Array:
		return "array"
	default:
		return "string"
	}
}
