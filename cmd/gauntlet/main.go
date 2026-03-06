package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/pmclSF/gauntlet/internal/api"
	"github.com/pmclSF/gauntlet/internal/assertions"
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
	"github.com/pmclSF/gauntlet/internal/world"
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
			resolved, err := loadPolicyIfPresent(configPath, suite, cmd.Flags().Changed("config"))
			if err != nil {
				return err
			}

			if resolved == nil && !cmd.Flags().Changed("config") {
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

	cmd.Flags().StringVar(&suite, "suite", "smoke", "Suite to run")
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

func newValidateCmd() *cobra.Command {
	var (
		suite    string
		evalsDir string
		toolsDir string
		dbDir    string
	)

	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate scenario files without executing agent code",
		RunE: func(cmd *cobra.Command, args []string) error {
			suiteDir := resolveSuitePathForValidate(suite, evalsDir)
			evalsRoot := filepath.Dir(suiteDir)
			if toolsDir == "" {
				toolsDir = filepath.Join(evalsRoot, "world", "tools")
			}
			if dbDir == "" {
				dbDir = filepath.Join(evalsRoot, "world", "databases")
			}

			report, err := validateSuiteFiles(suiteDir, toolsDir, dbDir)
			if err != nil {
				emitCLIErrorCode("invalid_input")
				return err
			}

			filePaths := make([]string, 0, len(report))
			for path := range report {
				filePaths = append(filePaths, path)
			}
			sort.Strings(filePaths)

			errorCount := 0
			for _, path := range filePaths {
				errorsForFile := report[path]
				if len(errorsForFile) == 0 {
					fmt.Printf("✓ %s — valid\n", path)
					continue
				}
				errorCount += len(errorsForFile)
				fmt.Printf("✗ %s — %d errors\n", path, len(errorsForFile))
				for _, issue := range errorsForFile {
					fmt.Printf("  %s\n", issue)
				}
			}

			if errorCount > 0 {
				emitCLIErrorCode("invalid_input")
				return fmt.Errorf("validation failed: %d errors", errorCount)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&suite, "suite", "smoke", "Suite name or suite directory path")
	cmd.Flags().StringVar(&evalsDir, "evals-dir", "evals", "Root evals directory when --suite is a suite name")
	cmd.Flags().StringVar(&toolsDir, "tools-dir", "", "Tool world directory override")
	cmd.Flags().StringVar(&dbDir, "db-dir", "", "Database world directory override")
	return cmd
}

func resolveSuitePathForValidate(suite, evalsDir string) string {
	if strings.TrimSpace(suite) == "" {
		suite = "smoke"
	}
	if stat, err := os.Stat(suite); err == nil && stat.IsDir() {
		return suite
	}
	return filepath.Join(evalsDir, suite)
}

func ensureScenarioSchemaDirective(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	content := string(data)
	if strings.HasPrefix(content, scenarioSchemaDirective) {
		return nil
	}
	updated := scenarioSchemaDirective + "\n" + content
	return os.WriteFile(path, []byte(updated), 0o644)
}

func validateSuiteFiles(suiteDir, toolsDir, dbDir string) (map[string][]string, error) {
	entries, err := os.ReadDir(suiteDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read suite directory %s: %w", suiteDir, err)
	}
	filePaths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml") {
			filePaths = append(filePaths, filepath.Join(suiteDir, name))
		}
	}
	sort.Strings(filePaths)
	if len(filePaths) == 0 {
		return nil, fmt.Errorf("no scenario files found in %s", suiteDir)
	}

	ws, err := world.Assemble(toolsDir, dbDir, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to assemble world definitions: %w", err)
	}

	report := make(map[string][]string, len(filePaths))
	scenarioNameToPath := make(map[string]string)
	type loadedScenario struct {
		Path     string
		Scenario *scenario.Scenario
	}
	loaded := make([]loadedScenario, 0, len(filePaths))

	for _, path := range filePaths {
		if err := ensureScenarioSchemaDirective(path); err != nil {
			report[path] = append(report[path], fmt.Sprintf("failed to inject schema directive: %v", err))
			continue
		}
		sc, loadErr := scenario.LoadFile(path)
		if loadErr != nil {
			report[path] = append(report[path], loadErr.Error())
			continue
		}
		if firstPath, exists := scenarioNameToPath[sc.Name]; exists {
			report[path] = append(report[path], fmt.Sprintf(
				"duplicate scenario name %q (already defined in %s:1)",
				sc.Name,
				firstPath,
			))
		} else {
			scenarioNameToPath[sc.Name] = path
		}
		for _, spec := range sc.Assertions {
			if _, ok := assertions.Get(spec.Type); !ok {
				report[path] = append(report[path], fmt.Sprintf("unknown assertion type %q", spec.Type))
			}
		}
		loaded = append(loaded, loadedScenario{Path: path, Scenario: sc})
	}

	for _, item := range loaded {
		if issues := validateScenarioWorldRefs(item.Scenario, ws); len(issues) > 0 {
			report[item.Path] = append(report[item.Path], issues...)
		}
		if _, exists := report[item.Path]; !exists {
			report[item.Path] = nil
		}
	}

	return report, nil
}

