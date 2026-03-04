package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/gauntlet-dev/gauntlet/internal/fixture"
)

const (
	cliErrorCodePrefix  = "GAUNTLET_ERROR_CODE="
	cliErrorCodeUnknown = "cli_error"
)

func emitCLIErrorCode(code string) {
	fmt.Fprintln(os.Stderr, formatCLIErrorCodeLine(code))
}

func formatCLIErrorCodeLine(code string) string {
	return cliErrorCodePrefix + normalizeCLIErrorCode(code)
}

func normalizeCLIErrorCode(code string) string {
	code = strings.TrimSpace(strings.ToLower(code))
	if code == "" {
		return cliErrorCodeUnknown
	}
	var b strings.Builder
	lastUnderscore := false
	for _, r := range code {
		allowed := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if allowed {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	normalized := strings.Trim(b.String(), "_")
	if normalized == "" {
		return cliErrorCodeUnknown
	}
	return normalized
}

func classifyCLIError(err error) string {
	if err == nil {
		return cliErrorCodeUnknown
	}
	var fixtureMiss *fixture.ErrFixtureMiss
	if errors.As(err, &fixtureMiss) {
		return "fixture_miss"
	}

	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "egress self-test failed"):
		return "egress_self_test_failed"
	case strings.Contains(msg, "proxy startup failed [root_cause=port_clash]"):
		return "proxy_startup_port_clash"
	case strings.Contains(msg, "proxy startup failed [root_cause=cert_issue]"):
		return "proxy_startup_cert_issue"
	case strings.Contains(msg, "proxy startup failed [root_cause=permission]"):
		return "proxy_startup_permission"
	case strings.Contains(msg, "proxy startup failed"):
		return "proxy_startup_failed"
	case strings.Contains(msg, "replay integrity check failed"):
		return "replay_integrity_failed"
	case strings.Contains(msg, "fixture trust policy failed"):
		return "fixture_trust_policy_failed"
	case strings.Contains(msg, "doctor found") && strings.Contains(msg, "failing check"):
		return "doctor_failed_checks"
	case strings.Contains(msg, "required approval label") && strings.Contains(msg, "is missing"):
		return "baseline_approval_missing"
	case strings.Contains(msg, "invalid runner mode") ||
		strings.Contains(msg, "invalid model mode") ||
		strings.Contains(msg, "received model mode") ||
		strings.Contains(msg, "received runner mode") ||
		(strings.Contains(msg, "--runner-mode") && strings.Contains(msg, "--mode") && strings.Contains(msg, "conflicts")):
		return "mode_validation_failed"
	case strings.Contains(msg, "failed to load policy") ||
		strings.Contains(msg, "policy file not found") ||
		strings.Contains(msg, "policy path is a directory") ||
		strings.Contains(msg, "policy schema"):
		return "policy_load_failed"
	default:
		return cliErrorCodeUnknown
	}
}
