package runner

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/pmclSF/gauntlet/internal/assertions"
	"github.com/pmclSF/gauntlet/internal/tut"
)

func (r *Runner) adapterCapabilityDiagnostics(handle tut.Handle, traces []tut.TraceEvent) []assertions.Result {
	if r.Adapter == nil {
		return nil
	}
	if r.Adapter.Level() == tut.LevelMinimal {
		return nil
	}

	capabilities := tut.ExtractSDKCapabilities(traces)
	if provider, ok := handle.(tut.CapabilityProvider); ok {
		if reported := provider.Capabilities(); reported != nil {
			capabilities = reported
		}
	}

	if capabilities == nil {
		return []assertions.Result{{
			AssertionType: "adapter_capabilities",
			Passed:        false,
			Soft:          true,
			Message:       "SDK capability negotiation unavailable; ensure gauntlet.connect() is called and SDK supports capability protocol v1",
		}}
	}

	var diagnostics []assertions.Result
	if capabilities.ProtocolVersion != tut.CapabilityProtocolV1 {
		diagnostics = append(diagnostics, assertions.Result{
			AssertionType: "adapter_capabilities",
			Passed:        false,
			Soft:          true,
			Message:       fmt.Sprintf("unsupported capability protocol version %d (expected %d)", capabilities.ProtocolVersion, tut.CapabilityProtocolV1),
		})
	}

	if len(capabilities.Adapters) == 0 {
		diagnostics = append(diagnostics, assertions.Result{
			AssertionType: "adapter_capabilities",
			Passed:        false,
			Soft:          true,
			Message:       "capability negotiation returned no adapter feature data",
		})
		return diagnostics
	}

	names := make([]string, 0, len(capabilities.Adapters))
	for name := range capabilities.Adapters {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		cap := capabilities.Adapters[name]
		if cap.Enabled && !cap.Patched {
			reason := strings.TrimSpace(cap.Reason)
			if reason == "" {
				reason = "unknown_reason"
			}
			diagnostics = append(diagnostics, assertions.Result{
				AssertionType: "adapter_capabilities",
				Passed:        false,
				Soft:          true,
				Message:       fmt.Sprintf("adapter %s missing instrumentation: %s", name, reason),
			})
		}
	}

	return diagnostics
}

func (r *Runner) environmentFreezeDiagnostics(handle tut.Handle, traces []tut.TraceEvent) []assertions.Result {
	if r.Adapter == nil || handle == nil {
		return nil
	}
	if r.Adapter.Level() == tut.LevelMinimal {
		return nil
	}

	report := tut.ExtractDeterminismEnvReport(traces)
	capabilities := tut.ExtractSDKCapabilities(traces)
	if provider, ok := handle.(tut.CapabilityProvider); ok {
		if reported := provider.Capabilities(); reported != nil {
			capabilities = reported
		}
	}

	sdkName := "unknown"
	if capabilities != nil && strings.TrimSpace(capabilities.SDK) != "" {
		sdkName = strings.TrimSpace(capabilities.SDK)
	}

	if report == nil {
		if sdkName != "gauntlet-python" {
			return []assertions.Result{{
				AssertionType: "nondeterminism.env",
				Passed:        false,
				Soft:          true,
				Message:       fmt.Sprintf("environment freeze verification unavailable for sdk %q; runtime verification currently implemented for gauntlet-python", sdkName),
			}}
		}
		return []assertions.Result{{
			AssertionType: "nondeterminism.env",
			Passed:        false,
			Soft:          true,
			Message:       "gauntlet-python runtime did not emit determinism_env verification; ensure gauntlet.connect() is called before agent execution",
		}}
	}

	diagnostics := make([]assertions.Result, 0, 4)
	expectedFreeze := strings.TrimSpace(r.Harness.FreezeTime.UTC().Format(time.RFC3339))
	if strings.TrimSpace(report.RequestedFreezeTime) != "" && !sameRFC3339Instant(report.RequestedFreezeTime, expectedFreeze) {
		diagnostics = append(diagnostics, assertions.Result{
			AssertionType: "nondeterminism.env",
			Passed:        false,
			Soft:          true,
			Message:       fmt.Sprintf("freeze time mismatch: expected %s, runtime requested %s", expectedFreeze, strings.TrimSpace(report.RequestedFreezeTime)),
		})
	}
	if !report.TimePatched {
		diagnostics = append(diagnostics, assertions.Result{
			AssertionType: "nondeterminism.env",
			Passed:        false,
			Soft:          true,
			Message:       "runtime reported time patch not applied",
		})
	}

	expectedTimezone := strings.TrimSpace(r.Harness.Timezone)
	if expectedTimezone != "" && (!report.TimezoneApplied || !timezoneEquivalent(report.EffectiveTimezone, expectedTimezone)) {
		diagnostics = append(diagnostics, assertions.Result{
			AssertionType: "nondeterminism.env",
			Passed:        false,
			Soft:          true,
			Message:       fmt.Sprintf("timezone verification failed: expected %q, effective %q (applied=%t)", expectedTimezone, strings.TrimSpace(report.EffectiveTimezone), report.TimezoneApplied),
		})
	}

	expectedLocale := strings.TrimSpace(r.Harness.Locale)
	if expectedLocale != "" && (!report.LocaleApplied || !localeEquivalent(report.EffectiveLocale, expectedLocale)) {
		diagnostics = append(diagnostics, assertions.Result{
			AssertionType: "nondeterminism.env",
			Passed:        false,
			Soft:          true,
			Message:       fmt.Sprintf("locale verification failed: expected %q, effective %q (applied=%t)", expectedLocale, strings.TrimSpace(report.EffectiveLocale), report.LocaleApplied),
		})
	}

	return diagnostics
}

func sameRFC3339Instant(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" || b == "" {
		return a == b
	}
	at, aErr := time.Parse(time.RFC3339, a)
	bt, bErr := time.Parse(time.RFC3339, b)
	if aErr != nil || bErr != nil {
		return strings.EqualFold(a, b)
	}
	return at.UTC().Equal(bt.UTC())
}

func timezoneEquivalent(actual, expected string) bool {
	actualNorm := strings.ToUpper(strings.TrimSpace(actual))
	expectedNorm := strings.ToUpper(strings.TrimSpace(expected))
	if actualNorm == "" || expectedNorm == "" {
		return actualNorm == expectedNorm
	}
	if actualNorm == expectedNorm {
		return true
	}
	return strings.Contains(actualNorm, expectedNorm)
}

func localeEquivalent(actual, expected string) bool {
	actualNorm := strings.ToLower(strings.TrimSpace(actual))
	expectedNorm := strings.ToLower(strings.TrimSpace(expected))
	if actualNorm == "" || expectedNorm == "" {
		return actualNorm == expectedNorm
	}
	if actualNorm == expectedNorm {
		return true
	}
	return strings.Contains(actualNorm, expectedNorm)
}
