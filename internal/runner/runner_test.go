package runner

import (
	"os"
	"testing"
	"time"
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
