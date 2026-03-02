package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// DetectMode: returns correct modes based on env vars
// ---------------------------------------------------------------------------

func TestDetectMode_Local(t *testing.T) {
	// Ensure GITHUB_ACTIONS is not set.
	t.Setenv("GITHUB_ACTIONS", "")
	t.Setenv("GITHUB_EVENT_NAME", "")

	mode := DetectMode()
	if mode != "local" {
		t.Errorf("DetectMode() = %q, want %q", mode, "local")
	}
}

func TestDetectMode_PRCI(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_EVENT_NAME", "pull_request")
	// Clear fork-related vars so it's treated as a same-repo PR.
	t.Setenv("GITHUB_HEAD_REF", "")
	t.Setenv("GITHUB_REPOSITORY", "")

	mode := DetectMode()
	if mode != "pr_ci" {
		t.Errorf("DetectMode() = %q, want %q", mode, "pr_ci")
	}
}

func TestDetectMode_ForkPR(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_EVENT_NAME", "pull_request")
	t.Setenv("GITHUB_HEAD_REF", "feature-branch")
	t.Setenv("GITHUB_REPOSITORY", "owner/repo")

	mode := DetectMode()
	if mode != "fork_pr" {
		t.Errorf("DetectMode() = %q, want %q", mode, "fork_pr")
	}
}

func TestDetectMode_Nightly(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_EVENT_NAME", "schedule")

	mode := DetectMode()
	if mode != "nightly" {
		t.Errorf("DetectMode() = %q, want %q", mode, "nightly")
	}
}

func TestDetectMode_Push(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_EVENT_NAME", "push")

	mode := DetectMode()
	if mode != "pr_ci" {
		t.Errorf("DetectMode() = %q, want %q (default for push)", mode, "pr_ci")
	}
}

// ---------------------------------------------------------------------------
// Enable: generates workflow and policy files in a temp dir
// ---------------------------------------------------------------------------

func TestEnable_GeneratesFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a minimal requirements.txt so framework detection works.
	reqPath := filepath.Join(tmpDir, "requirements.txt")
	if err := os.WriteFile(reqPath, []byte("fastapi==0.100.0\nuvicorn\n"), 0o644); err != nil {
		t.Fatalf("write requirements.txt: %v", err)
	}

	result, err := Enable(tmpDir)
	if err != nil {
		t.Fatalf("Enable: %v", err)
	}

	// Verify workflow file exists.
	if _, err := os.Stat(result.WorkflowPath); os.IsNotExist(err) {
		t.Errorf("workflow file does not exist: %s", result.WorkflowPath)
	}

	// Verify policy file exists.
	if _, err := os.Stat(result.PolicyPath); os.IsNotExist(err) {
		t.Errorf("policy file does not exist: %s", result.PolicyPath)
	}

	// Verify the framework was detected.
	if result.Framework != "fastapi" {
		t.Errorf("Framework = %q, want %q", result.Framework, "fastapi")
	}

	// Verify paths are under the project dir.
	if !isSubpath(tmpDir, result.WorkflowPath) {
		t.Errorf("WorkflowPath %q is not under %q", result.WorkflowPath, tmpDir)
	}
	if !isSubpath(tmpDir, result.PolicyPath) {
		t.Errorf("PolicyPath %q is not under %q", result.PolicyPath, tmpDir)
	}

	// Verify workflow content is non-empty.
	data, err := os.ReadFile(result.WorkflowPath)
	if err != nil {
		t.Fatalf("read workflow: %v", err)
	}
	if len(data) == 0 {
		t.Error("workflow file is empty")
	}

	// Verify policy content is non-empty.
	data, err = os.ReadFile(result.PolicyPath)
	if err != nil {
		t.Fatalf("read policy: %v", err)
	}
	if len(data) == 0 {
		t.Error("policy file is empty")
	}
}

func TestEnable_CreatesDirectories(t *testing.T) {
	tmpDir := t.TempDir()

	_, err := Enable(tmpDir)
	if err != nil {
		t.Fatalf("Enable: %v", err)
	}

	// .github/workflows should exist.
	wfDir := filepath.Join(tmpDir, ".github", "workflows")
	if info, err := os.Stat(wfDir); err != nil || !info.IsDir() {
		t.Errorf("expected directory %s to exist", wfDir)
	}

	// evals/smoke should exist.
	smokeDir := filepath.Join(tmpDir, "evals", "smoke")
	if info, err := os.Stat(smokeDir); err != nil || !info.IsDir() {
		t.Errorf("expected directory %s to exist", smokeDir)
	}
}

// ---------------------------------------------------------------------------
// DetectFramework: identifies frameworks from requirements.txt content
// ---------------------------------------------------------------------------

func TestDetectFramework_FastAPI(t *testing.T) {
	tmpDir := t.TempDir()
	writeReqs(t, tmpDir, "fastapi==0.100.0\nuvicorn\n")

	got := DetectFramework(tmpDir)
	if got != "fastapi" {
		t.Errorf("DetectFramework = %q, want %q", got, "fastapi")
	}
}

func TestDetectFramework_Flask(t *testing.T) {
	tmpDir := t.TempDir()
	writeReqs(t, tmpDir, "flask>=2.0\ngunicorn\n")

	got := DetectFramework(tmpDir)
	if got != "flask" {
		t.Errorf("DetectFramework = %q, want %q", got, "flask")
	}
}

func TestDetectFramework_Langchain(t *testing.T) {
	tmpDir := t.TempDir()
	writeReqs(t, tmpDir, "langchain==0.1.0\nopenai\n")

	got := DetectFramework(tmpDir)
	if got != "langchain" {
		t.Errorf("DetectFramework = %q, want %q", got, "langchain")
	}
}

func TestDetectFramework_Generic(t *testing.T) {
	tmpDir := t.TempDir()
	// No requirements.txt at all.

	got := DetectFramework(tmpDir)
	if got != "generic" {
		t.Errorf("DetectFramework = %q, want %q", got, "generic")
	}
}

func TestDetectFramework_FromPyproject(t *testing.T) {
	tmpDir := t.TempDir()
	pyprojectPath := filepath.Join(tmpDir, "pyproject.toml")
	content := `[project]
dependencies = ["fastapi>=0.100.0", "uvicorn"]
`
	if err := os.WriteFile(pyprojectPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write pyproject.toml: %v", err)
	}

	got := DetectFramework(tmpDir)
	if got != "fastapi" {
		t.Errorf("DetectFramework = %q, want %q", got, "fastapi")
	}
}

// ---------------------------------------------------------------------------
// PrintOnboardingChecklist: doesn't panic for each framework type
// ---------------------------------------------------------------------------

func TestPrintOnboardingChecklist_NoPanic(t *testing.T) {
	frameworks := []string{"fastapi", "flask", "langchain", "generic", "unknown", ""}

	for _, fw := range frameworks {
		t.Run(fw, func(t *testing.T) {
			// Simply verify it doesn't panic.
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("PrintOnboardingChecklist(%q) panicked: %v", fw, r)
				}
			}()
			PrintOnboardingChecklist(fw)
		})
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func writeReqs(t *testing.T, dir, content string) {
	t.Helper()
	reqPath := filepath.Join(dir, "requirements.txt")
	if err := os.WriteFile(reqPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write requirements.txt: %v", err)
	}
}

func isSubpath(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	return !strings.HasPrefix(rel, "..")
}
