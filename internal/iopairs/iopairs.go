// Package iopairs manages input/output pair libraries for Gauntlet.
// IO pairs are ground truth examples used to auto-derive behavioral assertions.
package iopairs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Pair is a single input/output example.
type Pair struct {
	ID          string                 `json:"id" yaml:"id"`
	Description string                 `json:"description" yaml:"description"`
	Input       map[string]interface{} `json:"input" yaml:"input"`
	Output      map[string]interface{} `json:"output" yaml:"output"`
	Category    string                 `json:"category" yaml:"category"` // good, bad, edge
	Tags        []string               `json:"tags" yaml:"tags"`
}

// Library is a collection of IO pairs for a scenario or tool.
type Library struct {
	Name  string `json:"name" yaml:"name"`
	Tool  string `json:"tool,omitempty" yaml:"tool,omitempty"`
	Pairs []Pair `json:"pairs" yaml:"pairs"`
}

// LoadLibrary reads an IO pair library from a YAML file.
func LoadLibrary(path string) (*Library, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to load IO pair library %s: %w", path, err)
	}

	var lib Library
	if err := yaml.Unmarshal(data, &lib); err != nil {
		return nil, fmt.Errorf("failed to parse IO pair library %s: %w", path, err)
	}

	return &lib, nil
}

// LoadLibrariesFromDir reads all IO pair libraries from a directory.
func LoadLibrariesFromDir(dir string) ([]*Library, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var libs []*Library
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := filepath.Ext(entry.Name())
		if ext != ".yaml" && ext != ".yml" {
			continue
		}

		lib, err := LoadLibrary(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		libs = append(libs, lib)
	}

	return libs, nil
}

// GoodPairs returns only pairs categorized as "good".
func (lib *Library) GoodPairs() []Pair {
	var pairs []Pair
	for _, p := range lib.Pairs {
		if p.Category == "good" {
			pairs = append(pairs, p)
		}
	}
	return pairs
}

// BadPairs returns only pairs categorized as "bad".
func (lib *Library) BadPairs() []Pair {
	var pairs []Pair
	for _, p := range lib.Pairs {
		if p.Category == "bad" {
			pairs = append(pairs, p)
		}
	}
	return pairs
}

// DeriveAssertions generates behavioral assertion specs from IO pairs.
func DeriveAssertions(lib *Library) []DerivedAssertion {
	var assertions []DerivedAssertion

	for _, good := range lib.GoodPairs() {
		// From each good pair, derive output_schema check
		if schema := inferSchema(good.Output); schema != nil {
			assertions = append(assertions, DerivedAssertion{
				Type:     "output_schema",
				Source:   fmt.Sprintf("io_pair:%s", good.ID),
				Schema:   schema,
				HardGate: true,
			})
		}

		// Derive required fields from good output
		for key := range good.Output {
			assertions = append(assertions, DerivedAssertion{
				Type:     "required_field",
				Source:   fmt.Sprintf("io_pair:%s", good.ID),
				Field:    key,
				HardGate: false,
			})
		}
	}

	for _, bad := range lib.BadPairs() {
		// From each bad pair, derive forbidden content checks
		if content, ok := bad.Output["forbidden_content"]; ok {
			if s, ok := content.(string); ok {
				assertions = append(assertions, DerivedAssertion{
					Type:             "forbidden_content",
					Source:           fmt.Sprintf("io_pair:%s", bad.ID),
					ForbiddenContent: s,
					HardGate:         true,
				})
			}
		}
	}

	return assertions
}

// DerivedAssertion is an assertion auto-generated from IO pairs.
type DerivedAssertion struct {
	Type             string                 `json:"type"`
	Source           string                 `json:"source"`
	Schema           map[string]interface{} `json:"schema,omitempty"`
	Field            string                 `json:"field,omitempty"`
	ForbiddenContent string                 `json:"forbidden_content,omitempty"`
	HardGate         bool                   `json:"hard_gate"`
}

func inferSchema(output map[string]interface{}) map[string]interface{} {
	if len(output) == 0 {
		return nil
	}

	properties := make(map[string]interface{})
	for key, val := range output {
		properties[key] = map[string]interface{}{
			"type": jsonType(val),
		}
	}

	return map[string]interface{}{
		"type":       "object",
		"properties": properties,
	}
}

func jsonType(val interface{}) string {
	switch val.(type) {
	case string:
		return "string"
	case float64, int, int64:
		return "number"
	case bool:
		return "boolean"
	case []interface{}:
		return "array"
	case map[string]interface{}:
		return "object"
	default:
		return "string"
	}
}

// SaveLibrary writes an IO pair library to a JSON file.
func SaveLibrary(lib *Library, path string) error {
	data, err := json.MarshalIndent(lib, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
