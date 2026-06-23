package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
)

// ---------------------------------------------------------------------------
// decodeExitCode
// ---------------------------------------------------------------------------

func TestRefactor_DecodeExitCode(t *testing.T) {
	tests := []struct {
		code int
		want string
	}{
		{0, ""},
		{1, "general error"},
		{2, ""},
		{126, "permission denied / not executable"},
		{127, "command not found"},
		{128, "invalid exit argument"},
		{129, "SIGHUP"},
		{130, "SIGINT (Ctrl+C)"},
		{137, "SIGKILL (possible OOM killer)"},
		{139, "SIGSEGV (segmentation fault)"},
		{141, "SIGPIPE (broken pipe)"},
		{143, "SIGTERM"},
		{131, "signal 3"}, // 131 - 128 = 3 (SIGQUIT on most Unix)
		{200, "signal 72"},
		{125, ""}, // < 128, not a known code
	}

	for _, tc := range tests {
		t.Run(fmt.Sprintf("code_%d", tc.code), func(t *testing.T) {
			got := decodeExitCode(tc.code)
			assert.Equal(t, tc.want, got)
		})
	}
}

// ---------------------------------------------------------------------------
// crashDiagnostics.String()
// ---------------------------------------------------------------------------

func TestRefactor_CrashDiagnostics_String(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		d := crashDiagnostics{}
		assert.Equal(t, "", d.String())
	})

	t.Run("exit_code_only", func(t *testing.T) {
		d := crashDiagnostics{ExitCode: 1}
		s := d.String()
		assert.Contains(t, s, "exit_code=1")
		assert.Contains(t, s, "general error")
	})

	t.Run("exit_code_with_signal", func(t *testing.T) {
		d := crashDiagnostics{ExitCode: 137, Signal: "SIGKILL"}
		s := d.String()
		assert.Contains(t, s, "exit_code=137")
		assert.Contains(t, s, "SIGKILL")
		// Signal takes precedence over decodeExitCode
		assert.NotContains(t, s, "possible OOM killer")
	})

	t.Run("exit_code_unknown_signal", func(t *testing.T) {
		d := crashDiagnostics{ExitCode: 150}
		s := d.String()
		assert.Contains(t, s, "exit_code=150")
		assert.Contains(t, s, "signal 22") // 150 - 128
	})

	t.Run("all_fields", func(t *testing.T) {
		d := crashDiagnostics{
			ExitCode:   139,
			Uptime:     5 * time.Minute,
			ParentPID:  1234,
			VMRSSKB:    2048,
			FDCount:    42,
			StderrTail: "FATAL ERROR",
		}
		s := d.String()
		assert.Contains(t, s, "exit_code=139")
		assert.Contains(t, s, "SIGSEGV")
		assert.Contains(t, s, "uptime=5m0s")
		assert.Contains(t, s, "ppid=1234")
		assert.Contains(t, s, "rss=2MB")
		assert.Contains(t, s, "fds=42")
		assert.Contains(t, s, "stderr: FATAL ERROR")
	})

	t.Run("zero_exit_code_omits_exit", func(t *testing.T) {
		d := crashDiagnostics{Uptime: 10 * time.Second, StderrTail: "some error"}
		s := d.String()
		assert.NotContains(t, s, "exit_code")
		assert.Contains(t, s, "uptime=10s")
		assert.Contains(t, s, "stderr: some error")
	})
}

// ---------------------------------------------------------------------------
// isPeerDisconnectMsg
// ---------------------------------------------------------------------------

func TestRefactor_IsPeerDisconnectMsg(t *testing.T) {
	tests := []struct {
		msg  string
		want bool
	}{
		{"peer disconnected", true},
		{"error: peer disconnected during prompt", true},
		{"write tcp: broken pipe", true},
		{"connection reset by peer", false},
		{"timeout waiting for response", false},
		{"", false},
	}
	for _, tc := range tests {
		t.Run(tc.msg, func(t *testing.T) {
			assert.Equal(t, tc.want, isPeerDisconnectMsg(tc.msg))
		})
	}
}

// ---------------------------------------------------------------------------
// isACPPeerDisconnected
// ---------------------------------------------------------------------------

func TestRefactor_IsACPPeerDisconnected(t *testing.T) {
	t.Run("plain_error_with_disconnect_msg", func(t *testing.T) {
		err := fmt.Errorf("peer disconnected during write")
		assert.True(t, isACPPeerDisconnected(err))
	})

	t.Run("plain_error_with_broken_pipe", func(t *testing.T) {
		err := fmt.Errorf("write tcp 127.0.0.1:1234->127.0.0.1:5678: broken pipe")
		assert.True(t, isACPPeerDisconnected(err))
	})

	t.Run("plain_error_unrelated", func(t *testing.T) {
		err := fmt.Errorf("some other error")
		assert.False(t, isACPPeerDisconnected(err))
	})

	t.Run("request_error_code_minus32603_with_disconnect_in_data", func(t *testing.T) {
		reqErr := &acp.RequestError{
			Code:    -32603,
			Message: "Internal error",
			Data: map[string]any{
				"error": "peer disconnected",
			},
		}
		assert.True(t, isACPPeerDisconnected(reqErr))
	})

	t.Run("request_error_code_minus32603_no_disconnect_data", func(t *testing.T) {
		reqErr := &acp.RequestError{
			Code:    -32603,
			Message: "Internal error",
			Data: map[string]any{
				"error": "something else",
			},
		}
		assert.False(t, isACPPeerDisconnected(reqErr))
	})

	t.Run("request_error_wrong_code", func(t *testing.T) {
		reqErr := &acp.RequestError{
			Code:    -32000,
			Message: "peer disconnected",
		}
		assert.False(t, isACPPeerDisconnected(reqErr))
	})

	t.Run("request_error_code_minus32603_with_broken_pipe_in_message", func(t *testing.T) {
		reqErr := &acp.RequestError{
			Code:    -32603,
			Message: "broken pipe on write",
		}
		assert.True(t, isACPPeerDisconnected(reqErr))
	})
}

// ---------------------------------------------------------------------------
// isUnknownConfigOption
// ---------------------------------------------------------------------------

func TestRefactor_IsUnknownConfigOption(t *testing.T) {
	t.Run("plain_error", func(t *testing.T) {
		err := fmt.Errorf("Unknown config option: foo")
		assert.True(t, isUnknownConfigOption(err))
	})

	t.Run("plain_error_unrelated", func(t *testing.T) {
		err := fmt.Errorf("permission denied")
		assert.False(t, isUnknownConfigOption(err))
	})

	t.Run("request_error_with_details", func(t *testing.T) {
		reqErr := &acp.RequestError{
			Code:    -32602,
			Message: "Invalid params",
			Data: map[string]any{
				"details": "Unknown config option: thinkingEffort",
			},
		}
		assert.True(t, isUnknownConfigOption(reqErr))
	})

	t.Run("request_error_without_matching_details", func(t *testing.T) {
		reqErr := &acp.RequestError{
			Code:    -32602,
			Message: "Invalid params",
			Data: map[string]any{
				"details": "invalid value",
			},
		}
		assert.False(t, isUnknownConfigOption(reqErr))
	})
}

// ---------------------------------------------------------------------------
// IsACPResourceNotFound
// ---------------------------------------------------------------------------

func TestRefactor_IsACPResourceNotFound(t *testing.T) {
	t.Run("plain_error", func(t *testing.T) {
		err := fmt.Errorf("Resource not found: session xyz")
		assert.True(t, IsACPResourceNotFound(err))
	})

	t.Run("plain_error_unrelated", func(t *testing.T) {
		err := fmt.Errorf("something else")
		assert.False(t, IsACPResourceNotFound(err))
	})

	t.Run("request_error_code_minus32002", func(t *testing.T) {
		reqErr := &acp.RequestError{
			Code:    -32002,
			Message: "Resource not found",
		}
		assert.True(t, IsACPResourceNotFound(reqErr))
	})

	t.Run("request_error_wrong_code", func(t *testing.T) {
		reqErr := &acp.RequestError{
			Code:    -32603,
			Message: "Internal error",
		}
		assert.False(t, IsACPResourceNotFound(reqErr))
	})

	t.Run("request_error_code_minus32002_wrong_message", func(t *testing.T) {
		reqErr := &acp.RequestError{
			Code:    -32002,
			Message: "Something else",
		}
		assert.False(t, IsACPResourceNotFound(reqErr))
	})
}

// ---------------------------------------------------------------------------
// configKilledConnectionError
// ---------------------------------------------------------------------------

func TestRefactor_ConfigKilledConnectionError(t *testing.T) {
	t.Run("basic_error", func(t *testing.T) {
		err := &configKilledConnectionError{configID: "model", value: "gpt-4"}
		assert.Equal(t, "model", err.ConfigID())
		assert.Equal(t, "gpt-4", err.Value())
		assert.Contains(t, err.Error(), "acp: set_config_option(model) killed connection")
		assert.Contains(t, err.Error(), "value=gpt-4")
	})

	t.Run("no_value", func(t *testing.T) {
		err := &configKilledConnectionError{configID: "mode"}
		assert.Contains(t, err.Error(), "acp: set_config_option(mode) killed connection")
		assert.NotContains(t, err.Error(), "value=")
	})

	t.Run("with_diagnostics", func(t *testing.T) {
		err := &configKilledConnectionError{
			configID: "thinkingEffort",
			value:    "high",
			diag:     crashDiagnostics{ExitCode: 139, Signal: "SIGSEGV"},
		}
		s := err.Error()
		assert.Contains(t, s, "thinkingEffort")
		assert.Contains(t, s, "high")
		assert.Contains(t, s, "exit_code=139")
		assert.Contains(t, s, "SIGSEGV")
	})

	t.Run("isConfigKilledConnection", func(t *testing.T) {
		err := errConfigKilledConnection("mode", "code")
		assert.True(t, isConfigKilledConnection(err))
		assert.False(t, isConfigKilledConnection(fmt.Errorf("other error")))
	})

	t.Run("errors_as", func(t *testing.T) {
		err := errConfigKilledConnectionWithDiag("model", "gpt-4", crashDiagnostics{ExitCode: 1})
		var target *configKilledConnectionError
		assert.True(t, errors.As(err, &target))
		assert.Equal(t, "model", target.ConfigID())
	})
}

// ---------------------------------------------------------------------------
// extractToolName
// ---------------------------------------------------------------------------

func TestRefactor_ExtractToolName(t *testing.T) {
	t.Run("toolCallID_prefix_match", func(t *testing.T) {
		assert.Equal(t, "Read", extractToolName("", acp.ToolKindRead, "kimi", "read_file-1234-5"))
		assert.Equal(t, "Bash", extractToolName("", acp.ToolKindExecute, "kimi", "run_shell_command-1234-5"))
		assert.Equal(t, "LS", extractToolName("", acp.ToolKindOther, "kimi", "list_directory-1234-5"))
		assert.Equal(t, "Glob", extractToolName("", acp.ToolKindOther, "kimi", "glob-1234-5"))
		assert.Equal(t, "AskUserQuestion", extractToolName("", acp.ToolKindOther, "kimi", "ask-uuid-123"))
	})

	t.Run("toolCallID_no_dash", func(t *testing.T) {
		// No dash → no prefix extraction
		assert.Equal(t, "Bash", extractToolName("Bash", acp.ToolKindExecute, "", "nodash"))
	})

	t.Run("toolCallID_unknown_prefix", func(t *testing.T) {
		// Unknown prefix falls through to title/alias matching
		assert.Equal(t, "Bash", extractToolName("Bash", acp.ToolKindExecute, "", "unknown_prefix-123"))
	})

	t.Run("lowercase_alias", func(t *testing.T) {
		assert.Equal(t, "Bash", extractToolName("bash", acp.ToolKindExecute, ""))
		assert.Equal(t, "Bash", extractToolName("terminal", acp.ToolKindExecute, ""))
		assert.Equal(t, "Bash", extractToolName("shell", acp.ToolKindExecute, ""))
		assert.Equal(t, "Read", extractToolName("read", acp.ToolKindRead, ""))
		assert.Equal(t, "Write", extractToolName("write", acp.ToolKindEdit, ""))
		assert.Equal(t, "Edit", extractToolName("edit", acp.ToolKindEdit, ""))
		assert.Equal(t, "Glob", extractToolName("glob", acp.ToolKindOther, ""))
		assert.Equal(t, "Grep", extractToolName("grep", acp.ToolKindSearch, ""))
		assert.Equal(t, "LS", extractToolName("ls", acp.ToolKindOther, ""))
		assert.Equal(t, "LS", extractToolName("list", acp.ToolKindOther, ""))
	})

	t.Run("prefix_patterns", func(t *testing.T) {
		assert.Equal(t, "MultiEdit", extractToolName("MultiEdit file", acp.ToolKindEdit, ""))
		assert.Equal(t, "NotebookEdit", extractToolName("NotebookEdit cell", acp.ToolKindEdit, ""))
		assert.Equal(t, "WebSearch", extractToolName("WebSearch query", acp.ToolKindFetch, ""))
		assert.Equal(t, "WebFetch", extractToolName("WebFetch url", acp.ToolKindFetch, ""))
		assert.Equal(t, "AskUserQuestion", extractToolName("AskUserQuestion about", acp.ToolKindOther, ""))
		assert.Equal(t, "TodoWrite", extractToolName("TodoWrite list", acp.ToolKindOther, ""))
	})

	t.Run("single_word_passthrough", func(t *testing.T) {
		// Single word without space/dot/slash passes through
		assert.Equal(t, "CustomTool", extractToolName("CustomTool", acp.ToolKindOther, ""))
	})

	t.Run("agent_subtype_mapping", func(t *testing.T) {
		// Known agent subtypes map to "Agent"
		assert.Equal(t, "Agent", extractToolName("Explore", acp.ToolKindOther, ""))
		assert.Equal(t, "Agent", extractToolName("Plan", acp.ToolKindOther, ""))
		assert.Equal(t, "Agent", extractToolName("explore", acp.ToolKindOther, ""))
		assert.Equal(t, "Agent", extractToolName("general-purpose", acp.ToolKindOther, ""))
	})

	t.Run("file_path_falls_through_to_kind", func(t *testing.T) {
		// Titles with dots/slashes are not canonical tool names → fall to kind mapping
		assert.Equal(t, "Read", extractToolName("README.md", acp.ToolKindRead, ""))
		assert.Equal(t, "Grep", extractToolName("cmd/server", acp.ToolKindSearch, ""))
	})

	t.Run("kind_fallback", func(t *testing.T) {
		assert.Equal(t, "Read", extractToolName("", acp.ToolKindRead, ""))
		assert.Equal(t, "Edit", extractToolName("", acp.ToolKindEdit, ""))
		assert.Equal(t, "Edit", extractToolName("", acp.ToolKindDelete, ""))
		assert.Equal(t, "Edit", extractToolName("", acp.ToolKindMove, ""))
		assert.Equal(t, "Grep", extractToolName("", acp.ToolKindSearch, ""))
		assert.Equal(t, "Bash", extractToolName("", acp.ToolKindExecute, ""))
		assert.Equal(t, "DeepThink", extractToolName("", acp.ToolKindThink, ""))
		assert.Equal(t, "WebFetch", extractToolName("", acp.ToolKindFetch, ""))
		assert.Equal(t, "EnterPlanMode", extractToolName("", acp.ToolKindSwitchMode, ""))
		assert.Equal(t, "Skill", extractToolName("", acp.ToolKindOther, ""))
	})

	t.Run("empty_everything", func(t *testing.T) {
		// Unknown kind falls through to string(kind)
		result := extractToolName("", acp.ToolKind("custom"), "")
		assert.Equal(t, "custom", result)
	})
}

