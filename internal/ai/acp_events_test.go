package ai

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	acp "github.com/coder/acp-go-sdk"

	"clawbench/internal/model"
)

// --- mapToolCallStatus tests ---

func TestMapToolCallStatus_Nil(t *testing.T) {
	tool := &ToolCall{}
	mapToolCallStatus(nil, tool)
	assert.False(t, tool.Done)
	assert.Equal(t, "", tool.Status)
}

func TestMapToolCallStatus_Completed(t *testing.T) {
	status := acp.ToolCallStatusCompleted
	tool := &ToolCall{}
	mapToolCallStatus(&status, tool)
	assert.True(t, tool.Done)
	assert.Equal(t, "success", tool.Status)
}

func TestMapToolCallStatus_Failed(t *testing.T) {
	status := acp.ToolCallStatusFailed
	tool := &ToolCall{}
	mapToolCallStatus(&status, tool)
	assert.True(t, tool.Done)
	assert.Equal(t, "error", tool.Status)
}

func TestMapToolCallStatus_Pending(t *testing.T) {
	status := acp.ToolCallStatusPending
	tool := &ToolCall{}
	mapToolCallStatus(&status, tool)
	assert.False(t, tool.Done)
	assert.Equal(t, "", tool.Status)
}

func TestMapToolCallStatus_InProgress(t *testing.T) {
	status := acp.ToolCallStatusInProgress
	tool := &ToolCall{}
	mapToolCallStatus(&status, tool)
	assert.False(t, tool.Done)
	assert.Equal(t, "", tool.Status)
}

// --- mapToolCallInput tests ---

func TestMapToolCallInput_RawInputNormalization(t *testing.T) {
	// RawInput with camelCase keys should be normalized to snake_case
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: acp.ToolCallId("tc-norm"),
		RawInput: map[string]any{
			"filePath":  "/tmp/test.go",
			"oldString": "old",
			"newString": "new",
		},
	}
	tool := &ToolCall{}
	mapToolCallInput(tcu, tool, "")
	assert.Contains(t, tool.Input, "file_path")
	assert.Contains(t, tool.Input, "old_string")
	assert.Contains(t, tool.Input, "new_string")
	assert.NotContains(t, tool.Input, "filePath")
	assert.NotContains(t, tool.Input, "oldString")
}

func TestMapToolCallInput_RawInputEmptyObject(t *testing.T) {
	// Empty object "{}" should not set tool.Input
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: acp.ToolCallId("tc-empty"),
		RawInput:   map[string]any{},
	}
	tool := &ToolCall{}
	mapToolCallInput(tcu, tool, "")
	assert.Equal(t, "", tool.Input, "empty rawInput object should not set input")
}

func TestMapToolCallInput_RawInputReturnsEarly(t *testing.T) {
	// When RawInput is present, should return early and not process execute/locations
	kind := acp.ToolKindExecute
	title := "ls"
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: acp.ToolCallId("tc-raw"),
		RawInput:   map[string]any{"command": "rm -rf /"},
		Kind:       &kind,
		Title:      &title,
	}
	tool := &ToolCall{}
	mapToolCallInput(tcu, tool, "")
	assert.Contains(t, tool.Input, "rm -rf /")
	assert.NotContains(t, tool.Input, "ls", "RawInput takes priority over title")
}

// --- mapToolCallInputFromExecute tests ---

func TestMapToolCallInputFromExecute_WithTitle(t *testing.T) {
	title := "npm run build"
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: acp.ToolCallId("tc-exec-title"),
		Title:      &title,
	}
	tool := &ToolCall{}
	mapToolCallInputFromExecute(tcu, tool)
	assert.Contains(t, tool.Input, "command")
	assert.Contains(t, tool.Input, "npm run build")
}

func TestMapToolCallInputFromExecute_NoTitleTerminalContent(t *testing.T) {
	// Terminal content but no title → empty command
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: acp.ToolCallId("tc-exec-notitle"),
		Title:      nil,
		Content: []acp.ToolCallContent{
			{Terminal: &acp.ToolCallContentTerminal{TerminalId: "term-1"}},
		},
	}
	tool := &ToolCall{}
	mapToolCallInputFromExecute(tcu, tool)
	// With nil title, no command key is added → input remains empty
	assert.Equal(t, "", tool.Input)
}

func TestMapToolCallInputFromExecute_TerminalContentWithTitle(t *testing.T) {
	title := "go test ./..."
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: acp.ToolCallId("tc-exec-term"),
		Title:      &title,
		Content: []acp.ToolCallContent{
			{Terminal: &acp.ToolCallContentTerminal{TerminalId: "term-1"}},
		},
	}
	tool := &ToolCall{}
	mapToolCallInputFromExecute(tcu, tool)
	assert.Contains(t, tool.Input, "command")
	assert.Contains(t, tool.Input, "go test ./...")
}

func TestMapToolCallInputFromExecute_EmptyTitleNoContent(t *testing.T) {
	title := ""
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: acp.ToolCallId("tc-exec-empty"),
		Title:      &title,
	}
	tool := &ToolCall{}
	mapToolCallInputFromExecute(tcu, tool)
	assert.Equal(t, "", tool.Input, "empty title should not set input")
}

func TestMapToolCallInputFromExecute_NoTitleNoContent(t *testing.T) {
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: acp.ToolCallId("tc-exec-none"),
		Title:      nil,
	}
	tool := &ToolCall{}
	mapToolCallInputFromExecute(tcu, tool)
	assert.Equal(t, "", tool.Input)
}

// --- mapToolCallInputFromLocations tests ---

func TestMapToolCallInputFromLocations_AlreadySet(t *testing.T) {
	// If tool.Input already set, should return early
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: acp.ToolCallId("tc-loc-set"),
	}
	tool := &ToolCall{Input: `{"command":"ls"}`}
	mapToolCallInputFromLocations(tcu, tool)
	assert.Equal(t, `{"command":"ls"}`, tool.Input, "should not overwrite existing input")
}

func TestMapToolCallInputFromLocations_ReadKindWithLocations(t *testing.T) {
	kind := acp.ToolKindRead
	title := "main.go"
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: acp.ToolCallId("tc-loc-read"),
		Kind:       &kind,
		Title:      &title,
		Locations: []acp.ToolCallLocation{
			{Path: "/home/user/main.go"},
		},
	}
	tool := &ToolCall{}
	mapToolCallInputFromLocations(tcu, tool)
	assert.Contains(t, tool.Input, "file_path")
	assert.Contains(t, tool.Input, "/home/user/main.go")
}

func TestMapToolCallInputFromLocations_NilKindDefaultsToOther(t *testing.T) {
	// When kind is nil, defaults to ToolKindOther — extractInputFromLocationsAndTitle returns nil for Other
	title := "search query"
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: acp.ToolCallId("tc-loc-nil"),
		Kind:       nil,
		Title:      &title,
	}
	tool := &ToolCall{}
	mapToolCallInputFromLocations(tcu, tool)
	assert.Equal(t, "", tool.Input, "ToolKindOther with no matching prefix should not set input")
}

