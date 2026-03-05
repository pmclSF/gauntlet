package assertions

import (
	"encoding/json"
	"testing"

	"github.com/pmclSF/gauntlet/internal/scenario"
	"github.com/pmclSF/gauntlet/internal/tut"
)

// ---------------------------------------------------------------------------
// Registry tests
// ---------------------------------------------------------------------------

func TestRegistryContainsAllSevenTypes(t *testing.T) {
	expected := []string{
		"output_schema",
		"tool_sequence",
		"tool_args_invariant",
		"retry_cap",
		"forbidden_tool",
		"output_derivable",
		"sensitive_leak",
	}

	for _, name := range expected {
		a, ok := Get(name)
		if !ok {
			t.Errorf("registry missing assertion type %q", name)
			continue
		}
		if a.Type() != name {
			t.Errorf("Type() = %q, want %q", a.Type(), name)
		}
	}

	// Make sure we don't have extra unexpected types
	if len(registry) != len(expected) {
		t.Errorf("registry has %d types, want %d", len(registry), len(expected))
	}
}

func TestGetUnknownTypeReturnsFalse(t *testing.T) {
	_, ok := Get("nonexistent_type")
	if ok {
		t.Error("Get returned true for nonexistent type")
	}
}

// ---------------------------------------------------------------------------
// IsSoft tests
// ---------------------------------------------------------------------------

func TestIsSoftValues(t *testing.T) {
	// Hard assertions (IsSoft == false)
	hardTypes := []string{
		"output_schema",
		"tool_sequence",
		"tool_args_invariant",
		"retry_cap",
		"forbidden_tool",
	}
	for _, name := range hardTypes {
		a, ok := Get(name)
		if !ok {
			t.Fatalf("registry missing %q", name)
		}
		if a.IsSoft() {
			t.Errorf("%q.IsSoft() = true, want false (hard gate)", name)
		}
	}

	// Soft assertions (IsSoft == true)
	softTypes := []string{
		"output_derivable",
		"sensitive_leak",
	}
	for _, name := range softTypes {
		a, ok := Get(name)
		if !ok {
			t.Fatalf("registry missing %q", name)
		}
		if !a.IsSoft() {
			t.Errorf("%q.IsSoft() = false, want true (soft signal)", name)
		}
	}
}

// ---------------------------------------------------------------------------
// ToolSequenceAssertion
// ---------------------------------------------------------------------------

func TestToolSequence_Pass(t *testing.T) {
	a := &ToolSequenceAssertion{}
	ctx := Context{
		ToolTrace: []tut.TraceEvent{
			{EventType: "tool_call", ToolName: "lookup_order"},
			{EventType: "tool_call", ToolName: "check_status"},
			{EventType: "tool_call", ToolName: "send_email"},
		},
		Baseline: &ContractBaseline{
			ToolSequence: []string{"lookup_order", "check_status", "send_email"},
		},
	}

	result := a.Evaluate(ctx)
	if !result.Passed {
		t.Errorf("expected pass, got fail: %s", result.Message)
	}
}

func TestToolSequence_Pass_PartialOrder(t *testing.T) {
	// Required sequence appears in order but not consecutively
	a := &ToolSequenceAssertion{}
	ctx := Context{
		ToolTrace: []tut.TraceEvent{
			{EventType: "tool_call", ToolName: "lookup_order"},
			{EventType: "tool_call", ToolName: "extra_tool"},
			{EventType: "tool_call", ToolName: "check_status"},
		},
		Baseline: &ContractBaseline{
			ToolSequence: []string{"lookup_order", "check_status"},
		},
	}

	result := a.Evaluate(ctx)
	if !result.Passed {
		t.Errorf("expected pass with interleaved tools, got fail: %s", result.Message)
	}
}

