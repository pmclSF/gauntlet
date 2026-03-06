package baseline

import "fmt"

// Golden baselines store the full output text for exact comparison.
// This is opt-in and explicitly experimental in v1.

// LoadGolden loads a golden baseline. Returns an error in v1.
func LoadGolden(baselineDir, suite, scenarioName string) (string, error) {
	return "", fmt.Errorf("golden baselines are experimental in v1 — use contract baselines instead")
}