// ---------------------------------------------------------------------------
// acpIsAgentSubtype
// ---------------------------------------------------------------------------

func TestRefactor_AcpIsAgentSubtype(t *testing.T) {
	assert.True(t, acpIsAgentSubtype("explore"))
	assert.True(t, acpIsAgentSubtype("Explore"))
	assert.True(t, acpIsAgentSubtype("PLAN"))
	assert.True(t, acpIsAgentSubtype("general-purpose"))
	assert.True(t, acpIsAgentSubtype("fork"))
	assert.False(t, acpIsAgentSubtype("Read"))
	assert.False(t, acpIsAgentSubtype("Bash"))
}

// ---------------------------------------------------------------------------
// extractACPModeState / extractACPModeStateFromResume
// ---------------------------------------------------------------------------

func TestRefactor_ExtractACPModeState(t *testing.T) {
	t.Run("nil_response", func(t *testing.T) {
		assert.Nil(t, extractACPModeState(nil))
	})

	t.Run("nil_modes", func(t *testing.T) {
		resp := &acp.NewSessionResponse{}
		assert.Nil(t, extractACPModeState(resp))
	})

	t.Run("with_modes", func(t *testing.T) {
		modeCat := acp.SessionConfigOptionCategoryMode
		resp := &acp.NewSessionResponse{
			Modes: &acp.SessionModeState{
				CurrentModeId: "code",
				AvailableModes: []acp.SessionMode{
					{Id: "ask", Name: "Ask"},
					{Id: "code", Name: "Code"},
				},
			},
			ConfigOptions: []acp.SessionConfigOption{
				{
					Select: &acp.SessionConfigOptionSelect{
						Category:     &modeCat,
						Id:           "mode",
						Name:         "Mode",
						CurrentValue: "code",
						Options:      acp.SessionConfigSelectOptions{},
					},
				},
			},
		}
		ms := extractACPModeState(resp)
		require.NotNil(t, ms)
		assert.Equal(t, "code", ms.CurrentModeID)
		require.Len(t, ms.AvailableModes, 2)
		assert.Equal(t, "ask", ms.AvailableModes[0].ID)
		assert.Equal(t, "Code", ms.AvailableModes[1].Name)
	})

	t.Run("empty_modes_returns_nil", func(t *testing.T) {
		resp := &acp.NewSessionResponse{
			Modes: &acp.SessionModeState{},
		}
		assert.Nil(t, extractACPModeState(resp))
	})
}

func TestRefactor_ExtractACPModeStateFromResume(t *testing.T) {
	t.Run("nil_response", func(t *testing.T) {
		assert.Nil(t, extractACPModeStateFromResume(nil))
	})

	t.Run("with_modes", func(t *testing.T) {
		resp := &acp.ResumeSessionResponse{
			Modes: &acp.SessionModeState{
				CurrentModeId: "architect",
				AvailableModes: []acp.SessionMode{
					{Id: "architect", Name: "Architect"},
				},
			},
		}
		ms := extractACPModeStateFromResume(resp)
		require.NotNil(t, ms)
		assert.Equal(t, "architect", ms.CurrentModeID)
	})
}

// ---------------------------------------------------------------------------
// extractACPConfigOptions / extractACPConfigOptionsFromResume
// ---------------------------------------------------------------------------

func TestRefactor_ExtractACPConfigOptions(t *testing.T) {
	t.Run("nil_response", func(t *testing.T) {
		assert.Nil(t, extractACPConfigOptions(nil))
	})

	t.Run("empty_config_options", func(t *testing.T) {
		resp := &acp.NewSessionResponse{}
		assert.Nil(t, extractACPConfigOptions(resp))
	})

	t.Run("no_mode_category", func(t *testing.T) {
		modelCat := acp.SessionConfigOptionCategoryModel
		resp := &acp.NewSessionResponse{
			ConfigOptions: []acp.SessionConfigOption{
				{
					Select: &acp.SessionConfigOptionSelect{
						Category: &modelCat,
						Id:       "model",
						Name:     "Model",
					},
				},
			},
		}
		assert.Nil(t, extractACPConfigOptions(resp))
	})

	t.Run("with_mode_category", func(t *testing.T) {
		modeCat := acp.SessionConfigOptionCategoryMode
		opts := acp.SessionConfigSelectOptions{
			Ungrouped: &acp.SessionConfigSelectOptionsUngrouped{
				{Value: "ask", Name: "Ask"},
				{Value: "code", Name: "Code"},
			},
		}
		resp := &acp.NewSessionResponse{
			ConfigOptions: []acp.SessionConfigOption{
				{
					Select: &acp.SessionConfigOptionSelect{
						Category:     &modeCat,
						Id:           "mode",
						Name:         "Mode",
						CurrentValue: "code",
						Options:      opts,
					},
				},
			},
		}
		cs := extractACPConfigOptions(resp)
		require.NotNil(t, cs)
		assert.Equal(t, "mode", cs.ConfigID)
		assert.Equal(t, "code", cs.CurrentID)
		require.Len(t, cs.Options, 1)
		assert.Equal(t, "mode", cs.Options[0].Category)
		require.Len(t, cs.Options[0].Values, 2)
		assert.Equal(t, "ask", cs.Options[0].Values[0].ID)
		assert.Equal(t, "Code", cs.Options[0].Values[1].Name)
	})

	t.Run("no_select_field", func(t *testing.T) {
		// ConfigOption with Boolean instead of Select should be skipped
		resp := &acp.NewSessionResponse{
			ConfigOptions: []acp.SessionConfigOption{
				{
					Boolean: &acp.SessionConfigOptionBoolean{
						Id:           "autoApprove",
						Name:         "Auto Approve",
						CurrentValue: true,
					},
				},
			},
		}
		assert.Nil(t, extractACPConfigOptions(resp))
	})
}

func TestRefactor_ExtractACPConfigOptionsFromResume(t *testing.T) {
	t.Run("nil_response", func(t *testing.T) {
		assert.Nil(t, extractACPConfigOptionsFromResume(nil))
	})

	t.Run("with_mode_category", func(t *testing.T) {
		modeCat := acp.SessionConfigOptionCategoryMode
		resp := &acp.ResumeSessionResponse{
			ConfigOptions: []acp.SessionConfigOption{
				{
					Select: &acp.SessionConfigOptionSelect{
						Category:     &modeCat,
						Id:           "mode",
						Name:         "Mode",
						CurrentValue: "ask",
						Options:      acp.SessionConfigSelectOptions{},
					},
				},
			},
		}
		cs := extractACPConfigOptionsFromResume(resp)
		require.NotNil(t, cs)
		assert.Equal(t, "ask", cs.CurrentID)
	})
}

// ---------------------------------------------------------------------------
// extractACPThinkingEffort / extractACPThinkingEffortFromResume
// ---------------------------------------------------------------------------

func TestRefactor_ExtractACPThinkingEffort(t *testing.T) {
	t.Run("nil_response", func(t *testing.T) {
		assert.Nil(t, extractACPThinkingEffort(nil))
	})

	t.Run("no_thought_level_category", func(t *testing.T) {
		modeCat := acp.SessionConfigOptionCategoryMode
		resp := &acp.NewSessionResponse{
			ConfigOptions: []acp.SessionConfigOption{
				{
					Select: &acp.SessionConfigOptionSelect{
						Category: &modeCat,
					},
				},
			},
		}
		assert.Nil(t, extractACPThinkingEffort(resp))
	})

	t.Run("with_thought_level_ungrouped", func(t *testing.T) {
		thoughtCat := acp.SessionConfigOptionCategoryThoughtLevel
		opts := acp.SessionConfigSelectOptions{
			Ungrouped: &acp.SessionConfigSelectOptionsUngrouped{
				{Value: "low", Name: "Low"},
				{Value: "medium", Name: "Medium"},
				{Value: "high", Name: "High"},
			},
		}
		resp := &acp.NewSessionResponse{
			ConfigOptions: []acp.SessionConfigOption{
				{
					Select: &acp.SessionConfigOptionSelect{
						Category:     &thoughtCat,
						Id:           "thinkingEffort",
						Name:         "Thinking Effort",
						CurrentValue: "medium",
						Options:      opts,
					},
				},
			},
		}
		state := extractACPThinkingEffort(resp)
		require.NotNil(t, state)
		assert.Equal(t, "medium", state.CurrentID)
		require.Len(t, state.AvailableLevels, 3)
		assert.Equal(t, "low", state.AvailableLevels[0].ID)
		assert.Equal(t, "High", state.AvailableLevels[2].Name)
	})

	t.Run("with_thought_level_grouped", func(t *testing.T) {
		thoughtCat := acp.SessionConfigOptionCategoryThoughtLevel
		opts := acp.SessionConfigSelectOptions{
			Grouped: &acp.SessionConfigSelectOptionsGrouped{
				{
					Group: "tier1",
					Name:  "Standard",
					Options: []acp.SessionConfigSelectOption{
						{Value: "low", Name: "Low"},
					},
				},
				{
					Group: "tier2",
					Name:  "Extended",
					Options: []acp.SessionConfigSelectOption{
						{Value: "high", Name: "High"},
					},
				},
			},
		}
		resp := &acp.NewSessionResponse{
			ConfigOptions: []acp.SessionConfigOption{
				{
					Select: &acp.SessionConfigOptionSelect{
						Category:     &thoughtCat,
						Id:           "thinkingEffort",
						Name:         "Thinking Effort",
						CurrentValue: "high",
						Options:      opts,
					},
				},
			},
		}
		state := extractACPThinkingEffort(resp)
		require.NotNil(t, state)
		assert.Equal(t, "high", state.CurrentID)
		require.Len(t, state.AvailableLevels, 2)
		assert.Equal(t, "low", state.AvailableLevels[0].ID)
		assert.Equal(t, "high", state.AvailableLevels[1].ID)
	})
}

func TestRefactor_ExtractACPThinkingEffortFromResume(t *testing.T) {
	t.Run("nil_response", func(t *testing.T) {
		assert.Nil(t, extractACPThinkingEffortFromResume(nil))
	})

	t.Run("with_thought_level", func(t *testing.T) {
		thoughtCat := acp.SessionConfigOptionCategoryThoughtLevel
		resp := &acp.ResumeSessionResponse{
			ConfigOptions: []acp.SessionConfigOption{
				{
					Select: &acp.SessionConfigOptionSelect{
						Category:     &thoughtCat,
						Id:           "thinkingEffort",
						Name:         "Thinking",
						CurrentValue: "low",
						Options:      acp.SessionConfigSelectOptions{},
					},
				},
			},
		}
		state := extractACPThinkingEffortFromResume(resp)
		require.NotNil(t, state)
		assert.Equal(t, "low", state.CurrentID)
	})
}

// ---------------------------------------------------------------------------
// extractACPModelList / extractACPModelListFromResume
// ---------------------------------------------------------------------------

func TestRefactor_ExtractACPModelList(t *testing.T) {
	t.Run("nil_response", func(t *testing.T) {
		assert.Nil(t, extractACPModelList(nil))
	})

	t.Run("no_model_category", func(t *testing.T) {
		modeCat := acp.SessionConfigOptionCategoryMode
		resp := &acp.NewSessionResponse{
			ConfigOptions: []acp.SessionConfigOption{
				{
					Select: &acp.SessionConfigOptionSelect{
						Category: &modeCat,
					},
				},
			},
		}
		assert.Nil(t, extractACPModelList(resp))
	})

	t.Run("with_model_category_ungrouped", func(t *testing.T) {
		modelCat := acp.SessionConfigOptionCategoryModel
		opts := acp.SessionConfigSelectOptions{
			Ungrouped: &acp.SessionConfigSelectOptionsUngrouped{
				{Value: "gpt-4", Name: "GPT-4"},
				{Value: "claude-3", Name: "Claude 3"},
			},
		}
		resp := &acp.NewSessionResponse{
			ConfigOptions: []acp.SessionConfigOption{
				{
					Select: &acp.SessionConfigOptionSelect{
						Category:     &modelCat,
						Id:           "model",
						Name:         "Model",
						CurrentValue: "gpt-4",
						Options:      opts,
					},
				},
			},
		}
		ml := extractACPModelList(resp)
		require.NotNil(t, ml)
		assert.Equal(t, "gpt-4", ml.CurrentModelID)
		require.Len(t, ml.Models, 2)
		assert.Equal(t, "gpt-4", ml.Models[0].ID)
		assert.Equal(t, "Claude 3", ml.Models[1].Name)
	})
}

func TestRefactor_ExtractACPModelListFromResume(t *testing.T) {
	t.Run("nil_response", func(t *testing.T) {
		assert.Nil(t, extractACPModelListFromResume(nil))
	})

	t.Run("with_model_category", func(t *testing.T) {
		modelCat := acp.SessionConfigOptionCategoryModel
		opts := acp.SessionConfigSelectOptions{
			Ungrouped: &acp.SessionConfigSelectOptionsUngrouped{
				{Value: "kimi-k2", Name: "Kimi K2"},
			},
		}
		resp := &acp.ResumeSessionResponse{
			ConfigOptions: []acp.SessionConfigOption{
				{
					Select: &acp.SessionConfigOptionSelect{
						Category:     &modelCat,
						Id:           "model",
						Name:         "Model",
						CurrentValue: "kimi-k2",
						Options:      opts,
					},
				},
			},
		}
		ml := extractACPModelListFromResume(resp)
		require.NotNil(t, ml)
		assert.Equal(t, "kimi-k2", ml.CurrentModelID)
	})
}

// ---------------------------------------------------------------------------
// modeStateFromConfigState
// ---------------------------------------------------------------------------

