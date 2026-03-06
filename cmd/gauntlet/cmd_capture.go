package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

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
