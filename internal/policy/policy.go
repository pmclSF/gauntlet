// Package policy loads and resolves evals/gauntlet.yml into runtime paths and
// execution settings used by the CLI runner.
package policy

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Resolved is the normalized runtime configuration derived from a policy file.
type Resolved struct {
	BudgetMs    int64
	RunnerMode  string
	ModelMode   string
	SuiteDir    string
	ToolsDir    string
	DBDir       string
	BaselineDir string
	FixturesDir string
	ProxyAddr   string
	TUT         TUTConfig
}

// TUTConfig is a policy-derived TUT launch configuration.
type TUTConfig struct {
	Adapter   string
	Command   string
	Args      []string
	WorkDir   string
	Env       map[string]string
	HTTPPort  int
	HTTPPath  string
	StartupMs int
}

type filePolicy struct {
	Suite        string                 `yaml:"suite"`
	Suites       map[string]suitePolicy `yaml:"suites"`
	Defaults     defaultsPolicy         `yaml:"defaults"`
	ScenariosDir string                 `yaml:"scenarios_dir"`
	WorldDir     string                 `yaml:"world_dir"`
	FixturesDir  string                 `yaml:"fixtures_dir"`
	BaselinesDir string                 `yaml:"baselines_dir"`
	TUT          tutPolicy              `yaml:"tut"`
	Proxy        proxyPolicy            `yaml:"proxy"`
}

type suitePolicy struct {
	Scenarios  string `yaml:"scenarios"`
	BudgetMs   int64  `yaml:"budget_ms"`
	Mode       string `yaml:"mode"`
	RunnerMode string `yaml:"runner_mode"`
	ModelMode  string `yaml:"model_mode"`
}

type defaultsPolicy struct {
	BudgetMs   int64  `yaml:"budget_ms"`
	Mode       string `yaml:"mode"`
	RunnerMode string `yaml:"runner_mode"`
	ModelMode  string `yaml:"model_mode"`
}

type tutPolicy struct {
	Type       string            `yaml:"type"`
	Adapter    string            `yaml:"adapter"`
	Command    string            `yaml:"command"`
	Args       []string          `yaml:"args"`
	WorkDir    string            `yaml:"work_dir"`
	WorkingDir string            `yaml:"working_dir"`
	Env        map[string]string `yaml:"env"`
	HTTPPort   int               `yaml:"http_port"`
	HTTPPath   string            `yaml:"http_path"`
	StartupMs  int               `yaml:"startup_ms"`
}

type proxyPolicy struct {
	Addr string `yaml:"addr"`
	Mode string `yaml:"mode"`
}

// Load resolves policy data for the requested suite.
func Load(path string, suite string) (*Resolved, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read policy %s: %w", path, err)
	}

	var raw filePolicy
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse policy %s: %w", path, err)
	}

	baseDir := filepath.Dir(path)
	suiteCfg := raw.Suites[suite]

	runnerMode, modelMode := parseModes(
		suiteCfg.RunnerMode,
		suiteCfg.Mode,
		raw.Defaults.RunnerMode,
		raw.Defaults.Mode,
		raw.Proxy.Mode,
		suiteCfg.ModelMode,
		raw.Defaults.ModelMode,
	)

	budgetMs := suiteCfg.BudgetMs
	if budgetMs <= 0 {
		budgetMs = raw.Defaults.BudgetMs
	}

	suiteDir := deriveSuiteDir(baseDir, suite, raw.ScenariosDir, suiteCfg.Scenarios)

	worldDir := smartResolvePath(baseDir, raw.WorldDir)
	if worldDir == "" {
		worldDir = filepath.Join(baseDir, "world")
	}

	fixturesDir := smartResolvePath(baseDir, raw.FixturesDir)
	if fixturesDir == "" {
		fixturesDir = filepath.Join(baseDir, "fixtures")
	}

	baselineDir := smartResolvePath(baseDir, raw.BaselinesDir)
	if baselineDir == "" {
		baselineDir = filepath.Join(baseDir, "baselines")
	}
	if filepath.Base(baselineDir) == suite {
		baselineDir = filepath.Dir(baselineDir)
	}

	res := &Resolved{
		BudgetMs:    budgetMs,
		RunnerMode:  runnerMode,
		ModelMode:   modelMode,
		SuiteDir:    suiteDir,
		ToolsDir:    filepath.Join(worldDir, "tools"),
		DBDir:       filepath.Join(worldDir, "databases"),
		BaselineDir: baselineDir,
		FixturesDir: fixturesDir,
		ProxyAddr:   strings.TrimSpace(raw.Proxy.Addr),
		TUT:         normalizeTUT(baseDir, raw.TUT),
	}
	if res.ProxyAddr == "" {
		res.ProxyAddr = "localhost:7431"
	}

	return res, nil
}

