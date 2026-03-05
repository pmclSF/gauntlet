package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/pmclSF/gauntlet/internal/fixture"
	"github.com/pmclSF/gauntlet/internal/policy"
	"github.com/pmclSF/gauntlet/internal/proxy"
	"github.com/pmclSF/gauntlet/internal/runner"
)

var (
	doctorCheckEgressFn       = runner.CheckEgressBlocked
	doctorLoadCAFn            = proxy.LoadCA
	doctorProxyBindCheckFn    = checkProxyAddrAvailability
	doctorNowFn               = time.Now
	doctorTrustedIdentityFrom = trustedRecorderIdentitiesFromEnv
)

type doctorStatus string

const (
	doctorPass doctorStatus = "pass"
	doctorWarn doctorStatus = "warn"
	doctorFail doctorStatus = "fail"
)

type doctorCheck struct {
	Name   string       `json:"name"`
	Status doctorStatus `json:"status"`
	Detail string       `json:"detail"`
	Hint   string       `json:"hint,omitempty"`
}

type doctorReport struct {
	Suite      string        `json:"suite"`
	ConfigPath string        `json:"config_path"`
	RunnerMode string        `json:"runner_mode"`
	ModelMode  string        `json:"model_mode"`
	Passed     bool          `json:"passed"`
	Checks     []doctorCheck `json:"checks"`
}

type doctorOptions struct {
	Suite        string
	ConfigPath   string
	RunnerMode   string
	LegacyMode   string
	ModelMode    string
	StrictPolicy bool
	JSONOutput   bool
}

func newDoctorCmd() *cobra.Command {
	var opts doctorOptions
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Validate policy, mode, proxy trust, fixtures, and egress readiness",
		RunE: func(cmd *cobra.Command, args []string) error {
			report, err := runDoctor(opts)
			if opts.JSONOutput {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				if encodeErr := enc.Encode(report); encodeErr != nil {
					return fmt.Errorf("failed to encode doctor report: %w", encodeErr)
				}
			} else {
				printDoctorReport(report)
			}
			return err
		},
	}

	cmd.Flags().StringVar(&opts.Suite, "suite", "smoke", "Suite to validate")
	cmd.Flags().StringVar(&opts.ConfigPath, "config", "evals/gauntlet.yml", "Path to policy file")
	cmd.Flags().StringVar(&opts.RunnerMode, "runner-mode", "", "Runner execution mode override (local, pr_ci, fork_pr, nightly)")
	cmd.Flags().StringVar(&opts.LegacyMode, "mode", "", "Legacy alias for --runner-mode")
	cmd.Flags().StringVar(&opts.ModelMode, "model-mode", "", "Model replay mode override (recorded, live, passthrough)")
	cmd.Flags().BoolVar(&opts.StrictPolicy, "strict-policy", true, "Require strict policy schema validation (unknown keys are errors)")
	cmd.Flags().BoolVar(&opts.JSONOutput, "json", false, "Emit machine-readable JSON report")
	return cmd
}

