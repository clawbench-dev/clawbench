package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectContext_LoadsKnownFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("rules A"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("rules B"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "CODEBUDDY.md"), []byte("rules C"), 0o644); err != nil {
		t.Fatal(err)
	}
	// GEMINI.md intentionally absent.

	out := projectContext(dir)
	if len(out) != 3 {
		t.Fatalf("expected 3 context files, got %d: %v", len(out), out)
	}
	if !strings.Contains(out[0], "AGENTS.md") || !strings.Contains(out[0], "rules A") {
		t.Fatalf("expected AGENTS.md first, got: %q", out[0])
	}
	if !strings.Contains(out[1], "CLAUDE.md") || !strings.Contains(out[1], "rules B") {
		t.Fatalf("expected CLAUDE.md second, got: %q", out[1])
	}
	if !strings.Contains(out[2], "CODEBUDDY.md") || !strings.Contains(out[2], "rules C") {
		t.Fatalf("expected CODEBUDDY.md third, got: %q", out[2])
	}
}

func TestProjectContext_EmptyPathOrMissingFiles(t *testing.T) {
	if out := projectContext(""); out != nil {
		t.Fatalf("expected nil for empty path, got %v", out)
	}
	if out := projectContext(t.TempDir()); out != nil {
		t.Fatalf("expected nil when no instruction files exist, got %v", out)
	}
}

func TestProjectContext_CapsFileSize(t *testing.T) {
	dir := t.TempDir()
	big := strings.Repeat("x", projectContextMaxBytes+1000)
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	out := projectContext(dir)
	if len(out) != 1 {
		t.Fatalf("expected 1 context file, got %d", len(out))
	}
	// The injected text includes the header prefix plus the capped file body.
	if !strings.Contains(out[0], strings.Repeat("x", projectContextMaxBytes)) {
		t.Fatalf("expected file content capped at max bytes, got %d bytes", len(out[0]))
	}
	if strings.Contains(out[0], strings.Repeat("x", projectContextMaxBytes+100)) {
		t.Fatal("expected file content to be truncated beyond the cap")
	}
}

func TestProjectContext_ReadmeFallbackOnlyWhenNoInstructionFiles(t *testing.T) {
	// Only README.md present → used as fallback.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("readme intro"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := projectContext(dir)
	if len(out) != 1 {
		t.Fatalf("expected 1 fallback file, got %d: %v", len(out), out)
	}
	if !strings.Contains(out[0], "README.md") || !strings.Contains(out[0], "readme intro") {
		t.Fatalf("expected README.md fallback, got: %q", out[0])
	}

	// Both README.md and an instruction file present → instruction file wins,
	// README.md is NOT included.
	dir2 := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir2, "README.md"), []byte("readme intro"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir2, "AGENTS.md"), []byte("rules A"), 0o644); err != nil {
		t.Fatal(err)
	}
	out2 := projectContext(dir2)
	if len(out2) != 1 {
		t.Fatalf("expected only AGENTS.md (no README fallback), got %d: %v", len(out2), out2)
	}
	if !strings.Contains(out2[0], "AGENTS.md") || strings.Contains(out2[0], "README.md") {
		t.Fatalf("expected AGENTS.md without README, got: %q", out2[0])
	}
}

func TestProjectContext_EmptyInstructionFilesThenReadmeFallback(t *testing.T) {
	// Instruction files exist but are empty → treated as absent → README fallback.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("   \n  "), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("readme intro"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := projectContext(dir)
	if len(out) != 1 || !strings.Contains(out[0], "README.md") {
		t.Fatalf("expected README.md fallback when instruction files are empty, got: %v", out)
	}
}
