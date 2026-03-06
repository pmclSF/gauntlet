package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/pmclSF/gauntlet/internal/fixture"
	"github.com/pmclSF/gauntlet/internal/tut"
)

func withWorkingDir(t *testing.T, dir string) {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
}

func setupMinimalProject(t *testing.T, withTUT bool) (string, string) {
	t.Helper()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "evals", "smoke"), 0o755); err != nil {
		t.Fatalf("mkdir smoke: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "evals", "world", "tools"), 0o755); err != nil {
		t.Fatalf("mkdir tools: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "evals", "world", "databases"), 0o755); err != nil {
		t.Fatalf("mkdir dbs: %v", err)
	}

	scenarioYAML := `scenario: smoke_pass
input:
  messages:
    - role: user
      content: hello
assertions: []
`
	if err := os.WriteFile(filepath.Join(root, "evals", "smoke", "smoke_pass.yaml"), []byte(scenarioYAML), 0o644); err != nil {
		t.Fatalf("write scenario: %v", err)
	}

	var scriptPath string
	if withTUT {
		scriptPath = filepath.Join(root, "agent.sh")
		script := `#!/bin/sh
cat >/dev/null
echo '{"ok":true}'
if [ -n "${GAUNTLET_TRACE_FILE:-}" ]; then
  printf '%s\n' '{"gauntlet_event":true,"type":"tool_call","timestamp":1,"tool_name":"noop","args":{},"result":{}}' >> "$GAUNTLET_TRACE_FILE"
fi
`
		if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
			t.Fatalf("write agent script: %v", err)
		}
	}

	var policy strings.Builder
	policy.WriteString("version: 1\n")
	policy.WriteString("suites:\n")
	policy.WriteString("  smoke:\n")
	policy.WriteString("    scenarios: \"evals/smoke/*.yaml\"\n")
	if withTUT {
		policy.WriteString("tut:\n")
		policy.WriteString("  adapter: cli\n")
		policy.WriteString(fmt.Sprintf("  command: %q\n", scriptPath))
		policy.WriteString(fmt.Sprintf("  work_dir: %q\n", root))
	}
	configPath := filepath.Join(root, "evals", "gauntlet.yml")
	if err := os.WriteFile(configPath, []byte(policy.String()), 0o644); err != nil {
		t.Fatalf("write policy: %v", err)
	}
	return root, configPath
}

func TestRunCmd_PassesMinimalSuite(t *testing.T) {
	root, _ := setupMinimalProject(t, false)
	withWorkingDir(t, root)

	prevVerbose, prevQuiet, prevJSON := flagVerbose, flagQuiet, flagJSON
	t.Cleanup(func() {
		flagVerbose = prevVerbose
		flagQuiet = prevQuiet
		flagJSON = prevJSON
	})
	flagVerbose, flagQuiet, flagJSON = false, false, false

	var out bytes.Buffer
	cmd := newRunCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"--suite", "smoke",
		"--config", "evals/gauntlet.yml",
		"--auto-discover=false",
		"--output-dir", "evals/runs/manual",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("run command failed: %v\noutput:\n%s", err, out.String())
	}

	if _, err := os.Stat(filepath.Join(root, "evals", "runs", "manual", "results.json")); err != nil {
		t.Fatalf("expected results.json output: %v", err)
	}
}

func TestBaselineCmd_GeneratesBaselineForPassingScenario(t *testing.T) {
	root, _ := setupMinimalProject(t, true)
	withWorkingDir(t, root)

	var out bytes.Buffer
	cmd := newBaselineCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"--suite", "smoke",
		"--config", "evals/gauntlet.yml",
		"--model-mode", "passthrough",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("baseline command failed: %v\noutput:\n%s", err, out.String())
	}

	baselinePath := filepath.Join(root, "evals", "baselines", "smoke", "smoke_pass.json")
	if _, err := os.Stat(baselinePath); err != nil {
		t.Fatalf("expected baseline output %s: %v", baselinePath, err)
	}
}

func TestRecordCmd_RecordsSuiteAndGeneratesSigningArtifacts(t *testing.T) {
	root, _ := setupMinimalProject(t, true)
	withWorkingDir(t, root)

	var out bytes.Buffer
	cmd := newRecordCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"--suite", "smoke",
		"--config", "evals/gauntlet.yml",
		"--proxy-addr", "localhost:0",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("record command failed: %v\noutput:\n%s", err, out.String())
	}

	signingKey := filepath.Join(root, "evals", ".gauntlet", "fixture-signing-key.pem")
	if _, err := os.Stat(signingKey); err != nil {
		t.Fatalf("expected fixture signing key %s: %v", signingKey, err)
	}
	lockfile := filepath.Join(root, "evals", "fixtures", fixture.DefaultReplayLockfileName)
	if _, err := os.Stat(lockfile); err != nil {
		t.Fatalf("expected replay lockfile %s: %v", lockfile, err)
	}
}

