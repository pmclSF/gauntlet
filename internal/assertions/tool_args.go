package assertions

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ToolArgsAssertion validates tool call arguments against invariants.
// Supports simple field comparisons like "args.order_id is not null".
// Hard gate.
type ToolArgsAssertion struct{}

func (a *ToolArgsAssertion) Type() string { return "tool_args_invariant" }
func (a *ToolArgsAssertion) IsSoft() bool { return false }

func (a *ToolArgsAssertion) Evaluate(ctx Context) Result {
	// This assertion needs the specific tool name and invariant from spec properties.
	// In the full implementation, these would come from the AssertionSpec.Raw map.
	// For now, validate that all tool calls have non-empty args.
	for _, t := range ctx.ToolTrace {
		if t.EventType != "tool_call" || t.ToolName == "" {
			continue
		}
		if len(t.Args) == 0 || string(t.Args) == "null" {
			return Result{
				AssertionType: a.Type(),
				Passed:        false,
				Expected:      "tool call args to be non-null",
				Actual:        fmt.Sprintf("tool %s called with null args", t.ToolName),
				Message:       fmt.Sprintf("tool %s: args are null", t.ToolName),
				DocketHint:    "tool.args_invalid",
			}
		}
	}

	return Result{
		AssertionType: a.Type(),
		Passed:        true,
		Message:       "all tool args valid",
	}
}

// EvaluateInvariant checks a simple invariant expression against tool args.
// Supports: "args.X is not null", "args.X == value"
func EvaluateInvariant(invariant string, args json.RawMessage) (bool, string) {
	var argsMap map[string]interface{}
	if err := json.Unmarshal(args, &argsMap); err != nil {
		return false, fmt.Sprintf("failed to parse args: %v", err)
	}

	parts := strings.Fields(invariant)
	if len(parts) < 3 {
		return false, fmt.Sprintf("invalid invariant expression: %s", invariant)
	}

	fieldPath := parts[0]
	// Strip "args." prefix
	fieldPath = strings.TrimPrefix(fieldPath, "args.")

	value, exists := argsMap[fieldPath]

	if len(parts) >= 4 && parts[1] == "is" && parts[2] == "not" && parts[3] == "null" {
		if !exists || value == nil {
			return false, fmt.Sprintf("args.%s is null", fieldPath)
		}
		return true, ""
	}

	if parts[1] == "==" {
		expected := strings.Join(parts[2:], " ")
		actual := fmt.Sprintf("%v", value)
		if actual != expected {
			return false, fmt.Sprintf("args.%s == %v, expected %s", fieldPath, value, expected)
		}
		return true, ""
	}

	return false, fmt.Sprintf("unsupported invariant operator: %s", parts[1])
}
