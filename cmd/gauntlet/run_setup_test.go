package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gauntlet-dev/gauntlet/internal/policy"
	"github.com/gauntlet-dev/gauntlet/internal/proxy"
	"github.com/gauntlet-dev/gauntlet/internal/runner"
	"github.com/gauntlet-dev/gauntlet/internal/tut"
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
		in      string
		want    proxy.Mode
		wantErr bool
	}{
		{in: "", want: proxy.ModeRecorded},
		{in: "recorded", want: proxy.ModeRecorded},
		{in: "live", want: proxy.ModeLive},
		{in: "passthrough", want: proxy.ModePassthrough},
		{in: "invalid", wantErr: true},
	}
	for _, tt := range tests {
		got, err := parseProxyMode(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Fatalf("parseProxyMode(%q): expected error", tt.in)
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
		TUT: policy.TUTConfig{
			Adapter: "cli",
			Command: "python3",
			Args:    []string{"-m", "agent.main"},
			WorkDir: root,
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
}

func TestEffectiveModelModePrecedence(t *testing.T) {
	t.Setenv("GAUNTLET_MODEL_MODE", "live")
	resolved := &policy.Resolved{ModelMode: "recorded"}

	if got := effectiveModelMode("passthrough", resolved); got != "passthrough" {
		t.Fatalf("override precedence failed: got %q", got)
	}
	if got := effectiveModelMode("", resolved); got != "live" {
		t.Fatalf("env precedence failed: got %q", got)
	}
	t.Setenv("GAUNTLET_MODEL_MODE", "")
	if got := effectiveModelMode("", resolved); got != "recorded" {
		t.Fatalf("policy precedence failed: got %q", got)
	}
	if got := effectiveModelMode("", nil); got != "recorded" {
		t.Fatalf("default mode failed: got %q", got)
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
	if got := cfg.TUTConfig.Env["GAUNTLET_FIXTURE_DIR"]; !strings.HasSuffix(got, "/evals/fixtures/tools") {
		t.Fatalf("GAUNTLET_FIXTURE_DIR = %q", got)
	}
}

func TestStartProxyForRun_InvalidModelModeFails(t *testing.T) {
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

func tutConfigForTest() tut.Config {
	return tut.Config{
		Command: "python3",
		Env:     map[string]string{},
	}
}
