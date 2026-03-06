package assertions

import (
	"encoding/json"
	"testing"

	"github.com/pmclSF/gauntlet/internal/tut"
)

func TestForbiddenContent_PassWhenPatternAbsent(t *testing.T) {
	a := &ForbiddenContentAssertion{}
	ctx := Context{
		Spec: map[string]interface{}{
			"type":    "forbidden_content",
			"pattern": "(?i)internal policy",
		},
		Output: tut.AgentOutput{
			Raw: []byte(`{"response":"Order confirmed"}`),
		},
	}

	result := a.Evaluate(ctx)
	if !result.Passed {
		t.Fatalf("expected pass, got fail: %s", result.Message)
	}
}

func TestForbiddenContent_FailOnOutputMatch(t *testing.T) {
	a := &ForbiddenContentAssertion{}
	ctx := Context{
		Spec: map[string]interface{}{
			"type":    "forbidden_content",
			"pattern": "(?i)internal policy",
			"fields":  []interface{}{"output"},
		},
		Output: tut.AgentOutput{
			Raw: []byte(`{"response":"This violates internal policy guidelines"}`),
		},
	}

	result := a.Evaluate(ctx)
	if result.Passed {
		t.Fatal("expected fail, got pass")
	}
	if result.DocketHint != "output.forbidden_content" {
		t.Fatalf("DocketHint = %q, want %q", result.DocketHint, "output.forbidden_content")
	}
}

func TestForbiddenContent_FailOnToolArgsMatch(t *testing.T) {
	a := &ForbiddenContentAssertion{}
	args, err := json.Marshal(map[string]string{"note": "contains secret-plan"})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	ctx := Context{
		Spec: map[string]interface{}{
			"type":    "forbidden_content",
			"pattern": "secret-plan",
			"fields":  []interface{}{"tool_args"},
		},
		ToolTrace: []tut.TraceEvent{
			{
				EventType: "tool_call",
				ToolName:  "planner",
				Args:      args,
			},
		},
	}

	result := a.Evaluate(ctx)
	if result.Passed {
		t.Fatal("expected fail, got pass")
	}
	if result.DocketHint != "tool.args_forbidden_content" {
		t.Fatalf("DocketHint = %q, want %q", result.DocketHint, "tool.args_forbidden_content")
	}
}

func TestForbiddenContent_FailOnInvalidRegex(t *testing.T) {
	a := &ForbiddenContentAssertion{}
	ctx := Context{
		Spec: map[string]interface{}{
			"type":    "forbidden_content",
			"pattern": "(",
		},
	}

	result := a.Evaluate(ctx)
	if result.Passed {
		t.Fatal("expected fail for invalid regex")
	}
	if result.DocketHint != "assertion.spec_invalid" {
		t.Fatalf("DocketHint = %q, want %q", result.DocketHint, "assertion.spec_invalid")
	}
}
