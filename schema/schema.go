package schema

import _ "embed"

//go:embed scenario.schema.json
var scenarioSchema []byte

//go:embed proposals.schema.json
var proposalsSchema []byte

// ScenarioSchema returns the bundled scenario JSON schema.
func ScenarioSchema() []byte {
	out := make([]byte, len(scenarioSchema))
	copy(out, scenarioSchema)
	return out
}

// ProposalsSchema returns the bundled discovery proposals JSON schema.
func ProposalsSchema() []byte {
	out := make([]byte, len(proposalsSchema))
	copy(out, proposalsSchema)
	return out
}
