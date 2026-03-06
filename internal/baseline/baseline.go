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
//
// Supports two JSON formats:
//
//	Nested (canonical, produced by Save):
//	  {"tool_sequence": {"required": [...], "order": "partial"}, "output": {...}}
//
//	Flat (hand-written):
//	  {"tool_sequence": ["order_lookup"], "required_fields": [...], "forbidden_content": [...]}
type Contract struct {
	BaselineType string                `json:"baseline_type"`
	Scenario     string                `json:"scenario"`
	Suite        string                `json:"suite,omitempty"`
	RecordedAt   string                `json:"recorded_at"`
	Commit       string                `json:"commit"`
	ToolSequence *ToolSequenceBaseline `json:"tool_sequence"`
	Output       *OutputBaseline       `json:"output"`
}

// UnmarshalJSON handles both the nested (canonical) and flat (hand-written)
// baseline formats. In flat format, tool_sequence is a plain string array and
// output fields are at the top level.
func (c *Contract) UnmarshalJSON(data []byte) error {
	// Parse into raw map so we can inspect value types before decoding.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	// Decode scalar fields.
	if v, ok := raw["baseline_type"]; ok {
		if err := json.Unmarshal(v, &c.BaselineType); err != nil {
			return fmt.Errorf("baseline_type: %w", err)
		}
	}
	if v, ok := raw["scenario"]; ok {
		if err := json.Unmarshal(v, &c.Scenario); err != nil {
			return fmt.Errorf("scenario: %w", err)
		}
	}
	if v, ok := raw["suite"]; ok {
		if err := json.Unmarshal(v, &c.Suite); err != nil {
			return fmt.Errorf("suite: %w", err)
		}
	}
	if v, ok := raw["recorded_at"]; ok {
		if err := json.Unmarshal(v, &c.RecordedAt); err != nil {
			return fmt.Errorf("recorded_at: %w", err)
		}
	}
	if v, ok := raw["commit"]; ok {
		if err := json.Unmarshal(v, &c.Commit); err != nil {
			return fmt.Errorf("commit: %w", err)
		}
	}

	// tool_sequence: try nested struct first, fall back to flat array.
	if tsRaw, ok := raw["tool_sequence"]; ok {
		var ts ToolSequenceBaseline
		if json.Unmarshal(tsRaw, &ts) == nil && (len(ts.Required) > 0 || ts.Order != "") {
			c.ToolSequence = &ts
		} else {
			// Flat format: plain string array
			var names []string
			if json.Unmarshal(tsRaw, &names) == nil {
				c.ToolSequence = &ToolSequenceBaseline{
					Required: names,
					Order:    "partial",
				}
			}
		}
	}

	// output: try nested struct first, fall back to flat top-level fields.
	if outRaw, ok := raw["output"]; ok {
		var out OutputBaseline
		if json.Unmarshal(outRaw, &out) == nil {
			c.Output = &out
		}
	}

	if c.Output == nil {
		var output OutputBaseline
		hasOutput := false

		if v, ok := raw["output_schema"]; ok {
			var schema map[string]interface{}
			if json.Unmarshal(v, &schema) == nil {
				output.Schema = schema
				hasOutput = true
			}
		}
		if v, ok := raw["required_fields"]; ok {
			var fields []string
			if json.Unmarshal(v, &fields) == nil {
				output.RequiredFields = fields
				hasOutput = true
			}
		}
		if v, ok := raw["forbidden_content"]; ok {
			var forbidden []string
			if json.Unmarshal(v, &forbidden) == nil {
				output.ForbiddenContent = forbidden
				hasOutput = true
			}
		}
		if v, ok := raw["expected_output"]; ok {
			var rawOutput json.RawMessage
			if json.Unmarshal(v, &rawOutput) == nil {
				output.ExpectedOutput = rawOutput
				hasOutput = true
			}
		}
		if hasOutput {
			c.Output = &output
		}
	}

	return nil
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
	ExpectedOutput   json.RawMessage        `json:"expected_output,omitempty"`
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
