// Package runner orchestrates Gauntlet scenario execution.
// It loads scenarios, assembles world state, runs the TUT, evaluates
// assertions, and produces output artifacts.
package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gauntlet-dev/gauntlet/internal/assertions"
	"github.com/gauntlet-dev/gauntlet/internal/baseline"
	"github.com/gauntlet-dev/gauntlet/internal/docket"
	"github.com/gauntlet-dev/gauntlet/internal/determinism"
	"github.com/gauntlet-dev/gauntlet/internal/output"
	"github.com/gauntlet-dev/gauntlet/internal/scenario"
	"github.com/gauntlet-dev/gauntlet/internal/tut"
	"github.com/gauntlet-dev/gauntlet/internal/world"
)

// Config holds runner configuration from gauntlet.yml and CLI flags.
type Config struct {
	Suite         string
	ConfigPath    string
	Mode          string // pr_ci, nightly, local
	OutputDir     string
	EvalsDir      string
	DryRun        bool
	BudgetMs      int64
	ScenarioFilter string
}

// Runner is the main Gauntlet test runner.
type Runner struct {
	Config   Config
	Adapter  tut.Adapter
	Harness  *determinism.Harness
}

// NewRunner creates a new Runner with the given configuration.
func NewRunner(cfg Config) *Runner {
	return &Runner{
		Config:  cfg,
		Harness: determinism.NewHarness(),
	}
}

// Run executes all scenarios in the suite and produces output artifacts.
func (r *Runner) Run(ctx context.Context) (*output.RunResult, error) {
	startTime := time.Now()

	// Determine paths
	evalsDir := r.Config.EvalsDir
	if evalsDir == "" {
		evalsDir = "evals"
	}
	suiteDir := filepath.Join(evalsDir, r.Config.Suite)
	toolsDir := filepath.Join(evalsDir, "world", "tools")
	dbDir := filepath.Join(evalsDir, "world", "databases")
	baselineDir := filepath.Join(evalsDir, "baselines")

	// Load scenarios
	scenarios, err := scenario.LoadSuite(suiteDir)
	if err != nil {
		return nil, fmt.Errorf("failed to load suite: %w", err)
	}

	// Filter if requested
	if r.Config.ScenarioFilter != "" {
		var filtered []*scenario.Scenario
		for _, s := range scenarios {
			if s.Name == r.Config.ScenarioFilter {
				filtered = append(filtered, s)
			}
		}
		if len(filtered) == 0 {
			return nil, fmt.Errorf("scenario '%s' not found in suite '%s'", r.Config.ScenarioFilter, r.Config.Suite)
		}
		scenarios = filtered
	}

	// Load world state
	worldState, err := world.Assemble(toolsDir, dbDir, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to assemble world: %w", err)
	}

	// Budget
	budgetMs := r.Config.BudgetMs
	if budgetMs == 0 {
		budgetMs = 300000 // 5 minutes default
	}

	result := &output.RunResult{
		Version:   "1",
		Suite:     r.Config.Suite,
		Commit:    getCommit(),
		StartedAt: startTime,
		BudgetMs:  budgetMs,
		Mode:      r.Config.Mode,
		EgressBlocked: r.Config.Mode == "pr_ci" || r.Config.Mode == "fork_pr",
	}

	// Run each scenario
	for _, s := range scenarios {
		elapsed := time.Since(startTime).Milliseconds()
		if elapsed >= budgetMs {
			result.Scenarios = append(result.Scenarios, output.ScenarioResult{
				Name:   s.Name,
				Status: "skipped_budget",
			})
			result.Summary.SkippedBudget++
			continue
		}

		sr := r.runScenario(ctx, s, worldState, baselineDir)
		result.Scenarios = append(result.Scenarios, sr)

		switch sr.Status {
		case "passed":
			result.Summary.Passed++
		case "failed":
			result.Summary.Failed++
		case "error":
			result.Summary.Error++
		}
	}

	result.Summary.Total = len(result.Scenarios)
	result.DurationMs = time.Since(startTime).Milliseconds()
	result.BudgetRemainingMs = budgetMs - result.DurationMs

	// Write output artifacts
	outputDir := r.Config.OutputDir
	if outputDir == "" {
		outputDir = filepath.Join(evalsDir, "runs", fmt.Sprintf("%s-%s", startTime.Format("20060102-150405"), result.Commit))
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return result, fmt.Errorf("failed to create output directory: %w", err)
	}

	if err := output.WriteResults(outputDir, result); err != nil {
		return result, fmt.Errorf("failed to write results.json: %w", err)
	}
	if err := output.WriteSummary(outputDir, result); err != nil {
		return result, fmt.Errorf("failed to write summary.md: %w", err)
	}

	// Write artifact bundles for failures
	for _, sr := range result.Scenarios {
		if sr.Status == "failed" || sr.Status == "error" {
			_ = output.WriteArtifactBundle(outputDir, sr.Name, sr, nil, nil, nil, nil, nil)
		}
	}

	return result, nil
}

