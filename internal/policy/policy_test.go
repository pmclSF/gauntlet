package policy

import (
	"os"
	"path/filepath"
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
	if resolved.ProxyAddr != "localhost:7431" {
		t.Fatalf("ProxyAddr = %q, want localhost:7431", resolved.ProxyAddr)
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
