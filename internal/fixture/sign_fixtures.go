package fixture

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SignFixtures signs all existing model/tool fixtures in place using the given
// fixture signing key. This is used after local recording so replay-time trust
// checks can enforce signed fixtures consistently.
func SignFixtures(store *Store, signingKeyPath string) (modelsSigned int, toolsSigned int, err error) {
	if store == nil || strings.TrimSpace(store.BaseDir) == "" {
		return 0, 0, fmt.Errorf("fixture store is required")
	}
	if err := store.EnableFixtureSigning(signingKeyPath); err != nil {
		return 0, 0, err
	}

	modelFiles, err := listJSONFiles(filepath.Join(store.BaseDir, "models"))
	if err != nil {
		return 0, 0, err
	}
	for _, path := range modelFiles {
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return modelsSigned, toolsSigned, fmt.Errorf("failed to read model fixture %s: %w", path, readErr)
		}
		var f ModelFixture
		if unmarshalErr := json.Unmarshal(raw, &f); unmarshalErr != nil {
			return modelsSigned, toolsSigned, fmt.Errorf("failed to parse model fixture %s: %w", path, unmarshalErr)
		}
		if strings.TrimSpace(f.CanonicalHash) == "" {
			f.CanonicalHash = strings.TrimSuffix(filepath.Base(path), ".json")
		}
		if strings.TrimSpace(f.FixtureID) == "" {
			f.FixtureID = f.CanonicalHash
		}
		if signErr := store.signModelFixture(&f); signErr != nil {
			return modelsSigned, toolsSigned, signErr
		}
		updated, marshalErr := json.MarshalIndent(&f, "", "  ")
		if marshalErr != nil {
			return modelsSigned, toolsSigned, fmt.Errorf("failed to marshal model fixture %s: %w", path, marshalErr)
		}
		if writeErr := os.WriteFile(path, updated, 0o644); writeErr != nil {
			return modelsSigned, toolsSigned, fmt.Errorf("failed to write model fixture %s: %w", path, writeErr)
		}
		modelsSigned++
	}

	toolFiles, err := listJSONFiles(filepath.Join(store.BaseDir, "tools"))
	if err != nil {
		return modelsSigned, toolsSigned, err
	}
	for _, path := range toolFiles {
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return modelsSigned, toolsSigned, fmt.Errorf("failed to read tool fixture %s: %w", path, readErr)
		}
		var f ToolFixture
		if unmarshalErr := json.Unmarshal(raw, &f); unmarshalErr != nil {
			return modelsSigned, toolsSigned, fmt.Errorf("failed to parse tool fixture %s: %w", path, unmarshalErr)
		}
		if strings.TrimSpace(f.CanonicalHash) == "" {
			f.CanonicalHash = strings.TrimSuffix(filepath.Base(path), ".json")
		}
		if strings.TrimSpace(f.ToolName) == "" {
			parent := filepath.Base(filepath.Dir(path))
			if parent != "tools" {
				f.ToolName = parent
			}
		}
		if strings.TrimSpace(f.FixtureID) == "" {
			f.FixtureID = f.CanonicalHash
		}
		if signErr := store.signToolFixture(&f); signErr != nil {
			return modelsSigned, toolsSigned, signErr
		}
		updated, marshalErr := json.MarshalIndent(&f, "", "  ")
		if marshalErr != nil {
			return modelsSigned, toolsSigned, fmt.Errorf("failed to marshal tool fixture %s: %w", path, marshalErr)
		}
		if writeErr := os.WriteFile(path, updated, 0o644); writeErr != nil {
			return modelsSigned, toolsSigned, fmt.Errorf("failed to write tool fixture %s: %w", path, writeErr)
		}
		toolsSigned++
	}

	return modelsSigned, toolsSigned, nil
}