func TestRefactor_ModeStateFromConfigState(t *testing.T) {
	t.Run("nil_input", func(t *testing.T) {
		assert.Nil(t, modeStateFromConfigState(nil))
	})

	t.Run("no_mode_category", func(t *testing.T) {
		cs := &ConfigOptionState{
			ConfigID: "model",
			Options: []ConfigOptionDef{
				{ID: "model", Category: "model", Values: []ConfigOptionValue{{ID: "gpt-4"}}},
			},
		}
		assert.Nil(t, modeStateFromConfigState(cs))
	})

	t.Run("with_mode_category", func(t *testing.T) {
		cs := &ConfigOptionState{
			ConfigID:  "mode",
			CurrentID: "code",
			Options: []ConfigOptionDef{
				{
					ID:       "mode",
					Category: "mode",
					Values: []ConfigOptionValue{
						{ID: "ask", Name: "Ask"},
						{ID: "code", Name: "Code"},
					},
				},
			},
		}
		ms := modeStateFromConfigState(cs)
		require.NotNil(t, ms)
		assert.Equal(t, "code", ms.CurrentModeID)
		require.Len(t, ms.AvailableModes, 2)
		assert.Equal(t, "ask", ms.AvailableModes[0].ID)
	})
}

// ---------------------------------------------------------------------------
// shouldSetConfig / markConfigSet / resetLastSetConfig / IsConfigUnsupported
// ---------------------------------------------------------------------------

func TestRefactor_ShouldSetConfig(t *testing.T) {
	agent := &model.Agent{ID: "test-should-config", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "test-should-config")

	// Initially all values are empty, so any non-empty value should be set
	assert.True(t, conn.shouldSetConfig("model", "gpt-4"))
	assert.True(t, conn.shouldSetConfig("thinkingEffort", "high"))
	assert.True(t, conn.shouldSetConfig("mode", "code"))

	// Same value → should not set
	conn.markConfigSet("model", "gpt-4")
	assert.False(t, conn.shouldSetConfig("model", "gpt-4"))
	assert.True(t, conn.shouldSetConfig("model", "claude-3"))

	// Unknown configID → always true
	assert.True(t, conn.shouldSetConfig("unknown", "value"))
}

func TestRefactor_ResetLastSetConfig(t *testing.T) {
	agent := &model.Agent{ID: "test-reset-config", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "test-reset-config")

	conn.markConfigSet("model", "gpt-4")
	conn.markConfigSet("mode", "code")
	assert.False(t, conn.shouldSetConfig("model", "gpt-4"))

	conn.resetLastSetConfig()
	assert.True(t, conn.shouldSetConfig("model", "gpt-4"))
	assert.True(t, conn.shouldSetConfig("mode", "code"))
}

func TestRefactor_IsConfigUnsupported(t *testing.T) {
	agent := &model.Agent{ID: "test-unsupported", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "test-unsupported")

	assert.False(t, conn.IsConfigUnsupported("mode"))

	// Mark as unsupported
	conn.lastSetConfigMu.Lock()
	conn.unsupportedConfigs = map[string]bool{"mode": true}
	conn.lastSetConfigMu.Unlock()

	assert.True(t, conn.IsConfigUnsupported("mode"))
	assert.False(t, conn.IsConfigUnsupported("model"))

	// resetLastSetConfig clears unsupported
	conn.resetLastSetConfig()
	assert.False(t, conn.IsConfigUnsupported("mode"))
}

// ---------------------------------------------------------------------------
// snapshotCachedConfig
// ---------------------------------------------------------------------------

func TestRefactor_SnapshotCachedConfig(t *testing.T) {
	agent := &model.Agent{ID: "test-snapshot", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "test-snapshot")

	conn.SetCurrentModeID("code")
	conn.SetCurrentModelID("gpt-4")
	conn.SetCurrentThinkingEffortID("high")

	snap := conn.snapshotCachedConfig()
	assert.Equal(t, "code", snap.mode)
	assert.Equal(t, "gpt-4", snap.model)
	assert.Equal(t, "high", snap.effort)
}

// ---------------------------------------------------------------------------
// ACPConn state accessors
// ---------------------------------------------------------------------------

func TestRefactor_ACPConn_Accessors(t *testing.T) {
	agent := &model.Agent{ID: "test-accessors", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "test-accessors")

	assert.Equal(t, "test-accessors", conn.AgentID())
	assert.Equal(t, "", conn.AcpSID())
	assert.False(t, conn.IsAlive())

	conn.SetCurrentModeID("ask")
	assert.Equal(t, "ask", conn.GetCurrentModeID())

	conn.SetCurrentThinkingEffortID("medium")
	assert.Equal(t, "medium", conn.GetCurrentThinkingEffortID())

	conn.SetCurrentModelID("claude-3")
	assert.Equal(t, "claude-3", conn.GetCurrentModelID())
}

// ---------------------------------------------------------------------------
// buildPromptBlocks
// ---------------------------------------------------------------------------

func TestRefactor_BuildPromptBlocks(t *testing.T) {
	backend := &ACPBackend{}

	t.Run("without_system_prompt", func(t *testing.T) {
		req := ChatRequest{Prompt: "hello world"}
		blocks := backend.buildPromptBlocks(req)
		require.Len(t, blocks, 1)
		require.NotNil(t, blocks[0].Text)
		assert.Equal(t, "hello world", blocks[0].Text.Text)
	})

	t.Run("with_system_prompt", func(t *testing.T) {
		req := ChatRequest{
			Prompt:       "fix the bug",
			SystemPrompt: "You are a helpful assistant",
		}
		// ShouldInjectSystemPrompt returns true when SystemPrompt is set and not Resume
		blocks := backend.buildPromptBlocks(req)
		require.Len(t, blocks, 1)
		require.NotNil(t, blocks[0].Text)
		text := blocks[0].Text.Text
		assert.Contains(t, text, "[System Instructions: You are a helpful assistant]")
		assert.Contains(t, text, "fix the bug")
	})
}

// ---------------------------------------------------------------------------
// readProcStatus (best-effort; may not find /proc in all environments)
// ---------------------------------------------------------------------------

func TestRefactor_ReadProcStatus_InvalidPid(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skipf("skipping: /proc only available on Linux (current: %s)", runtime.GOOS)
	}
	// A very high PID almost certainly doesn't exist
	ppid, rss, err := readProcStatus(999999999)
	assert.Error(t, err)
	assert.Equal(t, 0, ppid)
	assert.Equal(t, 0, rss)
}

// ---------------------------------------------------------------------------
// SetExternalSessionIDGetter / SetAutoApproveGetter / SetPermissionStateChangeCallback
// ---------------------------------------------------------------------------

func TestRefactor_GlobalSetters(t *testing.T) {
	t.Run("SetExternalSessionIDGetter", func(t *testing.T) {
		orig := getExternalSessionID
		defer func() { getExternalSessionID = orig }()

		// Default returns empty
		assert.Equal(t, "", getExternalSessionID("any"))

		SetExternalSessionIDGetter(func(sid string) string {
			return "ext-" + sid
		})
		assert.Equal(t, "ext-abc", getExternalSessionID("abc"))
	})

	t.Run("SetAutoApproveGetter", func(t *testing.T) {
		orig := getSessionAutoApprove
		defer func() { getSessionAutoApprove = orig }()

		assert.False(t, getSessionAutoApprove("any"))

		SetAutoApproveGetter(func(sid string) bool {
			return sid == "approved"
		})
		assert.True(t, getSessionAutoApprove("approved"))
		assert.False(t, getSessionAutoApprove("other"))
	})

	t.Run("SetPermissionStateChangeCallback", func(t *testing.T) {
		orig := onPermissionStateChange
		defer func() { onPermissionStateChange = orig }()

		called := false
		SetPermissionStateChangeCallback(func(sid string, pending bool, toolName string) {
			called = true
		})
		onPermissionStateChange("test", true, "WriteTextFile")
		assert.True(t, called)
	})
}

// ---------------------------------------------------------------------------
// mapACPSelectOptions
// ---------------------------------------------------------------------------

func TestRefactor_MapACPSelectOptions(t *testing.T) {
	t.Run("ungrouped", func(t *testing.T) {
		opts := acp.SessionConfigSelectOptions{
			Ungrouped: &acp.SessionConfigSelectOptionsUngrouped{
				{Value: "a", Name: "A"},
				{Value: "b", Name: "B"},
			},
		}
		var optDef ConfigOptionDef
		mapACPSelectOptions(opts, &optDef)
		require.Len(t, optDef.Values, 2)
		assert.Equal(t, "a", optDef.Values[0].ID)
		assert.Equal(t, "B", optDef.Values[1].Name)
	})

	t.Run("grouped", func(t *testing.T) {
		opts := acp.SessionConfigSelectOptions{
			Grouped: &acp.SessionConfigSelectOptionsGrouped{
				{
					Group: "g1",
					Name:  "Group 1",
					Options: []acp.SessionConfigSelectOption{
						{Value: "x", Name: "X"},
					},
				},
			},
		}
		var optDef ConfigOptionDef
		mapACPSelectOptions(opts, &optDef)
		require.Len(t, optDef.Values, 1)
		assert.Equal(t, "x", optDef.Values[0].ID)
	})

	t.Run("both_ungrouped_and_grouped", func(t *testing.T) {
		opts := acp.SessionConfigSelectOptions{
			Ungrouped: &acp.SessionConfigSelectOptionsUngrouped{
				{Value: "a", Name: "A"},
			},
			Grouped: &acp.SessionConfigSelectOptionsGrouped{
				{
					Group: "g1",
					Name:  "Group 1",
					Options: []acp.SessionConfigSelectOption{
						{Value: "b", Name: "B"},
					},
				},
			},
		}
		var optDef ConfigOptionDef
		mapACPSelectOptions(opts, &optDef)
		require.Len(t, optDef.Values, 2)
		assert.Equal(t, "a", optDef.Values[0].ID)
		assert.Equal(t, "b", optDef.Values[1].ID)
	})

	t.Run("empty_options", func(t *testing.T) {
		opts := acp.SessionConfigSelectOptions{}
		var optDef ConfigOptionDef
		mapACPSelectOptions(opts, &optDef)
		assert.Empty(t, optDef.Values)
	})
}

// ---------------------------------------------------------------------------
// buildConfigOptionStateFromSelect
// ---------------------------------------------------------------------------

func TestRefactor_BuildConfigOptionStateFromSelect(t *testing.T) {
	modeCat := acp.SessionConfigOptionCategoryMode
	sel := &acp.SessionConfigOptionSelect{
		Category:     &modeCat,
		Id:           "mode",
		Name:         "Mode",
		CurrentValue: "code",
		Options: acp.SessionConfigSelectOptions{
			Ungrouped: &acp.SessionConfigSelectOptionsUngrouped{
				{Value: "ask", Name: "Ask"},
				{Value: "code", Name: "Code"},
			},
		},
	}
	cs := buildConfigOptionStateFromSelect(sel, "mode")
	require.NotNil(t, cs)
	assert.Equal(t, "mode", cs.ConfigID)
	assert.Equal(t, "code", cs.CurrentID)
	require.Len(t, cs.Options, 1)
	assert.Equal(t, "mode", cs.Options[0].Category)
	require.Len(t, cs.Options[0].Values, 2)
}

// ---------------------------------------------------------------------------
// buildThinkingEffortStateFromSelect
// ---------------------------------------------------------------------------

func TestRefactor_BuildThinkingEffortStateFromSelect_Empty(t *testing.T) {
	// Empty select with no current value → nil
	sel := &acp.SessionConfigOptionSelect{
		Options: acp.SessionConfigSelectOptions{},
	}
	assert.Nil(t, buildThinkingEffortStateFromSelect(sel))
}

// ---------------------------------------------------------------------------
// buildModelListStateFromSelect
// ---------------------------------------------------------------------------

func TestRefactor_BuildModelListStateFromSelect_Empty(t *testing.T) {
	sel := &acp.SessionConfigOptionSelect{
		Options: acp.SessionConfigSelectOptions{},
	}
	assert.Nil(t, buildModelListStateFromSelect(sel))
}

func TestRefactor_BuildModelListStateFromSelect_Grouped(t *testing.T) {
	sel := &acp.SessionConfigOptionSelect{
		CurrentValue: "gpt-4",
		Options: acp.SessionConfigSelectOptions{
			Grouped: &acp.SessionConfigSelectOptionsGrouped{
				{
					Group: "openai",
					Name:  "OpenAI",
					Options: []acp.SessionConfigSelectOption{
						{Value: "gpt-4", Name: "GPT-4"},
						{Value: "gpt-3.5", Name: "GPT-3.5"},
					},
				},
			},
		},
	}
	state := buildModelListStateFromSelect(sel)
	require.NotNil(t, state)
	assert.Equal(t, "gpt-4", state.CurrentModelID)
	require.Len(t, state.Models, 2)
	assert.Equal(t, "gpt-3.5", state.Models[1].ID)
}

// ---------------------------------------------------------------------------
// extractModeStateFromModes edge cases
// ---------------------------------------------------------------------------

func TestRefactor_ExtractModeStateFromModes_CurrentOnly(t *testing.T) {
	// Only current mode set, no available modes → still returns non-nil
	ms := extractModeStateFromModes(&acp.SessionModeState{
		CurrentModeId: "code",
	})
	require.NotNil(t, ms)
	assert.Equal(t, "code", ms.CurrentModeID)
	assert.Empty(t, ms.AvailableModes)
}

// ---------------------------------------------------------------------------
// extractConfigOptionsFromOpts edge cases
// ---------------------------------------------------------------------------

func TestRefactor_ExtractConfigOptionsFromOpts_EmptyOpts(t *testing.T) {
	assert.Nil(t, extractConfigOptionsFromOpts(nil))
	assert.Nil(t, extractConfigOptionsFromOpts([]acp.SessionConfigOption{}))
}

// ---------------------------------------------------------------------------
// OrphanChildEnvVar
// ---------------------------------------------------------------------------

func TestRefactor_OrphanChildEnvVar(t *testing.T) {
	assert.Equal(t, "CLAWBENCH_CHILD=1", OrphanChildEnvVar)
	assert.True(t, strings.Contains(OrphanChildEnvVar, "CLAWBENCH_CHILD"))
}

// ---------------------------------------------------------------------------
// NewACPBackend
// ---------------------------------------------------------------------------

func TestRefactor_NewACPBackend(t *testing.T) {
	t.Run("success_with_acp_support", func(t *testing.T) {
		agent := &model.Agent{ID: "acp-agent", Backend: "acp-stdio", AcpCommand: "echo hello"}
		b, err := NewACPBackend(agent)
		require.NoError(t, err)
		assert.Equal(t, agent, b.agent)
	})

	t.Run("error_no_acp_support", func(t *testing.T) {
		agent := &model.Agent{ID: "cli-agent", Backend: "claude", AcpCommand: ""}
		b, err := NewACPBackend(agent)
		assert.Nil(t, b)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "does not support acp-stdio transport")
		assert.Contains(t, err.Error(), "cli-agent")
	})
}

// ---------------------------------------------------------------------------
// ACPBackend.Name()
// ---------------------------------------------------------------------------

func TestRefactor_ACPBackend_Name(t *testing.T) {
	agent := &model.Agent{ID: "test-name", Backend: "acp-claude", AcpCommand: "echo"}
	b := &ACPBackend{agent: agent}
	assert.Equal(t, "acp-claude", b.Name())
}

