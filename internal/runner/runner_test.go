package runner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gauntlet-dev/gauntlet/internal/scenario"
	"github.com/gauntlet-dev/gauntlet/internal/tut"
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
		Suite:      "smoke",
		ConfigPath: "/tmp/gauntlet.yml",
		Mode:       "local",
		OutputDir:  "/tmp/output",
		EvalsDir:   "/tmp/evals",
		DryRun:     true,
		BudgetMs:   30000,
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
	if len(result.Scenarios[0].Assertions) == 0 {
		t.Fatal("expected tut_execution assertion on adapter error")
	}
	if got := result.Scenarios[0].Assertions[0].AssertionType; got != "tut_execution" {
		t.Fatalf("assertion type = %q, want %q", got, "tut_execution")
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
}

func (a *fakeAdapter) Level() tut.IntegrationLevel {
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
	output    *tut.AgentOutput
	traces    []tut.TraceEvent
	runErr    error
	stopErr   error
	runCalls  int
	stopCalls int
}

func (h *fakeHandle) Run(ctx context.Context, input scenario.Input) (*tut.AgentOutput, error) {
	h.runCalls++
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
