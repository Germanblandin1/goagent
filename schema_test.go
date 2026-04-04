package goagent_test

import (
	"reflect"
	"testing"

	"github.com/Germanblandin1/goagent"
)

func TestSchemaFrom(t *testing.T) {
	t.Parallel()

	type argsWithQ struct {
		Q string `json:"q"`
	}

	tests := []struct {
		name         string
		input        any
		wantType     string
		wantProps    map[string]map[string]any // fieldName → expected prop map
		wantRequired []string
	}{
		{
			name: "single required string field with description",
			input: struct {
				Query string `json:"query" jsonschema_description:"La búsqueda"`
			}{},
			wantType: "object",
			wantProps: map[string]map[string]any{
				"query": {"type": "string", "description": "La búsqueda"},
			},
			wantRequired: []string{"query"},
		},
		{
			name: "required and optional fields",
			input: struct {
				Query string `json:"query"`
				TopK  int    `json:"top_k,omitempty"`
			}{},
			wantType: "object",
			wantProps: map[string]map[string]any{
				"query": {"type": "string"},
				"top_k": {"type": "integer"},
			},
			wantRequired: []string{"query"},
		},
		{
			name: "json dash field is ignored",
			input: struct {
				Name   string `json:"name"`
				Hidden string `json:"-"`
			}{},
			wantType: "object",
			wantProps: map[string]map[string]any{
				"name": {"type": "string"},
			},
			wantRequired: []string{"name"},
		},
		{
			name:         "empty struct has no required key",
			input:        struct{}{},
			wantType:     "object",
			wantProps:    map[string]map[string]any{},
			wantRequired: nil,
		},
		{
			name:     "pointer to struct",
			input:    &argsWithQ{},
			wantType: "object",
			wantProps: map[string]map[string]any{
				"q": {"type": "string"},
			},
			wantRequired: []string{"q"},
		},
		{
			name:         "non-struct returns bare object schema",
			input:        []string{},
			wantType:     "object",
			wantProps:    nil,
			wantRequired: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			schema := goagent.SchemaFrom(tc.input)

			if got, ok := schema["type"].(string); !ok || got != tc.wantType {
				t.Errorf("type = %v, want %q", schema["type"], tc.wantType)
			}

			// Validate properties when expected.
			if tc.wantProps != nil {
				props, ok := schema["properties"].(map[string]any)
				if !ok {
					t.Fatalf("properties missing or wrong type: %T", schema["properties"])
				}
				if len(props) != len(tc.wantProps) {
					t.Errorf("properties count = %d, want %d", len(props), len(tc.wantProps))
				}
				for field, wantProp := range tc.wantProps {
					got, exists := props[field]
					if !exists {
						t.Errorf("property %q not found in schema", field)
						continue
					}
					gotMap, ok := got.(map[string]any)
					if !ok {
						t.Errorf("property %q is %T, want map[string]any", field, got)
						continue
					}
					if !reflect.DeepEqual(gotMap, wantProp) {
						t.Errorf("property %q = %v, want %v", field, gotMap, wantProp)
					}
				}
			}

			// Validate required.
			gotRequired, hasRequired := schema["required"]
			if tc.wantRequired == nil {
				if hasRequired {
					t.Errorf("expected no 'required' key, got %v", gotRequired)
				}
			} else {
				if !hasRequired {
					t.Fatalf("missing 'required' key")
				}
				req, ok := gotRequired.([]string)
				if !ok {
					t.Fatalf("required is %T, want []string", gotRequired)
				}
				if !reflect.DeepEqual(req, tc.wantRequired) {
					t.Errorf("required = %v, want %v", req, tc.wantRequired)
				}
			}
		})
	}
}

func TestSchemaFrom_Enum(t *testing.T) {
	t.Parallel()

	schema := goagent.SchemaFrom(struct {
		Op string `json:"op" jsonschema_enum:"add,sub,mul,div"`
	}{})

	props := schema["properties"].(map[string]any)
	prop, ok := props["op"].(map[string]any)
	if !ok {
		t.Fatal("property 'op' missing")
	}
	got, ok := prop["enum"].([]string)
	if !ok {
		t.Fatalf("enum is %T, want []string", prop["enum"])
	}
	want := []string{"add", "sub", "mul", "div"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("enum = %v, want %v", got, want)
	}
}

func TestSchemaFrom_TypeMapping(t *testing.T) {
	t.Parallel()

	schema := goagent.SchemaFrom(struct {
		S  string  `json:"s"`
		I  int     `json:"i"`
		I8 int8    `json:"i8"`
		U  uint    `json:"u"`
		F  float64 `json:"f"`
		B  bool    `json:"b"`
		Sl []int   `json:"sl"`
	}{})

	props := schema["properties"].(map[string]any)
	cases := map[string]string{
		"s":  "string",
		"i":  "integer",
		"i8": "integer",
		"u":  "integer",
		"f":  "number",
		"b":  "boolean",
		"sl": "array",
	}
	for field, want := range cases {
		prop, ok := props[field].(map[string]any)
		if !ok {
			t.Errorf("property %q missing", field)
			continue
		}
		if got := prop["type"]; got != want {
			t.Errorf("field %q type = %q, want %q", field, got, want)
		}
	}
}