func TestToolSequence_Fail_MissingTools(t *testing.T) {
	a := &ToolSequenceAssertion{}
	ctx := Context{
		ToolTrace: []tut.TraceEvent{
			{EventType: "tool_call", ToolName: "lookup_order"},
		},
		Baseline: &ContractBaseline{
			ToolSequence: []string{"lookup_order", "check_status", "send_email"},
		},
	}

	result := a.Evaluate(ctx)
	if result.Passed {
		t.Error("expected fail for missing tools, got pass")
	}
	if result.DocketHint != "planner.premature_finalize" {
		t.Errorf("DocketHint = %q, want %q", result.DocketHint, "planner.premature_finalize")
	}
}

func TestToolSequence_Pass_NoBaseline(t *testing.T) {
	a := &ToolSequenceAssertion{}
	ctx := Context{
		ToolTrace: []tut.TraceEvent{
			{EventType: "tool_call", ToolName: "anything"},
		},
	}

	result := a.Evaluate(ctx)
	if !result.Passed {
		t.Errorf("expected pass with no baseline, got fail: %s", result.Message)
	}
}

// ---------------------------------------------------------------------------
// ToolArgsAssertion
// ---------------------------------------------------------------------------

func TestToolArgs_Pass_NonNullArgs(t *testing.T) {
	a := &ToolArgsAssertion{}
	ctx := Context{
		ToolTrace: []tut.TraceEvent{
			{
				EventType: "tool_call",
				ToolName:  "lookup_order",
				Args:      json.RawMessage(`{"order_id": "123"}`),
			},
		},
	}

	result := a.Evaluate(ctx)
	if !result.Passed {
		t.Errorf("expected pass, got fail: %s", result.Message)
	}
}

func TestToolArgs_Fail_NullArgs(t *testing.T) {
	a := &ToolArgsAssertion{}
	ctx := Context{
		ToolTrace: []tut.TraceEvent{
			{
				EventType: "tool_call",
				ToolName:  "lookup_order",
				Args:      json.RawMessage(`null`),
			},
		},
	}

	result := a.Evaluate(ctx)
	if result.Passed {
		t.Error("expected fail for null args, got pass")
	}
	if result.DocketHint != "tool.args_invalid" {
		t.Errorf("DocketHint = %q, want %q", result.DocketHint, "tool.args_invalid")
	}
}

func TestToolArgs_Fail_EmptyArgs(t *testing.T) {
	a := &ToolArgsAssertion{}
	ctx := Context{
		ToolTrace: []tut.TraceEvent{
			{
				EventType: "tool_call",
				ToolName:  "lookup_order",
				Args:      json.RawMessage(``),
			},
		},
	}

	result := a.Evaluate(ctx)
	if result.Passed {
		t.Error("expected fail for empty args, got pass")
	}
}

func TestToolArgs_SkipsNonToolCallEvents(t *testing.T) {
	a := &ToolArgsAssertion{}
	ctx := Context{
		ToolTrace: []tut.TraceEvent{
			{EventType: "model_call", ToolName: ""},
		},
	}

	result := a.Evaluate(ctx)
	if !result.Passed {
		t.Errorf("expected pass for non-tool_call events, got fail: %s", result.Message)
	}
}

// ---------------------------------------------------------------------------
// EvaluateInvariant
// ---------------------------------------------------------------------------

func TestEvaluateInvariant_IsNotNull_Pass(t *testing.T) {
	args := json.RawMessage(`{"order_id": "12345"}`)
	ok, msg := EvaluateInvariant("args.order_id is not null", args)
	if !ok {
		t.Errorf("expected pass, got fail: %s", msg)
	}
}

func TestEvaluateInvariant_IsNotNull_Fail(t *testing.T) {
	args := json.RawMessage(`{"order_id": null}`)
	ok, msg := EvaluateInvariant("args.order_id is not null", args)
	if ok {
		t.Error("expected fail for null field, got pass")
	}
	if msg == "" {
		t.Error("expected non-empty failure message")
	}
}

