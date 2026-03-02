// Package baseline implements contract-based baseline management.
// Baselines define the expected behavior that should not regress.
package baseline

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Contract is a contract-type baseline (tool sequence + output schema +
// forbidden content + required fields).
type Contract struct {
	BaselineType     string                 `json:"baseline_type"`
	Scenario         string                 `json:"scenario"`
	RecordedAt       string                 `json:"recorded_at"`
	Commit           string                 `json:"commit"`
	ToolSequence     *ToolSequenceBaseline  `json:"tool_sequence"`
	Output           *OutputBaseline        `json:"output"`
}

// ToolSequenceBaseline defines expected tool call ordering.
type ToolSequenceBaseline struct {
	Required []string `json:"required"`
	Order    string   `json:"order"` // "strict" or "partial"
}

// OutputBaseline defines expected output characteristics.
type OutputBaseline struct {
	Schema           map[string]interface{} `json:"schema,omitempty"`
	ForbiddenContent []string               `json:"forbidden_content"`
	RequiredFields   []string               `json:"required_fields"`
}

// Load reads a contract baseline from disk.
func Load(baselineDir, suite, scenarioName string) (*Contract, error) {
	path := filepath.Join(baselineDir, suite, scenarioName+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // no baseline yet
		}
		return nil, fmt.Errorf("failed to read baseline %s: %w", path, err)
	}
	var c Contract
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("failed to parse baseline %s: %w", path, err)
	}
	return &c, nil
}

// Save writes a contract baseline to disk.
func Save(baselineDir, suite string, c *Contract) error {
	dir := filepath.Join(baselineDir, suite)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create baseline directory: %w", err)
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal baseline: %w", err)
	}
	path := filepath.Join(dir, c.Scenario+".json")
	return os.WriteFile(path, data, 0o644)
}
