package docket

import (
	"testing"

	"github.com/pmclSF/gauntlet/internal/assertions"
)

func TestClassifyNoFailures(t *testing.T) {
	results := []assertions.Result{
		{AssertionType: "output_schema", Passed: true},
	}
	tags, primary := Classify(results)
	if len(tags) != 0 {
		t.Errorf("expected no tags for all-passing results, got %v", tags)
	}
	if primary != "" {
		t.Errorf("expected empty primary, got %s", primary)
	}
}

func TestClassifyNilResults(t *testing.T) {
	tags, primary := Classify(nil)
	if len(tags) != 0 {
		t.Errorf("expected no tags for nil results, got %v", tags)
	}
	if primary != "" {
		t.Errorf("expected empty primary, got %s", primary)
	}
}

func TestClassifySingleFailure(t *testing.T) {
	results := []assertions.Result{
		{AssertionType: "tool_sequence", Passed: false, DocketHint: TagPlannerPrematureEnd},
	}
	tags, primary := Classify(results)
	if len(tags) != 1 || tags[0] != TagPlannerPrematureEnd {
		t.Errorf("expected [%s], got %v", TagPlannerPrematureEnd, tags)
	}
	if primary != TagPlannerPrematureEnd {
		t.Errorf("expected primary=%s, got %s", TagPlannerPrematureEnd, primary)
	}
}

func TestClassifyMultipleFailures(t *testing.T) {
	results := []assertions.Result{
		{AssertionType: "tool_sequence", Passed: false, DocketHint: TagPlannerPrematureEnd},
		{AssertionType: "output_schema", Passed: false, DocketHint: TagInputMalformed},
	}
	tags, primary := Classify(results)

	if len(tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(tags))
	}

	// input.malformed has higher precedence (lower index)
	if primary != TagInputMalformed {
		t.Errorf("expected primary=%s (highest precedence), got %s", TagInputMalformed, primary)
	}

	// Tags should be sorted by precedence
	if tags[0] != TagInputMalformed {
		t.Errorf("expected first tag=%s, got %s", TagInputMalformed, tags[0])
	}
}

func TestClassifyFailureWithoutHint(t *testing.T) {
	results := []assertions.Result{
		{AssertionType: "output_schema", Passed: false, DocketHint: ""},
	}
	tags, primary := Classify(results)
	if len(tags) != 1 || tags[0] != TagUnknown {
		t.Errorf("expected [unknown], got %v", tags)
	}
	if primary != TagUnknown {
		t.Errorf("expected primary=unknown, got %s", primary)
	}
}

func TestPrecedenceOrder(t *testing.T) {
	// input.malformed should have higher precedence than planner.*
	if Precedence(TagInputMalformed) >= Precedence(TagPlannerPrematureEnd) {
		t.Error("input.malformed should have higher precedence (lower value) than planner.premature_finalize")
	}
}
