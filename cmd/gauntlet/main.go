package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/gauntlet-dev/gauntlet/internal/api"
	"github.com/gauntlet-dev/gauntlet/internal/ci"
	"github.com/gauntlet-dev/gauntlet/internal/discovery"
	"github.com/gauntlet-dev/gauntlet/internal/proxy"
	"github.com/gauntlet-dev/gauntlet/internal/redaction"
	"github.com/gauntlet-dev/gauntlet/internal/runner"
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
				fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
				return err
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
		suite    string
		scenario string
		update   bool
		force    bool
	)

	cmd := &cobra.Command{
		Use:   "baseline",
		Short: "Generate or update contract baselines",
		RunE: func(cmd *cobra.Command, args []string) error {
			if update && ci.DetectMode() != "local" && !force {
				return fmt.Errorf("baseline update in CI requires --force flag")
			}
			fmt.Printf("Generating baselines for suite: %s\n", suite)
			// TODO: implement baseline generation from passing runs
			return nil
		},
	}

	cmd.Flags().StringVar(&suite, "suite", "smoke", "Suite to baseline")
	cmd.Flags().StringVar(&scenario, "scenario", "", "Baseline a single scenario")
	cmd.Flags().BoolVar(&update, "update", false, "Overwrite existing baselines")
	cmd.Flags().BoolVar(&force, "force", false, "Force update in CI")

	return cmd
}

func newRecordCmd() *cobra.Command {
	var (
		suite    string
		scenario string
	)

	cmd := &cobra.Command{
		Use:   "record",
		Short: "Record fixtures from live agent runs",
		RunE: func(cmd *cobra.Command, args []string) error {
			if os.Getenv("GAUNTLET_MODEL_MODE") != "live" {
				return fmt.Errorf("recording requires GAUNTLET_MODEL_MODE=live\n  Run: GAUNTLET_MODEL_MODE=live gauntlet record --suite %s", suite)
			}
			fmt.Printf("Recording fixtures for suite: %s\n", suite)
			// TODO: implement live recording
			return nil
		},
	}

	cmd.Flags().StringVar(&suite, "suite", "smoke", "Suite to record")
	cmd.Flags().StringVar(&scenario, "scenario", "", "Record a single scenario")

	return cmd
}

func newScaffoldCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "scaffold",
		Short: "Generate a starter scenario from a template",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("Generating starter scenario...")
			// TODO: implement scaffolding
			return nil
		},
	}
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
		rootDir  string
		toolDirs string
		output   string
	)

	cmd := &cobra.Command{
		Use:   "discover",
		Short: "Auto-discover test proposals from codebase",
		RunE: func(cmd *cobra.Command, args []string) error {
			if rootDir == "" {
				rootDir, _ = os.Getwd()
			}
			cfg := discovery.DiscoveryConfig{
				RootDir:  rootDir,
				ToolDirs: []string{toolDirs},
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
				if err := discovery.SaveProposals(proposals, output); err != nil {
					return fmt.Errorf("failed to save proposals: %w", err)
				}
				fmt.Printf("Saved to %s\n", output)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&rootDir, "root", "", "Project root directory")
	cmd.Flags().StringVar(&toolDirs, "tools", "evals/world/tools", "Tool definitions directory")
	cmd.Flags().StringVar(&output, "output", "", "Save proposals to file")

	return cmd
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
