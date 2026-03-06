package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pmclSF/gauntlet/internal/determinism"
	"github.com/pmclSF/gauntlet/internal/redaction"
	"github.com/pmclSF/gauntlet/internal/tut"
)

type outputSnapshot struct {
	RawOutput       string          `json:"raw_output,omitempty"`
	CanonicalOutput json.RawMessage `json:"canonical_output,omitempty"`
	StdErr          string          `json:"stderr,omitempty"`
}

// WriteArtifactBundle writes a per-failure artifact bundle for a scenario.
func WriteArtifactBundle(runDir, scenarioName string, sr ScenarioResult, input, worldState, toolTrace, baselineOutput, prOutput interface{}) error {
	dir := filepath.Join(runDir, scenarioName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create artifact directory: %w", err)
	}
	redactor := redaction.DefaultRedactor()
	baselineSnapshot, err := normalizeOutputSnapshot(baselineOutput)
	if err != nil {
		return fmt.Errorf("failed to canonicalize baseline output: %w", err)
	}
	prSnapshot, err := normalizeOutputSnapshot(prOutput)
	if err != nil {
		return fmt.Errorf("failed to canonicalize PR output: %w", err)
	}

	files := map[string]interface{}{
		"input.json":           input,
		"world_state.json":     worldState,
		"tool_trace.json":      toolTrace,
		"baseline_output.json": baselineSnapshot,
		"pr_output.json":       prSnapshot,
		"assertions.json":      sr.Assertions,
	}

	if sr.Culprit != nil {
		files["culprit.json"] = sr.Culprit
	}

	fileNames := make([]string, 0, len(files))
	for name := range files {
		fileNames = append(fileNames, name)
	}
	sort.Strings(fileNames)
	for _, name := range fileNames {
		data := files[name]
		if data == nil {
			continue
		}
		jsonData, err := json.MarshalIndent(data, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal %s: %w", name, err)
		}
		redacted, err := redactor.RedactJSON(jsonData)
		if err != nil {
			return fmt.Errorf("failed to redact %s: %w", name, err)
		}
		path := filepath.Join(dir, name)
		if err := atomicWrite(path, redacted, 0o644); err != nil {
			return fmt.Errorf("failed to write %s: %w", name, err)
		}
	}

	// Write summary.md
	summaryMd := redactor.RedactString(generateScenarioSummary(sr))
	summaryPath := filepath.Join(dir, "summary.md")
	if err := atomicWrite(summaryPath, []byte(summaryMd), 0o644); err != nil {
		return fmt.Errorf("failed to write summary.md: %w", err)
	}

	return nil
}

func normalizeOutputSnapshot(output interface{}) (*outputSnapshot, error) {
	switch v := output.(type) {
	case nil:
		return nil, nil
	case *tut.AgentOutput:
		if v == nil {
			return nil, nil
		}
		return snapshotFromAgentOutput(*v)
	case tut.AgentOutput:
		return snapshotFromAgentOutput(v)
	case []byte:
		return snapshotFromRawBytes(v)
	case json.RawMessage:
		return snapshotFromRawBytes(v)
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal output: %w", err)
		}
		return snapshotFromRawBytes(data)
	}
}

func snapshotFromAgentOutput(out tut.AgentOutput) (*outputSnapshot, error) {
	stderr := strings.TrimSpace(out.StdErr)
	if len(bytes.TrimSpace(out.Raw)) > 0 {
		snapshot, err := snapshotFromRawBytes(out.Raw)
		if err == nil {
			snapshot.StdErr = stderr
			return snapshot, nil
		}
		// Preserve raw text for invalid JSON while still attempting canonicalization
		// from parsed output when available.
		if out.Parsed != nil {
			canonical, canonicalErr := determinism.CanonicalizeOutput(out.Parsed)
			if canonicalErr != nil {
				return nil, canonicalErr
			}
			return &outputSnapshot{
				RawOutput:       string(out.Raw),
				CanonicalOutput: json.RawMessage(canonical),
				StdErr:          stderr,
			}, nil
		}
		return &outputSnapshot{
			RawOutput: string(out.Raw),
			StdErr:    stderr,
		}, nil
	}
	if out.Parsed != nil {
		canonical, err := determinism.CanonicalizeOutput(out.Parsed)
		if err != nil {
			return nil, err
		}
		raw, err := json.Marshal(out.Parsed)
		if err != nil {
			return nil, err
		}
		return &outputSnapshot{
			RawOutput:       string(raw),
			CanonicalOutput: json.RawMessage(canonical),
			StdErr:          stderr,
		}, nil
	}
	if stderr != "" {
		return &outputSnapshot{
			StdErr: stderr,
		}, nil
	}
	return nil, nil
}

func snapshotFromRawBytes(raw []byte) (*outputSnapshot, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, nil
	}
	canonical, err := determinism.CanonicalizeJSON(trimmed)
	if err != nil {
		return nil, err
	}
	return &outputSnapshot{
		RawOutput:       string(trimmed),
		CanonicalOutput: json.RawMessage(canonical),
	}, nil
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
		s += "See docs/docket-tags.md for remediation guidance.\n"
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
