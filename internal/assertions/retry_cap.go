package assertions

import "fmt"

// RetryCapAssertion validates that tool retry count stays within bounds.
// Counts consecutive calls to the same tool. Hard gate.
type RetryCapAssertion struct{}

func (a *RetryCapAssertion) Type() string { return "retry_cap" }
func (a *RetryCapAssertion) IsSoft() bool { return false }

func (a *RetryCapAssertion) Evaluate(ctx Context) Result {
	// Count consecutive calls to each tool
	maxConsecutive := make(map[string]int)
	var lastTool string
	consecutive := 0

	for _, t := range ctx.ToolTrace {
		if t.EventType != "tool_call" {
			continue
		}
		if t.ToolName == lastTool {
			consecutive++
		} else {
			consecutive = 1
			lastTool = t.ToolName
		}
		if consecutive > maxConsecutive[t.ToolName] {
			maxConsecutive[t.ToolName] = consecutive
		}
	}

	// Default retry cap is 3.
	maxRetries := 3
	if configured, ok := specInt(ctx.Spec, "max_retries"); ok {
		maxRetries = configured
	}
	targetTool, hasTargetTool := specString(ctx.Spec, "tool")

	for tool, count := range maxConsecutive {
		if hasTargetTool && targetTool != "" && tool != targetTool {
			continue
		}
		if count > maxRetries {
			docketTag := "planner.retry_storm"
			// If the tool had a timeout/error state, use more specific tag
			if ws, ok := ctx.WorldState.Tools[tool]; ok {
				if ws.State == "timeout" {
					docketTag = "tool.timeout_retrycap"
				}
			}
				return Result{
					AssertionType: a.Type(),
					Passed:        false,
					Expected:      fmt.Sprintf("max %d consecutive calls to %s", maxRetries, tool),
					Actual:        fmt.Sprintf("%d consecutive calls to %s", count, tool),
					Message:       fmt.Sprintf("retry cap exceeded: %s called %d times consecutively (max %d)", tool, count, maxRetries),
					DocketHint:    docketTag,
				}
			}
	}

	return Result{
		AssertionType: a.Type(),
		Passed:        true,
		Message:       "all tool retry counts within bounds",
	}
}
