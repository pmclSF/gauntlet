// Package discovery implements the Gauntlet discovery engine.
// It reads a codebase to auto-generate test proposals based on
// tool definitions, DB schemas, and agent traces.
package discovery

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Proposal is a generated test scenario suggestion.
type Proposal struct {
	ID          string   `json:"id" yaml:"id"`
	Name        string   `json:"name" yaml:"name"`
	Description string   `json:"description" yaml:"description"`
	Tool        string   `json:"tool,omitempty" yaml:"tool,omitempty"`
	Variant     string   `json:"variant,omitempty" yaml:"variant,omitempty"`
	Database    string   `json:"database,omitempty" yaml:"database,omitempty"`
	SeedSet     string   `json:"seed_set,omitempty" yaml:"seed_set,omitempty"`
	Tags        []string `json:"tags,omitempty" yaml:"tags,omitempty"`
	Status      string   `json:"status" yaml:"status"`                             // pending, approved, rejected
	Source      string   `json:"source" yaml:"source"`                             // how it was discovered
	Framework   string   `json:"framework,omitempty" yaml:"framework,omitempty"`   // originating framework (gauntlet, pydantic-ai, openai-agents, langchain)
}

// DiscoveryConfig controls what the discovery engine scans.
type DiscoveryConfig struct {
	RootDir      string   `yaml:"root_dir"`
	ToolDirs     []string `yaml:"tool_dirs"`
	PythonDirs   []string `yaml:"python_dirs"`
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

	// Discover from Python @gauntlet.tool decorators
	pythonProposals, err := e.discoverFromPythonTools()
	if err != nil {
		return nil, fmt.Errorf("python tool discovery failed: %w", err)
	}
	proposals = append(proposals, pythonProposals...)

	// Merge by stable identity key to avoid duplicates from multiple sources.
	merged := make(map[string]Proposal, len(proposals))
	for _, p := range proposals {
		key := proposalKey(p)
		if existing, ok := merged[key]; ok {
			// Prefer sources with richer scenario context.
			if proposalSourcePriority(p.Source) > proposalSourcePriority(existing.Source) {
				p.Tags = mergeTags(existing.Tags, p.Tags)
				p.Source = mergeSources(existing.Source, p.Source)
				merged[key] = p
			} else {
				existing.Tags = mergeTags(existing.Tags, p.Tags)
				existing.Source = mergeSources(existing.Source, p.Source)
				merged[key] = existing
			}
			continue
		}
		merged[key] = p
	}

	var deduped []Proposal
	for _, p := range merged {
		deduped = append(deduped, p)
	}
	sort.SliceStable(deduped, func(i, j int) bool {
		if deduped[i].Tool != deduped[j].Tool {
			return deduped[i].Tool < deduped[j].Tool
		}
		return deduped[i].Variant < deduped[j].Variant
	})
	return deduped, nil
}

func proposalSourcePriority(source string) int {
	parts := strings.Split(source, "+")
	best := 0
	for _, part := range parts {
		switch strings.TrimSpace(part) {
		case "tool_definition":
			if best < 3 {
				best = 3
			}
		case "python_tool_ast":
			if best < 2 {
				best = 2
			}
		case "db_schema":
			if best < 1 {
				best = 1
			}
		}
	}
	return best
}

func proposalKey(p Proposal) string {
	switch {
	case p.Tool != "":
		return "tool:" + p.Tool + "|" + p.Variant
	case p.Database != "":
		return "db:" + p.Database + "|" + p.SeedSet
	default:
		return "misc:" + p.ID + "|" + p.Name
	}
}

func mergeTags(a, b []string) []string {
	seen := make(map[string]bool, len(a)+len(b))
	var out []string
	for _, tag := range a {
		if seen[tag] || tag == "" {
			continue
		}
		seen[tag] = true
		out = append(out, tag)
	}
	for _, tag := range b {
		if seen[tag] || tag == "" {
			continue
		}
		seen[tag] = true
		out = append(out, tag)
	}
	sort.Strings(out)
	return out
}

func mergeSources(a, b string) string {
	seen := map[string]bool{}
	var parts []string
	for _, source := range strings.Split(a+"+"+b, "+") {
		s := strings.TrimSpace(source)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		parts = append(parts, s)
	}
	sort.Strings(parts)
	return strings.Join(parts, "+")
}

func (e *Engine) discoverFromTools() ([]Proposal, error) {
	var proposals []Proposal

	for _, dir := range e.Config.ToolDirs {
		toolDir := dir
		if !filepath.IsAbs(toolDir) {
			toolDir = filepath.Join(e.Config.RootDir, dir)
		}
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
					Source:      "tool_definition",
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

	dbDir := e.Config.DBSchemaDir
	if !filepath.IsAbs(dbDir) {
		dbDir = filepath.Join(e.Config.RootDir, e.Config.DBSchemaDir)
	}
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

		path := filepath.Join(dbDir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var parsed struct {
			Database string                 `yaml:"database"`
			SeedSets map[string]interface{} `yaml:"seed_sets"`
		}
		if err := yaml.Unmarshal(data, &parsed); err != nil {
			return nil, err
		}

		dbName := strings.TrimSpace(parsed.Database)
		if dbName == "" {
			dbName = strings.TrimSuffix(entry.Name(), ".yaml")
		}

		if len(parsed.SeedSets) == 0 {
			proposals = append(proposals, Proposal{
				ID:          fmt.Sprintf("disc-db-%s", dbName),
				Name:        fmt.Sprintf("db_%s_seed_test", dbName),
				Description: fmt.Sprintf("Auto-discovered: test with %s database seed", dbName),
				Database:    dbName,
				Tags:        []string{"auto-discovered", "db-seed"},
				Status:      "pending",
				Source:      "db_schema",
			})
			continue
		}

		var seedNames []string
		for seed := range parsed.SeedSets {
			seedNames = append(seedNames, seed)
		}
		sort.Strings(seedNames)
		for _, seed := range seedNames {
			proposals = append(proposals, Proposal{
				ID:          fmt.Sprintf("disc-db-%s-%s", dbName, sanitizeID(seed)),
				Name:        fmt.Sprintf("db_%s_%s_seed_test", dbName, seed),
				Description: fmt.Sprintf("Auto-discovered: test with %s/%s seed set", dbName, seed),
				Database:    dbName,
				SeedSet:     seed,
				Tags:        []string{"auto-discovered", "db-seed"},
				Status:      "pending",
				Source:      "db_schema",
			})
		}
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
