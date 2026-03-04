package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gauntlet-dev/gauntlet/internal/fixture"
	"github.com/gauntlet-dev/gauntlet/internal/policy"
	"github.com/gauntlet-dev/gauntlet/internal/proxy"
	"github.com/gauntlet-dev/gauntlet/internal/runner"
	"github.com/gauntlet-dev/gauntlet/internal/tut"
)

func loadPolicyIfPresent(configPath, suite string, explicit bool) (*policy.Resolved, error) {
	info, err := os.Stat(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			if explicit {
				return nil, fmt.Errorf("policy file not found: %s", configPath)
			}
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read policy path %s: %w", configPath, err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("policy path is a directory, expected file: %s", configPath)
	}
	return policy.Load(configPath, suite)
}

func applyResolvedPolicy(cfg *runner.Config, resolved *policy.Resolved, configPath string) {
	if resolved == nil {
		return
	}
	cfg.EvalsDir = filepath.Dir(configPath)
	cfg.SuiteDir = resolved.SuiteDir
	cfg.ToolsDir = resolved.ToolsDir
	cfg.DBDir = resolved.DBDir
	cfg.BaselineDir = resolved.BaselineDir
	cfg.FixturesDir = resolved.FixturesDir
	cfg.TUTConfig = tut.Config{
		Command:   resolved.TUT.Command,
		Args:      append([]string{}, resolved.TUT.Args...),
		WorkDir:   resolved.TUT.WorkDir,
		Env:       copyStringMap(resolved.TUT.Env),
		Adapter:   resolved.TUT.Adapter,
		HTTPPort:  resolved.TUT.HTTPPort,
		HTTPPath:  resolved.TUT.HTTPPath,
		StartupMs: resolved.TUT.StartupMs,
	}
}

func selectAdapter(cfg tut.Config) tut.Adapter {
	switch strings.ToLower(strings.TrimSpace(cfg.Adapter)) {
	case "http":
		return &tut.HTTPAdapter{}
	case "minimal":
		return &tut.CLIAdapter{Minimal: true}
	case "cli", "":
		return &tut.CLIAdapter{}
	default:
		return &tut.CLIAdapter{}
	}
}

func startProxyForRun(cfg *runner.Config, resolved *policy.Resolved, modelModeOverride, proxyAddrOverride string) (*proxy.Proxy, error) {
	if cfg.TUTConfig.Command == "" {
		return nil, nil
	}

	modelMode := effectiveModelMode(modelModeOverride, resolved)
	if requiresRecordedModelMode(cfg.Mode) && modelMode != "recorded" {
		return nil, fmt.Errorf("mode %q requires GAUNTLET_MODEL_MODE=recorded (got %q)", cfg.Mode, modelMode)
	}
	if cfg.TUTConfig.Env == nil {
		cfg.TUTConfig.Env = make(map[string]string)
	}
	cfg.TUTConfig.Env["GAUNTLET_MODEL_MODE"] = modelMode

	fixturesDir := effectiveFixturesDir(cfg)
	cfg.TUTConfig.Env["GAUNTLET_FIXTURE_DIR"] = filepath.Join(fixturesDir, "tools")

	// Pass redaction field paths to the Python SDK for tool fixture redaction
	if resolved != nil && len(resolved.RedactFields) > 0 {
		cfg.TUTConfig.Env["GAUNTLET_REDACT_FIELDS"] = strings.Join(resolved.RedactFields, ",")
	}
	addr := effectiveProxyAddr(proxyAddrOverride, resolved)
	if addr == "" {
		addr = "localhost:7431"
	}

	if modelMode == "passthrough" {
		return nil, nil
	}

	proxyMode, err := parseProxyMode(modelMode)
	if err != nil {
		return nil, err
	}
	store := fixture.NewStore(fixturesDir)

	caDir := filepath.Join(filepath.Dir(cfg.ConfigPath), ".gauntlet")
	ca, err := proxy.LoadCA(caDir)
	if err != nil {
		ca, err = proxy.GenerateCA(caDir)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize proxy CA in %s: %w", caDir, err)
		}
	}

	p := proxy.NewProxy(addr, proxyMode, store, ca)
	if err := p.Start(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to start proxy on %s: %w", addr, err)
	}

	certPath := filepath.Join(caDir, "ca.pem")
	for _, kv := range p.EnvVars(certPath) {
		if k, v, ok := splitKV(kv); ok {
			cfg.TUTConfig.Env[k] = v
		}
	}

	return p, nil
}

func requiresRecordedModelMode(mode string) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "pr_ci", "fork_pr":
		return true
	default:
		return false
	}
}

func parseProxyMode(raw string) (proxy.Mode, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "recorded", "":
		return proxy.ModeRecorded, nil
	case "live":
		return proxy.ModeLive, nil
	case "passthrough":
		return proxy.ModePassthrough, nil
	default:
		return "", fmt.Errorf("invalid model mode %q (expected recorded, live, or passthrough)", raw)
	}
}

func effectiveModelMode(modelModeOverride string, resolved *policy.Resolved) string {
	if mode := strings.ToLower(strings.TrimSpace(modelModeOverride)); mode != "" {
		return mode
	}
	if mode := strings.ToLower(strings.TrimSpace(os.Getenv("GAUNTLET_MODEL_MODE"))); mode != "" {
		return mode
	}
	if resolved != nil {
		if mode := strings.ToLower(strings.TrimSpace(resolved.ModelMode)); mode != "" {
			return mode
		}
	}
	return "recorded"
}

func effectiveProxyAddr(proxyAddrOverride string, resolved *policy.Resolved) string {
	if addr := strings.TrimSpace(proxyAddrOverride); addr != "" {
		return addr
	}
	if resolved != nil {
		return strings.TrimSpace(resolved.ProxyAddr)
	}
	return ""
}

func effectiveFixturesDir(cfg *runner.Config) string {
	if strings.TrimSpace(cfg.FixturesDir) != "" {
		return cfg.FixturesDir
	}
	base := filepath.Dir(cfg.ConfigPath)
	if strings.TrimSpace(base) == "" || base == "." {
		base = "evals"
	}
	return filepath.Join(base, "fixtures")
}

func splitKV(raw string) (string, string, bool) {
	parts := strings.SplitN(raw, "=", 2)
	if len(parts) != 2 || parts[0] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func copyStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
