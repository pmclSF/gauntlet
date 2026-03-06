package assertions

import (
	"fmt"
	"regexp"
	"strconv"
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
	{"ssn", regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`)},
	{"api_key_pattern", regexp.MustCompile(`(?i)(sk-|api[_-]?key|bearer\s+)[a-zA-Z0-9]{20,}`)},
}

var creditCardCandidatePattern = regexp.MustCompile(`\b(?:\d[\s-]?){13,19}\b`)

func (a *SensitiveLeakAssertion) Evaluate(ctx Context) Result {
	output := string(ctx.Output.Raw)
	var matches []string

	for _, candidate := range creditCardCandidatePattern.FindAllString(output, -1) {
		digits := extractDigits(candidate)
		if len(digits) < 13 || len(digits) > 19 {
			continue
		}
		if !luhnValid(digits) {
			continue
		}
		matches = append(matches, fmt.Sprintf("credit_card: %s", maskMatch(candidate)))
		break
	}

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

func extractDigits(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func luhnValid(digits string) bool {
	sum := 0
	double := false
	for i := len(digits) - 1; i >= 0; i-- {
		d, err := strconv.Atoi(string(digits[i]))
		if err != nil {
			return false
		}
		if double {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		double = !double
	}
	return sum > 0 && sum%10 == 0
}
