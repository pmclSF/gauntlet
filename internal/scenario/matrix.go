package scenario

// ExpandMatrix generates scenario variants from world state combinations.
// In v1, this is a passthrough — each scenario runs as-is without matrix expansion.
// Matrix expansion (multiple tool states per scenario) is a v2 feature.
func ExpandMatrix(scenarios []*Scenario) []*Scenario {
	return scenarios
}
