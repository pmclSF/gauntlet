package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/pmclSF/gauntlet/internal/assertions"
	"github.com/pmclSF/gauntlet/internal/baseline"
	"github.com/pmclSF/gauntlet/internal/docket"
	"github.com/pmclSF/gauntlet/internal/output"
	"github.com/pmclSF/gauntlet/internal/scenario"
	"github.com/pmclSF/gauntlet/internal/tut"
	"github.com/pmclSF/gauntlet/internal/world"
)

func (r *Runner) runScenario(ctx context.Context, s *scenario.Scenario, ws *world.State, baselineDir string, requiresBlockedEgress bool, scenarioBudgetMs int64) scenarioExecution {
	start := time.Now()
	if scenarioBudgetMs > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(scenarioBudgetMs)*time.Millisecond)
		defer cancel()
	}

	sr := output.ScenarioResult{
		Name:     s.Name,
		BudgetMs: scenarioBudgetMs,
	}
	exec := scenarioExecution{
		Result:    sr,
		Input:     s.Input,
		WorldSpec: s.World,
	}

	// Validate variant policy
	if err := world.ValidateVariantPolicy(s.World.Tools, s.World.Databases, s.Chaos); err != nil {
		sr.Status = "error"
		sr.Assertions = []assertions.Result{{
			AssertionType: "variant_policy",
			Passed:        false,
			Message:       err.Error(),
		}}
		sr.DurationMs = time.Since(start).Milliseconds()
		exec.Result = sr
		return exec
	}

	agentOutput := tut.AgentOutput{
		Parsed: make(map[string]interface{}),
	}
	var toolTrace []tut.TraceEvent
	var capabilityDiagnostics []assertions.Result
	var envDiagnostics []assertions.Result

	var handle tut.Handle
	if r.Adapter != nil && !r.Config.DryRun {
		tutConfig := r.buildTUTConfig(requiresBlockedEgress)
		dbEnv, cleanupDBs, err := prepareScenarioDatabases(ws, s)
		if err != nil {
			sr.Status = "error"
			sr.Assertions = []assertions.Result{{
				AssertionType: "db_setup",
				Passed:        false,
				Message:       err.Error(),
			}}
			sr.DurationMs = time.Since(start).Milliseconds()
			exec.Result = sr
			return exec
		}
		defer cleanupDBs()
		for k, v := range dbEnv {
			tutConfig.Env[k] = v
		}

		started, err := r.Adapter.Start(ctx, tutConfig)
		if err != nil {
			assertionType := "tut_start"
			message := err.Error()
			if scenarioTimeoutExceeded(ctx, err) {
				assertionType = "scenario_timeout"
				message = fmt.Sprintf("scenario exceeded budget (%dms) while starting TUT", scenarioBudgetMs)
			}
			sr.Status = "error"
			sr.Assertions = []assertions.Result{{
				AssertionType: assertionType,
				Passed:        false,
				Message:       message,
			}}
			sr.DurationMs = time.Since(start).Milliseconds()
			exec.Result = sr
			return exec
		}
		handle = started
		defer func() {
			stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = handle.Stop(stopCtx)
		}()
	}

	if handle != nil {
		out, err := handle.Run(ctx, s.Input)
		if err != nil {
			assertionType := "tut_execution"
			message := err.Error()
			if scenarioTimeoutExceeded(ctx, err) {
				assertionType = "scenario_timeout"
				message = fmt.Sprintf("scenario exceeded budget (%dms) during execution", scenarioBudgetMs)
			}
			sr.Status = "error"
			sr.Assertions = []assertions.Result{{
				AssertionType: assertionType,
				Passed:        false,
				Message:       message,
			}}
			sr.DurationMs = time.Since(start).Milliseconds()
			exec.Result = sr
			return exec
		}
		if out != nil {
			agentOutput = *out
			if agentOutput.Parsed == nil {
				agentOutput.Parsed = make(map[string]interface{})
			}
			exec.PROutput = out
		}
		toolTrace = handle.Traces()
		exec.ToolTrace = toolTrace
		capabilityDiagnostics = r.adapterCapabilityDiagnostics(handle, toolTrace)
		envDiagnostics = r.environmentFreezeDiagnostics(handle, toolTrace)
	}

	// Load baseline
	bl, blErr := baseline.Load(baselineDir, r.Config.Suite, s.Name)
	if blErr != nil {
		log.Printf("warning: failed to load baseline for scenario %q: %v", s.Name, blErr)
	}

	// Build assertion context
	assertCtx := assertions.Context{
		ScenarioName: s.Name,
		RunnerMode:   r.Config.Mode,
		Input:        s.Input,
		Output:       agentOutput,
		ToolTrace:    toolTrace,
		WorldState: assertions.WorldState{
			Tools: buildToolState(ws, s.World.Tools),
		},
	}

	if bl != nil {
		assertCtx.Baseline = &assertions.ContractBaseline{
			ToolSequence:     getToolSequence(bl),
			ForbiddenContent: getForbiddenContent(bl),
			RequiredFields:   getRequiredFields(bl),
		}
		if bl.Output != nil {
			assertCtx.Baseline.OutputSchema = bl.Output.Schema
			if len(bl.Output.ExpectedOutput) > 0 {
				exec.Baseline = json.RawMessage(bl.Output.ExpectedOutput)
			}
		}
	}

	results := assertions.EvaluateAll(s.Assertions, assertCtx)
	if handle != nil {
		warnings := r.Harness.Validate(agentOutput, toolTrace)
		for _, w := range warnings {
			results = append(results, assertions.Result{
				AssertionType: w.Type,
				Passed:        false,
				Soft:          true,
				Message:       w.Message,
			})
		}
		results = append(results, capabilityDiagnostics...)
		results = append(results, envDiagnostics...)
	}
	if handle != nil && agentOutput.ExitCode != 0 {
		results = append(results, assertions.Result{
			AssertionType: "tut_exit_nonzero",
			Passed:        false,
			Message:       fmt.Sprintf("agent process exited with code %d", agentOutput.ExitCode),
			DocketHint:    "tut.exit_nonzero",
		})
	}
	results = enforceAssertionMode(results, r.Config.HardGates, r.Config.SoftSignals)

	sr.Assertions = results
	sr.DocketTags, sr.PrimaryTag = docket.Classify(results)
	if firstClass := detectFirstClassDocketTag(results, toolTrace); firstClass != "" {
		sr.DocketTags = []string{firstClass}
		sr.PrimaryTag = firstClass
	}
	sr.DurationMs = time.Since(start).Milliseconds()

	// Determine status
	sr.Status = "passed"
	hasHardFailure := false
	hasHardFailureOtherThanTUTExit := false
	for _, a := range results {
		if !a.Passed && !a.Soft {
			hasHardFailure = true
			if a.AssertionType != "tut_exit_nonzero" {
				hasHardFailureOtherThanTUTExit = true
			}
		}
	}
	if hasHardFailure {
		sr.Status = "failed"
	}
	if handle != nil && agentOutput.ExitCode != 0 && !hasHardFailureOtherThanTUTExit {
		sr.Status = "error"
	}

	// Classify culprit for failures
	if sr.Status == "failed" && !docket.IsFirstClassTag(sr.PrimaryTag) {
		sr.Culprit = output.ClassifyCulprit(results, s.World.Tools)
	}

	exec.Result = sr
	return exec
}

