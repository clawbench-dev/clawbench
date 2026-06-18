package backends_test

import (
	"encoding/json"
	"testing"

	"clawbench/internal/ai"
	_ "clawbench/internal/ai/backends/kimi"
	_ "clawbench/internal/ai/backends/opencode"

	acp "github.com/coder/acp-go-sdk"
)

// TestACPExtractToolName_WithBackendID tests that extractToolName uses
// the registry-based LookupACPToolCallIDPrefixesFn when backendID is provided.
func TestACPExtractToolName_WithBackendID(t *testing.T) {
	// Verify LookupACPToolCallIDPrefixesFn is wired up
	if ai.LookupACPToolCallIDPrefixesFn == nil {
		t.Fatal("LookupACPToolCallIDPrefixesFn should be wired up by acp_wire.go init()")
	}

	t.Run("kimi_prefixes_via_registry", func(t *testing.T) {
		// Kimi-style toolCallID prefix matching should work via the function variable
		got := ai.ExtractToolNameForTest("", acp.ToolKindRead, "kimi", "read_file-1234-5")
		if got != "Read" {
			t.Errorf("extractToolName('' ToolKindRead 'kimi' 'read_file-1234-5') = %q, want 'Read'", got)
		}
		got = ai.ExtractToolNameForTest("", acp.ToolKindExecute, "kimi", "run_shell_command-1234-5")
		if got != "Bash" {
			t.Errorf("extractToolName('' ToolKindExecute 'kimi' 'run_shell_command-1234-5') = %q, want 'Bash'", got)
		}
		got = ai.ExtractToolNameForTest("", acp.ToolKindOther, "kimi", "glob-1234-5")
		if got != "Glob" {
			t.Errorf("extractToolName('' ToolKindOther 'kimi' 'glob-1234-5') = %q, want 'Glob'", got)
		}
	})

	t.Run("empty_backendID_uses_legacy_fallback", func(t *testing.T) {
		// When backendID is empty, the legacy acpToolCallIDPrefix fallback is used
		got := ai.ExtractToolNameForTest("", acp.ToolKindRead, "", "read_file-1234-5")
		if got != "Read" {
			t.Errorf("extractToolName with empty backendID should use legacy fallback, got %q", got)
		}
	})

	t.Run("unknown_backendID_uses_legacy_fallback", func(t *testing.T) {
		got := ai.ExtractToolNameForTest("", acp.ToolKindRead, "unknown_backend", "read_file-1234-5")
		if got != "Read" {
			t.Errorf("extractToolName with unknown backendID should use legacy fallback, got %q", got)
		}
	})
}

// TestACPRemapsForBackend tests that the function variable lookup works
// for backend-specific ACP input remaps.
func TestACPRemapsForBackend(t *testing.T) {
	if ai.LookupACPRemapsFn == nil {
		t.Fatal("LookupACPRemapsFn should be wired up by acp_wire.go init()")
	}

	t.Run("opencode_specific_remix", func(t *testing.T) {
		remaps := ai.LookupACPRemapsFn("opencode")
		if remaps == nil {
			t.Fatal("expected non-nil remaps for opencode")
		}
		if remaps["oldString"] != "old_string" {
			t.Errorf("opencode remaps[oldString] = %q, want 'old_string'", remaps["oldString"])
		}
		if remaps["replaceAll"] != "replace_all" {
			t.Errorf("opencode remaps[replaceAll] = %q, want 'replace_all'", remaps["replaceAll"])
		}
	})

	t.Run("kimi_fallback_to_generic", func(t *testing.T) {
		remaps := ai.LookupACPRemapsFn("kimi")
		// Kimi has empty InputRemaps, so should fall back to generic
		if len(remaps) == 0 {
			t.Error("expected generic fallback remaps for kimi, got empty map")
		}
		if remaps["oldString"] != "old_string" {
			t.Errorf("generic fallback oldString = %q, want 'old_string'", remaps["oldString"])
		}
	})

	t.Run("claude_fallback_to_generic", func(t *testing.T) {
		remaps := ai.LookupACPRemapsFn("claude")
		if len(remaps) == 0 {
			t.Error("expected generic fallback remaps for claude, got empty map")
		}
	})

	t.Run("nonexistent_fallback_to_generic", func(t *testing.T) {
		remaps := ai.LookupACPRemapsFn("nonexistent")
		if len(remaps) == 0 {
			t.Error("expected generic fallback remaps for nonexistent, got empty map")
		}
	})
}

// TestNormalizeToolInput_WithOpenCodeACPRegistry tests that normalizeToolInput
// works correctly when using remaps from the registry for OpenCode ACP.
func TestNormalizeToolInput_WithOpenCodeACPRegistry(t *testing.T) {
	input := json.RawMessage(`{"filePath":"main.go","oldString":"foo","newString":"bar","replaceAll":true}`)
	remaps := ai.LookupACPRemapsFn("opencode")
	norm, err := ai.NormalizeToolInputForTest(input, remaps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(norm, &parsed); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}

	if parsed["file_path"] != "main.go" {
		t.Errorf("file_path = %v, want 'main.go'", parsed["file_path"])
	}
	if parsed["old_string"] != "foo" {
		t.Errorf("old_string = %v, want 'foo'", parsed["old_string"])
	}
	if parsed["new_string"] != "bar" {
		t.Errorf("new_string = %v, want 'bar'", parsed["new_string"])
	}
	if parsed["replace_all"] != true {
		t.Errorf("replace_all = %v, want true", parsed["replace_all"])
	}
}