func TestPromptAndTraceHelpers(t *testing.T) {
	choice, err := promptChoice(bufio.NewReader(strings.NewReader("\n")), io.Discard, []string{"one", "two"}, 0)
	if err != nil {
		t.Fatalf("promptChoice default: %v", err)
	}
	if choice != "one" {
		t.Fatalf("default choice = %q, want one", choice)
	}

	choice, err = promptChoice(bufio.NewReader(strings.NewReader("2\n")), io.Discard, []string{"one", "two"}, 0)
	if err != nil {
		t.Fatalf("promptChoice numeric: %v", err)
	}
	if choice != "two" {
		t.Fatalf("numeric choice = %q, want two", choice)
	}

	text, err := promptText(bufio.NewReader(strings.NewReader("\n")), io.Discard, "default")
	if err != nil {
		t.Fatalf("promptText default: %v", err)
	}
	if text != "default" {
		t.Fatalf("promptText default = %q, want default", text)
	}

	events := normalizeTraceEvents([]interface{}{
		map[string]interface{}{"type": "tool_call", "tool_name": "order_lookup"},
		"skip",
		map[string]interface{}{"type": "model_call"},
	})
	if len(events) != 2 {
		t.Fatalf("normalizeTraceEvents len = %d, want 2", len(events))
	}

	lines := splitLines(" one \n\n two \n")
	if !reflect.DeepEqual(lines, []string{"one", "two"}) {
		t.Fatalf("splitLines = %v", lines)
	}
}

func TestBaselinePolicyAndSetupHelpers(t *testing.T) {
	t.Setenv("GITHUB_BASE_REF", "main")
	if got := resolveBaseRef(""); got != "origin/main" {
		t.Fatalf("resolveBaseRef(env) = %q, want origin/main", got)
	}
	if got := resolveBaseRef("origin/custom"); got != "origin/custom" {
		t.Fatalf("resolveBaseRef(explicit) = %q, want origin/custom", got)
	}
	if _, err := gitDiffChangedFiles("", "HEAD", "evals/baselines"); err == nil {
		t.Fatal("expected gitDiffChangedFiles to reject empty base ref")
	}

	if _, ok := selectAdapter(tut.Config{Adapter: "http"}).(*tut.HTTPAdapter); !ok {
		t.Fatal("selectAdapter(http) should return HTTPAdapter")
	}
	if got := selectAdapter(tut.Config{Adapter: "minimal"}).Level(); got != tut.LevelMinimal {
		t.Fatalf("selectAdapter(minimal) level = %q, want minimal", got)
	}
	if got := selectAdapter(tut.Config{Adapter: "cli"}).Level(); got != tut.LevelGood {
		t.Fatalf("selectAdapter(cli) level = %q, want good", got)
	}

	t.Setenv("GAUNTLET_TRUSTED_RECORDER_IDENTITIES", "Alice,bob,alice, BOB ")
	ids := trustedRecorderIdentitiesFromEnv()
	if !reflect.DeepEqual(ids, []string{"Alice", "bob"}) {
		t.Fatalf("trustedRecorderIdentitiesFromEnv = %v, want [Alice bob]", ids)
	}

	if ui := getEmbeddedUI(); ui == nil {
		t.Fatal("expected embedded UI filesystem to be available")
	}
}

func TestValidatePathAndInteractiveHelpers(t *testing.T) {
	root := t.TempDir()
	explicitSuiteDir := filepath.Join(root, "explicit-suite")
	if err := os.MkdirAll(explicitSuiteDir, 0o755); err != nil {
		t.Fatalf("mkdir explicit suite: %v", err)
	}

	if got := resolveSuitePathForValidate(explicitSuiteDir, filepath.Join(root, "evals")); got != explicitSuiteDir {
		t.Fatalf("resolveSuitePathForValidate(dir) = %q, want %q", got, explicitSuiteDir)
	}
	if got := resolveSuitePathForValidate("", filepath.Join(root, "evals")); got != filepath.Join(root, "evals", "smoke") {
		t.Fatalf("resolveSuitePathForValidate(default) = %q", got)
	}

	if isInteractiveInput(bytes.NewBufferString("not a tty")) {
		t.Fatal("isInteractiveInput should be false for non-file readers")
	}
}
