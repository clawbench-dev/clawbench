package agentcore

import (
	"os"
	"path/filepath"
	"testing"

	"clawbench/internal/agentcore/llm"
)

// writeFile is a test helper that writes a.txt under root, creating parents.
//
// Every caller writes the same file: the tests are about how a conversation
// references it, not about directory layouts.
func writeFile(t *testing.T, root, content string) {
	t.Helper()
	full := filepath.Join(root, "a.txt")
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write a.txt: %v", err)
	}
}

// toolNames extracts the declared tool names from a request.
func toolNames(specs []llm.ToolSpec) []string {
	names := make([]string, 0, len(specs))
	for _, s := range specs {
		names = append(names, s.Name)
	}
	return names
}

// contains reports whether list holds want.
func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// containsKind reports whether kinds holds want.
func containsKind(kinds []EventKind, want EventKind) bool {
	for _, k := range kinds {
		if k == want {
			return true
		}
	}
	return false
}
