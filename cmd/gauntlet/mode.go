package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/pmclSF/gauntlet/internal/ci"
	"github.com/pmclSF/gauntlet/internal/policy"
)

const (
	runnerModeFlagName = "--runner-mode"
	legacyModeFlagName = "--mode"
	modelModeFlagName  = "--model-mode"
)

var (
	runnerModeValues = []string{"local", "pr_ci", "fork_pr", "nightly", "hermetic", "replay"}
	modelModeValues  = []string{"recorded", "live", "passthrough"}
)

func normalizeMode(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func isRunnerModeValue(mode string) bool {
	switch normalizeMode(mode) {
	case "local", "pr_ci", "fork_pr", "nightly", "hermetic", "replay":
		return true
	default:
		return false
	}
}

func isModelModeValue(mode string) bool {
	switch normalizeMode(mode) {
	case "recorded", "live", "passthrough":
		return true
	default:
		return false
	}
}

func validateRunnerMode(raw, source string) (string, error) {
	mode := normalizeMode(raw)
	if mode == "" {
		return "", nil
	}
	switch mode {
	case "hermetic":
		return "pr_ci", nil
	case "replay":
		return "fork_pr", nil
	}
	if isRunnerModeValue(mode) {
		return mode, nil
	}
	if isModelModeValue(mode) {
		return "", fmt.Errorf(
			"%s received model mode %q; runner mode must be one of %s. Use %s for model replay mode",
			source,
			mode,
			strings.Join(runnerModeValues, ", "),
			modelModeFlagName,
		)
	}
	return "", fmt.Errorf(
		"invalid runner mode %q from %s (expected one of %s)",
		raw,
		source,
		strings.Join(runnerModeValues, ", "),
	)
}

func validateModelMode(raw, source string) (string, error) {
	mode := normalizeMode(raw)
	if mode == "" {
		return "", nil
	}
	if isModelModeValue(mode) {
		return mode, nil
	}
	if isRunnerModeValue(mode) {
		return "", fmt.Errorf(
			"%s received runner mode %q; model mode must be one of %s. Use %s for runner execution mode",
			source,
			mode,
			strings.Join(modelModeValues, ", "),
			runnerModeFlagName,
		)
	}
	return "", fmt.Errorf(
		"invalid model mode %q from %s (expected one of %s)",
		raw,
		source,
		strings.Join(modelModeValues, ", "),
	)
}

func resolveRunnerMode(runnerModeOverride, legacyModeOverride string, resolved *policy.Resolved) (string, error) {
	runnerMode, err := validateRunnerMode(runnerModeOverride, runnerModeFlagName)
	if err != nil {
		return "", err
	}
	legacyMode, err := validateRunnerMode(legacyModeOverride, legacyModeFlagName)
	if err != nil {
		return "", err
	}
	if runnerMode != "" && legacyMode != "" && runnerMode != legacyMode {
		return "", fmt.Errorf(
			"%s=%q conflicts with %s=%q; use only one runner mode flag",
			runnerModeFlagName,
			runnerMode,
			legacyModeFlagName,
			legacyMode,
		)
	}
	if runnerMode != "" {
		return runnerMode, nil
	}
	if legacyMode != "" {
		return legacyMode, nil
	}
	if resolved != nil {
		mode, err := validateRunnerMode(resolved.RunnerMode, "policy runner_mode")
		if err != nil {
			return "", err
		}
		if mode != "" {
			return mode, nil
		}
	}
	mode, err := validateRunnerMode(ci.DetectMode(), "detected CI mode")
	if err != nil {
		return "", err
	}
	if mode != "" {
		return mode, nil
	}
	return "local", nil
}

func resolveModelMode(modelModeOverride string, resolved *policy.Resolved) (string, error) {
	mode, err := validateModelMode(modelModeOverride, modelModeFlagName)
	if err != nil {
		return "", err
	}
	if mode != "" {
		return mode, nil
	}
	mode, err = validateModelMode(os.Getenv("GAUNTLET_MODEL_MODE"), "GAUNTLET_MODEL_MODE")
	if err != nil {
		return "", err
	}
	if mode != "" {
		return mode, nil
	}
	if resolved != nil {
		mode, err = validateModelMode(resolved.ModelMode, "policy model_mode")
		if err != nil {
			return "", err
		}
		if mode != "" {
			return mode, nil
		}
	}
	return "recorded", nil
}

func runnerModeRequiresBlockedEgress(mode string) bool {
	switch normalizeMode(mode) {
	case "pr_ci", "fork_pr":
		return true
	default:
		return false
	}
}
