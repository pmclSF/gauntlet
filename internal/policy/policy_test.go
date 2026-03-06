package policy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad_TemplateStylePolicy(t *testing.T) {
	root := t.TempDir()
	evalsDir := filepath.Join(root, "evals")
	mustMkdir(t, filepath.Join(evalsDir, "smoke"))
	mustMkdir(t, filepath.Join(evalsDir, "world", "tools"))
	mustMkdir(t, filepath.Join(evalsDir, "world", "databases"))
	mustMkdir(t, filepath.Join(evalsDir, "fixtures"))
	mustMkdir(t, filepath.Join(evalsDir, "baselines", "smoke"))

	policyPath := filepath.Join(evalsDir, "gauntlet.yml")
	mustWriteFile(t, policyPath, `
version: 1
suites:
  smoke:
    scenarios: "evals/smoke/*.yaml"
    budget_ms: 45000
    mode: pr_ci
proxy:
  addr: "127.0.0.1:7431"
`)

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir root: %v", err)
	}

	resolved, err := Load(policyPath, "smoke")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if resolved.BudgetMs != 45000 {
		t.Fatalf("BudgetMs = %d, want 45000", resolved.BudgetMs)
	}
	if resolved.RunnerMode != "pr_ci" {
		t.Fatalf("RunnerMode = %q, want pr_ci", resolved.RunnerMode)
	}
	if resolved.ModelMode != "" {
		t.Fatalf("ModelMode = %q, want empty", resolved.ModelMode)
	}
	if resolved.SuiteDir != filepath.Join(evalsDir, "smoke") {
		assertSamePath(t, resolved.SuiteDir, filepath.Join(evalsDir, "smoke"), "SuiteDir")
	}
	if resolved.ToolsDir != filepath.Join(evalsDir, "world", "tools") {
		assertSamePath(t, resolved.ToolsDir, filepath.Join(evalsDir, "world", "tools"), "ToolsDir")
	}
	if resolved.BaselineDir != filepath.Join(evalsDir, "baselines") {
		assertSamePath(t, resolved.BaselineDir, filepath.Join(evalsDir, "baselines"), "BaselineDir")
	}
	if resolved.ProxyAddr != "127.0.0.1:7431" {
		t.Fatalf("ProxyAddr = %q, want 127.0.0.1:7431", resolved.ProxyAddr)
	}
	if len(resolved.AssertionMode.HardGates) != 0 || len(resolved.AssertionMode.SoftSignals) != 0 {
		t.Fatalf("expected empty assertion mode when policy omits assertions block, got %+v", resolved.AssertionMode)
	}
	if !resolved.PromptInjectionDenylist {
		t.Fatalf("PromptInjectionDenylist = %t, want true", resolved.PromptInjectionDenylist)
	}
}

func TestLoad_ExampleStylePolicy(t *testing.T) {
	root := t.TempDir()
	evalsDir := filepath.Join(root, "evals")
	mustMkdir(t, filepath.Join(evalsDir, "smoke"))
	mustMkdir(t, filepath.Join(evalsDir, "world", "tools"))
	mustMkdir(t, filepath.Join(evalsDir, "world", "databases"))
	mustMkdir(t, filepath.Join(evalsDir, "fixtures"))
	mustMkdir(t, filepath.Join(evalsDir, "baselines", "smoke"))

	policyPath := filepath.Join(evalsDir, "gauntlet.yml")
	mustWriteFile(t, policyPath, `
version: 1
suite: smoke
tut:
  type: cli
  command: "python3 -m agent.main"
  working_dir: "."
defaults:
  budget_ms: 30000
  mode: recorded
scenarios_dir: evals/smoke
world_dir: evals/world
fixtures_dir: evals/fixtures
baselines_dir: evals/baselines/smoke
`)

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir root: %v", err)
	}

	resolved, err := Load(policyPath, "smoke")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if resolved.BudgetMs != 30000 {
		t.Fatalf("BudgetMs = %d, want 30000", resolved.BudgetMs)
	}
	if resolved.RunnerMode != "" {
		t.Fatalf("RunnerMode = %q, want empty", resolved.RunnerMode)
	}
	if resolved.ModelMode != "recorded" {
		t.Fatalf("ModelMode = %q, want recorded", resolved.ModelMode)
	}
	if resolved.SuiteDir != filepath.Join(evalsDir, "smoke") {
		assertSamePath(t, resolved.SuiteDir, filepath.Join(evalsDir, "smoke"), "SuiteDir")
	}
	if resolved.BaselineDir != filepath.Join(evalsDir, "baselines") {
		assertSamePath(t, resolved.BaselineDir, filepath.Join(evalsDir, "baselines"), "BaselineDir")
	}
	if resolved.TUT.Adapter != "cli" {
		t.Fatalf("TUT.Adapter = %q, want cli", resolved.TUT.Adapter)
	}
	if resolved.TUT.Command != "python3" {
		t.Fatalf("TUT.Command = %q, want python3", resolved.TUT.Command)
	}
	if len(resolved.TUT.Args) != 2 || resolved.TUT.Args[0] != "-m" || resolved.TUT.Args[1] != "agent.main" {
		t.Fatalf("TUT.Args = %v, want [-m agent.main]", resolved.TUT.Args)
	}
	if resolved.TUT.WorkDir != root {
		assertSamePath(t, resolved.TUT.WorkDir, root, "TUT.WorkDir")
	}
	if resolved.ProxyAddr != "localhost:0" {
		t.Fatalf("ProxyAddr = %q, want localhost:0", resolved.ProxyAddr)
	}
}

