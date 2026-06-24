package backends_test

import (
	"testing"

	"clawbench/internal/ai/backends"
	_ "clawbench/internal/ai/backends/claude"
	_ "clawbench/internal/ai/backends/codebuddy"
	_ "clawbench/internal/ai/backends/deepseek"
	_ "clawbench/internal/ai/backends/kimi"
	_ "clawbench/internal/ai/backends/opencode"
	_ "clawbench/internal/ai/backends/pi"
)

// TestACPInitRegistration verifies that each backend's init() correctly
// registers ACP mapping data via backends.Register().
// This test does NOT call ResetForTest() — it validates the real init state.
func TestACPInitRegistration(t *testing.T) {
	t.Run("kimi", func(t *testing.T) {
		p := backends.Lookup("kimi")
		if p == nil {
			t.Fatal("expected kimi plugin to be registered after init")
		}
		if p.ACP == nil {
			t.Fatal("expected kimi ACP to be registered after init")
		}

		// Verify ToolCallIDPrefixes
		prefixes := p.ACP.ToolCallIDPrefixes
		if prefixes == nil {
			t.Fatal("expected kimi ToolCallIDPrefixes to be non-nil")
		}
		expectedPrefixes := map[string]string{
			"read_file": "Read", "list_directory": "LS", "glob": "Glob",
			"run_shell_command": "Bash", "ask": "AskUserQuestion",
			"write_file": "Write", "edit_file": "Edit", "replace": "Edit",
			"search_file": "Grep", "search_directory": "Grep",
		}
		for k, v := range expectedPrefixes {
			if prefixes[k] != v {
				t.Errorf("ToolCallIDPrefixes[%q] = %q, want %q", k, prefixes[k], v)
			}
		}
		if len(prefixes) != len(expectedPrefixes) {
			t.Errorf("ToolCallIDPrefixes has %d entries, want %d", len(prefixes), len(expectedPrefixes))
		}

		// Kimi ACP has no InputRemaps
		if len(p.ACP.InputRemaps) > 0 {
			t.Errorf("expected empty InputRemaps for kimi ACP, got %v", p.ACP.InputRemaps)
		}
	})

	t.Run("opencode", func(t *testing.T) {
		p := backends.Lookup("opencode")
		if p == nil {
			t.Fatal("expected opencode plugin to be registered after init")
		}
		if p.ACP == nil {
			t.Fatal("expected opencode ACP to be registered after init")
		}

		remaps := p.ACP.InputRemaps
		if remaps == nil {
			t.Fatal("expected opencode InputRemaps to be non-nil")
		}
		expected := map[string]string{
			"oldString": "old_string", "newString": "new_string", "replaceAll": "replace_all",
		}
		for k, v := range expected {
			if remaps[k] != v {
				t.Errorf("InputRemaps[%q] = %q, want %q", k, remaps[k], v)
			}
		}
		// OpenCode has no ToolCallIDPrefixes
		if p.ACP.ToolCallIDPrefixes != nil {
			t.Errorf("expected nil ToolCallIDPrefixes for opencode, got %v", p.ACP.ToolCallIDPrefixes)
		}
	})

	t.Run("claude", func(t *testing.T) {
		p := backends.Lookup("claude")
		if p == nil {
			t.Fatal("expected claude plugin to be registered after init")
		}
		if p.ACP == nil {
			t.Fatal("expected claude ACP to be registered after init")
		}
		// Claude ACP has the generic 6-field normalization map
		if len(p.ACP.InputRemaps) == 0 {
			t.Error("expected non-empty InputRemaps for claude ACP (generic fields)")
		}
		if p.ACP.InputRemaps["filePath"] != "file_path" {
			t.Errorf("expected generic filePath->file_path for claude, got %q", p.ACP.InputRemaps["filePath"])
		}
		if p.ACP.ToolCallIDPrefixes != nil {
			t.Errorf("expected nil ToolCallIDPrefixes for claude, got %v", p.ACP.ToolCallIDPrefixes)
		}
	})

	t.Run("codebuddy", func(t *testing.T) {
		p := backends.Lookup("codebuddy")
		if p == nil {
			t.Fatal("expected codebuddy plugin to be registered after init")
		}
		if p.ACP == nil {
			t.Fatal("expected codebuddy ACP to be registered after init")
		}
		// Codebuddy ACP has the generic 6-field normalization map
		if len(p.ACP.InputRemaps) == 0 {
			t.Error("expected non-empty InputRemaps for codebuddy ACP (generic fields)")
		}
		if p.ACP.InputRemaps["filePath"] != "file_path" {
			t.Errorf("expected generic filePath->file_path for codebuddy, got %q", p.ACP.InputRemaps["filePath"])
		}
		if p.ACP.ToolCallIDPrefixes != nil {
			t.Errorf("expected nil ToolCallIDPrefixes for codebuddy, got %v", p.ACP.ToolCallIDPrefixes)
		}
	})

	t.Run("pi", func(t *testing.T) {
		p := backends.Lookup("pi")
		if p == nil {
			t.Fatal("expected pi plugin to be registered after init")
		}
		// Pi does not support ACP — ACP plugin should be nil
		if p.ACP != nil {
			t.Fatal("expected pi ACP to be nil (ACP support removed)")
		}
	})

	t.Run("deepseek", func(t *testing.T) {
		p := backends.Lookup("deepseek")
		if p == nil {
			t.Fatal("expected deepseek plugin to be registered after init")
		}
		if p.ACP == nil {
			t.Fatal("expected deepseek ACP to be registered after init")
		}
		// DeepSeek (CodeWhale) ACP has both InputRemaps and ToolCallIDPrefixes
		remaps := p.ACP.InputRemaps
		if len(remaps) == 0 {
			t.Fatal("expected non-empty InputRemaps for deepseek ACP")
		}
		if remaps["path"] != "file_path" {
			t.Errorf("expected path->file_path, got %q", remaps["path"])
		}
		prefixes := p.ACP.ToolCallIDPrefixes
		if prefixes == nil {
			t.Fatal("expected non-nil ToolCallIDPrefixes for deepseek ACP")
		}
		if prefixes["read_file"] != "Read" {
			t.Errorf("expected read_file->Read, got %q", prefixes["read_file"])
		}
	})
}

