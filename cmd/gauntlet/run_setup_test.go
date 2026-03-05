package main

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pmclSF/gauntlet/internal/fixture"
	"github.com/pmclSF/gauntlet/internal/policy"
	"github.com/pmclSF/gauntlet/internal/proxy"
	"github.com/pmclSF/gauntlet/internal/proxy/providers"
	"github.com/pmclSF/gauntlet/internal/runner"
	"github.com/pmclSF/gauntlet/internal/tut"
)

func TestLoadPolicyIfPresent_DefaultMissingReturnsNil(t *testing.T) {
	p, err := loadPolicyIfPresent(filepath.Join(t.TempDir(), "gauntlet.yml"), "smoke", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p != nil {
		t.Fatalf("expected nil policy when default path missing, got %#v", p)
	}
}

func TestLoadPolicyIfPresent_ExplicitMissingErrors(t *testing.T) {
	_, err := loadPolicyIfPresent(filepath.Join(t.TempDir(), "gauntlet.yml"), "smoke", true)
	if err == nil {
		t.Fatal("expected error for missing explicit policy path")
	}
}

func TestParseProxyMode(t *testing.T) {
	tests := []struct {
		in           string
		want         proxy.Mode
		wantErr      bool
		wantContains string
	}{
		{in: "", want: proxy.ModeRecorded},
		{in: "recorded", want: proxy.ModeRecorded},
		{in: "live", want: proxy.ModeLive},
		{in: "passthrough", want: proxy.ModePassthrough},
		{in: "invalid", wantErr: true, wantContains: "invalid model mode"},
		{in: "pr_ci", wantErr: true, wantContains: "--runner-mode"},
	}
	for _, tt := range tests {
		got, err := parseProxyMode(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Fatalf("parseProxyMode(%q): expected error", tt.in)
			}
			if tt.wantContains != "" && !strings.Contains(err.Error(), tt.wantContains) {
				t.Fatalf("parseProxyMode(%q): expected error containing %q, got %v", tt.in, tt.wantContains, err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("parseProxyMode(%q): %v", tt.in, err)
		}
		if got != tt.want {
			t.Fatalf("parseProxyMode(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestApplyResolvedPolicy(t *testing.T) {
	root := t.TempDir()
	cfg := runner.Config{}
	resolved := &policy.Resolved{
		SuiteDir:    filepath.Join(root, "evals", "smoke"),
		ToolsDir:    filepath.Join(root, "evals", "world", "tools"),
		DBDir:       filepath.Join(root, "evals", "world", "databases"),
		BaselineDir: filepath.Join(root, "evals", "baselines"),
		FixturesDir: filepath.Join(root, "evals", "fixtures"),
		AssertionMode: policy.AssertionMode{
			HardGates:   []string{"output_schema"},
			SoftSignals: []string{"sensitive_leak"},
		},
		TUT: policy.TUTConfig{
			Adapter: "cli",
			Command: "python3",
			Args:    []string{"-m", "agent.main"},
			WorkDir: root,
			ResourceLimits: tut.ResourceLimits{
				CPUSeconds: 7,
				MemoryMB:   384,
				OpenFiles:  256,
			},
			Guardrails: tut.Guardrails{
				HostilePayload: true,
				MaxProcesses:   48,
			},
			Env: map[string]string{
				"A": "1",
			},
		},
	}
	configPath := filepath.Join(root, "evals", "gauntlet.yml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("version: 1\n"), 0o644); err != nil {
		t.Fatalf("write policy: %v", err)
	}

	applyResolvedPolicy(&cfg, resolved, configPath)

	if cfg.EvalsDir != filepath.Join(root, "evals") {
		t.Fatalf("EvalsDir = %q", cfg.EvalsDir)
	}
	if cfg.SuiteDir != resolved.SuiteDir {
		t.Fatalf("SuiteDir = %q", cfg.SuiteDir)
	}
	if cfg.TUTConfig.Command != "python3" {
		t.Fatalf("TUT command = %q", cfg.TUTConfig.Command)
	}
	if cfg.TUTConfig.Env["A"] != "1" {
		t.Fatalf("TUT env A = %q", cfg.TUTConfig.Env["A"])
	}
	if cfg.TUTConfig.ResourceLimits.CPUSeconds != 7 || cfg.TUTConfig.ResourceLimits.MemoryMB != 384 || cfg.TUTConfig.ResourceLimits.OpenFiles != 256 {
		t.Fatalf("unexpected TUT resource limits: %+v", cfg.TUTConfig.ResourceLimits)
	}
	if !cfg.TUTConfig.Guardrails.HostilePayload || cfg.TUTConfig.Guardrails.MaxProcesses != 48 {
		t.Fatalf("unexpected TUT guardrails: %+v", cfg.TUTConfig.Guardrails)
	}
	if !cfg.HardGates["output_schema"] {
		t.Fatalf("expected output_schema hard gate from policy")
	}
	if !cfg.SoftSignals["sensitive_leak"] {
		t.Fatalf("expected sensitive_leak soft signal from policy")
	}
}

func TestComputeScenarioSetDigest_ChangesWhenScenarioContentChanges(t *testing.T) {
	suiteDir := t.TempDir()
	path := filepath.Join(suiteDir, "scenario.yaml")
	first := `scenario: order_status
input:
  messages:
    - role: user
      content: "status for ord-001"
`
	if err := os.WriteFile(path, []byte(first), 0o644); err != nil {
		t.Fatalf("write scenario: %v", err)
	}
	digestA := computeScenarioSetDigest(suiteDir)
	if digestA == "" {
		t.Fatal("expected non-empty scenario digest")
	}

	second := `scenario: order_status
input:
  messages:
    - role: user
      content: "status for ord-002"
`
	if err := os.WriteFile(path, []byte(second), 0o644); err != nil {
		t.Fatalf("rewrite scenario: %v", err)
	}
	digestB := computeScenarioSetDigest(suiteDir)
	if digestB == "" {
		t.Fatal("expected non-empty scenario digest after rewrite")
	}
	if digestA == digestB {
		t.Fatalf("expected digest to change when scenario content changes, got %q", digestA)
	}
}

func TestEffectiveProxyAddrPrecedence(t *testing.T) {
	resolved := &policy.Resolved{ProxyAddr: "localhost:7000"}
	if got := effectiveProxyAddr("127.0.0.1:8000", resolved); got != "127.0.0.1:8000" {
		t.Fatalf("override precedence failed: got %q", got)
	}
	if got := effectiveProxyAddr("", resolved); got != "localhost:7000" {
		t.Fatalf("policy precedence failed: got %q", got)
	}
	if got := effectiveProxyAddr("", nil); got != "" {
		t.Fatalf("empty fallback failed: got %q", got)
	}
}

func TestStartProxyForRun_PassthroughSkipsProxyAndInjectsEnv(t *testing.T) {
	forceNonForkCIContext(t)

	root := t.TempDir()
	configPath := filepath.Join(root, "evals", "gauntlet.yml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("version: 1\n"), 0o644); err != nil {
		t.Fatalf("write policy: %v", err)
	}

	cfg := runner.Config{
		ConfigPath: configPath,
		TUTConfig:  tutConfigForTest(),
	}
	resolved := &policy.Resolved{
		ModelMode: "recorded",
		ProxyAddr: "127.0.0.1:0",
	}

	p, err := startProxyForRun(&cfg, resolved, "passthrough", "")
	if err != nil {
		t.Fatalf("startProxyForRun: %v", err)
	}
	if p != nil {
		t.Fatal("expected nil proxy in passthrough mode")
	}
	if got := cfg.TUTConfig.Env["GAUNTLET_MODEL_MODE"]; got != "passthrough" {
		t.Fatalf("GAUNTLET_MODEL_MODE = %q, want passthrough", got)
	}
	if got := cfg.TUTConfig.Env["GAUNTLET_FIXTURE_DIR"]; got == "" {
		t.Fatal("expected GAUNTLET_FIXTURE_DIR to be injected")
	}
}

func TestStartProxyForRun_RecordedStartsProxyAndInjectsEnv(t *testing.T) {
	forceNonForkCIContext(t)

	root := t.TempDir()
	configPath := filepath.Join(root, "evals", "gauntlet.yml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("version: 1\n"), 0o644); err != nil {
		t.Fatalf("write policy: %v", err)
	}

	cfg := runner.Config{
		ConfigPath: configPath,
		TUTConfig:  tutConfigForTest(),
	}
	resolved := &policy.Resolved{
		ModelMode: "recorded",
		ProxyAddr: "127.0.0.1:0",
	}

	p, err := startProxyForRun(&cfg, resolved, "", "")
	if err != nil {
		t.Fatalf("startProxyForRun: %v", err)
	}
	if p == nil {
		t.Fatal("expected proxy instance in recorded mode")
	}
	defer func() { _ = p.Stop() }()

	if got := cfg.TUTConfig.Env["GAUNTLET_MODEL_MODE"]; got != "recorded" {
		t.Fatalf("GAUNTLET_MODEL_MODE = %q, want recorded", got)
	}
	httpProxy := cfg.TUTConfig.Env["HTTP_PROXY"]
	if !strings.HasPrefix(httpProxy, "http://127.0.0.1:") {
		t.Fatalf("HTTP_PROXY = %q, want loopback proxy", httpProxy)
	}
	if strings.HasSuffix(httpProxy, ":0") {
		t.Fatalf("HTTP_PROXY must use resolved listener port, got %q", httpProxy)
	}
	if cfg.TUTConfig.Env["SSL_CERT_FILE"] == "" {
		t.Fatal("expected SSL_CERT_FILE injected")
	}
	if got := cfg.TUTConfig.Env["GAUNTLET_PROXY_PORT"]; got == "" || got == "0" {
		t.Fatalf("GAUNTLET_PROXY_PORT = %q, want resolved non-zero port", got)
	}
	if got := cfg.TUTConfig.Env["GAUNTLET_FIXTURE_DIR"]; !strings.HasSuffix(got, "/evals/fixtures/tools") {
		t.Fatalf("GAUNTLET_FIXTURE_DIR = %q", got)
	}
}

func TestStartProxyForRun_DefaultProxyAddrUsesEphemeralPort(t *testing.T) {
	forceNonForkCIContext(t)

	root := t.TempDir()
	configPath := filepath.Join(root, "evals", "gauntlet.yml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("version: 1\n"), 0o644); err != nil {
		t.Fatalf("write policy: %v", err)
	}

	cfg := runner.Config{
		ConfigPath: configPath,
		TUTConfig:  tutConfigForTest(),
	}
	resolved := &policy.Resolved{
		ModelMode: "recorded",
	}

	p, err := startProxyForRun(&cfg, resolved, "", "")
	if err != nil {
		t.Fatalf("startProxyForRun: %v", err)
	}
	if p == nil {
		t.Fatal("expected proxy instance in recorded mode")
	}
	defer func() { _ = p.Stop() }()

	_, port, splitErr := net.SplitHostPort(p.Addr)
	if splitErr != nil {
		t.Fatalf("proxy addr %q should include host:port: %v", p.Addr, splitErr)
	}
	if port == "0" || port == "" {
		t.Fatalf("proxy addr should resolve to non-zero port, got %q", p.Addr)
	}
	if got := cfg.TUTConfig.Env["GAUNTLET_PROXY_PORT"]; got != port {
		t.Fatalf("GAUNTLET_PROXY_PORT = %q, want %q", got, port)
	}
}

func TestStartProxyForRun_InvalidModelModeFails(t *testing.T) {
	forceNonForkCIContext(t)

	root := t.TempDir()
	configPath := filepath.Join(root, "evals", "gauntlet.yml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("version: 1\n"), 0o644); err != nil {
		t.Fatalf("write policy: %v", err)
	}

	cfg := runner.Config{
		ConfigPath: configPath,
		TUTConfig:  tutConfigForTest(),
	}
	resolved := &policy.Resolved{
		ProxyAddr: "127.0.0.1:0",
	}

	p, err := startProxyForRun(&cfg, resolved, "badmode", "")
	if err == nil {
		if p != nil {
			_ = p.Stop()
		}
		t.Fatal("expected invalid model mode error")
	}
	if !strings.Contains(err.Error(), "invalid model mode") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStartProxyForRun_PRCIRejectsLiveAndPassthrough(t *testing.T) {
	forceNonForkCIContext(t)

	root := t.TempDir()
	configPath := filepath.Join(root, "evals", "gauntlet.yml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("version: 1\n"), 0o644); err != nil {
		t.Fatalf("write policy: %v", err)
	}

	cfg := runner.Config{
		ConfigPath: configPath,
		Mode:       "pr_ci",
		TUTConfig:  tutConfigForTest(),
	}
	for _, mode := range []string{"live", "passthrough"} {
		_, err := startProxyForRun(&cfg, nil, mode, "")
		if err == nil {
			t.Fatalf("expected error for model mode %q in pr_ci", mode)
		}
		if !strings.Contains(err.Error(), "requires GAUNTLET_MODEL_MODE=recorded") {
			t.Fatalf("unexpected error for model mode %q: %v", mode, err)
		}
	}
}

func TestStartProxyForRun_PRCIRecordedRequiresReplayLockfile(t *testing.T) {
	forceNonForkCIContext(t)

	root := t.TempDir()
	configPath := filepath.Join(root, "evals", "gauntlet.yml")
	suiteDir := filepath.Join(root, "evals", "smoke")
	if err := os.MkdirAll(suiteDir, 0o755); err != nil {
		t.Fatalf("mkdir suite: %v", err)
	}
	if err := os.WriteFile(filepath.Join(suiteDir, "scenario.yaml"), []byte("scenario: lock_required\ninput:\n  messages:\n    - role: user\n      content: hi\n"), 0o644); err != nil {
		t.Fatalf("write scenario: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("version: 1\n"), 0o644); err != nil {
		t.Fatalf("write policy: %v", err)
	}

	cfg := runner.Config{
		ConfigPath: configPath,
		Suite:      "smoke",
		SuiteDir:   suiteDir,
		Mode:       "pr_ci",
		TUTConfig:  tutConfigForTest(),
	}
	resolved := &policy.Resolved{
		ModelMode: "recorded",
		ProxyAddr: "127.0.0.1:0",
	}

	_, err := startProxyForRun(&cfg, resolved, "recorded", "")
	if err == nil {
		t.Fatal("expected replay lockfile enforcement error")
	}
	if !strings.Contains(err.Error(), "replay lockfile missing") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStartProxyForRun_PRCIRecordedRequiresTrustedFixturePublicKey(t *testing.T) {
	forceNonForkCIContext(t)
	t.Setenv("GAUNTLET_FIXTURE_TRUSTED_PUBLIC_KEY", "")
	t.Setenv("GAUNTLET_FIXTURE_SIGNING_KEY", "")
	t.Setenv("GAUNTLET_TRUSTED_RECORDER_IDENTITIES", "")

	root := t.TempDir()
	configPath := filepath.Join(root, "evals", "gauntlet.yml")
	suiteDir := filepath.Join(root, "evals", "smoke")
	if err := os.MkdirAll(suiteDir, 0o755); err != nil {
		t.Fatalf("mkdir suite: %v", err)
	}
	if err := os.WriteFile(filepath.Join(suiteDir, "scenario.yaml"), []byte("scenario: trust_required\ninput:\n  messages:\n    - role: user\n      content: hi\n"), 0o644); err != nil {
		t.Fatalf("write scenario: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("version: 1\n"), 0o644); err != nil {
		t.Fatalf("write policy: %v", err)
	}

	cfg := runner.Config{
		ConfigPath: configPath,
		Suite:      "smoke",
		SuiteDir:   suiteDir,
		Mode:       "pr_ci",
		TUTConfig:  tutConfigForTest(),
	}
	scenarioDigest := computeScenarioSetDigest(suiteDir)
	store := fixture.NewStore(filepath.Join(root, "evals", "fixtures"))
	cr := &providers.CanonicalRequest{
		GauntletCanonicalVersion: 1,
		ProviderFamily:           "openai_compatible",
		Model:                    "gpt-4o-mini",
		Messages:                 []providers.CanonicalMessage{{Role: "user", Content: "hello"}},
		Sampling:                 providers.CanonicalSampling{},
	}
	canonicalBytes, err := fixture.CanonicalizeRequest(cr)
	if err != nil {
		t.Fatalf("canonicalize request: %v", err)
	}
	hash := fixture.HashCanonical(canonicalBytes)
	mf := &fixture.ModelFixture{
		FixtureID:         hash,
		HashVersion:       1,
		CanonicalHash:     hash,
		ProviderFamily:    "openai_compatible",
		Model:             "gpt-4o-mini",
		CanonicalRequest:  canonicalBytes,
		Response:          json.RawMessage(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`),
		RecordedAt:        time.Now().UTC(),
		RecordedBy:        "test",
		Suite:             "smoke",
		ScenarioSetSHA256: scenarioDigest,
	}
	if err := store.PutModelFixture(mf); err != nil {
		t.Fatalf("put fixture: %v", err)
	}
	if _, _, err := fixture.WriteReplayLockfile(store, "smoke", scenarioDigest, "", time.Now().UTC()); err != nil {
		t.Fatalf("write replay lockfile: %v", err)
	}

	resolved := &policy.Resolved{
		ModelMode: "recorded",
		ProxyAddr: "127.0.0.1:0",
	}
	_, err = startProxyForRun(&cfg, resolved, "recorded", "")
	if err == nil {
		t.Fatal("expected fixture trust policy error")
	}
	if !strings.Contains(err.Error(), "fixture trust policy failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStartProxyForRun_LiveCreatesFixtureSigningKey(t *testing.T) {
	forceNonForkCIContext(t)

	root := t.TempDir()
	configPath := filepath.Join(root, "evals", "gauntlet.yml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("version: 1\n"), 0o644); err != nil {
		t.Fatalf("write policy: %v", err)
	}

	cfg := runner.Config{
		ConfigPath: configPath,
		TUTConfig:  tutConfigForTest(),
	}
	resolved := &policy.Resolved{
		ProxyAddr: "127.0.0.1:0",
	}
	p, err := startProxyForRun(&cfg, resolved, "live", "")
	if err != nil {
		t.Fatalf("startProxyForRun: %v", err)
	}
	if p == nil {
		t.Fatal("expected proxy instance")
	}
	defer func() { _ = p.Stop() }()

	keyPath := filepath.Join(filepath.Dir(configPath), ".gauntlet", "fixture-signing-key.pem")
	if _, err := os.Stat(keyPath); err != nil {
		t.Fatalf("expected fixture signing key at %s: %v", keyPath, err)
	}
	if _, err := os.Stat(keyPath + ".pub.pem"); err != nil {
		t.Fatalf("expected fixture signing public key at %s.pub.pem: %v", keyPath, err)
	}
}

func TestStartProxyForRun_UntrustedCIRejectsLiveRegardlessOfModeFlag(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_EVENT_NAME", "pull_request")
	t.Setenv("GITHUB_REPOSITORY", "acme/agent")
	t.Setenv("GITHUB_HEAD_REPO", "contrib/agent-fork")

	root := t.TempDir()
	configPath := filepath.Join(root, "evals", "gauntlet.yml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("version: 1\n"), 0o644); err != nil {
		t.Fatalf("write policy: %v", err)
	}

	cfg := runner.Config{
		ConfigPath: configPath,
		Mode:       "local",
		TUTConfig:  tutConfigForTest(),
	}
	_, err := startProxyForRun(&cfg, nil, "live", "")
	if err == nil {
		t.Fatal("expected live mode rejection in untrusted CI context")
	}
	if !strings.Contains(err.Error(), "untrusted CI context") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStartProxyForRun_ProxyPortClashCategorized(t *testing.T) {
	forceNonForkCIContext(t)

	root := t.TempDir()
	configPath := filepath.Join(root, "evals", "gauntlet.yml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("version: 1\n"), 0o644); err != nil {
		t.Fatalf("write policy: %v", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	cfg := runner.Config{
		ConfigPath: configPath,
		TUTConfig:  tutConfigForTest(),
	}
	resolved := &policy.Resolved{
		ModelMode: "recorded",
		ProxyAddr: ln.Addr().String(),
	}
	_, err = startProxyForRun(&cfg, resolved, "", "")
	if err == nil {
		t.Fatal("expected proxy startup failure for occupied address")
	}
	if !strings.Contains(err.Error(), "root_cause=port_clash") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStartProxyForRun_InvalidCAFilesCategorizedAsCertIssue(t *testing.T) {
	forceNonForkCIContext(t)

	root := t.TempDir()
	configPath := filepath.Join(root, "evals", "gauntlet.yml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("version: 1\n"), 0o644); err != nil {
		t.Fatalf("write policy: %v", err)
	}

	caDir := filepath.Join(filepath.Dir(configPath), ".gauntlet")
	if err := os.MkdirAll(caDir, 0o755); err != nil {
		t.Fatalf("mkdir ca dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(caDir, "ca.pem"), []byte("not-a-cert"), 0o644); err != nil {
		t.Fatalf("write ca.pem: %v", err)
	}
	if err := os.WriteFile(filepath.Join(caDir, "ca.key"), []byte("not-a-key"), 0o600); err != nil {
		t.Fatalf("write ca.key: %v", err)
	}

	cfg := runner.Config{
		ConfigPath: configPath,
		TUTConfig:  tutConfigForTest(),
	}
	resolved := &policy.Resolved{
		ModelMode: "recorded",
		ProxyAddr: "127.0.0.1:0",
	}
	_, err := startProxyForRun(&cfg, resolved, "", "")
	if err == nil {
		t.Fatal("expected proxy startup failure for malformed CA files")
	}
	if !strings.Contains(err.Error(), "root_cause=cert_issue") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStartProxyForRun_InsecureCAKeyPermissionsCategorizedAsCertIssue(t *testing.T) {
	forceNonForkCIContext(t)

	root := t.TempDir()
	configPath := filepath.Join(root, "evals", "gauntlet.yml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("version: 1\n"), 0o644); err != nil {
		t.Fatalf("write policy: %v", err)
	}

	caDir := filepath.Join(filepath.Dir(configPath), ".gauntlet")
	if _, err := proxy.GenerateCA(caDir); err != nil {
		t.Fatalf("generate ca: %v", err)
	}
	if err := os.Chmod(filepath.Join(caDir, "ca.key"), 0o644); err != nil {
		t.Fatalf("chmod ca.key: %v", err)
	}

	cfg := runner.Config{
		ConfigPath: configPath,
		TUTConfig:  tutConfigForTest(),
	}
	resolved := &policy.Resolved{
		ModelMode: "recorded",
		ProxyAddr: "127.0.0.1:0",
	}
	_, err := startProxyForRun(&cfg, resolved, "", "")
	if err == nil {
		t.Fatal("expected proxy startup failure for insecure CA key permissions")
	}
	if !strings.Contains(err.Error(), "root_cause=cert_issue") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "permissions") {
		t.Fatalf("expected permission context, got: %v", err)
	}
}

func tutConfigForTest() tut.Config {
	return tut.Config{
		Command: "python3",
		Env:     map[string]string{},
	}
}

func forceNonForkCIContext(t *testing.T) {
	t.Helper()
	t.Setenv("GITHUB_ACTIONS", "")
	t.Setenv("GITHUB_EVENT_NAME", "")
	t.Setenv("GITHUB_REPOSITORY", "")
	t.Setenv("GITHUB_HEAD_REPO", "")
	t.Setenv("GITHUB_EVENT_PATH", "")
}
