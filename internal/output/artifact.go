package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/pmclSF/gauntlet/internal/assertions"
	"github.com/pmclSF/gauntlet/internal/determinism"
	"github.com/pmclSF/gauntlet/internal/redaction"
	"github.com/pmclSF/gauntlet/internal/scenario"
	"github.com/pmclSF/gauntlet/internal/tut"
)

// DefaultMaxArtifactBytes is the default upper bound for an artifact bundle.
const DefaultMaxArtifactBytes int64 = 10 * 1024 * 1024

type outputSnapshot struct {
	RawOutput       string          `json:"raw_output,omitempty"`
	CanonicalOutput json.RawMessage `json:"canonical_output,omitempty"`
	StdErr          string          `json:"stderr,omitempty"`
}

// WriteArtifactBundle writes a per-failure artifact bundle for a scenario.
func WriteArtifactBundle(runDir, scenarioName string, sr ScenarioResult, input, worldState, toolTrace, baselineOutput, prOutput interface{}) error {
	return WriteArtifactBundleWithLimit(runDir, scenarioName, sr, input, worldState, toolTrace, baselineOutput, prOutput, DefaultMaxArtifactBytes)
}

// WriteArtifactBundleWithLimit writes a per-failure artifact bundle for a scenario with a
// maximum artifact size cap. If the bundle would exceed maxArtifactBytes, tool trace data
// is truncated and a warning is emitted.
func WriteArtifactBundleWithLimit(runDir, scenarioName string, sr ScenarioResult, input, worldState, toolTrace, baselineOutput, prOutput interface{}, maxArtifactBytes int64) error {
	dir := filepath.Join(runDir, scenarioName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create artifact directory: %w", err)
	}
	if maxArtifactBytes <= 0 {
		maxArtifactBytes = DefaultMaxArtifactBytes
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

	// Write summary.md
	summaryMd := redactor.RedactString(generateScenarioSummary(sr, worldState, baselineSnapshot, prSnapshot))
	summaryPath := filepath.Join(dir, "summary.md")
	summaryBytes := []byte(summaryMd)
	prepared, totalBytes, err := prepareArtifactFiles(files, redactor)
	if err != nil {
		return err
	}
	totalBytes += int64(len(summaryBytes))
	if totalBytes > maxArtifactBytes {
		originalBytes := totalBytes
		truncatedFiles, truncatedTotal, truncated, omitted := truncateToolTraceForBudget(files, redactor, summaryBytes, maxArtifactBytes)
		if truncated {
			prepared = truncatedFiles
			totalBytes = truncatedTotal
			log.Printf("warning: artifact for scenario %q exceeded %d bytes (%d bytes); truncated tool_trace with marker [truncated — %d additional tool calls omitted] (new size: %d bytes)", scenarioName, maxArtifactBytes, originalBytes, omitted, totalBytes)
		} else {
			log.Printf("warning: artifact for scenario %q exceeded %d bytes (%d bytes) and could not be truncated via tool_trace", scenarioName, maxArtifactBytes, originalBytes)
		}
	}

	fileNames := make([]string, 0, len(prepared))
	for name := range prepared {
		fileNames = append(fileNames, name)
	}
	sort.Strings(fileNames)
	for _, name := range fileNames {
		path := filepath.Join(dir, name)
		if err := atomicWrite(path, prepared[name], 0o644); err != nil {
			return fmt.Errorf("failed to write %s: %w", name, err)
		}
	}
	if err := atomicWrite(summaryPath, summaryBytes, 0o644); err != nil {
		return fmt.Errorf("failed to write summary.md: %w", err)
	}

	return nil
}

func prepareArtifactFiles(files map[string]interface{}, redactor *redaction.Redactor) (map[string][]byte, int64, error) {
	prepared := make(map[string][]byte, len(files))
	var totalBytes int64
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
			return nil, 0, fmt.Errorf("failed to marshal %s: %w", name, err)
		}
		redacted, err := redactor.RedactJSON(jsonData)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to redact %s: %w", name, err)
		}
		prepared[name] = redacted
		totalBytes += int64(len(redacted))
	}
	return prepared, totalBytes, nil
}

func truncateToolTraceForBudget(files map[string]interface{}, redactor *redaction.Redactor, summaryBytes []byte, maxArtifactBytes int64) (map[string][]byte, int64, bool, int) {
	rawToolTrace, ok := files["tool_trace.json"]
	if !ok || rawToolTrace == nil {
		prepared, total, err := prepareArtifactFiles(files, redactor)
		if err != nil {
			return nil, 0, false, 0
		}
		return prepared, total + int64(len(summaryBytes)), false, 0
	}

	items, ok := asInterfaceSlice(rawToolTrace)
	if !ok || len(items) == 0 {
		prepared, total, err := prepareArtifactFiles(files, redactor)
		if err != nil {
			return nil, 0, false, 0
		}
		return prepared, total + int64(len(summaryBytes)), false, 0
	}

	bestPrepared := map[string][]byte{}
	var bestTotal int64
	bestFound := false
	bestOmitted := 0

	for keep := len(items); keep >= 0; keep-- {
		omitted := len(items) - keep
		candidate := make([]interface{}, 0, keep+1)
		candidate = append(candidate, items[:keep]...)
		if omitted > 0 {
			candidate = append(candidate, map[string]interface{}{
				"truncated": fmt.Sprintf("[truncated — %d additional tool calls omitted]", omitted),
			})
		}

		candidateFiles := cloneMap(files)
		candidateFiles["tool_trace.json"] = candidate

		prepared, total, err := prepareArtifactFiles(candidateFiles, redactor)
		if err != nil {
			continue
		}
		totalWithSummary := total + int64(len(summaryBytes))
		bestPrepared = prepared
		bestTotal = totalWithSummary
		bestOmitted = omitted
		if totalWithSummary <= maxArtifactBytes {
			bestFound = true
			break
		}
	}
	if len(bestPrepared) == 0 {
		return nil, 0, false, 0
	}
	return bestPrepared, bestTotal, bestFound || bestOmitted > 0, bestOmitted
}

