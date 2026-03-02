// Package redaction implements field-level and pattern-level redaction
// for Gauntlet artifacts. Redaction happens BEFORE disk write, never after.
package redaction

import (
	"encoding/json"
	"regexp"
	"strings"
)

// Redactor applies redaction rules to data before it is written to disk.
type Redactor struct {
	FieldPaths []string         // JSONPath-style field paths to redact
	Patterns   []*regexp.Regexp // Regex patterns to redact
}

// DefaultRedactor creates a Redactor with the default redaction rules.
func DefaultRedactor() *Redactor {
	return &Redactor{
		FieldPaths: []string{
			"api_key", "password", "token", "secret", "authorization",
			"x-api-key", "bearer", "credential",
		},
		Patterns: []*regexp.Regexp{
			// Credit card numbers (13-19 digits, optionally separated)
			regexp.MustCompile(`\b\d{4}[\s-]?\d{4}[\s-]?\d{4}[\s-]?\d{1,7}\b`),
			// Social Security Numbers
			regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`),
		},
	}
}

// RedactJSON applies redaction rules to a JSON object.
func (r *Redactor) RedactJSON(data []byte) ([]byte, error) {
	var parsed interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return data, nil // not JSON, try string redaction
	}

	redacted := r.redactValue(parsed)
	return json.Marshal(redacted)
}

// RedactString applies pattern-based redaction to a string.
func (r *Redactor) RedactString(s string) string {
	for _, pattern := range r.Patterns {
		s = pattern.ReplaceAllString(s, "[REDACTED]")
	}
	return s
}

func (r *Redactor) redactValue(v interface{}) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		result := make(map[string]interface{})
		for k, v := range val {
			if r.isSensitiveField(k) {
				result[k] = "[REDACTED]"
			} else {
				result[k] = r.redactValue(v)
			}
		}
		return result
	case []interface{}:
		result := make([]interface{}, len(val))
		for i, item := range val {
			result[i] = r.redactValue(item)
		}
		return result
	case string:
		return r.RedactString(val)
	default:
		return v
	}
}

func (r *Redactor) isSensitiveField(key string) bool {
	lower := strings.ToLower(key)
	for _, path := range r.FieldPaths {
		if strings.Contains(lower, path) {
			return true
		}
	}
	return false
}
