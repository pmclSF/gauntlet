package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/pmclSF/gauntlet/internal/api"
	"github.com/pmclSF/gauntlet/internal/baseline"
	"github.com/pmclSF/gauntlet/internal/ci"
	"github.com/pmclSF/gauntlet/internal/discovery"
	"github.com/pmclSF/gauntlet/internal/fixture"
	"github.com/pmclSF/gauntlet/internal/output"
	"github.com/pmclSF/gauntlet/internal/proxy"
	"github.com/pmclSF/gauntlet/internal/redaction"
	"github.com/pmclSF/gauntlet/internal/runner"
	"github.com/pmclSF/gauntlet/internal/scaffold"
	"github.com/pmclSF/gauntlet/internal/scenario"
	"github.com/pmclSF/gauntlet/internal/tut"
)

var version = "0.1.0"
var (
	flagVerbose bool
	flagQuiet   bool
	flagJSON    bool
)

const scenarioSchemaDirective = "# yaml-language-server: $schema=https://gauntlet.dev/schema/scenario.json"

func main() {
	rootCmd := buildRootCmd()
	if err := rootCmd.Execute(); err != nil {
		emitCLIErrorCode(classifyCLIError(err))
		os.Exit(exitCodeForError(err))
	}
}

func buildRootCmd() *cobra.Command {
	var policyStrict bool
	rootCmd := &cobra.Command{
		Use:   "gauntlet",
		Short: "Deterministic scenario testing for agentic systems",
		Long:  "Gauntlet freezes the world and tests your agent's behavior against that frozen world.",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if !cmd.Flags().Changed("policy-strict") {
				return
			}
			if policyStrict {
				_ = os.Setenv("GAUNTLET_POLICY_STRICT", "true")
				return
			}
			_ = os.Unsetenv("GAUNTLET_POLICY_STRICT")
		},
	}
	rootCmd.PersistentFlags().BoolVar(&policyStrict, "policy-strict", false, "Enable strict policy parsing (error on unknown keys)")
	rootCmd.PersistentFlags().BoolVarP(&flagVerbose, "verbose", "v", false, "Include per-assertion details and fixture/world resolution logs")
	rootCmd.PersistentFlags().BoolVarP(&flagQuiet, "quiet", "q", false, "Only print failures and final verdict")
	rootCmd.PersistentFlags().BoolVar(&flagJSON, "json", false, "Emit structured JSON output")

	rootCmd.Version = version

	rootCmd.AddCommand(
		newRunCmd(),
		newValidateCmd(),
		newDoctorCmd(),
		newEnableCmd(),
		newInitCmd(),
		newBaselineCmd(),
		newCheckBaselineApprovalCmd(),
		newRecordCmd(),
		newCaptureCmd(),
		newMigrateFixturesCmd(),
		newLockFixturesCmd(),
		newScanFixturesCmd(),
		newScaffoldCmd(),
		newScanArtifactsCmd(),
		newSignArtifactsCmd(),
		newDiscoverCmd(),
		newReviewCmd(),
	)
	return rootCmd
}