func TestMapToolCallInputFromLocations_NilTitle(t *testing.T) {
	kind := acp.ToolKindRead
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: acp.ToolCallId("tc-loc-notitle"),
		Kind:       &kind,
		Title:      nil,
		Locations: []acp.ToolCallLocation{
			{Path: "/home/user/main.go"},
		},
	}
	tool := &ToolCall{}
	mapToolCallInputFromLocations(tcu, tool)
	assert.Contains(t, tool.Input, "file_path")
}

// --- mapToolCallName tests ---

func TestMapToolCallName_NilTitle(t *testing.T) {
	tool := &ToolCall{}
	tcu := acp.SessionToolCallUpdate{
		Title: nil,
	}
	mapToolCallName(tcu, tool, "")
	assert.Equal(t, "", tool.Name)
}

func TestMapToolCallName_EmptyTitle(t *testing.T) {
	tool := &ToolCall{}
	title := ""
	tcu := acp.SessionToolCallUpdate{
		Title: &title,
	}
	mapToolCallName(tcu, tool, "")
	assert.Equal(t, "", tool.Name)
}

func TestMapToolCallName_DoneToolSkipsName(t *testing.T) {
	tool := &ToolCall{Done: true}
	title := "Read"
	tcu := acp.SessionToolCallUpdate{
		Title: &title,
	}
	mapToolCallName(tcu, tool, "")
	assert.Equal(t, "", tool.Name, "done tool should not update name from title")
}

func TestMapToolCallName_InProgressUpdatesName(t *testing.T) {
	tool := &ToolCall{}
	title := "Read"
	tcu := acp.SessionToolCallUpdate{
		Title: &title,
	}
	mapToolCallName(tcu, tool, "")
	assert.Equal(t, "Read", tool.Name)
}

func TestMapToolCallName_NilKindDefaultsToExecute(t *testing.T) {
	// When kind is nil, extractToolName defaults kind to ToolKindExecute
	tool := &ToolCall{}
	title := "bash"
	tcu := acp.SessionToolCallUpdate{
		Title: &title,
		Kind:  nil,
	}
	mapToolCallName(tcu, tool, "")
	assert.Equal(t, "Bash", tool.Name, "nil kind defaults to execute, lowercase 'bash' maps to Bash")
}

func TestMapToolCallName_WithKind(t *testing.T) {
	tool := &ToolCall{}
	kind := acp.ToolKindRead
	title := "Read file contents"
	tcu := acp.SessionToolCallUpdate{
		Title: &title,
		Kind:  &kind,
	}
	mapToolCallName(tcu, tool, "")
	assert.Equal(t, "Read", tool.Name)
}

// --- mapToolCallOutput tests ---

func TestMapToolCallOutput_RawOutput(t *testing.T) {
	tcu := acp.SessionToolCallUpdate{
		RawOutput: map[string]any{"result": "file contents here"},
	}
	tool := &ToolCall{}
	mapToolCallOutput(tcu, tool)
	assert.Contains(t, tool.Output, "file contents here")
}

func TestMapToolCallOutput_ContentBlocks(t *testing.T) {
	tcu := acp.SessionToolCallUpdate{
		Content: []acp.ToolCallContent{
			{
				Content: &acp.ToolCallContentContent{
					Content: acp.ContentBlock{
						Text: &acp.ContentBlockText{Text: "output from content"},
					},
				},
			},
		},
	}
	tool := &ToolCall{}
	mapToolCallOutput(tcu, tool)
	assert.Contains(t, tool.Output, "output from content")
}

func TestMapToolCallOutput_NoOutput(t *testing.T) {
	tcu := acp.SessionToolCallUpdate{}
	tool := &ToolCall{}
	mapToolCallOutput(tcu, tool)
	assert.Equal(t, "", tool.Output)
}

// --- extractACPToolOutput additional tests ---

func TestExtractACPToolOutput_Float32(t *testing.T) {
	result := extractACPToolOutput(float32(3.14))
	assert.Equal(t, "3.14", result)
}

func TestExtractACPToolOutput_Int64(t *testing.T) {
	result := extractACPToolOutput(int64(42))
	assert.Equal(t, "42", result)
}

func TestExtractACPToolOutput_Int32(t *testing.T) {
	result := extractACPToolOutput(int32(7))
	assert.Equal(t, "7", result)
}

func TestExtractACPToolOutput_Float64(t *testing.T) {
	result := extractACPToolOutput(float64(2.718))
	assert.Equal(t, "2.718", result)
}

func TestExtractACPToolOutput_Int(t *testing.T) {
	result := extractACPToolOutput(100)
	assert.Equal(t, "100", result)
}

func TestExtractACPToolOutput_Map_StdoutWithEmptyStderr(t *testing.T) {
	// stdout present but stderr is empty string → should not append stderr
	result := extractACPToolOutput(map[string]any{
		"stdout": "output only",
		"stderr": "",
	})
	assert.Equal(t, "output only", result)
}

func TestExtractACPToolOutput_Map_StdoutWithNilStderr(t *testing.T) {
	// stdout present but stderr is nil → should not append
	result := extractACPToolOutput(map[string]any{
		"stdout": "output only",
		"stderr": nil,
	})
	assert.Equal(t, "output only", result)
}

func TestExtractACPToolOutput_Map_NestedMap(t *testing.T) {
	result := extractACPToolOutput(map[string]any{
		"result": map[string]any{"nested": "value"},
	})
	assert.Contains(t, result, `"nested"`)
	assert.Contains(t, result, `"value"`)
}

func TestExtractACPToolOutput_Map_NestedArray(t *testing.T) {
	result := extractACPToolOutput(map[string]any{
		"result": []any{"a", "b"},
	})
	assert.Contains(t, result, `"a"`)
	assert.Contains(t, result, `"b"`)
}

func TestExtractACPToolOutput_Map_DefaultValue(t *testing.T) {
	// Non-string, non-map, non-array value in a priority key
	result := extractACPToolOutput(map[string]any{
		"result": 42,
	})
	assert.Equal(t, "42", result)
}

func TestExtractACPToolOutput_Map_NilValue(t *testing.T) {
	// nil value in priority key should be skipped
	result := extractACPToolOutput(map[string]any{
		"result": nil,
		"output": "fallback",
	})
	assert.Equal(t, "fallback", result)
}

func TestExtractACPToolOutput_Map_ErrorKeyString(t *testing.T) {
	result := extractACPToolOutput(map[string]any{
		"error": "permission denied",
	})
	assert.Equal(t, "permission denied", result)
}

func TestExtractACPToolOutput_Map_ErrorKeyMapNoMessage(t *testing.T) {
	// Error key with map but no "message" key → falls to fmt.Sprintf
	result := extractACPToolOutput(map[string]any{
		"error": map[string]any{"code": 403},
	})
	assert.Contains(t, result, "403")
}

func TestExtractACPToolOutput_Map_ErrorKeyNonStringNonMap(t *testing.T) {
	// Error key with non-string, non-map value → fmt.Sprintf
	result := extractACPToolOutput(map[string]any{
		"error": 500,
	})
	assert.Equal(t, "500", result)
}

