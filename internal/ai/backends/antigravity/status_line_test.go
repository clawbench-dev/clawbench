package antigravity

import "testing"

// `agy models` mixes the model list with diagnostics. Every diagnostic form the
// CLI has been observed to print must be dropped, or the picker offers a
// sentence as if it were a model.
func TestIsAgyStatusLine(t *testing.T) {
	status := []string{
		"Fetching available models...",
		"You are not logged into Antigravity",
		"Error: not signed in",
		"error",
		"ERROR: boom",
		"Failed to fetch models",
		"I0428 10:00:00.000000 1234 something",
		"W0428 10:00:00.000000 1234 something",
	}
	for _, line := range status {
		if !isAgyStatusLine(line) {
			t.Fatalf("%q must be treated as diagnostic output", line)
		}
	}

	models := []string{
		"gemini-3-pro",
		"Gemini 3 Pro",
		"",
		"fetched", // only the exact "Fetching…" sentence is a status line
	}
	for _, line := range models {
		if isAgyStatusLine(line) {
			t.Fatalf("%q must be treated as a model entry", line)
		}
	}
}
