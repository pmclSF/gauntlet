package baseline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gauntlet-dev/gauntlet/internal/tut"
)

// ---------------------------------------------------------------------------
// Compare — tool sequence matching
// ---------------------------------------------------------------------------

func TestCompare_ToolSequence_Pass(t *testing.T) {
	bl := &Contract{
		ToolSequence: &ToolSequenceBaseline{
			Required: []string{"lookup_order", "check_status", "send_email"},
			Order:    "partial",
		},
	}

	trace := []tut.TraceEvent{
		{EventType: "tool_call", ToolName: "lookup_order"},
		{EventType: "tool_call", ToolName: "check_status"},
		{EventType: "tool_call", ToolName: "send_email"},
	}
	output := tut.AgentOutput{}

	mismatches := Compare(bl, trace, output)
	if len(mismatches) != 0 {
		t.Errorf("expected 0 mismatches, got %d: %v", len(mismatches), mismatches)
	}
}

func TestCompare_ToolSequence_Pass_WithExtraTools(t *testing.T) {
	bl := &Contract{
		ToolSequence: &ToolSequenceBaseline{
			Required: []string{"lookup_order", "send_email"},
			Order:    "partial",
		},
	}

	trace := []tut.TraceEvent{
		{EventType: "tool_call", ToolName: "lookup_order"},
		{EventType: "tool_call", ToolName: "extra_tool"},
		{EventType: "tool_call", ToolName: "send_email"},
	}
	output := tut.AgentOutput{}

	mismatches := Compare(bl, trace, output)
	if len(mismatches) != 0 {
		t.Errorf("expected 0 mismatches, got %d: %v", len(mismatches), mismatches)
	}
}

func TestCompare_ToolSequence_Fail_MissingTools(t *testing.T) {
	bl := &Contract{
		ToolSequence: &ToolSequenceBaseline{
			Required: []string{"lookup_order", "check_status", "send_email"},
			Order:    "partial",
		},
	}

	trace := []tut.TraceEvent{
		{EventType: "tool_call", ToolName: "lookup_order"},
		// check_status and send_email never called
	}
	output := tut.AgentOutput{}

	mismatches := Compare(bl, trace, output)
	if len(mismatches) != 1 {
		t.Fatalf("expected 1 mismatch, got %d", len(mismatches))
	}
	if mismatches[0].Field != "tool_sequence" {
		t.Errorf("mismatch field = %q, want %q", mismatches[0].Field, "tool_sequence")
	}
	if !strings.Contains(mismatches[0].Message, "missing tools") {
		t.Errorf("expected message to mention missing tools, got: %s", mismatches[0].Message)
	}
}

func TestCompare_ToolSequence_Fail_WrongOrder(t *testing.T) {
	bl := &Contract{
		ToolSequence: &ToolSequenceBaseline{
			Required: []string{"A", "B", "C"},
			Order:    "partial",
		},
	}

	// Called in wrong order: C then A then B
	trace := []tut.TraceEvent{
		{EventType: "tool_call", ToolName: "C"},
		{EventType: "tool_call", ToolName: "A"},
		{EventType: "tool_call", ToolName: "B"},
	}
	output := tut.AgentOutput{}

	mismatches := Compare(bl, trace, output)
	// The subsequence matcher finds C at pos 0, then can't find A or B in order after C
	// Actually: reqIdx=0 (looking for A). Sees C: no match. Sees A: match, reqIdx=1. Sees B: match, reqIdx=2. Now looking for C, index 2. No more events.
	// So C is missing. There should be 1 mismatch.
	if len(mismatches) != 1 {
		t.Fatalf("expected 1 mismatch for wrong-order tools, got %d", len(mismatches))
	}
}

func TestCompare_NilBaseline(t *testing.T) {
	mismatches := Compare(nil, nil, tut.AgentOutput{})
	if mismatches != nil {
		t.Errorf("expected nil mismatches for nil baseline, got %v", mismatches)
	}
}

