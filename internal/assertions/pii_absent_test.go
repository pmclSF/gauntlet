package assertions

import (
	"testing"

	"github.com/pmclSF/gauntlet/internal/tut"
)

func TestPIIAbsent_Pass(t *testing.T) {
	a := &PIIAbsentAssertion{}
	ctx := Context{
		Spec: map[string]interface{}{
			"detectors": []interface{}{"email"},
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

func TestPIIAbsent_FailEmail(t *testing.T) {
	a := &PIIAbsentAssertion{}
	ctx := Context{
		Spec: map[string]interface{}{
			"detectors": []interface{}{"email"},
		},
		Output: tut.AgentOutput{
			Raw: []byte(`{"response":"contact me at user@example.com"}`),
		},
	}
	result := a.Evaluate(ctx)
	if result.Passed {
		t.Fatal("expected fail for email PII")
	}
}

func TestPIIAbsent_InvalidDetector(t *testing.T) {
	a := &PIIAbsentAssertion{}
	ctx := Context{
		Spec: map[string]interface{}{
			"detectors": []interface{}{"unknown_detector"},
		},
		Output: tut.AgentOutput{
			Raw: []byte(`{"response":"ok"}`),
		},
	}
	result := a.Evaluate(ctx)
	if result.Passed {
		t.Fatal("expected invalid detector failure")
	}
}
