// Package runner orchestrates Gauntlet scenario execution.
// It loads scenarios, assembles world state, runs the TUT, evaluates
// assertions, and produces output artifacts.
package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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

	// In dry-run mode or when no adapter is set, evaluate with fixture data
	// For the example agent, we simulate the execution by reading fixture responses
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


func (r *Runner) adapterCapabilityDiagnostics(handle tut.Handle, traces []tut.TraceEvent) []assertions.Result {
	if r.Adapter == nil {
		return nil
	}
	if r.Adapter.Level() == tut.LevelMinimal {
		return nil
	}

	capabilities := tut.ExtractSDKCapabilities(traces)
	if provider, ok := handle.(tut.CapabilityProvider); ok {
		if reported := provider.Capabilities(); reported != nil {
			capabilities = reported
		}
	}

	if capabilities == nil {
		return []assertions.Result{{
			AssertionType: "adapter_capabilities",
			Passed:        false,
			Soft:          true,
			Message:       "SDK capability negotiation unavailable; ensure gauntlet.connect() is called and SDK supports capability protocol v1",
		}}
	}

	var diagnostics []assertions.Result
	if capabilities.ProtocolVersion != tut.CapabilityProtocolV1 {
		diagnostics = append(diagnostics, assertions.Result{
			AssertionType: "adapter_capabilities",
			Passed:        false,
			Soft:          true,
			Message:       fmt.Sprintf("unsupported capability protocol version %d (expected %d)", capabilities.ProtocolVersion, tut.CapabilityProtocolV1),
		})
	}

	if len(capabilities.Adapters) == 0 {
		diagnostics = append(diagnostics, assertions.Result{
			AssertionType: "adapter_capabilities",
			Passed:        false,
			Soft:          true,
			Message:       "capability negotiation returned no adapter feature data",
		})
		return diagnostics
	}

	names := make([]string, 0, len(capabilities.Adapters))
	for name := range capabilities.Adapters {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		cap := capabilities.Adapters[name]
		if cap.Enabled && !cap.Patched {
			reason := strings.TrimSpace(cap.Reason)
			if reason == "" {
				reason = "unknown_reason"
			}
			diagnostics = append(diagnostics, assertions.Result{
				AssertionType: "adapter_capabilities",
				Passed:        false,
				Soft:          true,
				Message:       fmt.Sprintf("adapter %s missing instrumentation: %s", name, reason),
			})
		}
	}

	return diagnostics
}

func (r *Runner) environmentFreezeDiagnostics(handle tut.Handle, traces []tut.TraceEvent) []assertions.Result {
	if r.Adapter == nil || handle == nil {
		return nil
	}
	if r.Adapter.Level() == tut.LevelMinimal {
		return nil
	}

	report := tut.ExtractDeterminismEnvReport(traces)
	capabilities := tut.ExtractSDKCapabilities(traces)
	if provider, ok := handle.(tut.CapabilityProvider); ok {
		if reported := provider.Capabilities(); reported != nil {
			capabilities = reported
		}
	}

	sdkName := "unknown"
	if capabilities != nil && strings.TrimSpace(capabilities.SDK) != "" {
		sdkName = strings.TrimSpace(capabilities.SDK)
	}

	if report == nil {
		if sdkName != "gauntlet-python" {
			return []assertions.Result{{
				AssertionType: "nondeterminism.env",
				Passed:        false,
				Soft:          true,
				Message:       fmt.Sprintf("environment freeze verification unavailable for sdk %q; runtime verification currently implemented for gauntlet-python", sdkName),
			}}
		}
		return []assertions.Result{{
			AssertionType: "nondeterminism.env",
			Passed:        false,
			Soft:          true,
			Message:       "gauntlet-python runtime did not emit determinism_env verification; ensure gauntlet.connect() is called before agent execution",
		}}
	}

	diagnostics := make([]assertions.Result, 0, 4)
	expectedFreeze := strings.TrimSpace(r.Harness.FreezeTime.UTC().Format(time.RFC3339))
	if strings.TrimSpace(report.RequestedFreezeTime) != "" && !sameRFC3339Instant(report.RequestedFreezeTime, expectedFreeze) {
		diagnostics = append(diagnostics, assertions.Result{
			AssertionType: "nondeterminism.env",
			Passed:        false,
			Soft:          true,
			Message:       fmt.Sprintf("freeze time mismatch: expected %s, runtime requested %s", expectedFreeze, strings.TrimSpace(report.RequestedFreezeTime)),
		})
	}
	if !report.TimePatched {
		diagnostics = append(diagnostics, assertions.Result{
			AssertionType: "nondeterminism.env",
			Passed:        false,
			Soft:          true,
			Message:       "runtime reported time patch not applied",
		})
	}

	expectedTimezone := strings.TrimSpace(r.Harness.Timezone)
	if expectedTimezone != "" && (!report.TimezoneApplied || !timezoneEquivalent(report.EffectiveTimezone, expectedTimezone)) {
		diagnostics = append(diagnostics, assertions.Result{
			AssertionType: "nondeterminism.env",
			Passed:        false,
			Soft:          true,
			Message:       fmt.Sprintf("timezone verification failed: expected %q, effective %q (applied=%t)", expectedTimezone, strings.TrimSpace(report.EffectiveTimezone), report.TimezoneApplied),
		})
	}

	expectedLocale := strings.TrimSpace(r.Harness.Locale)
	if expectedLocale != "" && (!report.LocaleApplied || !localeEquivalent(report.EffectiveLocale, expectedLocale)) {
		diagnostics = append(diagnostics, assertions.Result{
			AssertionType: "nondeterminism.env",
			Passed:        false,
			Soft:          true,
			Message:       fmt.Sprintf("locale verification failed: expected %q, effective %q (applied=%t)", expectedLocale, strings.TrimSpace(report.EffectiveLocale), report.LocaleApplied),
		})
	}

	return diagnostics
}

func sameRFC3339Instant(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" || b == "" {
		return a == b
	}
	at, aErr := time.Parse(time.RFC3339, a)
	bt, bErr := time.Parse(time.RFC3339, b)
	if aErr != nil || bErr != nil {
		return strings.EqualFold(a, b)
	}
	return at.UTC().Equal(bt.UTC())
}

func timezoneEquivalent(actual, expected string) bool {
	actualNorm := strings.ToUpper(strings.TrimSpace(actual))
	expectedNorm := strings.ToUpper(strings.TrimSpace(expected))
	if actualNorm == "" || expectedNorm == "" {
		return actualNorm == expectedNorm
	}
	if actualNorm == expectedNorm {
		return true
	}
	return strings.Contains(actualNorm, expectedNorm)
}

func localeEquivalent(actual, expected string) bool {
	actualNorm := strings.ToLower(strings.TrimSpace(actual))
	expectedNorm := strings.ToLower(strings.TrimSpace(expected))
	if actualNorm == "" || expectedNorm == "" {
		return actualNorm == expectedNorm
	}
	if actualNorm == expectedNorm {
		return true
	}
	return strings.Contains(actualNorm, expectedNorm)
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
	out, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

