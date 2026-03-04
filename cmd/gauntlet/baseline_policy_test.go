package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFilterBaselineChangedFiles(t *testing.T) {
	input := []string{
		"README.md",
		"./evals/baselines/smoke/a.json",
		"evals/baselines/smoke/b.json",
		"evals/fixtures/replay.lock.json",
		"evals/baselines/smoke/b.json",
	}
	got := filterBaselineChangedFiles(input, "evals/baselines")
	want := []string{
		"evals/baselines/smoke/a.json",
		"evals/baselines/smoke/b.json",
	}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestReadPullRequestLabels(t *testing.T) {
	eventPath := filepath.Join(t.TempDir(), "event.json")
	payload := `{
  "pull_request": {
    "labels": [
      {"name": "gauntlet/baseline-approved"},
      {"name": "team/security"},
      {"name": "gauntlet/baseline-approved"}
    ]
  }
}`
	if err := os.WriteFile(eventPath, []byte(payload), 0o644); err != nil {
		t.Fatalf("write event: %v", err)
	}

	labels, err := readPullRequestLabels(eventPath)
	if err != nil {
		t.Fatalf("readPullRequestLabels: %v", err)
	}
	if !containsLabel(labels, "gauntlet/baseline-approved") {
		t.Fatalf("expected label in %v", labels)
	}
	if len(labels) != 2 {
		t.Fatalf("expected deduped labels, got %v", labels)
	}
}

func TestCheckBaselineApprovalCmd_NoBaselineChangesPasses(t *testing.T) {
	cmd := newCheckBaselineApprovalCmd()
	cmd.SetArgs([]string{
		"--changed-file", "README.md",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected pass with no baseline changes: %v", err)
	}
}

func TestCheckBaselineApprovalCmd_BaselineChangeWithoutLabelFails(t *testing.T) {
	cmd := newCheckBaselineApprovalCmd()
	cmd.SetArgs([]string{
		"--changed-file", "evals/baselines/smoke/order_status.json",
	})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected failure when baseline changed without approval label")
	}
	if !strings.Contains(err.Error(), "required approval label") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCheckBaselineApprovalCmd_BaselineChangeWithLabelPasses(t *testing.T) {
	cmd := newCheckBaselineApprovalCmd()
	cmd.SetArgs([]string{
		"--changed-file", "evals/baselines/smoke/order_status.json",
		"--label", "gauntlet/baseline-approved",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected pass with approval label: %v", err)
	}
}

func TestCheckBaselineApprovalCmd_BaselineChangeWithEventLabelPasses(t *testing.T) {
	eventPath := filepath.Join(t.TempDir(), "event.json")
	payload := `{
  "pull_request": {
    "labels": [
      {"name": "gauntlet/baseline-approved"}
    ]
  }
}`
	if err := os.WriteFile(eventPath, []byte(payload), 0o644); err != nil {
		t.Fatalf("write event: %v", err)
	}

	cmd := newCheckBaselineApprovalCmd()
	cmd.SetArgs([]string{
		"--changed-file", "evals/baselines/smoke/order_status.json",
		"--event-path", eventPath,
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected pass from event labels: %v", err)
	}
}