func TestLoad_TUTResourceLimitsParsed(t *testing.T) {
	root := t.TempDir()
	policyPath := filepath.Join(root, "evals", "gauntlet.yml")
	mustMkdir(t, filepath.Dir(policyPath))
	mustWriteFile(t, policyPath, `
version: 1
tut:
  adapter: cli
  command: "python3 main.py"
  resource_limits:
    cpu_seconds: 8
    memory_mb: 256
    open_files: 512
`)

	resolved, err := Load(policyPath, "smoke")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := resolved.TUT.ResourceLimits.CPUSeconds; got != 8 {
		t.Fatalf("cpu_seconds = %d, want 8", got)
	}
	if got := resolved.TUT.ResourceLimits.MemoryMB; got != 256 {
		t.Fatalf("memory_mb = %d, want 256", got)
	}
	if got := resolved.TUT.ResourceLimits.OpenFiles; got != 512 {
		t.Fatalf("open_files = %d, want 512", got)
	}
}

func TestLoad_TUTGuardrailsParsed(t *testing.T) {
	root := t.TempDir()
	policyPath := filepath.Join(root, "evals", "gauntlet.yml")
	mustMkdir(t, filepath.Dir(policyPath))
	mustWriteFile(t, policyPath, `
version: 1
tut:
  adapter: cli
  command: "python3 main.py"
  guardrails:
    hostile_payload: true
    max_processes: 32
`)

	resolved, err := Load(policyPath, "smoke")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !resolved.TUT.Guardrails.HostilePayload {
		t.Fatal("expected hostile_payload=true")
	}
	if got := resolved.TUT.Guardrails.MaxProcesses; got != 32 {
		t.Fatalf("max_processes = %d, want 32", got)
	}
}

func TestLoad_AssertionModeParsed(t *testing.T) {
	root := t.TempDir()
	policyPath := filepath.Join(root, "evals", "gauntlet.yml")
	mustMkdir(t, filepath.Dir(policyPath))
	mustWriteFile(t, policyPath, `
version: 1
assertions:
  hard_gates: [retry_cap, output_schema, retry_cap]
  soft_signals: [sensitive_leak, output_derivable]
`)

	resolved, err := Load(policyPath, "smoke")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got, want := strings.Join(resolved.AssertionMode.HardGates, ","), "output_schema,retry_cap"; got != want {
		t.Fatalf("hard gates = %q, want %q", got, want)
	}
	if got, want := strings.Join(resolved.AssertionMode.SoftSignals, ","), "output_derivable,sensitive_leak"; got != want {
		t.Fatalf("soft signals = %q, want %q", got, want)
	}
}

func TestLoad_RunnerFailFastParsed(t *testing.T) {
	root := t.TempDir()
	policyPath := filepath.Join(root, "evals", "gauntlet.yml")
	mustMkdir(t, filepath.Dir(policyPath))
	mustWriteFile(t, policyPath, `
version: 1
runner:
  fail_fast: true
`)

	resolved, err := Load(policyPath, "smoke")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !resolved.FailFast {
		t.Fatal("FailFast = false, want true")
	}
}

