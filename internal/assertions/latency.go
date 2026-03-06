package assertions

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// LatencyP99Assertion enforces p99 latency ceiling for selected scope.
type LatencyP99Assertion struct{}

func (a *LatencyP99Assertion) Type() string { return "latency_p99" }
func (a *LatencyP99Assertion) IsSoft() bool { return false }

func (a *LatencyP99Assertion) Evaluate(ctx Context) Result {
	maxMs, hasMax := specInt(ctx.Spec, "max_ms")
	if !hasMax || maxMs < 0 {
		return Result{
			AssertionType: a.Type(),
			Passed:        false,
			Message:       "latency_p99: missing required field 'max_ms'",
			DocketHint:    "assertion.spec_invalid",
		}
	}

	scope, _ := specString(ctx.Spec, "scope")
	scope = strings.ToLower(strings.TrimSpace(scope))
	if scope == "" {
		scope = "tool_calls"
	}

	samples, err := latencySamplesForScope(scope, ctx)
	if err != nil {
		return Result{
			AssertionType: a.Type(),
			Passed:        false,
			Message:       fmt.Sprintf("latency_p99: %v", err),
			DocketHint:    "assertion.spec_invalid",
		}
	}
	if len(samples) == 0 {
		return Result{
			AssertionType: a.Type(),
			Passed:        true,
			Message:       fmt.Sprintf("latency_p99: skipped because no %s latency samples were available", scope),
		}
	}

	p99 := percentile99(samples)
	if p99 > maxMs {
		return Result{
			AssertionType: a.Type(),
			Passed:        false,
			Expected:      fmt.Sprintf("p99 %s latency <= %dms", scope, maxMs),
			Actual:        fmt.Sprintf("p99 %s latency = %dms", scope, p99),
			Message:       fmt.Sprintf("latency_p99: p99 %s latency %dms exceeds max %dms", scope, p99, maxMs),
			DocketHint:    "latency.p99_exceeded",
		}
	}

	return Result{
		AssertionType: a.Type(),
		Passed:        true,
		Message:       fmt.Sprintf("latency_p99: p99 %s latency %dms within max %dms", scope, p99, maxMs),
	}
}

func latencySamplesForScope(scope string, ctx Context) ([]int, error) {
	samples := []int{}
	switch scope {
	case "tool_calls":
		for _, event := range ctx.ToolTrace {
			if event.EventType == "tool_call" && event.DurationMs > 0 {
				samples = append(samples, event.DurationMs)
			}
		}
	case "model_calls":
		for _, event := range ctx.ToolTrace {
			if event.EventType == "model_call" && event.DurationMs > 0 {
				samples = append(samples, event.DurationMs)
			}
		}
	case "total":
		totalMs := int(ctx.Output.Duration.Milliseconds())
		if totalMs > 0 {
			samples = append(samples, totalMs)
		}
	default:
		return nil, fmt.Errorf("unsupported scope %q (supported: tool_calls, model_calls, total)", scope)
	}
	return samples, nil
}

func percentile99(samples []int) int {
	if len(samples) == 0 {
		return 0
	}
	ordered := append([]int{}, samples...)
	sort.Ints(ordered)
	rank := int(math.Ceil(0.99*float64(len(ordered)))) - 1
	if rank < 0 {
		rank = 0
	}
	if rank >= len(ordered) {
		rank = len(ordered) - 1
	}
	return ordered[rank]
}
