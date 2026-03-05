// Package policy loads and resolves evals/gauntlet.yml into runtime paths and
// execution settings used by the CLI runner.
package policy

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pmclSF/gauntlet/internal/assertions"
	"github.com/pmclSF/gauntlet/internal/tut"
	"gopkg.in/yaml.v3"
)

// Resolved is the normalized runtime configuration derived from a policy file.
type Resolved struct {
	BudgetMs                int64
	RunnerMode              string
	ModelMode               string
	SuiteDir                string
	ToolsDir                string
	DBDir                   string
	BaselineDir             string
	FixturesDir             string
	ProxyAddr               string
	TUT                     TUTConfig
	RedactFields            []string // field paths like "**.api_key"
	PromptInjectionDenylist bool
	AssertionMode           AssertionMode
}

// LoadOptions configures policy loading behavior.
type LoadOptions struct {
	// Strict rejects unknown keys anywhere in the policy document.
	Strict bool
}

// AssertionMode controls which assertion types are hard gates vs soft signals.
type AssertionMode struct {
	HardGates   []string
	SoftSignals []string
}

// TUTConfig is a policy-derived TUT launch configuration.
type TUTConfig struct {
	Adapter        string
	Command        string
	Args           []string
	WorkDir        string
	Env            map[string]string
	HTTPPort       int
	HTTPPath       string
	StartupMs      int
	ResourceLimits tut.ResourceLimits
	Guardrails     tut.Guardrails
}

type filePolicy struct {
	Suite        string                 `yaml:"suite"`
	Suites       map[string]suitePolicy `yaml:"suites"`
	Defaults     defaultsPolicy         `yaml:"defaults"`
	ScenariosDir string                 `yaml:"scenarios_dir"`
	WorldDir     string                 `yaml:"world_dir"`
	FixturesDir  string                 `yaml:"fixtures_dir"`
	BaselinesDir string                 `yaml:"baselines_dir"`
	Assertions   assertionsPolicy       `yaml:"assertions"`
	TUT          tutPolicy              `yaml:"tut"`
	Proxy        proxyPolicy            `yaml:"proxy"`
	Redaction    redactionPolicy        `yaml:"redaction"`
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
	Type           string               `yaml:"type"`
	Adapter        string               `yaml:"adapter"`
	Command        string               `yaml:"command"`
	Args           []string             `yaml:"args"`
	WorkDir        string               `yaml:"work_dir"`
	WorkingDir     string               `yaml:"working_dir"`
	Env            map[string]string    `yaml:"env"`
	HTTPPort       int                  `yaml:"http_port"`
	HTTPPath       string               `yaml:"http_path"`
	StartupMs      int                  `yaml:"startup_ms"`
	ResourceLimits resourceLimitsPolicy `yaml:"resource_limits"`
	Guardrails     guardrailsPolicy     `yaml:"guardrails"`
}

type resourceLimitsPolicy struct {
	CPUSeconds int   `yaml:"cpu_seconds"`
	MemoryMB   int64 `yaml:"memory_mb"`
	OpenFiles  int   `yaml:"open_files"`
}

type guardrailsPolicy struct {
	HostilePayload bool `yaml:"hostile_payload"`
	MaxProcesses   int  `yaml:"max_processes"`
}

type proxyPolicy struct {
	Addr string `yaml:"addr"`
	Mode string `yaml:"mode"`
}

type redactionPolicy struct {
	FieldPaths              []string `yaml:"field_paths"`
	Patterns                []string `yaml:"patterns"`
	PromptInjectionDenylist *bool    `yaml:"prompt_injection_denylist"`
}

type assertionsPolicy struct {
	HardGates   []string               `yaml:"hard_gates"`
	SoftSignals []string               `yaml:"soft_signals"`
	Extra       map[string]interface{} `yaml:",inline"`
}

// Load resolves policy data for the requested suite.
func Load(path string, suite string) (*Resolved, error) {
	return LoadWithOptions(path, suite, LoadOptions{Strict: strictPolicyFromEnv()})
}

