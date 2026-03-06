package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/pmclSF/gauntlet/internal/ci"
	"github.com/pmclSF/gauntlet/internal/runner"
)

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Interactive setup for a new Gauntlet-enabled project",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, _ := os.Getwd()
			reader := bufio.NewReader(cmd.InOrStdin())
			out := cmd.OutOrStdout()

			// Use detection as defaults instead of hardcoded values
			detectedFramework := ci.DetectFramework(cwd)
			detectedEntrypoint := ci.DetectEntryPoint(cwd)

			defaultFramework := "OpenAI SDK"
			defaultScenarioDir := "evals/"
			defaultCI := "GitHub Actions"
			defaultEntrypoint := detectedEntrypoint
			if defaultEntrypoint == "" {
				defaultEntrypoint = "agent.py"
			}

			framework := defaultFramework
			scenarioDir := defaultScenarioDir
			ciSystem := defaultCI
			entrypoint := defaultEntrypoint

			if isInteractiveInput(cmd.InOrStdin()) {
				fmt.Fprintln(out, "What framework does your agent use?")
				defaultIdx := 0
				if detectedFramework != "generic" {
					fmt.Fprintf(out, "  (detected: %s)\n", detectedFramework)
				}
				value, err := promptChoice(reader, out, []string{"OpenAI SDK", "Anthropic SDK", "LangChain", "Other (HTTP endpoint)"}, defaultIdx)
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
			fmt.Fprintf(out, "\nFramework: %s (detected: %s)\n", framework, detectedFramework)

			// Run auto-discovery to generate initial scenarios
			autoResult, autoErr := ensureAutoDiscoverySuite(runner.Config{
				Suite:    "smoke",
				EvalsDir: filepath.Join(cwd, strings.TrimSpace(scenarioDir)),
			}, false)
			if autoErr == nil && autoResult != nil && autoResult.GeneratedScenarios > 0 {
				fmt.Fprintf(out, "✓ Auto-discovered %d scenarios\n", autoResult.GeneratedScenarios)
			}

			ci.PrintOnboardingChecklist(result.Framework)
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
	content := "# gauntlet:auto-generated\n" + scenarioSchemaDirective + `
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
	if err != nil {
		return 0, fmt.Errorf("failed to read entrypoint %s: %w\n  Create the file first or specify a different entrypoint", entrypointPath, err)
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

	lines := strings.Split(content, "\n")
	insertAt := 0
	// Skip shebang line
	if len(lines) > 0 && strings.HasPrefix(lines[0], "#!") {
		insertAt = 1
	}
	// Skip __future__ imports (must be first real imports in Python)
	for insertAt < len(lines) {
		trimmed := strings.TrimSpace(lines[insertAt])
		if strings.HasPrefix(trimmed, "from __future__") {
			insertAt++
			continue
		}
		break
	}
	prefix := []string{"import gauntlet_sdk as gauntlet", "gauntlet.connect()", ""}
	newLines := make([]string, 0, len(lines)+len(prefix))
	newLines = append(newLines, lines[:insertAt]...)
	newLines = append(newLines, prefix...)
	newLines = append(newLines, lines[insertAt:]...)
	newContent := strings.Join(newLines, "\n")
	if err := os.WriteFile(entrypointPath, []byte(newContent), 0o644); err != nil {
		return 0, fmt.Errorf("failed to update entrypoint %s: %w", entrypointPath, err)
	}
	return insertAt + 2, nil
}