func TestEvaluateInvariant_IsNotNull_MissingField(t *testing.T) {
	args := json.RawMessage(`{"other_field": "value"}`)
	ok, _ := EvaluateInvariant("args.order_id is not null", args)
	if ok {
		t.Error("expected fail for missing field, got pass")
	}
}

func TestEvaluateInvariant_Equals_Pass(t *testing.T) {
	args := json.RawMessage(`{"status": "active"}`)
	ok, msg := EvaluateInvariant("args.status == active", args)
	if !ok {
		t.Errorf("expected pass, got fail: %s", msg)
	}
}

func TestEvaluateInvariant_Equals_Fail(t *testing.T) {
	args := json.RawMessage(`{"status": "inactive"}`)
	ok, _ := EvaluateInvariant("args.status == active", args)
	if ok {
		t.Error("expected fail for mismatched value, got pass")
	}
}

func TestEvaluateInvariant_InvalidExpression(t *testing.T) {
	args := json.RawMessage(`{"x": 1}`)
	ok, msg := EvaluateInvariant("x y", args)
	if ok {
		t.Error("expected fail for invalid expression, got pass")
	}
	if msg == "" {
		t.Error("expected non-empty error message")
	}
}

func TestEvaluateInvariant_InvalidJSON(t *testing.T) {
	args := json.RawMessage(`not json`)
	ok, msg := EvaluateInvariant("args.x is not null", args)
	if ok {
		t.Error("expected fail for invalid JSON, got pass")
	}
	if msg == "" {
		t.Error("expected non-empty error message")
	}
}

func TestEvaluateInvariant_UnsupportedOperator(t *testing.T) {
	args := json.RawMessage(`{"x": 1}`)
	ok, msg := EvaluateInvariant("args.x != 1", args)
	if ok {
		t.Error("expected fail for unsupported operator, got pass")
	}
	if msg == "" {
		t.Error("expected non-empty error message for unsupported operator")
	}
}

// ---------------------------------------------------------------------------
// RetryCapAssertion
// ---------------------------------------------------------------------------

func TestRetryCap_Pass_WithinLimit(t *testing.T) {
	a := &RetryCapAssertion{}
	ctx := Context{
		ToolTrace: []tut.TraceEvent{
			{EventType: "tool_call", ToolName: "lookup"},
			{EventType: "tool_call", ToolName: "lookup"},
			{EventType: "tool_call", ToolName: "lookup"},
			{EventType: "tool_call", ToolName: "send"},
		},
		WorldState: WorldState{Tools: map[string]ToolState{}},
	}

	result := a.Evaluate(ctx)
	if !result.Passed {
		t.Errorf("expected pass (3 consecutive within cap of 3), got fail: %s", result.Message)
	}
}

func TestRetryCap_Fail_ExceedsLimit(t *testing.T) {
	a := &RetryCapAssertion{}
	ctx := Context{
		ToolTrace: []tut.TraceEvent{
			{EventType: "tool_call", ToolName: "lookup"},
			{EventType: "tool_call", ToolName: "lookup"},
			{EventType: "tool_call", ToolName: "lookup"},
			{EventType: "tool_call", ToolName: "lookup"},
		},
		WorldState: WorldState{Tools: map[string]ToolState{}},
	}

	result := a.Evaluate(ctx)
	if result.Passed {
		t.Error("expected fail for 4 consecutive calls (cap is 3), got pass")
	}
	if result.DocketHint != "planner.retry_storm" {
		t.Errorf("DocketHint = %q, want %q", result.DocketHint, "planner.retry_storm")
	}
}