func scenarioTimeoutExceeded(ctx context.Context, err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return true
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(msg, "deadline exceeded") || strings.Contains(msg, "timed out")
}

func inferFailureCategory(sr output.ScenarioResult) string {
	switch sr.Status {
	case "failed":
		return "assertion_failure"
	case "error":
		for _, assertion := range sr.Assertions {
			if assertion.AssertionType == "scenario_timeout" {
				return "timeout"
			}
		}
		return "infra_failure"
	case "skipped_budget":
		return "budget_exhausted"
	case "passed":
		for _, assertion := range sr.Assertions {
			if strings.HasPrefix(assertion.AssertionType, "nondeterminism.") && !assertion.Passed {
				return "nondeterminism_warning"
			}
		}
	}
	return ""
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

func detectFirstClassDocketTag(results []assertions.Result, toolTrace []tut.TraceEvent) string {
	for _, result := range results {
		switch result.AssertionType {
		case "tut_exit_nonzero":
			return docket.TagTUTExitNonzero
		}
		msg := strings.ToLower(strings.TrimSpace(result.Message))
		if msg == "" {
			continue
		}
		if strings.Contains(msg, "fixture miss") {
			return docket.TagFixtureMiss
		}
		if strings.Contains(msg, "fixture trust failure") ||
			strings.Contains(msg, "replay lockfile") ||
			(strings.Contains(msg, "fixture") && strings.Contains(msg, "signature")) {
			return docket.TagFixtureIntegrity
		}
	}

	for _, event := range toolTrace {
		if event.EventType != "tool_error" {
			continue
		}
		msg := strings.ToLower(strings.TrimSpace(event.Error))
		if msg == "" {
			continue
		}
		if strings.Contains(msg, "fixture miss") {
			return docket.TagFixtureMiss
		}
		if strings.Contains(msg, "fixture trust failure") ||
			strings.Contains(msg, "replay lockfile") ||
			(strings.Contains(msg, "fixture") && strings.Contains(msg, "signature")) {
			return docket.TagFixtureIntegrity
		}
	}
	return ""
}