func validateScenarioWorldRefs(s *scenario.Scenario, ws *world.State) []string {
	if s == nil || ws == nil {
		return nil
	}
	availableTools := make([]string, 0, len(ws.Tools))
	for toolName := range ws.Tools {
		availableTools = append(availableTools, toolName)
	}
	sort.Strings(availableTools)

	var issues []string
	for toolName, stateName := range s.World.Tools {
		toolDef, ok := ws.Tools[toolName]
		if !ok {
			issues = append(issues, fmt.Sprintf(
				"scenario %q: tool %q not found in world definitions (available tools: %s)",
				s.Name,
				toolName,
				strings.Join(availableTools, ", "),
			))
			continue
		}
		if _, ok := toolDef.States[stateName]; !ok {
			states := make([]string, 0, len(toolDef.States))
			for name := range toolDef.States {
				states = append(states, name)
			}
			sort.Strings(states)
			issues = append(issues, fmt.Sprintf(
				"scenario %q: tool %q has no state %q (available states: %s)",
				s.Name,
				toolName,
				stateName,
				strings.Join(states, ", "),
			))
		}
	}
	return issues
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

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Interactive setup for a new Gauntlet-enabled project",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, _ := os.Getwd()
			reader := bufio.NewReader(cmd.InOrStdin())
			out := cmd.OutOrStdout()

			defaultFramework := "OpenAI SDK"
			defaultScenarioDir := "evals/"
			defaultCI := "GitHub Actions"
			defaultEntrypoint := "agent.py"

			framework := defaultFramework
			scenarioDir := defaultScenarioDir
			ciSystem := defaultCI
			entrypoint := defaultEntrypoint

			if isInteractiveInput(cmd.InOrStdin()) {
				fmt.Fprintln(out, "What framework does your agent use?")
				value, err := promptChoice(reader, out, []string{"OpenAI SDK", "Anthropic SDK", "LangChain", "Other (HTTP endpoint)"}, 0)
				if err != nil {
					return err
				}
				framework = value

				fmt.Fprintf(out, "\nWhere should Gauntlet write scenario files? [%s]\n", defaultScenarioDir)
				value, err = promptText(reader, out, defaultScenarioDir)
				if err != nil {
					return err
				}
				scenarioDir = value

				fmt.Fprintln(out, "\nWhat CI system are you using?")
				value, err = promptChoice(reader, out, []string{"GitHub Actions", "GitLab CI", "Other"}, 0)
				if err != nil {
					return err
				}
				ciSystem = value

				fmt.Fprintf(out, "\nWhat's the name of your agent's main entrypoint? [%s]\n", defaultEntrypoint)
				value, err = promptText(reader, out, defaultEntrypoint)
				if err != nil {
					return err
				}
				entrypoint = value
			}

			result, err := ci.Enable(cwd)
			if err != nil {
				return err
			}

			scenarioPath, err := writeInitScenarioFile(cwd, scenarioDir)
			if err != nil {
				return err
			}
			entrypointPath := filepath.Join(cwd, entrypoint)
			lineNumber, err := ensureConnectHook(entrypointPath)
			if err != nil {
				return err
			}

			fmt.Fprintf(out, "\n✓ Created %s\n", result.PolicyPath)
			fmt.Fprintf(out, "✓ Created %s\n", scenarioPath)
			if strings.EqualFold(ciSystem, "GitHub Actions") {
				fmt.Fprintf(out, "✓ Created %s\n", result.WorkflowPath)
			} else {
				fmt.Fprintf(out, "✓ Created %s (GitHub template; adapt for %s)\n", result.WorkflowPath, ciSystem)
			}
			fmt.Fprintf(out, "✓ Added gauntlet.connect() to %s (line %d)\n", entrypoint, lineNumber)
			fmt.Fprintf(out, "\nFramework: %s\n", framework)
			fmt.Fprintln(out, "Next: run `gauntlet record --suite smoke` to capture your first fixtures.")
			return nil
		},
	}
}

