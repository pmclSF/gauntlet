package assertions

import "fmt"

func specString(spec map[string]interface{}, key string) (string, bool) {
	if spec == nil {
		return "", false
	}
	v, ok := spec[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

func specInt(spec map[string]interface{}, key string) (int, bool) {
	if spec == nil {
		return 0, false
	}
	v, ok := spec[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	default:
		return 0, false
	}
}

func specFloat(spec map[string]interface{}, key string) (float64, bool) {
	if spec == nil {
		return 0, false
	}
	v, ok := spec[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}

func specBool(spec map[string]interface{}, key string) (bool, bool) {
	if spec == nil {
		return false, false
	}
	v, ok := spec[key]
	if !ok {
		return false, false
	}
	switch b := v.(type) {
	case bool:
		return b, true
	default:
		return false, false
	}
}

func specStringSlice(spec map[string]interface{}, key string) []string {
	if spec == nil {
		return nil
	}
	v, ok := spec[key]
	if !ok {
		return nil
	}
	list, ok := v.([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		if s, ok := item.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

func asStringMap(v interface{}) (map[string]interface{}, error) {
	switch m := v.(type) {
	case map[string]interface{}:
		result := make(map[string]interface{}, len(m))
		for k, val := range m {
			converted, err := convertYAMLValue(val)
			if err != nil {
				return nil, err
			}
			result[k] = converted
		}
		return result, nil
	case map[interface{}]interface{}:
		result := make(map[string]interface{}, len(m))
		for k, val := range m {
			ks, ok := k.(string)
			if !ok {
				return nil, fmt.Errorf("schema key %v is not a string", k)
			}
			converted, err := convertYAMLValue(val)
			if err != nil {
				return nil, err
			}
			result[ks] = converted
		}
		return result, nil
	default:
		return nil, fmt.Errorf("schema must be an object")
	}
}

func convertYAMLValue(v interface{}) (interface{}, error) {
	switch t := v.(type) {
	case map[string]interface{}, map[interface{}]interface{}:
		return asStringMap(t)
	case []interface{}:
		out := make([]interface{}, 0, len(t))
		for _, item := range t {
			converted, err := convertYAMLValue(item)
			if err != nil {
				return nil, err
			}
			out = append(out, converted)
		}
		return out, nil
	default:
		return v, nil
	}
}