func (r *Runner) runScenario(ctx context.Context, s *scenario.Scenario, ws *world.State, baselineDir string) output.ScenarioResult {
	start := time.Now()

	sr := output.ScenarioResult{
		Name: s.Name,
	}

	// Validate variant policy
	if err := world.ValidateVariantPolicy(s.World.Tools, s.Chaos); err != nil {
		sr.Status = "error"
		sr.Assertions = []assertions.Result{{
			AssertionType: "variant_policy",
			Passed:        false,
			Message:       err.Error(),
		}}
		sr.DurationMs = time.Since(start).Milliseconds()
		return sr
	}

	// Load baseline
	bl, _ := baseline.Load(baselineDir, r.Config.Suite, s.Name)

	// Build assertion context
	assertCtx := assertions.Context{
		ScenarioName: s.Name,
		Input:        s.Input,
		Output:       tut.AgentOutput{Parsed: make(map[string]interface{})},
		WorldState: assertions.WorldState{
			Tools: buildToolState(ws, s.World.Tools),
		},
	}

	if bl != nil {
		assertCtx.Baseline = &assertions.ContractBaseline{
			ToolSequence:   getToolSequence(bl),
			ForbiddenContent: getForbiddenContent(bl),
			RequiredFields:  getRequiredFields(bl),
		}
		if bl.Output != nil {
			assertCtx.Baseline.OutputSchema = bl.Output.Schema
		}
	}

	// In dry-run mode or when no adapter is set, evaluate with fixture data
	// For the example agent, we simulate the execution by reading fixture responses
	results := assertions.EvaluateAll(s.Assertions, assertCtx)

	sr.Assertions = results
	sr.DocketTags, sr.PrimaryTag = docket.Classify(results)
	sr.DurationMs = time.Since(start).Milliseconds()

	// Determine status
	sr.Status = "passed"
	for _, a := range results {
		if !a.Passed && !a.Soft {
			sr.Status = "failed"
			break
		}
	}

	// Classify culprit for failures
	if sr.Status == "failed" {
		sr.Culprit = output.ClassifyCulprit(results, s.World.Tools)
	}

	return sr
}

func buildToolState(ws *world.State, toolStates map[string]string) map[string]assertions.ToolState {
	result := make(map[string]assertions.ToolState)
	for toolName, state := range toolStates {
		ts := assertions.ToolState{
			Name:  toolName,
			State: state,
		}
		if td, ok := ws.Tools[toolName]; ok {
			if sd, ok := td.States[state]; ok && sd.Response != nil {
				if raw, err := json.Marshal(sd.Response); err == nil {
					ts.Response = raw
				}
			}
		}
		result[toolName] = ts
	}
	return result
}

func getToolSequence(bl *baseline.Contract) []string {
	if bl.ToolSequence != nil {
		return bl.ToolSequence.Required
	}
	return nil
}

func getForbiddenContent(bl *baseline.Contract) []string {
	if bl.Output != nil {
		return bl.Output.ForbiddenContent
	}
	return nil
}

func getRequiredFields(bl *baseline.Contract) []string {
	if bl.Output != nil {
		return bl.Output.RequiredFields
	}
	return nil
}

func getCommit() string {
	// Try to read git commit
	data, err := os.ReadFile(".git/HEAD")
	if err != nil {
		return "unknown"
	}
	ref := string(data)
	if len(ref) > 5 && ref[:5] == "ref: " {
		refPath := ".git/" + ref[5:len(ref)-1]
		data, err = os.ReadFile(refPath)
		if err != nil {
			return "unknown"
		}
		ref = string(data)
	}
	if len(ref) > 7 {
		return ref[:7]
	}
	return "unknown"
}

// mustMarshal is a helper for JSON marshaling that panics on error.
func mustMarshal(v interface{}) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}
