package fixture

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"regexp"
	"sort"
	"strings"
)

// SensitiveFinding is one sensitive-data match.
type SensitiveFinding struct {
	Path    string `json:"path"`
	Pattern string `json:"pattern"`
	Sample  string `json:"sample"`
}

var sensitiveValuePatterns = []struct {
	name string
	re   *regexp.Regexp
}{
	{name: "OpenAI key pattern", re: regexp.MustCompile(`(?i)\bsk-[a-z0-9]{20,}\b`)},
	{name: "Bearer token detected", re: regexp.MustCompile(`(?i)\bbearer\s+[a-z0-9._\-]{16,}\b`)},
	{name: "API key assignment pattern", re: regexp.MustCompile(`(?i)\bapi[_-]?key\b\s*[:=]\s*[\"']?[a-z0-9._\-]{8,}`)},
}

// ScanSensitiveJSON scans JSON payload bytes for sensitive-data patterns.
func ScanSensitiveJSON(data []byte, root string) ([]SensitiveFinding, error) {
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, nil
	}
	var v interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, fmt.Errorf("parse json for sensitive scan: %w", err)
	}
	findings := []SensitiveFinding{}
	scanSensitiveValue(v, root, &findings)
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Path != findings[j].Path {
			return findings[i].Path < findings[j].Path
		}
		return findings[i].Pattern < findings[j].Pattern
	})
	return findings, nil
}

func scanSensitiveValue(v interface{}, path string, findings *[]SensitiveFinding) {
	switch value := v.(type) {
	case map[string]interface{}:
		keys := make([]string, 0, len(value))
		for k := range value {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, key := range keys {
			nextPath := key
			if path != "" {
				nextPath = path + "." + key
			}
			scanSensitiveValue(value[key], nextPath, findings)
		}
	case []interface{}:
		for i, item := range value {
			nextPath := fmt.Sprintf("%s[%d]", path, i)
			scanSensitiveValue(item, nextPath, findings)
		}
	case string:
		s := strings.TrimSpace(value)
		if s == "" {
			return
		}
		for _, p := range sensitiveValuePatterns {
			if p.re.MatchString(s) {
				*findings = append(*findings, SensitiveFinding{
					Path:    path,
					Pattern: p.name,
					Sample:  redactSample(s),
				})
				break
			}
		}
		if looksLikeCredentialPath(path) && len(s) >= 8 {
			*findings = append(*findings, SensitiveFinding{
				Path:    path,
				Pattern: "Credential-like field name with non-empty secret value",
				Sample:  redactSample(s),
			})
			return
		}
		if hasHighEntropy(s) {
			*findings = append(*findings, SensitiveFinding{
				Path:    path,
				Pattern: "High-entropy string detected",
				Sample:  redactSample(s),
			})
		}
	}
}

func looksLikeCredentialPath(path string) bool {
	p := strings.ToLower(path)
	for _, key := range []string{"api_key", "apikey", "token", "secret", "password", "authorization"} {
		if strings.Contains(p, key) {
			return true
		}
	}
	return false
}

func hasHighEntropy(value string) bool {
	if len(value) < 24 || strings.Contains(value, " ") {
		return false
	}
	hasLower := false
	hasUpper := false
	hasDigit := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			hasLower = true
		case r >= 'A' && r <= 'Z':
			hasUpper = true
		case r >= '0' && r <= '9':
			hasDigit = true
		}
	}
	if !(hasDigit && (hasLower || hasUpper)) {
		return false
	}

	freq := map[rune]int{}
	for _, r := range value {
		freq[r]++
	}
	entropy := 0.0
	total := float64(len(value))
	for _, count := range freq {
		p := float64(count) / total
		entropy -= p * math.Log2(p)
	}
	return entropy > 4.5
}

func redactSample(value string) string {
	if len(value) <= 8 {
		return "****"
	}
	return value[:2] + strings.Repeat("*", len(value)-6) + value[len(value)-4:]
}

func allowSensitiveFixtureWrites() bool {
	return strings.TrimSpace(os.Getenv("GAUNTLET_ALLOW_SENSITIVE_FIXTURE")) == "1"
}

func sensitiveFixtureError(toolName string, finding SensitiveFinding) error {
	return fmt.Errorf(
		"ERROR: sensitive data detected in fixture for tool %q\n  field: %s\n  pattern: %s\n\n  If this is a false positive, set:\n    GAUNTLET_ALLOW_SENSITIVE_FIXTURE=1 gauntlet record\n\n  If this is real credential data, do not commit this fixture.",
		toolName,
		finding.Path,
		finding.Pattern,
	)
}
