package baseline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileSHA256(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.json")
	if err := os.WriteFile(path, []byte(`{"ok":true}`), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	hash, exists, err := FileSHA256(path)
	if err != nil {
		t.Fatalf("FileSHA256: %v", err)
	}
	if !exists {
		t.Fatal("expected file to exist")
	}
	if len(hash) != 64 {
		t.Fatalf("unexpected hash length: %d", len(hash))
	}
}

func TestFileSHA256_Missing(t *testing.T) {
	hash, exists, err := FileSHA256(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exists {
		t.Fatal("expected exists=false for missing file")
	}
	if hash != "" {
		t.Fatalf("hash = %q, want empty", hash)
	}
}

func TestWriteRollbackManifest_SortsAndDefaults(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "rollback.manifest.json")
	manifest := &RollbackManifest{
		Suite: "smoke",
		Changes: []RollbackChange{
			{Path: "evals/baselines/smoke/b.json", CurrentSHA256: "b"},
			{Path: "evals/baselines/smoke/a.json", CurrentSHA256: "a"},
			{Path: "evals/baselines/smoke/a.json", CurrentSHA256: "dup"},
		},
	}
	if err := WriteRollbackManifest(manifestPath, manifest); err != nil {
		t.Fatalf("WriteRollbackManifest: %v", err)
	}

	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var got RollbackManifest
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	if got.ManifestVersion != 1 {
		t.Fatalf("manifest_version = %d, want 1", got.ManifestVersion)
	}
	if got.RequiredApprovalLabel != DefaultBaselineApprovalLabel {
		t.Fatalf("required_approval_label = %q", got.RequiredApprovalLabel)
	}
	if got.BaseRef != "origin/main" {
		t.Fatalf("base_ref = %q, want origin/main", got.BaseRef)
	}
	if len(got.Changes) != 2 {
		t.Fatalf("changes len = %d, want 2", len(got.Changes))
	}
	if got.Changes[0].Path != "evals/baselines/smoke/a.json" {
		t.Fatalf("first change path = %q", got.Changes[0].Path)
	}
}

func TestWriteRollbackTemplate_ContainsRevertInstructions(t *testing.T) {
	dir := t.TempDir()
	templatePath := filepath.Join(dir, "ROLLBACK_PR_TEMPLATE.md")
	manifest := &RollbackManifest{
		Suite:                 "smoke",
		GeneratedAt:           "2026-03-04T12:00:00Z",
		BaseRef:               "origin/main",
		RequiredApprovalLabel: "gauntlet/baseline-approved",
		Changes: []RollbackChange{
			{
				Path:           "evals/baselines/smoke/order_status.json",
				Action:         "updated",
				PreviousSHA256: "prev",
				CurrentSHA256:  "curr",
			},
		},
	}
	if err := WriteRollbackTemplate(templatePath, manifest); err != nil {
		t.Fatalf("WriteRollbackTemplate: %v", err)
	}

	data, err := os.ReadFile(templatePath)
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	text := string(data)
	required := []string{
		"Revert Gauntlet Baseline Update",
		"`gauntlet/baseline-approved`",
		"git switch -c revert/gauntlet-baselines-smoke-20260304",
		"git checkout origin/main --",
		"evals/baselines/smoke/order_status.json",
		"gh pr create --title \"revert: gauntlet baseline update (smoke)\"",
	}
	for _, token := range required {
		if !strings.Contains(text, token) {
			t.Fatalf("template missing %q:\n%s", token, text)
		}
	}
}
