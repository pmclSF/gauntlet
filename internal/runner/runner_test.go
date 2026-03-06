package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pmclSF/gauntlet/internal/docket"
	"github.com/pmclSF/gauntlet/internal/scenario"
	"github.com/pmclSF/gauntlet/internal/tut"
)

// --- BudgetEnforcer tests ---

func TestNewBudgetEnforcer(t *testing.T) {
	be := NewBudgetEnforcer(5000)
	if be.BudgetMs != 5000 {
		t.Errorf("BudgetMs: got %d, want 5000", be.BudgetMs)
	}
	if be.StartTime.IsZero() {
		t.Error("StartTime should not be zero")
	}
}

func TestBudgetEnforcerNotExceededInitially(t *testing.T) {
	be := NewBudgetEnforcer(60000) // 60 seconds
	if be.Exceeded() {
		t.Error("Exceeded(): should be false immediately after creation with large budget")
	}
}

func TestBudgetEnforcerExceededAfterDeadline(t *testing.T) {
	be := &BudgetEnforcer{
		BudgetMs:  100,
		StartTime: time.Now().Add(-200 * time.Millisecond), // started 200ms ago
	}
	if !be.Exceeded() {
		t.Error("Exceeded(): should be true after deadline has passed")
	}
}

func TestBudgetEnforcerRemainingMsDecreases(t *testing.T) {
	be := NewBudgetEnforcer(60000)
	r1 := be.RemainingMs()

	// Wait a small amount
	time.Sleep(10 * time.Millisecond)

	r2 := be.RemainingMs()
	if r2 >= r1 {
		t.Errorf("RemainingMs should decrease over time: first=%d, second=%d", r1, r2)
	}
}

func TestBudgetEnforcerRemainingMsNonNegative(t *testing.T) {
	be := &BudgetEnforcer{
		BudgetMs:  100,
		StartTime: time.Now().Add(-1 * time.Second), // started 1s ago, budget is 100ms
	}
	remaining := be.RemainingMs()
	if remaining < 0 {
		t.Errorf("RemainingMs: got %d, should not be negative", remaining)
	}
	if remaining != 0 {
		t.Errorf("RemainingMs: got %d, expected 0 when budget exceeded", remaining)
	}
}

func TestBudgetEnforcerContextWithBudget(t *testing.T) {
	be := NewBudgetEnforcer(5000)
	ctx, cancel := be.ContextWithBudget(t.Context())
	defer cancel()

	if ctx == nil {
		t.Error("ContextWithBudget returned nil context")
	}
}

// --- EgressStatus tests ---

func TestEgressStatusConstants(t *testing.T) {
	if EgressBlocked != 0 {
		t.Errorf("EgressBlocked: got %d, want 0", EgressBlocked)
	}
	if EgressOpen != 1 {
		t.Errorf("EgressOpen: got %d, want 1", EgressOpen)
	}
	if EgressUnknown != 2 {
		t.Errorf("EgressUnknown: got %d, want 2", EgressUnknown)
	}
}

// --- InCIContext tests ---

func TestInCIContextWithCI(t *testing.T) {
	original := os.Getenv("CI")
	originalGHA := os.Getenv("GITHUB_ACTIONS")
	defer func() {
		os.Setenv("CI", original)
		os.Setenv("GITHUB_ACTIONS", originalGHA)
	}()

	os.Setenv("CI", "true")
	os.Setenv("GITHUB_ACTIONS", "")
	if !InCIContext() {
		t.Error("InCIContext(): should be true when CI=true")
	}
}

func TestInCIContextWithGitHubActions(t *testing.T) {
	original := os.Getenv("CI")
	originalGHA := os.Getenv("GITHUB_ACTIONS")
	defer func() {
		os.Setenv("CI", original)
		os.Setenv("GITHUB_ACTIONS", originalGHA)
	}()

	os.Setenv("CI", "")
	os.Setenv("GITHUB_ACTIONS", "true")
	if !InCIContext() {
		t.Error("InCIContext(): should be true when GITHUB_ACTIONS=true")
	}
}

func TestInCIContextNotCI(t *testing.T) {
	original := os.Getenv("CI")
	originalGHA := os.Getenv("GITHUB_ACTIONS")
	defer func() {
		os.Setenv("CI", original)
		os.Setenv("GITHUB_ACTIONS", originalGHA)
	}()

	os.Setenv("CI", "")
	os.Setenv("GITHUB_ACTIONS", "")
	if InCIContext() {
		t.Error("InCIContext(): should be false when no CI env vars are set")
	}
}

// --- NewRunner tests ---

func TestNewRunner(t *testing.T) {
	cfg := Config{
		Suite:            "smoke",
		ConfigPath:       "/tmp/gauntlet.yml",
		Mode:             "local",
		OutputDir:        "/tmp/output",
		EvalsDir:         "/tmp/evals",
		DryRun:           true,
		BudgetMs:         30000,
		ScenarioBudgetMs: 15000,
	}

	r := NewRunner(cfg)

	if r.Config.Suite != "smoke" {
		t.Errorf("Config.Suite: got %q, want %q", r.Config.Suite, "smoke")
	}
	if r.Config.Mode != "local" {
		t.Errorf("Config.Mode: got %q, want %q", r.Config.Mode, "local")
	}
	if r.Config.BudgetMs != 30000 {
		t.Errorf("Config.BudgetMs: got %d, want 30000", r.Config.BudgetMs)
	}
	if r.Config.ScenarioBudgetMs != 15000 {
		t.Errorf("Config.ScenarioBudgetMs: got %d, want 15000", r.Config.ScenarioBudgetMs)
	}
	if !r.Config.DryRun {
		t.Error("Config.DryRun: expected true")
	}
	if r.Harness == nil {
		t.Error("Harness: expected non-nil")
	}
}

