package scenario

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// YAML parsing of Scenario struct
// ---------------------------------------------------------------------------

func TestScenarioYAMLParsing_FullDocument(t *testing.T) {
	raw := `
scenario: refund_happy_path
description: Agent processes a valid refund request
input:
  messages:
    - role: user
      content: I want a refund for order 12345
world:
  tools:
    order_lookup: nominal
    refund_processor: nominal
  databases:
    orders:
      seed_sets:
        - default_orders
assertions:
  - type: tool_sequence
    required:
      - refund_processor
  - type: output_schema
    schema:
      type: object
chaos: false
tags:
  - refund
  - happy-path
beta_model: true
beta_reason: requires tool chaining
`
	var s Scenario
	if err := yaml.Unmarshal([]byte(raw), &s); err != nil {
		t.Fatalf("failed to unmarshal scenario YAML: %v", err)
	}
	if s.Name != "refund_happy_path" {
		t.Errorf("Name = %q, want %q", s.Name, "refund_happy_path")
	}
	if s.Description != "Agent processes a valid refund request" {
		t.Errorf("Description = %q, want expected value", s.Description)
	}
	if len(s.Input.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(s.Input.Messages))
	}
	if s.Input.Messages[0].Role != "user" {
		t.Errorf("Messages[0].Role = %q, want %q", s.Input.Messages[0].Role, "user")
	}
	if s.Input.Messages[0].Content != "I want a refund for order 12345" {
		t.Errorf("Messages[0].Content mismatch")
	}
	if len(s.World.Tools) != 2 {
		t.Errorf("expected 2 tools, got %d", len(s.World.Tools))
	}
	if s.World.Tools["order_lookup"] != "nominal" {
		t.Errorf("World.Tools[order_lookup] = %q, want %q", s.World.Tools["order_lookup"], "nominal")
	}
	if s.World.Tools["refund_processor"] != "nominal" {
		t.Errorf("World.Tools[refund_processor] = %q, want %q", s.World.Tools["refund_processor"], "nominal")
	}
	if db, ok := s.World.Databases["orders"]; !ok {
		t.Error("expected database 'orders' in World.Databases")
	} else if len(db.SeedSets) != 1 || db.SeedSets[0] != "default_orders" {
		t.Errorf("SeedSets = %v, want [default_orders]", db.SeedSets)
	}
	if len(s.Assertions) != 2 {
		t.Fatalf("expected 2 assertions, got %d", len(s.Assertions))
	}
	if s.Assertions[0].Type != "tool_sequence" {
		t.Errorf("Assertions[0].Type = %q, want %q", s.Assertions[0].Type, "tool_sequence")
	}
	if s.Assertions[1].Type != "output_schema" {
		t.Errorf("Assertions[1].Type = %q, want %q", s.Assertions[1].Type, "output_schema")
	}
	if s.Chaos {
		t.Error("Chaos should be false")
	}
	if len(s.Tags) != 2 || s.Tags[0] != "refund" || s.Tags[1] != "happy-path" {
		t.Errorf("Tags = %v, want [refund, happy-path]", s.Tags)
	}
	if !s.BetaModel {
		t.Error("BetaModel should be true")
	}
	if s.BetaReason != "requires tool chaining" {
		t.Errorf("BetaReason = %q, want %q", s.BetaReason, "requires tool chaining")
	}
}

func TestScenarioYAMLParsing_PayloadInput(t *testing.T) {
	raw := `
scenario: raw_payload_test
description: Tests raw payload input
input:
  payload:
    query: "SELECT * FROM users"
    limit: 10
world:
  tools: {}
assertions:
  - type: sensitive_leak
`
	var s Scenario
	if err := yaml.Unmarshal([]byte(raw), &s); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if s.Name != "raw_payload_test" {
		t.Errorf("Name = %q", s.Name)
	}
	if len(s.Input.Messages) != 0 {
		t.Errorf("expected no messages, got %d", len(s.Input.Messages))
	}
	if s.Input.Payload["query"] != "SELECT * FROM users" {
		t.Errorf("Payload[query] = %v", s.Input.Payload["query"])
	}
	// YAML parses numbers as int by default; verify it round-trips
	if limit, ok := s.Input.Payload["limit"]; !ok {
		t.Error("expected 'limit' in Payload")
	} else if limit != 10 {
		t.Errorf("Payload[limit] = %v (type %T)", limit, limit)
	}
}

