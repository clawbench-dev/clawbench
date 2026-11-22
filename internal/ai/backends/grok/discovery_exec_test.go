package grok

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

// `grok models` is what fills the picker before the first ACP handshake, so the
// parse result has to reach the caller unchanged.
func TestDiscoverGrokModels_UsesTheLiveList(t *testing.T) {
	fakeBinary(t, "grok", "printf 'Default model: grok-4.6\\n  * grok-4.6\\n  - grok-4.5\\n'\n")

	models, reason := discoverGrokModels()
	if reason != "" {
		t.Fatalf("a successful probe must not report a reason, got %q", reason)
	}
	if len(models) != 2 || models[0].ID != "grok-4.6" || !models[0].Default {
		t.Fatalf("expected the parsed list, got %+v", models)
	}
}

func TestDiscoverGrokModels_UnparsableOutputFallsBackToCatalog(t *testing.T) {
	fakeBinary(t, "grok", "printf 'nothing useful here\\n'\n")

	models, reason := discoverGrokModels()
	if len(models) != len(GrokCatalog) {
		t.Fatalf("expected the static catalog, got %d entries", len(models))
	}
	if !strings.Contains(reason, "no models parsed") {
		t.Fatalf("expected a parse-failure reason, got %q", reason)
	}
}

func TestDiscoverGrokModels_MissingBinaryFallsBackToCatalog(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PATH handling differs on Windows")
	}
	t.Setenv("PATH", t.TempDir())

	models, reason := discoverGrokModels()
	if len(models) != len(GrokCatalog) {
		t.Fatalf("expected the static catalog, got %d entries", len(models))
	}
	if !strings.Contains(reason, "failed") {
		t.Fatalf("expected a failure reason, got %q", reason)
	}
}
