package world

import (
	"fmt"
	"log"
	"strings"
)

// ValidateVariantPolicy checks that at most one tool or DB is in a non-nominal
// state, unless chaos mode is enabled. Single-fault enforcement.
func ValidateVariantPolicy(tools map[string]string, chaos bool) error {
	var nonNominal []string
	for tool, state := range tools {
		if state != "nominal" {
			nonNominal = append(nonNominal, fmt.Sprintf("%s: %s", tool, state))
		}
	}

	if chaos {
		if len(nonNominal) > 1 {
			log.Printf("WARN: chaos=true with multi-fault scenario; running anyway (tools: [%s])", strings.Join(nonNominal, ", "))
		}
		return nil // multi-fault explicitly allowed
	}

	if len(nonNominal) > 1 {
		return fmt.Errorf(`multi-fault scenario detected (chaos: false)
  Tools in non-nominal state: [%s]
  To run multi-fault scenarios, set chaos: true in the scenario file.
  See: docs/variant-policy.md`, strings.Join(nonNominal, ", "))
	}

	return nil
}