func TestScenarioYAMLParsing_AssertionInlineFields(t *testing.T) {
	raw := `
scenario: inline_test
description: Check inline assertion fields
input:
  messages:
    - role: user
      content: hello
world:
  tools: {}
assertions:
  - type: tool_args_invariant
    tool: lookup_order
    invariant: "args.order_id == input.order_id"
`
	var s Scenario
	if err := yaml.Unmarshal([]byte(raw), &s); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if len(s.Assertions) != 1 {
		t.Fatalf("expected 1 assertion, got %d", len(s.Assertions))
	}
	a := s.Assertions[0]
	if a.Type != "tool_args_invariant" {
		t.Errorf("Type = %q", a.Type)
	}
	if a.Raw["tool"] != "lookup_order" {
		t.Errorf("Raw[tool] = %v", a.Raw["tool"])
	}
	if a.Raw["invariant"] != "args.order_id == input.order_id" {
		t.Errorf("Raw[invariant] = %v", a.Raw["invariant"])
	}
}

func TestScenarioYAMLParsing_ChaosTrue(t *testing.T) {
	raw := `
scenario: chaos_scenario
description: Multi-fault chaos scenario
input:
  messages:
    - role: user
      content: test
world:
  tools:
    a: timeout
    b: server_error
assertions: []
chaos: true
`
	var s Scenario
	if err := yaml.Unmarshal([]byte(raw), &s); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if !s.Chaos {
		t.Error("Chaos should be true")
	}
}

// ---------------------------------------------------------------------------
// LoadFile
// ---------------------------------------------------------------------------

func writeTemp(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write temp file %s: %v", path, err)
	}
	return path
}

func TestLoadFile_ValidScenario(t *testing.T) {
	dir := t.TempDir()
	yaml := `
scenario: load_test
description: valid file
input:
  messages:
    - role: user
      content: ping
world:
  tools:
    echo: nominal
assertions:
  - type: output_schema
    schema:
      type: object
`
	path := writeTemp(t, dir, "test.yaml", yaml)
	s, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile failed: %v", err)
	}
	if s.Name != "load_test" {
		t.Errorf("Name = %q, want %q", s.Name, "load_test")
	}
	if s.Description != "valid file" {
		t.Errorf("Description = %q", s.Description)
	}
	if len(s.Input.Messages) != 1 {
		t.Errorf("expected 1 message, got %d", len(s.Input.Messages))
	}
	if s.World.Tools["echo"] != "nominal" {
		t.Errorf("Tools[echo] = %q", s.World.Tools["echo"])
	}
}

func TestLoadFile_MissingNameField(t *testing.T) {
	dir := t.TempDir()
	yaml := `
description: no name
input:
  messages:
    - role: user
      content: test
`
	path := writeTemp(t, dir, "noname.yaml", yaml)
	_, err := LoadFile(path)
	if err == nil {
		t.Fatal("expected error for missing 'scenario' field")
	}
	if got := err.Error(); !contains(got, "missing required field 'scenario'") {
		t.Errorf("unexpected error: %s", got)
	}
}

func TestLoadFile_MissingInput(t *testing.T) {
	dir := t.TempDir()
	yaml := `
scenario: empty_input
input: {}
`
	path := writeTemp(t, dir, "noinput.yaml", yaml)
	_, err := LoadFile(path)
	if err == nil {
		t.Fatal("expected error for missing messages and payload")
	}
	if got := err.Error(); !contains(got, "must have either 'messages' or 'payload'") {
		t.Errorf("unexpected error: %s", got)
	}
}