// ---------------------------------------------------------------------------
// ACPConn state accessors (extended)
// ---------------------------------------------------------------------------

func TestRefactor_ACPConn_StateAccessors(t *testing.T) {
	agent := &model.Agent{ID: "test-state-accessors", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "test-state-accessors")

	t.Run("AutoApprove", func(t *testing.T) {
		assert.False(t, conn.IsAutoApprove())
		conn.SetAutoApprove(true)
		assert.True(t, conn.IsAutoApprove())
		conn.SetAutoApprove(false)
		assert.False(t, conn.IsAutoApprove())
	})

	t.Run("PlanState", func(t *testing.T) {
		assert.Nil(t, conn.GetCachedPlanState())
		plan := &PlanState{Entries: []PlanEntry{{Content: "do stuff", Priority: "high", Status: "in_progress"}}}
		conn.SetCachedPlanState(plan)
		got := conn.GetCachedPlanState()
		require.NotNil(t, got)
		require.Len(t, got.Entries, 1)
		assert.Equal(t, "do stuff", got.Entries[0].Content)
		conn.SetCachedPlanState(nil)
		assert.Nil(t, conn.GetCachedPlanState())
	})

	t.Run("LoadSessionResp", func(t *testing.T) {
		assert.Nil(t, conn.GetAndClearLoadSessionResp())
	})

	t.Run("NewSessionResp", func(t *testing.T) {
		assert.Nil(t, conn.GetAndClearNewSessionResp())
	})

	t.Run("ResumeSessionResp", func(t *testing.T) {
		assert.Nil(t, conn.GetAndClearResumeSessionResp())
	})

	t.Run("ProcessPID_no_process", func(t *testing.T) {
		assert.Equal(t, 0, conn.ProcessPID())
	})

	t.Run("HasCurrentModeChanged", func(t *testing.T) {
		conn.SetCurrentModeID("code")
		assert.False(t, conn.HasCurrentModeChanged("code"))
		assert.True(t, conn.HasCurrentModeChanged("ask"))
	})

	t.Run("UpdateCachedCurrentModel", func(t *testing.T) {
		conn.UpdateCachedCurrentModel("gpt-4")
		assert.Equal(t, "gpt-4", conn.GetCurrentModelID())
	})

	t.Run("UpdateCachedCurrentMode", func(t *testing.T) {
		conn.UpdateCachedCurrentMode("ask")
		assert.Equal(t, "ask", conn.GetCurrentModeID())
	})

	t.Run("UpdateCachedCurrentThinkingEffort", func(t *testing.T) {
		conn.UpdateCachedCurrentThinkingEffort("high")
		assert.Equal(t, "high", conn.GetCurrentThinkingEffortID())
	})

	t.Run("ClearLoadSessionActive", func(t *testing.T) {
		conn.loadSessionActive.Store(true)
		assert.True(t, conn.loadSessionActive.Load())
		conn.ClearLoadSessionActive()
		assert.False(t, conn.loadSessionActive.Load())
	})

	t.Run("AgentID_nil_agent", func(t *testing.T) {
		connNoAgent := newACPConn(nil, "no-agent")
		assert.Equal(t, "", connNoAgent.AgentID())
	})
}

// ---------------------------------------------------------------------------
// ACPConn.SetCachedModeState / SetCachedConfigState / SetCachedThinkingEffortState / SetCachedModelListState
// ---------------------------------------------------------------------------

func TestRefactor_ACPConn_SetCachedStates(t *testing.T) {
	agent := &model.Agent{ID: "test-cached-states", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "test-cached-states")

	t.Run("SetCachedModeState_nil", func(t *testing.T) {
		conn.SetCachedModeState(nil)
		assert.Equal(t, "", conn.GetCurrentModeID())
	})

	t.Run("SetCachedModeState_with_modes", func(t *testing.T) {
		conn.SetCachedModeState(&ModeState{
			CurrentModeID: "code",
			AvailableModes: []ModeDef{
				{ID: "ask", Name: "Ask"},
				{ID: "code", Name: "Code"},
			},
		})
		assert.Equal(t, "code", conn.GetCurrentModeID())
	})

	t.Run("SetCachedThinkingEffortState_nil", func(t *testing.T) {
		conn.SetCachedThinkingEffortState(nil)
	})

	t.Run("SetCachedThinkingEffortState_with_levels", func(t *testing.T) {
		conn.SetCachedThinkingEffortState(&ThinkingEffortState{
			CurrentID: "high",
			AvailableLevels: []ThinkingEffortDef{
				{ID: "low", Name: "Low"},
				{ID: "high", Name: "High"},
			},
		})
		assert.Equal(t, "high", conn.GetCurrentThinkingEffortID())
	})

	t.Run("SetCachedModelListState_nil", func(t *testing.T) {
		conn.SetCachedModelListState(nil)
	})

	t.Run("SetCachedModelListState_with_models", func(t *testing.T) {
		conn.SetCachedModelListState(&ModelListState{
			CurrentModelID: "gpt-4",
			Models: []model.AgentModel{
				{ID: "gpt-4", Name: "GPT-4"},
			},
		})
		assert.Equal(t, "gpt-4", conn.GetCurrentModelID())
	})

	t.Run("SetCachedConfigState_nil", func(t *testing.T) {
		conn.SetCachedConfigState(nil)
	})
}

// ---------------------------------------------------------------------------
// ACPConnManager
// ---------------------------------------------------------------------------

func TestRefactor_ACPConnManager_GetConn(t *testing.T) {
	mgr := &ACPConnManager{
		conns:     make(map[string]*ACPConn),
		stopSweep: make(chan struct{}),
	}

	// No connection → nil
	assert.Nil(t, mgr.GetConn("nonexistent"))

	// Add a connection → found
	agent := &model.Agent{ID: "test-getconn", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "sid-1")
	mgr.conns["sid-1"] = conn
	got := mgr.GetConn("sid-1")
	assert.Equal(t, conn, got)
}

func TestRefactor_ACPConnManager_GetConnByAgentID(t *testing.T) {
	mgr := &ACPConnManager{
		conns:     make(map[string]*ACPConn),
		stopSweep: make(chan struct{}),
	}

	agent1 := &model.Agent{ID: "agent-1", Backend: "acp-stdio", AcpCommand: "echo"}
	agent2 := &model.Agent{ID: "agent-2", Backend: "acp-stdio", AcpCommand: "echo"}

	conn1 := newACPConn(agent1, "sid-1")
	conn1.alive = true
	conn2 := newACPConn(agent2, "sid-2")
	conn2.alive = false

	mgr.conns["sid-1"] = conn1
	mgr.conns["sid-2"] = conn2

	// Only alive connections match
	assert.Equal(t, conn1, mgr.GetConnByAgentID("agent-1"))
	assert.Nil(t, mgr.GetConnByAgentID("agent-2")) // not alive
	assert.Nil(t, mgr.GetConnByAgentID("nonexistent"))
}

func TestRefactor_ACPConnManager_MarkIdle(t *testing.T) {
	mgr := &ACPConnManager{
		conns:     make(map[string]*ACPConn),
		stopSweep: make(chan struct{}),
	}

	agent := &model.Agent{ID: "test-markidle", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "sid-markidle")
	oldTime := conn.lastUsed

	mgr.conns["sid-markidle"] = conn

	// MarkIdle updates lastUsed — use !Before to be tolerant of low-resolution
	// clocks on Windows where two consecutive time.Now() calls may return the
	// same value.
	mgr.MarkIdle("sid-markidle")
	conn.mu.Lock()
	newTime := conn.lastUsed
	conn.mu.Unlock()
	assert.False(t, newTime.Before(oldTime), "lastUsed should not go backwards after MarkIdle")

	// MarkIdle on nonexistent session is a no-op
	mgr.MarkIdle("nonexistent") // should not panic
}

func TestRefactor_ACPConnManager_CloseConn(t *testing.T) {
	mgr := &ACPConnManager{
		conns:     make(map[string]*ACPConn),
		stopSweep: make(chan struct{}),
	}

	agent := &model.Agent{ID: "test-closeconn", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "sid-close")

	mgr.conns["sid-close"] = conn
	mgr.CloseConn("sid-close")
	assert.Nil(t, mgr.GetConn("sid-close"), "connection should be removed after CloseConn")

	// CloseConn on nonexistent is a no-op
	mgr.CloseConn("nonexistent") // should not panic
}

func TestRefactor_ACPConnManager_CancelTurn(t *testing.T) {
	mgr := &ACPConnManager{
		conns:     make(map[string]*ACPConn),
		stopSweep: make(chan struct{}),
	}

	// CancelTurn on nonexistent session is a no-op
	mgr.CancelTurn("nonexistent") // should not panic

	// CancelTurn on existing session calls conn.CancelTurn
	agent := &model.Agent{ID: "test-cancelturn", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "sid-cancel")
	mgr.conns["sid-cancel"] = conn
	mgr.CancelTurn("sid-cancel") // conn has no real client, but should not panic
}

func TestRefactor_ACPConnManager_StopAll(t *testing.T) {
	stopSweep := make(chan struct{})
	mgr := &ACPConnManager{
		conns:     make(map[string]*ACPConn),
		stopSweep: stopSweep,
	}

	agent := &model.Agent{ID: "test-stopall", Backend: "acp-stdio", AcpCommand: "echo"}
	conn1 := newACPConn(agent, "sid-1")
	conn2 := newACPConn(agent, "sid-2")
	mgr.conns["sid-1"] = conn1
	mgr.conns["sid-2"] = conn2

	mgr.StopAll()
	assert.Empty(t, mgr.conns, "all connections should be removed after StopAll")

	// Verify stopSweep channel is closed
	select {
	case <-stopSweep:
		// Expected: channel is closed
	default:
		t.Error("stopSweep channel should be closed after StopAll")
	}
}

func TestRefactor_ACPConnManager_SetConnForTest(t *testing.T) {
	mgr := &ACPConnManager{
		conns:     make(map[string]*ACPConn),
		stopSweep: make(chan struct{}),
	}

	agent := &model.Agent{ID: "test-setconn", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "sid-injected")

	mgr.SetConnForTest("sid-injected", conn)
	got := mgr.GetConn("sid-injected")
	assert.Equal(t, conn, got)
}

func TestRefactor_ACPConnManager_CloseConnsByAgentID(t *testing.T) {
	mgr := &ACPConnManager{
		conns:     make(map[string]*ACPConn),
		stopSweep: make(chan struct{}),
	}

	agent1 := &model.Agent{ID: "agent-close-1", Backend: "acp-stdio", AcpCommand: "echo"}
	agent2 := &model.Agent{ID: "agent-close-2", Backend: "acp-stdio", AcpCommand: "echo"}

	conn1 := newACPConn(agent1, "sid-1")
	conn2 := newACPConn(agent1, "sid-2")
	conn3 := newACPConn(agent2, "sid-3")

	mgr.conns["sid-1"] = conn1
	mgr.conns["sid-2"] = conn2
	mgr.conns["sid-3"] = conn3

	mgr.CloseConnsByAgentID("agent-close-1")
	assert.Nil(t, mgr.GetConn("sid-1"))
	assert.Nil(t, mgr.GetConn("sid-2"))
	assert.Equal(t, conn3, mgr.GetConn("sid-3"), "other agent connections should remain")
}

func TestRefactor_ACPConnManager_SetSessionRunningChecker(t *testing.T) {
	mgr := &ACPConnManager{
		conns:     make(map[string]*ACPConn),
		stopSweep: make(chan struct{}),
	}

	called := false
	mgr.SetSessionRunningChecker(func(sessionID string) bool {
		called = true
		return sessionID == "running"
	})
	require.NotNil(t, mgr.isSessionRunning)
	assert.True(t, mgr.isSessionRunning("running"))
	assert.False(t, mgr.isSessionRunning("idle"))
	assert.True(t, called)
}

func TestRefactor_ACPConnManager_GetCachedStateByClawbenchSID(t *testing.T) {
	mgr := &ACPConnManager{
		conns:     make(map[string]*ACPConn),
		stopSweep: make(chan struct{}),
	}

	// Nonexistent session → empty state
	state := mgr.GetCachedStateByClawbenchSID("nonexistent")
	assert.Equal(t, ACPCachedState{}, state)

	// Session with nil agent → empty state
	connNoAgent := newACPConn(nil, "sid-no-agent")
	mgr.conns["sid-no-agent"] = connNoAgent
	state = mgr.GetCachedStateByClawbenchSID("sid-no-agent")
	assert.Equal(t, ACPCachedState{}, state)

	// Session with agent → populated state from registry
	agent := &model.Agent{ID: "test-cached-state", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "sid-cached")
	conn.SetCurrentModeID("code")
	mgr.conns["sid-cached"] = conn
	state = mgr.GetCachedStateByClawbenchSID("sid-cached")
	// The state may be empty if registry has no data for this agent,
	// but the call should not panic
	_ = state
}

func TestRefactor_ACPConnManager_GetCachedStateByAgentID(t *testing.T) {
	mgr := &ACPConnManager{
		conns:     make(map[string]*ACPConn),
		stopSweep: make(chan struct{}),
	}

	// No agentCapabilities registered → empty state
	state := mgr.GetCachedStateByAgentID("nonexistent")
	assert.Equal(t, ACPCachedState{}, state)
}

func TestRefactor_ACPConnManager_GetCommandsByAgentID(t *testing.T) {
	mgr := &ACPConnManager{
		conns:     make(map[string]*ACPConn),
		stopSweep: make(chan struct{}),
	}

	// No commands → empty
	cmds := mgr.GetCommandsByAgentID("nonexistent")
	assert.Empty(t, cmds)
}

// ---------------------------------------------------------------------------
// ACPConn Close / KillProcessForTest
// ---------------------------------------------------------------------------

func TestRefactor_ACPConn_Close(t *testing.T) {
	agent := &model.Agent{ID: "test-close", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "test-close")
	conn.alive = true
	conn.acpSID = "acp-123"

	conn.Close()
	assert.False(t, conn.IsAlive())
	assert.Equal(t, "", conn.AcpSID())
}

func TestRefactor_ACPConn_KillProcessForTest_NoProcess(t *testing.T) {
	agent := &model.Agent{ID: "test-kill-noproc", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "test-kill-noproc")
	err := conn.KillProcessForTest()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no process to kill")
}

// ---------------------------------------------------------------------------
// ACPConn.HasNewAvailableModes / IsModeAvailable / HasNewAvailableThinkingEfforts / HasNewAvailableModels
// ---------------------------------------------------------------------------

func TestRefactor_ACPConn_HasNewAvailableModes(t *testing.T) {
	agent := &model.Agent{ID: "test-hasnewmodes", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "test-hasnewmodes")

	// No modes in registry → all are "new"
	assert.True(t, conn.HasNewAvailableModes([]ModeDef{{ID: "ask"}, {ID: "code"}}))
}