func TestExtractACPToolOutput_Array_EmptyFromEvents(t *testing.T) {
	result := extractACPToolOutput([]any{})
	// Empty array → allStrings is true but len(parts)==0 → falls to JSON
	assert.Equal(t, "[]", result)
}

func TestExtractACPToolOutput_Array_SingleString(t *testing.T) {
	result := extractACPToolOutput([]any{"only one"})
	assert.Equal(t, "only one", result)
}

func TestExtractACPToolOutput_UnmarshallableType(t *testing.T) {
	// Type that can't be marshaled to JSON → falls to fmt.Sprintf
	result := extractACPToolOutput(make(chan int))
	assert.NotEmpty(t, result)
}

// --- extractMapOutput additional tests ---

func TestExtractMapOutput_EmptyMap(t *testing.T) {
	// All priority keys absent, no error key → pretty-print empty map
	result := extractMapOutput(map[string]any{})
	assert.Equal(t, "{}", result)
}

func TestExtractMapOutput_OnlyUnknownKeys(t *testing.T) {
	result := extractMapOutput(map[string]any{
		"custom_a": "val_a",
		"custom_b": "val_b",
	})
	assert.Contains(t, result, `"custom_a"`)
	assert.Contains(t, result, `"val_a"`)
}

// --- extractArrayOutput additional tests ---

func TestExtractArrayOutput_MixedTypes(t *testing.T) {
	// Array with non-string element → JSON pretty-print
	result := extractArrayOutput([]any{"text", 42})
	assert.Contains(t, result, `"text"`)
}

func TestExtractArrayOutput_AllStrings(t *testing.T) {
	result := extractArrayOutput([]any{"alpha", "beta"})
	assert.Equal(t, "alpha\nbeta", result)
}

// --- extractInputFromLocationsAndTitle additional tests ---

func TestExtractInputFromLocationsAndTitle_SearchDirectoryPrefix(t *testing.T) {
	input := extractInputFromLocationsAndTitle(
		nil,
		"src/pkg",
		acp.ToolKindSearch,
		"search_directory-456-7",
	)
	require.NotNil(t, input)
	assert.Equal(t, "src/pkg", input["path"])
}

func TestExtractInputFromLocationsAndTitle_ReadNoLocationsNoTitle(t *testing.T) {
	input := extractInputFromLocationsAndTitle(
		nil,
		"",
		acp.ToolKindRead,
		"read_file-123-1",
	)
	assert.Nil(t, input, "read kind with no locations and no title should return nil")
}

func TestExtractInputFromLocationsAndTitle_SearchNoTitle(t *testing.T) {
	input := extractInputFromLocationsAndTitle(
		nil,
		"",
		acp.ToolKindSearch,
		"glob-123-4",
	)
	assert.Nil(t, input, "search kind with no title should return nil")
}

func TestExtractInputFromLocationsAndTitle_EditNoLocations(t *testing.T) {
	input := extractInputFromLocationsAndTitle(
		nil,
		"file.go",
		acp.ToolKindEdit,
		"edit_file-123-2",
	)
	// Edit without locations → no input (title not used for edit kind)
	assert.Nil(t, input)
}

func TestExtractInputFromLocationsAndTitle_OtherKind(t *testing.T) {
	input := extractInputFromLocationsAndTitle(
		nil,
		"something",
		acp.ToolKindOther,
		"other-123-1",
	)
	assert.Nil(t, input, "Other kind is not handled by this function")
}

// --- extractToolName additional edge cases ---

func TestExtractToolName_EmptyToolCallID(t *testing.T) {
	assert.Equal(t, "Read", extractToolName("Read", acp.ToolKindRead, "", ""))
}

func TestExtractToolName_ToolCallIDNoDash(t *testing.T) {
	// toolCallID with no dash → no prefix extracted → falls to title/kind matching
	assert.Equal(t, "Bash", extractToolName("Bash", acp.ToolKindExecute, "", "nodash"))
}

func TestExtractToolName_TitleWithDotAndSlash(t *testing.T) {
	// Title with both dot and slash → falls through to kind mapping
	assert.Equal(t, "Read", extractToolName("src/pkg/main.go", acp.ToolKindRead, ""))
}

func TestExtractToolName_TitleWithOnlySlash(t *testing.T) {
	// Title with slash → falls through to kind mapping
	assert.Equal(t, "Read", extractToolName("src/main", acp.ToolKindRead, ""))
}

func TestExtractToolName_ReplacePrefix(t *testing.T) {
	assert.Equal(t, "Edit", extractToolName("file.go", acp.ToolKindEdit, "kimi", "replace-123-1"))
}

func TestExtractToolName_LowerAliasAgent(t *testing.T) {
	assert.Equal(t, "Agent", extractToolName("agent", acp.ToolKindOther, ""))
}

func TestExtractToolName_LowerAliasSkill(t *testing.T) {
	assert.Equal(t, "Skill", extractToolName("skill", acp.ToolKindOther, ""))
}

func TestExtractToolName_LowerAliasList(t *testing.T) {
	assert.Equal(t, "LS", extractToolName("list", acp.ToolKindOther, ""))
}

// --- extractInputFromContent additional tests ---

func TestExtractInputFromContent_TerminalWithTitle(t *testing.T) {
	tc := acp.SessionUpdateToolCall{
		Title: "npm install",
		Content: []acp.ToolCallContent{
			{Terminal: &acp.ToolCallContentTerminal{TerminalId: "term-1"}},
		},
	}
	input := extractInputFromContent(tc)
	require.NotNil(t, input)
	assert.Equal(t, "npm install", input["command"])
}

func TestExtractInputFromContent_TerminalNoTitle(t *testing.T) {
	tc := acp.SessionUpdateToolCall{
		Title: "",
		Content: []acp.ToolCallContent{
			{Terminal: &acp.ToolCallContentTerminal{TerminalId: "term-1"}},
		},
	}
	input := extractInputFromContent(tc)
	require.NotNil(t, input)
	_, hasCommand := input["command"]
	assert.False(t, hasCommand, "terminal without title should not add command key")
}

func TestExtractInputFromContent_TextContent(t *testing.T) {
	tc := acp.SessionUpdateToolCall{
		Content: []acp.ToolCallContent{
			{
				Content: &acp.ToolCallContentContent{
					Content: acp.ContentBlock{
						Text: &acp.ContentBlockText{Text: "A description"},
					},
				},
			},
		},
	}
	input := extractInputFromContent(tc)
	require.NotNil(t, input)
	assert.Equal(t, "A description", input["description"])
}

func TestExtractInputFromContent_TextContentEmpty(t *testing.T) {
	tc := acp.SessionUpdateToolCall{
		Content: []acp.ToolCallContent{
			{
				Content: &acp.ToolCallContentContent{
					Content: acp.ContentBlock{
						Text: &acp.ContentBlockText{Text: ""},
					},
				},
			},
		},
	}
	input := extractInputFromContent(tc)
	assert.Nil(t, input, "empty text content should return nil")
}