func TestRetryCap_Fail_TimeoutState(t *testing.T) {
	a := &RetryCapAssertion{}
	ctx := Context{
		ToolTrace: []tut.TraceEvent{
			{EventType: "tool_call", ToolName: "lookup"},
			{EventType: "tool_call", ToolName: "lookup"},
			{EventType: "tool_call", ToolName: "lookup"},
			{EventType: "tool_call", ToolName: "lookup"},
		},
		WorldState: WorldState{
			Tools: map[string]ToolState{
				"lookup": {Name: "lookup", State: "timeout"},
			},
		},
	}

	result := a.Evaluate(ctx)
	if result.Passed {
		t.Error("expected fail, got pass")
	}
	if result.DocketHint != "tool.timeout_retrycap" {
		t.Errorf("DocketHint = %q, want %q", result.DocketHint, "tool.timeout_retrycap")
	}
}

func TestRetryCap_Pass_InterleavedTools(t *testing.T) {
	a := &RetryCapAssertion{}
	ctx := Context{
		ToolTrace: []tut.TraceEvent{
			{EventType: "tool_call", ToolName: "lookup"},
			{EventType: "tool_call", ToolName: "send"},
			{EventType: "tool_call", ToolName: "lookup"},
			{EventType: "tool_call", ToolName: "send"},
		},
		WorldState: WorldState{Tools: map[string]ToolState{}},
	}

	result := a.Evaluate(ctx)
	if !result.Passed {
		t.Errorf("expected pass for interleaved tools, got fail: %s", result.Message)
	}
}

func TestRetryCap_SkipsNonToolCallEvents(t *testing.T) {
	a := &RetryCapAssertion{}
	ctx := Context{
		ToolTrace: []tut.TraceEvent{
			{EventType: "tool_call", ToolName: "lookup"},
			{EventType: "model_call"},
			{EventType: "model_call"},
			{EventType: "model_call"},
			{EventType: "model_call"},
			{EventType: "tool_call", ToolName: "lookup"},
		},
		WorldState: WorldState{Tools: map[string]ToolState{}},
	}

	result := a.Evaluate(ctx)
	if !result.Passed {
		t.Errorf("expected pass (model_call events not counted), got fail: %s", result.Message)
	}
}

// ---------------------------------------------------------------------------
// ForbiddenToolAssertion
// ---------------------------------------------------------------------------

func TestForbiddenTool_Pass_NoForbiddenCalled(t *testing.T) {
	a := &ForbiddenToolAssertion{}
	ctx := Context{
		ToolTrace: []tut.TraceEvent{
			{EventType: "tool_call", ToolName: "safe_tool"},
		},
		Baseline: &ContractBaseline{
			ForbiddenContent: []string{"tool:dangerous_tool"},
		},
	}

	result := a.Evaluate(ctx)
	if !result.Passed {
		t.Errorf("expected pass, got fail: %s", result.Message)
	}
}

func TestForbiddenTool_Fail_ForbiddenCalled(t *testing.T) {
	a := &ForbiddenToolAssertion{}
	ctx := Context{
		ToolTrace: []tut.TraceEvent{
			{EventType: "tool_call", ToolName: "dangerous_tool"},
		},
		Baseline: &ContractBaseline{
			ForbiddenContent: []string{"tool:dangerous_tool"},
		},
	}

	result := a.Evaluate(ctx)
	if result.Passed {
		t.Error("expected fail for calling forbidden tool, got pass")
	}
	if result.DocketHint != "tool.forbidden" {
		t.Errorf("DocketHint = %q, want %q", result.DocketHint, "tool.forbidden")
	}
}

func TestForbiddenTool_Pass_NoBaseline(t *testing.T) {
	a := &ForbiddenToolAssertion{}
	ctx := Context{
		ToolTrace: []tut.TraceEvent{
			{EventType: "tool_call", ToolName: "any_tool"},
		},
	}

	result := a.Evaluate(ctx)
	if !result.Passed {
		t.Errorf("expected pass with no baseline, got fail: %s", result.Message)
	}
}

