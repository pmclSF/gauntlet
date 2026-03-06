package assertions

import (
	"encoding/json"
	"fmt"
	"strings"
)

// OutputDerivableAssertion checks that factual claims in the output are
// grounded in the world state (fixtures, DB seeds). Soft gate by default.
type OutputDerivableAssertion struct{}

func (a *OutputDerivableAssertion) Type() string { return "output_derivable" }
func (a *OutputDerivableAssertion) IsSoft() bool { return true }

func (a *OutputDerivableAssertion) Evaluate(ctx Context) Result {
	if ctx.Output.Parsed == nil {
		return Result{
			AssertionType: a.Type(),
			Passed:        true,
			Soft:          true,
			Message:       "output is not JSON, skipping derivability check",
		}
	}

	// Collect all string values from the world state
	worldStrings := collectWorldStrings(ctx)

	// Collect all string values from output
	outputStrings := extractStrings(ctx.Output.Parsed)

	// Check each output string > 10 chars for grounding
	var ungrounded []string
	for _, s := range outputStrings {
		if len(s) <= 10 {
			continue
		}
		if !isGrounded(s, worldStrings) {
			ungrounded = append(ungrounded, s)
		}
	}

	if len(ungrounded) > 0 {
		return Result{
			AssertionType: a.Type(),
			Passed:        false,
			Soft:          true,
			Expected:      "all factual claims grounded in world state",
			Actual:        fmt.Sprintf("%d ungrounded claims found", len(ungrounded)),
			Message: fmt.Sprintf("ungrounded claims in output: %s",
				strings.Join(truncateStrings(ungrounded, 3), "; ")),
			DocketHint: "output.ungrounded",
		}
	}

	return Result{
		AssertionType: a.Type(),
		Passed:        true,
		Soft:          true,
		Message:       "all output claims grounded in world state",
	}
}

func collectWorldStrings(ctx Context) map[string]bool {
	strings := make(map[string]bool)

	// From tool fixtures
	for _, t := range ctx.ToolTrace {
		if t.Response != nil {
			for _, s := range extractStringsFromJSON(t.Response) {
				strings[s] = true
			}
		}
	}

	// From DB state
	for _, db := range ctx.WorldState.Databases {
		for _, s := range extractStrings(db.Data) {
			strings[s] = true
		}
	}

	return strings
}

func extractStrings(v interface{}) []string {
	var result []string
	switch val := v.(type) {
	case string:
		result = append(result, val)
	case map[string]interface{}:
		for _, v := range val {
			result = append(result, extractStrings(v)...)
		}
	case []interface{}:
		for _, v := range val {
			result = append(result, extractStrings(v)...)
		}
	}
	return result
}

func extractStringsFromJSON(data json.RawMessage) []string {
	var v interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		return nil
	}
	return extractStrings(v)
}

// isGrounded checks if a string is present (or >80% token overlap) in the world.
func isGrounded(s string, worldStrings map[string]bool) bool {
	// Exact match
	if worldStrings[s] {
		return true
	}

	// Token overlap check (>80%)
	tokens := strings.Fields(strings.ToLower(s))
	if len(tokens) == 0 {
		return true
	}

	for ws := range worldStrings {
		wsTokens := make(map[string]bool)
		for _, t := range strings.Fields(strings.ToLower(ws)) {
			wsTokens[t] = true
		}
		overlap := 0
		for _, t := range tokens {
			if wsTokens[t] {
				overlap++
			}
		}
		if float64(overlap)/float64(len(tokens)) > 0.8 {
			return true
		}
	}
	return false
}

func truncateStrings(ss []string, max int) []string {
	if len(ss) <= max {
		return ss
	}
	result := ss[:max]
	result = append(result, fmt.Sprintf("... and %d more", len(ss)-max))
	return result
}