func TestExtractInputFromContent_NilContentBlock(t *testing.T) {
	tc := acp.SessionUpdateToolCall{
		Content: []acp.ToolCallContent{
			{
				Content: &acp.ToolCallContentContent{
					Content: acp.ContentBlock{}, // Text is nil
				},
			},
		},
	}
	input := extractInputFromContent(tc)
	assert.Nil(t, input)
}

func TestExtractInputFromContent_Empty(t *testing.T) {
	tc := acp.SessionUpdateToolCall{
		Content: []acp.ToolCallContent{},
	}
	input := extractInputFromContent(tc)
	assert.Nil(t, input)
}

// --- extractACPModeState tests ---

func TestExtractACPModeState_Nil(t *testing.T) {
	assert.Nil(t, extractACPModeState(nil))
}

func TestExtractACPModeState_WithModes(t *testing.T) {
	sessResp := &acp.NewSessionResponse{
		Modes: &acp.SessionModeState{
			CurrentModeId: acp.SessionModeId("code"),
			AvailableModes: []acp.SessionMode{
				{Id: acp.SessionModeId("code"), Name: "Code"},
				{Id: acp.SessionModeId("ask"), Name: "Ask"},
			},
		},
	}
	ms := extractACPModeState(sessResp)
	require.NotNil(t, ms)
	assert.Equal(t, "code", ms.CurrentModeID)
	assert.Len(t, ms.AvailableModes, 2)
}

func TestExtractACPModeState_NilModes(t *testing.T) {
	sessResp := &acp.NewSessionResponse{}
	assert.Nil(t, extractACPModeState(sessResp))
}

// --- extractACPConfigOptions tests ---

func TestExtractACPConfigOptions_Nil(t *testing.T) {
	assert.Nil(t, extractACPConfigOptions(nil))
}

func TestExtractACPConfigOptions_WithModeCategory(t *testing.T) {
	cat := acp.SessionConfigOptionCategoryMode
	ungrouped := acp.SessionConfigSelectOptionsUngrouped(
		[]acp.SessionConfigSelectOption{
			{Name: "Code", Value: acp.SessionConfigValueId("code")},
		},
	)
	sessResp := &acp.NewSessionResponse{
		ConfigOptions: []acp.SessionConfigOption{
			{
				Select: &acp.SessionConfigOptionSelect{
					Id:           acp.SessionConfigId("mode"),
					Name:         "Mode",
					Category:     &cat,
					CurrentValue: acp.SessionConfigValueId("code"),
					Options:      acp.SessionConfigSelectOptions{Ungrouped: &ungrouped},
				},
			},
		},
	}
	cs := extractACPConfigOptions(sessResp)
	require.NotNil(t, cs)
	assert.Equal(t, "mode", cs.ConfigID)
	assert.Equal(t, "code", cs.CurrentID)
	require.Len(t, cs.Options, 1)
	assert.Equal(t, "mode", cs.Options[0].Category)
	require.Len(t, cs.Options[0].Values, 1)
	assert.Equal(t, "code", cs.Options[0].Values[0].ID)
}

func TestExtractACPConfigOptions_NoModeCategory(t *testing.T) {
	cat := acp.SessionConfigOptionCategoryThoughtLevel
	sessResp := &acp.NewSessionResponse{
		ConfigOptions: []acp.SessionConfigOption{
			{
				Select: &acp.SessionConfigOptionSelect{
					Id:           acp.SessionConfigId("thinking"),
					Name:         "Thinking",
					Category:     &cat,
					CurrentValue: acp.SessionConfigValueId("high"),
				},
			},
		},
	}
	assert.Nil(t, extractACPConfigOptions(sessResp))
}

func TestExtractACPConfigOptions_NoSelect(t *testing.T) {
	sessResp := &acp.NewSessionResponse{
		ConfigOptions: []acp.SessionConfigOption{
			{}, // No Select
		},
	}
	assert.Nil(t, extractACPConfigOptions(sessResp))
}

// --- extractACPConfigOptionsFromResume additional tests ---

func TestExtractACPConfigOptionsFromResume_WithModeCategory(t *testing.T) {
	cat := acp.SessionConfigOptionCategoryMode
	ungrouped := acp.SessionConfigSelectOptionsUngrouped(
		[]acp.SessionConfigSelectOption{
			{Name: "Code", Value: acp.SessionConfigValueId("code")},
		},
	)
	resumeResp := &acp.ResumeSessionResponse{
		ConfigOptions: []acp.SessionConfigOption{
			{
				Select: &acp.SessionConfigOptionSelect{
					Id:           acp.SessionConfigId("mode"),
					Name:         "Mode",
					Category:     &cat,
					CurrentValue: acp.SessionConfigValueId("code"),
					Options:      acp.SessionConfigSelectOptions{Ungrouped: &ungrouped},
				},
			},
		},
	}
	cs := extractACPConfigOptionsFromResume(resumeResp)
	require.NotNil(t, cs)
	assert.Equal(t, "code", cs.CurrentID)
}

// --- extractACPThinkingEffort tests ---

func TestExtractACPThinkingEffort_Nil(t *testing.T) {
	assert.Nil(t, extractACPThinkingEffort(nil))
}

func TestExtractACPThinkingEffort_WithThoughtLevel(t *testing.T) {
	cat := acp.SessionConfigOptionCategoryThoughtLevel
	ungrouped := acp.SessionConfigSelectOptionsUngrouped(
		[]acp.SessionConfigSelectOption{
			{Name: "Low", Value: acp.SessionConfigValueId("low")},
			{Name: "High", Value: acp.SessionConfigValueId("high")},
		},
	)
	sessResp := &acp.NewSessionResponse{
		ConfigOptions: []acp.SessionConfigOption{
			{
				Select: &acp.SessionConfigOptionSelect{
					Id:           acp.SessionConfigId("thinking"),
					Name:         "Thinking",
					Category:     &cat,
					CurrentValue: acp.SessionConfigValueId("high"),
					Options:      acp.SessionConfigSelectOptions{Ungrouped: &ungrouped},
				},
			},
		},
	}
	effort := extractACPThinkingEffort(sessResp)
	require.NotNil(t, effort)
	assert.Equal(t, "high", effort.CurrentID)
	require.Len(t, effort.AvailableLevels, 2)
	assert.Equal(t, "low", effort.AvailableLevels[0].ID)
	assert.Equal(t, "high", effort.AvailableLevels[1].ID)
}

func TestExtractACPThinkingEffort_NoThoughtLevelCategory(t *testing.T) {
	cat := acp.SessionConfigOptionCategoryMode
	sessResp := &acp.NewSessionResponse{
		ConfigOptions: []acp.SessionConfigOption{
			{
				Select: &acp.SessionConfigOptionSelect{
					Id:           acp.SessionConfigId("mode"),
					Name:         "Mode",
					Category:     &cat,
					CurrentValue: acp.SessionConfigValueId("code"),
				},
			},
		},
	}
	assert.Nil(t, extractACPThinkingEffort(sessResp))
}

// --- buildThinkingEffortStateFromSelect additional tests ---