func TestForbiddenTool_Pass_NonToolPrefixNotChecked(t *testing.T) {
	a := &ForbiddenToolAssertion{}
	ctx := Context{
		ToolTrace: []tut.TraceEvent{
			{EventType: "tool_call", ToolName: "some_tool"},
		},
		Baseline: &ContractBaseline{
			ForbiddenContent: []string{"not_a_tool_prefix"},
		},
	}

	result := a.Evaluate(ctx)
	if !result.Passed {
		t.Errorf("expected pass when forbidden content lacks 'tool:' prefix, got fail: %s", result.Message)
	}
}

// ---------------------------------------------------------------------------
// OutputSchemaAssertion
// ---------------------------------------------------------------------------

func TestOutputSchema_Pass_NoBaseline(t *testing.T) {
	a := &OutputSchemaAssertion{}
	ctx := Context{
		Output: tut.AgentOutput{
			Parsed: map[string]interface{}{"key": "value"},
		},
	}

	result := a.Evaluate(ctx)
	if !result.Passed {
		t.Errorf("expected pass with no baseline schema, got fail: %s", result.Message)
	}
}

func TestOutputSchema_Fail_NilParsed(t *testing.T) {
	a := &OutputSchemaAssertion{}
	ctx := Context{
		Output: tut.AgentOutput{
			Parsed: nil,
		},
	}

	result := a.Evaluate(ctx)
	if result.Passed {
		t.Error("expected fail for nil parsed output, got pass")
	}
	if result.DocketHint != "output.invalid_json" {
		t.Errorf("DocketHint = %q, want %q", result.DocketHint, "output.invalid_json")
	}
}

func TestOutputSchema_Pass_ValidSchema(t *testing.T) {
	a := &OutputSchemaAssertion{}
	ctx := Context{
		Output: tut.AgentOutput{
			Parsed: map[string]interface{}{
				"name":  "Alice",
				"email": "alice@example.com",
			},
		},
		Baseline: &ContractBaseline{
			OutputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name":  map[string]interface{}{"type": "string"},
					"email": map[string]interface{}{"type": "string"},
				},
				"required": []interface{}{"name", "email"},
			},
		},
	}

	result := a.Evaluate(ctx)
	if !result.Passed {
		t.Errorf("expected pass for valid schema match, got fail: %s", result.Message)
	}
}

func TestOutputSchema_Fail_SchemaMismatch(t *testing.T) {
	a := &OutputSchemaAssertion{}
	ctx := Context{
		Output: tut.AgentOutput{
			Parsed: map[string]interface{}{
				"name": 12345, // should be string, not number
			},
		},
		Baseline: &ContractBaseline{
			OutputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{"type": "string"},
				},
				"required": []interface{}{"name"},
			},
		},
	}

	result := a.Evaluate(ctx)
	if result.Passed {
		t.Error("expected fail for schema mismatch, got pass")
	}
	if result.DocketHint != "output.schema_mismatch" {
		t.Errorf("DocketHint = %q, want %q", result.DocketHint, "output.schema_mismatch")
	}
}

// ---------------------------------------------------------------------------
// OutputDerivableAssertion
// ---------------------------------------------------------------------------

func TestOutputDerivable_Pass_NilParsedOutput(t *testing.T) {
	a := &OutputDerivableAssertion{}
	ctx := Context{
		Output: tut.AgentOutput{Parsed: nil},
	}

	result := a.Evaluate(ctx)
	if !result.Passed {
		t.Errorf("expected pass for nil parsed output (skip), got fail: %s", result.Message)
	}
	if !result.Soft {
		t.Error("expected Soft=true")
	}
}

func TestOutputDerivable_Pass_GroundedOutput(t *testing.T) {
	a := &OutputDerivableAssertion{}
	ctx := Context{
		Output: tut.AgentOutput{
			Parsed: map[string]interface{}{
				"description": "The quick brown fox jumps over",
			},
		},
		ToolTrace: []tut.TraceEvent{
			{
				Response: json.RawMessage(`{"text": "The quick brown fox jumps over"}`),
			},
		},
		WorldState: WorldState{Databases: map[string]DBState{}},
	}

	result := a.Evaluate(ctx)
	if !result.Passed {
		t.Errorf("expected pass for grounded output, got fail: %s", result.Message)
	}
}

