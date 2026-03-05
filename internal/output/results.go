// Package output produces Gauntlet run artifacts: results.json,
// Markdown summaries, and per-failure artifact bundles.
package output

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/pmclSF/gauntlet/internal/assertions"
	"github.com/pmclSF/gauntlet/internal/redaction"
)

// RunResult is the top-level results.json structure.
type RunResult struct {
	Version           string           `json:"version"`
	Suite             string           `json:"suite"`
	Commit            string           `json:"commit"`
	StartedAt         time.Time        `json:"started_at"`
	DurationMs        int64            `json:"duration_ms"`
	BudgetMs          int64            `json:"budget_ms"`
	ScenarioBudgetMs  int64            `json:"scenario_budget_ms,omitempty"`
	BudgetRemainingMs int64            `json:"budget_remaining_ms"`
	Mode              string           `json:"mode"`
	EgressBlocked     bool             `json:"egress_blocked"`
	History           *HistoryMetadata `json:"history,omitempty"`
	Summary           Summary          `json:"summary"`
	Scenarios         []ScenarioResult `json:"scenarios"`
}

// Summary holds aggregate counts.
type Summary struct {
	Total         int `json:"total"`
	Passed        int `json:"passed"`
	Failed        int `json:"failed"`
	SkippedBudget int `json:"skipped_budget"`
	Error         int `json:"error"`
}

// HistoryMetadata summarizes recent run outcomes for the same suite.
type HistoryMetadata struct {
	Window   int           `json:"window"`
	Recent   []HistoryRun  `json:"recent,omitempty"`
	Previous *HistoryRun   `json:"previous,omitempty"`
	Delta    *SummaryDelta `json:"delta,omitempty"`
}

// HistoryRun stores summary data for a prior run artifact.
type HistoryRun struct {
	RunID      string    `json:"run_id"`
	Commit     string    `json:"commit"`
	StartedAt  time.Time `json:"started_at"`
	DurationMs int64     `json:"duration_ms"`
	Summary    Summary   `json:"summary"`
}

// SummaryDelta records current-run deltas relative to the previous run.
type SummaryDelta struct {
	Passed        int     `json:"passed"`
	Failed        int     `json:"failed"`
	SkippedBudget int     `json:"skipped_budget"`
	Error         int     `json:"error"`
	PassRate      float64 `json:"pass_rate"`
}

// ScenarioResult is the result of a single scenario.
type ScenarioResult struct {
	Name            string              `json:"name"`
	Status          string              `json:"status"` // passed, failed, skipped_budget, error
	FailureCategory string              `json:"failure_category,omitempty"`
	BudgetMs        int64               `json:"budget_ms,omitempty"`
	DurationMs      int64               `json:"duration_ms"`
	Assertions      []assertions.Result `json:"assertions"`
	DocketTags      []string            `json:"docket_tags"`
	PrimaryTag      string              `json:"primary_tag"`
	Culprit         *Culprit            `json:"culprit,omitempty"`
}

// Culprit identifies the most likely cause of a failure.
type Culprit struct {
	Class      string `json:"class"`      // e.g. "db.seed.conflicting_state"
	Confidence string `json:"confidence"` // high, medium, low
	Reasoning  string `json:"reasoning"`
}

// WriteResults writes results.json to the output directory.
func WriteResults(dir string, result *RunResult) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal results: %w", err)
	}
	redacted, err := redaction.DefaultRedactor().RedactJSON(data)
	if err != nil {
		return fmt.Errorf("failed to redact results: %w", err)
	}

	path := filepath.Join(dir, "results.json")
	return os.WriteFile(path, redacted, 0o644)
}