// LoadWithOptions resolves policy data for the requested suite with explicit
// parsing options.
func LoadWithOptions(path string, suite string, opts LoadOptions) (*Resolved, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read policy %s: %w", path, err)
	}
	if err := validatePolicyDocument(path, data, opts); err != nil {
		return nil, err
	}

	var raw filePolicy
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse policy %s: %w", path, err)
	}
	assertionMode, err := normalizeAssertionMode(raw.Assertions)
	if err != nil {
		return nil, fmt.Errorf("invalid assertions policy in %s: %w", path, err)
	}
	if err := validateProxyMode(raw.Proxy.Mode); err != nil {
		return nil, fmt.Errorf("invalid proxy.mode in %s: %w", path, err)
	}

	baseDir := filepath.Dir(path)
	suiteCfg := raw.Suites[suite]

	runnerMode := firstRunnerMode(
		suiteCfg.RunnerMode,
		suiteCfg.Mode,
		raw.Defaults.RunnerMode,
		raw.Defaults.Mode,
	)
	modelMode := firstModelMode(
		suiteCfg.ModelMode,
		suiteCfg.Mode,
		raw.Defaults.ModelMode,
		raw.Defaults.Mode,
		raw.Proxy.Mode,
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
		BudgetMs:                budgetMs,
		RunnerMode:              runnerMode,
		ModelMode:               modelMode,
		SuiteDir:                suiteDir,
		ToolsDir:                filepath.Join(worldDir, "tools"),
		DBDir:                   filepath.Join(worldDir, "databases"),
		BaselineDir:             baselineDir,
		FixturesDir:             fixturesDir,
		ProxyAddr:               strings.TrimSpace(raw.Proxy.Addr),
		TUT:                     normalizeTUT(baseDir, raw.TUT),
		RedactFields:            raw.Redaction.FieldPaths,
		PromptInjectionDenylist: boolOrDefault(raw.Redaction.PromptInjectionDenylist, true),
		AssertionMode:           assertionMode,
	}
	if res.ProxyAddr == "" {
		res.ProxyAddr = "localhost:7431"
	}

	return res, nil
}

func normalizeAssertionMode(in assertionsPolicy) (AssertionMode, error) {
	if len(in.Extra) > 0 {
		keys := make([]string, 0, len(in.Extra))
		for key := range in.Extra {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		return AssertionMode{}, fmt.Errorf("unknown key(s) under assertions: %s (expected hard_gates, soft_signals)", strings.Join(keys, ", "))
	}

	known := make(map[string]bool, len(assertions.RegisteredTypes()))
	for _, name := range assertions.RegisteredTypes() {
		known[name] = true
	}

	hard, err := normalizeAssertionList("hard_gates", in.HardGates, known)
	if err != nil {
		return AssertionMode{}, err
	}
	soft, err := normalizeAssertionList("soft_signals", in.SoftSignals, known)
	if err != nil {
		return AssertionMode{}, err
	}

	hardSet := make(map[string]bool, len(hard))
	for _, name := range hard {
		hardSet[name] = true
	}
	for _, name := range soft {
		if hardSet[name] {
			return AssertionMode{}, fmt.Errorf("assertion type %q cannot be listed in both hard_gates and soft_signals", name)
		}
	}

	return AssertionMode{
		HardGates:   hard,
		SoftSignals: soft,
	}, nil
}

func normalizeAssertionList(field string, raw []string, known map[string]bool) ([]string, error) {
	out := make([]string, 0, len(raw))
	seen := map[string]bool{}
	for _, value := range raw {
		name := strings.TrimSpace(value)
		if name == "" {
			continue
		}
		if !known[name] {
			return nil, fmt.Errorf("%s contains unknown assertion type %q", field, name)
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
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
		ResourceLimits: tut.ResourceLimits{
			CPUSeconds: in.ResourceLimits.CPUSeconds,
			MemoryMB:   in.ResourceLimits.MemoryMB,
			OpenFiles:  in.ResourceLimits.OpenFiles,
		},
		Guardrails: tut.Guardrails{
			HostilePayload: in.Guardrails.HostilePayload,
			MaxProcesses:   in.Guardrails.MaxProcesses,
		},
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

func boolOrDefault(v *bool, fallback bool) bool {
	if v == nil {
		return fallback
	}
	return *v
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

func firstRunnerMode(candidates ...string) string {
	for _, raw := range candidates {
		mode := strings.ToLower(strings.TrimSpace(raw))
		if mode == "" {
			continue
		}
		if isRunnerMode(mode) {
			return mode
		}
	}
	return ""
}

func firstModelMode(candidates ...string) string {
	for _, raw := range candidates {
		mode := strings.ToLower(strings.TrimSpace(raw))
		if mode == "" {
			continue
		}
		if isModelMode(mode) {
			return mode
		}
	}
	return ""
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

func validateProxyMode(raw string) error {
	mode := strings.ToLower(strings.TrimSpace(raw))
	if mode == "" {
		return nil
	}
	if isModelMode(mode) {
		return nil
	}
	if isRunnerMode(mode) {
		return fmt.Errorf(
			"%q is a runner mode; proxy.mode only accepts recorded, live, or passthrough. Use defaults.runner_mode or suites.<name>.runner_mode for runner execution mode",
			mode,
		)
	}
	return fmt.Errorf("%q is not a supported proxy mode", mode)
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
	_, err := os.Stat(probe)
	return err == nil
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
