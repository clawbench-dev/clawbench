package ai

import (
	"encoding/json"
	"testing"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/ai/acptest"
)

// The two parser builders are the shared half of every backend's tool mapping.
// These tests drive them directly so the contract is pinned here, not only
// through the backends that happen to use them (those tests live in other
// packages and do not contribute to this package's coverage).

func TestNewMetaToolCallParser_NameFromMeta(t *testing.T) {
	parse, _ := NewMetaToolCallParser("meta", func(meta map[string]any) string {
		name, _ := meta["name"].(string)
		return name
	})

	tc := acp.SessionUpdateToolCall{
		ToolCallId: "call-1",
		Title:      "Edit file",
		Kind:       acp.ToolKindEdit,
		Meta:       acptest.Meta(map[string]any{"name": "Edit"}),
		RawInput:   acptest.Raw(map[string]any{"filePath": "/tmp/a.go", "oldString": "a", "newString": "b"}),
	}
	tool := parse(tc)

	assert.Equal(t, "Edit", tool.Name, "_meta wins over the title")
	assert.Equal(t, "call-1", tool.ID)
	assert.False(t, tool.Done)

	var input map[string]any
	require.NoError(t, json.Unmarshal([]byte(tool.Input), &input))
	assert.Equal(t, "/tmp/a.go", input["file_path"], "defaultMappings apply")
	assert.Equal(t, "a", input["old_string"], "the shared generic table applies")
	assert.Equal(t, "b", input["new_string"])
}

func TestNewMetaToolCallParser_FallsBackToTitleWhenMetaHasNoName(t *testing.T) {
	parse, _ := NewMetaToolCallParser("meta", func(map[string]any) string { return "" })

	tool := parse(acp.SessionUpdateToolCall{
		ToolCallId: "call-2",
		Title:      "Read",
		Kind:       acp.ToolKindRead,
	})
	assert.NotEmpty(t, tool.Name, "a missing _meta name must not leave the tool unnamed")
}

func TestNewMetaToolCallParser_NilNameFnMeansTitleOnly(t *testing.T) {
	parse, _ := NewMetaToolCallParser("plain", nil)

	tool := parse(acp.SessionUpdateToolCall{
		ToolCallId: "call-3",
		Title:      "Bash",
		Kind:       acp.ToolKindExecute,
		Meta:       acptest.Meta(map[string]any{"name": "Ignored"}),
	})
	assert.Equal(t, "Bash", tool.Name, "a nil name source must ignore _meta entirely")
}

func TestNewMetaToolCallParser_Update(t *testing.T) {
	_, parseUpdate := NewMetaToolCallParser("meta", func(meta map[string]any) string {
		name, _ := meta["name"].(string)
		return name
	})

	done := acp.ToolCallStatusCompleted
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: "call-4",
		Status:     acptest.Patch(done),
		Meta:       acptest.PatchMeta(map[string]any{"name": "Edit"}),
		RawInput:   acptest.PatchRaw(map[string]any{"filePath": "/tmp/b.go"}),
		RawOutput:  acptest.PatchRaw("applied"),
	}
	tool := parseUpdate(tcu)

	assert.Equal(t, "Edit", tool.Name)
	assert.True(t, tool.Done)
	assert.Equal(t, "success", tool.Status)
	assert.Contains(t, tool.Input, "file_path")
	assert.Contains(t, tool.Output, "applied")
}

func TestNewNormalizingToolCallParser(t *testing.T) {
	normalize := func(toolName string, raw []byte) string {
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		m["normalized_for"] = toolName
		out, _ := json.Marshal(m)
		return string(out)
	}
	nameFromTitle := func(title string) string { return title }

	parse, parseUpdate := NewNormalizingToolCallParser("norm", nameFromTitle, normalize)

	tool := parse(acp.SessionUpdateToolCall{
		ToolCallId: "call-5",
		Title:      "edit",
		Kind:       acp.ToolKindEdit,
		RawInput:   acptest.Raw(map[string]any{"path": "/tmp/c.go"}),
	})
	var input map[string]any
	require.NoError(t, json.Unmarshal([]byte(tool.Input), &input))
	assert.Equal(t, "edit", input["normalized_for"], "the normalizer receives the title-derived tool name")
	assert.Equal(t, "/tmp/c.go", input["path"])

	// The update path must use the same normalizer when RawInput is present.
	done := acp.ToolCallStatusCompleted
	upd := parseUpdate(acp.SessionToolCallUpdate{
		ToolCallId: "call-6",
		Status:     acptest.Patch(done),
		Title:      acptest.Patch("read"),
		RawInput:   acptest.PatchRaw(map[string]any{"path": "/tmp/d.go"}),
	})
	require.NoError(t, json.Unmarshal([]byte(upd.Input), &input))
	assert.Equal(t, "read", input["normalized_for"])
	assert.True(t, upd.Done)
}

func TestNewNormalizingToolCallParser_UpdateWithoutRawInputFallsBack(t *testing.T) {
	normalize := func(string, []byte) string { return "{}" }
	_, parseUpdate := NewNormalizingToolCallParser("norm", func(t string) string { return t }, normalize)

	tool := parseUpdate(acp.SessionToolCallUpdate{
		ToolCallId: "call-7",
		Title:      acptest.Patch("ls"),
		Kind:       acptest.Patch(acp.ToolKindRead),
	})
	assert.NotEmpty(t, tool.Name, "the title must still name the tool when there is no rawInput")
}
