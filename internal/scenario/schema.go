// Package scenario defines the types for Gauntlet scenario files.
// Scenarios are YAML files that describe a single test case:
// input messages, world state, and assertions to evaluate.
package scenario

// Scenario represents a single test scenario parsed from a YAML file.
type Scenario struct {
	Name        string          `yaml:"scenario"    json:"scenario"`
	Description string          `yaml:"description" json:"description"`
	Input       Input           `yaml:"input"       json:"input"`
	World       WorldSpec       `yaml:"world"       json:"world"`
	Assertions  []AssertionSpec `yaml:"assertions"  json:"assertions"`
	Chaos       bool            `yaml:"chaos"       json:"chaos"`
	Tags        []string        `yaml:"tags"        json:"tags"`
	BetaModel   bool            `yaml:"beta_model"  json:"beta_model"`
	BetaReason  string          `yaml:"beta_reason" json:"beta_reason"`
}

// Input is the payload sent to the target under test.
// Either Messages (OpenAI-format) or Payload (arbitrary JSON) is used.
type Input struct {
	Messages []Message              `yaml:"messages" json:"messages,omitempty"`
	Payload  map[string]interface{} `yaml:"payload"  json:"payload,omitempty"`
}

// Message is an OpenAI-format chat message.
type Message struct {
	Role    string `yaml:"role"    json:"role"`
	Content string `yaml:"content" json:"content"`
}

// WorldSpec defines the frozen world state for a scenario.
// Tools map tool names to state names (e.g. "order_lookup" -> "nominal").
// Databases map DB names to seed set configurations.
type WorldSpec struct {
	Tools     map[string]string `yaml:"tools"     json:"tools"`
	Databases map[string]DBSpec `yaml:"databases" json:"databases"`
}

// DBSpec defines which seed sets to load for an ephemeral database.
// Supports both a plain string (single seed set) and an object with seed_sets array.
type DBSpec struct {
	SeedSets []string `yaml:"seed_sets" json:"seed_sets"`
}

// UnmarshalYAML allows DBSpec to be specified as a plain string (single seed set name)
// or as an object with a seed_sets array.
func (d *DBSpec) UnmarshalYAML(unmarshal func(interface{}) error) error {
	// Try plain string first
	var s string
	if err := unmarshal(&s); err == nil {
		d.SeedSets = []string{s}
		return nil
	}

	// Try object with seed_sets
	type rawDBSpec struct {
		SeedSets []string `yaml:"seed_sets"`
	}
	var raw rawDBSpec
	if err := unmarshal(&raw); err != nil {
		return err
	}
	d.SeedSets = raw.SeedSets
	return nil
}

// AssertionSpec is a single assertion to evaluate against the scenario result.
// The Type field selects the assertion implementation. All other fields are
// captured via yaml inline for type-safe later parsing by each assertion type.
type AssertionSpec struct {
	Type string                 `yaml:"type" json:"type"`
	Raw  map[string]interface{} `yaml:",inline" json:",inline"`
}
