package fixture

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/gauntlet-dev/gauntlet/internal/proxy/providers"
)

// DenylistFields are stripped from all requests before canonicalization.
// Unknown fields NOT in this list are preserved (denylist approach).
var DenylistFields = map[string]bool{
	"request_id": true,
	"user":       true,
	"session_id": true,
	"stream":     true,
}

// DenylistSuffixes are field name suffixes that cause stripping.
var DenylistSuffixes = []string{"_id", "_ts", "_at", "_timestamp"}

// DenylistPrefixes are field name prefixes that cause stripping.
var DenylistPrefixes = []string{"metadata", "extra_headers", "http_client"}

// DenylistHeaders are HTTP headers stripped before canonicalization.
var DenylistHeaders = map[string]bool{
	"x-request-id":  true,
	"date":          true,
	"user-agent":    true,
	"authorization": true,
	"x-api-key":     true,
}

// shouldStripField returns true if the field should be removed per denylist.
func shouldStripField(key string) bool {
	if DenylistFields[key] {
		return true
	}
	for _, suffix := range DenylistSuffixes {
		if strings.HasSuffix(key, suffix) {
			return true
		}
	}
	for _, prefix := range DenylistPrefixes {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

// StripDenylistHeaders removes denylist headers from the map.
// Header keys are lowercased before comparison.
func StripDenylistHeaders(headers map[string]string) map[string]string {
	clean := make(map[string]string)
	for k, v := range headers {
		if !DenylistHeaders[strings.ToLower(k)] {
			clean[k] = v
		}
	}
	return clean
}

// CanonicalizeRequest produces a stable JSON representation of a CanonicalRequest
// for hashing. Keys are sorted lexicographically at every level. Tools are sorted
// by name. Messages order is preserved.
func CanonicalizeRequest(cr *providers.CanonicalRequest) ([]byte, error) {
	// Strip denylist fields from Extra
	cleanExtra := make(map[string]interface{})
	for k, v := range cr.Extra {
		if !shouldStripField(k) {
			cleanExtra[k] = v
		}
	}

	// Build a map for deterministic JSON serialization
	canonical := map[string]interface{}{
		"gauntlet_canonical_version": cr.GauntletCanonicalVersion,
		"provider_family":           cr.ProviderFamily,
		"model":                     cr.Model,
		"system":                    cr.System,
		"messages":                  cr.Messages,
		"sampling":                  cr.Sampling,
	}

	// Tools sorted by name (already done in normalizers, but ensure)
	tools := make([]providers.CanonicalTool, len(cr.Tools))
	copy(tools, cr.Tools)
	sort.Slice(tools, func(i, j int) bool {
		return tools[i].Name < tools[j].Name
	})
	canonical["tools"] = tools

	if len(cleanExtra) > 0 {
		canonical["extra"] = cleanExtra
	}

	return sortedJSON(canonical)
}

// CanonicalizeToolCall produces a stable JSON representation of a tool call
// for hashing. The tool name and sorted args form the canonical representation.
func CanonicalizeToolCall(toolName string, args map[string]interface{}) ([]byte, error) {
	// Strip denylist fields from args
	cleanArgs := make(map[string]interface{})
	for k, v := range args {
		if !shouldStripField(k) {
			cleanArgs[k] = v
		}
	}

	canonical := map[string]interface{}{
		"tool": toolName,
		"args": cleanArgs,
	}

	return sortedJSON(canonical)
}

// HashCanonical computes the SHA-256 hash of canonical bytes.
func HashCanonical(data []byte) string {
	return Hash(data)
}

// sortedJSON produces JSON with all keys sorted lexicographically at every level.
func sortedJSON(v interface{}) ([]byte, error) {
	// Marshal to get the structure, then re-process for sorted keys
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal for canonicalization: %w", err)
	}

	// Parse into interface{} to sort
	var parsed interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("failed to re-parse for canonicalization: %w", err)
	}

	sorted := deepSortKeys(parsed)
	return json.Marshal(sorted)
}

// deepSortKeys recursively sorts map keys at every level.
// Returns ordered structures suitable for deterministic JSON marshaling.
func deepSortKeys(v interface{}) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		ordered := &orderedMap{keys: keys, values: make(map[string]interface{})}
		for _, k := range keys {
			ordered.values[k] = deepSortKeys(val[k])
		}
		return ordered
	case []interface{}:
		// Arrays preserve order (semantically meaningful)
		result := make([]interface{}, len(val))
		for i, item := range val {
			result[i] = deepSortKeys(item)
		}
		return result
	default:
		return v
	}
}

// orderedMap preserves key order for JSON marshaling.
type orderedMap struct {
	keys   []string
	values map[string]interface{}
}

func (m *orderedMap) MarshalJSON() ([]byte, error) {
	var buf strings.Builder
	buf.WriteByte('{')
	for i, k := range m.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		keyJSON, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		buf.Write(keyJSON)
		buf.WriteByte(':')
		valJSON, err := json.Marshal(m.values[k])
		if err != nil {
			return nil, err
		}
		buf.Write(valJSON)
	}
	buf.WriteByte('}')
	return []byte(buf.String()), nil
}
