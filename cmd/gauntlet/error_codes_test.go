package main

import (
	"fmt"
	"testing"

	"github.com/gauntlet-dev/gauntlet/internal/fixture"
)

func TestNormalizeCLIErrorCode(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "scenario_assertion_failed", want: "scenario_assertion_failed"},
		{in: " Scenario Assertion Failed ", want: "scenario_assertion_failed"},
		{in: "proxy-startup/cert-issue", want: "proxy_startup_cert_issue"},
		{in: "___", want: cliErrorCodeUnknown},
		{in: "", want: cliErrorCodeUnknown},
	}
	for _, tt := range tests {
		if got := normalizeCLIErrorCode(tt.in); got != tt.want {
			t.Fatalf("normalizeCLIErrorCode(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestFormatCLIErrorCodeLine(t *testing.T) {
	got := formatCLIErrorCodeLine("Scenario Assertion Failed")
	want := "GAUNTLET_ERROR_CODE=scenario_assertion_failed"
	if got != want {
		t.Fatalf("formatCLIErrorCodeLine = %q, want %q", got, want)
	}
}

func TestClassifyCLIError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "fixture miss direct",
			err: &fixture.ErrFixtureMiss{
				FixtureType:    "model:gpt-4o-mini",
				ProviderFamily: "openai_compatible",
				Model:          "gpt-4o-mini",
				CanonicalHash:  "abc",
			},
			want: "fixture_miss",
		},
		{
			name: "fixture miss wrapped",
			err: fmt.Errorf("run failed: %w", &fixture.ErrFixtureMiss{
				FixtureType:   "tool:order_lookup",
				CanonicalHash: "abc",
			}),
			want: "fixture_miss",
		},
		{
			name: "proxy startup root cause",
			err:  fmt.Errorf("proxy startup failed [root_cause=permission]: bind: permission denied"),
			want: "proxy_startup_permission",
		},
		{
			name: "mode validation",
			err:  fmt.Errorf("invalid model mode \"pr_ci\" from --model-mode"),
			want: "mode_validation_failed",
		},
		{
			name: "unknown",
			err:  fmt.Errorf("something unexpected"),
			want: cliErrorCodeUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyCLIError(tt.err); got != tt.want {
				t.Fatalf("classifyCLIError() = %q, want %q", got, tt.want)
			}
		})
	}
}
