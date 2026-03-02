package assertions

import (
	"fmt"
	"regexp"
	"strings"
)

// SensitiveLeakAssertion checks for PII/sensitive patterns in output.
// ALWAYS a soft signal — never a hard gate in v1.
type SensitiveLeakAssertion struct{}

func (a *SensitiveLeakAssertion) Type() string { return "sensitive_leak" }
func (a *SensitiveLeakAssertion) IsSoft() bool { return true }

// sensitivePatterns are the default patterns checked.
var sensitivePatterns = []struct {
	name    string
	pattern *regexp.Regexp
}{
	{"credit_card", regexp.MustCompile(`\b\d{4}[\s-]?\d{4}[\s-]?\d{4}[\s-]?\d{1,7}\b`)},
	{"ssn", regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`)},
	{"api_key_pattern", regexp.MustCompile(`(?i)(sk-|api[_-]?key|bearer\s+)[a-zA-Z0-9]{20,}`)},
}

func (a *SensitiveLeakAssertion) Evaluate(ctx Context) Result {
	output := string(ctx.Output.Raw)
	var matches []string

	for _, sp := range sensitivePatterns {
		if loc := sp.pattern.FindString(output); loc != "" {
			matches = append(matches, fmt.Sprintf("%s: %s", sp.name, maskMatch(loc)))
		}
	}

	// Also check for sensitive field paths in parsed output
	if ctx.Output.Parsed != nil {
		sensitiveFields := []string{"api_key", "password", "token", "secret", "authorization"}
		for _, field := range sensitiveFields {
			if _, exists := ctx.Output.Parsed[field]; exists {
				matches = append(matches, fmt.Sprintf("sensitive_field: %s present in output", field))
			}
		}
	}

	if len(matches) > 0 {
		return Result{
			AssertionType: a.Type(),
			Passed:        false,
			Soft:          true,
			Expected:      "no sensitive patterns in output",
			Actual:        fmt.Sprintf("%d sensitive patterns detected", len(matches)),
			Message: fmt.Sprintf("sensitive data patterns found in output: %s",
				strings.Join(matches, "; ")),
			DocketHint: "output.sensitive_leak",
		}
	}

	return Result{
		AssertionType: a.Type(),
		Passed:        true,
		Soft:          true,
		Message:       "no sensitive patterns detected in output",
	}
}

// maskMatch partially masks a sensitive value for safe logging.
func maskMatch(s string) string {
	if len(s) <= 8 {
		return "****"
	}
	return s[:4] + strings.Repeat("*", len(s)-8) + s[len(s)-4:]
}
