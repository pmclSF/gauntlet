package discovery

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pmclSF/gauntlet/internal/scenario"
)

func TestEnsureAutoSuite_GeneratesToolAndDBScenarios(t *testing.T) {
	root := t.TempDir()
	evals := filepath.Join(root, "evals")
	suiteDir := filepath.Join(evals, "smoke")
	toolsDir := filepath.Join(evals, "world", "tools")
	dbDir := filepath.Join(evals, "world", "databases")
	pairsDir := filepath.Join(evals, "pairs")
	agentDir := filepath.Join(root, "agent")
	mustMkdir(t, suiteDir)
	mustMkdir(t, toolsDir)
	mustMkdir(t, dbDir)
	mustMkdir(t, pairsDir)
	mustMkdir(t, agentDir)

	mustWrite(t, filepath.Join(toolsDir, "order_lookup.yaml"), `
tool: order_lookup
states:
  nominal:
    response: {status: "ok"}
  timeout:
    error: "timed out"
`)
	mustWrite(t, filepath.Join(dbDir, "orders_db.yaml"), `
database: orders_db
seed_sets:
  standard_order:
    tables: {}
  conflicting_state:
    tables: {}
`)
	mustWrite(t, filepath.Join(pairsDir, "order_lookup.yaml"), `
name: order_lookup_pairs
tool: order_lookup
pairs:
  - id: good_one
    description: good
    category: good
    input:
      order_id: "ord-001"
    output:
      status: "ok"
`)
	mustWrite(t, filepath.Join(agentDir, "tools.py"), `
import gauntlet

@gauntlet.tool(name="order_lookup")
def lookup(order_id):
    return {"status": "ok"}
`)

	proposalsPath := filepath.Join(evals, "proposals.yaml")
	res, err := EnsureAutoSuite(AutoSuiteConfig{
		RootDir:       root,
		EvalsDir:      evals,
		Suite:         "smoke",
		SuiteDir:      suiteDir,
		ToolsDir:      toolsDir,
		DBDir:         dbDir,
		PairsDir:      pairsDir,
		ProposalsPath: proposalsPath,
		PythonDirs:    []string{"agent"},
	})
	if err != nil {
		t.Fatalf("EnsureAutoSuite: %v", err)
	}
	proposals, err := LoadProposals(proposalsPath)
	if err != nil {
		t.Fatalf("LoadProposals: %v", err)
	}
	if res.GeneratedScenarios < 4 {
		t.Fatalf("GeneratedScenarios = %d, want >= 4", res.GeneratedScenarios)
	}
	if res.ProposalsCount < 4 {
		t.Fatalf("ProposalsCount = %d, want >= 4", res.ProposalsCount)
	}
	var foundDBSeed bool
	for _, p := range proposals {
		if p.Database == "orders_db" && p.SeedSet == "standard_order" {
			foundDBSeed = true
			break
		}
	}
	if !foundDBSeed {
		t.Fatal("expected DB seed proposal for orders_db/standard_order")
	}

	scenarios, err := scenario.LoadSuite(suiteDir)
	if err != nil {
		t.Fatalf("LoadSuite generated scenarios: %v", err)
	}
	var foundToolScenario, foundDBScenario bool
	for _, s := range scenarios {
		if strings.HasPrefix(s.Name, "auto_tool_order_lookup_") {
			foundToolScenario = true
		}
		if strings.HasPrefix(s.Name, "auto_db_orders_db_") {
			foundDBScenario = true
		}
	}
	if !foundToolScenario {
		t.Fatal("missing generated tool scenario")
	}
	if !foundDBScenario {
		t.Fatal("missing generated DB scenario")
	}
}

func TestEnsureAutoSuite_SkipsWhenManualScenariosPresent(t *testing.T) {
	root := t.TempDir()
	evals := filepath.Join(root, "evals")
	suiteDir := filepath.Join(evals, "smoke")
	toolsDir := filepath.Join(evals, "world", "tools")
	dbDir := filepath.Join(evals, "world", "databases")
	mustMkdir(t, suiteDir)
	mustMkdir(t, toolsDir)
	mustMkdir(t, dbDir)

	mustWrite(t, filepath.Join(toolsDir, "order_lookup.yaml"), `
tool: order_lookup
states:
  nominal:
    response: {status: "ok"}
`)
	mustWrite(t, filepath.Join(dbDir, "orders_db.yaml"), `
database: orders_db
seed_sets:
  standard_order:
    tables: {}
`)
	mustWrite(t, filepath.Join(suiteDir, "manual.yaml"), `
scenario: manual_case
description: manual
input:
  messages:
    - role: user
      content: "hello"
world: {}
assertions:
  - type: sensitive_leak
`)

	res, err := EnsureAutoSuite(AutoSuiteConfig{
		RootDir:    root,
		EvalsDir:   evals,
		Suite:      "smoke",
		SuiteDir:   suiteDir,
		ToolsDir:   toolsDir,
		DBDir:      dbDir,
		PythonDirs: []string{"."},
	})
	if err != nil {
		t.Fatalf("EnsureAutoSuite: %v", err)
	}
	if !res.Skipped {
		t.Fatal("expected auto generation to be skipped with manual scenarios")
	}
	if res.GeneratedScenarios != 0 {
		t.Fatalf("GeneratedScenarios = %d, want 0", res.GeneratedScenarios)
	}
	if _, err := os.Stat(filepath.Join(suiteDir, "manual.yaml")); err != nil {
		t.Fatalf("manual scenario should remain: %v", err)
	}
}