// ---------------------------------------------------------------------------
// Compare — output checks
// ---------------------------------------------------------------------------

func TestCompare_RequiredFields_Pass(t *testing.T) {
	bl := &Contract{
		Output: &OutputBaseline{
			RequiredFields: []string{"name", "email"},
		},
	}

	output := tut.AgentOutput{
		Parsed: map[string]interface{}{
			"name":  "Alice",
			"email": "alice@example.com",
		},
	}

	mismatches := Compare(bl, nil, output)
	if len(mismatches) != 0 {
		t.Errorf("expected 0 mismatches, got %d: %v", len(mismatches), mismatches)
	}
}

func TestCompare_RequiredFields_Fail(t *testing.T) {
	bl := &Contract{
		Output: &OutputBaseline{
			RequiredFields: []string{"name", "email"},
		},
	}

	output := tut.AgentOutput{
		Parsed: map[string]interface{}{
			"name": "Alice",
			// email is missing
		},
	}

	mismatches := Compare(bl, nil, output)
	if len(mismatches) != 1 {
		t.Fatalf("expected 1 mismatch for missing required field, got %d", len(mismatches))
	}
	if mismatches[0].Field != "output.required_field" {
		t.Errorf("mismatch field = %q, want %q", mismatches[0].Field, "output.required_field")
	}
}

func TestCompare_ForbiddenContent_Pass(t *testing.T) {
	bl := &Contract{
		Output: &OutputBaseline{
			ForbiddenContent: []string{"INTERNAL_ERROR"},
		},
	}

	output := tut.AgentOutput{
		Raw:    []byte(`{"status": "ok"}`),
		Parsed: map[string]interface{}{"status": "ok"},
	}

	mismatches := Compare(bl, nil, output)
	if len(mismatches) != 0 {
		t.Errorf("expected 0 mismatches, got %d: %v", len(mismatches), mismatches)
	}
}

func TestCompare_ForbiddenContent_Fail(t *testing.T) {
	bl := &Contract{
		Output: &OutputBaseline{
			ForbiddenContent: []string{"INTERNAL_ERROR"},
		},
	}

	output := tut.AgentOutput{
		Raw:    []byte(`{"error": "INTERNAL_ERROR occurred"}`),
		Parsed: map[string]interface{}{"error": "INTERNAL_ERROR occurred"},
	}

	mismatches := Compare(bl, nil, output)
	if len(mismatches) != 1 {
		t.Fatalf("expected 1 mismatch for forbidden content, got %d", len(mismatches))
	}
	if mismatches[0].Field != "output.forbidden_content" {
		t.Errorf("mismatch field = %q, want %q", mismatches[0].Field, "output.forbidden_content")
	}
}

func TestCompare_Multiple_Mismatches(t *testing.T) {
	bl := &Contract{
		ToolSequence: &ToolSequenceBaseline{
			Required: []string{"step_a", "step_b"},
		},
		Output: &OutputBaseline{
			RequiredFields:   []string{"result"},
			ForbiddenContent: []string{"BAD_WORD"},
		},
	}

	trace := []tut.TraceEvent{
		// Missing step_a and step_b entirely
	}
	output := tut.AgentOutput{
		Raw:    []byte(`{"error": "BAD_WORD"}`),
		Parsed: map[string]interface{}{"error": "BAD_WORD"},
		// Missing "result" field
	}

	mismatches := Compare(bl, trace, output)
	// 1 for tool_sequence, 1 for missing required field, 1 for forbidden content
	if len(mismatches) != 3 {
		t.Errorf("expected 3 mismatches, got %d: %v", len(mismatches), mismatches)
	}
}

// ---------------------------------------------------------------------------
// Golden baseline
// ---------------------------------------------------------------------------

func TestLoadGolden_ReturnsExperimentalError(t *testing.T) {
	_, err := LoadGolden("/any/dir", "suite", "scenario")
	if err == nil {
		t.Fatal("expected error from LoadGolden, got nil")
	}
	if !strings.Contains(err.Error(), "experimental") {
		t.Errorf("error = %q, want it to contain 'experimental'", err.Error())
	}
	if !strings.Contains(err.Error(), "contract") {
		t.Errorf("error = %q, want it to mention 'contract' baselines", err.Error())
	}
}

