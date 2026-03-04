package output

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const defaultHistoryWindow = 5

// PopulateHistoryMetadata enriches result with recent-run metadata for the
// same suite. This is best-effort and skips malformed prior artifacts.
func PopulateHistoryMetadata(result *RunResult, outputDir string) error {
	if result == nil {
		return nil
	}
	outputDir = filepath.Clean(strings.TrimSpace(outputDir))
	if outputDir == "" || outputDir == "." {
		return nil
	}

	history, err := loadRecentSuiteHistory(filepath.Dir(outputDir), outputDir, result.Suite, defaultHistoryWindow)
	if err != nil || len(history) == 0 {
		return err
	}

	meta := &HistoryMetadata{
		Window: defaultHistoryWindow,
		Recent: history,
	}
	previous := history[0]
	meta.Previous = &previous
	meta.Delta = &SummaryDelta{
		Passed:        result.Summary.Passed - previous.Summary.Passed,
		Failed:        result.Summary.Failed - previous.Summary.Failed,
		SkippedBudget: result.Summary.SkippedBudget - previous.Summary.SkippedBudget,
		Error:         result.Summary.Error - previous.Summary.Error,
		PassRate:      currentPassRate(result.Summary) - currentPassRate(previous.Summary),
	}
	result.History = meta
	return nil
}

func loadRecentSuiteHistory(runsRoot, currentOutputDir, suite string, window int) ([]HistoryRun, error) {
	if window <= 0 {
		return nil, nil
	}
	entries, err := os.ReadDir(runsRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	currentOutputDir = filepath.Clean(currentOutputDir)
	targetSuite := strings.TrimSpace(suite)

	history := make([]HistoryRun, 0, window)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		runDir := filepath.Join(runsRoot, entry.Name())
		if filepath.Clean(runDir) == currentOutputDir {
			continue
		}
		resultPath := filepath.Join(runDir, "results.json")
		data, readErr := os.ReadFile(resultPath)
		if readErr != nil {
			continue
		}
		var parsed RunResult
		if unmarshalErr := json.Unmarshal(data, &parsed); unmarshalErr != nil {
			continue
		}
		if targetSuite != "" && parsed.Suite != targetSuite {
			continue
		}
		history = append(history, HistoryRun{
			RunID:      entry.Name(),
			Commit:     parsed.Commit,
			StartedAt:  parsed.StartedAt,
			DurationMs: parsed.DurationMs,
			Summary:    parsed.Summary,
		})
	}
	sort.Slice(history, func(i, j int) bool {
		if history[i].StartedAt.Equal(history[j].StartedAt) {
			return history[i].RunID > history[j].RunID
		}
		return history[i].StartedAt.After(history[j].StartedAt)
	})
	if len(history) > window {
		history = history[:window]
	}
	return history, nil
}

func currentPassRate(summary Summary) float64 {
	total := summary.Total
	if total <= 0 {
		return 0
	}
	return float64(summary.Passed) / float64(total)
}
