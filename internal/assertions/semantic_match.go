package assertions

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"unicode"
)

// SemanticMatchAssertion runs a semantic match judge in trusted nightly mode.
type SemanticMatchAssertion struct{}

func (a *SemanticMatchAssertion) Type() string { return "semantic_match" }
func (a *SemanticMatchAssertion) IsSoft() bool { return true }

func (a *SemanticMatchAssertion) Evaluate(ctx Context) Result {
	prompt, _ := specString(ctx.Spec, "prompt")
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return Result{
			AssertionType: a.Type(),
			Passed:        false,
			Soft:          true,
			Message:       "semantic_match: missing required field 'prompt'",
			DocketHint:    "assertion.spec_invalid",
		}
	}
	threshold, ok := specFloat(ctx.Spec, "threshold")
	if !ok {
		threshold = 0.8
	}
	if threshold < 0 || threshold > 1 {
		return Result{
			AssertionType: a.Type(),
			Passed:        false,
			Soft:          true,
			Message:       "semantic_match: threshold must be between 0.0 and 1.0",
			DocketHint:    "assertion.spec_invalid",
		}
	}

	mode := strings.ToLower(strings.TrimSpace(ctx.RunnerMode))
	if mode != "nightly" {
		return Result{
			AssertionType: a.Type(),
			Passed:        true,
			Soft:          true,
			Message:       "semantic_match skipped in hermetic mode (runs only in nightly mode)",
		}
	}

	outputText := strings.TrimSpace(string(ctx.Output.Raw))
	if outputText == "" {
		outputText = strings.TrimSpace(fmt.Sprintf("%v", ctx.Output.Parsed))
	}
	if outputText == "" {
		return Result{
			AssertionType: a.Type(),
			Passed:        false,
			Soft:          true,
			Message:       "semantic_match: output is empty",
			DocketHint:    "output.semantic_mismatch",
		}
	}

	score := semanticScore(prompt, outputText)
	if envScore := strings.TrimSpace(os.Getenv("GAUNTLET_SEMANTIC_MATCH_SCORE")); envScore != "" {
		if parsed, err := strconv.ParseFloat(envScore, 64); err == nil {
			score = parsed
		}
	}
	if score < threshold {
		return Result{
			AssertionType: a.Type(),
			Passed:        false,
			Soft:          true,
			Expected:      fmt.Sprintf("semantic score >= %.2f", threshold),
			Actual:        fmt.Sprintf("semantic score = %.2f", score),
			Message:       fmt.Sprintf("semantic_match: score %.2f below threshold %.2f", score, threshold),
			DocketHint:    "output.semantic_mismatch",
		}
	}
	return Result{
		AssertionType: a.Type(),
		Passed:        true,
		Soft:          true,
		Message:       fmt.Sprintf("semantic_match: score %.2f meets threshold %.2f", score, threshold),
	}
}

func semanticScore(prompt, output string) float64 {
	required := keywords(prompt)
	if len(required) == 0 {
		return 0
	}
	text := strings.ToLower(output)
	hits := 0
	for _, kw := range required {
		if strings.Contains(text, kw) {
			hits++
		}
	}
	return float64(hits) / float64(len(required))
}

func keywords(text string) []string {
	lower := strings.ToLower(text)
	parts := strings.FieldsFunc(lower, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	out := make([]string, 0, len(parts))
	seen := map[string]bool{}
	for _, part := range parts {
		if len(part) < 4 {
			continue
		}
		if seen[part] {
			continue
		}
		seen[part] = true
		out = append(out, part)
	}
	return out
}