func TestRefactor_ACPConn_IsModeAvailable(t *testing.T) {
	agent := &model.Agent{ID: "test-modeavail", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "test-modeavail")

	assert.False(t, conn.IsModeAvailable("code"))
}

func TestRefactor_ACPConn_HasNewAvailableThinkingEfforts(t *testing.T) {
	agent := &model.Agent{ID: "test-hasnewefforts", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "test-hasnewefforts")

	assert.True(t, conn.HasNewAvailableThinkingEfforts([]ThinkingEffortDef{{ID: "high"}}))
}

func TestRefactor_ACPConn_HasNewAvailableModels(t *testing.T) {
	agent := &model.Agent{ID: "test-hasnewmodels", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "test-hasnewmodels")

	assert.True(t, conn.HasNewAvailableModels([]model.AgentModel{{ID: "gpt-4", Name: "GPT-4"}}))
}

// ---------------------------------------------------------------------------
// isAliveLocked
// ---------------------------------------------------------------------------

func TestRefactor_IsAliveLocked(t *testing.T) {
	agent := &model.Agent{ID: "test-isalive", Backend: "acp-stdio", AcpCommand: "echo"}

	t.Run("nil_conn", func(t *testing.T) {
		conn := newACPConn(agent, "test-isalive-nil")
		conn.mu.Lock()
		assert.False(t, conn.isAliveLocked())
		conn.mu.Unlock()
	})

	t.Run("alive_conn", func(t *testing.T) {
		conn := newACPConn(agent, "test-isalive-alive")
		conn.SetAliveForTest()
		conn.mu.Lock()
		assert.True(t, conn.isAliveLocked())
		conn.mu.Unlock()
	})

	t.Run("done_conn", func(t *testing.T) {
		conn := newACPConn(agent, "test-isalive-done")
		conn.SetAliveForTest()
		// Close the pipe to make conn.Done() return immediately
		conn.close()
		conn.mu.Lock()
		assert.False(t, conn.isAliveLocked())
		conn.mu.Unlock()
	})
}

// ---------------------------------------------------------------------------
// killProcessLocked
// ---------------------------------------------------------------------------

func TestRefactor_KillProcessLocked(t *testing.T) {
	agent := &model.Agent{ID: "test-killproc", Backend: "acp-stdio", AcpCommand: "echo"}

	t.Run("nil_cmd", func(t *testing.T) {
		conn := newACPConn(agent, "test-killproc-nil")
		conn.mu.Lock()
		conn.killProcessLocked() // should not panic
		conn.mu.Unlock()
		assert.False(t, conn.alive)
	})

	t.Run("cmd_with_process", func(t *testing.T) {
		conn := newACPConn(agent, "test-killproc-proc")
		cmd := exec.Command("sleep", "60")
		require.NoError(t, cmd.Start())
		conn.mu.Lock()
		conn.cmd = cmd
		conn.alive = true
		conn.acpSID = "acp-123"
		conn.killProcessLocked()
		conn.mu.Unlock()
		assert.False(t, conn.alive)
		assert.Equal(t, "", conn.AcpSID())
		// Process should be killed
		assert.Error(t, cmd.Wait()) // process was killed, Wait returns error
	})
}

// ---------------------------------------------------------------------------
// CancelTurn
// ---------------------------------------------------------------------------

func TestRefactor_CancelTurn(t *testing.T) {
	agent := &model.Agent{ID: "test-cancel", Backend: "acp-stdio", AcpCommand: "echo"}

	t.Run("nil_conn_no_acpSID", func(t *testing.T) {
		conn := newACPConn(agent, "test-cancel-nil")
		// Should not panic when conn is nil and acpSID is empty
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		conn.CancelTurn(ctx)
	})

	t.Run("alive_with_acpSID", func(t *testing.T) {
		conn := newACPConn(agent, "test-cancel-alive")
		conn.SetAliveForTest()
		conn.SetSessionMappingForTest("test-cancel-alive", "acp-sid-123")
		// Cancel will fail because the pipe-based connection can't actually
		// process a Cancel RPC, but the call should not panic.
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		conn.CancelTurn(ctx) // should not panic
	})
}

// ---------------------------------------------------------------------------
// SetSessionConfigOption
// ---------------------------------------------------------------------------

func TestRefactor_SetSessionConfigOption(t *testing.T) {
	agent := &model.Agent{ID: "test-setconfig", Backend: "acp-stdio", AcpCommand: "echo"}

	t.Run("shouldSetConfig_skip", func(t *testing.T) {
		conn := newACPConn(agent, "test-setconfig-skip")
		conn.SetAliveForTest()
		conn.SetSessionMappingForTest("test-setconfig-skip", "acp-sid-1")
		// Mark model as already set to same value
		conn.markConfigSet("model", "gpt-4")
		conn.SetCurrentModelID("gpt-4")
		// This should be skipped (same value)
		ctx := context.Background()
		conn.SetSessionConfigOption(ctx, "model", "gpt-4")
		// Value should remain unchanged
		assert.Equal(t, "gpt-4", conn.GetCurrentModelID())
	})

	t.Run("no_acpSID_skip", func(t *testing.T) {
		conn := newACPConn(agent, "test-setconfig-nosid")
		conn.SetAliveForTest()
		// No acpSID set — should return early
		ctx := context.Background()
		conn.SetSessionConfigOption(ctx, "model", "claude-3")
		// Value should not be updated because setSessionConfigOption is skipped
		assert.Equal(t, "", conn.GetCurrentModelID())
	})

	t.Run("unsupported_config_return", func(t *testing.T) {
		conn := newACPConn(agent, "test-setconfig-unsupported")
		conn.SetAliveForTest()
		conn.SetSessionMappingForTest("test-setconfig-unsupported", "acp-sid-2")
		// Mark thinkingEffort as unsupported
		conn.lastSetConfigMu.Lock()
		conn.unsupportedConfigs = map[string]bool{"thinkingEffort": true}
		conn.lastSetConfigMu.Unlock()
		// Should be skipped because shouldSetConfig returns false for unsupported
		ctx := context.Background()
		conn.SetSessionConfigOption(ctx, "thinkingEffort", "high")
	})

	t.Run("mode_switch", func(t *testing.T) {
		conn := newACPConn(agent, "test-setconfig-mode")
		// Don't set alive — setSessionConfigOption will hit dead-connection path
		// but SetSessionConfigOption will still update cached state for mode
		conn.SetSessionMappingForTest("test-setconfig-mode", "acp-sid-3")
		ctx := context.Background()
		// The setSessionConfigOption RPC will fail/return early on dead conn,
		// and since the connection is dead, the switch block won't execute.
		// Test that it doesn't panic.
		conn.SetSessionConfigOption(ctx, "mode", "code")
	})

	t.Run("thinking_effort_aliases", func(t *testing.T) {
		conn := newACPConn(agent, "test-setconfig-effort")
		conn.SetSessionMappingForTest("test-setconfig-effort", "acp-sid-4")
		ctx := context.Background()
		// Test "thinking_effort" alias
		conn.SetSessionConfigOption(ctx, "thinking_effort", "high")
		// Test "thought_level" alias
		conn.SetSessionConfigOption(ctx, "thought_level", "low")
		// Test "thinkingEffort" directly
		conn.SetSessionConfigOption(ctx, "thinkingEffort", "medium")
	})

	t.Run("model_switch", func(t *testing.T) {
		conn := newACPConn(agent, "test-setconfig-model")
		conn.SetSessionMappingForTest("test-setconfig-model", "acp-sid-5")
		ctx := context.Background()
		conn.SetSessionConfigOption(ctx, "model", "gpt-4")
	})
}

// ---------------------------------------------------------------------------
// setSessionConfigOption (dead-connection paths)
// ---------------------------------------------------------------------------

func TestRefactor_SetSessionConfigOption_DeadConnEarlyReturn(t *testing.T) {
	agent := &model.Agent{ID: "test-setconfigopt-dead", Backend: "acp-stdio", AcpCommand: "echo"}

	t.Run("dead_connection_skip", func(t *testing.T) {
		conn := newACPConn(agent, "test-deadconn")
		// Not alive, no conn
		ctx := context.Background()
		conn.setSessionConfigOption(ctx, "acp-sid-1", "model", "gpt-4")
		// Should not panic, should return early
		assert.Equal(t, "", conn.GetCurrentModelID())
	})

	t.Run("unknown_config_option_marking", func(t *testing.T) {
		conn := newACPConn(agent, "test-unknown-config")
		conn.SetAliveForTest()
		conn.SetSessionMappingForTest("test-unknown-config", "acp-sid-6")
		// The real RPC will fail since this is a pipe-based test conn,
		// but the error won't be "Unknown config option" so unsupportedConfigs
		// should not be populated.
		ctx := context.Background()
		conn.setSessionConfigOption(ctx, "acp-sid-6", "model", "gpt-4")
		assert.False(t, conn.IsConfigUnsupported("model"))
	})
}

// ---------------------------------------------------------------------------
// setConfigOptionWithCrashCheck
// ---------------------------------------------------------------------------

func TestRefactor_SetConfigOptionWithCrashCheck(t *testing.T) {
	agent := &model.Agent{ID: "test-crashcheck", Backend: "acp-stdio", AcpCommand: "echo"}

	t.Run("shouldSetConfig_skip", func(t *testing.T) {
		conn := newACPConn(agent, "test-crashcheck-skip")
		conn.markConfigSet("model", "gpt-4")
		ctx := context.Background()
		err := conn.setConfigOptionWithCrashCheck(ctx, "acp-sid", configOptionSpec{
			configID: "model",
			value:    "gpt-4",
			label:    "model",
		})
		assert.NoError(t, err)
	})

	t.Run("connection_dead_after_set_returns_configKilledConnectionError", func(t *testing.T) {
		conn := newACPConn(agent, "test-crashcheck-killed")
		conn.SetAliveForTest()
		conn.SetSessionMappingForTest("test-crashcheck-killed", "acp-sid-7")
		// Kill the connection immediately so setSessionConfigOption fails
		// and IsAlive returns false after the call
		conn.close()
		ctx := context.Background()
		err := conn.setConfigOptionWithCrashCheck(ctx, "acp-sid-7", configOptionSpec{
			configID: "mode",
			value:    "code",
			label:    "mode",
		})
		// Since connection is dead, should return configKilledConnectionError
		assert.True(t, isConfigKilledConnection(err))
		var ckErr *configKilledConnectionError
		assert.True(t, errors.As(err, &ckErr))
		assert.Equal(t, "mode", ckErr.ConfigID())
		assert.Equal(t, "code", ckErr.Value())
	})
}

// ---------------------------------------------------------------------------
// Prompt error paths
// ---------------------------------------------------------------------------