// ---------------------------------------------------------------------------
// GenerateDiff
// ---------------------------------------------------------------------------

func TestGenerateDiff_NoMismatches(t *testing.T) {
	result := GenerateDiff(nil)
	if !strings.Contains(result, "No differences") {
		t.Errorf("expected 'No differences' message, got: %s", result)
	}
}

func TestGenerateDiff_WithMismatches(t *testing.T) {
	mismatches := []Mismatch{
		{
			Field:    "tool_sequence",
			Expected: "[A, B, C]",
			Actual:   "[A]",
			Message:  "missing tools: [B, C]",
		},
		{
			Field:    "output.required_field",
			Expected: "field 'email' present",
			Actual:   "field missing",
			Message:  "required output field 'email' is missing",
		},
	}

	result := GenerateDiff(mismatches)

	if !strings.Contains(result, "2 mismatches") {
		t.Errorf("expected header with mismatch count, got: %s", result)
	}
	if !strings.Contains(result, "tool_sequence") {
		t.Error("expected diff to contain 'tool_sequence'")
	}
	if !strings.Contains(result, "output.required_field") {
		t.Error("expected diff to contain 'output.required_field'")
	}
	if !strings.Contains(result, "Expected:") {
		t.Error("expected diff to contain 'Expected:'")
	}
	if !strings.Contains(result, "Actual:") {
		t.Error("expected diff to contain 'Actual:'")
	}
	if !strings.Contains(result, "Detail:") {
		t.Error("expected diff to contain 'Detail:'")
	}
}

func TestGenerateDiff_EmptyMessage(t *testing.T) {
	mismatches := []Mismatch{
		{
			Field:    "test_field",
			Expected: "something",
			Actual:   "other",
			Message:  "", // empty message
		},
	}

	result := GenerateDiff(mismatches)
	// Should not contain "Detail:" for empty message
	if strings.Contains(result, "Detail:") {
		t.Error("expected no 'Detail:' line for empty message")
	}
}

// ---------------------------------------------------------------------------
// Save and Load round-trip
// ---------------------------------------------------------------------------

func TestSaveAndLoad_RoundTrip(t *testing.T) {
	dir := t.TempDir()

	contract := &Contract{
		BaselineType: "contract",
		Scenario:     "test_scenario",
		RecordedAt:   "2025-01-01T00:00:00Z",
		Commit:       "abc123",
		ToolSequence: &ToolSequenceBaseline{
			Required: []string{"step_a", "step_b"},
			Order:    "partial",
		},
		Output: &OutputBaseline{
			RequiredFields:   []string{"status"},
			ForbiddenContent: []string{"error"},
		},
	}

	err := Save(dir, "my_suite", contract)
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Verify file exists
	path := filepath.Join(dir, "my_suite", "test_scenario.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected file at %s, got error: %v", path, err)
	}

	// Verify it is valid JSON
	var parsed Contract
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("saved file is not valid JSON: %v", err)
	}

	// Load it back
	loaded, err := Load(dir, "my_suite", "test_scenario")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded == nil {
		t.Fatal("Load returned nil contract")
	}
	if loaded.Scenario != "test_scenario" {
		t.Errorf("Scenario = %q, want %q", loaded.Scenario, "test_scenario")
	}
	if len(loaded.ToolSequence.Required) != 2 {
		t.Errorf("ToolSequence.Required len = %d, want 2", len(loaded.ToolSequence.Required))
	}
}

func TestLoad_NoBaselineReturnsNil(t *testing.T) {
	dir := t.TempDir()
	contract, err := Load(dir, "suite", "nonexistent")
	if err != nil {
		t.Fatalf("expected nil error for missing baseline, got: %v", err)
	}
	if contract != nil {
		t.Error("expected nil contract for missing baseline")
	}
}

