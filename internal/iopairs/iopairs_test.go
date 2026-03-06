package iopairs

import (
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// LoadLibrary: parses YAML
// ---------------------------------------------------------------------------

func TestLoadLibrary_ParsesYAML(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "lib.yaml")

	yamlContent := `name: order_lookup
tool: order_lookup
pairs:
  - id: pair1
    description: "happy path"
    input:
      order_id: "123"
    output:
      status: "shipped"
    category: good
    tags:
      - smoke
  - id: pair2
    description: "not found"
    input:
      order_id: "999"
    output:
      error: "not found"
    category: bad
    tags:
      - edge
`
	if err := os.WriteFile(path, []byte(yamlContent), 0o644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}

	lib, err := LoadLibrary(path)
	if err != nil {
		t.Fatalf("LoadLibrary: %v", err)
	}

	if lib.Name != "order_lookup" {
		t.Errorf("Name = %q, want %q", lib.Name, "order_lookup")
	}
	if lib.Tool != "order_lookup" {
		t.Errorf("Tool = %q, want %q", lib.Tool, "order_lookup")
	}
	if len(lib.Pairs) != 2 {
		t.Fatalf("len(Pairs) = %d, want 2", len(lib.Pairs))
	}

	p := lib.Pairs[0]
	if p.ID != "pair1" {
		t.Errorf("Pairs[0].ID = %q, want %q", p.ID, "pair1")
	}
	if p.Category != "good" {
		t.Errorf("Pairs[0].Category = %q, want %q", p.Category, "good")
	}
	if len(p.Tags) != 1 || p.Tags[0] != "smoke" {
		t.Errorf("Pairs[0].Tags = %v, want [smoke]", p.Tags)
	}
}

func TestLoadLibrary_FileNotFound(t *testing.T) {
	_, err := LoadLibrary("/nonexistent/path/lib.yaml")
	if err == nil {
		t.Error("expected error for non-existent file")
	}
}

