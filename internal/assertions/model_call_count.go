package assertions

import "fmt"

// ModelCallCountAssertion enforces bounds on number of model calls.
type ModelCallCountAssertion struct{}

func (a *ModelCallCountAssertion) Type() string { return "model_call_count" }
func (a *ModelCallCountAssertion) IsSoft() bool { return false }

func (a *ModelCallCountAssertion) Evaluate(ctx Context) Result {
	maxCalls, hasMax := specInt(ctx.Spec, "max")
	minCalls, hasMin := specInt(ctx.Spec, "min")
	if !hasMax && !hasMin {
		return Result{
			AssertionType: a.Type(),
			Passed:        false,
			Message:       "model_call_count: missing required field 'max' or 'min'",
			DocketHint:    "assertion.spec_invalid",
		}
	}

	count := 0
	for _, event := range ctx.ToolTrace {
		if event.EventType == "model_call" {
			count++
		}
	}

	lower := 0
	if hasMin {
		lower = minCalls
	}
	upper := count
	if hasMax {
		upper = maxCalls
	}

	if hasMin && count < minCalls {
		return Result{
			AssertionType: a.Type(),
			Passed:        false,
			Expected:      fmt.Sprintf("%d-%d model calls", lower, upper),
			Actual:        fmt.Sprintf("%d model calls", count),
			Message:       fmt.Sprintf("model_call_count: expected %d-%d model calls, got %d", lower, upper, count),
			DocketHint:    "planner.model_call_count_exceeded",
		}
	}
	if hasMax && count > maxCalls {
		return Result{
			AssertionType: a.Type(),
			Passed:        false,
			Expected:      fmt.Sprintf("%d-%d model calls", lower, upper),
			Actual:        fmt.Sprintf("%d model calls", count),
			Message:       fmt.Sprintf("model_call_count: expected %d-%d model calls, got %d", lower, upper, count),
			DocketHint:    "planner.model_call_count_exceeded",
		}
	}

	return Result{
		AssertionType: a.Type(),
		Passed:        true,
		Message:       fmt.Sprintf("model_call_count: %d model calls within expected range %d-%d", count, lower, upper),
	}
}