// ---------------------------------------------------------------------------
// UnmarshalJSON — flat vs nested format
// ---------------------------------------------------------------------------

func TestUnmarshalJSON_FlatFormat(t *testing.T) {
	flat := `{
		"scenario": "order_status_nominal",
		"suite": "smoke",
		"tool_sequence": ["order_lookup"],
		"output_schema": {
			"type": "object",
			"required": ["response"]
		},
		"required_fields": ["response"],
		"forbidden_content": ["INTERNAL_ERROR"]
	}`

	var c Contract
	if err := json.Unmarshal([]byte(flat), &c); err != nil {
		t.Fatalf("Unmarshal flat format: %v", err)
	}

	if c.Scenario != "order_status_nominal" {
		t.Errorf("Scenario = %q, want order_status_nominal", c.Scenario)
	}
	if c.Suite != "smoke" {
		t.Errorf("Suite = %q, want smoke", c.Suite)
	}

	// ToolSequence must be parsed from the flat array
	if c.ToolSequence == nil {
		t.Fatal("ToolSequence is nil — flat format not parsed")
	}
	if len(c.ToolSequence.Required) != 1 || c.ToolSequence.Required[0] != "order_lookup" {
		t.Errorf("ToolSequence.Required = %v, want [order_lookup]", c.ToolSequence.Required)
	}
	if c.ToolSequence.Order != "partial" {
		t.Errorf("ToolSequence.Order = %q, want partial", c.ToolSequence.Order)
	}

	// Output must be assembled from flat fields
	if c.Output == nil {
		t.Fatal("Output is nil — flat output fields not parsed")
	}
	if len(c.Output.RequiredFields) != 1 || c.Output.RequiredFields[0] != "response" {
		t.Errorf("Output.RequiredFields = %v, want [response]", c.Output.RequiredFields)
	}
	if len(c.Output.ForbiddenContent) != 1 || c.Output.ForbiddenContent[0] != "INTERNAL_ERROR" {
		t.Errorf("Output.ForbiddenContent = %v, want [INTERNAL_ERROR]", c.Output.ForbiddenContent)
	}
	if c.Output.Schema == nil {
		t.Fatal("Output.Schema is nil")
	}
}

func TestUnmarshalJSON_NestedFormat(t *testing.T) {
	nested := `{
		"baseline_type": "contract",
		"scenario": "test",
		"recorded_at": "2025-01-01T00:00:00Z",
		"commit": "abc123",
		"tool_sequence": {
			"required": ["step_a", "step_b"],
			"order": "strict"
		},
		"output": {
			"required_fields": ["status"],
			"forbidden_content": ["error"]
		}
	}`

	var c Contract
	if err := json.Unmarshal([]byte(nested), &c); err != nil {
		t.Fatalf("Unmarshal nested format: %v", err)
	}

	if c.ToolSequence == nil {
		t.Fatal("ToolSequence is nil")
	}
	if len(c.ToolSequence.Required) != 2 {
		t.Errorf("ToolSequence.Required len = %d, want 2", len(c.ToolSequence.Required))
	}
	if c.ToolSequence.Order != "strict" {
		t.Errorf("ToolSequence.Order = %q, want strict", c.ToolSequence.Order)
	}

	if c.Output == nil {
		t.Fatal("Output is nil")
	}
	if len(c.Output.RequiredFields) != 1 || c.Output.RequiredFields[0] != "status" {
		t.Errorf("Output.RequiredFields = %v, want [status]", c.Output.RequiredFields)
	}
}

func TestUnmarshalJSON_FlatEmptyArrays(t *testing.T) {
	flat := `{
		"scenario": "test",
		"tool_sequence": [],
		"required_fields": ["response"],
		"forbidden_content": []
	}`

	var c Contract
	if err := json.Unmarshal([]byte(flat), &c); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if c.ToolSequence == nil {
		t.Fatal("ToolSequence is nil for empty array")
	}
	if len(c.ToolSequence.Required) != 0 {
		t.Errorf("ToolSequence.Required = %v, want empty", c.ToolSequence.Required)
	}

	if c.Output == nil {
		t.Fatal("Output is nil")
	}
	if len(c.Output.RequiredFields) != 1 {
		t.Errorf("Output.RequiredFields len = %d, want 1", len(c.Output.RequiredFields))
	}
}