func runDoctor(opts doctorOptions) (*doctorReport, error) {
	rawConfigPath := strings.TrimSpace(opts.ConfigPath)
	if rawConfigPath == "" {
		rawConfigPath = filepath.Join("evals", "gauntlet.yml")
	}
	report := &doctorReport{
		Suite:      strings.TrimSpace(opts.Suite),
		ConfigPath: filepath.Clean(rawConfigPath),
		Checks:     make([]doctorCheck, 0, 8),
		Passed:     true,
	}
	if report.Suite == "" {
		report.Suite = "smoke"
	}

	resolved, err := policy.LoadWithOptions(report.ConfigPath, report.Suite, policy.LoadOptions{Strict: opts.StrictPolicy})
	if err != nil {
		report.addCheck(doctorFail, "policy_sanity", fmt.Sprintf("failed to load policy: %v", err), "Fix policy schema/path issues and rerun doctor.")
		return report, report.err()
	}
	report.addCheck(doctorPass, "policy_sanity", fmt.Sprintf("policy loaded successfully (strict=%t)", opts.StrictPolicy), "")

	cfg := runner.Config{
		Suite:      report.Suite,
		ConfigPath: report.ConfigPath,
	}
	applyResolvedPolicy(&cfg, resolved, report.ConfigPath)

	runnerMode, err := resolveRunnerMode(opts.RunnerMode, opts.LegacyMode, resolved)
	if err != nil {
		report.addCheck(doctorFail, "mode_resolution", err.Error(), "Use explicit --runner-mode and --model-mode values with separate semantics.")
		return report, report.err()
	}
	modelMode, err := resolveModelMode(opts.ModelMode, resolved)
	if err != nil {
		report.addCheck(doctorFail, "mode_resolution", err.Error(), "Use explicit --runner-mode and --model-mode values with separate semantics.")
		return report, report.err()
	}
	report.RunnerMode = runnerMode
	report.ModelMode = modelMode
	cfg.Mode = runnerMode
	report.addCheck(doctorPass, "mode_resolution", fmt.Sprintf("runner_mode=%s, model_mode=%s", runnerMode, modelMode), "")

	egressStatus := doctorCheckEgressFn()
	if runnerModeRequiresBlockedEgress(runnerMode) {
		if egressStatus != runner.EgressBlocked {
			report.addCheck(
				doctorFail,
				"egress",
				fmt.Sprintf("runner_mode=%s requires blocked egress, but probe reported %s", runnerMode, egressStatus.String()),
				"Enforce outbound network blocking in CI, or run with --runner-mode local/nightly.",
			)
		} else {
			report.addCheck(doctorPass, "egress", "outbound socket probe reports blocked egress (required)", "")
		}
	} else {
		switch egressStatus {
		case runner.EgressUnknown:
			report.addCheck(doctorWarn, "egress", fmt.Sprintf("egress probe returned %s; mode %s does not require blocking", egressStatus.String(), runnerMode), "")
		case runner.EgressBlocked:
			report.addCheck(doctorWarn, "egress", fmt.Sprintf("egress probe returned blocked while mode %s does not require it", runnerMode), "")
		default:
			report.addCheck(doctorPass, "egress", fmt.Sprintf("egress probe returned %s", egressStatus.String()), "")
		}
	}

	proxyMode, err := parseProxyMode(modelMode)
	if err != nil {
		report.addCheck(doctorFail, "proxy_mode", err.Error(), "Set model mode to recorded, live, or passthrough.")
		return report, report.err()
	}
	if proxyMode == proxy.ModePassthrough {
		report.addCheck(doctorPass, "proxy_routing", "model_mode=passthrough; proxy routing check skipped", "")
		report.addCheck(doctorPass, "cert_trust", "model_mode=passthrough; CA trust check skipped", "")
	} else {
		addr := effectiveProxyAddr("", resolved)
		if addr == "" {
			addr = "localhost:7431"
		}
		if err := doctorProxyBindCheckFn(addr); err != nil {
			report.addCheck(doctorFail, "proxy_routing", err.Error(), "Use --runner-mode/--model-mode overrides or set proxy.addr to a bindable address like 127.0.0.1:0.")
		} else {
			report.addCheck(doctorPass, "proxy_routing", fmt.Sprintf("proxy address %s is bindable", addr), "")
		}

		caDir := filepath.Join(filepath.Dir(report.ConfigPath), ".gauntlet")
		ca, err := doctorLoadCAFn(caDir)
		if err != nil {
			hint := "Create/repair CA assets by running a local record/run flow."
			switch {
			case errors.Is(err, proxy.ErrCAInsecurePermissions):
				hint = fmt.Sprintf("Harden permissions for %s (directory not group/other writable; ca.key owner-only).", caDir)
			case errors.Is(err, proxy.ErrCAExpired):
				hint = fmt.Sprintf("Rotate local CA assets in %s to refresh certificate validity.", caDir)
			case errors.Is(err, proxy.ErrCANotYetValid):
				hint = fmt.Sprintf("Correct system clock or regenerate CA assets in %s.", caDir)
			}
			report.addCheck(doctorFail, "cert_trust", fmt.Sprintf("failed to load proxy CA from %s: %v", caDir, err), hint)
		} else {
			now := doctorNowFn().UTC()
			if ca.Cert == nil {
				report.addCheck(doctorFail, "cert_trust", "loaded CA without certificate material", "Recreate .gauntlet/ca.pem and .gauntlet/ca.key.")
			} else if now.After(ca.Cert.NotAfter) {
				report.addCheck(doctorFail, "cert_trust", fmt.Sprintf("CA certificate expired at %s", ca.Cert.NotAfter.UTC().Format(time.RFC3339)), "Regenerate local CA assets.")
			} else if rotate, remaining := proxy.CARotationRecommended(ca.Cert, now, proxy.DefaultCARotationWindow); rotate {
				days := int(remaining.Hours() / 24)
				if days < 0 {
					days = 0
				}
				report.addCheck(
					doctorWarn,
					"cert_trust",
					fmt.Sprintf("CA certificate rotates soon: expires at %s (%d day(s) remaining)", ca.Cert.NotAfter.UTC().Format(time.RFC3339), days),
					fmt.Sprintf("Rotate CA assets in %s before expiry to avoid CI disruption on trusted hosts.", caDir),
				)
			} else {
				report.addCheck(doctorPass, "cert_trust", fmt.Sprintf("CA certificate valid until %s", ca.Cert.NotAfter.UTC().Format(time.RFC3339)), "")
			}
		}
	}

	if modelMode != "recorded" {
		report.addCheck(doctorPass, "fixtures", fmt.Sprintf("model_mode=%s; replay fixture checks skipped", modelMode), "")
		return report, report.err()
	}

	fixturesDir := effectiveFixturesDir(&cfg)
	scenarioDigest := computeScenarioSetDigest(cfg.SuiteDir)
	if scenarioDigest == "" {
		report.addCheck(doctorWarn, "fixture_context", "scenario set digest unavailable; suite context checks are limited", "Ensure suite scenarios exist and are loadable.")
	} else {
		report.addCheck(doctorPass, "fixture_context", fmt.Sprintf("scenario_set_sha256=%s", scenarioDigest), "")
	}

	store := fixture.NewStore(fixturesDir)
	store.SetReplayContext(cfg.Suite, scenarioDigest)
	signingKeyPath := effectiveFixtureSigningKeyPath(cfg.ConfigPath)
	trustedKeyPath := effectiveFixtureTrustedPublicKeyPath(signingKeyPath)
	if err := store.ConfigureFixtureTrust(fixture.FixtureTrustOptions{
		RequireSignatures:         true,
		TrustedPublicKeyPaths:     []string{trustedKeyPath},
		TrustedRecorderIdentities: doctorTrustedIdentityFrom(),
	}); err != nil {
		report.addCheck(doctorFail, "fixtures", fmt.Sprintf("fixture trust setup failed: %v", err), "Ensure trusted public key exists and is readable before recorded replay.")
		return report, report.err()
	}
	if err := fixture.VerifyReplayLockfile(store, cfg.Suite, scenarioDigest, ""); err != nil {
		report.addCheck(
			doctorFail,
			"fixtures",
			fmt.Sprintf("replay fixture verification failed: %v", err),
			fmt.Sprintf("Regenerate replay lockfile with: gauntlet lock-fixtures --suite %s --config %s", cfg.Suite, cfg.ConfigPath),
		)
		return report, report.err()
	}
	report.addCheck(doctorPass, "fixtures", fmt.Sprintf("replay lockfile and fixture integrity verified at %s", filepath.Join(fixturesDir, fixture.DefaultReplayLockfileName)), "")

	return report, report.err()
}

