package behavioral

import (
	"math"
	"testing"
)

func TestCheckToolSequence(t *testing.T) {
	trace := &ToolTrace{
		Calls: []ToolCall{
			{Name: "order_lookup"},
			{Name: "payment_lookup"},
			{Name: "send_email"},
		},
	}

	// Required subsequence present
	result := CheckToolSequence(trace, []string{"order_lookup", "payment_lookup"})
	if !result.Passed {
		t.Errorf("expected pass for present subsequence: %s", result.Message)
	}

	// Required subsequence missing
	result = CheckToolSequence(trace, []string{"web_search", "payment_lookup"})
	if result.Passed {
		t.Error("expected fail for missing subsequence")
	}
}

func TestCheckForbiddenTool(t *testing.T) {
	trace := &ToolTrace{
		Calls: []ToolCall{
			{Name: "order_lookup"},
			{Name: "send_email"},
		},
	}

	// Forbidden tool called
	result := CheckForbiddenTool(trace, []string{"send_email"})
	if result.Passed {
		t.Error("expected fail when forbidden tool is called")
	}

	// No forbidden tools called
	result = CheckForbiddenTool(trace, []string{"web_search"})
	if !result.Passed {
		t.Error("expected pass when no forbidden tools called")
	}
}

func TestCheckRetryCap(t *testing.T) {
	trace := &ToolTrace{
		Calls: []ToolCall{
			{Name: "order_lookup"},
			{Name: "order_lookup"},
			{Name: "order_lookup"},
			{Name: "order_lookup"},
		},
	}

	// 4 consecutive calls exceeds cap of 3
	result := CheckRetryCap(trace, 3)
	if result.Passed {
		t.Error("expected fail when retry cap exceeded")
	}

	// Same trace passes with cap of 4
	result = CheckRetryCap(trace, 4)
	if !result.Passed {
		t.Errorf("expected pass with higher cap: %s", result.Message)
	}
}

func TestCosineSimilarity(t *testing.T) {
	// Identical vectors
	a := []float64{1, 0, 0}
	sim := CosineSimilarity(a, a)
	if math.Abs(sim-1.0) > 0.001 {
		t.Errorf("expected similarity ~1.0 for identical vectors, got %f", sim)
	}

	// Orthogonal vectors
	b := []float64{0, 1, 0}
	sim = CosineSimilarity(a, b)
	if math.Abs(sim) > 0.001 {
		t.Errorf("expected similarity ~0.0 for orthogonal vectors, got %f", sim)
	}

	// Opposite vectors
	c := []float64{-1, 0, 0}
	sim = CosineSimilarity(a, c)
	if math.Abs(sim+1.0) > 0.001 {
		t.Errorf("expected similarity ~-1.0 for opposite vectors, got %f", sim)
	}
}

func TestTokenOverlap(t *testing.T) {
	overlap := TokenOverlap("the quick brown fox", "the quick brown dog")
	if overlap < 0.5 {
		t.Errorf("expected moderate overlap, got %f", overlap)
	}

	overlap = TokenOverlap("hello world", "goodbye universe")
	if overlap > 0.1 {
		t.Errorf("expected low overlap, got %f", overlap)
	}
}
