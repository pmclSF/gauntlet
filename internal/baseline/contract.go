package baseline

import (
	"fmt"
	"strings"

	"github.com/gauntlet-dev/gauntlet/internal/tut"
)

// Compare checks actual results against a contract baseline.
// Returns a list of mismatches. Empty list = baseline passes.
func Compare(baseline *Contract, toolTrace []tut.TraceEvent, output tut.AgentOutput) []Mismatch {
	if baseline == nil {
		return nil
	}
	var mismatches []Mismatch

	// Tool sequence check
	if baseline.ToolSequence != nil {
		var actual []string
		for _, t := range toolTrace {
			if t.EventType == "tool_call" && t.ToolName != "" {
				actual = append(actual, t.ToolName)
			}
		}

		required := baseline.ToolSequence.Required
		reqIdx := 0
		for _, tool := range actual {
			if reqIdx < len(required) && tool == required[reqIdx] {
				reqIdx++
			}
		}
		if reqIdx < len(required) {
			mismatches = append(mismatches, Mismatch{
				Field:    "tool_sequence",
				Expected: fmt.Sprintf("%v", required),
				Actual:   fmt.Sprintf("%v", actual),
				Message:  fmt.Sprintf("missing tools: %v", required[reqIdx:]),
			})
		}
	}

	// Output checks
	if baseline.Output != nil && output.Parsed != nil {
		// Required fields
		for _, field := range baseline.Output.RequiredFields {
			if _, ok := output.Parsed[field]; !ok {
				mismatches = append(mismatches, Mismatch{
					Field:    "output.required_field",
					Expected: fmt.Sprintf("field '%s' present", field),
					Actual:   "field missing",
					Message:  fmt.Sprintf("required output field '%s' is missing", field),
				})
			}
		}

		// Forbidden content
		outputStr := string(output.Raw)
		for _, forbidden := range baseline.Output.ForbiddenContent {
			if strings.Contains(outputStr, forbidden) {
				mismatches = append(mismatches, Mismatch{
					Field:    "output.forbidden_content",
					Expected: fmt.Sprintf("output must not contain '%s'", forbidden),
					Actual:   "forbidden content found",
					Message:  fmt.Sprintf("output contains forbidden content: '%s'", forbidden),
				})
			}
		}
	}

	return mismatches
}

// Mismatch represents a single baseline contract violation.
type Mismatch struct {
	Field    string `json:"field"`
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
	Message  string `json:"message"`
}
