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
	t.Setenv("GITHUB_REPOSITORY", "acme/agent")
	t.Setenv("GITHUB_HEAD_REPO", "acme/agent")

	mode := DetectMode()
	if mode != "pr_ci" {
		t.Errorf("DetectMode() = %q, want %q", mode, "pr_ci")
	}
}

func TestDetectMode_ForkPR_FromEnv(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_EVENT_NAME", "pull_request")
	t.Setenv("GITHUB_REPOSITORY", "acme/agent")
	t.Setenv("GITHUB_HEAD_REPO", "contrib/agent-fork")

	mode := DetectMode()
	if mode != "fork_pr" {
		t.Errorf("DetectMode() = %q, want %q", mode, "fork_pr")
	}
}

func TestDetectMode_ForkPR_FromEventPayload(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_EVENT_NAME", "pull_request")
	t.Setenv("GITHUB_REPOSITORY", "acme/agent")
	t.Setenv("GITHUB_HEAD_REPO", "")
	t.Setenv("GITHUB_EVENT_PATH", writeEventPayload(t, `{
		"pull_request": {
			"head": {"repo": {"full_name": "contrib/agent-fork"}},
			"base": {"repo": {"full_name": "acme/agent"}}
		}
	}`))

	mode := DetectMode()
	if mode != "fork_pr" {
		t.Errorf("DetectMode() = %q, want %q", mode, "fork_pr")
	}
}

func TestDetectMode_NightlySchedule(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_EVENT_NAME", "schedule")

	mode := DetectMode()
	if mode != "nightly" {
		t.Errorf("DetectMode() = %q, want %q", mode, "nightly")
	}
}

func TestDetectMode_NightlyPushMain(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_EVENT_NAME", "push")
	t.Setenv("GITHUB_REF", "refs/heads/main")

	mode := DetectMode()
	if mode != "nightly" {
		t.Errorf("DetectMode() = %q, want %q", mode, "nightly")
	}
}

func TestDetectMode_PushNonMain(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_EVENT_NAME", "push")
	t.Setenv("GITHUB_REF", "refs/heads/feature/test")
	t.Setenv("GITHUB_REF_NAME", "feature/test")

	mode := DetectMode()
	if mode != "pr_ci" {
		t.Errorf("DetectMode() = %q, want %q", mode, "pr_ci")
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
	workflowText := string(data)
	if !strings.Contains(workflowText, "persist-credentials: false") {
		t.Error("workflow should set checkout persist-credentials: false")
	}
	if !strings.Contains(workflowText, "permissions:") || !strings.Contains(workflowText, "contents: read") {
		t.Error("workflow should use least-privilege permissions")
	}
	if !strings.Contains(workflowText, "gauntlet scan-artifacts --dir evals") {
		t.Error("workflow should run gauntlet scan-artifacts before upload")
	}
	if !strings.Contains(workflowText, "go install github.com/pmclSF/gauntlet/cmd/gauntlet@") {
		t.Error("workflow should install gauntlet CLI via go install")
	}
	if !strings.Contains(workflowText, "pip install gauntlet-sdk") {
		t.Error("workflow should install gauntlet-sdk")
	}
	if !strings.Contains(workflowText, "command -v unshare") || !strings.Contains(workflowText, "unshare --net /bin/sh -lc") {
		t.Error("workflow should run pr_ci suite inside a network namespace for hermetic egress")
	}
	if !strings.Contains(workflowText, "gauntlet sign-artifacts --dir evals/runs") {
		t.Error("workflow should sign artifact evidence bundle before upload")
	}
	if !strings.Contains(workflowText, "id: scan_artifacts") {
		t.Error("workflow should assign scan_artifacts step id for gating")
	}
	if !strings.Contains(workflowText, "id: sign_artifacts") {
		t.Error("workflow should assign sign_artifacts step id for upload gating")
	}
	if !strings.Contains(workflowText, "continue-on-error: true") {
		t.Error("sign-artifacts step should continue-on-error so artifact upload still runs")
	}
	if !strings.Contains(workflowText, "name: Upload results\n        if: always()") {
		t.Error("upload step must always run so results are available for debugging")
	}
	if !strings.Contains(workflowText, "gauntlet check-baseline-approval") {
		t.Error("workflow should enforce baseline approval policy for baseline-changing PRs")
	}
	if !strings.Contains(workflowText, "git fetch --no-tags --prune --depth=1 origin \"${{ github.base_ref }}\"") {
		t.Error("workflow should fetch PR base ref for deterministic baseline diff")
	}
	approvalIdx := strings.Index(workflowText, "gauntlet check-baseline-approval")
	runIdx := strings.Index(workflowText, "name: Run Gauntlet smoke suite")
	if approvalIdx == -1 || runIdx == -1 || approvalIdx > runIdx {
		t.Error("baseline approval check must run before gauntlet suite execution")
	}
	scanIdx := strings.Index(workflowText, "gauntlet scan-artifacts --dir evals")
	signIdx := strings.Index(workflowText, "gauntlet sign-artifacts --dir evals/runs")
	uploadIdx := strings.Index(workflowText, "name: Upload results")
	if scanIdx == -1 || signIdx == -1 || uploadIdx == -1 {
		t.Error("scan/sign/upload steps must all exist in workflow")
	}
	if scanIdx > signIdx {
		t.Error("scan-artifacts step must appear before sign-artifacts step")
	}
	if signIdx > uploadIdx {
		t.Error("sign-artifacts step must appear before upload step")
	}
	requiredPins := []string{
		"actions/checkout@11bd71901bbe5b1630ceea73d27597364c9af683",
		"actions/setup-go@3041bf56c941b39c61721a86cd11f3bb1338122a",
		"actions/setup-python@42375524e23c412d93fb67b49958b491fce71c38",
		"actions/upload-artifact@65c4c4a1ddee5b72f698fdd19549f0f0fb45cf08",
	}
	for _, pin := range requiredPins {
		if !strings.Contains(workflowText, pin) {
			t.Errorf("workflow missing required pinned action reference: %s", pin)
		}
	}
	if strings.Contains(workflowText, "actions/checkout@v4") ||
		strings.Contains(workflowText, "actions/setup-go@v5") ||
		strings.Contains(workflowText, "actions/setup-python@v5") ||
		strings.Contains(workflowText, "actions/upload-artifact@v4") {
		t.Error("workflow should not reference tag-based action versions")
	}

	// Verify policy content is non-empty.
	data, err = os.ReadFile(result.PolicyPath)
	if err != nil {
		t.Fatalf("read policy: %v", err)
	}
	if len(data) == 0 {
		t.Error("policy file is empty")
	}
	policyText := string(data)
	if !strings.Contains(policyText, `addr: "localhost:0"`) {
		t.Error("policy template should default proxy addr to localhost:0 for concurrent runs")
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

func writeEventPayload(t *testing.T, content string) string {
	t.Helper()
	eventPath := filepath.Join(t.TempDir(), "event.json")
	if err := os.WriteFile(eventPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write event payload: %v", err)
	}
	return eventPath
}

func isSubpath(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	return !strings.HasPrefix(rel, "..")
}
