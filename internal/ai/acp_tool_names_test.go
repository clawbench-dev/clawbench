package ai

import (
	"testing"

	"github.com/stretchr/testify/assert"

	acp "github.com/coder/acp-go-sdk"
)

func TestExtractToolName_LowerAlias(t *testing.T) {
	result := ExtractToolNameForTest("bash", acp.ToolKindExecute, "")
	assert.Equal(t, "Bash", result)
}

func TestExtractToolName_PatternPrefix(t *testing.T) {
	result := ExtractToolNameForTest("Read file", acp.ToolKindRead, "")
	assert.Equal(t, "Read", result)
}

func TestExtractToolName_SingleWordPassthrough(t *testing.T) {
	result := ExtractToolNameForTest("CustomTool", acp.ToolKindOther, "")
	assert.Equal(t, "CustomTool", result)
}

func TestExtractToolName_AgentSubtype(t *testing.T) {
	result := ExtractToolNameForTest("Explore", acp.ToolKindOther, "")
	assert.Equal(t, "Agent", result)
}

func TestExtractToolName_FilePathFallsToKind(t *testing.T) {
	// Title with dots (like a file path) should fall through to kind mapping
	result := ExtractToolNameForTest("README.md", acp.ToolKindRead, "")
	assert.Equal(t, "Read", result)
}

func TestExtractToolName_KindFallbackViaExported(t *testing.T) {
	// Empty title falls through to kind mapping
	result := ExtractToolNameForTest("", acp.ToolKindExecute, "")
	assert.Equal(t, "Bash", result)
}

func TestExtractToolName_ToolCallIDPrefix(t *testing.T) {
	orig := LookupACPToolCallIDPrefixesFn
	defer func() { LookupACPToolCallIDPrefixesFn = orig }()

	LookupACPToolCallIDPrefixesFn = func(backendID string) map[string]string {
		if backendID == "kimi" {
			return map[string]string{
				"read_file": "Read",
			}
		}
		return nil
	}

	result := ExtractToolNameForTest("read file", acp.ToolKindRead, "kimi", "read_file-123-4")
	assert.Equal(t, "Read", result)
}

func TestExtractToolName_LegacyGlobalPrefix(t *testing.T) {
	orig := LookupACPToolCallIDPrefixesFn
	defer func() { LookupACPToolCallIDPrefixesFn = orig }()

	LookupACPToolCallIDPrefixesFn = nil

	result := ExtractToolNameForTest("", acp.ToolKindRead, "", "read_file-123-4")
	assert.Equal(t, "Read", result)
}

// TestExtractToolName_OtherKindNoSkillCatchAll locks in the root fix: kind=other
// tools whose title is unrecognized must NOT be renamed to "Skill". This was the
// shared catch-all that mislabeled every unknown/control tool as a Skill pill
// (observed on codex subagent lifecycle frames across all ACP backends routed to
// the generic parser, e.g. opencode/mimo/copilot/deepseek/grok/antigravity).
func TestExtractToolName_OtherKindNoSkillCatchAll(t *testing.T) {
	cases := []struct {
		name  string
		title string
		want  string
	}{
		// Multi-word control/unknown frames previously collapsed to "Skill".
		{"multi-word unknown preserved", "Start subagent codebase_research", "Start subagent codebase_research"},
		{"multi-word unknown preserved 2", "Launch background task xyz", "Launch background task xyz"},
		// Empty title + other falls through to the kind enum, not "Skill".
		{"empty title other", "", "other"},
		// Real Skill tools still resolve via the single-word alias table.
		{"lowercase skill alias", "skill", "Skill"},
		{"pascal skill alias", "Skill", "Skill"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, ExtractToolNameForTest(c.title, acp.ToolKindOther, ""))
		})
	}

	// Non-other kinds keep their kind→canonical fallback for empty titles.
	assert.Equal(t, "Bash", ExtractToolNameForTest("", acp.ToolKindExecute, ""))
	assert.Equal(t, "Read", ExtractToolNameForTest("", acp.ToolKindRead, ""))
}