func TestLoadLibrary_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "bad.yaml")

	if err := os.WriteFile(path, []byte("name: [unclosed bracket\n  bad:\n    - {{invalid"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := LoadLibrary(path)
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

// ---------------------------------------------------------------------------
// GoodPairs / BadPairs: filtering
// ---------------------------------------------------------------------------

func TestGoodPairs(t *testing.T) {
	lib := &Library{
		Pairs: []Pair{
			{ID: "p1", Category: "good"},
			{ID: "p2", Category: "bad"},
			{ID: "p3", Category: "good"},
			{ID: "p4", Category: "edge"},
		},
	}

	good := lib.GoodPairs()
	if len(good) != 2 {
		t.Fatalf("len(GoodPairs) = %d, want 2", len(good))
	}
	if good[0].ID != "p1" {
		t.Errorf("GoodPairs[0].ID = %q, want %q", good[0].ID, "p1")
	}
	if good[1].ID != "p3" {
		t.Errorf("GoodPairs[1].ID = %q, want %q", good[1].ID, "p3")
	}
}

func TestBadPairs(t *testing.T) {
	lib := &Library{
		Pairs: []Pair{
			{ID: "p1", Category: "good"},
			{ID: "p2", Category: "bad"},
			{ID: "p3", Category: "bad"},
			{ID: "p4", Category: "edge"},
		},
	}

	bad := lib.BadPairs()
	if len(bad) != 2 {
		t.Fatalf("len(BadPairs) = %d, want 2", len(bad))
	}
	if bad[0].ID != "p2" {
		t.Errorf("BadPairs[0].ID = %q, want %q", bad[0].ID, "p2")
	}
	if bad[1].ID != "p3" {
		t.Errorf("BadPairs[1].ID = %q, want %q", bad[1].ID, "p3")
	}
}

func TestGoodPairs_Empty(t *testing.T) {
	lib := &Library{
		Pairs: []Pair{
			{ID: "p1", Category: "bad"},
		},
	}
	good := lib.GoodPairs()
	if len(good) != 0 {
		t.Errorf("expected 0 good pairs, got %d", len(good))
	}
}

func TestBadPairs_Empty(t *testing.T) {
	lib := &Library{
		Pairs: []Pair{
			{ID: "p1", Category: "good"},
		},
	}
	bad := lib.BadPairs()
	if len(bad) != 0 {
		t.Errorf("expected 0 bad pairs, got %d", len(bad))
	}
}

// ---------------------------------------------------------------------------
// DeriveAssertions: generates correct assertion types
// ---------------------------------------------------------------------------

func TestDeriveAssertions_OutputSchema(t *testing.T) {
	lib := &Library{
		Pairs: []Pair{
			{
				ID:       "good1",
				Category: "good",
				Output: map[string]interface{}{
					"status": "shipped",
					"total":  42.5,
				},
			},
		},
	}

	assertions := DeriveAssertions(lib)

	// Should have: 1 output_schema + 2 required_field = 3 total.
	schemaCount := 0
	fieldCount := 0
	for _, a := range assertions {
		switch a.Type {
		case "output_schema":
			schemaCount++
			if a.Schema == nil {
				t.Error("output_schema assertion should have non-nil Schema")
			}
			if !a.HardGate {
				t.Error("output_schema should be a hard gate")
			}
			if a.Source != "io_pair:good1" {
				t.Errorf("Source = %q, want %q", a.Source, "io_pair:good1")
			}
		case "required_field":
			fieldCount++
			if a.Field != "status" && a.Field != "total" {
				t.Errorf("unexpected required_field: %q", a.Field)
			}
			if a.HardGate {
				t.Error("required_field should NOT be a hard gate")
			}
		}
	}

	if schemaCount != 1 {
		t.Errorf("expected 1 output_schema assertion, got %d", schemaCount)
	}
	if fieldCount != 2 {
		t.Errorf("expected 2 required_field assertions, got %d", fieldCount)
	}
}

func TestDeriveAssertions_ForbiddenContent(t *testing.T) {
	lib := &Library{
		Pairs: []Pair{
			{
				ID:       "bad1",
				Category: "bad",
				Output: map[string]interface{}{
					"forbidden_content": "credit card number",
				},
			},
		},
	}

	assertions := DeriveAssertions(lib)

	found := false
	for _, a := range assertions {
		if a.Type == "forbidden_content" {
			found = true
			if a.ForbiddenContent != "credit card number" {
				t.Errorf("ForbiddenContent = %q, want %q", a.ForbiddenContent, "credit card number")
			}
			if !a.HardGate {
				t.Error("forbidden_content should be a hard gate")
			}
			if a.Source != "io_pair:bad1" {
				t.Errorf("Source = %q, want %q", a.Source, "io_pair:bad1")
			}
		}
	}

	if !found {
		t.Error("expected a forbidden_content assertion")
	}
}

func TestDeriveAssertions_EmptyLibrary(t *testing.T) {
	lib := &Library{Pairs: []Pair{}}

	assertions := DeriveAssertions(lib)
	if len(assertions) != 0 {
		t.Errorf("expected 0 assertions for empty library, got %d", len(assertions))
	}
}

func TestDeriveAssertions_EmptyOutput(t *testing.T) {
	lib := &Library{
		Pairs: []Pair{
			{
				ID:       "good1",
				Category: "good",
				Output:   map[string]interface{}{},
			},
		},
	}

	assertions := DeriveAssertions(lib)
	// With empty output, inferSchema returns nil so no output_schema and no required_field.
	if len(assertions) != 0 {
		t.Errorf("expected 0 assertions for empty output, got %d", len(assertions))
	}
}

// ---------------------------------------------------------------------------
// inferSchema: returns correct JSON types
// ---------------------------------------------------------------------------

func TestInferSchema_StringField(t *testing.T) {
	output := map[string]interface{}{"name": "Alice"}
	schema := inferSchema(output)

	assertFieldType(t, schema, "name", "string")
}

func TestInferSchema_NumberField(t *testing.T) {
	output := map[string]interface{}{"total": 42.5}
	schema := inferSchema(output)

	assertFieldType(t, schema, "total", "number")
}

func TestInferSchema_BoolField(t *testing.T) {
	output := map[string]interface{}{"active": true}
	schema := inferSchema(output)

	assertFieldType(t, schema, "active", "boolean")
}

func TestInferSchema_ArrayField(t *testing.T) {
	output := map[string]interface{}{"items": []interface{}{"a", "b"}}
	schema := inferSchema(output)

	assertFieldType(t, schema, "items", "array")
}

func TestInferSchema_ObjectField(t *testing.T) {
	output := map[string]interface{}{"nested": map[string]interface{}{"key": "val"}}
	schema := inferSchema(output)

	assertFieldType(t, schema, "nested", "object")
}

func TestInferSchema_NilOutput(t *testing.T) {
	schema := inferSchema(nil)
	if schema != nil {
		t.Errorf("expected nil schema for nil output, got %v", schema)
	}
}

func TestInferSchema_EmptyOutput(t *testing.T) {
	schema := inferSchema(map[string]interface{}{})
	if schema != nil {
		t.Errorf("expected nil schema for empty output, got %v", schema)
	}
}

func TestInferSchema_Structure(t *testing.T) {
	output := map[string]interface{}{"name": "Alice", "age": 30.0}
	schema := inferSchema(output)

	if schema == nil {
		t.Fatal("expected non-nil schema")
	}
	if schema["type"] != "object" {
		t.Errorf("schema type = %v, want object", schema["type"])
	}

	props, ok := schema["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("schema properties should be a map")
	}
	if len(props) != 2 {
		t.Errorf("expected 2 properties, got %d", len(props))
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func assertFieldType(t *testing.T, schema map[string]interface{}, field, expectedType string) {
	t.Helper()

	if schema == nil {
		t.Fatalf("schema is nil")
	}

	props, ok := schema["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("schema properties is not a map")
	}

	fieldSchema, ok := props[field].(map[string]interface{})
	if !ok {
		t.Fatalf("property %q is not a map", field)
	}

	got := fieldSchema["type"]
	if got != expectedType {
		t.Errorf("field %q type = %v, want %q", field, got, expectedType)
	}
}
