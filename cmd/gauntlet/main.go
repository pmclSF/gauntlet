package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gauntlet-dev/gauntlet/internal/api"
	"github.com/gauntlet-dev/gauntlet/internal/baseline"
	"github.com/gauntlet-dev/gauntlet/internal/ci"
	"github.com/gauntlet-dev/gauntlet/internal/discovery"
	"github.com/gauntlet-dev/gauntlet/internal/proxy"
	"github.com/gauntlet-dev/gauntlet/internal/redaction"
	"github.com/gauntlet-dev/gauntlet/internal/runner"
	"github.com/gauntlet-dev/gauntlet/internal/scaffold"
	"github.com/gauntlet-dev/gauntlet/internal/scenario"
	"github.com/gauntlet-dev/gauntlet/internal/tut"
)

var version = "0.1.0"

func main() {
	rootCmd := &cobra.Command{
		Use:   "gauntlet",
		Short: "Deterministic scenario testing for agentic systems",
		Long:  "Gauntlet freezes the world and tests your agent's behavior against that frozen world.",
	}

	rootCmd.Version = version

	rootCmd.AddCommand(
		newRunCmd(),
		newEnableCmd(),
		newBaselineCmd(),
		newRecordCmd(),
		newScaffoldCmd(),
		newScanArtifactsCmd(),
		newDiscoverCmd(),
		newReviewCmd(),
	)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func newRunCmd() *cobra.Command {
	var (
		suite          string
		configPath     string
		scenarioFilter string
		mode           string
		modelMode      string
		proxyAddr      string
		autoDiscover   bool
		discoverForce  bool
		dryRun         bool
		outputDir      string
		budgetMs       int64
	)

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run a scenario suite",
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := loadPolicyIfPresent(configPath, suite, cmd.Flags().Changed("config"))
			if err != nil {
				return err
			}

			if mode == "" && resolved != nil && resolved.RunnerMode != "" {
				mode = resolved.RunnerMode
			}
			if mode == "" {
				mode = ci.DetectMode()
			}
			if !cmd.Flags().Changed("budget") && resolved != nil && resolved.BudgetMs > 0 {
				budgetMs = resolved.BudgetMs
			}

			cfg := runner.Config{
				Suite:          suite,
				ConfigPath:     configPath,
				Mode:           mode,
				OutputDir:      outputDir,
				DryRun:         dryRun,
				BudgetMs:       budgetMs,
				ScenarioFilter: scenarioFilter,
			}
			applyResolvedPolicy(&cfg, resolved, configPath)
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
				os.Exit(1)
			}

			if result.Summary.Error > 0 {
				os.Exit(2)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&suite, "suite", "smoke", "Suite to run")
	cmd.Flags().StringVar(&configPath, "config", "evals/gauntlet.yml", "Path to policy file")
	cmd.Flags().StringVar(&scenarioFilter, "scenario", "", "Run a single scenario by name")
	cmd.Flags().StringVar(&mode, "mode", "", "Execution mode (pr_ci, nightly, local, fork_pr)")
	cmd.Flags().StringVar(&modelMode, "model-mode", "", "Model replay mode (recorded, live, passthrough)")
	cmd.Flags().StringVar(&proxyAddr, "proxy-addr", "", "Proxy listen address override")
	cmd.Flags().BoolVar(&autoDiscover, "auto-discover", true, "Auto-discover and materialize suite scenarios when needed")
	cmd.Flags().BoolVar(&discoverForce, "discover-force", false, "Force regeneration of auto scenarios even if manual scenarios exist")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Validate scenarios without executing")
	cmd.Flags().StringVar(&outputDir, "output-dir", "", "Output directory for artifacts")
	cmd.Flags().Int64Var(&budgetMs, "budget", 300000, "Wall-clock budget in milliseconds")

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
				defer p.Stop()
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

				contract := &baseline.Contract{
					BaselineType: "contract",
					Scenario:     sr.Name,
					RecordedAt:   runResult.StartedAt.Format("2006-01-02T15:04:05Z"),
					Commit:       runResult.Commit,
					ToolSequence: &baseline.ToolSequenceBaseline{
						Required: required,
						Order:    "partial",
					},
				}

				existing, _ := baseline.Load(baselineDir, suite, sr.Name)
				if existing != nil && !update {
					fmt.Printf("  Skipped (exists): %s\n", sr.Name)
					continue
				}

				if err := baseline.Save(baselineDir, suite, contract); err != nil {
					return fmt.Errorf("failed to save baseline for %s: %w", sr.Name, err)
				}
				fmt.Printf("  Saved baseline: %s\n", sr.Name)
				saved++
			}

			fmt.Printf("\nBaseline generation complete: %d saved, %d total scenarios\n", saved, len(runResult.Scenarios))
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
				defer p.Stop()
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
				fixturesDir := filepath.Join(filepath.Dir(configPath), "fixtures")
				fmt.Printf("\nRecording complete: %d scenarios executed\n", len(runResult.Scenarios))
				fmt.Printf("  Fixtures saved to: %s\n", fixturesDir)
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
	var dir string

	cmd := &cobra.Command{
		Use:   "scan-artifacts",
		Short: "Scan evals directory for sensitive content",
		RunE: func(cmd *cobra.Command, args []string) error {
			r := redaction.DefaultRedactor()
			results, err := redaction.ScanDirectory(dir, r)
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
			os.Exit(1)
			return nil
		},
	}

	cmd.Flags().StringVar(&dir, "dir", "evals", "Directory to scan")

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
	cmd.Flags().StringVar(&output, "output", "", "Save proposals to file")

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
			srv := api.NewServer(addr, evalsDir, staticDir)
			fmt.Printf("Gauntlet review UI: http://%s\n", addr)
			return srv.Start()
		},
	}

	cmd.Flags().StringVar(&addr, "addr", "localhost:7432", "Address to listen on")
	cmd.Flags().StringVar(&evalsDir, "evals", "evals", "Evals directory")
	cmd.Flags().StringVar(&staticDir, "static", "", "Static files directory for UI")

	return cmd
}
