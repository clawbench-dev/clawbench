package ai

import (
	"encoding/json"
)


// normalizeToolInput remaps camelCase input field names to canonical snake_case.
// It accepts an optional pathMappings map to rename additional fields (e.g., dirPath->path).
// If rawInput is not valid JSON, it returns the input unchanged.
//
// The defaultMappings provide standard camelCase → snake_case remappings shared by all
// backends (e.g., filePath → file_path). These are applied first; caller-provided
// pathMappings can override them if needed (e.g., mapping filePath to a different target).
func normalizeToolInput(rawInput []byte, pathMappings map[string]string) ([]byte, error) {
	if len(rawInput) == 0 {
		return rawInput, nil
	}

	var input map[string]any
	if err := json.Unmarshal(rawInput, &input); err != nil {
		return rawInput, err
	}

	// Default camelCase → snake_case remappings shared by all backends.
	// Callers can override these via pathMappings by providing a mapping
	// for the same source key (e.g., filePath → custom_path).
	defaultMappings := map[string]string{
		"filePath": "file_path",
		"cmd":      "command",
		"exec":     "command",
	}

	// Merge: caller pathMappings take precedence over defaults.
	// If a caller maps the same source key to a different target,
	// the caller's mapping wins.
	merged := make(map[string]string, len(defaultMappings)+len(pathMappings))
	for k, v := range defaultMappings {
		merged[k] = v
	}
	for k, v := range pathMappings {
		merged[k] = v
	}

	// Apply the remappings in a single pass. A rule may only consume a key the
	// frame ACTUALLY carried: without this snapshot, one rule's output becomes
	// another rule's input and the result depends on map iteration order. That
	// is not hypothetical — deepseek's table maps dirPath→path and path→file_path,
	// so a directory argument used to land on either "path" or "file_path"
	// depending on which rule ran first.
	original := make(map[string]struct{}, len(input))
	for k := range input {
		original[k] = struct{}{}
	}
	for from, to := range merged {
		if _, carried := original[from]; !carried {
			continue
		}
		v, ok := input[from]
		if !ok {
			continue
		}
		delete(input, from)
		input[to] = v
	}

	normalized, err := json.Marshal(input)
	if err != nil {
		return rawInput, err
	}
	return normalized, nil
}

// NormalizeToolInputForTest exports normalizeToolInput for use in integration tests.
// Production code must not use this.
func NormalizeToolInputForTest(rawInput []byte, pathMappings map[string]string) ([]byte, error) {
	return normalizeToolInput(rawInput, pathMappings)
}

// execCommandJSON is a shared helper that returns canonical {"command":"..."} JSON
// for Bash tool call input normalization. Used by codex_stream.go for its resume
// output parser.
func execCommandJSON(command string) string {
	m := map[string]string{"command": command}
	b, _ := json.Marshal(m)
	return string(b)
}