func TestLoad_AssertionModeRejectsUnknownKey(t *testing.T) {
	root := t.TempDir()
	policyPath := filepath.Join(root, "evals", "gauntlet.yml")
	mustMkdir(t, filepath.Dir(policyPath))
	mustWriteFile(t, policyPath, `
version: 1
assertions:
  hard_gates: [output_schema]
  unexpected: [retry_cap]
`)

	_, err := Load(policyPath, "smoke")
	if err == nil {
		t.Fatal("expected unknown assertions key error")
	}
	if !strings.Contains(err.Error(), "unknown key(s) under assertions") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoad_AssertionModeRejectsUnknownAssertionType(t *testing.T) {
	root := t.TempDir()
	policyPath := filepath.Join(root, "evals", "gauntlet.yml")
	mustMkdir(t, filepath.Dir(policyPath))
	mustWriteFile(t, policyPath, `
version: 1
assertions:
  hard_gates: [output_schema, not_real]
`)

	_, err := Load(policyPath, "smoke")
	if err == nil {
		t.Fatal("expected unknown assertion type error")
	}
	if !strings.Contains(err.Error(), `unknown assertion type "not_real"`) &&
		!strings.Contains(err.Error(), "must be one of the following") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoad_AssertionModeRejectsOverlap(t *testing.T) {
	root := t.TempDir()
	policyPath := filepath.Join(root, "evals", "gauntlet.yml")
	mustMkdir(t, filepath.Dir(policyPath))
	mustWriteFile(t, policyPath, `
version: 1
assertions:
  hard_gates: [output_schema]
  soft_signals: [output_schema]
`)

	_, err := Load(policyPath, "smoke")
	if err == nil {
		t.Fatal("expected hard/soft overlap error")
	}
	if !strings.Contains(err.Error(), "both hard_gates and soft_signals") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoad_NonStrictAllowsUnknownTopLevelKey(t *testing.T) {
	root := t.TempDir()
	policyPath := filepath.Join(root, "evals", "gauntlet.yml")
	mustMkdir(t, filepath.Dir(policyPath))
	mustWriteFile(t, policyPath, `
version: 1
unknown_setting: true
suites:
  smoke:
    scenarios: "evals/smoke/*.yaml"
`)

	_, err := LoadWithOptions(policyPath, "smoke", LoadOptions{Strict: false})
	if err != nil {
		t.Fatalf("LoadWithOptions strict=false should ignore unknown keys, got: %v", err)
	}
}

func TestLoad_StrictRejectsUnknownTopLevelKeyWithLine(t *testing.T) {
	root := t.TempDir()
	policyPath := filepath.Join(root, "evals", "gauntlet.yml")
	mustMkdir(t, filepath.Dir(policyPath))
	mustWriteFile(t, policyPath, `
version: 1
unknown_setting: true
suites:
  smoke:
    scenarios: "evals/smoke/*.yaml"
`)

	_, err := LoadWithOptions(policyPath, "smoke", LoadOptions{Strict: true})
	if err == nil {
		t.Fatal("expected strict unknown key error")
	}
	if !strings.Contains(err.Error(), "unknown_setting") {
		t.Fatalf("expected unknown key name in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "line") {
		t.Fatalf("expected line diagnostics in error, got: %v", err)
	}
}

func TestLoad_SchemaValidationIncludesFieldAndLine(t *testing.T) {
	root := t.TempDir()
	policyPath := filepath.Join(root, "evals", "gauntlet.yml")
	mustMkdir(t, filepath.Dir(policyPath))
	mustWriteFile(t, policyPath, `version: 1
suites:
  smoke:
    scenarios: "evals/smoke/*.yaml"
    budget_ms: "bad"
`)

	_, err := Load(policyPath, "smoke")
	if err == nil {
		t.Fatal("expected schema validation failure for budget_ms type")
	}
	if !strings.Contains(err.Error(), "root.suites.smoke.budget_ms") {
		t.Fatalf("expected precise field path in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "line") {
		t.Fatalf("expected line diagnostics in error, got: %v", err)
	}
}

func TestLoad_ProxyModeRejectsRunnerMode(t *testing.T) {
	root := t.TempDir()
	policyPath := filepath.Join(root, "evals", "gauntlet.yml")
	mustMkdir(t, filepath.Dir(policyPath))
	mustWriteFile(t, policyPath, `
version: 1
proxy:
  mode: pr_ci
`)

	_, err := Load(policyPath, "smoke")
	if err == nil {
		t.Fatal("expected proxy.mode validation failure")
	}
	if !strings.Contains(err.Error(), "proxy.mode") {
		t.Fatalf("expected proxy.mode guidance in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "runner mode") && !strings.Contains(err.Error(), "must be one of the following") {
		t.Fatalf("expected runner-mode guidance in error, got: %v", err)
	}
}

func TestLoad_RedactionPromptInjectionDenylistOptOut(t *testing.T) {
	root := t.TempDir()
	policyPath := filepath.Join(root, "evals", "gauntlet.yml")
	mustMkdir(t, filepath.Dir(policyPath))
	mustWriteFile(t, policyPath, `
version: 1
redaction:
  prompt_injection_denylist: false
`)

	resolved, err := Load(policyPath, "smoke")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if resolved.PromptInjectionDenylist {
		t.Fatalf("PromptInjectionDenylist = %t, want false", resolved.PromptInjectionDenylist)
	}
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
}

func mustWriteFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func assertSamePath(t *testing.T, got, want, field string) {
	t.Helper()
	gotResolved := mustResolvePath(t, got)
	wantResolved := mustResolvePath(t, want)
	if gotResolved != wantResolved {
		t.Fatalf("%s = %q, want %q", field, gotResolved, wantResolved)
	}
}

func mustResolvePath(t *testing.T, path string) string {
	t.Helper()
	evaluated, err := filepath.EvalSymlinks(path)
	if err != nil {
		evaluated = path
	}
	abs, err := filepath.Abs(evaluated)
	if err != nil {
		t.Fatalf("abs path %s: %v", path, err)
	}
	return filepath.Clean(abs)
}
