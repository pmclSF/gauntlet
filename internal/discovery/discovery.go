// Package discovery implements the Gauntlet discovery engine.
// It reads a codebase to auto-generate test proposals based on
// tool definitions, DB schemas, and agent traces.
package discovery

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Proposal is a generated test scenario suggestion.
type Proposal struct {
	ID          string   `json:"id" yaml:"id"`
	Name        string   `json:"name" yaml:"name"`
	Description string   `json:"description" yaml:"description"`
	Tool        string   `json:"tool" yaml:"tool"`
	Variant     string   `json:"variant" yaml:"variant"`
	Tags        []string `json:"tags" yaml:"tags"`
	Status      string   `json:"status" yaml:"status"` // pending, approved, rejected
	Source      string   `json:"source" yaml:"source"` // how it was discovered
}

// DiscoveryConfig controls what the discovery engine scans.
type DiscoveryConfig struct {
	RootDir      string   `yaml:"root_dir"`
	ToolDirs     []string `yaml:"tool_dirs"`
	DBSchemaDir  string   `yaml:"db_schema_dir"`
	TraceDir     string   `yaml:"trace_dir"`
	ExcludeTools []string `yaml:"exclude_tools"`
}

// Engine performs codebase discovery and generates test proposals.
type Engine struct {
	Config DiscoveryConfig
}

// NewEngine creates a new discovery engine.
func NewEngine(cfg DiscoveryConfig) *Engine {
	return &Engine{Config: cfg}
}

// Discover scans the configured directories and generates proposals.
func (e *Engine) Discover() ([]Proposal, error) {
	var proposals []Proposal

	// Discover from tool definitions
	toolProposals, err := e.discoverFromTools()
	if err != nil {
		return nil, fmt.Errorf("tool discovery failed: %w", err)
	}
	proposals = append(proposals, toolProposals...)

	// Discover from DB schemas
	dbProposals, err := e.discoverFromDB()
	if err != nil {
		return nil, fmt.Errorf("db discovery failed: %w", err)
	}
	proposals = append(proposals, dbProposals...)

	return proposals, nil
}

func (e *Engine) discoverFromTools() ([]Proposal, error) {
	var proposals []Proposal

	for _, dir := range e.Config.ToolDirs {
		toolDir := filepath.Join(e.Config.RootDir, dir)
		entries, err := os.ReadDir(toolDir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}

		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
				continue
			}

			toolName := strings.TrimSuffix(entry.Name(), ".yaml")
			if e.isExcluded(toolName) {
				continue
			}

			toolPath := filepath.Join(toolDir, entry.Name())
			variants, err := e.readToolVariants(toolPath)
			if err != nil {
				continue
			}

			for _, variant := range variants {
				proposals = append(proposals, Proposal{
					ID:          fmt.Sprintf("disc-%s-%s", toolName, variant),
					Name:        fmt.Sprintf("%s_%s", toolName, variant),
					Description: fmt.Sprintf("Auto-discovered: test %s tool in %s state", toolName, variant),
					Tool:        toolName,
					Variant:     variant,
					Tags:        []string{"auto-discovered", "tool-variant"},
					Status:      "pending",
					Source:       "tool_definition",
				})
			}
		}
	}

	return proposals, nil
}

func (e *Engine) discoverFromDB() ([]Proposal, error) {
	var proposals []Proposal

	if e.Config.DBSchemaDir == "" {
		return nil, nil
	}

	dbDir := filepath.Join(e.Config.RootDir, e.Config.DBSchemaDir)
	entries, err := os.ReadDir(dbDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}

		dbName := strings.TrimSuffix(entry.Name(), ".yaml")
		proposals = append(proposals, Proposal{
			ID:          fmt.Sprintf("disc-db-%s", dbName),
			Name:        fmt.Sprintf("db_%s_seed_test", dbName),
			Description: fmt.Sprintf("Auto-discovered: test with %s database seed", dbName),
			Tags:        []string{"auto-discovered", "db-seed"},
			Status:      "pending",
			Source:       "db_schema",
		})
	}

	return proposals, nil
}

func (e *Engine) readToolVariants(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var toolDef struct {
		States map[string]interface{} `yaml:"states"`
	}
	if err := yaml.Unmarshal(data, &toolDef); err != nil {
		return nil, err
	}

	var variants []string
	for name := range toolDef.States {
		variants = append(variants, name)
	}
	return variants, nil
}

func (e *Engine) isExcluded(toolName string) bool {
	for _, excluded := range e.Config.ExcludeTools {
		if excluded == toolName {
			return true
		}
	}
	return false
}

// SaveProposals writes proposals to a YAML file.
func SaveProposals(proposals []Proposal, path string) error {
	data, err := yaml.Marshal(proposals)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// LoadProposals reads proposals from a YAML file.
func LoadProposals(path string) ([]Proposal, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var proposals []Proposal
	return proposals, yaml.Unmarshal(data, &proposals)
}
