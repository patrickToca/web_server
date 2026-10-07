package credentials

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// parseYAMLSecrets decodes the decrypted YAML into a flat string map.
//
// The SOPS file is expected to be a single-level mapping of string
// keys to string values. Nested maps are rejected: a secret is a
// scalar, and a nested structure means the file has drifted from the
// shape the loader expects.
func parseYAMLSecrets(data []byte) (map[string]string, error) {
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, err
	}

	out := make(map[string]string, len(raw))
	for key, value := range raw {
		switch v := value.(type) {
		case string:
			out[key] = v
		case nil:
			out[key] = ""
		case int, int64, float64, bool:
			// A secret that happens to be numeric or boolean. Convert
			// to its string form. This is a convenience; the SOPS file
			// should use quoted strings for all values to avoid
			// ambiguity about leading zeros and similar.
			out[key] = fmt.Sprintf("%v", v)
		default:
			return nil, fmt.Errorf(
				"key %q has non-scalar value of type %T; "+
					"secrets must be strings", key, value)
		}
	}
	return out, nil
}