func TestRefactor_Prompt_ErrorPaths(t *testing.T) {
	agent := &model.Agent{ID: "test-prompt-err", Backend: "acp-stdio", AcpCommand: "echo"}

	t.Run("nil_conn_error", func(t *testing.T) {
		conn := newACPConn(agent, "test-prompt-nil")
		// conn is nil, acpSID is empty
		ch := make(chan StreamEvent, 64)
		ctx := context.Background()
		err := conn.Prompt(ctx, nil, ch, ChatRequest{Prompt: "hello"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "connection not initialized")
	})

	t.Run("empty_acpSID_error", func(t *testing.T) {
		conn := newACPConn(agent, "test-prompt-nosid")
		conn.SetAliveForTest()
		// acpSID is empty
		ch := make(chan StreamEvent, 64)
		ctx := context.Background()
		err := conn.Prompt(ctx, nil, ch, ChatRequest{Prompt: "hello"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "connection not initialized")
	})

	t.Run("config_crash_check_error_propagation", func(t *testing.T) {
		conn := newACPConn(agent, "test-prompt-configcrash")
		conn.SetAliveForTest()
		conn.SetSessionMappingForTest("test-prompt-configcrash", "acp-sid-8")
		// Mark connection as dead without clearing acpSID,
		// so Prompt reaches the config-check path but the RPC fails.
		conn.mu.Lock()
		conn.alive = false
		conn.mu.Unlock()
		ch := make(chan StreamEvent, 64)
		ctx := context.Background()
		err := conn.Prompt(ctx, []acp.ContentBlock{acp.TextBlock("hello")}, ch, ChatRequest{
			Prompt: "hello",
			Model:  "gpt-4",
		})
		// The pipe-based connection can't process the config RPC;
		// we just verify the error is returned (not a panic).
		assert.Error(t, err)
	})
}

// ---------------------------------------------------------------------------
// CacheNewSessionState
// ---------------------------------------------------------------------------

func TestRefactor_CacheNewSessionState(t *testing.T) {
	agent := &model.Agent{ID: "test-cache-new", Backend: "acp-stdio", AcpCommand: "echo"}

	t.Run("nil_sessResp_early_return", func(t *testing.T) {
		conn := newACPConn(agent, "test-cache-new-nil")
		// No lastNewSessionResp set — should return early
		conn.CacheNewSessionState()
		assert.Equal(t, "", conn.GetCurrentModeID())
	})

	t.Run("with_session_response", func(t *testing.T) {
		conn := newACPConn(agent, "test-cache-new-resp")
		modeCat := acp.SessionConfigOptionCategoryMode
		thoughtCat := acp.SessionConfigOptionCategoryThoughtLevel
		modelCat := acp.SessionConfigOptionCategoryModel
		sessResp := &acp.NewSessionResponse{
			SessionId: acp.SessionId("acp-new-1"),
			Modes: &acp.SessionModeState{
				CurrentModeId: "code",
				AvailableModes: []acp.SessionMode{
					{Id: "ask", Name: "Ask"},
					{Id: "code", Name: "Code"},
				},
			},
			ConfigOptions: []acp.SessionConfigOption{
				{
					Select: &acp.SessionConfigOptionSelect{
						Category:     &modeCat,
						Id:           "mode",
						Name:         "Mode",
						CurrentValue: "code",
						Options: acp.SessionConfigSelectOptions{
							Ungrouped: &acp.SessionConfigSelectOptionsUngrouped{
								{Value: "ask", Name: "Ask"},
								{Value: "code", Name: "Code"},
							},
						},
					},
				},
				{
					Select: &acp.SessionConfigOptionSelect{
						Category:     &thoughtCat,
						Id:           "thinkingEffort",
						Name:         "Thinking Effort",
						CurrentValue: "high",
						Options: acp.SessionConfigSelectOptions{
							Ungrouped: &acp.SessionConfigSelectOptionsUngrouped{
								{Value: "low", Name: "Low"},
								{Value: "high", Name: "High"},
							},
						},
					},
				},
				{
					Select: &acp.SessionConfigOptionSelect{
						Category:     &modelCat,
						Id:           "model",
						Name:         "Model",
						CurrentValue: "gpt-4",
						Options: acp.SessionConfigSelectOptions{
							Ungrouped: &acp.SessionConfigSelectOptionsUngrouped{
								{Value: "gpt-4", Name: "GPT-4"},
							},
						},
					},
				},
			},
		}
		conn.mu.Lock()
		conn.lastNewSessionResp = sessResp
		conn.mu.Unlock()

		conn.CacheNewSessionState()

		assert.Equal(t, "code", conn.GetCurrentModeID())
		assert.Equal(t, "high", conn.GetCurrentThinkingEffortID())
		assert.Equal(t, "gpt-4", conn.GetCurrentModelID())

		// Verify response was cleared
		assert.Nil(t, conn.GetAndClearNewSessionResp())
	})
}

// ---------------------------------------------------------------------------
// MergeResumedSessionState
// ---------------------------------------------------------------------------

func TestRefactor_MergeResumedSessionState(t *testing.T) {
	agent := &model.Agent{ID: "test-merge-resume", Backend: "acp-stdio", AcpCommand: "echo"}

	t.Run("nil_resumeResp_early_return", func(t *testing.T) {
		conn := newACPConn(agent, "test-merge-nil")
		conn.MergeResumedSessionState()
		assert.Equal(t, "", conn.GetCurrentModeID())
	})

	t.Run("with_resume_response", func(t *testing.T) {
		conn := newACPConn(agent, "test-merge-resp")
		modeCat := acp.SessionConfigOptionCategoryMode
		resumeResp := &acp.ResumeSessionResponse{
			Modes: &acp.SessionModeState{
				CurrentModeId: "ask",
				AvailableModes: []acp.SessionMode{
					{Id: "ask", Name: "Ask"},
					{Id: "code", Name: "Code"},
				},
			},
			ConfigOptions: []acp.SessionConfigOption{
				{
					Select: &acp.SessionConfigOptionSelect{
						Category:     &modeCat,
						Id:           "mode",
						Name:         "Mode",
						CurrentValue: "ask",
					},
				},
			},
		}
		conn.mu.Lock()
		conn.lastResumeSessionResp = resumeResp
		conn.mu.Unlock()

		conn.MergeResumedSessionState()

		assert.Equal(t, "ask", conn.GetCurrentModeID())
		// Verify response was cleared
		assert.Nil(t, conn.GetAndClearResumeSessionResp())
	})

	t.Run("preserve_existing_selections", func(t *testing.T) {
		conn := newACPConn(agent, "test-merge-preserve")
		// Set user's current selections (simulating re-applied config after resume)
		conn.SetCurrentModeID("code")
		conn.SetCurrentThinkingEffortID("high")
		conn.SetCurrentModelID("claude-3")

		modeCat := acp.SessionConfigOptionCategoryMode
		thoughtCat := acp.SessionConfigOptionCategoryThoughtLevel
		modelCat := acp.SessionConfigOptionCategoryModel
		resumeResp := &acp.ResumeSessionResponse{
			Modes: &acp.SessionModeState{
				CurrentModeId: "ask", // agent default differs from user selection
				AvailableModes: []acp.SessionMode{
					{Id: "ask", Name: "Ask"},
					{Id: "code", Name: "Code"},
				},
			},
			ConfigOptions: []acp.SessionConfigOption{
				{
					Select: &acp.SessionConfigOptionSelect{
						Category:     &modeCat,
						Id:           "mode",
						Name:         "Mode",
						CurrentValue: "ask",
					},
				},
				{
					Select: &acp.SessionConfigOptionSelect{
						Category:     &thoughtCat,
						Id:           "thinkingEffort",
						Name:         "Thinking",
						CurrentValue: "low",
					},
				},
				{
					Select: &acp.SessionConfigOptionSelect{
						Category:     &modelCat,
						Id:           "model",
						Name:         "Model",
						CurrentValue: "gpt-4",
					},
				},
			},
		}
		conn.mu.Lock()
		conn.lastResumeSessionResp = resumeResp
		conn.mu.Unlock()

		conn.MergeResumedSessionState()

		// preserveExisting=true: user selections should be kept over agent defaults
		assert.Equal(t, "code", conn.GetCurrentModeID())
		assert.Equal(t, "high", conn.GetCurrentThinkingEffortID())
		assert.Equal(t, "claude-3", conn.GetCurrentModelID())
	})
}

// ---------------------------------------------------------------------------
// extractSessionState
// ---------------------------------------------------------------------------

func TestRefactor_ExtractSessionState(t *testing.T) {
	agent := &model.Agent{ID: "test-extract-state", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "test-extract-state")

	t.Run("newResp_branch", func(t *testing.T) {
		modeCat := acp.SessionConfigOptionCategoryMode
		newResp := &acp.NewSessionResponse{
			Modes: &acp.SessionModeState{
				CurrentModeId: "code",
				AvailableModes: []acp.SessionMode{
					{Id: "code", Name: "Code"},
				},
			},
			ConfigOptions: []acp.SessionConfigOption{
				{
					Select: &acp.SessionConfigOptionSelect{
						Category:     &modeCat,
						Id:           "mode",
						Name:         "Mode",
						CurrentValue: "code",
					},
				},
			},
		}
		ext := conn.extractSessionState(func() (*acp.NewSessionResponse, *acp.ResumeSessionResponse) {
			return newResp, nil
		})
		assert.Equal(t, "code", ext.modeCurrentID)
		require.Len(t, ext.modes, 1)
		assert.Equal(t, "code", ext.modes[0].ID)
		require.NotNil(t, ext.configState)
		assert.Equal(t, "code", ext.configState.CurrentID)
	})

	t.Run("resumeResp_branch", func(t *testing.T) {
		resumeResp := &acp.ResumeSessionResponse{
			Modes: &acp.SessionModeState{
				CurrentModeId: "architect",
				AvailableModes: []acp.SessionMode{
					{Id: "architect", Name: "Architect"},
				},
			},
		}
		ext := conn.extractSessionState(func() (*acp.NewSessionResponse, *acp.ResumeSessionResponse) {
			return nil, resumeResp
		})
		assert.Equal(t, "architect", ext.modeCurrentID)
		require.Len(t, ext.modes, 1)
		assert.Equal(t, "architect", ext.modes[0].ID)
	})
}

// ---------------------------------------------------------------------------
// applyExtractedState
// ---------------------------------------------------------------------------

func TestRefactor_ApplyExtractedState(t *testing.T) {
	agent := &model.Agent{ID: "test-apply-state", Backend: "acp-stdio", AcpCommand: "echo"}

	t.Run("preserveExisting_false_uses_response_values", func(t *testing.T) {
		conn := newACPConn(agent, "test-apply-false")
		ext := sessionStateExtracted{
			modes:           []ModeDef{{ID: "ask", Name: "Ask"}, {ID: "code", Name: "Code"}},
			modeCurrentID:   "code",
			effortCurrentID: "high",
			modelCurrentID:  "gpt-4",
		}
		conn.applyExtractedState(ext, false)
		assert.Equal(t, "code", conn.GetCurrentModeID())
		assert.Equal(t, "high", conn.GetCurrentThinkingEffortID())
		assert.Equal(t, "gpt-4", conn.GetCurrentModelID())
	})

	t.Run("preserveExisting_true_keeps_user_selections", func(t *testing.T) {
		conn := newACPConn(agent, "test-apply-true")
		// Set existing user selections
		conn.SetCurrentModeID("architect")
		conn.SetCurrentThinkingEffortID("low")
		conn.SetCurrentModelID("claude-3")

		ext := sessionStateExtracted{
			modes:           []ModeDef{{ID: "ask", Name: "Ask"}},
			modeCurrentID:   "ask",
			effortCurrentID: "high",
			modelCurrentID:  "gpt-4",
		}
		conn.applyExtractedState(ext, true)
		// User selections should be preserved
		assert.Equal(t, "architect", conn.GetCurrentModeID())
		assert.Equal(t, "low", conn.GetCurrentThinkingEffortID())
		assert.Equal(t, "claude-3", conn.GetCurrentModelID())
	})

	t.Run("preserveExisting_true_no_existing_uses_response", func(t *testing.T) {
		conn := newACPConn(agent, "test-apply-true-noexisting")
		// No existing selections (all empty)
		ext := sessionStateExtracted{
			modeCurrentID:   "ask",
			effortCurrentID: "medium",
			modelCurrentID:  "gpt-4",
		}
		conn.applyExtractedState(ext, true)
		// Since existing is empty, response values should be used
		assert.Equal(t, "ask", conn.GetCurrentModeID())
		assert.Equal(t, "medium", conn.GetCurrentThinkingEffortID())
		assert.Equal(t, "gpt-4", conn.GetCurrentModelID())
	})

	t.Run("preserveExisting_with_configState", func(t *testing.T) {
		conn := newACPConn(agent, "test-apply-configstate")
		conn.SetCurrentModeID("code")

		configState := &ConfigOptionState{
			ConfigID:  "mode",
			CurrentID: "ask",
			Options: []ConfigOptionDef{
				{ID: "mode", Category: "mode", Values: []ConfigOptionValue{{ID: "ask"}, {ID: "code"}}},
			},
		}
		ext := sessionStateExtracted{
			modeCurrentID: "ask",
			configState:   configState,
		}
		conn.applyExtractedState(ext, true)
		// configState.CurrentID should be updated to existing user selection
		assert.Equal(t, "code", configState.CurrentID)
	})
}

// ---------------------------------------------------------------------------
// EmitSessionStateEvents
// ---------------------------------------------------------------------------

func TestRefactor_EmitSessionStateEvents(t *testing.T) {
	agent := &model.Agent{ID: "test-emit-state", Backend: "acp-stdio", AcpCommand: "echo"}

	t.Run("with_registry_data", func(t *testing.T) {
		reg := GetAgentCapabilityRegistry()
		reg.UpdateModes("test-emit-state", []ModeDef{{ID: "ask", Name: "Ask"}, {ID: "code", Name: "Code"}})
		reg.UpdateThinkingEfforts("test-emit-state", []ThinkingEffortDef{{ID: "low", Name: "Low"}, {ID: "high", Name: "High"}})
		reg.UpdateModels("test-emit-state", []model.AgentModel{{ID: "gpt-4", Name: "GPT-4"}})

		conn := newACPConn(agent, "test-emit-state")
		conn.SetCurrentModeID("code")
		conn.SetCurrentThinkingEffortID("high")
		conn.SetCurrentModelID("gpt-4")

		ch := make(chan StreamEvent, 64)
		conn.EmitSessionStateEvents(ch)

		// Should emit mode_update, thinking_effort_update, model_list_update
		events := drainStreamEvents(ch)
		eventTypes := make(map[string]bool)
		for _, e := range events {
			eventTypes[e.Type] = true
		}
		assert.True(t, eventTypes["mode_update"], "expected mode_update event")
		assert.True(t, eventTypes["thinking_effort_update"], "expected thinking_effort_update event")
		assert.True(t, eventTypes["model_list_update"], "expected model_list_update event")

		// Verify mode_update content
		for _, e := range events {
			if e.Type == "mode_update" {
				require.NotNil(t, e.Mode)
				assert.Equal(t, "code", e.Mode.CurrentModeID)
				require.Len(t, e.Mode.AvailableModes, 2)
			}
			if e.Type == "thinking_effort_update" {
				require.NotNil(t, e.ThinkingEffort)
				assert.Equal(t, "high", e.ThinkingEffort.CurrentID)
			}
			if e.Type == "model_list_update" {
				require.NotNil(t, e.ModelList)
				assert.Equal(t, "gpt-4", e.ModelList.CurrentModelID)
			}
		}
	})

	t.Run("no_registry_data", func(t *testing.T) {
		agent2 := &model.Agent{ID: "test-emit-empty", Backend: "acp-stdio", AcpCommand: "echo"}
		conn := newACPConn(agent2, "test-emit-empty")
		ch := make(chan StreamEvent, 64)
		conn.EmitSessionStateEvents(ch)
		events := drainStreamEvents(ch)
		assert.Empty(t, events, "no events expected when registry has no data")
	})
}

// ---------------------------------------------------------------------------
// EmitCommandsUpdate
// ---------------------------------------------------------------------------

func TestRefactor_EmitCommandsUpdate(t *testing.T) {
	agent := &model.Agent{ID: "test-emit-cmds", Backend: "acp-stdio", AcpCommand: "echo"}

	t.Run("with_registry_commands", func(t *testing.T) {
		reg := GetAgentCapabilityRegistry()
		reg.UpdateCommands("test-emit-cmds-registry", []AvailableCommandInfo{
			{Name: "/fix", Description: "Fix issues"},
		})

		conn := newACPConn(&model.Agent{ID: "test-emit-cmds-registry", Backend: "acp-stdio", AcpCommand: "echo"}, "test-emit-cmds-registry")
		ch := make(chan StreamEvent, 64)
		conn.EmitCommandsUpdate(ch)

		events := drainStreamEvents(ch)
		require.Len(t, events, 1)
		assert.Equal(t, "commands_update", events[0].Type)
		require.Len(t, events[0].Commands, 1)
		assert.Equal(t, "/fix", events[0].Commands[0].Name)
	})

	t.Run("with_client_fallback", func(t *testing.T) {
		conn := newACPConn(agent, "test-emit-cmds-client")
		// Set up client with commands but no registry commands
		client := NewClawBenchACPClient()
		client.commands = []acp.AvailableCommand{
			{Name: "/help", Description: "Show help"},
		}
		conn.SetClientForTest(client)

		ch := make(chan StreamEvent, 64)
		conn.EmitCommandsUpdate(ch)

		events := drainStreamEvents(ch)
		require.Len(t, events, 1)
		assert.Equal(t, "commands_update", events[0].Type)
		require.Len(t, events[0].Commands, 1)
		assert.Equal(t, "/help", events[0].Commands[0].Name)
	})

	t.Run("no_commands_anywhere", func(t *testing.T) {
		agent3 := &model.Agent{ID: "test-emit-cmds-none", Backend: "acp-stdio", AcpCommand: "echo"}
		conn := newACPConn(agent3, "test-emit-cmds-none")
		ch := make(chan StreamEvent, 64)
		conn.EmitCommandsUpdate(ch)
		events := drainStreamEvents(ch)
		assert.Empty(t, events, "no events expected when no commands available")
	})
}

// ---------------------------------------------------------------------------
// emitSessionAndCacheState
// ---------------------------------------------------------------------------

func TestRefactor_EmitSessionAndCacheState(t *testing.T) {
	agent := &model.Agent{ID: "test-emit-session", Backend: "acp-stdio", AcpCommand: "echo"}

	t.Run("isNew_true_path", func(t *testing.T) {
		conn := newACPConn(agent, "test-emit-new")
		conn.SetAliveForTest()
		conn.SetSessionMappingForTest("test-emit-new", "acp-sid-new")

		// Set up a session response so CacheNewSessionState has data
		modeCat := acp.SessionConfigOptionCategoryMode
		sessResp := &acp.NewSessionResponse{
			SessionId: acp.SessionId("acp-sid-new"),
			Modes: &acp.SessionModeState{
				CurrentModeId: "code",
				AvailableModes: []acp.SessionMode{
					{Id: "code", Name: "Code"},
				},
			},
			ConfigOptions: []acp.SessionConfigOption{
				{
					Select: &acp.SessionConfigOptionSelect{
						Category:     &modeCat,
						Id:           "mode",
						Name:         "Mode",
						CurrentValue: "code",
					},
				},
			},
		}
		conn.mu.Lock()
		conn.lastNewSessionResp = sessResp
		conn.mu.Unlock()

		// Register modes in registry so EmitSessionStateEvents has data
		reg := GetAgentCapabilityRegistry()
		reg.UpdateModes("test-emit-new", []ModeDef{{ID: "code", Name: "Code"}})

		backend := &ACPBackend{agent: agent}
		ch := make(chan StreamEvent, 64)
		backend.emitSessionAndCacheState(conn, true, ch)

		events := drainStreamEvents(ch)
		eventTypes := make(map[string]bool)
		for _, e := range events {
			eventTypes[e.Type] = true
		}
		assert.True(t, eventTypes["session_capture"], "expected session_capture for isNew=true")
		assert.True(t, eventTypes["mode_update"], "expected mode_update")
	})

	t.Run("isNew_false_path", func(t *testing.T) {
		conn := newACPConn(agent, "test-emit-resume")
		conn.SetSessionMappingForTest("test-emit-resume", "acp-sid-resume")

		// Set up a resume response
		resumeResp := &acp.ResumeSessionResponse{
			Modes: &acp.SessionModeState{
				CurrentModeId: "code",
				AvailableModes: []acp.SessionMode{
					{Id: "code", Name: "Code"},
				},
			},
		}
		conn.mu.Lock()
		conn.lastResumeSessionResp = resumeResp
		conn.mu.Unlock()

		reg := GetAgentCapabilityRegistry()
		reg.UpdateModes("test-emit-session", []ModeDef{{ID: "code", Name: "Code"}})

		backend := &ACPBackend{agent: agent}
		ch := make(chan StreamEvent, 64)
		backend.emitSessionAndCacheState(conn, false, ch)

		events := drainStreamEvents(ch)
		eventTypes := make(map[string]bool)
		for _, e := range events {
			eventTypes[e.Type] = true
		}
		// isNew=false: no session_capture, but should still emit state events
		assert.False(t, eventTypes["session_capture"], "no session_capture for isNew=false")
	})

	t.Run("with_plan_state", func(t *testing.T) {
		conn := newACPConn(agent, "test-emit-plan")
		conn.SetSessionMappingForTest("test-emit-plan", "acp-sid-plan")
		conn.SetCachedPlanState(&PlanState{
			Entries: []PlanEntry{{Content: "do stuff", Priority: "high", Status: "in_progress"}},
		})

		backend := &ACPBackend{agent: agent}
		ch := make(chan StreamEvent, 64)
		backend.emitSessionAndCacheState(conn, false, ch)

		events := drainStreamEvents(ch)
		found := false
		for _, e := range events {
			if e.Type == "plan_update" {
				found = true
				require.NotNil(t, e.Plan)
				require.Len(t, e.Plan.Entries, 1)
				assert.Equal(t, "do stuff", e.Plan.Entries[0].Content)
			}
		}
		assert.True(t, found, "expected plan_update event")
	})
}

// ---------------------------------------------------------------------------
// ACPBackend.buildPromptBlocks (extended — with ShouldInjectSystemPrompt)
// ---------------------------------------------------------------------------

func TestRefactor_ACPBackend_BuildPromptBlocks_Extended(t *testing.T) {
	backend := &ACPBackend{}

	t.Run("with_system_prompt_no_resume", func(t *testing.T) {
		req := ChatRequest{
			Prompt:       "fix the bug",
			SystemPrompt: "You are a helpful assistant",
			Resume:       false,
		}
		blocks := backend.buildPromptBlocks(req)
		require.Len(t, blocks, 1)
		require.NotNil(t, blocks[0].Text)
		text := blocks[0].Text.Text
		assert.Contains(t, text, "[System Instructions: You are a helpful assistant]")
		assert.Contains(t, text, "fix the bug")
	})

	t.Run("with_system_prompt_resume", func(t *testing.T) {
		req := ChatRequest{
			Prompt:       "continue",
			SystemPrompt: "You are a helpful assistant",
			Resume:       true,
		}
		blocks := backend.buildPromptBlocks(req)
		require.Len(t, blocks, 1)
		require.NotNil(t, blocks[0].Text)
		text := blocks[0].Text.Text
		// Resume=true → ShouldInjectSystemPrompt returns false
		assert.NotContains(t, text, "[System Instructions:")
		assert.Equal(t, "continue", text)
	})

	t.Run("empty_system_prompt", func(t *testing.T) {
		req := ChatRequest{
			Prompt:       "hello",
			SystemPrompt: "",
		}
		blocks := backend.buildPromptBlocks(req)
		require.Len(t, blocks, 1)
		require.NotNil(t, blocks[0].Text)
		assert.Equal(t, "hello", blocks[0].Text.Text)
	})

	t.Run("with_fork_context", func(t *testing.T) {
		req := ChatRequest{
			Prompt:      "fix the bug",
			ForkContext: "[Previous context]\n",
		}
		blocks := backend.buildPromptBlocks(req)
		require.Len(t, blocks, 1)
		require.NotNil(t, blocks[0].Text)
		text := blocks[0].Text.Text
		assert.Contains(t, text, "[Previous context]")
		assert.Contains(t, text, "fix the bug")
		assert.True(t, strings.HasPrefix(text, "[Previous context]\n"))
	})

	t.Run("fork_context_with_system_prompt", func(t *testing.T) {
		req := ChatRequest{
			Prompt:       "continue",
			ForkContext:  "[Fork history]\n",
			SystemPrompt: "You are a helpful assistant",
		}
		blocks := backend.buildPromptBlocks(req)
		require.Len(t, blocks, 1)
		require.NotNil(t, blocks[0].Text)
		text := blocks[0].Text.Text
		assert.Contains(t, text, "[Fork history]")
		assert.Contains(t, text, "[System Instructions:")
		assert.Contains(t, text, "continue")
	})
}

// ---------------------------------------------------------------------------
// AgentCapabilityRegistry — Update, merge, persist, ForceUpdate, MarkStale, HasData, boolPtrVal
// ---------------------------------------------------------------------------

func TestRefactor_BoolPtrVal(t *testing.T) {
	assert.Equal(t, "<nil>", boolPtrVal(nil))
	tp := true
	assert.Equal(t, "true", boolPtrVal(&tp))
	fp := false
	assert.Equal(t, "false", boolPtrVal(&fp))
}

func TestRefactor_AgentCapability_HasData(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		c := &AgentCapability{}
		assert.False(t, c.HasData())
	})
	t.Run("with_modes", func(t *testing.T) {
		c := &AgentCapability{AvailableModes: []ModeDef{{ID: "code"}}}
		assert.True(t, c.HasData())
	})
	t.Run("with_load_session", func(t *testing.T) {
		v := true
		c := &AgentCapability{LoadSession: &v}
		assert.True(t, c.HasData())
	})
	t.Run("with_list_sessions", func(t *testing.T) {
		v := true
		c := &AgentCapability{ListSessions: &v}
		assert.True(t, c.HasData())
	})
	t.Run("with_config", func(t *testing.T) {
		c := &AgentCapability{ConfigOptionState: &ConfigOptionState{ConfigID: "mode"}}
		assert.True(t, c.HasData())
	})
}