func TestOutputDerivable_Fail_UngroundedOutput(t *testing.T) {
	a := &OutputDerivableAssertion{}
	ctx := Context{
		Output: tut.AgentOutput{
			Parsed: map[string]interface{}{
				"description": "This is a completely fabricated claim with no basis",
			},
		},
		ToolTrace:  []tut.TraceEvent{},
		WorldState: WorldState{Databases: map[string]DBState{}},
	}

	result := a.Evaluate(ctx)
	if result.Passed {
		t.Error("expected fail for ungrounded output, got pass")
	}
	if !result.Soft {
		t.Error("expected Soft=true for output_derivable")
	}
	if result.DocketHint != "output.ungrounded" {
		t.Errorf("DocketHint = %q, want %q", result.DocketHint, "output.ungrounded")
	}
}

// ---------------------------------------------------------------------------
// SensitiveLeakAssertion
// ---------------------------------------------------------------------------

func TestSensitiveLeak_Pass_NoSensitiveData(t *testing.T) {
	a := &SensitiveLeakAssertion{}
	ctx := Context{
		Output: tut.AgentOutput{
			Raw:    []byte("Here is your order status: shipped."),
			Parsed: nil,
		},
	}

	result := a.Evaluate(ctx)
	if !result.Passed {
		t.Errorf("expected pass, got fail: %s", result.Message)
	}
	if !result.Soft {
		t.Error("expected Soft=true for sensitive_leak")
	}
}

func TestSensitiveLeak_Fail_CreditCard(t *testing.T) {
	a := &SensitiveLeakAssertion{}
	ctx := Context{
		Output: tut.AgentOutput{
			Raw:    []byte("Your card: 4111 1111 1111 1111"),
			Parsed: nil,
		},
	}

	result := a.Evaluate(ctx)
	if result.Passed {
		t.Error("expected fail for credit card in output, got pass")
	}
	if !result.Soft {
		t.Error("expected Soft=true for sensitive_leak")
	}
	if result.DocketHint != "output.sensitive_leak" {
		t.Errorf("DocketHint = %q, want %q", result.DocketHint, "output.sensitive_leak")
	}
}

func TestSensitiveLeak_Pass_NonLuhnDigits(t *testing.T) {
	a := &SensitiveLeakAssertion{}
	ctx := Context{
		Output: tut.AgentOutput{
			Raw:    []byte("Order reference 1234 5678 9012 3456"),
			Parsed: nil,
		},
	}

	result := a.Evaluate(ctx)
	if !result.Passed {
		t.Errorf("expected pass for non-Luhn digit sequence, got fail: %s", result.Message)
	}
}

func TestSensitiveLeak_Fail_SSN(t *testing.T) {
	a := &SensitiveLeakAssertion{}
	ctx := Context{
		Output: tut.AgentOutput{
			Raw:    []byte("SSN is 123-45-6789"),
			Parsed: nil,
		},
	}

	result := a.Evaluate(ctx)
	if result.Passed {
		t.Error("expected fail for SSN in output, got pass")
	}
}

func TestSensitiveLeak_Fail_APIKey(t *testing.T) {
	a := &SensitiveLeakAssertion{}
	ctx := Context{
		Output: tut.AgentOutput{
			Raw:    []byte("key: sk-abcdefghijklmnopqrstuvwxyz"),
			Parsed: nil,
		},
	}

	result := a.Evaluate(ctx)
	if result.Passed {
		t.Error("expected fail for API key pattern in output, got pass")
	}
}

