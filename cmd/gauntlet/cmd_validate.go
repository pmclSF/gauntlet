package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/pmclSF/gauntlet/internal/assertions"
	"github.com/pmclSF/gauntlet/internal/scenario"
	"github.com/pmclSF/gauntlet/internal/world"
)

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

type runSuitePathResolution struct {
	SuiteName  string
	SuiteDir   string
	EvalsDir   string
	ConfigPath string
	FromPath   bool
}

func resolveSuitePathForRun(suite, configPath string, explicitConfig bool) runSuitePathResolution {
	name := strings.TrimSpace(suite)
	if name == "" {
		name = "smoke"
	}
	result := runSuitePathResolution{
		SuiteName:  name,
		ConfigPath: strings.TrimSpace(configPath),
	}
	if result.ConfigPath == "" {
		result.ConfigPath = filepath.Join("evals", "gauntlet.yml")
	}

	if stat, err := os.Stat(name); err == nil && stat.IsDir() {
		suiteDir := filepath.Clean(name)
		evalsDir := filepath.Clean(filepath.Dir(suiteDir))
		result.SuiteName = filepath.Base(suiteDir)
		result.SuiteDir = suiteDir
		result.EvalsDir = evalsDir
		result.FromPath = true
		if !explicitConfig {
			result.ConfigPath = filepath.Join(evalsDir, "gauntlet.yml")
		}
	}
	return result
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
