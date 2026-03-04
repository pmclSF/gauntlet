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
	toolName, hasTool := specString(ctx.Spec, "tool")
	invariant, hasInvariant := specString(ctx.Spec, "invariant")

	if hasInvariant && invariant != "" {
		matched := false
		for _, t := range ctx.ToolTrace {
			if t.EventType != "tool_call" || t.ToolName == "" {
				continue
			}
			if hasTool && toolName != "" && t.ToolName != toolName {
				continue
			}
			matched = true
			ok, msg := evaluateInvariantWithInput(invariant, t.Args, ctx.Input.Payload)
			if !ok {
				return Result{
					AssertionType: a.Type(),
					Passed:        false,
					Expected:      fmt.Sprintf("invariant %q for tool %s", invariant, t.ToolName),
					Actual:        msg,
					Message:       fmt.Sprintf("tool %s invariant failed: %s", t.ToolName, msg),
					DocketHint:    "tool.args_invalid",
				}
			}
		}
		if !matched {
			return Result{
				AssertionType: a.Type(),
				Passed:        true,
				Message:       "no matching tool calls for invariant check",
			}
		}
		return Result{
			AssertionType: a.Type(),
			Passed:        true,
			Message:       "all tool argument invariants passed",
		}
	}

	// This assertion needs the specific tool name and invariant from spec properties.
	// When not provided, validate all tool calls have non-empty args.
	for _, t := range ctx.ToolTrace {
		if t.EventType != "tool_call" || t.ToolName == "" {
			continue
		}
		if hasTool && toolName != "" && t.ToolName != toolName {
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

func evaluateInvariantWithInput(invariant string, args json.RawMessage, input map[string]interface{}) (bool, string) {
	parts := strings.SplitN(invariant, "==", 2)
	if len(parts) == 2 {
		left := strings.TrimSpace(parts[0])
		right := strings.TrimSpace(parts[1])
		if strings.HasPrefix(left, "args.") && strings.HasPrefix(right, "input.") {
			var argsMap map[string]interface{}
			if err := json.Unmarshal(args, &argsMap); err != nil {
				return false, fmt.Sprintf("failed to parse args: %v", err)
			}
			argField := strings.TrimPrefix(left, "args.")
			inputField := strings.TrimPrefix(right, "input.")
			argVal, argOK := argsMap[argField]
			inputVal, inputOK := input[inputField]
			if !argOK {
				return false, fmt.Sprintf("args.%s is missing", argField)
			}
			if !inputOK {
				return false, fmt.Sprintf("input.%s is missing", inputField)
			}
			if fmt.Sprintf("%v", argVal) != fmt.Sprintf("%v", inputVal) {
				return false, fmt.Sprintf("args.%s == %v, expected input.%s == %v", argField, argVal, inputField, inputVal)
			}
			return true, ""
		}
	}
	return EvaluateInvariant(invariant, args)
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