// TestACPLookupAfterInit verifies LookupACPRemaps and LookupACPToolCallIDPrefixes
// work correctly with the real init state.
func TestACPLookupAfterInit(t *testing.T) {
	t.Run("kimi_remaps_fallback", func(t *testing.T) {
		// Kimi has empty InputRemaps, so LookupACPRemaps should fall back to generic
		remaps := backends.LookupACPRemaps("kimi")
		if remaps == nil {
			t.Fatal("expected generic fallback remaps for kimi")
		}
		if remaps["oldString"] != "old_string" {
			t.Errorf("expected generic fallback oldString->old_string, got %q", remaps["oldString"])
		}
	})

	t.Run("kimi_prefixes", func(t *testing.T) {
		prefixes := backends.LookupACPToolCallIDPrefixes("kimi")
		if prefixes == nil {
			t.Fatal("expected non-nil prefixes for kimi")
		}
		if prefixes["read_file"] != "Read" {
			t.Errorf("expected read_file->Read, got %q", prefixes["read_file"])
		}
	})

	t.Run("opencode_remaps", func(t *testing.T) {
		remaps := backends.LookupACPRemaps("opencode")
		if remaps == nil {
			t.Fatal("expected non-nil remaps for opencode")
		}
		if remaps["oldString"] != "old_string" {
			t.Errorf("expected oldString->old_string, got %q", remaps["oldString"])
		}
		if remaps["replaceAll"] != "replace_all" {
			t.Errorf("expected replaceAll->replace_all, got %q", remaps["replaceAll"])
		}
	})

	t.Run("claude_remaps_fallback", func(t *testing.T) {
		remaps := backends.LookupACPRemaps("claude")
		if remaps == nil {
			t.Fatal("expected generic fallback remaps for claude")
		}
		if remaps["oldString"] != "old_string" {
			t.Errorf("expected generic fallback oldString->old_string, got %q", remaps["oldString"])
		}
	})

	t.Run("nonexistent_fallback", func(t *testing.T) {
		remaps := backends.LookupACPRemaps("nonexistent_backend")
		if remaps == nil {
			t.Fatal("expected generic fallback remaps for nonexistent backend")
		}
		if remaps["oldString"] != "old_string" {
			t.Errorf("expected generic fallback oldString->old_string, got %q", remaps["oldString"])
		}
	})

	t.Run("nonexistent_prefixes_nil", func(t *testing.T) {
		prefixes := backends.LookupACPToolCallIDPrefixes("nonexistent_backend")
		if prefixes != nil {
			t.Errorf("expected nil prefixes for nonexistent backend, got %v", prefixes)
		}
	})
}
