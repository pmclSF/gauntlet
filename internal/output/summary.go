package output

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pmclSF/gauntlet/internal/redaction"
)

// WriteSummary writes a GitHub-flavored Markdown summary.
func WriteSummary(dir string, result *RunResult) error {
	md := redaction.DefaultRedactor().RedactString(GenerateMarkdown(result))
	path := filepath.Join(dir, "summary.md")
	return os.WriteFile(path, []byte(md), 0o644)
}

// GenerateMarkdown produces the Markdown summary string.
func GenerateMarkdown(r *RunResult) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("## Gauntlet — %s suite\n\n", r.Suite))

	// Summary table
	sb.WriteString("| | Count |\n|---|---|\n")
	sb.WriteString(fmt.Sprintf("| Passed | %d |\n", r.Summary.Passed))
	sb.WriteString(fmt.Sprintf("| Failed | %d |\n", r.Summary.Failed))
	sb.WriteString(fmt.Sprintf("| Skipped (budget) | %d |\n", r.Summary.SkippedBudget))
	if r.Summary.Error > 0 {
		sb.WriteString(fmt.Sprintf("| Error | %d |\n", r.Summary.Error))
	}
	sb.WriteString("\n")

	sb.WriteString(fmt.Sprintf("**Duration:** %dms / %dms budget\n\n", r.DurationMs, r.BudgetMs))

	// Failed scenarios
	var failed []ScenarioResult
	for _, s := range r.Scenarios {
		if s.Status == "failed" || s.Status == "error" {
			failed = append(failed, s)
		}
	}

	if len(failed) > 0 {
		sb.WriteString("### Failed Scenarios\n\n")
		for _, s := range failed {
			sb.WriteString(fmt.Sprintf("#### %s\n", s.Name))
			if s.Culprit != nil {
				sb.WriteString(fmt.Sprintf("**Culprit:** `%s` (%s confidence)\n", s.Culprit.Class, s.Culprit.Confidence))
			}
			if s.PrimaryTag != "" {
				sb.WriteString(fmt.Sprintf("**Docket:** `%s`\n", s.PrimaryTag))
			}

			// Show failing assertions
			for _, a := range s.Assertions {
				if !a.Passed {
					softLabel := ""
					if a.Soft {
						softLabel = " (soft signal)"
					}
					sb.WriteString(fmt.Sprintf("**Assertion failed:** `%s`%s — %s\n",
						a.AssertionType, softLabel, a.Message))
				}
			}

			sb.WriteString("\n<details>\n<summary>Full details</summary>\n\n")
			if s.Culprit != nil && s.Culprit.Reasoning != "" {
				sb.WriteString(fmt.Sprintf("**Reasoning:** %s\n\n", s.Culprit.Reasoning))
			}
			for _, a := range s.Assertions {
				if !a.Passed {
					if a.Expected != "" || a.Actual != "" {
						sb.WriteString(fmt.Sprintf("**Expected:** %s\n", a.Expected))
						sb.WriteString(fmt.Sprintf("**Actual:** %s\n\n", a.Actual))
					}
				}
			}
			sb.WriteString("</details>\n\n")
		}
	}

	// Passed scenarios (brief)
	var passed []ScenarioResult
	for _, s := range r.Scenarios {
		if s.Status == "passed" {
			passed = append(passed, s)
		}
	}
	if len(passed) > 0 {
		sb.WriteString("### Passed Scenarios\n\n")
		for _, s := range passed {
			sb.WriteString(fmt.Sprintf("- %s (%dms)\n", s.Name, s.DurationMs))
		}
	}

	return sb.String()
}
