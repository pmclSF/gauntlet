package assertions

import (
	"testing"

	"github.com/pmclSF/gauntlet/internal/tut"
)

func TestModelCallCount_PassWithinRange(t *testing.T) {
	a := &ModelCallCountAssertion{}
	ctx := Context{
		Spec: map[string]interface{}{
			"min": 1,
			"max": 3,
		},
		ToolTrace: []tut.TraceEvent{
			{EventType: "model_call"},
			{EventType: "model_call"},
		},
	}
	result := a.Evaluate(ctx)
	if !result.Passed {
		t.Fatalf("expected pass, got fail: %s", result.Message)
	}
}

func TestModelCallCount_FailAboveMax(t *testing.T) {
	a := &ModelCallCountAssertion{}
	ctx := Context{
		Spec: map[string]interface{}{
			"max": 1,
		},
		ToolTrace: []tut.TraceEvent{
			{EventType: "model_call"},
			{EventType: "model_call"},
		},
	}
	result := a.Evaluate(ctx)
	if result.Passed {
		t.Fatal("expected failure for too many model calls")
	}
}

func TestModelCallCount_FailInvalidSpec(t *testing.T) {
	a := &ModelCallCountAssertion{}
	result := a.Evaluate(Context{Spec: map[string]interface{}{}})
	if result.Passed {
		t.Fatal("expected spec validation failure")
	}
}
