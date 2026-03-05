package output

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pmclSF/gauntlet/internal/assertions"
)

// ---------------------------------------------------------------------------
// WriteResults — creates valid JSON
// ---------------------------------------------------------------------------

func TestWriteResults_CreatesValidJSON(t *testing.T) {
	dir := t.TempDir()

	result := &RunResult{
		Version:           "1.0",
		Suite:             "test_suite",
		Commit:            "abc123",
		StartedAt:         time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		DurationMs:        5000,
		BudgetMs:          60000,
		BudgetRemainingMs: 55000,
		Mode:              "ci",
		EgressBlocked:     true,
		Summary: Summary{
			Total:         3,
			Passed:        2,
			Failed:        1,
			SkippedBudget: 0,
			Error:         0,
		},
		Scenarios: []ScenarioResult{
			{
				Name:       "scenario_pass",
				Status:     "passed",
				DurationMs: 1000,
				Assertions: []assertions.Result{
					{AssertionType: "tool_sequence", Passed: true, Message: "ok"},
				},
				DocketTags: nil,
				PrimaryTag: "",
			},
			{
				Name:       "scenario_fail",
				Status:     "failed",
				DurationMs: 2000,
				Assertions: []assertions.Result{
					{AssertionType: "retry_cap", Passed: false, Message: "exceeded"},
				},
				DocketTags: []string{"planner.retry_storm"},
				PrimaryTag: "planner.retry_storm",
				Culprit: &Culprit{
					Class:      "agent.retry_logic",
					Confidence: "medium",
					Reasoning:  "Agent retried too many times",
				},
			},
		},
	}

	err := WriteResults(dir, result)
	if err != nil {
		t.Fatalf("WriteResults failed: %v", err)
	}

	path := filepath.Join(dir, "results.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read results.json: %v", err)
	}

	// Verify it is valid JSON
	var parsed RunResult
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("results.json is not valid JSON: %v", err)
	}

	// Verify fields round-tripped correctly
	if parsed.Version != "1.0" {
		t.Errorf("Version = %q, want %q", parsed.Version, "1.0")
	}
	if parsed.Suite != "test_suite" {
		t.Errorf("Suite = %q, want %q", parsed.Suite, "test_suite")
	}
	if parsed.Summary.Total != 3 {
		t.Errorf("Summary.Total = %d, want 3", parsed.Summary.Total)
	}
	if parsed.Summary.Passed != 2 {
		t.Errorf("Summary.Passed = %d, want 2", parsed.Summary.Passed)
	}
	if parsed.Summary.Failed != 1 {
		t.Errorf("Summary.Failed = %d, want 1", parsed.Summary.Failed)
	}
	if len(parsed.Scenarios) != 2 {
		t.Errorf("len(Scenarios) = %d, want 2", len(parsed.Scenarios))
	}
	if parsed.Scenarios[1].Culprit == nil {
		t.Error("expected Culprit to be non-nil for failed scenario")
	} else if parsed.Scenarios[1].Culprit.Class != "agent.retry_logic" {
		t.Errorf("Culprit.Class = %q, want %q", parsed.Scenarios[1].Culprit.Class, "agent.retry_logic")
	}
}

func TestWriteResults_CreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "output")

	result := &RunResult{
		Version: "1.0",
		Suite:   "test",
	}

	err := WriteResults(dir, result)
	if err != nil {
		t.Fatalf("WriteResults failed to create nested directory: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "results.json")); err != nil {
		t.Errorf("results.json not found after WriteResults: %v", err)
	}
}