func TestEnsureAutoSuite_ForceOverridesSkip(t *testing.T) {
	root := t.TempDir()
	evals := filepath.Join(root, "evals")
	suiteDir := filepath.Join(evals, "smoke")
	toolsDir := filepath.Join(evals, "world", "tools")
	dbDir := filepath.Join(evals, "world", "databases")
	mustMkdir(t, suiteDir)
	mustMkdir(t, toolsDir)
	mustMkdir(t, dbDir)

	mustWrite(t, filepath.Join(toolsDir, "order_lookup.yaml"), `
tool: order_lookup
states:
  nominal:
    response: {status: "ok"}
`)
	// Write a manual scenario — normally this would cause skip.
	mustWrite(t, filepath.Join(suiteDir, "manual.yaml"), `
scenario: manual_case
description: manual
input:
  messages:
    - role: user
      content: "hello"
world: {}
assertions:
  - type: sensitive_leak
`)

	res, err := EnsureAutoSuite(AutoSuiteConfig{
		RootDir:    root,
		EvalsDir:   evals,
		Suite:      "smoke",
		SuiteDir:   suiteDir,
		ToolsDir:   toolsDir,
		DBDir:      dbDir,
		PythonDirs: []string{"."},
		Force:      true,
	})
	if err != nil {
		t.Fatalf("EnsureAutoSuite: %v", err)
	}
	if res.Skipped {
		t.Fatal("expected Force=true to override manual scenario skip")
	}
	if res.GeneratedScenarios == 0 {
		t.Fatal("expected scenarios to be generated with Force=true")
	}
	// Manual file should still exist.
	if _, err := os.Stat(filepath.Join(suiteDir, "manual.yaml")); err != nil {
		t.Fatal("manual scenario should not be deleted")
	}
}

func TestEnsureAutoSuite_StalenessSkip(t *testing.T) {
	root := t.TempDir()
	evals := filepath.Join(root, "evals")
	suiteDir := filepath.Join(evals, "smoke")
	toolsDir := filepath.Join(evals, "world", "tools")
	dbDir := filepath.Join(evals, "world", "databases")
	mustMkdir(t, suiteDir)
	mustMkdir(t, toolsDir)
	mustMkdir(t, dbDir)

	mustWrite(t, filepath.Join(toolsDir, "order_lookup.yaml"), `
tool: order_lookup
states:
  nominal:
    response: {status: "ok"}
`)

	cfg := AutoSuiteConfig{
		RootDir:    root,
		EvalsDir:   evals,
		Suite:      "smoke",
		SuiteDir:   suiteDir,
		ToolsDir:   toolsDir,
		DBDir:      dbDir,
		PythonDirs: []string{"."},
	}

	// First run should generate.
	res1, err := EnsureAutoSuite(cfg)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	if res1.Skipped {
		t.Fatal("first run should not be skipped")
	}
	if res1.GeneratedScenarios == 0 {
		t.Fatal("first run should generate scenarios")
	}

	// Second run should skip (inputs unchanged).
	res2, err := EnsureAutoSuite(cfg)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if !res2.Skipped {
		t.Fatal("second run should be skipped (up to date)")
	}
	if !strings.Contains(res2.SkipReason, "up to date") {
		t.Fatalf("expected 'up to date' skip reason, got: %s", res2.SkipReason)
	}

	// Force should override staleness.
	cfg.Force = true
	res3, err := EnsureAutoSuite(cfg)
	if err != nil {
		t.Fatalf("force run: %v", err)
	}
	if res3.Skipped {
		t.Fatal("force run should not be skipped")
	}
}

