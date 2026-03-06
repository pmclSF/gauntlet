package assertions

import (
	"testing"

	"github.com/pmclSF/gauntlet/internal/tut"
)

func TestCostBudget_Pass(t *testing.T) {
	a := &CostBudgetAssertion{}
	ctx := Context{
		Spec: map[string]interface{}{
			"usd_max": 0.05,
		},
		ToolTrace: []tut.TraceEvent{
			{
				EventType: "model_call",
				ModelCall: &tut.ModelCallEvent{
					Model:            "gpt-4o-mini",
					PromptTokens:     200,
					CompletionTokens: 80,
				},
			},
		},
	}
	result := a.Evaluate(ctx)
	if !result.Passed {
		t.Fatalf("expected pass, got fail: %s", result.Message)
	}
}

func TestCostBudget_Fail(t *testing.T) {
	a := &CostBudgetAssertion{}
	ctx := Context{
		Spec: map[string]interface{}{
			"usd_max": 0.001,
		},
		ToolTrace: []tut.TraceEvent{
			{
				EventType: "model_call",
				ModelCall: &tut.ModelCallEvent{
					Model:            "gpt-4o",
					PromptTokens:     2000,
					CompletionTokens: 1000,
				},
			},
		},
	}
	result := a.Evaluate(ctx)
	if result.Passed {
		t.Fatal("expected cost budget failure")
	}
}

func TestCostBudget_SkipWhenTokenCountsMissing(t *testing.T) {
	a := &CostBudgetAssertion{}
	ctx := Context{
		Spec: map[string]interface{}{
			"usd_max": 0.01,
		},
		ToolTrace: []tut.TraceEvent{
			{
				EventType: "model_call",
				ModelCall: &tut.ModelCallEvent{
					Model: "gpt-4o-mini",
				},
			},
		},
	}
	result := a.Evaluate(ctx)
	if !result.Passed {
		t.Fatalf("expected pass/skip when token counts missing, got fail: %s", result.Message)
	}
}