func TestNewRunnerConfigFields(t *testing.T) {
	cfg := Config{
		Suite:          "nightly",
		ScenarioFilter: "test-scenario",
	}

	r := NewRunner(cfg)

	if r.Config.ScenarioFilter != "test-scenario" {
		t.Errorf("Config.ScenarioFilter: got %q, want %q", r.Config.ScenarioFilter, "test-scenario")
	}
	if r.Adapter != nil {
		t.Error("Adapter: expected nil initially")
	}
}

// --- Config tests ---

func TestConfigDefaults(t *testing.T) {
	cfg := Config{}
	if cfg.Suite != "" {
		t.Errorf("default Suite should be empty, got %q", cfg.Suite)
	}
	if cfg.BudgetMs != 0 {
		t.Errorf("default BudgetMs should be 0, got %d", cfg.BudgetMs)
	}
	if cfg.ScenarioBudgetMs != 0 {
		t.Errorf("default ScenarioBudgetMs should be 0, got %d", cfg.ScenarioBudgetMs)
	}
	if cfg.DryRun {
		t.Error("default DryRun should be false")
	}
}

func TestModeRequiresBlockedEgress(t *testing.T) {
	tests := []struct {
		mode string
		want bool
	}{
		{mode: "pr_ci", want: true},
		{mode: "fork_pr", want: true},
		{mode: "nightly", want: false},
		{mode: "local", want: false},
		{mode: "", want: false},
	}

	for _, tt := range tests {
		got := modeRequiresBlockedEgress(tt.mode)
		if got != tt.want {
			t.Errorf("modeRequiresBlockedEgress(%q) = %v, want %v", tt.mode, got, tt.want)
		}
	}
}