func TestSensitiveLeak_Fail_SensitiveField(t *testing.T) {
	a := &SensitiveLeakAssertion{}
	ctx := Context{
		Output: tut.AgentOutput{
			Raw: []byte(`{"password": "secret123"}`),
			Parsed: map[string]interface{}{
				"password": "secret123",
			},
		},
	}

	result := a.Evaluate(ctx)
	if result.Passed {
		t.Error("expected fail for sensitive field 'password' in output, got pass")
	}
}

// ---------------------------------------------------------------------------
// EvaluateAll
// ---------------------------------------------------------------------------

func TestEvaluateAll_RunsMultipleAssertions(t *testing.T) {
	specs := []scenario.AssertionSpec{
		{Type: "tool_sequence"},
		{Type: "retry_cap"},
	}

	ctx := Context{
		ToolTrace: []tut.TraceEvent{
			{EventType: "tool_call", ToolName: "lookup"},
		},
		Baseline: &ContractBaseline{
			ToolSequence: []string{"lookup"},
		},
		WorldState: WorldState{Tools: map[string]ToolState{}},
	}

	results := EvaluateAll(specs, ctx)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	for i, r := range results {
		if !r.Passed {
			t.Errorf("result[%d] (%s): expected pass, got fail: %s", i, r.AssertionType, r.Message)
		}
	}
}

func TestEvaluateAll_UnknownType(t *testing.T) {
	specs := []scenario.AssertionSpec{
		{Type: "completely_fake_type"},
	}

	results := EvaluateAll(specs, Context{})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Passed {
		t.Error("expected fail for unknown assertion type, got pass")
	}
	if results[0].DocketHint != "unknown" {
		t.Errorf("DocketHint = %q, want %q", results[0].DocketHint, "unknown")
	}
}

func TestEvaluateAll_EmptySpecs(t *testing.T) {
	results := EvaluateAll(nil, Context{})
	if len(results) != 0 {
		t.Errorf("expected 0 results for nil specs, got %d", len(results))
	}
}

func TestEvaluateAll_MixedPassFail(t *testing.T) {
	specs := []scenario.AssertionSpec{
		{Type: "tool_sequence"},
		{Type: "forbidden_tool"},
	}

	ctx := Context{
		ToolTrace: []tut.TraceEvent{
			{EventType: "tool_call", ToolName: "dangerous"},
		},
		Baseline: &ContractBaseline{
			ToolSequence:     []string{"dangerous"},
			ForbiddenContent: []string{"tool:dangerous"},
		},
	}

	results := EvaluateAll(specs, ctx)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	// tool_sequence should pass (tool "dangerous" matches required sequence)
	if !results[0].Passed {
		t.Errorf("tool_sequence: expected pass, got fail: %s", results[0].Message)
	}
	// forbidden_tool should fail (tool "dangerous" is forbidden)
	if results[1].Passed {
		t.Error("forbidden_tool: expected fail, got pass")
	}
}

func TestToolSequence_Fail_ForbiddenToolFromSpec(t *testing.T) {
	a := &ToolSequenceAssertion{}
	ctx := Context{
		ToolTrace: []tut.TraceEvent{
			{EventType: "tool_call", ToolName: "order_lookup"},
			{EventType: "tool_call", ToolName: "cancel_order"},
		},
		Spec: map[string]interface{}{
			"required":  []interface{}{"order_lookup"},
			"forbidden": []interface{}{"cancel_order"},
		},
	}

	result := a.Evaluate(ctx)
	if result.Passed {
		t.Fatal("expected fail when forbidden tool is called")
	}
	if result.DocketHint != "tool.forbidden" {
		t.Errorf("DocketHint = %q, want %q", result.DocketHint, "tool.forbidden")
	}
}

