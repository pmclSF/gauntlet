// Package world manages world state assembly for Gauntlet scenarios.
// A "world" is the frozen environment the TUT runs against: tool states,
// database seeds, and their configurations.
package world

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// State is the assembled world state for a scenario.
type State struct {
	Tools     map[string]*ToolDef
	Databases map[string]*DBDef
}

// ToolDef is a tool with its possible states.
type ToolDef struct {
	Tool   string                    `yaml:"tool" json:"tool"`
	States map[string]*ToolStateDef  `yaml:"states" json:"states"`
}

// ToolStateDef is a single tool state.
type ToolStateDef struct {
	Response          interface{} `yaml:"response" json:"response,omitempty"`
	DelayMs           int         `yaml:"delay_ms" json:"delay_ms,omitempty"`
	StatusCode        int         `yaml:"status_code" json:"status_code,omitempty"`
	Error             string      `yaml:"error" json:"error,omitempty"`
	Behavior          string      `yaml:"behavior" json:"behavior,omitempty"`
	AgentExpectation  string      `yaml:"agent_expectation" json:"agent_expectation,omitempty"`
}

// DBDef is a database definition with seed sets.
type DBDef struct {
	Database string                  `yaml:"database" json:"database"`
	Adapter  string                  `yaml:"adapter" json:"adapter"`
	SeedSets map[string]*SeedSetDef  `yaml:"seed_sets" json:"seed_sets"`
}

// SeedSetDef defines data to seed into an ephemeral database.
type SeedSetDef struct {
	Tables map[string]*TableDef `yaml:"tables" json:"tables,omitempty"`
}

// TableDef defines a single table's schema and seed data.
type TableDef struct {
	Columns map[string]string        `yaml:"columns" json:"columns,omitempty"`
	Rows    []map[string]interface{} `yaml:"rows" json:"rows,omitempty"`
}

// LoadToolDef loads a tool definition from a YAML file.
func LoadToolDef(path string) (*ToolDef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read tool definition %s: %w", path, err)
	}
	var td ToolDef
	if err := yaml.Unmarshal(data, &td); err != nil {
		return nil, fmt.Errorf("failed to parse tool definition %s: %w", path, err)
	}
	return &td, nil
}

// LoadDBDef loads a database definition from a YAML file.
func LoadDBDef(path string) (*DBDef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read DB definition %s: %w", path, err)
	}
	var dd DBDef
	if err := yaml.Unmarshal(data, &dd); err != nil {
		return nil, fmt.Errorf("failed to parse DB definition %s: %w", path, err)
	}
	return &dd, nil
}

// Assemble builds a world State from tool and DB definition directories
// and a scenario's world spec.
func Assemble(toolsDir, dbDir string, tools map[string]string, dbs map[string][]string) (*State, error) {
	state := &State{
		Tools:     make(map[string]*ToolDef),
		Databases: make(map[string]*DBDef),
	}

	// Load tool definitions
	if toolsDir != "" {
		entries, err := os.ReadDir(toolsDir)
		if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("failed to read tools directory %s: %w", toolsDir, err)
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			td, err := LoadToolDef(filepath.Join(toolsDir, entry.Name()))
			if err != nil {
				return nil, err
			}
			state.Tools[td.Tool] = td
		}
	}

	// Load DB definitions
	if dbDir != "" {
		entries, err := os.ReadDir(dbDir)
		if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("failed to read databases directory %s: %w", dbDir, err)
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			dd, err := LoadDBDef(filepath.Join(dbDir, entry.Name()))
			if err != nil {
				return nil, err
			}
			state.Databases[dd.Database] = dd
		}
	}

	return state, nil
}