func TestEnsureAutoSuite_AutoGeneratedHeader(t *testing.T) {
	root := t.TempDir()
	evals := filepath.Join(root, "evals")
	suiteDir := filepath.Join(evals, "smoke")
	toolsDir := filepath.Join(evals, "world", "tools")
	mustMkdir(t, suiteDir)
	mustMkdir(t, toolsDir)

	mustWrite(t, filepath.Join(toolsDir, "weather.yaml"), `
tool: get_weather
states:
  nominal:
    response: {temp: 72}
`)

	res, err := EnsureAutoSuite(AutoSuiteConfig{
		RootDir:    root,
		EvalsDir:   evals,
		Suite:      "smoke",
		SuiteDir:   suiteDir,
		ToolsDir:   toolsDir,
		PythonDirs: []string{"."},
	})
	if err != nil {
		t.Fatalf("EnsureAutoSuite: %v", err)
	}
	if res.GeneratedScenarios == 0 {
		t.Fatal("expected at least one scenario")
	}

	// Verify all generated files have the header.
	entries, _ := os.ReadDir(suiteDir)
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(suiteDir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		if !strings.HasPrefix(string(data), "# gauntlet:auto-generated") {
			t.Errorf("file %s missing auto-generated header", e.Name())
		}
	}

	// Verify all generated files pass scenario.LoadFile validation.
	scenarios, err := scenario.LoadSuite(suiteDir)
	if err != nil {
		t.Fatalf("LoadSuite: %v", err)
	}
	if len(scenarios) == 0 {
		t.Fatal("LoadSuite returned no scenarios")
	}
}

func TestEnsureAutoSuite_GeneratedScenariosAreSchemaValid(t *testing.T) {
	root := t.TempDir()
	evals := filepath.Join(root, "evals")
	suiteDir := filepath.Join(evals, "smoke")
	toolsDir := filepath.Join(evals, "world", "tools")
	dbDir := filepath.Join(evals, "world", "databases")
	pairsDir := filepath.Join(evals, "pairs")
	mustMkdir(t, suiteDir)
	mustMkdir(t, toolsDir)
	mustMkdir(t, dbDir)
	mustMkdir(t, pairsDir)

	mustWrite(t, filepath.Join(toolsDir, "order_lookup.yaml"), `
tool: order_lookup
states:
  nominal:
    response: {status: "ok"}
  timeout:
    error: "timed out"
`)
	mustWrite(t, filepath.Join(toolsDir, "refund.yaml"), `
tool: process_refund
states:
  nominal:
    response: {refunded: true}
`)
	mustWrite(t, filepath.Join(dbDir, "orders_db.yaml"), `
database: orders_db
seed_sets:
  standard_order:
    tables: {}
`)
	mustWrite(t, filepath.Join(pairsDir, "order_lookup.yaml"), `
name: order_lookup_pairs
tool: order_lookup
pairs:
  - id: good
    description: good
    category: good
    input:
      order_id: "ord-001"
    output:
      status: "ok"
`)

	_, err := EnsureAutoSuite(AutoSuiteConfig{
		RootDir:    root,
		EvalsDir:   evals,
		Suite:      "smoke",
		SuiteDir:   suiteDir,
		ToolsDir:   toolsDir,
		DBDir:      dbDir,
		PairsDir:   pairsDir,
		PythonDirs: []string{"."},
	})
	if err != nil {
		t.Fatalf("EnsureAutoSuite: %v", err)
	}

	// Every generated YAML must individually pass LoadFile.
	entries, _ := os.ReadDir(suiteDir)
	count := 0
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		path := filepath.Join(suiteDir, e.Name())
		if _, err := scenario.LoadFile(path); err != nil {
			t.Errorf("generated scenario %s failed validation: %v", e.Name(), err)
		}
		count++
	}
	if count == 0 {
		t.Fatal("no scenario files found")
	}
}

func TestEnsureAutoSuite_ManualScenariosNeverDeleted(t *testing.T) {
	root := t.TempDir()
	evals := filepath.Join(root, "evals")
	suiteDir := filepath.Join(evals, "smoke")
	toolsDir := filepath.Join(evals, "world", "tools")
	mustMkdir(t, suiteDir)
	mustMkdir(t, toolsDir)

	mustWrite(t, filepath.Join(toolsDir, "weather.yaml"), `
tool: get_weather
states:
  nominal:
    response: {temp: 72}
`)

	// Create a manual scenario (no auto header, no auto_ prefix).
	mustWrite(t, filepath.Join(suiteDir, "my_handwritten_test.yaml"), `
scenario: my_handwritten_test
description: my handwritten test
input:
  messages:
    - role: user
      content: "hello"
world: {}
assertions:
  - type: sensitive_leak
`)

	// Force=true should still not delete manual scenarios.
	_, err := EnsureAutoSuite(AutoSuiteConfig{
		RootDir:    root,
		EvalsDir:   evals,
		Suite:      "smoke",
		SuiteDir:   suiteDir,
		ToolsDir:   toolsDir,
		PythonDirs: []string{"."},
		Force:      true,
	})
	if err != nil {
		t.Fatalf("EnsureAutoSuite: %v", err)
	}

	// Manual file must still exist.
	if _, err := os.Stat(filepath.Join(suiteDir, "my_handwritten_test.yaml")); err != nil {
		t.Fatal("manual scenario was deleted")
	}
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