func TestToolArgs_InvariantFromSpec_Pass(t *testing.T) {
	a := &ToolArgsAssertion{}
	ctx := Context{
		Input: scenario.Input{
			Payload: map[string]interface{}{"order_id": "ord-001"},
		},
		ToolTrace: []tut.TraceEvent{
			{
				EventType: "tool_call",
				ToolName:  "order_lookup",
				Args:      json.RawMessage(`{"order_id":"ord-001"}`),
			},
		},
		Spec: map[string]interface{}{
			"tool":      "order_lookup",
			"invariant": "args.order_id == input.order_id",
		},
	}

	result := a.Evaluate(ctx)
	if !result.Passed {
		t.Fatalf("expected pass, got fail: %s", result.Message)
	}
}

func TestToolArgs_InvariantFromSpec_Fail(t *testing.T) {
	a := &ToolArgsAssertion{}
	ctx := Context{
		Input: scenario.Input{
			Payload: map[string]interface{}{"order_id": "ord-001"},
		},
		ToolTrace: []tut.TraceEvent{
			{
				EventType: "tool_call",
				ToolName:  "order_lookup",
				Args:      json.RawMessage(`{"order_id":"ord-999"}`),
			},
		},
		Spec: map[string]interface{}{
			"tool":      "order_lookup",
			"invariant": "args.order_id == input.order_id",
		},
	}

	result := a.Evaluate(ctx)
	if result.Passed {
		t.Fatal("expected fail for invariant mismatch")
	}
	if result.DocketHint != "tool.args_invalid" {
		t.Errorf("DocketHint = %q, want %q", result.DocketHint, "tool.args_invalid")
	}
}

func TestRetryCap_UsesSpecMaxRetries(t *testing.T) {
	a := &RetryCapAssertion{}
	ctx := Context{
		ToolTrace: []tut.TraceEvent{
			{EventType: "tool_call", ToolName: "order_lookup"},
			{EventType: "tool_call", ToolName: "order_lookup"},
		},
		Spec: map[string]interface{}{
			"tool":        "order_lookup",
			"max_retries": 1,
		},
	}

	result := a.Evaluate(ctx)
	if result.Passed {
		t.Fatal("expected fail when max_retries from spec is exceeded")
	}
}

func TestForbiddenTool_Fail_FromSpec(t *testing.T) {
	a := &ForbiddenToolAssertion{}
	ctx := Context{
		ToolTrace: []tut.TraceEvent{
			{EventType: "tool_call", ToolName: "send_email"},
		},
		Spec: map[string]interface{}{
			"forbidden": []interface{}{"send_email"},
		},
	}

	result := a.Evaluate(ctx)
	if result.Passed {
		t.Fatal("expected fail for forbidden tool from scenario spec")
	}
}

func TestOutputSchema_UsesScenarioSpecSchema(t *testing.T) {
	a := &OutputSchemaAssertion{}
	ctx := Context{
		Output: tut.AgentOutput{
			Parsed: map[string]interface{}{"status": "ok"},
		},
		Spec: map[string]interface{}{
			"schema": map[string]interface{}{
				"type":     "object",
				"required": []interface{}{"status"},
			},
		},
	}

	result := a.Evaluate(ctx)
	if !result.Passed {
		t.Fatalf("expected pass with inline scenario schema, got: %s", result.Message)
	}
}

// ---------------------------------------------------------------------------
// maskMatch (utility)
// ---------------------------------------------------------------------------

func TestMaskMatch_Short(t *testing.T) {
	result := maskMatch("short")
	if result != "****" {
		t.Errorf("maskMatch(%q) = %q, want %q", "short", result, "****")
	}
}

func TestMaskMatch_Long(t *testing.T) {
	result := maskMatch("1234567890abcdef")
	// First 4 chars + masked middle + last 4 chars
	if len(result) != len("1234567890abcdef") {
		t.Errorf("maskMatch length = %d, want %d", len(result), len("1234567890abcdef"))
	}
	if result[:4] != "1234" {
		t.Errorf("maskMatch prefix = %q, want %q", result[:4], "1234")
	}
	if result[len(result)-4:] != "cdef" {
		t.Errorf("maskMatch suffix = %q, want %q", result[len(result)-4:], "cdef")
	}
}
