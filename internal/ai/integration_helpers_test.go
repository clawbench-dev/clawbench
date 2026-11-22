//go:build integration

package ai

import (
	acp "github.com/coder/acp-go-sdk"
)

// truncate shortens a string for test logs. Shared by the integration probes,
// which print raw agent output that can be arbitrarily long.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// metaToMap flattens an acp.Meta into the plain map the probes print and
// inspect. The SDK stores each value as json.RawMessage; rawJSONValue decodes
// it back to the Go value the probe expects.
func metaToMap(m acp.Meta) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, raw := range m {
		out[k] = rawJSONValue(raw)
	}
	return out
}