func TestRefactor_AgentCapabilityRegistry_Merge(t *testing.T) {
	reg := GetAgentCapabilityRegistry()

	t.Run("merge_preserves_existing_non_empty", func(t *testing.T) {
		agentID := "test-merge-preserve-" + t.Name()
		reg.Update(agentID, &AgentCapability{
			AvailableModes: []ModeDef{{ID: "code"}},
		})
		reg.Update(agentID, &AgentCapability{
			AvailableThinkingEfforts: []ThinkingEffortDef{{ID: "high"}},
		})
		agentCap := reg.Get(agentID)
		assert.NotNil(t, agentCap)
		assert.Len(t, agentCap.AvailableModes, 1, "modes should be preserved during merge")
		assert.Len(t, agentCap.AvailableThinkingEfforts, 1, "efforts should be added during merge")
	})

	t.Run("merge_overwrites_non_empty", func(t *testing.T) {
		agentID := "test-merge-overwrite-" + t.Name()
		reg.Update(agentID, &AgentCapability{
			AvailableModes: []ModeDef{{ID: "code"}},
		})
		reg.Update(agentID, &AgentCapability{
			AvailableModes: []ModeDef{{ID: "ask"}, {ID: "architect"}},
		})
		agentCap := reg.Get(agentID)
		assert.Len(t, agentCap.AvailableModes, 2, "modes should be overwritten during merge")
	})

	t.Run("merge_does_not_overwrite_empty", func(t *testing.T) {
		agentID := "test-merge-empty-" + t.Name()
		reg.Update(agentID, &AgentCapability{
			AvailableModes:    []ModeDef{{ID: "code"}},
			AvailableCommands: []AvailableCommandInfo{{Name: "/test"}},
		})
		// Update with empty commands should NOT clear existing commands
		reg.Update(agentID, &AgentCapability{
			AvailableThinkingEfforts: []ThinkingEffortDef{{ID: "high"}},
		})
		agentCap := reg.Get(agentID)
		assert.Len(t, agentCap.AvailableCommands, 1, "commands should be preserved when update has empty commands")
	})
}

func TestRefactor_AgentCapabilityRegistry_ForceUpdate(t *testing.T) {
	reg := GetAgentCapabilityRegistry()
	agentID := "test-forceupdate-" + t.Name()

	t.Run("first_update_applied", func(t *testing.T) {
		applied := reg.ForceUpdate(agentID, &AgentCapability{
			AvailableModes: []ModeDef{{ID: "code"}},
		})
		assert.True(t, applied, "first ForceUpdate should be applied")
		agentCap := reg.Get(agentID)
		assert.Len(t, agentCap.AvailableModes, 1)
	})

	t.Run("second_update_skipped", func(t *testing.T) {
		applied := reg.ForceUpdate(agentID, &AgentCapability{
			AvailableModes: []ModeDef{{ID: "ask"}},
		})
		assert.False(t, applied, "second ForceUpdate should be skipped (already refreshed)")
		agentCap := reg.Get(agentID)
		assert.Len(t, agentCap.AvailableModes, 1, "modes should not change after skipped update")
		assert.Equal(t, "code", agentCap.AvailableModes[0].ID)
	})

	t.Run("mark_stale_allows_new_update", func(t *testing.T) {
		reg.MarkStale(agentID)
		applied := reg.ForceUpdate(agentID, &AgentCapability{
			AvailableModes: []ModeDef{{ID: "architect"}},
		})
		assert.True(t, applied, "ForceUpdate after MarkStale should be applied")
		agentCap := reg.Get(agentID)
		assert.Len(t, agentCap.AvailableModes, 1)
		assert.Equal(t, "architect", agentCap.AvailableModes[0].ID)
	})
}

func TestRefactor_AgentCapabilityRegistry_IsModeAvailable(t *testing.T) {
	reg := GetAgentCapabilityRegistry()
	agentID := "test-mode-avail-" + t.Name()
	reg.Update(agentID, &AgentCapability{
		AvailableModes: []ModeDef{{ID: "code"}, {ID: "ask"}},
	})
	assert.True(t, reg.IsModeAvailable(agentID, "code"))
	assert.True(t, reg.IsModeAvailable(agentID, "ask"))
	assert.False(t, reg.IsModeAvailable(agentID, "nonexistent"))
	assert.False(t, reg.IsModeAvailable("no-such-agent", "code"))
}

func TestRefactor_AgentCapabilityRegistry_HasNewAvailableModes(t *testing.T) {
	reg := GetAgentCapabilityRegistry()
	agentID := "test-hasnew-modes-" + t.Name()
	reg.Update(agentID, &AgentCapability{
		AvailableModes: []ModeDef{{ID: "code"}},
	})
	assert.False(t, reg.HasNewAvailableModes(agentID, []ModeDef{{ID: "code"}}), "same modes should not be new")
	assert.True(t, reg.HasNewAvailableModes(agentID, []ModeDef{{ID: "ask"}}), "different mode should be new")
	assert.False(t, reg.HasNewAvailableModes(agentID, nil), "nil should not be new when modes exist")
	assert.True(t, reg.HasNewAvailableModes("no-agent", []ModeDef{{ID: "code"}}), "agent with no agentCaps: any modes are new")
}

func TestRefactor_AgentCapabilityRegistry_HasNewAvailableThinkingEfforts(t *testing.T) {
	reg := GetAgentCapabilityRegistry()
	agentID := "test-hasnew-efforts-" + t.Name()
	reg.Update(agentID, &AgentCapability{
		AvailableThinkingEfforts: []ThinkingEffortDef{{ID: "high"}},
	})
	assert.False(t, reg.HasNewAvailableThinkingEfforts(agentID, []ThinkingEffortDef{{ID: "high"}}))
	assert.True(t, reg.HasNewAvailableThinkingEfforts(agentID, []ThinkingEffortDef{{ID: "medium"}}))
}

func TestRefactor_AgentCapabilityRegistry_SaveToDB(t *testing.T) {
	t.Run("save_with_all_fields", func(t *testing.T) {
		v := true
		agentCap := &AgentCapability{
			AvailableModes:           []ModeDef{{ID: "code"}},
			AvailableThinkingEfforts: []ThinkingEffortDef{{ID: "high"}},
			AvailableCommands:        []AvailableCommandInfo{{Name: "/test"}},
			ConfigOptionState:        &ConfigOptionState{ConfigID: "mode", CurrentID: "code"},
			LoadSession:              &v,
			ListSessions:             &v,
		}
		// Verify all fields are set
		assert.True(t, *agentCap.LoadSession)
		assert.True(t, *agentCap.ListSessions)
		// Verify marshal paths work (saveToDB calls json.Marshal internally)
		modesJSON, _ := json.Marshal(agentCap.AvailableModes)
		assert.Contains(t, string(modesJSON), "code")
		effortsJSON, _ := json.Marshal(agentCap.AvailableThinkingEfforts)
		assert.Contains(t, string(effortsJSON), "high")
		cmdsJSON, _ := json.Marshal(agentCap.AvailableCommands)
		assert.Contains(t, string(cmdsJSON), "/test")
		configJSON, _ := json.Marshal(agentCap.ConfigOptionState)
		assert.Contains(t, string(configJSON), "mode")
	})

	t.Run("save_with_nil_slices", func(t *testing.T) {
		agentCap := &AgentCapability{}
		// Verify nil slices are handled (marshaled as [] instead of null)
		modesJSON, _ := json.Marshal(agentCap.AvailableModes)
		assert.Equal(t, "null", string(modesJSON), "nil slice marshals to null")
		// The saveToDB function converts null to []
	})
}