func asInterfaceSlice(value interface{}) ([]interface{}, bool) {
	rv := reflect.ValueOf(value)
	if !rv.IsValid() || rv.Kind() != reflect.Slice {
		return nil, false
	}
	out := make([]interface{}, 0, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		out = append(out, rv.Index(i).Interface())
	}
	return out, true
}

func cloneMap(in map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
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

func generateScenarioSummary(sr ScenarioResult, worldState interface{}, baseline, pr *outputSnapshot) string {
	statusWord := "FAILED"
	if strings.EqualFold(sr.Status, "error") {
		statusWord = "ERROR"
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("%s  %s\n\n", statusWord, sr.Name))

	if sr.Culprit != nil {
		b.WriteString(fmt.Sprintf("Culprit: %s\n", sr.Culprit.Class))
		b.WriteString(fmt.Sprintf("Confidence: %s\n\n", sr.Culprit.Confidence))
	}

	if failing := firstFailingAssertion(sr.Assertions); failing != nil {
		b.WriteString("Failing assertion:\n")
		b.WriteString(fmt.Sprintf("  %s\n", failing.AssertionType))
		if strings.TrimSpace(failing.Expected) != "" {
			b.WriteString(fmt.Sprintf("  Expected: %s\n", failing.Expected))
		}
		if strings.TrimSpace(failing.Actual) != "" {
			b.WriteString(fmt.Sprintf("  Actual:   %s\n", failing.Actual))
		}
		if strings.TrimSpace(failing.Message) != "" {
			b.WriteString(fmt.Sprintf("  Message:  %s\n", failing.Message))
		}
		b.WriteString("\n")
	}

	if formattedWorld := formatWorldStateSummary(worldState); formattedWorld != "" {
		b.WriteString("World state:\n")
		b.WriteString(formattedWorld)
		b.WriteString("\n")
	}

	if baselineText := formatSnapshotLine(baseline); baselineText != "" {
		b.WriteString(fmt.Sprintf("Baseline output: %q\n", baselineText))
	}
	if prText := formatSnapshotLine(pr); prText != "" {
		b.WriteString(fmt.Sprintf("PR output:       %q\n", prText))
	}
	if sr.PrimaryTag != "" {
		b.WriteString(fmt.Sprintf("\nDocket tag: %s\n", sr.PrimaryTag))
		b.WriteString("See docs/docket-tags.md for remediation guidance.\n")
	}

	return b.String()
}

func firstFailingAssertion(results []assertions.Result) *assertions.Result {
	for i := range results {
		if !results[i].Passed {
			return &results[i]
		}
	}
	return nil
}

func formatWorldStateSummary(worldState interface{}) string {
	switch ws := worldState.(type) {
	case scenario.WorldSpec:
		return formatScenarioWorldSpec(ws)
	case *scenario.WorldSpec:
		if ws == nil {
			return ""
		}
		return formatScenarioWorldSpec(*ws)
	default:
		if worldState == nil {
			return ""
		}
		raw, err := json.MarshalIndent(worldState, "", "  ")
		if err != nil {
			return ""
		}
		lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
		for i := range lines {
			lines[i] = "  " + lines[i]
		}
		return strings.Join(lines, "\n")
	}
}

func formatScenarioWorldSpec(ws scenario.WorldSpec) string {
	var lines []string
	if len(ws.Tools) > 0 {
		toolNames := make([]string, 0, len(ws.Tools))
		for name := range ws.Tools {
			toolNames = append(toolNames, name)
		}
		sort.Strings(toolNames)
		lines = append(lines, "  tools:")
		for _, name := range toolNames {
			lines = append(lines, fmt.Sprintf("    %s -> %s", name, ws.Tools[name]))
		}
	}
	if len(ws.Databases) > 0 {
		dbNames := make([]string, 0, len(ws.Databases))
		for name := range ws.Databases {
			dbNames = append(dbNames, name)
		}
		sort.Strings(dbNames)
		lines = append(lines, "  databases:")
		for _, name := range dbNames {
			spec := ws.Databases[name]
			seeds := append([]string{}, spec.SeedSets...)
			sort.Strings(seeds)
			lines = append(lines, fmt.Sprintf("    %s -> %s", name, strings.Join(seeds, ", ")))
		}
	}
	return strings.Join(lines, "\n")
}

func formatSnapshotLine(snapshot *outputSnapshot) string {
	if snapshot == nil {
		return ""
	}
	candidate := strings.TrimSpace(snapshot.RawOutput)
	if candidate == "" && len(snapshot.CanonicalOutput) > 0 {
		candidate = strings.TrimSpace(string(snapshot.CanonicalOutput))
	}
	if candidate == "" {
		candidate = strings.TrimSpace(snapshot.StdErr)
	}
	return candidate
}