func TestWriteResults_RedactsSensitiveData(t *testing.T) {
	dir := t.TempDir()
	result := &RunResult{
		Version: "1.0",
		Suite:   "redaction",
		Summary: Summary{Total: 1, Failed: 1},
		Scenarios: []ScenarioResult{
			{
				Name:   "leak",
				Status: "failed",
				Assertions: []assertions.Result{
					{
						AssertionType: "sensitive_leak",
						Passed:        false,
						Message:       "found card 4111 1111 1111 1111 and ssn 123-45-6789",
					},
				},
			},
		},
	}

	if err := WriteResults(dir, result); err != nil {
		t.Fatalf("WriteResults failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "results.json"))
	if err != nil {
		t.Fatalf("failed to read results.json: %v", err)
	}
	content := string(data)
	if strings.Contains(content, "4111 1111 1111 1111") || strings.Contains(content, "123-45-6789") {
		t.Fatalf("sensitive data was not redacted in results.json: %s", content)
	}
	if !strings.Contains(content, "[REDACTED]") {
		t.Fatalf("expected redaction marker in results.json, got: %s", content)
	}
}

// ---------------------------------------------------------------------------
// GenerateMarkdown — produces non-empty output
// ---------------------------------------------------------------------------

func TestGenerateMarkdown_NonEmpty(t *testing.T) {
	r := &RunResult{
		Suite:      "demo_suite",
		DurationMs: 10000,
		BudgetMs:   60000,
		Summary: Summary{
			Total:  2,
			Passed: 1,
			Failed: 1,
		},
		Scenarios: []ScenarioResult{
			{
				Name:       "happy_path",
				Status:     "passed",
				DurationMs: 3000,
			},
			{
				Name:       "error_scenario",
				Status:     "failed",
				DurationMs: 5000,
				Assertions: []assertions.Result{
					{
						AssertionType: "tool_sequence",
						Passed:        false,
						Message:       "tool sequence incomplete",
						Expected:      "A -> B -> C",
						Actual:        "A",
					},
				},
				PrimaryTag: "planner.premature_finalize",
				Culprit: &Culprit{
					Class:      "agent.planner",
					Confidence: "medium",
					Reasoning:  "Agent terminated early",
				},
			},
		},
	}

	md := GenerateMarkdown(r)

	if md == "" {
		t.Fatal("GenerateMarkdown returned empty string")
	}

	// Verify essential sections
	checks := []struct {
		label    string
		expected string
	}{
		{"suite header", "demo_suite"},
		{"passed count", "| Passed | 1 |"},
		{"failed count", "| Failed | 1 |"},
		{"duration", "10000ms"},
		{"failed scenario name", "error_scenario"},
		{"culprit class", "agent.planner"},
		{"docket tag", "planner.premature_finalize"},
		{"passed scenario", "happy_path"},
		{"assertion type", "tool_sequence"},
		{"assertion message", "tool sequence incomplete"},
	}

	for _, c := range checks {
		if !strings.Contains(md, c.expected) {
			t.Errorf("markdown missing %s (%q)", c.label, c.expected)
		}
	}
}

func TestGenerateMarkdown_NoFailedScenarios(t *testing.T) {
	r := &RunResult{
		Suite:      "clean_suite",
		DurationMs: 5000,
		BudgetMs:   60000,
		Summary: Summary{
			Total:  1,
			Passed: 1,
		},
		Scenarios: []ScenarioResult{
			{Name: "only_pass", Status: "passed", DurationMs: 2000},
		},
	}

	md := GenerateMarkdown(r)
	if strings.Contains(md, "### Failed Scenarios") {
		t.Error("expected no 'Failed Scenarios' section when all pass")
	}
	if !strings.Contains(md, "only_pass") {
		t.Error("expected passed scenario to appear in markdown")
	}
}

func TestGenerateMarkdown_ErrorsAppearInFailed(t *testing.T) {
	r := &RunResult{
		Suite: "error_suite",
		Summary: Summary{
			Total: 1,
			Error: 1,
		},
		Scenarios: []ScenarioResult{
			{Name: "error_case", Status: "error", DurationMs: 100},
		},
	}

	md := GenerateMarkdown(r)
	if !strings.Contains(md, "### Failed Scenarios") {
		t.Error("expected 'Failed Scenarios' section for error status")
	}
	if !strings.Contains(md, "error_case") {
		t.Error("expected error scenario to appear in failed section")
	}
}

func TestGenerateMarkdown_SoftSignalLabel(t *testing.T) {
	r := &RunResult{
		Suite: "soft_suite",
		Summary: Summary{
			Total:  1,
			Failed: 1,
		},
		Scenarios: []ScenarioResult{
			{
				Name:   "soft_fail",
				Status: "failed",
				Assertions: []assertions.Result{
					{
						AssertionType: "sensitive_leak",
						Passed:        false,
						Soft:          true,
						Message:       "sensitive data found",
					},
				},
			},
		},
	}

	md := GenerateMarkdown(r)
	if !strings.Contains(md, "(soft signal)") {
		t.Error("expected '(soft signal)' label in markdown for soft assertions")
	}
}

// ---------------------------------------------------------------------------
// ClassifyCulprit
// ---------------------------------------------------------------------------

func TestClassifyCulprit_NilForEmptyResults(t *testing.T) {
	c := ClassifyCulprit(nil, nil)
	if c != nil {
		t.Errorf("expected nil culprit for empty results, got %+v", c)
	}
}

func TestClassifyCulprit_ForbiddenTool(t *testing.T) {
	results := []assertions.Result{
		{Passed: false, Soft: false, DocketHint: "tool.forbidden"},
	}

	c := ClassifyCulprit(results, nil)
	if c == nil {
		t.Fatal("expected non-nil culprit")
	}
	if c.Class != "agent.prompt" {
		t.Errorf("Class = %q, want %q", c.Class, "agent.prompt")
	}
	if c.Confidence != "high" {
		t.Errorf("Confidence = %q, want %q", c.Confidence, "high")
	}
}

func TestClassifyCulprit_PrematureFinalize_NominalTools(t *testing.T) {
	results := []assertions.Result{
		{Passed: false, Soft: false, DocketHint: "planner.premature_finalize"},
	}

	c := ClassifyCulprit(results, map[string]string{
		"order_lookup": "nominal",
	})
	if c == nil {
		t.Fatal("expected non-nil culprit")
	}
	if c.Class != "agent.planner" {
		t.Errorf("Class = %q, want %q", c.Class, "agent.planner")
	}
	if c.Confidence != "medium" {
		t.Errorf("Confidence = %q, want %q", c.Confidence, "medium")
	}
}

func TestClassifyCulprit_PrematureFinalize_NonNominalTool(t *testing.T) {
	results := []assertions.Result{
		{Passed: false, Soft: false, DocketHint: "planner.premature_finalize"},
	}

	c := ClassifyCulprit(results, map[string]string{
		"order_lookup": "timeout",
	})
	if c == nil {
		t.Fatal("expected non-nil culprit")
	}
	if c.Class != "tool.state.timeout" {
		t.Errorf("Class = %q, want %q", c.Class, "tool.state.timeout")
	}
	if c.Confidence != "high" {
		t.Errorf("Confidence = %q, want %q", c.Confidence, "high")
	}
}

func TestClassifyCulprit_RetryStorm_TimeoutState(t *testing.T) {
	results := []assertions.Result{
		{Passed: false, Soft: false, DocketHint: "planner.retry_storm"},
	}

	c := ClassifyCulprit(results, map[string]string{
		"api_call": "timeout",
	})
	if c == nil {
		t.Fatal("expected non-nil culprit")
	}
	if c.Class != "tool.state.timeout" {
		t.Errorf("Class = %q, want %q", c.Class, "tool.state.timeout")
	}
}

func TestClassifyCulprit_RetryStorm_ServerError(t *testing.T) {
	results := []assertions.Result{
		{Passed: false, Soft: false, DocketHint: "tool.timeout_retrycap"},
	}

	c := ClassifyCulprit(results, map[string]string{
		"api_call": "server_error",
	})
	if c == nil {
		t.Fatal("expected non-nil culprit")
	}
	if c.Class != "tool.state.server_error" {
		t.Errorf("Class = %q, want %q", c.Class, "tool.state.server_error")
	}
}

func TestClassifyCulprit_RetryStorm_NoToolState(t *testing.T) {
	results := []assertions.Result{
		{Passed: false, Soft: false, DocketHint: "planner.retry_storm"},
	}

	c := ClassifyCulprit(results, map[string]string{})
	if c == nil {
		t.Fatal("expected non-nil culprit")
	}
	if c.Class != "agent.retry_logic" {
		t.Errorf("Class = %q, want %q", c.Class, "agent.retry_logic")
	}
}

func TestClassifyCulprit_OutputSchemaMismatch(t *testing.T) {
	results := []assertions.Result{
		{Passed: false, Soft: false, DocketHint: "output.schema_mismatch"},
	}

	c := ClassifyCulprit(results, nil)
	if c == nil {
		t.Fatal("expected non-nil culprit")
	}
	if c.Class != "agent.output" {
		t.Errorf("Class = %q, want %q", c.Class, "agent.output")
	}
}

func TestClassifyCulprit_InvalidJSON(t *testing.T) {
	results := []assertions.Result{
		{Passed: false, Soft: false, DocketHint: "output.invalid_json"},
	}

	c := ClassifyCulprit(results, nil)
	if c == nil {
		t.Fatal("expected non-nil culprit")
	}
	if c.Class != "agent.output" {
		t.Errorf("Class = %q, want %q", c.Class, "agent.output")
	}
}

func TestClassifyCulprit_ToolArgsInvalid(t *testing.T) {
	results := []assertions.Result{
		{Passed: false, Soft: false, DocketHint: "tool.args_invalid"},
	}

	c := ClassifyCulprit(results, nil)
	if c == nil {
		t.Fatal("expected non-nil culprit")
	}
	if c.Class != "agent.tool_use" {
		t.Errorf("Class = %q, want %q", c.Class, "agent.tool_use")
	}
}

func TestClassifyCulprit_SkipsSoftSignals(t *testing.T) {
	// Only soft failures should yield the "unknown" fallback
	results := []assertions.Result{
		{Passed: false, Soft: true, DocketHint: "output.sensitive_leak"},
	}

	c := ClassifyCulprit(results, nil)
	if c == nil {
		t.Fatal("expected non-nil culprit (fallback)")
	}
	if c.Class != "unknown" {
		t.Errorf("Class = %q, want %q (soft signals should be skipped)", c.Class, "unknown")
	}
	if c.Confidence != "low" {
		t.Errorf("Confidence = %q, want %q", c.Confidence, "low")
	}
}

func TestClassifyCulprit_SkipsPassedResults(t *testing.T) {
	results := []assertions.Result{
		{Passed: true, Soft: false, DocketHint: "output.schema_mismatch"},
	}

	c := ClassifyCulprit(results, nil)
	if c == nil {
		t.Fatal("expected non-nil culprit (fallback)")
	}
	if c.Class != "unknown" {
		t.Errorf("Class = %q, want %q (passed results should be skipped)", c.Class, "unknown")
	}
}

func TestClassifyCulprit_FirstHardFailureWins(t *testing.T) {
	results := []assertions.Result{
		{Passed: true, Soft: false, DocketHint: "tool.forbidden"},
		{Passed: false, Soft: false, DocketHint: "output.schema_mismatch"},
		{Passed: false, Soft: false, DocketHint: "tool.forbidden"},
	}

	c := ClassifyCulprit(results, nil)
	if c == nil {
		t.Fatal("expected non-nil culprit")
	}
	// First hard failure is "output.schema_mismatch" (index 1 — index 0 passed)
	if c.Class != "agent.output" {
		t.Errorf("Class = %q, want %q (first hard failure should win)", c.Class, "agent.output")
	}
}

// ---------------------------------------------------------------------------
// WriteSummary
// ---------------------------------------------------------------------------

func TestWriteSummary_CreatesFile(t *testing.T) {
	dir := t.TempDir()

	r := &RunResult{
		Suite: "summary_test",
		Summary: Summary{
			Total:  1,
			Passed: 1,
		},
		Scenarios: []ScenarioResult{
			{Name: "test", Status: "passed", DurationMs: 100},
		},
	}

	err := WriteSummary(dir, r)
	if err != nil {
		t.Fatalf("WriteSummary failed: %v", err)
	}

	path := filepath.Join(dir, "summary.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read summary.md: %v", err)
	}
	if len(data) == 0 {
		t.Error("summary.md is empty")
	}
	if !strings.Contains(string(data), "summary_test") {
		t.Error("summary.md does not contain suite name")
	}
}

func TestWriteSummary_RedactsSensitiveData(t *testing.T) {
	dir := t.TempDir()
	r := &RunResult{
		Suite: "summary_redaction",
		Summary: Summary{
			Total:  1,
			Failed: 1,
		},
		Scenarios: []ScenarioResult{
			{
				Name:   "failed_case",
				Status: "failed",
				Assertions: []assertions.Result{
					{
						AssertionType: "output_schema",
						Passed:        false,
						Message:       "returned ssn 123-45-6789",
					},
				},
			},
		},
	}

	if err := WriteSummary(dir, r); err != nil {
		t.Fatalf("WriteSummary failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "summary.md"))
	if err != nil {
		t.Fatalf("failed to read summary.md: %v", err)
	}
	content := string(data)
	if strings.Contains(content, "123-45-6789") {
		t.Fatalf("sensitive data was not redacted in summary.md: %s", content)
	}
	if !strings.Contains(content, "[REDACTED]") {
		t.Fatalf("expected redaction marker in summary.md, got: %s", content)
	}
}

func TestWriteArtifactBundle_RedactsSensitiveData(t *testing.T) {
	runDir := t.TempDir()
	sr := ScenarioResult{
		Name:   "artifact_redaction",
		Status: "failed",
		Assertions: []assertions.Result{
			{
				AssertionType: "output_derivable",
				Passed:        false,
				Message:       "credit card 4111-1111-1111-1111 leaked",
			},
		},
	}

	input := map[string]interface{}{
		"api_key": "top-secret",
		"note":    "card 4111-1111-1111-1111",
	}
	if err := WriteArtifactBundle(runDir, "artifact_redaction", sr, input, nil, nil, nil, nil); err != nil {
		t.Fatalf("WriteArtifactBundle failed: %v", err)
	}

	inputData, err := os.ReadFile(filepath.Join(runDir, "artifact_redaction", "input.json"))
	if err != nil {
		t.Fatalf("failed to read input.json: %v", err)
	}
	inputContent := string(inputData)
	if strings.Contains(inputContent, "top-secret") || strings.Contains(inputContent, "4111-1111-1111-1111") {
		t.Fatalf("sensitive data was not redacted in input.json: %s", inputContent)
	}
	if !strings.Contains(inputContent, "[REDACTED]") {
		t.Fatalf("expected redaction marker in input.json, got: %s", inputContent)
	}

	summaryData, err := os.ReadFile(filepath.Join(runDir, "artifact_redaction", "summary.md"))
	if err != nil {
		t.Fatalf("failed to read artifact summary.md: %v", err)
	}
	summaryContent := string(summaryData)
	if strings.Contains(summaryContent, "4111-1111-1111-1111") {
		t.Fatalf("sensitive data was not redacted in artifact summary.md: %s", summaryContent)
	}
}
