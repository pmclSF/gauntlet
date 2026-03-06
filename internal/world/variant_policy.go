package world

import (
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/pmclSF/gauntlet/internal/scenario"
)

// ValidateVariantPolicy checks that at most one tool or DB is in a non-nominal
// state, unless chaos mode is enabled. Single-fault enforcement.
func ValidateVariantPolicy(tools map[string]string, databases map[string]scenario.DBSpec, chaos bool) error {
	var nonNominal []string
	for tool, state := range tools {
		if !isNominalVariantName(state) {
			nonNominal = append(nonNominal, fmt.Sprintf("tool %s: %s", tool, state))
		}
	}
	for dbName, spec := range databases {
		var nonNominalSeeds []string
		for _, seedSet := range spec.SeedSets {
			if !isNominalVariantName(seedSet) {
				nonNominalSeeds = append(nonNominalSeeds, seedSet)
			}
		}
		if len(nonNominalSeeds) > 0 {
			sort.Strings(nonNominalSeeds)
			nonNominal = append(nonNominal, fmt.Sprintf("db %s: seed_sets [%s]", dbName, strings.Join(nonNominalSeeds, ", ")))
		}
	}
	sort.Strings(nonNominal)

	if chaos {
		if len(nonNominal) > 1 {
			log.Printf("WARN: chaos=true with multi-fault scenario; running anyway (components: [%s])", strings.Join(nonNominal, ", "))
		}
		return nil // multi-fault explicitly allowed
	}

	if len(nonNominal) > 1 {
		return fmt.Errorf(`multi-fault scenario detected (chaos: false)
  Components in non-nominal state: [%s]
  To run multi-fault scenarios, set chaos: true in the scenario file.
  See: docs/variant-policy.md`, strings.Join(nonNominal, ", "))
	}

	return nil
}

func isNominalVariantName(name string) bool {
	normalized := strings.ToLower(strings.TrimSpace(name))
	switch normalized {
	case "", "nominal", "default", "standard", "baseline", "happy_path", "happy-path":
		return true
	}
	return strings.HasPrefix(normalized, "nominal_") ||
		strings.HasPrefix(normalized, "default_") ||
		strings.HasPrefix(normalized, "standard_") ||
		strings.HasSuffix(normalized, "_nominal") ||
		strings.HasSuffix(normalized, "_default") ||
		strings.HasSuffix(normalized, "_standard")
}
