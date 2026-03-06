package assertions

import (
	"testing"

	"github.com/pmclSF/gauntlet/internal/tut"
)

func TestTokenBudget_ScopeInputOnly(t *testing.T) {
	a := &TokenBudgetAssertion{}
	ctx := Context{
		Spec: map[string]interface{}{
			"scope":             "input_only",
			"max_prompt_tokens": 120,
		},
		ToolTrace: []tut.TraceEvent{
			{EventType: "model_call", ModelCall: &tut.ModelCallEvent{Model: "gpt-4o-mini", PromptTokens: 100, CompletionTokens: 90}},
		},
	}
	result := a.Evaluate(ctx)
	if !result.Passed {
		t.Fatalf("expected pass, got fail: %s", result.Message)
	}
}

func TestTokenBudget_ScopeOutputOnlyFail(t *testing.T) {
	a := &TokenBudgetAssertion{}
	ctx := Context{
		Spec: map[string]interface{}{
			"scope":                 "output_only",
			"max_completion_tokens": 10,
		},
		ToolTrace: []tut.TraceEvent{
			{EventType: "model_call", ModelCall: &tut.ModelCallEvent{Model: "gpt-4o-mini", PromptTokens: 5, CompletionTokens: 20}},
		},
	}
	result := a.Evaluate(ctx)
	if result.Passed {
		t.Fatal("expected output_only budget failure")
	}
}

func TestTokenBudget_SkipsWhenCountsMissing(t *testing.T) {
	a := &TokenBudgetAssertion{}
	ctx := Context{
		Spec: map[string]interface{}{
			"max_total_tokens": 100,
		},
		ToolTrace: []tut.TraceEvent{
			{EventType: "model_call", ModelCall: &tut.ModelCallEvent{Model: "gpt-4o-mini"}},
		},
	}
	result := a.Evaluate(ctx)
	if !result.Passed {
		t.Fatalf("expected pass/skip when counts missing, got fail: %s", result.Message)
	}
}
