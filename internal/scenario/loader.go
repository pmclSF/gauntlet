// Package scenario provides loading and validation of scenario YAML files.
package scenario

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// LoadFile loads a single scenario from a YAML file.
func LoadFile(path string) (*Scenario, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read scenario file %s: %w", path, err)
	}

	var s Scenario
	if err := yaml.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("failed to parse scenario file %s: %w", path, err)
	}
	if s.Name == "" {
		return nil, fmt.Errorf("scenario file %s: missing required field 'scenario'", path)
	}
	if len(s.Input.Messages) == 0 && len(s.Input.Payload) == 0 {
		return nil, fmt.Errorf("scenario %s: must have either 'messages' or 'payload' in input", s.Name)
	}
	if err := validateScenarioDocument(path, data); err != nil {
		return nil, err
	}
	return &s, nil
}

// LoadSuite loads all scenario YAML files from a directory.
func LoadSuite(dir string) ([]*Scenario, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read suite directory %s: %w", dir, err)
	}
	var scenarios []*Scenario
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}
		s, err := LoadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		scenarios = append(scenarios, s)
	}
	if len(scenarios) == 0 {
		return nil, fmt.Errorf("no scenario files found in %s", dir)
	}
	return scenarios, nil
}