func TestBuildThinkingEffortStateFromSelect_GroupedOptions(t *testing.T) {
	grouped := acp.SessionConfigSelectOptionsGrouped{
		{
			Name: "Standard",
			Options: []acp.SessionConfigSelectOption{
				{Name: "Low", Value: acp.SessionConfigValueId("low")},
				{Name: "Medium", Value: acp.SessionConfigValueId("medium")},
			},
		},
		{
			Name: "Extended",
			Options: []acp.SessionConfigSelectOption{
				{Name: "High", Value: acp.SessionConfigValueId("high")},
			},
		},
	}
	sel := &acp.SessionConfigOptionSelect{
		Id:           acp.SessionConfigId("thinking"),
		Name:         "Thinking",
		CurrentValue: acp.SessionConfigValueId("high"),
		Options:      acp.SessionConfigSelectOptions{Grouped: &grouped},
	}

	state := buildThinkingEffortStateFromSelect(sel)
	require.NotNil(t, state)
	assert.Equal(t, "high", state.CurrentID)
	require.Len(t, state.AvailableLevels, 3)
	assert.Equal(t, "low", state.AvailableLevels[0].ID)
	assert.Equal(t, "medium", state.AvailableLevels[1].ID)
	assert.Equal(t, "high", state.AvailableLevels[2].ID)
}

func TestBuildThinkingEffortStateFromSelect_Empty(t *testing.T) {
	sel := &acp.SessionConfigOptionSelect{
		Id:           acp.SessionConfigId("thinking"),
		Name:         "Thinking",
		CurrentValue: acp.SessionConfigValueId(""),
	}
	state := buildThinkingEffortStateFromSelect(sel)
	assert.Nil(t, state, "empty options and empty currentID should return nil")
}

func TestBuildThinkingEffortStateFromSelect_OnlyCurrentID(t *testing.T) {
	sel := &acp.SessionConfigOptionSelect{
		Id:           acp.SessionConfigId("thinking"),
		Name:         "Thinking",
		CurrentValue: acp.SessionConfigValueId("high"),
	}
	state := buildThinkingEffortStateFromSelect(sel)
	require.NotNil(t, state)
	assert.Equal(t, "high", state.CurrentID)
	assert.Empty(t, state.AvailableLevels)
}

// --- mapACPSelectOptions additional tests ---

func TestMapACPSelectOptions_GroupedOnly(t *testing.T) {
	grouped := acp.SessionConfigSelectOptionsGrouped{
		{
			Name: "Group A",
			Options: []acp.SessionConfigSelectOption{
				{Name: "Option 1", Value: acp.SessionConfigValueId("opt1")},
			},
		},
	}
	opts := acp.SessionConfigSelectOptions{Grouped: &grouped}
	optDef := &ConfigOptionDef{}
	mapACPSelectOptions(opts, optDef)
	require.Len(t, optDef.Values, 1)
	assert.Equal(t, "opt1", optDef.Values[0].ID)
	assert.Equal(t, "Option 1", optDef.Values[0].Name)
}

func TestMapACPSelectOptions_UngroupedAndGrouped(t *testing.T) {
	ungrouped := acp.SessionConfigSelectOptionsUngrouped(
		[]acp.SessionConfigSelectOption{
			{Name: "U1", Value: acp.SessionConfigValueId("u1")},
		},
	)
	grouped := acp.SessionConfigSelectOptionsGrouped{
		{
			Name: "G1",
			Options: []acp.SessionConfigSelectOption{
				{Name: "G1-1", Value: acp.SessionConfigValueId("g1_1")},
			},
		},
	}
	opts := acp.SessionConfigSelectOptions{Ungrouped: &ungrouped, Grouped: &grouped}
	optDef := &ConfigOptionDef{}
	mapACPSelectOptions(opts, optDef)
	require.Len(t, optDef.Values, 2)
	assert.Equal(t, "u1", optDef.Values[0].ID)
	assert.Equal(t, "g1_1", optDef.Values[1].ID)
}

func TestMapACPSelectOptions_Empty(t *testing.T) {
	opts := acp.SessionConfigSelectOptions{}
	optDef := &ConfigOptionDef{}
	mapACPSelectOptions(opts, optDef)
	assert.Empty(t, optDef.Values)
}

// --- buildModelListStateFromSelect additional tests ---

func TestBuildModelListStateFromSelect_GroupedOptions(t *testing.T) {
	grouped := acp.SessionConfigSelectOptionsGrouped{
		{
			Name: "Claude",
			Options: []acp.SessionConfigSelectOption{
				{Name: "Claude 3.5", Value: acp.SessionConfigValueId("claude-3.5")},
			},
		},
		{
			Name: "GPT",
			Options: []acp.SessionConfigSelectOption{
				{Name: "GPT-4o", Value: acp.SessionConfigValueId("gpt-4o")},
			},
		},
	}
	sel := &acp.SessionConfigOptionSelect{
		Id:           acp.SessionConfigId("model"),
		Name:         "Model",
		CurrentValue: acp.SessionConfigValueId("claude-3.5"),
		Options:      acp.SessionConfigSelectOptions{Grouped: &grouped},
	}
	state := buildModelListStateFromSelect(sel)
	require.NotNil(t, state)
	assert.Equal(t, "claude-3.5", state.CurrentModelID)
	require.Len(t, state.Models, 2)
	assert.Equal(t, "claude-3.5", state.Models[0].ID)
	assert.Equal(t, "Claude 3.5", state.Models[0].Name)
	assert.Equal(t, "gpt-4o", state.Models[1].ID)
}

func TestBuildModelListStateFromSelect_Empty(t *testing.T) {
	sel := &acp.SessionConfigOptionSelect{
		Id:           acp.SessionConfigId("model"),
		Name:         "Model",
		CurrentValue: acp.SessionConfigValueId(""),
	}
	state := buildModelListStateFromSelect(sel)
	assert.Nil(t, state, "empty options and empty currentModelID should return nil")
}

func TestBuildModelListStateFromSelect_OnlyCurrentModelID(t *testing.T) {
	sel := &acp.SessionConfigOptionSelect{
		Id:           acp.SessionConfigId("model"),
		Name:         "Model",
		CurrentValue: acp.SessionConfigValueId("gpt-4o"),
	}
	state := buildModelListStateFromSelect(sel)
	require.NotNil(t, state)
	assert.Equal(t, "gpt-4o", state.CurrentModelID)
	assert.Empty(t, state.Models)
}

// --- extractACPModelList tests ---

func TestExtractACPModelList_Nil(t *testing.T) {
	assert.Nil(t, extractACPModelList(nil))
}

