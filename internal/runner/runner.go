// Package runner orchestrates Gauntlet scenario execution.
// It loads scenarios, assembles world state, runs the TUT, evaluates
// assertions, and produces output artifacts.
package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/gauntlet-dev/gauntlet/internal/assertions"
	"github.com/gauntlet-dev/gauntlet/internal/baseline"
	"github.com/gauntlet-dev/gauntlet/internal/determinism"
	"github.com/gauntlet-dev/gauntlet/internal/docket"
	"github.com/gauntlet-dev/gauntlet/internal/output"
	"github.com/gauntlet-dev/gauntlet/internal/scenario"
	"github.com/gauntlet-dev/gauntlet/internal/tut"
	"github.com/gauntlet-dev/gauntlet/internal/world"
)

// Config holds runner configuration from gauntlet.yml and CLI flags.
type Config struct {
	Suite            string
	ConfigPath       string
	Mode             string // pr_ci, nightly, local
	OutputDir        string
	EvalsDir         string
	SuiteDir         string
	ToolsDir         string
	DBDir            string
	BaselineDir      string
	FixturesDir      string
	TUTConfig        tut.Config
	DryRun           bool
	BudgetMs         int64
	ScenarioBudgetMs int64
	ScenarioFilter   string
	HardGates        map[string]bool
	SoftSignals      map[string]bool
}

// Runner is the main Gauntlet test runner.
type Runner struct {
	Config  Config
	Adapter tut.Adapter
	Harness *determinism.Harness
}

