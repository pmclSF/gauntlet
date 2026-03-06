package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/pmclSF/gauntlet/internal/ci"
	"github.com/pmclSF/gauntlet/internal/fixture"
	"github.com/pmclSF/gauntlet/internal/policy"
	"github.com/pmclSF/gauntlet/internal/proxy"
	"github.com/pmclSF/gauntlet/internal/runner"
	"github.com/pmclSF/gauntlet/internal/scenario"
	"github.com/pmclSF/gauntlet/internal/tut"
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
	cfg.FailFast = resolved.FailFast
	cfg.HardGates = toBoolSet(resolved.AssertionMode.HardGates)
	cfg.SoftSignals = toBoolSet(resolved.AssertionMode.SoftSignals)
	cfg.TUTConfig = tut.Config{
		Command:   resolved.TUT.Command,
		Args:      append([]string{}, resolved.TUT.Args...),
		WorkDir:   resolved.TUT.WorkDir,
		Env:       copyStringMap(resolved.TUT.Env),
		Adapter:   resolved.TUT.Adapter,
		HTTPPort:  resolved.TUT.HTTPPort,
		HTTPPath:  resolved.TUT.HTTPPath,
		StartupMs: resolved.TUT.StartupMs,
		ResourceLimits: tut.ResourceLimits{
			CPUSeconds: resolved.TUT.ResourceLimits.CPUSeconds,
			MemoryMB:   resolved.TUT.ResourceLimits.MemoryMB,
			OpenFiles:  resolved.TUT.ResourceLimits.OpenFiles,
		},
		Guardrails: tut.Guardrails{
			HostilePayload: resolved.TUT.Guardrails.HostilePayload,
			MaxProcesses:   resolved.TUT.Guardrails.MaxProcesses,
		},
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

	modelMode, err := resolveModelMode(modelModeOverride, resolved)
	if err != nil {
		return nil, err
	}
	if isUntrustedCIContext() && modelMode != "recorded" {
		return nil, fmt.Errorf("untrusted CI context (%s) requires GAUNTLET_MODEL_MODE=recorded (got %q)", ci.DetectMode(), modelMode)
	}
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
		addr = "localhost:0"
	}

	if modelMode == "passthrough" {
		return nil, nil
	}

	proxyMode, err := parseProxyMode(modelMode)
	if err != nil {
		return nil, err
	}
	store := fixture.NewStore(fixturesDir)
	scenarioDigest := computeScenarioSetDigest(cfg.SuiteDir)
	store.SetReplayContext(cfg.Suite, scenarioDigest)
	cfg.TUTConfig.Env["GAUNTLET_RUNNER_MODE"] = strings.TrimSpace(cfg.Mode)
	if suite := strings.TrimSpace(cfg.Suite); suite != "" {
		cfg.TUTConfig.Env["GAUNTLET_SUITE"] = suite
	}
	if scenarioDigest != "" {
		cfg.TUTConfig.Env["GAUNTLET_SCENARIO_SET_SHA256"] = scenarioDigest
	}
	cfg.TUTConfig.Env["GAUNTLET_REPLAY_LOCKFILE"] = filepath.Join(fixturesDir, fixture.DefaultReplayLockfileName)
	cfg.TUTConfig.Env["GAUNTLET_REQUIRE_TOOL_FIXTURE_LOCKFILE"] = "0"
	cfg.TUTConfig.Env["GAUNTLET_REQUIRE_FIXTURE_SIGNATURES"] = "0"

	signingKeyPath := effectiveFixtureSigningKeyPath(cfg.ConfigPath)
	if proxyMode == proxy.ModeLive {
		if err := store.EnableFixtureSigning(signingKeyPath); err != nil {
			return nil, fmt.Errorf("fixture signing setup failed: %w", err)
		}
	}

	if modelMode == "recorded" && requiresRecordedModelMode(cfg.Mode) {
		if err := fixture.VerifyReplayLockfile(store, cfg.Suite, scenarioDigest, ""); err != nil {
			return nil, fmt.Errorf("replay integrity check failed: %w\n  Regenerate lockfile with: gauntlet lock-fixtures --suite %s --config %s", err, cfg.Suite, cfg.ConfigPath)
		}
		if err := store.ConfigureFixtureTrust(fixture.FixtureTrustOptions{
			RequireSignatures:         true,
			TrustedPublicKeyPaths:     []string{effectiveFixtureTrustedPublicKeyPath(signingKeyPath)},
			TrustedRecorderIdentities: trustedRecorderIdentitiesFromEnv(),
		}); err != nil {
			return nil, fmt.Errorf(
				"fixture trust policy failed: %w\n  Ensure trusted replay public key exists and matches recorder signatures.\n  Expected key: %s\n  Record/migrate fixtures in trusted context to (re)sign them.",
				err,
				effectiveFixtureTrustedPublicKeyPath(signingKeyPath),
			)
		}
		if err := fixture.VerifyReplayLockfile(store, cfg.Suite, scenarioDigest, ""); err != nil {
			return nil, fmt.Errorf("replay integrity check failed: %w\n  Regenerate lockfile with: gauntlet lock-fixtures --suite %s --config %s", err, cfg.Suite, cfg.ConfigPath)
		}
		cfg.TUTConfig.Env["GAUNTLET_REQUIRE_TOOL_FIXTURE_LOCKFILE"] = "1"
		cfg.TUTConfig.Env["GAUNTLET_REQUIRE_FIXTURE_SIGNATURES"] = "1"
	}

	caDir := filepath.Join(filepath.Dir(cfg.ConfigPath), ".gauntlet")
	ca, err := proxy.LoadCA(caDir)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, classifyProxyStartupError(proxyStartCertIssue, addr, caDir, err)
		}
		ca, err = proxy.GenerateCA(caDir)
		if err != nil {
			return nil, classifyProxyStartupError(proxyStartCertIssue, addr, caDir, err)
		}
	}

	p := proxy.NewProxy(addr, proxyMode, store, ca)
	p.Suite = cfg.Suite
	p.ScenarioSetSHA256 = scenarioDigest
	if err := p.Start(context.Background()); err != nil {
		return nil, classifyProxyStartupError(proxyStartListen, addr, caDir, err)
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
	mode, err := validateModelMode(raw, modelModeFlagName)
	if err != nil {
		return "", err
	}
	switch mode {
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

func effectiveFixtureSigningKeyPath(configPath string) string {
	if override := strings.TrimSpace(os.Getenv("GAUNTLET_FIXTURE_SIGNING_KEY")); override != "" {
		return filepath.Clean(override)
	}
	return filepath.Clean(fixture.DefaultFixtureSigningKeyPath(configPath))
}

func effectiveFixtureTrustedPublicKeyPath(signingKeyPath string) string {
	if override := strings.TrimSpace(os.Getenv("GAUNTLET_FIXTURE_TRUSTED_PUBLIC_KEY")); override != "" {
		return filepath.Clean(override)
	}
	return filepath.Clean(strings.TrimSpace(signingKeyPath) + ".pub.pem")
}

func trustedRecorderIdentitiesFromEnv() []string {
	raw := strings.TrimSpace(os.Getenv("GAUNTLET_TRUSTED_RECORDER_IDENTITIES"))
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	seen := map[string]bool{}
	for _, part := range parts {
		id := strings.TrimSpace(part)
		if id == "" {
			continue
		}
		key := strings.ToLower(id)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, id)
	}
	return out
}

func splitKV(raw string) (string, string, bool) {
	parts := strings.SplitN(raw, "=", 2)
	if len(parts) != 2 || parts[0] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func computeScenarioSetDigest(suiteDir string) string {
	suiteDir = strings.TrimSpace(suiteDir)
	if suiteDir == "" {
		return ""
	}
	scenarios, err := scenario.LoadSuite(suiteDir)
	if err != nil {
		return ""
	}
	entries := make([]string, 0, len(scenarios))
	for _, s := range scenarios {
		payload, marshalErr := json.Marshal(s)
		if marshalErr != nil {
			return ""
		}
		entries = append(entries, fmt.Sprintf("%s:%s", strings.TrimSpace(s.Name), fixture.Hash(payload)))
	}
	sort.Strings(entries)
	return fixture.Hash([]byte(strings.Join(entries, "\n")))
}

func toBoolSet(items []string) map[string]bool {
	if len(items) == 0 {
		return nil
	}
	out := make(map[string]bool, len(items))
	for _, item := range items {
		name := strings.TrimSpace(item)
		if name == "" {
			continue
		}
		out[name] = true
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func isUntrustedCIContext() bool {
	return ci.DetectMode() == "fork_pr"
}

type proxyStartCause string

const (
	proxyStartListen    proxyStartCause = "listen"
	proxyStartCertIssue proxyStartCause = "cert_issue"
)

func classifyProxyStartupError(cause proxyStartCause, addr, caDir string, err error) error {
	rootCause := "unknown"
	remediation := "Check previous error details and rerun with --proxy-addr 127.0.0.1:0 for ephemeral port binding."

	switch {
	case isAddrInUseError(err):
		rootCause = "port_clash"
		remediation = "Stop the process using this port, or rerun with --proxy-addr 127.0.0.1:0."
	case isPermissionError(err):
		rootCause = "permission"
		remediation = "Grant permission for socket/certificate file access or choose a writable config directory."
	case cause == proxyStartCertIssue:
		rootCause = "cert_issue"
		remediation = fmt.Sprintf("Inspect and repair CA files in %s (.gauntlet/ca.pem, .gauntlet/ca.key).", caDir)
	}

	return fmt.Errorf("proxy startup failed [root_cause=%s]: %w\n  Address: %s\n  Fix: %s", rootCause, err, addr, remediation)
}

func isAddrInUseError(err error) bool {
	return errors.Is(err, syscall.EADDRINUSE) ||
		strings.Contains(strings.ToLower(err.Error()), "address already in use")
}

func isPermissionError(err error) bool {
	return errors.Is(err, os.ErrPermission) ||
		errors.Is(err, syscall.EPERM) ||
		errors.Is(err, syscall.EACCES)
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