func TestExtractACPModelList_WithModelCategory(t *testing.T) {
	cat := acp.SessionConfigOptionCategoryModel
	ungrouped := acp.SessionConfigSelectOptionsUngrouped(
		[]acp.SessionConfigSelectOption{
			{Name: "GPT-4o", Value: acp.SessionConfigValueId("gpt-4o")},
		},
	)
	sessResp := &acp.NewSessionResponse{
		ConfigOptions: []acp.SessionConfigOption{
			{
				Select: &acp.SessionConfigOptionSelect{
					Id:           acp.SessionConfigId("model"),
					Name:         "Model",
					Category:     &cat,
					CurrentValue: acp.SessionConfigValueId("gpt-4o"),
					Options:      acp.SessionConfigSelectOptions{Ungrouped: &ungrouped},
				},
			},
		},
	}
	ml := extractACPModelList(sessResp)
	require.NotNil(t, ml)
	assert.Equal(t, "gpt-4o", ml.CurrentModelID)
	require.Len(t, ml.Models, 1)
	assert.Equal(t, "gpt-4o", ml.Models[0].ID)
	assert.Equal(t, "GPT-4o", ml.Models[0].Name)
}

// --- extractACPModelListFromResume additional tests ---

func TestExtractACPModelListFromResume_WithModelCategory(t *testing.T) {
	cat := acp.SessionConfigOptionCategoryModel
	ungrouped := acp.SessionConfigSelectOptionsUngrouped(
		[]acp.SessionConfigSelectOption{
			{Name: "Claude 3.5", Value: acp.SessionConfigValueId("claude-3.5")},
		},
	)
	resumeResp := &acp.ResumeSessionResponse{
		ConfigOptions: []acp.SessionConfigOption{
			{
				Select: &acp.SessionConfigOptionSelect{
					Id:           acp.SessionConfigId("model"),
					Name:         "Model",
					Category:     &cat,
					CurrentValue: acp.SessionConfigValueId("claude-3.5"),
					Options:      acp.SessionConfigSelectOptions{Ungrouped: &ungrouped},
				},
			},
		},
	}
	ml := extractACPModelListFromResume(resumeResp)
	require.NotNil(t, ml)
	assert.Equal(t, "claude-3.5", ml.CurrentModelID)
}

// --- mapACPError additional tests ---

func TestMapACPError_InvalidParams(t *testing.T) {
	event := mapACPError(-32602, "invalid params")
	assert.Equal(t, "error", event.Type)
	assert.Equal(t, ReasonParseError, event.Reason)
}

func TestMapACPError_InternalError(t *testing.T) {
	event := mapACPError(-32603, "internal error")
	assert.Equal(t, ReasonBackendExit, event.Reason)
}

// --- mapACPToolCallUpdate integration tests ---

func TestMapACPToolCallUpdate_NilStatus(t *testing.T) {
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: acp.ToolCallId("tc-nil-status"),
		Status:     nil,
	}
	event := mapACPToolCallUpdate(tcu, "")
	assert.Equal(t, "tool_use", event.Type)
	assert.False(t, event.Tool.Done)
	assert.Equal(t, "", event.Tool.Status)
}

func TestMapACPToolCallUpdate_ExecuteKindWithLocationsFallback(t *testing.T) {
	// Execute kind without RawInput → title used as command
	kind := acp.ToolKindExecute
	title := "go build"
	inProgress := acp.ToolCallStatusInProgress
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: acp.ToolCallId("exec-build-1"),
		Kind:       &kind,
		Title:      &title,
		Status:     &inProgress,
	}
	event := mapACPToolCallUpdate(tcu, "")
	assert.Equal(t, "tool_use", event.Type)
	assert.Contains(t, event.Tool.Input, "command")
	assert.Contains(t, event.Tool.Input, "go build")
}

func TestMapACPToolCallUpdate_ReadKindWithLocations(t *testing.T) {
	kind := acp.ToolKindRead
	title := "main.go"
	inProgress := acp.ToolCallStatusInProgress
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: acp.ToolCallId("read-main-1"),
		Kind:       &kind,
		Title:      &title,
		Status:     &inProgress,
		Locations: []acp.ToolCallLocation{
			{Path: "/home/user/main.go"},
		},
	}
	event := mapACPToolCallUpdate(tcu, "")
	assert.Equal(t, "tool_use", event.Type)
	assert.Contains(t, event.Tool.Input, "file_path")
	assert.Contains(t, event.Tool.Input, "/home/user/main.go")
}

func TestMapACPToolCallUpdate_WithFilePathRawInputNormalization(t *testing.T) {
	inProgress := acp.ToolCallStatusInProgress
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: acp.ToolCallId("tc-camel"),
		Status:     &inProgress,
		RawInput: map[string]any{
			"filePath": "/tmp/main.go",
			"dirPath":  "/tmp/subdir",
		},
	}
	event := mapACPToolCallUpdate(tcu, "")
	assert.Contains(t, event.Tool.Input, "file_path")
	assert.Contains(t, event.Tool.Input, "path")
	assert.NotContains(t, event.Tool.Input, "filePath")
	assert.NotContains(t, event.Tool.Input, "dirPath")
}

func TestMapACPToolCallUpdate_CompletedWithRawOutput(t *testing.T) {
	completed := acp.ToolCallStatusCompleted
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: acp.ToolCallId("tc-out"),
		Status:     &completed,
		RawOutput:  "plain string output",
	}
	event := mapACPToolCallUpdate(tcu, "")
	assert.Equal(t, "tool_result", event.Type)
	assert.True(t, event.Tool.Done)
	assert.Equal(t, "success", event.Tool.Status)
	assert.Equal(t, "plain string output", event.Tool.Output)
}

// --- extractACPToolOutputFromContent additional tests ---

func TestExtractACPToolOutputFromContent_MixedContentAndTerminal(t *testing.T) {
	contents := []acp.ToolCallContent{
		{
			Content: &acp.ToolCallContentContent{
				Content: acp.ContentBlock{
					Text: &acp.ContentBlockText{Text: "text output"},
				},
			},
		},
		{
			Terminal: &acp.ToolCallContentTerminal{TerminalId: "term-1"},
		},
		{
			Content: &acp.ToolCallContentContent{
				Content: acp.ContentBlock{
					Text: &acp.ContentBlockText{Text: "more text"},
				},
			},
		},
	}
	result := extractACPToolOutputFromContent(contents)
	assert.Equal(t, "text output\nmore text", result, "should join text blocks, skip terminal")
}

func TestExtractACPToolOutputFromContent_NilTextBlock(t *testing.T) {
	contents := []acp.ToolCallContent{
		{
			Content: &acp.ToolCallContentContent{
				Content: acp.ContentBlock{}, // Text is nil
			},
		},
	}
	result := extractACPToolOutputFromContent(contents)
	assert.Equal(t, "", result)
}

// --- mapACPToolCall RawInput key normalization edge cases ---

func TestMapACPToolCall_RawInputDirPathNormalization(t *testing.T) {
	tc := acp.SessionUpdateToolCall{
		ToolCallId: acp.ToolCallId("tc-dirpath"),
		Title:      "LS",
		Kind:       acp.ToolKindSearch,
		RawInput:   map[string]any{"dirPath": "/home/user/project"},
	}
	event := mapACPToolCall(tc, "")
	assert.Contains(t, event.Tool.Input, "path")
	assert.NotContains(t, event.Tool.Input, "dirPath")
	assert.Contains(t, event.Tool.Input, "/home/user/project")
}