type scenarioExecution struct {
	Result    output.ScenarioResult
	Input     scenario.Input
	WorldSpec scenario.WorldSpec
	ToolTrace []tut.TraceEvent
	PROutput  *tut.AgentOutput
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
	requiresBlockedEgress := modeRequiresBlockedEgress(r.Config.Mode)
	egressStatus := EgressUnknown
	if requiresBlockedEgress {
		var err error
		egressStatus, err = r.runEgressSelfTest()
		if err != nil {
			return nil, err
		}
	}

	// Determine paths
	evalsDir := r.Config.EvalsDir
	if evalsDir == "" {
		evalsDir = "evals"
	}
	suiteDir := r.Config.SuiteDir
	if suiteDir == "" {
		suiteDir = filepath.Join(evalsDir, r.Config.Suite)
	}
	toolsDir := r.Config.ToolsDir
	if toolsDir == "" {
		toolsDir = filepath.Join(evalsDir, "world", "tools")
	}
	dbDir := r.Config.DBDir
	if dbDir == "" {
		dbDir = filepath.Join(evalsDir, "world", "databases")
	}
	baselineDir := r.Config.BaselineDir
	if baselineDir == "" {
		baselineDir = filepath.Join(evalsDir, "baselines")
	}

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
		exec := r.runScenario(ctx, s, worldState, baselineDir, requiresBlockedEgress, effectiveScenarioBudgetMs)
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

	_ = output.PopulateHistoryMetadata(result, outputDir)

	if err := output.WriteResults(outputDir, result); err != nil {
		return result, fmt.Errorf("failed to write results.json: %w", err)
	}
	if err := output.WriteSummary(outputDir, result); err != nil {
		return result, fmt.Errorf("failed to write summary.md: %w", err)
	}

	// Write artifact bundles for failures
	for _, exec := range executions {
		sr := exec.Result
		if sr.Status == "failed" || sr.Status == "error" {
			_ = output.WriteArtifactBundle(outputDir, sr.Name, sr, exec.Input, exec.WorldSpec, exec.ToolTrace, nil, exec.PROutput)
		}
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
	bl, _ := baseline.Load(baselineDir, r.Config.Suite, s.Name)

	// Build assertion context
	assertCtx := assertions.Context{
		ScenarioName: s.Name,
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
	results = enforceAssertionMode(results, r.Config.HardGates, r.Config.SoftSignals)

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

func enforceAssertionMode(results []assertions.Result, hardGates, softSignals map[string]bool) []assertions.Result {
	if len(hardGates) == 0 && len(softSignals) == 0 {
		return results
	}
	for i := range results {
		name := strings.TrimSpace(results[i].AssertionType)
		if name == "" {
			continue
		}
		if softSignals[name] {
			results[i].Soft = true
			continue
		}
		if hardGates[name] {
			results[i].Soft = false
		}
	}
	return results
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

func (r *Runner) buildTUTConfig(requiresBlockedEgress bool) tut.Config {
	cfg := r.Config.TUTConfig
	cfg.Env = cloneStringMap(cfg.Env)
	if r.Config.Mode == "fork_pr" {
		cfg.RestrictHostEnv = true
		cfg.Env = stripSensitiveEnv(cfg.Env)
	}
	for _, kv := range r.Harness.Env() {
		if k, v, ok := splitEnvVar(kv); ok {
			cfg.Env[k] = v
		}
	}
	cfg.BlockNetworkEgress = requiresBlockedEgress
	return cfg
}

func stripSensitiveEnv(in map[string]string) map[string]string {
	if len(in) == 0 {
		return in
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		if isSensitiveEnvKey(k) {
			continue
		}
		out[k] = v
	}
	return out
}

func isSensitiveEnvKey(key string) bool {
	k := strings.ToUpper(strings.TrimSpace(key))
	if k == "" {
		return false
	}
	known := map[string]bool{
		"OPENAI_API_KEY":                 true,
		"ANTHROPIC_API_KEY":              true,
		"GOOGLE_API_KEY":                 true,
		"GOOGLE_APPLICATION_CREDENTIALS": true,
		"AWS_ACCESS_KEY_ID":              true,
		"AWS_SECRET_ACCESS_KEY":          true,
		"AWS_SESSION_TOKEN":              true,
		"COHERE_API_KEY":                 true,
	}
	if known[k] {
		return true
	}
	if strings.Contains(k, "API_KEY") ||
		strings.Contains(k, "SECRET") ||
		strings.Contains(k, "TOKEN") ||
		strings.Contains(k, "PASSWORD") {
		return true
	}
	return false
}

func cloneStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func splitEnvVar(kv string) (string, string, bool) {
	parts := strings.SplitN(kv, "=", 2)
	if len(parts) != 2 || parts[0] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func prepareScenarioDatabases(ws *world.State, s *scenario.Scenario) (map[string]string, func(), error) {
	if len(s.World.Databases) == 0 {
		return map[string]string{}, func() {}, nil
	}

	runDir, err := os.MkdirTemp("", "gauntlet-db-*")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create DB temp dir: %w", err)
	}
	cleanup := func() {
		_ = os.RemoveAll(runDir)
	}

	env := make(map[string]string)
	for dbName, spec := range s.World.Databases {
		dbDef, ok := ws.Databases[dbName]
		if !ok {
			cleanup()
			return nil, nil, fmt.Errorf("database '%s' referenced by scenario '%s' not found in world definitions", dbName, s.Name)
		}
		dbPath, err := world.SeedDB(dbDef, spec.SeedSets, runDir)
		if err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("failed to seed database '%s' for scenario '%s': %w", dbName, s.Name, err)
		}
		envName := "GAUNTLET_DB_" + toEnvToken(dbName)
		env[envName] = sqliteURI(dbPath)
	}

	return env, cleanup, nil
}

var nonEnvTokenChars = regexp.MustCompile(`[^A-Za-z0-9]+`)

func toEnvToken(name string) string {
	normalized := nonEnvTokenChars.ReplaceAllString(strings.ToUpper(name), "_")
	normalized = strings.Trim(normalized, "_")
	if normalized == "" {
		return "DB"
	}
	return normalized
}

func sqliteURI(path string) string {
	p := filepath.ToSlash(path)
	if strings.HasPrefix(p, "/") {
		return "sqlite:////" + strings.TrimPrefix(p, "/")
	}
	return "sqlite:///" + p
}

func getCommit() string {
	out, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

func modeRequiresBlockedEgress(mode string) bool {
	return mode == "pr_ci" || mode == "fork_pr"
}

// mustMarshal is a helper for JSON marshaling that panics on error.
func mustMarshal(v interface{}) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}