func (r *doctorReport) addCheck(status doctorStatus, name, detail, hint string) {
	r.Checks = append(r.Checks, doctorCheck{
		Name:   name,
		Status: status,
		Detail: detail,
		Hint:   hint,
	})
	if status == doctorFail {
		r.Passed = false
	}
}

func (r *doctorReport) failingCount() int {
	count := 0
	for _, c := range r.Checks {
		if c.Status == doctorFail {
			count++
		}
	}
	return count
}

func (r *doctorReport) err() error {
	failures := r.failingCount()
	if failures == 0 {
		return nil
	}
	return fmt.Errorf("doctor found %d failing check(s)", failures)
}

func printDoctorReport(report *doctorReport) {
	fmt.Println("Gauntlet doctor")
	fmt.Printf("  Suite: %s\n", report.Suite)
	fmt.Printf("  Config: %s\n", report.ConfigPath)
	if report.RunnerMode != "" {
		fmt.Printf("  Runner mode: %s\n", report.RunnerMode)
	}
	if report.ModelMode != "" {
		fmt.Printf("  Model mode: %s\n", report.ModelMode)
	}
	for _, check := range report.Checks {
		fmt.Printf("  [%s] %s: %s\n", strings.ToUpper(string(check.Status)), check.Name, check.Detail)
		if strings.TrimSpace(check.Hint) != "" {
			fmt.Printf("      Fix: %s\n", check.Hint)
		}
	}
	if report.Passed {
		fmt.Println("Doctor status: PASS")
		return
	}
	fmt.Printf("Doctor status: FAIL (%d failing checks)\n", report.failingCount())
}

func checkProxyAddrAvailability(addr string) error {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return fmt.Errorf("proxy address is empty")
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		switch {
		case isAddrInUseError(err):
			return fmt.Errorf("proxy address %s is already in use (root_cause=port_clash)", addr)
		case isPermissionError(err):
			return fmt.Errorf("proxy address %s cannot be bound due to permissions (root_cause=permission)", addr)
		default:
			return fmt.Errorf("proxy address %s is not bindable: %w", addr, err)
		}
	}
	_ = ln.Close()
	return nil
}
