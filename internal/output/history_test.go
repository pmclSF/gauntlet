package output

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPopulateHistoryMetadata_AttachesRecentHistoryAndDelta(t *testing.T) {
	root := t.TempDir()
	runsRoot := filepath.Join(root, "evals", "runs")

	writeRunResult(t, filepath.Join(runsRoot, "run-older"), RunResult{
		Suite:     "smoke",
		Commit:    "c1",
		StartedAt: time.Date(2026, time.March, 1, 10, 0, 0, 0, time.UTC),
		Summary: Summary{
			Total:  2,
			Passed: 2,
			Failed: 0,
		},
	})
	writeRunResult(t, filepath.Join(runsRoot, "run-newer"), RunResult{
		Suite:     "smoke",
		Commit:    "c2",
		StartedAt: time.Date(2026, time.March, 3, 10, 0, 0, 0, time.UTC),
		Summary: Summary{
			Total:  2,
			Passed: 1,
			Failed: 1,
		},
	})
	writeRunResult(t, filepath.Join(runsRoot, "run-other-suite"), RunResult{
		Suite:     "nightly",
		Commit:    "c3",
		StartedAt: time.Date(2026, time.March, 4, 10, 0, 0, 0, time.UTC),
		Summary: Summary{
			Total:  5,
			Passed: 5,
		},
	})

	currentOutputDir := filepath.Join(runsRoot, "run-current")
	if err := os.MkdirAll(currentOutputDir, 0o755); err != nil {
		t.Fatalf("mkdir current output dir: %v", err)
	}
	current := &RunResult{
		Suite:     "smoke",
		Commit:    "c4",
		StartedAt: time.Date(2026, time.March, 4, 11, 0, 0, 0, time.UTC),
		Summary: Summary{
			Total:  2,
			Passed: 0,
			Failed: 2,
		},
	}

	if err := PopulateHistoryMetadata(current, currentOutputDir); err != nil {
		t.Fatalf("PopulateHistoryMetadata: %v", err)
	}
	if current.History == nil {
		t.Fatal("expected history metadata")
	}
	if current.History.Window != defaultHistoryWindow {
		t.Fatalf("history window = %d, want %d", current.History.Window, defaultHistoryWindow)
	}
	if len(current.History.Recent) != 2 {
		t.Fatalf("recent history len = %d, want 2", len(current.History.Recent))
	}
	if current.History.Previous == nil {
		t.Fatal("expected previous run metadata")
	}
	if current.History.Previous.RunID != "run-newer" {
		t.Fatalf("previous run id = %q, want run-newer", current.History.Previous.RunID)
	}
	if current.History.Delta == nil {
		t.Fatal("expected history delta")
	}
	if current.History.Delta.Passed != -1 {
		t.Fatalf("delta.passed = %d, want -1", current.History.Delta.Passed)
	}
	if current.History.Delta.Failed != 1 {
		t.Fatalf("delta.failed = %d, want 1", current.History.Delta.Failed)
	}
	if diff := math.Abs(current.History.Delta.PassRate - (-0.5)); diff > 1e-9 {
		t.Fatalf("delta.pass_rate = %f, want -0.5", current.History.Delta.PassRate)
	}
}

func TestPopulateHistoryMetadata_NoPriorRuns(t *testing.T) {
	root := t.TempDir()
	outputDir := filepath.Join(root, "evals", "runs", "run-current")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir output dir: %v", err)
	}

	result := &RunResult{Suite: "smoke", Summary: Summary{Total: 1, Passed: 1}}
	if err := PopulateHistoryMetadata(result, outputDir); err != nil {
		t.Fatalf("PopulateHistoryMetadata: %v", err)
	}
	if result.History != nil {
		t.Fatalf("expected no history metadata, got %+v", result.History)
	}
}

func TestPopulateHistoryMetadata_SkipsMalformedResults(t *testing.T) {
	root := t.TempDir()
	runsRoot := filepath.Join(root, "evals", "runs")
	badRunDir := filepath.Join(runsRoot, "run-bad")
	if err := os.MkdirAll(badRunDir, 0o755); err != nil {
		t.Fatalf("mkdir bad run: %v", err)
	}
	if err := os.WriteFile(filepath.Join(badRunDir, "results.json"), []byte("{invalid"), 0o644); err != nil {
		t.Fatalf("write malformed results: %v", err)
	}
	writeRunResult(t, filepath.Join(runsRoot, "run-good"), RunResult{
		Suite:     "smoke",
		Commit:    "good",
		StartedAt: time.Date(2026, time.March, 2, 10, 0, 0, 0, time.UTC),
		Summary: Summary{
			Total:  1,
			Passed: 1,
		},
	})

	currentOutputDir := filepath.Join(runsRoot, "run-current")
	if err := os.MkdirAll(currentOutputDir, 0o755); err != nil {
		t.Fatalf("mkdir current output dir: %v", err)
	}
	result := &RunResult{Suite: "smoke", Summary: Summary{Total: 1, Failed: 1}}
	if err := PopulateHistoryMetadata(result, currentOutputDir); err != nil {
		t.Fatalf("PopulateHistoryMetadata: %v", err)
	}
	if result.History == nil || result.History.Previous == nil {
		t.Fatalf("expected valid history metadata after skipping malformed run, got %+v", result.History)
	}
	if result.History.Previous.RunID != "run-good" {
		t.Fatalf("previous run id = %q, want run-good", result.History.Previous.RunID)
	}
}

func writeRunResult(t *testing.T, runDir string, result RunResult) {
	t.Helper()
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatalf("mkdir run dir: %v", err)
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal run result: %v", err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "results.json"), data, 0o644); err != nil {
		t.Fatalf("write run result: %v", err)
	}
}
