package assertions

import (
	"os"
	"testing"

	"github.com/pmclSF/gauntlet/internal/tut"
)

func TestSemanticMatch_SkipsOutsideNightly(t *testing.T) {
	a := &SemanticMatchAssertion{}
	ctx := Context{
		RunnerMode: "pr_ci",
		Spec: map[string]interface{}{
			"prompt":    "Response confirms order is confirmed",
			"threshold": 0.8,
		},
		Output: tut.AgentOutput{
			Raw: []byte(`{"response":"Order confirmed"}`),
		},
	}
	result := a.Evaluate(ctx)
	if !result.Passed {
		t.Fatalf("expected skip/pass in non-nightly mode, got fail: %s", result.Message)
	}
}

func TestSemanticMatch_FailNightlyBelowThreshold(t *testing.T) {
	a := &SemanticMatchAssertion{}
	t.Setenv("GAUNTLET_SEMANTIC_MATCH_SCORE", "0.2")
	ctx := Context{
		RunnerMode: "nightly",
		Spec: map[string]interface{}{
			"prompt":    "Response confirms order is confirmed",
			"threshold": 0.8,
		},
		Output: tut.AgentOutput{
			Raw: []byte(`{"response":"Unable to find order"}`),
		},
	}
	result := a.Evaluate(ctx)
	if result.Passed {
		t.Fatal("expected nightly semantic mismatch failure")
	}
}

func TestSemanticMatch_PassNightly(t *testing.T) {
	a := &SemanticMatchAssertion{}
	t.Setenv("GAUNTLET_SEMANTIC_MATCH_SCORE", "0.95")
	ctx := Context{
		RunnerMode: "nightly",
		Spec: map[string]interface{}{
			"prompt":    "Response confirms order is confirmed",
			"threshold": 0.8,
		},
		Output: tut.AgentOutput{
			Raw: []byte(`{"response":"Order is confirmed"}`),
		},
	}
	result := a.Evaluate(ctx)
	if !result.Passed {
		t.Fatalf("expected pass, got fail: %s", result.Message)
	}
}

func TestSemanticMatch_InvalidThreshold(t *testing.T) {
	a := &SemanticMatchAssertion{}
	os.Unsetenv("GAUNTLET_SEMANTIC_MATCH_SCORE")
	ctx := Context{
		RunnerMode: "nightly",
		Spec: map[string]interface{}{
			"prompt":    "confirm order",
			"threshold": 1.5,
		},
	}
	result := a.Evaluate(ctx)
	if result.Passed {
		t.Fatal("expected invalid threshold failure")
	}
}
