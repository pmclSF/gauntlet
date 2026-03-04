package assertions

import (
	"fmt"
	"strings"
)

// ToolSequenceAssertion validates that tools were called in the required order
// and that no forbidden tools were called. Hard gate.
type ToolSequenceAssertion struct{}

func (a *ToolSequenceAssertion) Type() string { return "tool_sequence" }
func (a *ToolSequenceAssertion) IsSoft() bool { return false }

func (a *ToolSequenceAssertion) Evaluate(ctx Context) Result {
	// Extract actual tool call sequence from traces
	var actual []string
	for _, t := range ctx.ToolTrace {
		if t.EventType == "tool_call" && t.ToolName != "" {
			actual = append(actual, t.ToolName)
		}
	}

	required := specStringSlice(ctx.Spec, "required")
	if len(required) == 0 && ctx.Baseline != nil && len(ctx.Baseline.ToolSequence) > 0 {
		required = ctx.Baseline.ToolSequence
	}

	// Check that required tools appear in order (not necessarily consecutive)
	reqIdx := 0
	for _, tool := range actual {
		if reqIdx < len(required) && tool == required[reqIdx] {
			reqIdx++
		}
	}
	if reqIdx < len(required) {
		missing := required[reqIdx:]
		return Result{
			AssertionType: a.Type(),
			Passed:        false,
			Expected:      fmt.Sprintf("tool sequence %v", required),
			Actual:        fmt.Sprintf("tool sequence %v (missing: %v)", actual, missing),
			Message: fmt.Sprintf("tool sequence incomplete: expected %s but %s never called",
				strings.Join(required, " -> "),
				strings.Join(missing, ", ")),
			DocketHint: "planner.premature_finalize",
		}
	}

	forbidden := specStringSlice(ctx.Spec, "forbidden")
	forbiddenSet := make(map[string]bool, len(forbidden))
	for _, f := range forbidden {
		forbiddenSet[f] = true
	}
	for _, tool := range actual {
		if forbiddenSet[tool] {
			return Result{
				AssertionType: a.Type(),
				Passed:        false,
				Expected:      fmt.Sprintf("forbidden tools %v should not be called", forbidden),
				Actual:        fmt.Sprintf("tool %s was called", tool),
				Message:       fmt.Sprintf("forbidden tool called: %s", tool),
				DocketHint:    "tool.forbidden",
			}
		}
	}

	return Result{
		AssertionType: a.Type(),
		Passed:        true,
		Message:       fmt.Sprintf("tool sequence valid: %v", actual),
	}
}
