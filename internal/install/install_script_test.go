package install_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to resolve runtime caller path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
}

func TestInstallScript_EnforcesChecksumVerification(t *testing.T) {
	root := repositoryRoot(t)
	scriptPath := filepath.Join(root, "install.sh")
	data, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("read install.sh: %v", err)
	}
	script := string(data)

	requiredSnippets := []string{
		"checksums.txt",
		"checksum mismatch",
		"EXPECTED_SHA",
		"ACTUAL_SHA",
	}
	for _, snippet := range requiredSnippets {
		if !strings.Contains(script, snippet) {
			t.Fatalf("install.sh missing required checksum verification snippet: %q", snippet)
		}
	}
}

func TestCIWorkflow_VerifiesInstallEndpoint(t *testing.T) {
	root := repositoryRoot(t)
	workflowPath := filepath.Join(root, ".github", "workflows", "gauntlet.yml")
	data, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("read workflow: %v", err)
	}
	workflow := string(data)

	if !strings.Contains(workflow, "Verify install endpoint") {
		t.Fatalf("workflow missing install endpoint verification step")
	}
	if !strings.Contains(workflow, "https://gauntlet.dev/install.sh") {
		t.Fatalf("workflow must check gauntlet.dev install endpoint")
	}
}