func newRunCmd() *cobra.Command {
	var (
		suite            string
		configPath       string
		scenarioFilter   string
		runnerMode       string
		mode             string
		modelMode        string
		proxyAddr        string
		autoDiscover     bool
		discoverForce    bool
		dryRun           bool
		failFast         bool
		outputDir        string
		budgetMs         int64
		scenarioBudgetMs int64
	)

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run a scenario suite",
		RunE: func(cmd *cobra.Command, args []string) error {
			if flagVerbose && flagQuiet {
				return fmt.Errorf("--verbose and --quiet cannot be used together")
			}

			runSuite := resolveSuitePathForRun(suite, configPath, cmd.Flags().Changed("config"))
			suite = runSuite.SuiteName
			configPath = runSuite.ConfigPath

			resolved, err := loadPolicyIfPresent(configPath, suite, cmd.Flags().Changed("config"))
			if err != nil {
				return err
			}

			if !runSuite.FromPath && resolved == nil && !cmd.Flags().Changed("config") {
				if _, statErr := os.Stat("evals"); os.IsNotExist(statErr) {
					fmt.Println("No gauntlet.yaml or evals/ directory found.")
					fmt.Println()
					fmt.Println("Get started:")
					fmt.Println("  gauntlet init       Generate CI workflow and policy files")
					fmt.Println("  gauntlet discover   Scan your codebase for tools and scenarios")
					fmt.Println("  gauntlet run        Run the test suite")
					fmt.Println()
					fmt.Println("See: docs/quickstart.md")
					return nil
				}
			}

			mode, err = resolveRunnerMode(runnerMode, mode, resolved)
			if err != nil {
				return err
			}
			if !cmd.Flags().Changed("budget") && resolved != nil && resolved.BudgetMs > 0 {
				budgetMs = resolved.BudgetMs
			}

			cfg := runner.Config{
				Suite:            suite,
				ConfigPath:       configPath,
				Mode:             mode,
				OutputDir:        outputDir,
				DryRun:           dryRun,
				BudgetMs:         budgetMs,
				ScenarioBudgetMs: scenarioBudgetMs,
				ScenarioFilter:   scenarioFilter,
			}
			applyResolvedPolicy(&cfg, resolved, configPath)
			if runSuite.FromPath {
				cfg.Suite = runSuite.SuiteName
				cfg.SuiteDir = runSuite.SuiteDir
				cfg.EvalsDir = runSuite.EvalsDir
			}
			if cmd.Flags().Changed("fail-fast") {
				cfg.FailFast = failFast
			}
			if autoDiscover {
				autoResult, err := ensureAutoDiscoverySuite(cfg, discoverForce)
				if err != nil {
					return fmt.Errorf("auto-discovery failed for suite %s: %w\n  Disable with: --auto-discover=false", cfg.Suite, err)
				}
				if autoResult.GeneratedScenarios > 0 {
					fmt.Printf("Auto-discovery generated %d scenarios for suite %s\n", autoResult.GeneratedScenarios, cfg.Suite)
				} else if autoResult.Skipped {
					fmt.Printf("Auto-discovery skipped scenario generation (%s)\n", autoResult.SkipReason)
				}
			}

			var adapter tut.Adapter
			if cfg.TUTConfig.Command != "" {
				adapter = selectAdapter(cfg.TUTConfig)
			}

			var p *proxy.Proxy
			if !cfg.DryRun && adapter != nil {
				p, err = startProxyForRun(&cfg, resolved, modelMode, proxyAddr)
				if err != nil {
					return err
				}
				if p != nil {
					defer func() { _ = p.Stop() }()
				}
			}

			r := runner.NewRunner(cfg)
			r.Adapter = adapter
			result, err := r.Run(context.Background())
			if err != nil {
				return fmt.Errorf("run failed for suite %s: %w", suite, err)
			}

			if flagJSON {
				data, marshalErr := json.MarshalIndent(result, "", "  ")
				if marshalErr != nil {
					return fmt.Errorf("failed to marshal run result to json: %w", marshalErr)
				}
				fmt.Println(string(data))
			} else if flagQuiet {
				if result.Summary.Failed > 0 {
					for _, s := range result.Scenarios {
						if s.Status == "failed" {
							fmt.Printf("FAILED %s\n", s.Name)
						}
					}
				}
				if result.Summary.Error > 0 {
					for _, s := range result.Scenarios {
						if s.Status == "error" {
							fmt.Printf("ERROR %s\n", s.Name)
						}
					}
				}
				fmt.Printf("Verdict: passed=%d failed=%d error=%d skipped=%d\n",
					result.Summary.Passed,
					result.Summary.Failed,
					result.Summary.Error,
					result.Summary.SkippedBudget,
				)
			} else {
				// Print summary
				fmt.Printf("\nGauntlet — %s suite\n", suite)
				fmt.Printf("  Passed:  %d\n", result.Summary.Passed)
				fmt.Printf("  Failed:  %d\n", result.Summary.Failed)
				fmt.Printf("  Skipped: %d\n", result.Summary.SkippedBudget)
				fmt.Printf("  Errors:  %d\n", result.Summary.Error)
				fmt.Printf("  Duration: %dms / %dms budget\n", result.DurationMs, result.BudgetMs)

				if result.Summary.Failed > 0 {
					fmt.Println("\nFailed scenarios:")
					for _, s := range result.Scenarios {
						if s.Status == "failed" {
							fmt.Printf("  - %s", s.Name)
							if s.PrimaryTag != "" {
								fmt.Printf(" [%s]", s.PrimaryTag)
							}
							fmt.Println()
						}
					}
				}
				if flagVerbose {
					for _, s := range result.Scenarios {
						fmt.Printf("\nScenario: %s (%s)\n", s.Name, s.Status)
						for _, assertion := range s.Assertions {
							status := "PASS"
							if !assertion.Passed {
								status = "FAIL"
							}
							fmt.Printf("  [%s] %s: %s\n", status, assertion.AssertionType, assertion.Message)
						}
					}
				}
			}

			if result.Summary.Failed > 0 {
				emitCLIErrorCode("scenario_assertion_failed")
				os.Exit(ExitFailure)
			}

			if result.Summary.Error > 0 {
				emitCLIErrorCode("scenario_execution_error")
				os.Exit(ExitError)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&suite, "suite", "smoke", "Suite name or suite directory path")
	cmd.Flags().StringVar(&configPath, "config", "evals/gauntlet.yml", "Path to policy file")
	cmd.Flags().StringVar(&scenarioFilter, "scenario", "", "Run a single scenario by name")
	cmd.Flags().StringVar(&runnerMode, "runner-mode", "", "Runner execution mode (local, pr_ci, fork_pr, nightly)")
	cmd.Flags().StringVar(&mode, "mode", "", "Legacy alias for --runner-mode (local, pr_ci, fork_pr, nightly)")
	cmd.Flags().StringVar(&modelMode, "model-mode", "", "Model replay mode (recorded, live, passthrough)")
	cmd.Flags().StringVar(&proxyAddr, "proxy-addr", "", "Proxy listen address override")
	cmd.Flags().BoolVar(&autoDiscover, "auto-discover", true, "Auto-discover and materialize suite scenarios when needed")
	cmd.Flags().BoolVar(&discoverForce, "discover-force", false, "Force regeneration of auto scenarios even if manual scenarios exist")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Validate scenarios without executing")
	cmd.Flags().BoolVar(&failFast, "fail-fast", false, "Stop after the first failed or error scenario")
	cmd.Flags().StringVar(&outputDir, "output-dir", "", "Output directory for artifacts")
	cmd.Flags().Int64Var(&budgetMs, "budget", 300000, "Wall-clock budget in milliseconds")
	cmd.Flags().Int64Var(&scenarioBudgetMs, "scenario-budget", 0, "Per-scenario timeout budget in milliseconds (defaults to --budget)")

	return cmd
}

func newEnableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "enable",
		Short: "Generate CI workflow and policy files",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, _ := os.Getwd()
			result, err := ci.Enable(cwd)
			if err != nil {
				return err
			}

			fmt.Printf("Generated:\n  %s\n  %s\n", result.WorkflowPath, result.PolicyPath)
			ci.PrintOnboardingChecklist(result.Framework)
			return nil
		},
	}
}

