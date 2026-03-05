package main

import (
	"strings"
	"testing"

	"github.com/pmclSF/gauntlet/internal/policy"
)

func TestResolveRunnerMode_ExplicitOverridesPolicy(t *testing.T) {
	resolved := &policy.Resolved{RunnerMode: "pr_ci"}
	got, err := resolveRunnerMode("nightly", "", resolved)
	if err != nil {
		t.Fatalf("resolveRunnerMode: %v", err)
	}
	if got != "nightly" {
		t.Fatalf("resolveRunnerMode = %q, want nightly", got)
	}
}

func TestResolveRunnerMode_ConflictingFlagsFail(t *testing.T) {
	_, err := resolveRunnerMode("local", "nightly", nil)
	if err == nil {
		t.Fatal("expected conflict error")
	}
	if !strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveRunnerMode_RejectsModelModeValue(t *testing.T) {
	_, err := resolveRunnerMode("live", "", nil)
	if err == nil {
		t.Fatal("expected runner mode validation error")
	}
	if !strings.Contains(err.Error(), "--model-mode") {
		t.Fatalf("expected --model-mode guidance, got: %v", err)
	}
}

func TestValidateModelMode_RejectsRunnerModeValue(t *testing.T) {
	_, err := validateModelMode("pr_ci", "--model-mode")
	if err == nil {
		t.Fatal("expected model mode validation error")
	}
	if !strings.Contains(err.Error(), "--runner-mode") {
		t.Fatalf("expected --runner-mode guidance, got: %v", err)
	}
}

func TestResolveModelMode_PreferenceOrder(t *testing.T) {
	t.Setenv("GAUNTLET_MODEL_MODE", "live")
	resolved := &policy.Resolved{ModelMode: "recorded"}

	got, err := resolveModelMode("passthrough", resolved)
	if err != nil {
		t.Fatalf("resolveModelMode override: %v", err)
	}
	if got != "passthrough" {
		t.Fatalf("resolveModelMode override = %q, want passthrough", got)
	}

	got, err = resolveModelMode("", resolved)
	if err != nil {
		t.Fatalf("resolveModelMode env: %v", err)
	}
	if got != "live" {
		t.Fatalf("resolveModelMode env = %q, want live", got)
	}

	t.Setenv("GAUNTLET_MODEL_MODE", "")
	got, err = resolveModelMode("", resolved)
	if err != nil {
		t.Fatalf("resolveModelMode policy: %v", err)
	}
	if got != "recorded" {
		t.Fatalf("resolveModelMode policy = %q, want recorded", got)
	}
}
