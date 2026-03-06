package assertions

import (
	"testing"
	"time"

	"github.com/pmclSF/gauntlet/internal/tut"
)

func TestLatencyP99_PassToolScope(t *testing.T) {
	a := &LatencyP99Assertion{}
	ctx := Context{
		Spec: map[string]interface{}{
			"max_ms": 120,
			"scope":  "tool_calls",
		},
		ToolTrace: []tut.TraceEvent{
			{EventType: "tool_call", DurationMs: 20},
			{EventType: "tool_call", DurationMs: 50},
			{EventType: "tool_call", DurationMs: 80},
		},
	}
	result := a.Evaluate(ctx)
	if !result.Passed {
		t.Fatalf("expected pass, got fail: %s", result.Message)
	}
}

func TestLatencyP99_FailModelScope(t *testing.T) {
	a := &LatencyP99Assertion{}
	ctx := Context{
		Spec: map[string]interface{}{
			"max_ms": 60,
			"scope":  "model_calls",
		},
		ToolTrace: []tut.TraceEvent{
			{EventType: "model_call", DurationMs: 10},
			{EventType: "model_call", DurationMs: 90},
		},
	}
	result := a.Evaluate(ctx)
	if result.Passed {
		t.Fatal("expected p99 failure for model scope")
	}
}

func TestLatencyP99_TotalScopeUsesOutputDuration(t *testing.T) {
	a := &LatencyP99Assertion{}
	ctx := Context{
		Spec: map[string]interface{}{
			"max_ms": 100,
			"scope":  "total",
		},
		Output: tut.AgentOutput{
			Duration: 120 * time.Millisecond,
		},
	}
	result := a.Evaluate(ctx)
	if result.Passed {
		t.Fatal("expected total-scope failure")
	}
}
