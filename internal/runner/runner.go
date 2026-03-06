// Package runner orchestrates Gauntlet scenario execution.
// It loads scenarios, assembles world state, runs the TUT, evaluates
// assertions, and produces output artifacts.
package runner

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/pmclSF/gauntlet/internal/output"
	"github.com/pmclSF/gauntlet/internal/world"
)

// Run executes all scenarios in the suite and produces output artifacts.
func (r *Runner) Run(ctx context.Context) (*output.RunResult, error) {
	startTime := time.Now()
	requiresBlockedEgress := modeRequiresBlockedEgress(r.Config.Mode)
	egressStatus := EgressUnknown
	if requiresBlockedEgress {
		var err error
		egressStatus, err = r.runEgressSelfTest()
		if err != nil {
			return nil, err
		}
	}

	paths := resolvePaths(r.Config)

	scenarios, err := loadAndFilterScenarios(paths.SuiteDir, r.Config.ScenarioFilter, r.Config.Suite)
	if err != nil {
		return nil, err
	}

	worldState, err := world.Assemble(paths.ToolsDir, paths.DBDir)
	if err != nil {
		return nil, fmt.Errorf("failed to assemble world: %w", err)
	}
	if err := validateWorldToolRefs(scenarios, worldState); err != nil {
		return nil, err
	}

	// Budget
	budgetMs := r.Config.BudgetMs
	if budgetMs == 0 {
		budgetMs = 300000 // 5 minutes default
	}
	scenarioBudgetMs := r.Config.ScenarioBudgetMs
	if scenarioBudgetMs <= 0 {
		scenarioBudgetMs = budgetMs
	}

	result := &output.RunResult{
		Version:          "1",
		Suite:            r.Config.Suite,
		Commit:           getCommit(),
		StartedAt:        startTime,
		BudgetMs:         budgetMs,
		ScenarioBudgetMs: scenarioBudgetMs,
		Mode:             r.Config.Mode,
		EgressBlocked:    requiresBlockedEgress && egressStatus == EgressBlocked,
	}

	var executions []scenarioExecution
	// Run each scenario
	for _, s := range scenarios {
		elapsed := time.Since(startTime).Milliseconds()
		remainingSuiteMs := budgetMs - elapsed
		if remainingSuiteMs <= 0 {
			result.Scenarios = append(result.Scenarios, output.ScenarioResult{
				Name:            s.Name,
				Status:          "skipped_budget",
				FailureCategory: "budget_exhausted",
			})
			result.Summary.SkippedBudget++
			continue
		}

		effectiveScenarioBudgetMs := scenarioBudgetMs
		if effectiveScenarioBudgetMs <= 0 || remainingSuiteMs < effectiveScenarioBudgetMs {
			effectiveScenarioBudgetMs = remainingSuiteMs
		}
		exec := r.runScenario(ctx, s, worldState, paths.BaselineDir, requiresBlockedEgress, effectiveScenarioBudgetMs)
		sr := exec.Result
		sr.FailureCategory = inferFailureCategory(sr)
		exec.Result = sr
		executions = append(executions, exec)
		result.Scenarios = append(result.Scenarios, sr)

		switch sr.Status {
		case "passed":
			result.Summary.Passed++
		case "failed":
			result.Summary.Failed++
		case "error":
			result.Summary.Error++
		}
		if r.Config.FailFast && (sr.Status == "failed" || sr.Status == "error") {
			break
		}
	}

	result.Summary.Total = len(result.Scenarios)
	result.DurationMs = time.Since(startTime).Milliseconds()
	result.BudgetRemainingMs = budgetMs - result.DurationMs

	// Write output artifacts
	outputDir := r.Config.OutputDir
	if outputDir == "" {
		outputDir = filepath.Join(paths.EvalsDir, "runs", fmt.Sprintf("%s-%s", startTime.Format("20060102-150405"), result.Commit))
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return result, fmt.Errorf("failed to create output directory: %w", err)
	}

	if err := output.PopulateHistoryMetadata(result, outputDir); err != nil {
		log.Printf("warning: failed to populate run history metadata: %v", err)
	}

	if err := output.WriteResults(outputDir, result); err != nil {
		return result, fmt.Errorf("failed to write results.json: %w", err)
	}
	if err := output.WriteSummary(outputDir, result); err != nil {
		return result, fmt.Errorf("failed to write summary.md: %w", err)
	}

	// Write artifact bundles for failures
	var artifactErrors []string
	for _, exec := range executions {
		sr := exec.Result
		if sr.Status == "failed" || sr.Status == "error" {
			if err := output.WriteArtifactBundleWithLimit(outputDir, sr.Name, sr, exec.Input, exec.WorldSpec, exec.ToolTrace, exec.Baseline, exec.PROutput, r.Config.MaxArtifactBytes); err != nil {
				artifactErrors = append(artifactErrors, fmt.Sprintf("scenario %q: %v", sr.Name, err))
			}
		}
	}
	if len(artifactErrors) > 0 {
		return result, fmt.Errorf("failed to write artifact bundles:\n  - %s", strings.Join(artifactErrors, "\n  - "))
	}

	return result, nil
}

func (r *Runner) runEgressSelfTest() (EgressStatus, error) {
	status := checkEgressBlockedFn()
	if status != EgressBlocked {
		return status, fmt.Errorf(
			"egress self-test failed: mode %q requires blocked network egress, but outbound socket probe reported %s; enforce OS-level egress blocking before running (or use --runner-mode local/nightly)",
			r.Config.Mode,
			status.String(),
		)
	}
	return status, nil
}

func getCommit() string {
	out, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}