func TestMapACPToolCall_RawInputCellNormalization(t *testing.T) {
	tc := acp.SessionUpdateToolCall{
		ToolCallId: acp.ToolCallId("tc-cell"),
		Title:      "NotebookEdit",
		Kind:       acp.ToolKindEdit,
		RawInput: map[string]any{
			"cellIndex": 0,
			"cellType":  "code",
		},
	}
	event := mapACPToolCall(tc, "")
	assert.Contains(t, event.Tool.Input, "cell_index")
	assert.Contains(t, event.Tool.Input, "cell_type")
	assert.NotContains(t, event.Tool.Input, "cellIndex")
	assert.NotContains(t, event.Tool.Input, "cellType")
}

// --- mapACPToolCallUpdate RawInput key normalization ---

func TestMapACPToolCallUpdate_RawInputDirPathNormalization(t *testing.T) {
	inProgress := acp.ToolCallStatusInProgress
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: acp.ToolCallId("tc-upd-dirpath"),
		Status:     &inProgress,
		RawInput:   map[string]any{"dirPath": "/home/user/project"},
	}
	event := mapACPToolCallUpdate(tcu, "")
	assert.Contains(t, event.Tool.Input, "path")
	assert.NotContains(t, event.Tool.Input, "dirPath")
}

// --- extractInputFromContentUpdate additional tests ---

func TestExtractInputFromContentUpdate_NilContentBlock(t *testing.T) {
	tcu := acp.SessionToolCallUpdate{
		Content: []acp.ToolCallContent{
			{
				Content: &acp.ToolCallContentContent{
					Content: acp.ContentBlock{}, // Text is nil
				},
			},
		},
	}
	input := extractInputFromContentUpdate(tcu)
	assert.Nil(t, input)
}

func TestExtractInputFromContentUpdate_EmptyText(t *testing.T) {
	textStr := ""
	tcu := acp.SessionToolCallUpdate{
		Content: []acp.ToolCallContent{
			{
				Content: &acp.ToolCallContentContent{
					Content: acp.TextBlock(textStr),
				},
			},
		},
	}
	input := extractInputFromContentUpdate(tcu)
	assert.Nil(t, input, "empty text should return nil")
}

// --- buildConfigOptionStateFromSelect tests ---

func TestBuildConfigOptionStateFromSelect_Basic(t *testing.T) {
	ungrouped := acp.SessionConfigSelectOptionsUngrouped(
		[]acp.SessionConfigSelectOption{
			{Name: "Code", Value: acp.SessionConfigValueId("code")},
			{Name: "Ask", Value: acp.SessionConfigValueId("ask")},
		},
	)
	sel := &acp.SessionConfigOptionSelect{
		Id:           acp.SessionConfigId("mode"),
		Name:         "Mode",
		CurrentValue: acp.SessionConfigValueId("code"),
		Options:      acp.SessionConfigSelectOptions{Ungrouped: &ungrouped},
	}
	cs := buildConfigOptionStateFromSelect(sel, "mode")
	assert.Equal(t, "mode", cs.ConfigID)
	assert.Equal(t, "code", cs.CurrentID)
	require.Len(t, cs.Options, 1)
	assert.Equal(t, "mode", cs.Options[0].Category)
	require.Len(t, cs.Options[0].Values, 2)
}

// --- extractThinkingEffortFromOpts additional tests ---

func TestExtractThinkingEffortFromOpts_NoSelect(t *testing.T) {
	opts := []acp.SessionConfigOption{
		{}, // no Select
	}
	assert.Nil(t, extractThinkingEffortFromOpts(opts))
}

func TestExtractThinkingEffortFromOpts_WithThoughtLevel(t *testing.T) {
	cat := acp.SessionConfigOptionCategoryThoughtLevel
	ungrouped := acp.SessionConfigSelectOptionsUngrouped(
		[]acp.SessionConfigSelectOption{
			{Name: "Low", Value: acp.SessionConfigValueId("low")},
		},
	)
	opts := []acp.SessionConfigOption{
		{
			Select: &acp.SessionConfigOptionSelect{
				Id:           acp.SessionConfigId("thinking"),
				Name:         "Thinking",
				Category:     &cat,
				CurrentValue: acp.SessionConfigValueId("low"),
				Options:      acp.SessionConfigSelectOptions{Ungrouped: &ungrouped},
			},
		},
	}
	effort := extractThinkingEffortFromOpts(opts)
	require.NotNil(t, effort)
	assert.Equal(t, "low", effort.CurrentID)
	require.Len(t, effort.AvailableLevels, 1)
}

// --- extractModelListFromOpts additional tests ---

func TestExtractModelListFromOpts_NoSelect(t *testing.T) {
	opts := []acp.SessionConfigOption{
		{}, // no Select
	}
	assert.Nil(t, extractModelListFromOpts(opts))
}

func TestExtractModelListFromOpts_WithModelCategory(t *testing.T) {
	cat := acp.SessionConfigOptionCategoryModel
	ungrouped := acp.SessionConfigSelectOptionsUngrouped(
		[]acp.SessionConfigSelectOption{
			{Name: "GPT-4o", Value: acp.SessionConfigValueId("gpt-4o")},
		},
	)
	opts := []acp.SessionConfigOption{
		{
			Select: &acp.SessionConfigOptionSelect{
				Id:           acp.SessionConfigId("model"),
				Name:         "Model",
				Category:     &cat,
				CurrentValue: acp.SessionConfigValueId("gpt-4o"),
				Options:      acp.SessionConfigSelectOptions{Ungrouped: &ungrouped},
			},
		},
	}
	ml := extractModelListFromOpts(opts)
	require.NotNil(t, ml)
	assert.Equal(t, "gpt-4o", ml.CurrentModelID)
	require.Len(t, ml.Models, 1)
	assert.Equal(t, model.AgentModel{ID: "gpt-4o", Name: "GPT-4o"}, ml.Models[0])
}

// --- JSON round-trip for mapToolCallInputFromExecute with terminal content and title ---

func TestMapToolCallInputFromExecute_TerminalContentProducesValidJSON(t *testing.T) {
	title := "cargo build"
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: acp.ToolCallId("tc-term-json"),
		Title:      &title,
		Content: []acp.ToolCallContent{
			{Terminal: &acp.ToolCallContentTerminal{TerminalId: "term-1"}},
		},
	}
	tool := &ToolCall{}
	mapToolCallInputFromExecute(tcu, tool)
	assert.NotEmpty(t, tool.Input)
	var parsed map[string]any
	err := json.Unmarshal([]byte(tool.Input), &parsed)
	require.NoError(t, err, "tool input should be valid JSON")
	assert.Equal(t, "cargo build", parsed["command"])
}

func TestMapToolCallInputFromExecute_TerminalNoTitleEmptyInput(t *testing.T) {
	// Terminal content with empty title → title is "", first condition fails (empty),
	// enters for loop, terminal found, title is "" so command not added,
	// input map is empty → len(input)==0 → tool.Input not set
	emptyTitle := ""
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: acp.ToolCallId("tc-term-empty"),
		Title:      &emptyTitle,
		Content: []acp.ToolCallContent{
			{Terminal: &acp.ToolCallContentTerminal{TerminalId: "term-1"}},
		},
	}
	tool := &ToolCall{}
	mapToolCallInputFromExecute(tcu, tool)
	assert.Equal(t, "", tool.Input, "empty title with terminal should not set input")
}

