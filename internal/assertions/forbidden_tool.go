package assertions

import (
	"fmt"
	"strings"
)

// ForbiddenToolAssertion checks that specific tools were never called.
// Hard gate.
type ForbiddenToolAssertion struct{}

func (a *ForbiddenToolAssertion) Type() string { return "forbidden_tool" }
func (a *ForbiddenToolAssertion) IsSoft() bool { return false }

func (a *ForbiddenToolAssertion) Evaluate(ctx Context) Result {
	// Collect all tools called
	calledTools := make(map[string]bool)
	for _, t := range ctx.ToolTrace {
		if t.EventType == "tool_call" && t.ToolName != "" {
			calledTools[t.ToolName] = true
		}
	}

	forbidden := specStringSlice(ctx.Spec, "forbidden")
	if len(forbidden) == 0 && ctx.Baseline != nil {
		for _, candidate := range ctx.Baseline.ForbiddenContent {
			if strings.HasPrefix(candidate, "tool:") {
				forbidden = append(forbidden, strings.TrimPrefix(candidate, "tool:"))
			}
		}
	}

	// Check against forbidden list
	for _, toolName := range forbidden {
		if calledTools[toolName] {
			return Result{
				AssertionType: a.Type(),
				Passed:        false,
				Expected:      fmt.Sprintf("tool %s should never be called", toolName),
				Actual:        fmt.Sprintf("tool %s was called", toolName),
				Message:       fmt.Sprintf("forbidden tool called: %s", toolName),
				DocketHint:    "tool.forbidden",
			}
		}
	}

	return Result{
		AssertionType: a.Type(),
		Passed:        true,
		Message:       "no forbidden tools called",
	}
}
