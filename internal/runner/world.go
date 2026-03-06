package runner

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/pmclSF/gauntlet/internal/assertions"
	"github.com/pmclSF/gauntlet/internal/scenario"
	"github.com/pmclSF/gauntlet/internal/world"
)

func validateWorldToolRefs(scenarios []*scenario.Scenario, ws *world.State) error {
	availableTools := sortedToolNames(ws.Tools)
	var violations []string
	for _, s := range scenarios {
		for toolName, stateName := range s.World.Tools {
			toolDef, ok := ws.Tools[toolName]
			if !ok {
				violations = append(violations, fmt.Sprintf(
					"scenario %q: tool %q not found in world definitions\n  available tools: %s",
					s.Name,
					toolName,
					strings.Join(availableTools, ", "),
				))
				continue
			}
			if _, ok := toolDef.States[stateName]; !ok {
				availableStates := sortedToolStates(toolDef.States)
				violations = append(violations, fmt.Sprintf(
					"scenario %q: tool %q has no state %q\n  available states: %s",
					s.Name,
					toolName,
					stateName,
					strings.Join(availableStates, ", "),
				))
			}
		}
	}
	if len(violations) == 0 {
		return nil
	}
	return fmt.Errorf("invalid world tool references:\n  - %s", strings.Join(violations, "\n  - "))
}

func sortedToolNames(tools map[string]*world.ToolDef) []string {
	names := make([]string, 0, len(tools))
	for name := range tools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func sortedToolStates(states map[string]*world.ToolStateDef) []string {
	names := make([]string, 0, len(states))
	for name := range states {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func buildToolState(ws *world.State, toolStates map[string]string) map[string]assertions.ToolState {
	result := make(map[string]assertions.ToolState)
	for toolName, state := range toolStates {
		ts := assertions.ToolState{
			Name:  toolName,
			State: state,
		}
		if td, ok := ws.Tools[toolName]; ok {
			if sd, ok := td.States[state]; ok && sd.Response != nil {
				if raw, err := json.Marshal(sd.Response); err == nil {
					ts.Response = raw
				}
			}
		}
		result[toolName] = ts
	}
	return result
}

func prepareScenarioDatabases(ws *world.State, s *scenario.Scenario) (map[string]string, func(), error) {
	if len(s.World.Databases) == 0 {
		return map[string]string{}, func() {}, nil
	}

	runDir, err := os.MkdirTemp("", "gauntlet-db-*")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create DB temp dir: %w", err)
	}
	cleanup := func() {
		_ = os.RemoveAll(runDir)
	}

	env := make(map[string]string)
	for dbName, spec := range s.World.Databases {
		dbDef, ok := ws.Databases[dbName]
		if !ok {
			cleanup()
			return nil, nil, fmt.Errorf("database '%s' referenced by scenario '%s' not found in world definitions", dbName, s.Name)
		}
		dbPath, err := world.SeedDB(dbDef, spec.SeedSets, runDir)
		if err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("failed to seed database '%s' for scenario '%s': %w", dbName, s.Name, err)
		}
		envName := "GAUNTLET_DB_" + toEnvToken(dbName)
		env[envName] = sqliteURI(dbPath)
	}

	return env, cleanup, nil
}

var nonEnvTokenChars = regexp.MustCompile(`[^A-Za-z0-9]+`)

func toEnvToken(name string) string {
	normalized := nonEnvTokenChars.ReplaceAllString(strings.ToUpper(name), "_")
	normalized = strings.Trim(normalized, "_")
	if normalized == "" {
		return "DB"
	}
	return normalized
}

func sqliteURI(path string) string {
	p := filepath.ToSlash(path)
	if strings.HasPrefix(p, "/") {
		return "sqlite:////" + strings.TrimPrefix(p, "/")
	}
	return "sqlite:///" + p
}
