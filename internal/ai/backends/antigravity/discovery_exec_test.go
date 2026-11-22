package antigravity

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeBinary puts an executable named name on PATH. The body is a shell script,
// so the case is skipped on Windows rather than pretending to cover it there.
func fakeBinary(t *testing.T, name, body string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake CLI is a shell script")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatalf("write fake binary: %v", err)
	}
	t.Setenv("PATH", dir)
}

// A signed-in CLI gives the picker the live list; diagnostics on the same stream
// must not become model entries.
func TestDiscoverAntigravityModels_UsesTheLiveList(t *testing.T) {
	fakeBinary(t, "agy", "printf 'gemini-3-pro\\nI0428 10:00:00.000000 1234 log noise\\ngemini-3-flash\\n'\n")

	models, reason := discoverAntigravityModels()
	if reason != "" {
		t.Fatalf("a successful probe must not report a reason, got %q", reason)
	}
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %+v", models)
	}
	for _, m := range models {
		if strings.HasPrefix(m.ID, "I0428") {
			t.Fatalf("log noise must not become a model: %+v", models)
		}
	}
}

func TestDiscoverAntigravityModels_UnparsableOutputFallsBackToCatalog(t *testing.T) {
	fakeBinary(t, "agy", "printf 'I0428 10:00:00.000000 1234 only noise\\n'\n")

	models, reason := discoverAntigravityModels()
	if len(models) != len(AntigravityCatalog) {
		t.Fatalf("expected the static catalog, got %d entries", len(models))
	}
	if !strings.Contains(reason, "no models parsed") {
		t.Fatalf("expected a parse-failure reason, got %q", reason)
	}
}

func TestDiscoverAntigravityModels_MissingBinaryFallsBackToCatalog(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PATH handling differs on Windows")
	}
	t.Setenv("PATH", t.TempDir())

	models, reason := discoverAntigravityModels()
	if len(models) != len(AntigravityCatalog) {
		t.Fatalf("expected the static catalog, got %d entries", len(models))
	}
	if !strings.Contains(reason, "failed") {
		t.Fatalf("expected a failure reason, got %q", reason)
	}
}
