package runner

import (
	"fmt"
	"path/filepath"

	"github.com/pmclSF/gauntlet/internal/scenario"
)

// resolvedPaths holds the resolved directory paths for a run.
type resolvedPaths struct {
	EvalsDir    string
	SuiteDir    string
	ToolsDir    string
	DBDir       string
	BaselineDir string
}

func resolvePaths(cfg Config) resolvedPaths {
	p := resolvedPaths{EvalsDir: cfg.EvalsDir}
	if p.EvalsDir == "" {
		p.EvalsDir = "evals"
	}
	p.SuiteDir = cfg.SuiteDir
	if p.SuiteDir == "" {
		p.SuiteDir = filepath.Join(p.EvalsDir, cfg.Suite)
	}
	p.ToolsDir = cfg.ToolsDir
	if p.ToolsDir == "" {
		p.ToolsDir = filepath.Join(p.EvalsDir, "world", "tools")
	}
	p.DBDir = cfg.DBDir
	if p.DBDir == "" {
		p.DBDir = filepath.Join(p.EvalsDir, "world", "databases")
	}
	p.BaselineDir = cfg.BaselineDir
	if p.BaselineDir == "" {
		p.BaselineDir = filepath.Join(p.EvalsDir, "baselines")
	}
	return p
}

func loadAndFilterScenarios(suiteDir, filter string, suiteName string) ([]*scenario.Scenario, error) {
	scenarios, err := scenario.LoadSuite(suiteDir)
	if err != nil {
		return nil, fmt.Errorf("failed to load suite: %w", err)
	}
	if filter == "" {
		return scenarios, nil
	}
	var filtered []*scenario.Scenario
	for _, s := range scenarios {
		if s.Name == filter {
			filtered = append(filtered, s)
		}
	}
	if len(filtered) == 0 {
		return nil, fmt.Errorf("scenario '%s' not found in suite '%s'", filter, suiteName)
	}
	return filtered, nil
}