func isInteractiveInput(in io.Reader) bool {
	file, ok := in.(*os.File)
	if !ok {
		return false
	}
	stat, err := file.Stat()
	if err != nil {
		return false
	}
	return (stat.Mode() & os.ModeCharDevice) != 0
}

func promptChoice(reader *bufio.Reader, out io.Writer, options []string, defaultIndex int) (string, error) {
	for idx, option := range options {
		prefix := " "
		if idx == defaultIndex {
			prefix = ">"
		}
		fmt.Fprintf(out, "  %s %s\n", prefix, option)
	}
	fmt.Fprint(out, "> ")
	text, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return options[defaultIndex], nil
	}
	if idx, convErr := strconv.Atoi(text); convErr == nil {
		if idx >= 1 && idx <= len(options) {
			return options[idx-1], nil
		}
	}
	for _, option := range options {
		if strings.EqualFold(option, text) {
			return option, nil
		}
	}
	return text, nil
}

func promptText(reader *bufio.Reader, out io.Writer, defaultValue string) (string, error) {
	fmt.Fprint(out, "> ")
	text, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return defaultValue, nil
	}
	return text, nil
}

func writeInitScenarioFile(projectDir, scenarioDir string) (string, error) {
	cleanDir := strings.TrimSpace(scenarioDir)
	if cleanDir == "" {
		cleanDir = "evals/"
	}
	smokeDir := filepath.Join(projectDir, cleanDir, "smoke")
	if err := os.MkdirAll(smokeDir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create scenario directory %s: %w", smokeDir, err)
	}
	path := filepath.Join(smokeDir, "example_scenario.yaml")
	content := scenarioSchemaDirective + `
scenario: example_scenario
description: Generated starter scenario

input:
  messages:
    - role: user
      content: Hello

assertions:
  - type: output_schema
    schema:
      type: object
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("failed to write starter scenario %s: %w", path, err)
	}
	return path, nil
}

func ensureConnectHook(entrypointPath string) (int, error) {
	data, err := os.ReadFile(entrypointPath)
	if err != nil && !os.IsNotExist(err) {
		return 0, fmt.Errorf("failed to read entrypoint %s: %w", entrypointPath, err)
	}

	content := string(data)
	if strings.Contains(content, "gauntlet.connect()") {
		lines := strings.Split(content, "\n")
		for idx, line := range lines {
			if strings.Contains(line, "gauntlet.connect()") {
				return idx + 1, nil
			}
		}
		return 1, nil
	}

	lines := []string{}
	if content != "" {
		lines = strings.Split(content, "\n")
	}
	insertAt := 0
	if len(lines) > 0 && strings.HasPrefix(lines[0], "#!") {
		insertAt = 1
	}
	prefix := []string{"import gauntlet_sdk as gauntlet", "gauntlet.connect()", ""}
	newLines := make([]string, 0, len(lines)+len(prefix))
	newLines = append(newLines, lines[:insertAt]...)
	newLines = append(newLines, prefix...)
	newLines = append(newLines, lines[insertAt:]...)
	newContent := strings.Join(newLines, "\n")
	if strings.TrimSpace(newContent) == "" {
		newContent = "import gauntlet_sdk as gauntlet\ngauntlet.connect()\n"
	}
	if err := os.WriteFile(entrypointPath, []byte(newContent), 0o644); err != nil {
		return 0, fmt.Errorf("failed to update entrypoint %s: %w", entrypointPath, err)
	}
	return insertAt + 2, nil
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

func newCaptureCmd() *cobra.Command {
	var (
		tracePath  string
		outputPath string
	)

	cmd := &cobra.Command{
		Use:   "capture --trace <file> [--output <scenario-path>]",
		Short: "Generate a scenario YAML from a trace file",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(tracePath) == "" {
				return fmt.Errorf("missing required --trace path")
			}

			events, err := loadTraceEvents(tracePath)
			if err != nil {
				return fmt.Errorf("failed to read trace %s: %w", tracePath, err)
			}
			doc, err := buildScenarioFromTrace(tracePath, events)
			if err != nil {
				return fmt.Errorf("failed to build scenario from trace: %w", err)
			}
			rendered, err := yaml.Marshal(doc)
			if err != nil {
				return fmt.Errorf("failed to render captured scenario yaml: %w", err)
			}

			header := scenarioSchemaDirective + "\n# Generated from trace. Review assertions before committing.\n"
			payload := []byte(header + string(rendered))
			if outputPath == "" {
				fmt.Print(string(payload))
				return nil
			}
			if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
				return fmt.Errorf("failed to create output directory for %s: %w", outputPath, err)
			}
			if err := os.WriteFile(outputPath, payload, 0o644); err != nil {
				return fmt.Errorf("failed to write captured scenario to %s: %w", outputPath, err)
			}
			fmt.Printf("Captured scenario written to %s\n", outputPath)
			return nil
		},
	}

	cmd.Flags().StringVar(&tracePath, "trace", "", "Path to trace file (JSON array/object or NDJSON)")
	cmd.Flags().StringVar(&outputPath, "output", "", "Output scenario path (defaults to stdout)")
	return cmd
}

func loadTraceEvents(path string) ([]map[string]interface{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var parsed interface{}
	if err := json.Unmarshal(data, &parsed); err == nil {
		switch v := parsed.(type) {
		case []interface{}:
			return normalizeTraceEvents(v), nil
		case map[string]interface{}:
			if rawEvents, ok := v["events"]; ok {
				if list, ok := rawEvents.([]interface{}); ok {
					return normalizeTraceEvents(list), nil
				}
			}
			return []map[string]interface{}{v}, nil
		}
	}

	// Fallback: NDJSON
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var events []map[string]interface{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event map[string]interface{}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(events) == 0 {
		return nil, fmt.Errorf("trace contained no parseable events")
	}
	return events, nil
}

func normalizeTraceEvents(raw []interface{}) []map[string]interface{} {
	events := make([]map[string]interface{}, 0, len(raw))
	for _, item := range raw {
		event, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		events = append(events, event)
	}
	return events
}

func buildScenarioFromTrace(tracePath string, events []map[string]interface{}) (map[string]interface{}, error) {
	scenarioName := strings.TrimSuffix(filepath.Base(tracePath), filepath.Ext(tracePath))
	if scenarioName == "" {
		scenarioName = "captured_scenario"
	}

	requiredSequence := make([]string, 0)
	worldTools := make(map[string]string)
	seenTools := make(map[string]bool)
	for _, event := range events {
		eventType := firstNonEmptyString(event["event_type"], event["type"])
		if eventType != "tool_call" {
			continue
		}
		toolName := firstNonEmptyString(event["tool_name"], event["tool"])
		if toolName == "" {
			continue
		}
		requiredSequence = append(requiredSequence, toolName)
		if !seenTools[toolName] {
			worldTools[toolName] = "nominal"
			seenTools[toolName] = true
		}
	}

	assertionsList := make([]map[string]interface{}, 0, 2)
	if len(requiredSequence) > 0 {
		assertionsList = append(assertionsList, map[string]interface{}{
			"type":     "tool_sequence",
			"required": requiredSequence,
		})
	}
	assertionsList = append(assertionsList, map[string]interface{}{
		"type": "output_schema",
		"schema": map[string]interface{}{
			"type": "object",
		},
	})

	doc := map[string]interface{}{
		"scenario":    scenarioName,
		"description": "Captured from trace. Update description and assertions.",
		"input": map[string]interface{}{
			"payload": map[string]interface{}{
				"source": "captured_trace",
			},
		},
		"world": map[string]interface{}{
			"tools": worldTools,
		},
		"assertions": assertionsList,
	}
	return doc, nil
}

func firstNonEmptyString(values ...interface{}) string {
	for _, value := range values {
		switch typed := value.(type) {
		case string:
			if strings.TrimSpace(typed) != "" {
				return strings.TrimSpace(typed)
			}
		}
	}
	return ""
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