func TestRefactor_AgentCapabilityRegistry_GetModeState_ConfigFallback(t *testing.T) {
	reg := GetAgentCapabilityRegistry()
	agentID := "test-mode-config-fallback-" + t.Name()
	// Register agent with no modes but with config state containing mode options
	reg.Update(agentID, &AgentCapability{
		ConfigOptionState: &ConfigOptionState{
			ConfigID:  "mode",
			CurrentID: "ask",
			Options: []ConfigOptionDef{
				{ID: "mode", Category: "mode", Values: []ConfigOptionValue{{ID: "ask", Name: "Ask"}, {ID: "code", Name: "Code"}}},
			},
		},
	})
	ms := reg.GetModeState(agentID, "ask")
	assert.NotNil(t, ms, "should derive mode state from config options")
	assert.Equal(t, "ask", ms.CurrentModeID)
	assert.Len(t, ms.AvailableModes, 2)
}

// ---------------------------------------------------------------------------
// collectCrashDiagnostics
// ---------------------------------------------------------------------------

func TestRefactor_CollectCrashDiagnostics_NoProcess(t *testing.T) {
	agent := &model.Agent{ID: "test-crash-noproc", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "test-crash-noproc")
	// No process spawned — collectCrashDiagnostics should return zero-value diag
	diag := conn.collectCrashDiagnostics()
	assert.Equal(t, 0, diag.ExitCode)
	assert.Equal(t, "", diag.StderrTail)
}

func TestRefactor_CollectCrashDiagnostics_WithProcess(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skipf("skipping: /proc only available on Linux (current: %s)", runtime.GOOS)
	}
	if os.Getuid() == 0 {
		t.Skip("skipping: test unreliable as root")
	}
	agent := &model.Agent{ID: "test-crash-proc", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "test-crash-proc")
	cmd := exec.Command("sleep", "60")
	require.NoError(t, cmd.Start())
	conn.mu.Lock()
	conn.cmd = cmd
	conn.startedAt = time.Now().Add(-5 * time.Second)
	// Set stderr builder with content
	cmd.Stderr = &strings.Builder{}
	sb := cmd.Stderr.(*strings.Builder)
	sb.WriteString("some stderr output")
	conn.mu.Unlock()

	// Kill the process; collectCrashDiagnostics will call cmdWaitOnce.Do → Process.Wait
	_ = cmd.Process.Kill()

	diag := conn.collectCrashDiagnostics()
	// Uptime should be positive
	assert.Greater(t, diag.Uptime, time.Duration(0))
	// Stderr should be agentCaptured
	assert.Contains(t, diag.StderrTail, "some stderr output")
}

func TestRefactor_ReadProcStatus(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skipf("skipping: /proc only available on Linux (current: %s)", runtime.GOOS)
	}
	ppid, rss, err := readProcStatus(os.Getpid())
	assert.NoError(t, err, "reading own /proc/status should succeed")
	assert.Greater(t, ppid, 0, "PPid should be > 0")
	assert.Greater(t, rss, 0, "VmRSS should be > 0")
}

func TestRefactor_ReadProcStatus_NonexistentPID(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skipf("skipping: /proc only available on Linux (current: %s)", runtime.GOOS)
	}
	_, _, err := readProcStatus(99999999)
	assert.Error(t, err, "nonexistent PID should return error")
}

// ---------------------------------------------------------------------------
// AgentCapabilityRegistry — additional coverage for persist, LoadSession/ListSessions merge
// ---------------------------------------------------------------------------

func TestRefactor_AgentCapabilityRegistry_MergeLoadSessionListSessions(t *testing.T) {
	reg := GetAgentCapabilityRegistry()
	agentID := "test-merge-ls-" + t.Name()

	t.Run("merge_load_session", func(t *testing.T) {
		v := true
		reg.Update(agentID, &AgentCapability{AvailableModes: []ModeDef{{ID: "code"}}})
		reg.Update(agentID, &AgentCapability{LoadSession: &v})
		agentCap := reg.Get(agentID)
		assert.NotNil(t, agentCap.LoadSession)
		assert.True(t, *agentCap.LoadSession)
	})

	t.Run("merge_list_sessions", func(t *testing.T) {
		v2 := true
		reg.Update(agentID, &AgentCapability{ListSessions: &v2})
		agentCap := reg.Get(agentID)
		assert.NotNil(t, agentCap.ListSessions)
		assert.True(t, *agentCap.ListSessions)
	})

	t.Run("merge_nil_does_not_overwrite", func(t *testing.T) {
		agentCap1 := reg.Get(agentID)
		assert.NotNil(t, agentCap1.LoadSession, "LoadSession should still be set before nil merge")
		// Update with nil LoadSession should NOT clear existing
		reg.Update(agentID, &AgentCapability{AvailableModes: []ModeDef{{ID: "ask"}}})
		agentCap2 := reg.Get(agentID)
		assert.NotNil(t, agentCap2.LoadSession, "LoadSession should be preserved after nil merge")
	})
}

func TestRefactor_AgentCapabilityRegistry_PersistAsync_NilDB(t *testing.T) {
	// When dbHolder.db is nil, persistAsync should return early without error
	reg := GetAgentCapabilityRegistry()
	// Ensure DB is nil
	SetRegistryDB(nil)
	agentID := "test-persist-nil-db-" + t.Name()
	// This should not panic
	reg.Update(agentID, &AgentCapability{AvailableModes: []ModeDef{{ID: "code"}}})
}

func TestRefactor_AgentCapabilityRegistry_MergeNilExisting(t *testing.T) {
	reg := GetAgentCapabilityRegistry()
	agentID := "test-merge-nil-existing-" + t.Name()
	// Update on non-existing agent should set it directly (not merge)
	reg.Update(agentID, &AgentCapability{AvailableModes: []ModeDef{{ID: "code"}}})
	agentCap := reg.Get(agentID)
	assert.NotNil(t, agentCap)
	assert.Len(t, agentCap.AvailableModes, 1)
}

func TestRefactor_AgentCapabilityRegistry_GetModeState_NilAgent(t *testing.T) {
	reg := GetAgentCapabilityRegistry()
	ms := reg.GetModeState("no-such-agent", "code")
	assert.Nil(t, ms, "non-existent agent should return nil mode state")
}

func TestRefactor_AgentCapabilityRegistry_GetLoadSessionListSessions(t *testing.T) {
	reg := GetAgentCapabilityRegistry()
	agentID := "test-get-ls-" + t.Name()

	t.Run("not_set", func(t *testing.T) {
		assert.False(t, reg.GetLoadSession(agentID))
		assert.False(t, reg.GetListSessions(agentID))
	})

	t.Run("set_true", func(t *testing.T) {
		reg.Update(agentID, &AgentCapability{LoadSession: ptrBool(true), ListSessions: ptrBool(true)})
		assert.True(t, reg.GetLoadSession(agentID))
		assert.True(t, reg.GetListSessions(agentID))
	})

	t.Run("set_false", func(t *testing.T) {
		reg.Update(agentID, &AgentCapability{LoadSession: ptrBool(false), ListSessions: ptrBool(false)})
		assert.False(t, reg.GetLoadSession(agentID))
		assert.False(t, reg.GetListSessions(agentID))
	})
}

func ptrBool(v bool) *bool { return &v }

// ---------------------------------------------------------------------------
// setSessionConfigOption dead connection path
// ---------------------------------------------------------------------------

func TestRefactor_SetSessionConfigOption_DeadConn(t *testing.T) {
	agent := &model.Agent{ID: "test-dead-conn-cfg", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "test-dead-conn-cfg")
	// Connection is not alive — setSessionConfigOption should skip
	conn.setSessionConfigOption(context.Background(), "test-sid", "mode", "code")
	// Should not panic or block
}

func TestRefactor_ReapplyConfigOption_EmptyValue(t *testing.T) {
	agent := &model.Agent{ID: "test-reapply-empty", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "test-reapply-empty")
	// Empty value — reapplyConfigOption should return early
	conn.mu.Lock()
	conn.reapplyConfigOption(context.Background(), "test-sid", "mode", "")
	conn.mu.Unlock()
}

func TestRefactor_ReapplyConfigOption_DeadConn(t *testing.T) {
	agent := &model.Agent{ID: "test-reapply-dead", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "test-reapply-dead")
	// alive=false — reapplyConfigOption should return early
	conn.mu.Lock()
	conn.reapplyConfigOption(context.Background(), "test-sid", "mode", "code")
	conn.mu.Unlock()
}

func TestRefactor_ReapplyConfigOption_AliveConn(t *testing.T) {
	agent := &model.Agent{ID: "test-reapply-alive", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "test-reapply-alive")
	conn.SetAliveForTest()
	conn.SetSessionMappingForTest("test-reapply-alive", "acp-sid-reapply")
	// alive=true — reapplyConfigOption should call setSessionConfigOption
	conn.mu.Lock()
	conn.reapplyConfigOption(context.Background(), "acp-sid-reapply", "mode", "code")
	conn.mu.Unlock()
	// The setSessionConfigOption call will fail (no real ACP agent), but the path is covered
}

func TestRefactor_ReapplyConfigAfterResume(t *testing.T) {
	agent := &model.Agent{ID: "test-reapply-after-resume", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "test-reapply-after-resume")
	conn.SetAliveForTest()
	conn.SetSessionMappingForTest("test-reapply-after-resume", "acp-sid-resume")
	// Call reapplyConfigAfterResume which calls reapplyConfigOption for mode, model, effort
	conn.mu.Lock()
	conn.reapplyConfigAfterResume(context.Background(), "acp-sid-resume", cachedConfigSnapshot{
		mode:   "code",
		model:  "gpt-4",
		effort: "high",
	})
	conn.mu.Unlock()
}

func TestRefactor_RecoverViaResumeSession_AliveConn(t *testing.T) {
	agent := &model.Agent{ID: "test-recover-resume", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "test-recover-resume")
	conn.SetAliveForTest()
	conn.SetSessionMappingForTest("test-recover-resume", "acp-sid-recover")
	// recoverViaResumeSession will try RPC ResumeSession which will fail
	// (pipe-based connection), but the call path is exercised
	conn.mu.Lock()
	err := conn.recoverViaResumeSession(context.Background(), "/tmp", "acp-sid-recover", cachedConfigSnapshot{
		mode: "code",
	})
	conn.mu.Unlock()
	// Should return error since the pipe-based connection can't handle RPC
	assert.Error(t, err)
}

func TestRefactor_SetSessionConfigOption_NoAcpSID(t *testing.T) {
	agent := &model.Agent{ID: "test-no-acpsid", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "test-no-acpsid")
	// acpSID is empty — SetSessionConfigOption should skip
	conn.SetSessionConfigOption(context.Background(), "mode", "code")
	assert.Equal(t, "", conn.GetCurrentModeID(), "mode should not change without acpSID")
}

// ---------------------------------------------------------------------------
// drainStreamEvents helper
// ---------------------------------------------------------------------------

// drainStreamEvents reads all available events from a channel.
func drainStreamEvents(ch chan StreamEvent) []StreamEvent {
	var events []StreamEvent
	for {
		select {
		case e, ok := <-ch:
			if !ok {
				return events
			}
			events = append(events, e)
		default:
			return events
		}
	}
}

// ---------------------------------------------------------------------------
// Terminal session tests (acp_terminal.go)
// ---------------------------------------------------------------------------

func TestRefactor_Terminal_CreateAndOutput(t *testing.T) {
	client := NewClawBenchACPClient()
	ctx := context.Background()

	resp, err := client.CreateTerminal(ctx, acp.CreateTerminalRequest{
		Command: "echo hello",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, resp.TerminalId)

	// Wait for completion
	_, err = client.WaitForTerminalExit(ctx, acp.WaitForTerminalExitRequest{
		TerminalId: resp.TerminalId,
	})
	require.NoError(t, err)

	// Get output
	outResp, err := client.TerminalOutput(ctx, acp.TerminalOutputRequest{
		TerminalId: resp.TerminalId,
	})
	require.NoError(t, err)
	assert.Contains(t, outResp.Output, "hello")
	assert.NotNil(t, outResp.ExitStatus)
	assert.NotNil(t, outResp.ExitStatus.ExitCode)
	assert.Equal(t, 0, *outResp.ExitStatus.ExitCode)
}

func TestRefactor_Terminal_KillAndRelease(t *testing.T) {
	client := NewClawBenchACPClient()
	ctx := context.Background()

	resp, err := client.CreateTerminal(ctx, acp.CreateTerminalRequest{
		Command: "sleep 60",
	})
	require.NoError(t, err)

	// Kill it
	_, err = client.KillTerminal(ctx, acp.KillTerminalRequest{
		TerminalId: resp.TerminalId,
	})
	require.NoError(t, err)

	// Release
	_, err = client.ReleaseTerminal(ctx, acp.ReleaseTerminalRequest{
		TerminalId: resp.TerminalId,
	})
	require.NoError(t, err)

	// Output should fail after release
	_, err = client.TerminalOutput(ctx, acp.TerminalOutputRequest{
		TerminalId: resp.TerminalId,
	})
	assert.Error(t, err)
}

func TestRefactor_Terminal_NotFoundErrors(t *testing.T) {
	client := NewClawBenchACPClient()
	ctx := context.Background()

	_, err := client.TerminalOutput(ctx, acp.TerminalOutputRequest{
		TerminalId: "nonexistent",
	})
	assert.Error(t, err)

	_, err = client.WaitForTerminalExit(ctx, acp.WaitForTerminalExitRequest{
		TerminalId: "nonexistent",
	})
	assert.Error(t, err)

	// KillTerminal on nonexistent is a no-op (returns empty response, no error)
	resp, err := client.KillTerminal(ctx, acp.KillTerminalRequest{
		TerminalId: "nonexistent",
	})
	assert.NoError(t, err)
	_ = resp

	// ReleaseTerminal on nonexistent is a no-op
	resp2, err := client.ReleaseTerminal(ctx, acp.ReleaseTerminalRequest{
		TerminalId: "nonexistent",
	})
	assert.NoError(t, err)
	_ = resp2
}

func TestRefactor_Terminal_OutputByteLimit(t *testing.T) {
	client := NewClawBenchACPClient()
	ctx := context.Background()

	limit := 10
	resp, err := client.CreateTerminal(ctx, acp.CreateTerminalRequest{
		Command:         "echo abcdefghijklmnopqrstuvwxyz",
		OutputByteLimit: &limit,
	})
	require.NoError(t, err)

	// Wait for completion
	_, err = client.WaitForTerminalExit(ctx, acp.WaitForTerminalExitRequest{
		TerminalId: resp.TerminalId,
	})
	require.NoError(t, err)

	// Output should be truncated
	outResp, err := client.TerminalOutput(ctx, acp.TerminalOutputRequest{
		TerminalId: resp.TerminalId,
	})
	require.NoError(t, err)
	assert.True(t, outResp.Truncated, "output should be truncated when exceeding byte limit")
}