func TestLoad_FlatFormatFile(t *testing.T) {
	dir := t.TempDir()
	suiteDir := filepath.Join(dir, "smoke")
	if err := os.MkdirAll(suiteDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	flat := `{
		"scenario": "order_status_nominal",
		"suite": "smoke",
		"tool_sequence": ["order_lookup"],
		"output_schema": {"type": "object", "required": ["response"]},
		"required_fields": ["response"],
		"forbidden_content": []
	}`
	if err := os.WriteFile(filepath.Join(suiteDir, "order_status_nominal.json"), []byte(flat), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	c, err := Load(dir, "smoke", "order_status_nominal")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c == nil {
		t.Fatal("Load returned nil")
	}
	if c.ToolSequence == nil {
		t.Fatal("ToolSequence is nil after Load of flat format")
	}
	if len(c.ToolSequence.Required) != 1 || c.ToolSequence.Required[0] != "order_lookup" {
		t.Errorf("ToolSequence.Required = %v, want [order_lookup]", c.ToolSequence.Required)
	}
	if c.Output == nil {
		t.Fatal("Output is nil after Load of flat format")
	}
	if len(c.Output.RequiredFields) != 1 || c.Output.RequiredFields[0] != "response" {
		t.Errorf("Output.RequiredFields = %v, want [response]", c.Output.RequiredFields)
	}
}

func TestCompare_FlatBaseline_ActuallyChecks(t *testing.T) {
	// This is the critical test: a flat-format baseline loaded via UnmarshalJSON
	// must produce real mismatches, not vacuously pass.
	flat := `{
		"scenario": "test",
		"tool_sequence": ["order_lookup"],
		"required_fields": ["response"],
		"forbidden_content": ["SECRET"]
	}`

	var c Contract
	if err := json.Unmarshal([]byte(flat), &c); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	// No tool calls, missing required field, has forbidden content
	trace := []tut.TraceEvent{}
	output := tut.AgentOutput{
		Raw:    []byte(`{"error": "SECRET leaked"}`),
		Parsed: map[string]interface{}{"error": "SECRET leaked"},
	}

	mismatches := Compare(&c, trace, output)
	if len(mismatches) != 3 {
		t.Errorf("expected 3 mismatches (tool_sequence, required_field, forbidden_content), got %d: %v",
			len(mismatches), mismatches)
	}
}

func TestSaveAndLoad_RoundTrip_WithOutput(t *testing.T) {
	dir := t.TempDir()

	contract := &Contract{
		BaselineType: "contract",
		Scenario:     "round_trip_test",
		RecordedAt:   "2025-01-01T00:00:00Z",
		Commit:       "abc123",
		ToolSequence: &ToolSequenceBaseline{
			Required: []string{"step_a", "step_b"},
			Order:    "partial",
		},
		Output: &OutputBaseline{
			Schema:           map[string]interface{}{"type": "object"},
			RequiredFields:   []string{"status", "message"},
			ForbiddenContent: []string{"error", "INTERNAL"},
		},
	}

	if err := Save(dir, "smoke", contract); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load(dir, "smoke", "round_trip_test")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded == nil {
		t.Fatal("Load returned nil")
	}
	if loaded.Output == nil {
		t.Fatal("Output is nil after round-trip")
	}
	if len(loaded.Output.RequiredFields) != 2 {
		t.Errorf("Output.RequiredFields len = %d, want 2", len(loaded.Output.RequiredFields))
	}
	if len(loaded.Output.ForbiddenContent) != 2 {
		t.Errorf("Output.ForbiddenContent len = %d, want 2", len(loaded.Output.ForbiddenContent))
	}
}
