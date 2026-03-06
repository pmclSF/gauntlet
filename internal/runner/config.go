package runner

import (
	"github.com/pmclSF/gauntlet/internal/determinism"
	"github.com/pmclSF/gauntlet/internal/output"
	"github.com/pmclSF/gauntlet/internal/scenario"
	"github.com/pmclSF/gauntlet/internal/tut"
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
	FailFast         bool
	MaxArtifactBytes int64
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
	Baseline  interface{}
	PROutput  *tut.AgentOutput
}

// NewRunner creates a new Runner with the given configuration.
func NewRunner(cfg Config) *Runner {
	if cfg.MaxArtifactBytes <= 0 {
		cfg.MaxArtifactBytes = output.DefaultMaxArtifactBytes
	}
	return &Runner{
		Config:  cfg,
		Harness: determinism.NewHarness(),
	}
}