func newBaselineCmd() *cobra.Command {
	var (
		suite          string
		scenarioFilter string
		update         bool
		force          bool
		configPath     string
		proxyAddr      string
		modelMode      string
	)

	cmd := &cobra.Command{
		Use:   "baseline",
		Short: "Generate or update contract baselines from passing runs",
		RunE: func(cmd *cobra.Command, args []string) error {
			if update && ci.DetectMode() != "local" && !force {
				return fmt.Errorf("baseline update in CI requires --force flag\n  Run with: gauntlet baseline --update --force")
			}

			if configPath == "" {
				configPath = filepath.Join("evals", "gauntlet.yml")
			}

			cfg := runner.Config{
				Suite:          suite,
				Mode:           "local",
				ConfigPath:     configPath,
				ScenarioFilter: scenarioFilter,
			}

			resolved, err := loadPolicyIfPresent(configPath, suite, false)
			if err != nil {
				return fmt.Errorf("failed to load policy from %s: %w\n  Ensure the file exists or run: gauntlet enable", configPath, err)
			}
			applyResolvedPolicy(&cfg, resolved, configPath)

			if cfg.TUTConfig.Command == "" {
				return fmt.Errorf("no TUT command configured\n  Set tut.command in %s, e.g.:\n    tut:\n      command: python gauntlet_adapter.py", configPath)
			}

			adapter := selectAdapter(cfg.TUTConfig)

			p, err := startProxyForRun(&cfg, resolved, modelMode, proxyAddr)
			if err != nil {
				return fmt.Errorf("failed to start proxy: %w", err)
			}
			if p != nil {
				defer func() { _ = p.Stop() }()
			}

			// Load scenarios to extract tool_sequence specs for baselines
			scenariosByName := map[string]*scenario.Scenario{}
			if cfg.SuiteDir != "" {
				scenarios, loadErr := scenario.LoadSuite(cfg.SuiteDir)
				if loadErr == nil {
					for _, s := range scenarios {
						scenariosByName[s.Name] = s
					}
				}
			}

			ctx := context.Background()
			r := runner.NewRunner(cfg)
			r.Adapter = adapter
			runResult, err := r.Run(ctx)
			if err != nil {
				return fmt.Errorf("baseline run failed: %w\n  Fix failing scenarios before generating baselines", err)
			}

			baselineDir := filepath.Join(filepath.Dir(configPath), "baselines")

			saved := 0
			rollbackChanges := make([]baseline.RollbackChange, 0)
			for _, sr := range runResult.Scenarios {
				if sr.Status != "passed" {
					fmt.Printf("  Skipped (not passed): %s [%s]\n", sr.Name, sr.Status)
					continue
				}

				// Extract required tool names from the scenario's assertion specs
				var required []string
				if s, ok := scenariosByName[sr.Name]; ok {
					for _, aSpec := range s.Assertions {
						if aSpec.Type == "tool_sequence" {
							if reqList, ok := aSpec.Raw["required"]; ok {
								if items, ok := reqList.([]interface{}); ok {
									for _, item := range items {
										if name, ok := item.(string); ok {
											required = append(required, name)
										}
									}
								}
							}
						}
					}
				}

				// Extract output-related fields from scenario assertions
				var outputSchema map[string]interface{}
				var requiredFields []string
				var forbiddenContent []string
				if s, ok := scenariosByName[sr.Name]; ok {
					for _, aSpec := range s.Assertions {
						if aSpec.Type == "output_schema" {
							if schemaRaw, ok := aSpec.Raw["schema"]; ok {
								if m, ok := schemaRaw.(map[string]interface{}); ok {
									outputSchema = m
								}
							}
						}
						if aSpec.Type == "forbidden_content" {
							if pattern, ok := aSpec.Raw["pattern"]; ok {
								if p, ok := pattern.(string); ok && p != "" {
									forbiddenContent = append(forbiddenContent, p)
								}
							}
						}
					}
					if outputSchema != nil {
						if reqRaw, ok := outputSchema["required"]; ok {
							if items, ok := reqRaw.([]interface{}); ok {
								for _, item := range items {
									if name, ok := item.(string); ok {
										requiredFields = append(requiredFields, name)
									}
								}
							}
						}
					}
				}

				var output *baseline.OutputBaseline
				if outputSchema != nil || len(requiredFields) > 0 || len(forbiddenContent) > 0 {
					output = &baseline.OutputBaseline{
						Schema:           outputSchema,
						RequiredFields:   requiredFields,
						ForbiddenContent: forbiddenContent,
					}
				}

				contract := &baseline.Contract{
					BaselineType: "contract",
					Scenario:     sr.Name,
					RecordedAt:   runResult.StartedAt.Format("2006-01-02T15:04:05Z"),
					Commit:       runResult.Commit,
					ToolSequence: &baseline.ToolSequenceBaseline{
						Required: required,
						Order:    "partial",
					},
					Output: output,
				}

				existing, _ := baseline.Load(baselineDir, suite, sr.Name)
				if existing != nil && !update {
					fmt.Printf("  Skipped (exists): %s\n", sr.Name)
					continue
				}

				baselinePath := filepath.Join(baselineDir, suite, sr.Name+".json")
				previousHash, hadPrevious, hashErr := baseline.FileSHA256(baselinePath)
				if hashErr != nil {
					return fmt.Errorf("failed to hash existing baseline %s: %w", baselinePath, hashErr)
				}
				if err := baseline.Save(baselineDir, suite, contract); err != nil {
					return fmt.Errorf("failed to save baseline for %s: %w", sr.Name, err)
				}
				currentHash, _, hashErr := baseline.FileSHA256(baselinePath)
				if hashErr != nil {
					return fmt.Errorf("failed to hash saved baseline %s: %w", baselinePath, hashErr)
				}
				if !hadPrevious || previousHash != currentHash {
					change := baseline.RollbackChange{
						Path:          baselinePath,
						Action:        "created",
						CurrentSHA256: currentHash,
					}
					if hadPrevious {
						change.Action = "updated"
						change.PreviousSHA256 = previousHash
					}
					rollbackChanges = append(rollbackChanges, change)
				}
				fmt.Printf("  Saved baseline: %s\n", sr.Name)
				saved++
			}

			fmt.Printf("\nBaseline generation complete: %d saved, %d total scenarios\n", saved, len(runResult.Scenarios))
			if len(rollbackChanges) > 0 {
				rollbackManifestPath, rollbackTemplatePath, err := writeBaselineRollbackArtifacts(
					baselineDir,
					suite,
					rollbackChanges,
					time.Now().UTC(),
				)
				if err != nil {
					return err
				}
				fmt.Printf("Rollback manifest: %s\n", rollbackManifestPath)
				fmt.Printf("Rollback PR template: %s\n", rollbackTemplatePath)
			}
			if lockPath, lockErr := writeReplayLockfileFromConfig(cfg, cfg.FixturesDir, ""); lockErr == nil {
				fmt.Printf("Replay lockfile updated: %s\n", lockPath)
			} else {
				fmt.Printf("WARN: failed to update replay lockfile: %v\n", lockErr)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&suite, "suite", "smoke", "Suite to baseline")
	cmd.Flags().StringVar(&scenarioFilter, "scenario", "", "Baseline a single scenario")
	cmd.Flags().BoolVar(&update, "update", false, "Overwrite existing baselines")
	cmd.Flags().BoolVar(&force, "force", false, "Force update in CI")
	cmd.Flags().StringVar(&configPath, "config", "", "Path to gauntlet.yml")
	cmd.Flags().StringVar(&proxyAddr, "proxy-addr", "", "Proxy address")
	cmd.Flags().StringVar(&modelMode, "model-mode", "", "Model mode (recorded/live/passthrough)")

	return cmd
}

func newRecordCmd() *cobra.Command {
	var (
		suite          string
		scenarioFilter string
		configPath     string
		proxyAddr      string
	)

	cmd := &cobra.Command{
		Use:   "record",
		Short: "Record fixtures from live agent runs",
		Long:  "Run the agent in live mode to capture tool and model fixtures.\nThe proxy records model fixtures; the @gauntlet.tool decorator records tool fixtures.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if configPath == "" {
				configPath = filepath.Join("evals", "gauntlet.yml")
			}

			cfg := runner.Config{
				Suite:          suite,
				Mode:           "local",
				ConfigPath:     configPath,
				ScenarioFilter: scenarioFilter,
			}

			resolved, err := loadPolicyIfPresent(configPath, suite, false)
			if err != nil {
				return fmt.Errorf("failed to load policy from %s: %w\n  Ensure the file exists or run: gauntlet enable", configPath, err)
			}
			applyResolvedPolicy(&cfg, resolved, configPath)

			if cfg.TUTConfig.Command == "" {
				return fmt.Errorf("no TUT command configured\n  Set tut.command in %s, e.g.:\n    tut:\n      command: python gauntlet_adapter.py", configPath)
			}

			adapter := selectAdapter(cfg.TUTConfig)

			// Force live mode for recording
			p, err := startProxyForRun(&cfg, resolved, "live", proxyAddr)
			if err != nil {
				return err
			}
			if p != nil {
				defer func() { _ = p.Stop() }()
			}

			fmt.Printf("Recording fixtures for suite: %s\n", suite)
			fmt.Println("  Model fixtures: captured by proxy (MITM)")
			fmt.Println("  Tool fixtures:  captured by @gauntlet.tool decorator")

			ctx := context.Background()
			r := runner.NewRunner(cfg)
			r.Adapter = adapter
			runResult, err := r.Run(ctx)
			if err != nil {
				// Recording may have partial results — still useful
				fmt.Fprintf(os.Stderr, "Warning: recording run had errors: %v\n", err)
			}

			if runResult != nil {
				fixturesDir := effectiveFixturesDir(&cfg)
				fmt.Printf("\nRecording complete: %d scenarios executed\n", len(runResult.Scenarios))
				fmt.Printf("  Fixtures saved to: %s\n", fixturesDir)
				signingKeyPath := effectiveFixtureSigningKeyPath(configPath)
				signStore := fixture.NewStore(fixturesDir)
				modelsSigned, toolsSigned, signErr := fixture.SignFixtures(signStore, signingKeyPath)
				if signErr != nil {
					return fmt.Errorf("failed to sign recorded fixtures: %w", signErr)
				}
				fmt.Printf("  Fixtures signed: models=%d tools=%d\n", modelsSigned, toolsSigned)
				if lockPath, lockErr := writeReplayLockfileFromConfig(cfg, fixturesDir, ""); lockErr == nil {
					fmt.Printf("  Replay lockfile: %s\n", lockPath)
				} else {
					fmt.Printf("  WARN: failed to update replay lockfile: %v\n", lockErr)
				}
				fmt.Printf("\nNext: run 'gauntlet run --suite %s' to replay with recorded fixtures\n", suite)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&suite, "suite", "smoke", "Suite to record")
	cmd.Flags().StringVar(&scenarioFilter, "scenario", "", "Record a single scenario")
	cmd.Flags().StringVar(&configPath, "config", "", "Path to gauntlet.yml")
	cmd.Flags().StringVar(&proxyAddr, "proxy-addr", "", "Proxy address")

	return cmd
}

func newMigrateFixturesCmd() *cobra.Command {
	var (
		suite        string
		configPath   string
		fixturesDir  string
		fromVersion  int
		toVersion    int
		dryRun       bool
		reportPath   string
		manifestPath string
		signingKey   string
	)

	cmd := &cobra.Command{
		Use:   "migrate-fixtures",
		Short: "Recompute fixture hashes for a new hash version",
		RunE: func(cmd *cobra.Command, args []string) error {
			if fromVersion <= 0 || toVersion <= 0 {
				return fmt.Errorf("hash versions must be positive (from=%d, to=%d)", fromVersion, toVersion)
			}

			cfg := runner.Config{
				Suite:       suite,
				ConfigPath:  configPath,
				FixturesDir: fixturesDir,
			}
			resolved, err := loadPolicyIfPresent(configPath, suite, false)
			if err != nil {
				return fmt.Errorf("failed to load policy from %s: %w", configPath, err)
			}
			applyResolvedPolicy(&cfg, resolved, configPath)

			effectiveFixtures := strings.TrimSpace(fixturesDir)
			if effectiveFixtures == "" {
				effectiveFixtures = effectiveFixturesDir(&cfg)
			}
			store := fixture.NewStore(effectiveFixtures)
			report, err := fixture.MigrateFixtures(store, fixture.MigrationOptions{
				FromVersion:    fromVersion,
				ToVersion:      toVersion,
				DryRun:         dryRun,
				ReportPath:     reportPath,
				ManifestPath:   manifestPath,
				SigningKeyPath: signingKey,
			})
			if err != nil {
				return fmt.Errorf("fixture migration failed: %w", err)
			}

			modeText := "applied"
			if dryRun {
				modeText = "dry-run (no fixture files changed)"
			}
			fmt.Printf("Fixture migration %s\n", modeText)
			fmt.Printf("  Store: %s\n", report.StoreBaseDir)
			fmt.Printf("  Version: %d -> %d\n", report.FromVersion, report.ToVersion)
			fmt.Printf("  Scanned: %d\n", report.Summary.Scanned)
			fmt.Printf("  Eligible: %d\n", report.Summary.Eligible)
			fmt.Printf("  Migrated: %d (rehashed: %d, version-only: %d)\n", report.Summary.Migrated, report.Summary.Rehashed, report.Summary.VersionOnly)
			fmt.Printf("  Skipped (version mismatch): %d\n", report.Summary.SkippedVersion)
			fmt.Printf("  Report: %s\n", report.ReportPath)
			fmt.Printf("  Manifest (signed): %s\n", report.ManifestPath)
			if !dryRun {
				if lockPath, lockErr := writeReplayLockfileFromConfig(cfg, effectiveFixtures, ""); lockErr == nil {
					fmt.Printf("  Replay lockfile: %s\n", lockPath)
				} else {
					fmt.Printf("  WARN: failed to refresh replay lockfile: %v\n", lockErr)
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&suite, "suite", "smoke", "Suite used to resolve policy paths")
	cmd.Flags().StringVar(&configPath, "config", "evals/gauntlet.yml", "Path to policy file")
	cmd.Flags().StringVar(&fixturesDir, "fixtures-dir", "", "Fixture directory override (defaults to policy fixtures_dir)")
	cmd.Flags().IntVar(&fromVersion, "from-version", 1, "Current fixture hash version")
	cmd.Flags().IntVar(&toVersion, "to-version", 2, "Target fixture hash version")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Generate migration report/manifest without mutating fixture files")
	cmd.Flags().StringVar(&reportPath, "report-out", "", "Path to write migration report JSON")
	cmd.Flags().StringVar(&manifestPath, "manifest-out", "", "Path to write signed migration manifest JSON")
	cmd.Flags().StringVar(&signingKey, "signing-key", "", "Ed25519 PKCS#8 PEM key for manifest signing")

	return cmd
}

func newLockFixturesCmd() *cobra.Command {
	var (
		suite       string
		configPath  string
		fixturesDir string
		outPath     string
	)

	cmd := &cobra.Command{
		Use:   "lock-fixtures",
		Short: "Generate deterministic replay lockfile for fixtures",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := runner.Config{
				Suite:       suite,
				ConfigPath:  configPath,
				FixturesDir: fixturesDir,
			}
			resolved, err := loadPolicyIfPresent(configPath, suite, false)
			if err != nil {
				return fmt.Errorf("failed to load policy from %s: %w", configPath, err)
			}
			applyResolvedPolicy(&cfg, resolved, configPath)

			lockPath, err := writeReplayLockfileFromConfig(cfg, fixturesDir, outPath)
			if err != nil {
				return fmt.Errorf("failed to generate replay lockfile: %w", err)
			}
			fmt.Printf("Replay lockfile generated: %s\n", lockPath)
			return nil
		},
	}

	cmd.Flags().StringVar(&suite, "suite", "smoke", "Suite for context binding")
	cmd.Flags().StringVar(&configPath, "config", "evals/gauntlet.yml", "Path to policy file")
	cmd.Flags().StringVar(&fixturesDir, "fixtures-dir", "", "Fixture directory override")
	cmd.Flags().StringVar(&outPath, "out", "", "Replay lockfile output path")

	return cmd
}

func newScaffoldCmd() *cobra.Command {
	var (
		evalsDir  string
		overwrite bool
	)
	cmd := &cobra.Command{
		Use:   "scaffold",
		Short: "Generate wrapper code and world definitions from discovered tools",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("failed to get working directory: %w", err)
			}

			if evalsDir == "" {
				evalsDir = "evals"
			}

			// Load proposals from discovery
			proposalsPath := filepath.Join(evalsDir, "proposals.yaml")
			proposals, err := discovery.LoadProposals(proposalsPath)
			if err != nil {
				return fmt.Errorf("failed to load proposals from %s: %w\n  Run first: gauntlet discover --output %s", proposalsPath, err, proposalsPath)
			}
			if len(proposals) == 0 {
				return fmt.Errorf("no proposals found in %s\n  Run first: gauntlet discover --python-dirs . --output %s", proposalsPath, proposalsPath)
			}

			result, err := scaffold.Generate(scaffold.Config{
				RootDir:   cwd,
				EvalsDir:  evalsDir,
				Proposals: proposals,
				Overwrite: overwrite,
			})
			if err != nil {
				return fmt.Errorf("scaffold failed: %w", err)
			}

			if result.WrapperPath != "" {
				fmt.Printf("  Generated wrapper: %s\n", result.WrapperPath)
			}
			if result.AdapterPath != "" {
				fmt.Printf("  Generated adapter: %s\n", result.AdapterPath)
			}
			for _, f := range result.WorldFiles {
				fmt.Printf("  Generated world:   %s\n", f)
			}
			for _, f := range result.SkippedFiles {
				fmt.Printf("  Skipped (exists):  %s\n", f)
			}
			fmt.Println("\nNext steps:")
			fmt.Println("  1. Review gauntlet_tools.py and wire real imports")
			fmt.Println("  2. Wire your agent into gauntlet_adapter.py")
			fmt.Println("  3. Run: gauntlet record --suite smoke")
			return nil
		},
	}
	cmd.Flags().StringVar(&evalsDir, "evals-dir", "", "Evals directory (default: evals)")
	cmd.Flags().BoolVar(&overwrite, "overwrite", false, "Overwrite existing files")
	return cmd
}

func newScanArtifactsCmd() *cobra.Command {
	var (
		dir        string
		configPath string
		suite      string
	)

	cmd := &cobra.Command{
		Use:   "scan-artifacts",
		Short: "Scan evals directory for sensitive content",
		RunE: func(cmd *cobra.Command, args []string) error {
			r := redaction.DefaultRedactor()
			scanOptions := redaction.DefaultScanOptions()
			resolved, err := loadPolicyIfPresent(configPath, suite, false)
			if err != nil {
				return fmt.Errorf("failed to load policy from %s: %w", configPath, err)
			}
			if resolved != nil {
				scanOptions.PromptInjectionDenylist = resolved.PromptInjectionDenylist
			}

			results, err := redaction.ScanDirectoryWithOptions(dir, r, scanOptions)
			if err != nil {
				return fmt.Errorf("scan failed: %w", err)
			}

			if len(results) == 0 {
				fmt.Println("No sensitive content detected.")
				return nil
			}

			fmt.Printf("Found %d sensitive patterns:\n", len(results))
			for _, result := range results {
				fmt.Printf("  %s:%d — %s: %s\n", result.File, result.Line, result.Pattern, result.Match)
			}
			emitCLIErrorCode("artifact_scan_failed")
			os.Exit(1)
			return nil
		},
	}

	cmd.Flags().StringVar(&dir, "dir", "evals", "Directory to scan")
	cmd.Flags().StringVar(&configPath, "config", "evals/gauntlet.yml", "Path to policy file (optional)")
	cmd.Flags().StringVar(&suite, "suite", "smoke", "Suite used to resolve policy settings")

	return cmd
}

type fixtureScanFileFinding struct {
	File     string
	Findings []fixture.SensitiveFinding
}

func newScanFixturesCmd() *cobra.Command {
	var dir string

	cmd := &cobra.Command{
		Use:   "scan-fixtures",
		Short: "Scan recorded fixtures for sensitive data",
		RunE: func(cmd *cobra.Command, args []string) error {
			results, err := scanFixtureDirectory(dir)
			if err != nil {
				return err
			}
			if len(results) == 0 {
				fmt.Println("No sensitive content detected in fixtures.")
				return nil
			}

			total := 0
			fmt.Printf("Sensitive data found in %d fixture file(s):\n", len(results))
			for _, result := range results {
				fmt.Printf("  %s\n", result.File)
				for _, finding := range result.Findings {
					total++
					fmt.Printf("    - field: %s\n", finding.Path)
					fmt.Printf("      pattern: %s\n", finding.Pattern)
					fmt.Printf("      sample: %s\n", finding.Sample)
				}
			}
			emitCLIErrorCode("fixture_sensitive_scan_failed")
			return fmt.Errorf("sensitive fixture data detected (%d finding(s)); remove secrets or rerun record with GAUNTLET_ALLOW_SENSITIVE_FIXTURE=1 for reviewed false positives", total)
		},
	}

	cmd.Flags().StringVar(&dir, "dir", filepath.Join("evals", "fixtures"), "Directory containing fixture JSON files")
	return cmd
}

func scanFixtureDirectory(dir string) ([]fixtureScanFileFinding, error) {
	root := filepath.Clean(strings.TrimSpace(dir))
	if root == "." && strings.TrimSpace(dir) == "" {
		root = filepath.Join("evals", "fixtures")
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("scan fixtures: failed to stat %s: %w", root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("scan fixtures: path %s is not a directory", root)
	}

	paths := []string{}
	if err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if strings.EqualFold(filepath.Ext(d.Name()), ".json") {
			paths = append(paths, path)
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("scan fixtures: walk failed for %s: %w", root, err)
	}
	sort.Strings(paths)

	results := make([]fixtureScanFileFinding, 0)
	for _, path := range paths {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil, fmt.Errorf("scan fixtures: read failed for %s: %w", path, readErr)
		}
		findings, scanErr := fixture.ScanSensitiveJSON(data, "")
		if scanErr != nil {
			return nil, fmt.Errorf("scan fixtures: %s is not valid JSON fixture: %w", path, scanErr)
		}
		if len(findings) == 0 {
			continue
		}
		results = append(results, fixtureScanFileFinding{
			File:     path,
			Findings: findings,
		})
	}
	return results, nil
}

func newSignArtifactsCmd() *cobra.Command {
	var (
		dir         string
		manifestOut string
		signingKey  string
	)

	cmd := &cobra.Command{
		Use:   "sign-artifacts",
		Short: "Sign run artifacts with a cryptographic evidence manifest",
		RunE: func(cmd *cobra.Command, args []string) error {
			manifest, err := output.SignEvidenceBundle(output.EvidenceSignOptions{
				ArtifactDir:    dir,
				ManifestPath:   manifestOut,
				SigningKeyPath: signingKey,
				GeneratedAt:    time.Now().UTC(),
			})
			if err != nil {
				return fmt.Errorf("evidence bundle signing failed: %w", err)
			}

			manifestPath := strings.TrimSpace(manifestOut)
			if manifestPath == "" {
				manifestPath = filepath.Join(filepath.Clean(strings.TrimSpace(dir)), output.DefaultEvidenceManifestName)
			}
			fmt.Println("Evidence bundle signed")
			fmt.Printf("  Artifacts: %s\n", manifest.ArtifactDir)
			fmt.Printf("  Entries: %d\n", len(manifest.Entries))
			fmt.Printf("  Manifest: %s\n", filepath.Clean(manifestPath))
			fmt.Printf("  Key fingerprint: %s\n", manifest.Signature.KeyFingerprint)
			return nil
		},
	}

	cmd.Flags().StringVar(&dir, "dir", filepath.Join("evals", "runs"), "Artifacts directory to sign")
	cmd.Flags().StringVar(&manifestOut, "manifest-out", "", "Path to write signed evidence manifest (default: <dir>/evidence.manifest.json)")
	cmd.Flags().StringVar(&signingKey, "signing-key", "", "Ed25519 PKCS#8 PEM key for evidence signing (default: <dir>/../.gauntlet/evidence-signing-key.pem)")

	return cmd
}

func newDiscoverCmd() *cobra.Command {
	var (
		rootDir      string
		toolDirs     string
		pythonDirs   string
		dbSchemasDir string
		traceDir     string
		excludeTools string
		output       string
	)

	cmd := &cobra.Command{
		Use:   "discover",
		Short: "Auto-discover test proposals from codebase",
		RunE: func(cmd *cobra.Command, args []string) error {
			if rootDir == "" {
				var err error
				rootDir, err = os.Getwd()
				if err != nil {
					return fmt.Errorf("failed to determine working directory: %w", err)
				}
			}
			cfg := discovery.DiscoveryConfig{
				RootDir:      rootDir,
				ToolDirs:     splitCSVFlag(toolDirs),
				PythonDirs:   splitCSVFlag(pythonDirs),
				DBSchemaDir:  dbSchemasDir,
				TraceDir:     traceDir,
				ExcludeTools: splitCSVFlag(excludeTools),
			}
			engine := discovery.NewEngine(cfg)
			proposals, err := engine.Discover()
			if err != nil {
				return err
			}

			fmt.Printf("Discovered %d test proposals\n", len(proposals))
			for _, p := range proposals {
				fmt.Printf("  %s — %s\n", p.Name, p.Description)
			}

			if output != "" {
				if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
					return fmt.Errorf("failed to create directory for %s: %w", output, err)
				}
				if err := discovery.SaveProposals(proposals, output); err != nil {
					return fmt.Errorf("failed to save proposals to %s: %w", output, err)
				}
				fmt.Printf("Saved to %s\n", output)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&rootDir, "root", "", "Project root directory")
	cmd.Flags().StringVar(&toolDirs, "tools", "evals/world/tools", "Comma-separated tool definition directories")
	cmd.Flags().StringVar(&pythonDirs, "python-dirs", ".", "Comma-separated Python directories to scan for @gauntlet.tool")
	cmd.Flags().StringVar(&dbSchemasDir, "db-schemas", "", "Database schema/definition directory")
	cmd.Flags().StringVar(&traceDir, "trace-dir", "", "Trace directory for trace-aware discovery")
	cmd.Flags().StringVar(&excludeTools, "exclude-tools", "", "Comma-separated tool names to exclude")
	cmd.Flags().StringVar(&output, "output", "evals/proposals.yaml", "Save proposals to file")

	return cmd
}

func splitCSVFlag(raw string) []string {
	parts := strings.Split(raw, ",")
	seen := map[string]bool{}
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func writeBaselineRollbackArtifacts(baselineDir, suite string, changes []baseline.RollbackChange, generatedAt time.Time) (string, string, error) {
	rollbackManifestPath := filepath.Join(baselineDir, suite, "rollback.manifest.json")
	rollbackTemplatePath := filepath.Join(baselineDir, suite, "ROLLBACK_PR_TEMPLATE.md")
	rollbackManifest := &baseline.RollbackManifest{
		ManifestVersion:       1,
		Suite:                 suite,
		GeneratedAt:           generatedAt.UTC().Format(time.RFC3339),
		BaseRef:               "origin/main",
		RequiredApprovalLabel: baseline.DefaultBaselineApprovalLabel,
		Changes:               changes,
	}
	if err := baseline.WriteRollbackManifest(rollbackManifestPath, rollbackManifest); err != nil {
		return "", "", fmt.Errorf("failed to write rollback manifest: %w", err)
	}
	if err := baseline.WriteRollbackTemplate(rollbackTemplatePath, rollbackManifest); err != nil {
		return "", "", fmt.Errorf("failed to write rollback PR template: %w", err)
	}
	return rollbackManifestPath, rollbackTemplatePath, nil
}

func writeReplayLockfileFromConfig(cfg runner.Config, fixturesDir, outPath string) (string, error) {
	fixturesDir = strings.TrimSpace(fixturesDir)
	if fixturesDir == "" {
		fixturesDir = effectiveFixturesDir(&cfg)
	}
	scenarioDigest := computeScenarioSetDigest(cfg.SuiteDir)
	store := fixture.NewStore(fixturesDir)
	_, lockPath, err := fixture.WriteReplayLockfile(store, cfg.Suite, scenarioDigest, outPath, time.Now().UTC())
	if err != nil {
		return "", err
	}
	return lockPath, nil
}

func ensureAutoDiscoverySuite(cfg runner.Config, force bool) (*discovery.AutoSuiteResult, error) {
	evalsDir := cfg.EvalsDir
	if strings.TrimSpace(evalsDir) == "" {
		evalsDir = "evals"
	}
	suiteDir := cfg.SuiteDir
	if strings.TrimSpace(suiteDir) == "" {
		suiteDir = filepath.Join(evalsDir, cfg.Suite)
	}
	toolsDir := cfg.ToolsDir
	if strings.TrimSpace(toolsDir) == "" {
		toolsDir = filepath.Join(evalsDir, "world", "tools")
	}
	dbDir := cfg.DBDir
	if strings.TrimSpace(dbDir) == "" {
		dbDir = filepath.Join(evalsDir, "world", "databases")
	}

	rootDir, err := os.Getwd()
	if err != nil {
		rootDir = "."
	}
	pythonDirs := []string{"."}
	if wd := strings.TrimSpace(cfg.TUTConfig.WorkDir); wd != "" && wd != "." {
		pythonDirs = append(pythonDirs, wd)
	}

	return discovery.EnsureAutoSuite(discovery.AutoSuiteConfig{
		RootDir:       rootDir,
		EvalsDir:      evalsDir,
		Suite:         cfg.Suite,
		SuiteDir:      suiteDir,
		ToolsDir:      toolsDir,
		DBDir:         dbDir,
		PairsDir:      filepath.Join(evalsDir, "pairs"),
		ProposalsPath: filepath.Join(evalsDir, "proposals.yaml"),
		PythonDirs:    pythonDirs,
		Force:         force,
	})
}

func newReviewCmd() *cobra.Command {
	var (
		addr      string
		evalsDir  string
		staticDir string
	)

	cmd := &cobra.Command{
		Use:   "review",
		Short: "Start the Gauntlet review UI",
		RunE: func(cmd *cobra.Command, args []string) error {
			var staticFS fs.FS
			if staticDir != "" {
				staticFS = os.DirFS(staticDir)
			} else {
				staticFS = getEmbeddedUI()
			}
			if staticFS == nil {
				fmt.Println("WARN: no UI available. Build with `make build` or use --static.")
			}
			srv := api.NewServer(addr, evalsDir, staticFS)
			fmt.Printf("Gauntlet review UI: http://%s\n", addr)
			return srv.Start()
		},
	}

	cmd.Flags().StringVar(&addr, "addr", "localhost:7432", "Address to listen on")
	cmd.Flags().StringVar(&evalsDir, "evals", "evals", "Evals directory")
	cmd.Flags().StringVar(&staticDir, "static", "", "Static files directory for UI")

	return cmd
}
