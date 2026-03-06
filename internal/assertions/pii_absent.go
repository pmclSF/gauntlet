package assertions

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// PIIAbsentAssertion enforces absence of configured PII patterns.
type PIIAbsentAssertion struct{}

func (a *PIIAbsentAssertion) Type() string { return "pii_absent" }
func (a *PIIAbsentAssertion) IsSoft() bool { return false }

var piiDetectors = map[string]*regexp.Regexp{
	"email":       regexp.MustCompile(`(?i)\b[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}\b`),
	"ssn":         regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`),
	"credit_card": regexp.MustCompile(`\b(?:\d[\s-]?){13,19}\b`),
	"phone_us":    regexp.MustCompile(`\b(?:\+1[\s.-]?)?(?:\(\d{3}\)|\d{3})[\s.-]?\d{3}[\s.-]?\d{4}\b`),
}

func (a *PIIAbsentAssertion) Evaluate(ctx Context) Result {
	detectors := specStringSlice(ctx.Spec, "detectors")
	if len(detectors) == 0 {
		detectors = []string{"email", "ssn", "credit_card", "phone_us"}
	}
	fields := specStringSlice(ctx.Spec, "fields")
	if len(fields) == 0 {
		fields = []string{"output"}
	}

	for _, detector := range detectors {
		if _, ok := piiDetectors[detector]; !ok {
			return Result{
				AssertionType: a.Type(),
				Passed:        false,
				Message:       fmt.Sprintf("pii_absent: unsupported detector %q (supported: email, ssn, credit_card, phone_us)", detector),
				DocketHint:    "assertion.spec_invalid",
			}
		}
	}

	for _, field := range fields {
		text := extractPIIFieldText(field, ctx)
		if strings.TrimSpace(text) == "" {
			continue
		}
		for _, detector := range detectors {
			re := piiDetectors[detector]
			match := re.FindString(text)
			if match == "" {
				continue
			}
			if detector == "credit_card" && !luhnValid(extractDigits(match)) {
				continue
			}
			redacted := redactPIIMatch(detector, match)
			return Result{
				AssertionType: a.Type(),
				Passed:        false,
				Expected:      fmt.Sprintf("no %s in %s", detector, field),
				Actual:        fmt.Sprintf("%s detected in %s", detector, field),
				Message:       fmt.Sprintf("pii_absent: %s detected in %s: %q (redacted)", detector, field, redacted),
				DocketHint:    "output.pii_detected",
			}
		}
	}

	return Result{
		AssertionType: a.Type(),
		Passed:        true,
		Message:       "pii_absent: no configured PII patterns detected",
	}
}

func extractPIIFieldText(field string, ctx Context) string {
	switch field {
	case "output":
		if len(ctx.Output.Raw) > 0 {
			return string(ctx.Output.Raw)
		}
		if ctx.Output.Parsed != nil {
			raw, err := json.Marshal(ctx.Output.Parsed)
			if err == nil {
				return string(raw)
			}
		}
		return ""
	case "tool_args":
		parts := make([]string, 0, len(ctx.ToolTrace))
		for _, event := range ctx.ToolTrace {
			if event.EventType == "tool_call" && len(event.Args) > 0 {
				parts = append(parts, string(event.Args))
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}

func redactPIIMatch(detector, match string) string {
	switch detector {
	case "email":
		at := strings.Index(match, "@")
		if at <= 1 || at == len(match)-1 {
			return "e****.com"
		}
		return match[:1] + "****" + match[at:]
	case "ssn", "phone_us":
		return maskMatch(match)
	case "credit_card":
		digits := extractDigits(match)
		if len(digits) >= 4 {
			return "**** **** **** " + digits[len(digits)-4:]
		}
		return "****"
	default:
		return "****"
	}
}
