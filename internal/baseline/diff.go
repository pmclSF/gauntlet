package baseline

import (
	"fmt"
	"strings"
)

// GenerateDiff produces a human-readable structured diff between baseline
// and actual results.
func GenerateDiff(mismatches []Mismatch) string {
	if len(mismatches) == 0 {
		return "No differences from baseline."
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Baseline diff (%d mismatches):\n", len(mismatches)))
	for i, m := range mismatches {
		sb.WriteString(fmt.Sprintf("\n  [%d] %s\n", i+1, m.Field))
		sb.WriteString(fmt.Sprintf("    Expected: %s\n", m.Expected))
		sb.WriteString(fmt.Sprintf("    Actual:   %s\n", m.Actual))
		if m.Message != "" {
			sb.WriteString(fmt.Sprintf("    Detail:   %s\n", m.Message))
		}
	}
	return sb.String()
}