func TestRunnerRunFailsWhenPRModeEgressOpen(t *testing.T) {
	original := checkEgressBlockedFn
	checkEgressBlockedFn = func() EgressStatus { return EgressOpen }
	defer func() { checkEgressBlockedFn = original }()

	r := NewRunner(Config{
		Suite: "smoke",
		Mode:  "pr_ci",
	})

	_, err := r.Run(t.Context())
	if err == nil {
		t.Fatal("expected egress enforcement error, got nil")
	}
	if !strings.Contains(err.Error(), "requires blocked network egress") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunnerRun_UsesAdapterAndStopsHandle(t *testing.T) {
	evalsDir := writeSingleScenarioSuite(t, "smoke", "scenario: happy_path\ninput:\n  messages:\n    - role: user\n      content: hello\n")

	handle := &fakeHandle{
		output: &tut.AgentOutput{
			Raw:    []byte(`{"result":"ok"}`),
			Parsed: map[string]interface{}{"result": "ok"},
		},
	}
	adapter := &fakeAdapter{handle: handle}

	r := NewRunner(Config{
		Suite:     "smoke",
		EvalsDir:  evalsDir,
		OutputDir: filepath.Join(t.TempDir(), "runs"),
		Mode:      "local",
		BudgetMs:  10000,
		TUTConfig: tut.Config{
			Command: "agent",
			Env:     map[string]string{"EXISTING": "1"},
		},
	})
	r.Adapter = adapter

	result, err := r.Run(t.Context())
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if adapter.startCalls != 1 {
		t.Fatalf("adapter start calls = %d, want 1", adapter.startCalls)
	}
	if handle.runCalls != 1 {
		t.Fatalf("handle run calls = %d, want 1", handle.runCalls)
	}
	if handle.stopCalls != 1 {
		t.Fatalf("handle stop calls = %d, want 1", handle.stopCalls)
	}
	if result.Summary.Passed != 1 {
		t.Fatalf("passed = %d, want 1", result.Summary.Passed)
	}
	if got := adapter.lastConfig.Env["GAUNTLET_ENABLED"]; got != "1" {
		t.Fatalf("GAUNTLET_ENABLED env = %q, want %q", got, "1")
	}
	if got := adapter.lastConfig.Env["EXISTING"]; got != "1" {
		t.Fatalf("existing env = %q, want %q", got, "1")
	}
}

func TestRunnerRun_NonZeroTUTExitIsErrorAndRecordedInArtifact(t *testing.T) {
	evalsDir := writeSingleScenarioSuite(t, "smoke", "scenario: crashes\ninput:\n  messages:\n    - role: user\n      content: hello\n")
	outputDir := filepath.Join(t.TempDir(), "runs")

	adapter := &fakeAdapter{
		handle: &fakeHandle{
			output: &tut.AgentOutput{
				Raw:      []byte(`{"result":"partial"}`),
				Parsed:   map[string]interface{}{"result": "partial"},
				ExitCode: 1,
			},
		},
	}

	r := NewRunner(Config{
		Suite:     "smoke",
		EvalsDir:  evalsDir,
		OutputDir: outputDir,
		Mode:      "local",
		BudgetMs:  10000,
		TUTConfig: tut.Config{Command: "agent"},
	})
	r.Adapter = adapter

	result, err := r.Run(t.Context())
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(result.Scenarios) != 1 {
		t.Fatalf("scenario count = %d, want 1", len(result.Scenarios))
	}
	scenarioResult := result.Scenarios[0]
	if scenarioResult.Status == "passed" {
		t.Fatalf("status = %q, want non-passed", scenarioResult.Status)
	}
	if scenarioResult.Status != "error" {
		t.Fatalf("status = %q, want error", scenarioResult.Status)
	}
	found := false
	for _, a := range scenarioResult.Assertions {
		if a.AssertionType == "tut_exit_nonzero" {
			found = true
			if a.Passed {
				t.Fatal("tut_exit_nonzero assertion should fail")
			}
			break
		}
	}
	if !found {
		t.Fatalf("expected tut_exit_nonzero assertion, got %+v", scenarioResult.Assertions)
	}

	artifactPath := filepath.Join(outputDir, "crashes", "assertions.json")
	artifactData, err := os.ReadFile(artifactPath)
	if err != nil {
		t.Fatalf("failed to read artifact assertions: %v", err)
	}
	if !strings.Contains(string(artifactData), "tut_exit_nonzero") {
		t.Fatalf("artifact missing tut_exit_nonzero assertion: %s", string(artifactData))
	}
}

func TestDeterminismStability(t *testing.T) {
	evalsDir := writeSingleScenarioSuite(t, "smoke", `scenario: determinism_stability
chaos: true
input:
  messages:
    - role: user
      content: hello
world:
  tools:
    zeta_tool: timeout
    alpha_tool: server_error
assertions:
  - type: tool_sequence
    required: [required_tool]
`)
	toolsDir := filepath.Join(evalsDir, "world", "tools")
	if err := os.MkdirAll(toolsDir, 0o755); err != nil {
		t.Fatalf("mkdir tools dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(toolsDir, "alpha_tool.yaml"), []byte(`tool: alpha_tool
states:
  server_error:
    status_code: 500
`), 0o644); err != nil {
		t.Fatalf("write alpha tool def: %v", err)
	}
	if err := os.WriteFile(filepath.Join(toolsDir, "zeta_tool.yaml"), []byte(`tool: zeta_tool
states:
  timeout:
    delay_ms: 5
`), 0o644); err != nil {
		t.Fatalf("write zeta tool def: %v", err)
	}
	baseOutputDir := t.TempDir()

	var expectedHash string
	for i := 0; i < 50; i++ {
		outputDir := filepath.Join(baseOutputDir, fmt.Sprintf("run-%02d", i))
		r := NewRunner(Config{
			Suite:     "smoke",
			EvalsDir:  evalsDir,
			OutputDir: outputDir,
			Mode:      "local",
			DryRun:    true,
			BudgetMs:  10000,
		})

		result, err := r.Run(t.Context())
		if err != nil {
			t.Fatalf("iteration %d: Run returned error: %v", i, err)
		}
		if result.Summary.Failed != 1 {
			t.Fatalf("iteration %d: failed count = %d, want 1", i, result.Summary.Failed)
		}

		culpritPath := filepath.Join(outputDir, "determinism_stability", "culprit.json")
		culpritData, err := os.ReadFile(culpritPath)
		if err != nil {
			t.Fatalf("iteration %d: read culprit artifact: %v", i, err)
		}
		sum := sha256.Sum256(culpritData)
		gotHash := hex.EncodeToString(sum[:])
		if i == 0 {
			expectedHash = gotHash
			continue
		}
		if gotHash != expectedHash {
			t.Fatalf("iteration %d: culprit artifact hash = %s, want %s", i, gotHash, expectedHash)
		}
	}
}

func TestRunnerRun_FixtureMissGetsFirstClassDocketTag(t *testing.T) {
	evalsDir := writeSingleScenarioSuite(t, "smoke", `scenario: fixture_miss_case
input:
  messages:
    - role: user
      content: hello
assertions:
  - type: tool_sequence
    required: [order_lookup]
`)
	outputDir := filepath.Join(t.TempDir(), "runs")
	adapter := &fakeAdapter{
		handle: &fakeHandle{
			output: &tut.AgentOutput{
				Raw:    []byte(`{"response":"fallback"}`),
				Parsed: map[string]interface{}{"response": "fallback"},
			},
			traces: []tut.TraceEvent{
				{
					EventType: "tool_error",
					ToolName:  "order_lookup",
					Error:     "FIXTURE MISS for tool:order_lookup",
				},
			},
		},
	}

	r := NewRunner(Config{
		Suite:     "smoke",
		EvalsDir:  evalsDir,
		OutputDir: outputDir,
		Mode:      "local",
		BudgetMs:  10000,
		TUTConfig: tut.Config{Command: "agent"},
	})
	r.Adapter = adapter

	result, err := r.Run(t.Context())
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(result.Scenarios) != 1 {
		t.Fatalf("scenario count = %d, want 1", len(result.Scenarios))
	}
	sr := result.Scenarios[0]
	if sr.PrimaryTag != docket.TagFixtureMiss {
		t.Fatalf("primary tag = %q, want %q", sr.PrimaryTag, docket.TagFixtureMiss)
	}
	if len(sr.DocketTags) != 1 || sr.DocketTags[0] != docket.TagFixtureMiss {
		t.Fatalf("docket tags = %v, want [%s]", sr.DocketTags, docket.TagFixtureMiss)
	}
	if sr.Culprit != nil {
		t.Fatalf("expected culprit heuristics to be skipped for first-class tag, got %+v", sr.Culprit)
	}
}

func TestRunnerRun_InvalidWorldToolStateFailsBeforeExecution(t *testing.T) {
	evalsDir := t.TempDir()
	suiteDir := filepath.Join(evalsDir, "smoke")
	if err := os.MkdirAll(suiteDir, 0o755); err != nil {
		t.Fatalf("mkdir suite dir: %v", err)
	}
	scenarioYAML := `scenario: invalid_world_ref
input:
  messages:
    - role: user
      content: hello
world:
  tools:
    order_lookup: missing_state
`
	if err := os.WriteFile(filepath.Join(suiteDir, "scenario.yaml"), []byte(scenarioYAML), 0o644); err != nil {
		t.Fatalf("write scenario: %v", err)
	}

	toolsDir := filepath.Join(evalsDir, "world", "tools")
	if err := os.MkdirAll(toolsDir, 0o755); err != nil {
		t.Fatalf("mkdir tools dir: %v", err)
	}
	toolDefYAML := `tool: order_lookup
states:
  nominal:
    response:
      status: ok
  timeout:
    delay_ms: 100
`
	if err := os.WriteFile(filepath.Join(toolsDir, "order_lookup.yaml"), []byte(toolDefYAML), 0o644); err != nil {
		t.Fatalf("write tool def: %v", err)
	}

	r := NewRunner(Config{
		Suite:     "smoke",
		EvalsDir:  evalsDir,
		OutputDir: filepath.Join(t.TempDir(), "runs"),
		Mode:      "local",
		DryRun:    true,
		BudgetMs:  10000,
	})
	_, err := r.Run(t.Context())
	if err == nil {
		t.Fatal("expected world tool state validation error, got nil")
	}
	errMsg := err.Error()
	if !strings.Contains(errMsg, `scenario "invalid_world_ref"`) {
		t.Fatalf("error missing scenario name: %v", err)
	}
	if !strings.Contains(errMsg, `tool "order_lookup" has no state "missing_state"`) {
		t.Fatalf("error missing tool/state details: %v", err)
	}
	if !strings.Contains(errMsg, "available states: nominal, timeout") {
		t.Fatalf("error missing available states: %v", err)
	}
}

func TestRunnerRun_FailFastStopsAfterFirstFailure(t *testing.T) {
	evalsDir := t.TempDir()
	suiteDir := filepath.Join(evalsDir, "smoke")
	if err := os.MkdirAll(suiteDir, 0o755); err != nil {
		t.Fatalf("mkdir suite dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(suiteDir, "a_fail.yaml"), []byte(`scenario: fail_first
input:
  messages:
    - role: user
      content: hello
assertions:
  - type: tool_sequence
    required: [order_lookup]
`), 0o644); err != nil {
		t.Fatalf("write fail scenario: %v", err)
	}
	if err := os.WriteFile(filepath.Join(suiteDir, "b_pass.yaml"), []byte(`scenario: pass_second
input:
  messages:
    - role: user
      content: hello
assertions: []
`), 0o644); err != nil {
		t.Fatalf("write pass scenario: %v", err)
	}

	r := NewRunner(Config{
		Suite:     "smoke",
		EvalsDir:  evalsDir,
		OutputDir: filepath.Join(t.TempDir(), "runs"),
		Mode:      "local",
		DryRun:    true,
		FailFast:  true,
		BudgetMs:  10000,
	})
	result, err := r.Run(t.Context())
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(result.Scenarios) != 1 {
		t.Fatalf("scenario count = %d, want 1 with fail_fast", len(result.Scenarios))
	}
	if result.Scenarios[0].Name != "fail_first" {
		t.Fatalf("first scenario = %q, want fail_first", result.Scenarios[0].Name)
	}
	if result.Summary.Failed != 1 {
		t.Fatalf("failed summary = %d, want 1", result.Summary.Failed)
	}
}

func TestRunnerRun_PRModeSetsTUTEgressBlock(t *testing.T) {
	evalsDir := writeSingleScenarioSuite(t, "smoke", "scenario: ci_mode\ninput:\n  messages:\n    - role: user\n      content: hello\n")

	original := checkEgressBlockedFn
	checkEgressBlockedFn = func() EgressStatus { return EgressBlocked }
	defer func() { checkEgressBlockedFn = original }()

	adapter := &fakeAdapter{handle: &fakeHandle{}}

	r := NewRunner(Config{
		Suite:     "smoke",
		EvalsDir:  evalsDir,
		OutputDir: filepath.Join(t.TempDir(), "runs"),
		Mode:      "pr_ci",
		BudgetMs:  10000,
		TUTConfig: tut.Config{Command: "agent"},
	})
	r.Adapter = adapter

	_, err := r.Run(t.Context())
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !adapter.lastConfig.BlockNetworkEgress {
		t.Fatal("expected BlockNetworkEgress=true in pr_ci mode")
	}
}

func TestRunnerRun_AssertionPolicyHardGateOverridesSoftAssertion(t *testing.T) {
	evalsDir := writeSingleScenarioSuite(t, "smoke", `scenario: sensitive_output
input:
  messages:
    - role: user
      content: hello
assertions:
  - type: sensitive_leak
`)

	adapter := &fakeAdapter{
		handle: &fakeHandle{
			output: &tut.AgentOutput{
				Raw:    []byte("card 4111 1111 1111 1111"),
				Parsed: map[string]interface{}{"response": "card 4111 1111 1111 1111"},
			},
		},
	}

	r := NewRunner(Config{
		Suite:       "smoke",
		EvalsDir:    evalsDir,
		OutputDir:   filepath.Join(t.TempDir(), "runs"),
		Mode:        "local",
		BudgetMs:    10000,
		TUTConfig:   tut.Config{Command: "agent"},
		HardGates:   map[string]bool{"sensitive_leak": true},
		SoftSignals: nil,
	})
	r.Adapter = adapter

	result, err := r.Run(t.Context())
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Summary.Failed != 1 {
		t.Fatalf("failed summary = %d, want 1", result.Summary.Failed)
	}
	if got := result.Scenarios[0].Status; got != "failed" {
		t.Fatalf("scenario status = %q, want failed", got)
	}
	if got := result.Scenarios[0].Assertions[0].Soft; got {
		t.Fatalf("sensitive_leak should be forced hard gate by policy")
	}
}

func TestRunnerRun_AssertionPolicySoftSignalOverridesHardAssertion(t *testing.T) {
	evalsDir := writeSingleScenarioSuite(t, "smoke", `scenario: output_schema_softened
input:
  messages:
    - role: user
      content: hello
assertions:
  - type: output_schema
    schema:
      type: object
      required: ["must_exist"]
`)

	adapter := &fakeAdapter{
		handle: &fakeHandle{
			output: &tut.AgentOutput{
				Raw:    []byte(`{"response":"ok"}`),
				Parsed: map[string]interface{}{"response": "ok"},
			},
		},
	}

	r := NewRunner(Config{
		Suite:       "smoke",
		EvalsDir:    evalsDir,
		OutputDir:   filepath.Join(t.TempDir(), "runs"),
		Mode:        "local",
		BudgetMs:    10000,
		TUTConfig:   tut.Config{Command: "agent"},
		SoftSignals: map[string]bool{"output_schema": true},
	})
	r.Adapter = adapter

	result, err := r.Run(t.Context())
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Summary.Passed != 1 {
		t.Fatalf("passed summary = %d, want 1", result.Summary.Passed)
	}
	if got := result.Scenarios[0].Status; got != "passed" {
		t.Fatalf("scenario status = %q, want passed", got)
	}
	if got := result.Scenarios[0].Assertions[0].Soft; !got {
		t.Fatalf("output_schema should be forced soft signal by policy")
	}
	if got := result.Scenarios[0].Assertions[0].Passed; got {
		t.Fatalf("expected assertion itself to fail, but be non-blocking")
	}
}

func TestRunnerRun_AdapterExecutionErrorMarksScenarioError(t *testing.T) {
	evalsDir := writeSingleScenarioSuite(t, "smoke", "scenario: broken_agent\ninput:\n  messages:\n    - role: user\n      content: hello\n")

	adapter := &fakeAdapter{
		handle: &fakeHandle{
			runErr: errors.New("agent crashed"),
		},
	}

	r := NewRunner(Config{
		Suite:     "smoke",
		EvalsDir:  evalsDir,
		OutputDir: filepath.Join(t.TempDir(), "runs"),
		Mode:      "local",
		BudgetMs:  10000,
		TUTConfig: tut.Config{Command: "agent"},
	})
	r.Adapter = adapter

	result, err := r.Run(t.Context())
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Summary.Error != 1 {
		t.Fatalf("error summary = %d, want 1", result.Summary.Error)
	}
	if len(result.Scenarios) != 1 {
		t.Fatalf("scenario count = %d, want 1", len(result.Scenarios))
	}
	if got := result.Scenarios[0].Status; got != "error" {
		t.Fatalf("scenario status = %q, want %q", got, "error")
	}
	if got := result.Scenarios[0].FailureCategory; got != "infra_failure" {
		t.Fatalf("failure category = %q, want infra_failure", got)
	}
	if len(result.Scenarios[0].Assertions) == 0 {
		t.Fatal("expected tut_execution assertion on adapter error")
	}
	if got := result.Scenarios[0].Assertions[0].AssertionType; got != "tut_execution" {
		t.Fatalf("assertion type = %q, want %q", got, "tut_execution")
	}
}

func TestRunnerRun_ScenarioBudgetTimeoutMarksTimeoutCategory(t *testing.T) {
	evalsDir := writeSingleScenarioSuite(t, "smoke", "scenario: slow_agent\ninput:\n  messages:\n    - role: user\n      content: hello\n")
	adapter := &fakeAdapter{
		handle: &fakeHandle{
			runDelay: 200 * time.Millisecond,
		},
	}

	r := NewRunner(Config{
		Suite:            "smoke",
		EvalsDir:         evalsDir,
		OutputDir:        filepath.Join(t.TempDir(), "runs"),
		Mode:             "local",
		BudgetMs:         5000,
		ScenarioBudgetMs: 20,
		TUTConfig:        tut.Config{Command: "agent"},
	})
	r.Adapter = adapter

	result, err := r.Run(t.Context())
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Summary.Error != 1 {
		t.Fatalf("error summary = %d, want 1", result.Summary.Error)
	}
	sr := result.Scenarios[0]
	if sr.Status != "error" {
		t.Fatalf("status = %q, want error", sr.Status)
	}
	if sr.FailureCategory != "timeout" {
		t.Fatalf("failure category = %q, want timeout", sr.FailureCategory)
	}
	if sr.BudgetMs != 20 {
		t.Fatalf("scenario budget = %d, want 20", sr.BudgetMs)
	}
	if len(sr.Assertions) == 0 || sr.Assertions[0].AssertionType != "scenario_timeout" {
		t.Fatalf("expected scenario_timeout assertion, got %+v", sr.Assertions)
	}
}

func TestRunnerRun_PassedWithNondeterminismWarningCategory(t *testing.T) {
	evalsDir := writeSingleScenarioSuite(t, "smoke", "scenario: nondeterminism_warning\ninput:\n  messages:\n    - role: user\n      content: hello\n")
	adapter := &fakeAdapter{
		handle: &fakeHandle{
			output: &tut.AgentOutput{
				Raw:    []byte(`{"timestamp":"2024-01-01T00:00:00"}`),
				Parsed: map[string]interface{}{"timestamp": "2024-01-01T00:00:00"},
			},
		},
	}

	r := NewRunner(Config{
		Suite:     "smoke",
		EvalsDir:  evalsDir,
		OutputDir: filepath.Join(t.TempDir(), "runs"),
		Mode:      "local",
		BudgetMs:  10000,
		TUTConfig: tut.Config{Command: "agent"},
	})
	r.Adapter = adapter

	result, err := r.Run(t.Context())
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Summary.Passed != 1 {
		t.Fatalf("passed summary = %d, want 1", result.Summary.Passed)
	}
	sr := result.Scenarios[0]
	if sr.Status != "passed" {
		t.Fatalf("status = %q, want passed", sr.Status)
	}
	if sr.FailureCategory != "nondeterminism_warning" {
		t.Fatalf("failure category = %q, want nondeterminism_warning", sr.FailureCategory)
	}
	if len(sr.Assertions) == 0 || !strings.HasPrefix(sr.Assertions[0].AssertionType, "nondeterminism.") {
		t.Fatalf("expected nondeterminism assertion, got %+v", sr.Assertions)
	}
}

func TestRunnerRun_InjectsScenarioDatabaseEnv(t *testing.T) {
	evalsDir := t.TempDir()
	suiteDir := filepath.Join(evalsDir, "smoke")
	if err := os.MkdirAll(suiteDir, 0o755); err != nil {
		t.Fatalf("mkdir suite dir: %v", err)
	}
	scenarioYAML := `scenario: db_path
input:
  messages:
    - role: user
      content: hello
world:
  databases:
    orders_db:
      seed_sets: [default]
`
	if err := os.WriteFile(filepath.Join(suiteDir, "scenario.yaml"), []byte(scenarioYAML), 0o644); err != nil {
		t.Fatalf("write scenario: %v", err)
	}

	dbDir := filepath.Join(evalsDir, "world", "databases")
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		t.Fatalf("mkdir db dir: %v", err)
	}
	dbYAML := `database: orders_db
adapter: sqlite3
seed_sets:
  default:
    tables:
      orders:
        columns:
          id: TEXT
          status: TEXT
        rows:
          - id: ord-001
            status: confirmed
`
	if err := os.WriteFile(filepath.Join(dbDir, "orders_db.yaml"), []byte(dbYAML), 0o644); err != nil {
		t.Fatalf("write db world: %v", err)
	}

	adapter := &fakeAdapter{handle: &fakeHandle{}}
	r := NewRunner(Config{
		Suite:     "smoke",
		EvalsDir:  evalsDir,
		OutputDir: filepath.Join(t.TempDir(), "runs"),
		Mode:      "local",
		BudgetMs:  10000,
		TUTConfig: tut.Config{
			Command: "agent",
		},
	})
	r.Adapter = adapter

	_, err := r.Run(t.Context())
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if adapter.startCalls != 1 {
		t.Fatalf("adapter start calls = %d, want 1", adapter.startCalls)
	}
	dbURL := adapter.lastConfig.Env["GAUNTLET_DB_ORDERS_DB"]
	if dbURL == "" {
		t.Fatal("expected GAUNTLET_DB_ORDERS_DB env to be injected")
	}
	if !strings.HasPrefix(dbURL, "sqlite:////") {
		t.Fatalf("expected absolute sqlite URI, got %q", dbURL)
	}
}

func TestToEnvToken(t *testing.T) {
	if got := toEnvToken("orders_db"); got != "ORDERS_DB" {
		t.Fatalf("toEnvToken orders_db = %q", got)
	}
	if got := toEnvToken("orders-db.v2"); got != "ORDERS_DB_V2" {
		t.Fatalf("toEnvToken orders-db.v2 = %q", got)
	}
}

func TestSQLiteURIAbsolute(t *testing.T) {
	got := sqliteURI("/tmp/gauntlet-test.db")
	if got != "sqlite:////tmp/gauntlet-test.db" {
		t.Fatalf("sqliteURI absolute path = %q", got)
	}
}

func TestBuildTUTConfig_ForkPRRestrictsHostEnvAndStripsSecrets(t *testing.T) {
	r := NewRunner(Config{
		Mode: "fork_pr",
		TUTConfig: tut.Config{
			Env: map[string]string{
				"SAFE_VAR":       "ok",
				"OPENAI_API_KEY": "secret",
				"RANDOM_TOKEN":   "secret2",
			},
		},
	})

	cfg := r.buildTUTConfig(true)
	if !cfg.RestrictHostEnv {
		t.Fatal("expected RestrictHostEnv=true in fork_pr mode")
	}
	if _, ok := cfg.Env["OPENAI_API_KEY"]; ok {
		t.Fatal("OPENAI_API_KEY should be stripped in fork_pr mode")
	}
	if _, ok := cfg.Env["RANDOM_TOKEN"]; ok {
		t.Fatal("RANDOM_TOKEN should be stripped in fork_pr mode")
	}
	if got := cfg.Env["SAFE_VAR"]; got != "ok" {
		t.Fatalf("SAFE_VAR = %q, want ok", got)
	}
}

func TestBuildTUTConfig_PRCIRestrictsHostEnvAndStripsSecrets(t *testing.T) {
	r := NewRunner(Config{
		Mode: "pr_ci",
		TUTConfig: tut.Config{
			Env: map[string]string{
				"SAFE_VAR":       "ok",
				"OPENAI_API_KEY": "secret",
			},
		},
	})

	cfg := r.buildTUTConfig(true)
	if !cfg.RestrictHostEnv {
		t.Fatal("expected RestrictHostEnv=true in pr_ci mode")
	}
	if _, ok := cfg.Env["OPENAI_API_KEY"]; ok {
		t.Fatal("OPENAI_API_KEY should be stripped in pr_ci mode")
	}
	if got := cfg.Env["SAFE_VAR"]; got != "ok" {
		t.Fatalf("SAFE_VAR = %q, want ok", got)
	}
}

func TestBuildTUTConfig_LocalKeepsEnvAndHostInheritance(t *testing.T) {
	r := NewRunner(Config{
		Mode: "local",
		TUTConfig: tut.Config{
			Env: map[string]string{
				"OPENAI_API_KEY": "secret",
			},
		},
	})
	cfg := r.buildTUTConfig(false)
	if cfg.RestrictHostEnv {
		t.Fatal("expected RestrictHostEnv=false in local mode")
	}
	if got := cfg.Env["OPENAI_API_KEY"]; got != "secret" {
		t.Fatalf("OPENAI_API_KEY = %q, want secret", got)
	}
}

func TestAdapterCapabilityDiagnostics_WarnsWhenNegotiationMissing(t *testing.T) {
	r := NewRunner(Config{})
	r.Adapter = &fakeAdapter{level: tut.LevelGood}

	diagnostics := r.adapterCapabilityDiagnostics(&fakeHandle{}, nil)
	if len(diagnostics) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", len(diagnostics))
	}
	d := diagnostics[0]
	if d.AssertionType != "adapter_capabilities" {
		t.Fatalf("assertion type = %q", d.AssertionType)
	}
	if !d.Soft || d.Passed {
		t.Fatalf("expected soft failing diagnostic, got %+v", d)
	}
	if !strings.Contains(d.Message, "capability negotiation unavailable") {
		t.Fatalf("unexpected diagnostic message: %s", d.Message)
	}
}

func TestAdapterCapabilityDiagnostics_WarnsOnUnpatchedAdapter(t *testing.T) {
	r := NewRunner(Config{})
	r.Adapter = &fakeAdapter{level: tut.LevelGood}
	handle := &fakeHandle{
		capabilities: &tut.SDKCapabilities{
			ProtocolVersion: tut.CapabilityProtocolV1,
			SDK:             "gauntlet-python",
			Adapters: map[string]tut.SDKAdapterCapability{
				"openai":    {Enabled: true, Patched: false, Reason: "openai_not_installed"},
				"langchain": {Enabled: true, Patched: true},
			},
		},
	}

	diagnostics := r.adapterCapabilityDiagnostics(handle, nil)
	if len(diagnostics) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", len(diagnostics))
	}
	if !strings.Contains(diagnostics[0].Message, "adapter openai missing instrumentation") {
		t.Fatalf("unexpected message: %s", diagnostics[0].Message)
	}
}

func TestAdapterCapabilityDiagnostics_SkipsForMinimalIntegration(t *testing.T) {
	r := NewRunner(Config{})
	r.Adapter = &fakeAdapter{level: tut.LevelMinimal}

	diagnostics := r.adapterCapabilityDiagnostics(&fakeHandle{}, nil)
	if len(diagnostics) != 0 {
		t.Fatalf("expected no diagnostics, got %d", len(diagnostics))
	}
}

func TestRunnerRun_DeterminismEnvVerifiedNoWarning(t *testing.T) {
	evalsDir := writeSingleScenarioSuite(t, "smoke", "scenario: env_verified\ninput:\n  messages:\n    - role: user\n      content: hello\n")
	adapter := &fakeAdapter{
		level: tut.LevelGood,
		handle: &fakeHandle{
			output: &tut.AgentOutput{
				Raw:    []byte(`{"ok":true}`),
				Parsed: map[string]interface{}{"ok": true},
			},
			capabilities: &tut.SDKCapabilities{
				ProtocolVersion: tut.CapabilityProtocolV1,
				SDK:             "gauntlet-python",
				Adapters: map[string]tut.SDKAdapterCapability{
					"openai": {Enabled: true, Patched: true},
				},
			},
			traces: []tut.TraceEvent{
				{
					EventType: "determinism_env",
					DeterminismEnv: &tut.DeterminismEnvReport{
						Language:            "python",
						Runtime:             "python3.11",
						RequestedFreezeTime: "2025-01-15T10:00:00Z",
						TimePatched:         true,
						RequestedTimezone:   "UTC",
						EffectiveTimezone:   "UTC",
						TimezoneApplied:     true,
						RequestedLocale:     "en_US.UTF-8",
						EffectiveLocale:     "en_US.UTF-8",
						LocaleApplied:       true,
					},
				},
			},
		},
	}

	r := NewRunner(Config{
		Suite:     "smoke",
		EvalsDir:  evalsDir,
		OutputDir: filepath.Join(t.TempDir(), "runs"),
		Mode:      "local",
		BudgetMs:  10000,
		TUTConfig: tut.Config{Command: "agent"},
	})
	r.Adapter = adapter

	result, err := r.Run(t.Context())
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Summary.Passed != 1 {
		t.Fatalf("passed summary = %d, want 1", result.Summary.Passed)
	}
	for _, a := range result.Scenarios[0].Assertions {
		if a.AssertionType == "nondeterminism.env" {
			t.Fatalf("unexpected nondeterminism.env warning: %+v", a)
		}
	}
}

func TestRunnerRun_DeterminismEnvMismatchWarns(t *testing.T) {
	evalsDir := writeSingleScenarioSuite(t, "smoke", "scenario: env_mismatch\ninput:\n  messages:\n    - role: user\n      content: hello\n")
	adapter := &fakeAdapter{
		level: tut.LevelGood,
		handle: &fakeHandle{
			output: &tut.AgentOutput{
				Raw:    []byte(`{"ok":true}`),
				Parsed: map[string]interface{}{"ok": true},
			},
			capabilities: &tut.SDKCapabilities{
				ProtocolVersion: tut.CapabilityProtocolV1,
				SDK:             "gauntlet-python",
				Adapters: map[string]tut.SDKAdapterCapability{
					"openai": {Enabled: true, Patched: true},
				},
			},
			traces: []tut.TraceEvent{
				{
					EventType: "determinism_env",
					DeterminismEnv: &tut.DeterminismEnvReport{
						Language:            "python",
						Runtime:             "python3.11",
						RequestedFreezeTime: "2025-01-15T10:00:00Z",
						TimePatched:         false,
						RequestedTimezone:   "UTC",
						EffectiveTimezone:   "America/Los_Angeles",
						TimezoneApplied:     false,
						RequestedLocale:     "en_US.UTF-8",
						EffectiveLocale:     "C",
						LocaleApplied:       false,
					},
				},
			},
		},
	}

	r := NewRunner(Config{
		Suite:     "smoke",
		EvalsDir:  evalsDir,
		OutputDir: filepath.Join(t.TempDir(), "runs"),
		Mode:      "local",
		BudgetMs:  10000,
		TUTConfig: tut.Config{Command: "agent"},
	})
	r.Adapter = adapter

	result, err := r.Run(t.Context())
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Summary.Passed != 1 {
		t.Fatalf("passed summary = %d, want 1", result.Summary.Passed)
	}
	if result.Scenarios[0].FailureCategory != "nondeterminism_warning" {
		t.Fatalf("failure category = %q, want nondeterminism_warning", result.Scenarios[0].FailureCategory)
	}
	found := false
	for _, a := range result.Scenarios[0].Assertions {
		if a.AssertionType == "nondeterminism.env" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected nondeterminism.env warning, got %+v", result.Scenarios[0].Assertions)
	}
}

func TestRunnerRun_NonPythonMissingDeterminismReportWarns(t *testing.T) {
	evalsDir := writeSingleScenarioSuite(t, "smoke", "scenario: non_python_env\ninput:\n  messages:\n    - role: user\n      content: hello\n")
	adapter := &fakeAdapter{
		level: tut.LevelGood,
		handle: &fakeHandle{
			output: &tut.AgentOutput{
				Raw:    []byte(`{"ok":true}`),
				Parsed: map[string]interface{}{"ok": true},
			},
			capabilities: &tut.SDKCapabilities{
				ProtocolVersion: tut.CapabilityProtocolV1,
				SDK:             "gauntlet-js",
				Adapters: map[string]tut.SDKAdapterCapability{
					"openai": {Enabled: true, Patched: true},
				},
			},
		},
	}

	r := NewRunner(Config{
		Suite:     "smoke",
		EvalsDir:  evalsDir,
		OutputDir: filepath.Join(t.TempDir(), "runs"),
		Mode:      "local",
		BudgetMs:  10000,
		TUTConfig: tut.Config{Command: "agent"},
	})
	r.Adapter = adapter

	result, err := r.Run(t.Context())
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Summary.Passed != 1 {
		t.Fatalf("passed summary = %d, want 1", result.Summary.Passed)
	}
	if result.Scenarios[0].FailureCategory != "nondeterminism_warning" {
		t.Fatalf("failure category = %q, want nondeterminism_warning", result.Scenarios[0].FailureCategory)
	}
	found := false
	for _, a := range result.Scenarios[0].Assertions {
		if a.AssertionType == "nondeterminism.env" && strings.Contains(a.Message, "gauntlet-js") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected non-python determinism warning, got %+v", result.Scenarios[0].Assertions)
	}
}

func TestRunnerRun_ArtifactWriteErrorSurfaced(t *testing.T) {
	evalsDir := writeSingleScenarioSuite(t, "smoke", "scenario: fails\ninput:\n  messages:\n    - role: user\n      content: hello\nassertions:\n  - type: tool_sequence\n    required: [missing_tool]\n")
	// Create a read-only output dir so artifact writes fail
	outputDir := filepath.Join(t.TempDir(), "runs")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir output dir: %v", err)
	}
	// Write results.json and summary.md will succeed, but the scenario artifact
	// directory creation will fail because we make the scenario subdir a file
	scenarioBlocker := filepath.Join(outputDir, "fails")
	if err := os.WriteFile(scenarioBlocker, []byte("blocker"), 0o444); err != nil {
		t.Fatalf("write blocker file: %v", err)
	}

	r := NewRunner(Config{
		Suite:     "smoke",
		EvalsDir:  evalsDir,
		OutputDir: outputDir,
		Mode:      "local",
		DryRun:    true,
		BudgetMs:  10000,
	})

	result, err := r.Run(t.Context())
	if err == nil {
		t.Fatal("expected artifact write error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to write artifact bundles") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(err.Error(), `scenario "fails"`) {
		t.Fatalf("error should name the scenario: %v", err)
	}
	// Result should still be returned even when artifact writes fail
	if result == nil {
		t.Fatal("expected non-nil result even on artifact write error")
	}
	if result.Summary.Failed != 1 {
		t.Fatalf("failed summary = %d, want 1", result.Summary.Failed)
	}
}

func writeSingleScenarioSuite(t *testing.T, suite, scenarioYAML string) string {
	t.Helper()
	evalsDir := t.TempDir()
	suiteDir := filepath.Join(evalsDir, suite)
	if err := os.MkdirAll(suiteDir, 0o755); err != nil {
		t.Fatalf("mkdir suite dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(suiteDir, "scenario.yaml"), []byte(scenarioYAML), 0o644); err != nil {
		t.Fatalf("write scenario: %v", err)
	}
	return evalsDir
}

type fakeAdapter struct {
	handle     *fakeHandle
	startErr   error
	startCalls int
	lastConfig tut.Config
	configs    []tut.Config
	level      tut.IntegrationLevel
}

func (a *fakeAdapter) Level() tut.IntegrationLevel {
	if a.level != "" {
		return a.level
	}
	return tut.LevelMinimal
}

func (a *fakeAdapter) Start(ctx context.Context, config tut.Config) (tut.Handle, error) {
	a.startCalls++
	cloned := config
	cloned.Env = cloneEnv(config.Env)
	a.lastConfig = cloned
	a.configs = append(a.configs, cloned)
	if a.startErr != nil {
		return nil, a.startErr
	}
	if a.handle == nil {
		a.handle = &fakeHandle{}
	}
	return a.handle, nil
}

type fakeHandle struct {
	output       *tut.AgentOutput
	traces       []tut.TraceEvent
	capabilities *tut.SDKCapabilities
	runDelay     time.Duration
	runErr       error
	stopErr      error
	runCalls     int
	stopCalls    int
}

func (h *fakeHandle) Run(ctx context.Context, input scenario.Input) (*tut.AgentOutput, error) {
	h.runCalls++
	if h.runDelay > 0 {
		select {
		case <-time.After(h.runDelay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if h.runErr != nil {
		return nil, h.runErr
	}
	if h.output != nil {
		return h.output, nil
	}
	return &tut.AgentOutput{Parsed: make(map[string]interface{})}, nil
}

func (h *fakeHandle) Traces() []tut.TraceEvent {
	return h.traces
}

func (h *fakeHandle) Capabilities() *tut.SDKCapabilities {
	return h.capabilities
}

func (h *fakeHandle) Stop(ctx context.Context) error {
	h.stopCalls++
	return h.stopErr
}

func cloneEnv(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