// --- extractACPThinkingEffortFromResume additional coverage ---

func TestExtractACPThinkingEffortFromResume_WithThoughtLevel(t *testing.T) {
	cat := acp.SessionConfigOptionCategoryThoughtLevel
	ungrouped := acp.SessionConfigSelectOptionsUngrouped(
		[]acp.SessionConfigSelectOption{
			{Name: "High", Value: acp.SessionConfigValueId("high")},
		},
	)
	resumeResp := &acp.ResumeSessionResponse{
		ConfigOptions: []acp.SessionConfigOption{
			{
				Select: &acp.SessionConfigOptionSelect{
					Id:           acp.SessionConfigId("thinking"),
					Name:         "Thinking",
					Category:     &cat,
					CurrentValue: acp.SessionConfigValueId("high"),
					Options:      acp.SessionConfigSelectOptions{Ungrouped: &ungrouped},
				},
			},
		},
	}
	effort := extractACPThinkingEffortFromResume(resumeResp)
	require.NotNil(t, effort)
	assert.Equal(t, "high", effort.CurrentID)
}

// --- extractACPModelListFromResume additional coverage (already at 100%, confirming) ---

// --- mapToolCallInput additional coverage: non-execute kind without RawInput falls to locations ---

func TestMapToolCallInput_NonExecuteFallsToLocations(t *testing.T) {
	kind := acp.ToolKindRead
	title := "main.go"
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: acp.ToolCallId("tc-read-loc"),
		Kind:       &kind,
		Title:      &title,
		Locations: []acp.ToolCallLocation{
			{Path: "/home/user/main.go"},
		},
	}
	tool := &ToolCall{}
	mapToolCallInput(tcu, tool, "")
	assert.Contains(t, tool.Input, "file_path")
	assert.Contains(t, tool.Input, "/home/user/main.go")
}

func TestMapToolCallInput_ExecuteKindNoRawInputNoTitle(t *testing.T) {
	kind := acp.ToolKindExecute
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: acp.ToolCallId("tc-exec-noraw"),
		Kind:       &kind,
		Title:      nil,
	}
	tool := &ToolCall{}
	mapToolCallInput(tcu, tool, "")
	assert.Equal(t, "", tool.Input, "execute kind with no rawInput and no title should not set input")
}

func TestMapToolCallInput_NilKindNoRawInputFallsToLocations(t *testing.T) {
	// When kind is nil and no RawInput, falls to mapToolCallInputFromLocations
	// which defaults kind to ToolKindOther, returns nil → empty input
	tcu := acp.SessionToolCallUpdate{
		ToolCallId: acp.ToolCallId("tc-nil-kind"),
		Kind:       nil,
	}
	tool := &ToolCall{}
	mapToolCallInput(tcu, tool, "")
	assert.Equal(t, "", tool.Input)
}

func TestExtractToolName_AgentSubtypeMappedToAgent(t *testing.T) {
	// Known Agent sub-type titles should be mapped to "Agent" so the frontend
	// uses the correct icon/category, not a fallback wrench.
	for _, title := range []string{"Explore", "explore", "Plan", "plan", "General-purpose", "code-reviewer"} {
		result := extractToolName(title, acp.ToolKindOther, "")
		assert.Equal(t, "Agent", result, "title %q should map to Agent", title)
	}
}

func TestExtractToolName_UnknownSingleWordNotAgent(t *testing.T) {
	// A single-word title that is NOT a known agent subtype should pass through.
	result := extractToolName("CustomTool", acp.ToolKindOther, "")
	assert.Equal(t, "CustomTool", result)
}

func TestMapToolCallName_ExistingCanonicalNotOverwritten(t *testing.T) {
	// When a tool already has a canonical name (e.g., "Agent" from the initial
	// ToolCall event), a ToolCallUpdate with a different title should NOT
	// overwrite it. ACP agents send progressive title updates like
	// "Agent" → "Explore project structure", and extractToolName would return
	// "Explore" which has no frontend icon mapping.
	tool := &ToolCall{Name: "Agent"}
	title := "Explore project structure"
	tcu := acp.SessionToolCallUpdate{
		Title: &title,
	}
	mapToolCallName(tcu, tool, "")
	assert.Equal(t, "Agent", tool.Name, "existing canonical name should not be overwritten by ToolCallUpdate")
}

func TestMapToolCallName_LowercaseNameGetsOverwritten(t *testing.T) {
	// If the existing name is all-lowercase (not yet canonicalized), it should
	// still be updated by the ToolCallUpdate.
	tool := &ToolCall{Name: "agent"}
	title := "Bash"
	tcu := acp.SessionToolCallUpdate{
		Title: &title,
	}
	mapToolCallName(tcu, tool, "")
	assert.Equal(t, "Bash", tool.Name, "lowercase name should be overwritten by ToolCallUpdate")
}

// --- UsageUpdate forwarding tests ---

func TestMapACPSessionUpdate_UsageUpdate(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	used := 53000
	size := 200000
	amount := 0.42
	currency := "USD"
	cost := acp.Cost{Amount: amount, Currency: currency}

	update := acp.SessionUpdate{
		UsageUpdate: &acp.SessionUsageUpdate{
			Used: used,
			Size: size,
			Cost: &cost,
		},
	}

	mapACPSessionUpdate(update, ch, context.Background(), nil, nil)

	// Should get 2 events: raw_output + usage_update
	var foundUsage bool
	for range 2 {
		select {
		case evt := <-ch:
			if evt.Type == "usage_update" && evt.Usage != nil {
				foundUsage = true
				assert.Equal(t, used, evt.Usage.Used)
				assert.Equal(t, size, evt.Usage.Size)
				assert.InDelta(t, amount, evt.Usage.Cost, 0.001)
				assert.Equal(t, currency, evt.Usage.Currency)
			}
		default:
			t.Fatal("expected event in channel")
		}
	}
	assert.True(t, foundUsage, "usage_update event not found")
}

func TestMapACPSessionUpdate_UsageUpdate_WithoutCost(t *testing.T) {
	ch := make(chan StreamEvent, 10)

	update := acp.SessionUpdate{
		UsageUpdate: &acp.SessionUsageUpdate{
			Used: 1000,
			Size: 5000,
			Cost: nil,
		},
	}

	mapACPSessionUpdate(update, ch, context.Background(), nil, nil)

	var foundUsage bool
	for range 2 {
		select {
		case evt := <-ch:
			if evt.Type == "usage_update" && evt.Usage != nil {
				foundUsage = true
				assert.Equal(t, 1000, evt.Usage.Used)
				assert.Equal(t, 5000, evt.Usage.Size)
				assert.Equal(t, 0.0, evt.Usage.Cost)
				assert.Equal(t, "", evt.Usage.Currency)
			}
		default:
			t.Fatal("expected event in channel")
		}
	}
	assert.True(t, foundUsage, "usage_update event not found")
}
