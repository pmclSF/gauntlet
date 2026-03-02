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