func normalizeTUT(baseDir string, in tutPolicy) TUTConfig {
	cfg := TUTConfig{
		Adapter:   strings.TrimSpace(firstNonEmpty(in.Adapter, in.Type)),
		Command:   strings.TrimSpace(in.Command),
		Args:      append([]string{}, in.Args...),
		WorkDir:   strings.TrimSpace(firstNonEmpty(in.WorkDir, in.WorkingDir)),
		HTTPPort:  in.HTTPPort,
		HTTPPath:  strings.TrimSpace(in.HTTPPath),
		StartupMs: in.StartupMs,
	}
	if cfg.WorkDir != "" {
		cfg.WorkDir = smartResolvePath(baseDir, cfg.WorkDir)
	}
	if cfg.Env != nil {
		cfg.Env = copyMap(cfg.Env)
	}
	if in.Env != nil {
		cfg.Env = copyMap(in.Env)
	}
	if cfg.Command != "" && len(cfg.Args) == 0 {
		fields := strings.Fields(cfg.Command)
		if len(fields) > 1 {
			cfg.Command = fields[0]
			cfg.Args = fields[1:]
		}
	}
	return cfg
}

func copyMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func deriveSuiteDir(baseDir, suite, scenariosDir, scenariosGlob string) string {
	if strings.TrimSpace(scenariosDir) != "" {
		return smartResolvePath(baseDir, scenariosDir)
	}
	if strings.TrimSpace(scenariosGlob) != "" {
		resolved := smartResolvePath(baseDir, scenariosGlob)
		if hasGlobPattern(resolved) {
			return filepath.Dir(resolved)
		}
		return resolved
	}
	return filepath.Join(baseDir, suite)
}

func parseModes(candidates ...string) (runnerMode, modelMode string) {
	for _, raw := range candidates {
		mode := strings.ToLower(strings.TrimSpace(raw))
		if mode == "" {
			continue
		}
		if runnerMode == "" && isRunnerMode(mode) {
			runnerMode = mode
		}
		if modelMode == "" && isModelMode(mode) {
			modelMode = mode
		}
	}
	return runnerMode, modelMode
}

func isRunnerMode(mode string) bool {
	switch mode {
	case "local", "pr_ci", "fork_pr", "nightly":
		return true
	default:
		return false
	}
}

func isModelMode(mode string) bool {
	switch mode {
	case "recorded", "live", "passthrough":
		return true
	default:
		return false
	}
}

func smartResolvePath(baseDir, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}

	cwdCandidate := filepath.Clean(value)
	baseCandidate := filepath.Clean(filepath.Join(baseDir, value))
	var evalsParentCandidate string
	if strings.HasPrefix(filepath.ToSlash(value), "evals/") && filepath.Base(baseDir) == "evals" {
		evalsParentCandidate = filepath.Clean(filepath.Join(filepath.Dir(baseDir), value))
	}

	cwdExists := probePathExists(cwdCandidate)
	baseExists := probePathExists(baseCandidate)
	evalsParentExists := evalsParentCandidate != "" && probePathExists(evalsParentCandidate)

	switch {
	case cwdExists && !baseExists:
		return absolutePath(cwdCandidate)
	case evalsParentExists && !cwdExists && !baseExists:
		return absolutePath(evalsParentCandidate)
	case baseExists && !cwdExists:
		return absolutePath(baseCandidate)
	case evalsParentExists && !baseExists:
		return absolutePath(evalsParentCandidate)
	case cwdExists:
		return absolutePath(cwdCandidate)
	default:
		return absolutePath(baseCandidate)
	}
}

func probePathExists(path string) bool {
	probe := path
	if hasGlobPattern(path) {
		probe = filepath.Dir(path)
	}
	info, err := os.Stat(probe)
	return err == nil && (info.IsDir() || !info.IsDir())
}

func hasGlobPattern(path string) bool {
	return strings.ContainsAny(path, "*?[")
}

func absolutePath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(abs)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
