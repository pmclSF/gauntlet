package determinism

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/gauntlet-dev/gauntlet/internal/tut"
)

var iso8601Pattern = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}`)

// Validate checks agent output for nondeterminism violations.
func (h *Harness) Validate(output tut.AgentOutput, traces []tut.TraceEvent) []Warning {
	var warnings []Warning

	outputStr := string(output.Raw)

	// Check for clock skew
	if w := h.detectClockSkew(outputStr); w != nil {
		warnings = append(warnings, *w)
	}

	// Check for high-entropy strings
	if output.Parsed != nil {
		if w := h.detectEntropy(output.Parsed, traces); w != nil {
			warnings = append(warnings, *w)
		}
	}

	// Check for locale-specific formatting
	if w := detectLocaleLeaks(outputStr); w != nil {
		warnings = append(warnings, *w)
	}

	return warnings
}

// detectClockSkew scans output for timestamps that differ from freeze time.
func (h *Harness) detectClockSkew(output string) *Warning {
	matches := iso8601Pattern.FindAllString(output, -1)
	for _, match := range matches {
		t, err := time.Parse("2006-01-02T15:04:05", match)
		if err != nil {
			continue
		}
		diff := t.Sub(h.FreezeTime)
		if diff < 0 {
			diff = -diff
		}
		if diff > time.Second {
			return &Warning{
				Type: "nondeterminism.time",
				Message: fmt.Sprintf("output contains timestamp %s that differs from freeze time %s by %v",
					match, h.FreezeTime.Format(time.RFC3339), diff),
			}
		}
	}
	return nil
}

// detectEntropy checks for high-entropy strings not present in fixtures.
func (h *Harness) detectEntropy(parsed map[string]interface{}, traces []tut.TraceEvent) *Warning {
	// Collect known strings from fixture responses
	knownStrings := make(map[string]bool)
	for _, t := range traces {
		if t.Response != nil {
			for _, s := range extractAllStrings(t.Response) {
				knownStrings[s] = true
			}
		}
	}

	// Check output strings
	outputStrings := extractAllStringsFromMap(parsed)
	for _, s := range outputStrings {
		if len(s) <= 8 {
			continue
		}
		if knownStrings[s] {
			continue
		}
		entropy := shannonEntropy(s)
		if entropy > 3.5 {
			return &Warning{
				Type: "nondeterminism.rng",
				Message: fmt.Sprintf("output contains high-entropy string (%.2f bits/char) not in fixtures: %s...",
					entropy, truncate(s, 32)),
			}
		}
	}
	return nil
}

// detectLocaleLeaks checks for locale-specific number formatting.
func detectLocaleLeaks(output string) *Warning {
	// Check for numbers with commas as decimal separators (non-US)
	// Pattern: digit,digit but not in a sequence like 1,000 (thousands separator)
	decimalComma := regexp.MustCompile(`\d,\d{1,2}(?:\D|$)`)
	if decimalComma.MatchString(output) {
		return &Warning{
			Type:    "nondeterminism.locale",
			Message: "output may contain locale-specific number formatting (comma as decimal separator)",
		}
	}
	return nil
}

// shannonEntropy computes the Shannon entropy of a string in bits per character.
func shannonEntropy(s string) float64 {
	if len(s) == 0 {
		return 0
	}
	freq := make(map[rune]int)
	for _, c := range s {
		freq[c]++
	}
	length := float64(len([]rune(s)))
	var entropy float64
	for _, count := range freq {
		p := float64(count) / length
		if p > 0 {
			entropy -= p * math.Log2(p)
		}
	}
	return entropy
}

func extractAllStrings(data json.RawMessage) []string {
	var v interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		return nil
	}
	return extractAllStringsFromInterface(v)
}

func extractAllStringsFromMap(m map[string]interface{}) []string {
	return extractAllStringsFromInterface(m)
}

func extractAllStringsFromInterface(v interface{}) []string {
	var result []string
	switch val := v.(type) {
	case string:
		result = append(result, val)
	case map[string]interface{}:
		for _, v := range val {
			result = append(result, extractAllStringsFromInterface(v)...)
		}
	case []interface{}:
		for _, v := range val {
			result = append(result, extractAllStringsFromInterface(v)...)
		}
	}
	return result
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}

// strings is imported as a package name alias to avoid conflict
func init() {
	_ = strings.Contains // ensure strings package is used
}
