package agentcore

import (
	"os"
	"path/filepath"
	"testing"

	"clawbench/internal/agentcore/llm"
)

// writeFile is a test helper that writes a file under root, creating parents.
func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
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
