package output

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// WriteArtifactBundle writes a per-failure artifact bundle for a scenario.
func WriteArtifactBundle(runDir, scenarioName string, sr ScenarioResult, input, worldState, toolTrace, baselineOutput, prOutput interface{}) error {
	dir := filepath.Join(runDir, scenarioName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create artifact directory: %w", err)
	}

	files := map[string]interface{}{
		"input.json":           input,
		"world_state.json":     worldState,
		"tool_trace.json":      toolTrace,
		"baseline_output.json": baselineOutput,
		"pr_output.json":       prOutput,
		"assertions.json":      sr.Assertions,
	}

	if sr.Culprit != nil {
		files["culprit.json"] = sr.Culprit
	}

	for name, data := range files {
		if data == nil {
			continue
		}
		jsonData, err := json.MarshalIndent(data, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal %s: %w", name, err)
		}
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, jsonData, 0o644); err != nil {
			return fmt.Errorf("failed to write %s: %w", name, err)
		}
	}

	// Write summary.md
	summaryMd := generateScenarioSummary(sr)
	summaryPath := filepath.Join(dir, "summary.md")
	if err := os.WriteFile(summaryPath, []byte(summaryMd), 0o644); err != nil {
		return fmt.Errorf("failed to write summary.md: %w", err)
	}

	return nil
}

func generateScenarioSummary(sr ScenarioResult) string {
	s := fmt.Sprintf("# %s\n\n**Status:** %s\n", sr.Name, sr.Status)
	if sr.Culprit != nil {
		s += fmt.Sprintf("**Culprit:** %s (%s confidence)\n", sr.Culprit.Class, sr.Culprit.Confidence)
		if sr.Culprit.Reasoning != "" {
			s += fmt.Sprintf("**Reasoning:** %s\n", sr.Culprit.Reasoning)
		}
	}
	if sr.PrimaryTag != "" {
		s += fmt.Sprintf("**Docket:** %s\n", sr.PrimaryTag)
	}
	s += "\n## Assertions\n\n"
	for _, a := range sr.Assertions {
		status := "PASS"
		if !a.Passed {
			status = "FAIL"
		}
		s += fmt.Sprintf("- [%s] %s: %s\n", status, a.AssertionType, a.Message)
	}
	return s
}
