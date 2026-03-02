package determinism

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// CanonicalizeOutput produces a stable JSON form of agent output for comparison.
// Keys sorted lexicographically. Floats normalized. Unicode NFC. No trailing whitespace.
// Arrays NOT sorted (order is semantically meaningful).
func CanonicalizeOutput(output interface{}) ([]byte, error) {
	canonical := normalizeValue(output)
	data, err := marshalSorted(canonical)
	if err != nil {
		return nil, fmt.Errorf("failed to canonicalize output: %w", err)
	}
	// NFC normalize unicode
	return []byte(norm.NFC.String(string(data))), nil
}

func normalizeValue(v interface{}) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		result := make(map[string]interface{})
		for k, v := range val {
			result[norm.NFC.String(k)] = normalizeValue(v)
		}
		return result
	case []interface{}:
		// Arrays preserve order
		result := make([]interface{}, len(val))
		for i, item := range val {
			result[i] = normalizeValue(item)
		}
		return result
	case float64:
		return normalizeFloat(val)
	case string:
		return strings.TrimRight(norm.NFC.String(val), " \t")
	default:
		return v
	}
}

// normalizeFloat ensures consistent float representation.
// No scientific notation for |x| < 1e15.
func normalizeFloat(f float64) interface{} {
	if math.IsNaN(f) {
		return "NaN"
	}
	if math.IsInf(f, 1) {
		return "Infinity"
	}
	if math.IsInf(f, -1) {
		return "-Infinity"
	}
	// For integers stored as float64, return as integer
	if f == math.Trunc(f) && math.Abs(f) < 1e15 {
		return int64(f)
	}
	return f
}

// marshalSorted produces JSON with keys sorted at every level.
func marshalSorted(v interface{}) ([]byte, error) {
	switch val := v.(type) {
	case map[string]interface{}:
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		var buf strings.Builder
		buf.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			keyJSON, _ := json.Marshal(k)
			buf.Write(keyJSON)
			buf.WriteByte(':')
			valJSON, err := marshalSorted(val[k])
			if err != nil {
				return nil, err
			}
			buf.Write(valJSON)
		}
		buf.WriteByte('}')
		return []byte(buf.String()), nil
	case []interface{}:
		var buf strings.Builder
		buf.WriteByte('[')
		for i, item := range val {
			if i > 0 {
				buf.WriteByte(',')
			}
			itemJSON, err := marshalSorted(item)
			if err != nil {
				return nil, err
			}
			buf.Write(itemJSON)
		}
		buf.WriteByte(']')
		return []byte(buf.String()), nil
	default:
		return json.Marshal(v)
	}
}