func TestLoadFile_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := writeTemp(t, dir, "bad.yaml", ":::not valid yaml[[[")
	_, err := LoadFile(path)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestLoadFile_NonexistentFile(t *testing.T) {
	_, err := LoadFile("/nonexistent/path/scenario.yaml")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestLoadFile_PayloadInput(t *testing.T) {
	dir := t.TempDir()
	yaml := `
scenario: payload_test
input:
  payload:
    key: value
`
	path := writeTemp(t, dir, "payload.yaml", yaml)
	s, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile failed: %v", err)
	}
	if s.Name != "payload_test" {
		t.Errorf("Name = %q", s.Name)
	}
	if s.Input.Payload["key"] != "value" {
		t.Errorf("Payload[key] = %v", s.Input.Payload["key"])
	}
}

func TestLoadFile_InvalidAssertionType(t *testing.T) {
	dir := t.TempDir()
	yaml := `
scenario: invalid_assertion_type
input:
  messages:
    - role: user
      content: hello
assertions:
  - type: totally_unknown_assertion
`
	path := writeTemp(t, dir, "invalid_assertion.yaml", yaml)
	_, err := LoadFile(path)
	if err == nil {
		t.Fatal("expected schema validation error for unknown assertion type")
	}
	if got := err.Error(); !contains(got, "schema validation failed") {
		t.Fatalf("unexpected error: %s", got)
	}
}

func TestLoadFile_MissingRequiredAssertionField(t *testing.T) {
	dir := t.TempDir()
	yaml := `
scenario: invalid_assertion_shape
input:
  messages:
    - role: user
      content: hello
assertions:
  - type: tool_args_invariant
    tool: order_lookup
`
	path := writeTemp(t, dir, "invalid_assertion_shape.yaml", yaml)
	_, err := LoadFile(path)
	if err == nil {
		t.Fatal("expected schema validation error for missing assertion field")
	}
	if got := err.Error(); !contains(got, "schema validation failed") {
		t.Fatalf("unexpected error: %s", got)
	}
}

// ---------------------------------------------------------------------------
// LoadSuite
// ---------------------------------------------------------------------------

func TestLoadSuite_MultipleScenariosYAMLAndYML(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, "a.yaml", `
scenario: alpha
input:
  messages:
    - role: user
      content: hello
assertions: []
`)
	writeTemp(t, dir, "b.yml", `
scenario: beta
input:
  messages:
    - role: user
      content: world
assertions: []
`)
	// Non-YAML files should be ignored
	writeTemp(t, dir, "readme.txt", "ignore me")

	scenarios, err := LoadSuite(dir)
	if err != nil {
		t.Fatalf("LoadSuite failed: %v", err)
	}
	if len(scenarios) != 2 {
		t.Fatalf("expected 2 scenarios, got %d", len(scenarios))
	}
	names := map[string]bool{}
	for _, s := range scenarios {
		names[s.Name] = true
	}
	if !names["alpha"] || !names["beta"] {
		t.Errorf("unexpected scenario names: %v", names)
	}
}

func TestLoadSuite_EmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	_, err := LoadSuite(dir)
	if err == nil {
		t.Fatal("expected error for empty suite directory")
	}
}

func TestLoadSuite_NonexistentDirectory(t *testing.T) {
	_, err := LoadSuite("/nonexistent/dir/suite")
	if err == nil {
		t.Fatal("expected error for nonexistent directory")
	}
}

// ---------------------------------------------------------------------------
// ExpandMatrix (v1 passthrough)
// ---------------------------------------------------------------------------

func TestExpandMatrix_Passthrough(t *testing.T) {
	scenarios := []*Scenario{
		{Name: "a"},
		{Name: "b"},
		{Name: "c"},
	}
	result := ExpandMatrix(scenarios)
	if len(result) != len(scenarios) {
		t.Fatalf("expected %d scenarios, got %d", len(scenarios), len(result))
	}
	for i, s := range result {
		if s != scenarios[i] {
			t.Errorf("result[%d] is not the same pointer as input[%d]", i, i)
		}
	}
}

func TestExpandMatrix_NilInput(t *testing.T) {
	result := ExpandMatrix(nil)
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}
}

func TestExpandMatrix_EmptySlice(t *testing.T) {
	result := ExpandMatrix([]*Scenario{})
	if len(result) != 0 {
		t.Errorf("expected 0 scenarios, got %d", len(result))
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
