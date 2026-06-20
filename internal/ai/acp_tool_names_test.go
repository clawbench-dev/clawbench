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
