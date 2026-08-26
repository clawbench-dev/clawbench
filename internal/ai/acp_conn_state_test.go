package ai

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
)

// ---------------------------------------------------------------------------
// extractSessionState — uncovered else branches
// ---------------------------------------------------------------------------

func TestExtractSessionState_NewResp_NoModeState(t *testing.T) {
	// Covers the else branch when extractACPModeState returns nil
	// (line 90-92: "acp: no mode from v1 Modes field, will rely on configOptions fallback")
	agent := &model.Agent{ID: "test-extract-no-mode", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "test-extract-no-mode")

	newResp := &acp.NewSessionResponse{
		// No Modes field → extractACPModeState returns nil
		ConfigOptions: []acp.SessionConfigOption{},
	}
	ext := conn.extractSessionState(func() (*acp.NewSessionResponse, *acp.ResumeSessionResponse) {
		return newResp, nil
	})
	assert.Empty(t, ext.modes)
	assert.Empty(t, ext.modeCurrentID)
}

func TestExtractSessionState_NewResp_NoConfigState(t *testing.T) {
	// Covers the else branch when extractACPConfigOptions returns nil
	// (line 96-98: "acp: no mode config from configOptions")
	agent := &model.Agent{ID: "test-extract-no-config", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "test-extract-no-config")

	thoughtCat := acp.SessionConfigOptionCategoryThoughtLevel
	newResp := &acp.NewSessionResponse{
		// No mode category in ConfigOptions → extractACPConfigOptions returns nil
		ConfigOptions: []acp.SessionConfigOption{
			{
				Select: &acp.SessionConfigOptionSelect{
					Category:     &thoughtCat,
					Id:           "thinkingEffort",
					Name:         "Thinking",
					CurrentValue: "high",
				},
			},
		},
	}
	ext := conn.extractSessionState(func() (*acp.NewSessionResponse, *acp.ResumeSessionResponse) {
		return newResp, nil
	})
	assert.Nil(t, ext.configState)
}

func TestExtractSessionState_ResumeResp_NoModeState(t *testing.T) {
	// Covers the else branch when extractACPModeStateFromResume returns nil
	// (line 118-120: "acp: no mode from resumed v1 Modes field")
	agent := &model.Agent{ID: "test-extract-resume-no-mode", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "test-extract-resume-no-mode")

	resumeResp := &acp.ResumeSessionResponse{
		// No Modes field → extractACPModeStateFromResume returns nil
		ConfigOptions: []acp.SessionConfigOption{},
	}
	ext := conn.extractSessionState(func() (*acp.NewSessionResponse, *acp.ResumeSessionResponse) {
		return nil, resumeResp
	})
	assert.Empty(t, ext.modes)
	assert.Empty(t, ext.modeCurrentID)
}

// ---------------------------------------------------------------------------
// applyExtractedState — cachedUsage restore branch
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// EmitCommandsUpdate — early return when no commands available
// ---------------------------------------------------------------------------

func TestEmitCommandsUpdate_NoCommandsNoClient(t *testing.T) {
	// Covers line 214-216: when len(cmds) == 0 and no client fallback → return early
	agent := &model.Agent{ID: "test-emit-nocmds-noclient", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "test-emit-nocmds-noclient")

	ch := make(chan StreamEvent, 64)
	conn.EmitCommandsUpdate(ch)

	events := drainStreamEvents(ch)
	assert.Empty(t, events, "no events expected when no commands and no client")
}

func TestEmitCommandsUpdate_ClientFallbackSource(t *testing.T) {
	// Covers line 221: the "client_fallback" return inside the slog closure.
	// This path is hit when registry has no commands for the agent but the client does,
	// and the registry's UpdateCommands hasn't been called yet (or the agent ID differs).
	agent := &model.Agent{ID: "test-emit-client-source", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "test-emit-client-source")

	// Set up client with commands — registry has no commands for this agent
	client := NewClawBenchACPClient()
	client.commands = []acp.AvailableCommand{
		{Name: "/fix", Description: "Fix issues"},
	}
	conn.SetClientForTest(client)

	ch := make(chan StreamEvent, 64)
	conn.EmitCommandsUpdate(ch)

	events := drainStreamEvents(ch)
	require.Len(t, events, 1)
	assert.Equal(t, "commands_update", events[0].Type)
	require.Len(t, events[0].Commands, 1)
	assert.Equal(t, "/fix", events[0].Commands[0].Name)
}

// ---------------------------------------------------------------------------
// CacheNewSessionState — no mode state in response (nil Modes + no mode configOptions)
// ---------------------------------------------------------------------------

func TestCacheNewSessionState_NoModeStateInResponse(t *testing.T) {
	// Covers extractSessionState newResp branch with nil mode state and nil configState
	agent := &model.Agent{ID: "test-cache-no-mode-state", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "test-cache-no-mode-state")

	sessResp := &acp.NewSessionResponse{
		SessionId: acp.SessionId("acp-no-mode"),
		// No Modes, no ConfigOptions with mode category
		ConfigOptions: []acp.SessionConfigOption{},
	}
	conn.mu.Lock()
	conn.lastNewSessionResp = sessResp
	conn.mu.Unlock()

	conn.CacheNewSessionState()

	// Mode should remain empty since no mode state was extracted
	assert.Equal(t, "", conn.GetCurrentModeID())
}

// ---------------------------------------------------------------------------
// MergeResumedSessionState — no mode state in resume response
// ---------------------------------------------------------------------------

func TestMergeResumedSessionState_NoModeStateInResponse(t *testing.T) {
	// Covers extractSessionState resumeResp branch with nil mode state
	agent := &model.Agent{ID: "test-merge-no-mode-state", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "test-merge-no-mode-state")

	resumeResp := &acp.ResumeSessionResponse{
		// No Modes, no ConfigOptions
		ConfigOptions: []acp.SessionConfigOption{},
	}
	conn.mu.Lock()
	conn.lastResumeSessionResp = resumeResp
	conn.mu.Unlock()

	conn.MergeResumedSessionState()

	// Mode should remain empty since no mode state was extracted
	assert.Equal(t, "", conn.GetCurrentModeID())
}

// ---------------------------------------------------------------------------
// extractSessionState — newResp with all sub-extractors returning non-nil
// ---------------------------------------------------------------------------

func TestExtractSessionState_NewResp_AllSubExtractorsPopulated(t *testing.T) {
	// Exercises all the "extracted" slog.Info branches (lines 89, 95, 102, 109)
	agent := &model.Agent{ID: "test-extract-all", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "test-extract-all")

	modeCat := acp.SessionConfigOptionCategoryMode
	thoughtCat := acp.SessionConfigOptionCategoryThoughtLevel
	modelCat := acp.SessionConfigOptionCategoryModel

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
					Options: acp.SessionConfigSelectOptions{
						Ungrouped: &acp.SessionConfigSelectOptionsUngrouped{
							{Value: "code", Name: "Code"},
						},
					},
				},
			},
			{
				Select: &acp.SessionConfigOptionSelect{
					Category:     &thoughtCat,
					Id:           "thinkingEffort",
					Name:         "Thinking",
					CurrentValue: "high",
					Options: acp.SessionConfigSelectOptions{
						Ungrouped: &acp.SessionConfigSelectOptionsUngrouped{
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

	ext := conn.extractSessionState(func() (*acp.NewSessionResponse, *acp.ResumeSessionResponse) {
		return newResp, nil
	})

	assert.Equal(t, "code", ext.modeCurrentID)
	require.Len(t, ext.modes, 1)
	assert.NotNil(t, ext.configState)
	assert.Equal(t, "high", ext.effortCurrentID)
	require.Len(t, ext.efforts, 1)
	assert.Equal(t, "gpt-4", ext.modelCurrentID)
	require.Len(t, ext.models, 1)
}

// ---------------------------------------------------------------------------
// extractSessionState — resumeResp with thinking effort and model list
// ---------------------------------------------------------------------------

func TestExtractSessionState_ResumeResp_WithThinkingAndModel(t *testing.T) {
	// Exercises lines 122-129 in the resumeResp branch
	agent := &model.Agent{ID: "test-extract-resume-full", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "test-extract-resume-full")

	thoughtCat := acp.SessionConfigOptionCategoryThoughtLevel
	modelCat := acp.SessionConfigOptionCategoryModel

	resumeResp := &acp.ResumeSessionResponse{
		Modes: &acp.SessionModeState{
			CurrentModeId: "code",
			AvailableModes: []acp.SessionMode{
				{Id: "code", Name: "Code"},
			},
		},
		ConfigOptions: []acp.SessionConfigOption{
			{
				Select: &acp.SessionConfigOptionSelect{
					Category:     &thoughtCat,
					Id:           "thinkingEffort",
					Name:         "Thinking",
					CurrentValue: "low",
					Options: acp.SessionConfigSelectOptions{
						Ungrouped: &acp.SessionConfigSelectOptionsUngrouped{
							{Value: "low", Name: "Low"},
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

	ext := conn.extractSessionState(func() (*acp.NewSessionResponse, *acp.ResumeSessionResponse) {
		return nil, resumeResp
	})

	assert.Equal(t, "code", ext.modeCurrentID)
	assert.Equal(t, "low", ext.effortCurrentID)
	require.Len(t, ext.efforts, 1)
	assert.Equal(t, "gpt-4", ext.modelCurrentID)
	require.Len(t, ext.models, 1)
}

// ---------------------------------------------------------------------------
// applyExtractedState — no cachedUsage (nil) branch
// ---------------------------------------------------------------------------

func TestApplyExtractedState_NoCachedUsage(t *testing.T) {
	// Covers the path where cachedUsage is nil (line 175-177 not taken)
	agent := &model.Agent{ID: "test-apply-no-usage", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "test-apply-no-usage")

	// No usage state in registry → cachedUsage is nil
	ext := sessionStateExtracted{
		modes:         []ModeDef{{ID: "code", Name: "Code"}},
		modeCurrentID: "code",
	}
	conn.applyExtractedState(ext)

	assert.Equal(t, "code", conn.GetCurrentModeID())
}

// ---------------------------------------------------------------------------
// extractSessionState — stdoutFilter fallback for SessionModelState extension
// ---------------------------------------------------------------------------

func TestExtractSessionState_NewResp_StdoutFilterFallback(t *testing.T) {
	// When extractACPModelList returns nil (no model ConfigOptions in the SDK response),
	// the stdoutFilter's cached models should be used as a fallback.
	agent := &model.Agent{ID: "test-stdout-filter-fallback", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "test-stdout-filter-fallback")

	// Pre-populate the stdoutFilter's cached models (simulates kimi ACP returning
	// models via SessionModelState extension that the SDK doesn't parse).
	filter := newACPStdoutFilter(strings.NewReader(""))
	defer filter.Close()
	filter.modelsMu.Lock()
	filter.cachedModels = &ModelListState{
		CurrentModelID: "kimi-code/k3",
		Models: []model.AgentModel{
			{ID: "kimi-code/k3", Name: "Kimi K3"},
			{ID: "kimi-code/kimi-for-coding", Name: "Kimi K2.7 Code"},
		},
	}
	filter.modelsMu.Unlock()
	conn.stdoutFilter = filter

	// NewSessionResponse with no model ConfigOptions
	newResp := &acp.NewSessionResponse{
		SessionId: acp.SessionId("acp-filter-test"),
		Modes: &acp.SessionModeState{
			CurrentModeId: "default",
			AvailableModes: []acp.SessionMode{
				{Id: "default", Name: "Default"},
			},
		},
	}

	ext := conn.extractSessionState(func() (*acp.NewSessionResponse, *acp.ResumeSessionResponse) {
		return newResp, nil
	})

	assert.Equal(t, "default", ext.modeCurrentID)
	assert.Equal(t, "kimi-code/k3", ext.modelCurrentID)
	require.Len(t, ext.models, 2)
	assert.Equal(t, "kimi-code/k3", ext.models[0].ID)
	assert.Equal(t, "Kimi K2.7 Code", ext.models[1].Name)

	// Verify the cache was cleared after reading
	filter.modelsMu.Lock()
	cached := filter.cachedModels
	filter.modelsMu.Unlock()
	assert.Nil(t, cached, "cached models should be cleared after reading")
}

func TestExtractSessionState_ResumeResp_StdoutFilterFallback(t *testing.T) {
	// Same as above but for the resume path.
	agent := &model.Agent{ID: "test-stdout-filter-resume", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "test-stdout-filter-resume")

	filter := newACPStdoutFilter(strings.NewReader(""))
	defer filter.Close()
	filter.modelsMu.Lock()
	filter.cachedModels = &ModelListState{
		CurrentModelID: "kimi-code/k3",
		Models: []model.AgentModel{
			{ID: "kimi-code/k3", Name: "Kimi K3"},
		},
	}
	filter.modelsMu.Unlock()
	conn.stdoutFilter = filter

	resumeResp := &acp.ResumeSessionResponse{
		Modes: &acp.SessionModeState{
			CurrentModeId: "default",
			AvailableModes: []acp.SessionMode{
				{Id: "default", Name: "Default"},
			},
		},
	}

	ext := conn.extractSessionState(func() (*acp.NewSessionResponse, *acp.ResumeSessionResponse) {
		return nil, resumeResp
	})

	assert.Equal(t, "kimi-code/k3", ext.modelCurrentID)
	require.Len(t, ext.models, 1)
}

func TestExtractSessionState_NewResp_ConfigOptionsTakePrecedence(t *testing.T) {
	// When both ConfigOptions and stdoutFilter have models,
	// ConfigOptions should take precedence (the SDK path is tried first).
	agent := &model.Agent{ID: "test-stdout-filter-precedence", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "test-stdout-filter-precedence")

	modelCat := acp.SessionConfigOptionCategoryModel

	// Populate stdoutFilter with different models
	filter := newACPStdoutFilter(strings.NewReader(""))
	defer filter.Close()
	filter.modelsMu.Lock()
	filter.cachedModels = &ModelListState{
		CurrentModelID: "filter-model",
		Models: []model.AgentModel{
			{ID: "filter-model", Name: "Filter Model"},
		},
	}
	filter.modelsMu.Unlock()
	conn.stdoutFilter = filter

	// ConfigOptions also has model
	newResp := &acp.NewSessionResponse{
		ConfigOptions: []acp.SessionConfigOption{
			{
				Select: &acp.SessionConfigOptionSelect{
					Category:     &modelCat,
					Id:           "model",
					Name:         "Model",
					CurrentValue: "sdk-model",
					Options: acp.SessionConfigSelectOptions{
						Ungrouped: &acp.SessionConfigSelectOptionsUngrouped{
							{Value: "sdk-model", Name: "SDK Model"},
						},
					},
				},
			},
		},
	}

	ext := conn.extractSessionState(func() (*acp.NewSessionResponse, *acp.ResumeSessionResponse) {
		return newResp, nil
	})

	// SDK ConfigOptions should take precedence
	assert.Equal(t, "sdk-model", ext.modelCurrentID)
	require.Len(t, ext.models, 1)
	assert.Equal(t, "sdk-model", ext.models[0].ID)
}

// ---------------------------------------------------------------------------
// buildPromptBlocks tests
// ---------------------------------------------------------------------------

func TestBuildPromptBlocks_PlainPrompt(t *testing.T) {
	agent := &model.Agent{ID: "test-build-prompt", Backend: "acp-stdio", AcpCommand: "echo"}
	backend, err := NewACPBackend(agent)
	require.NoError(t, err)

	req := ChatRequest{Prompt: "hello world"}
	blocks := backend.buildPromptBlocks(req)
	require.Len(t, blocks, 1)
	require.NotNil(t, blocks[0].Text)
	assert.Contains(t, blocks[0].Text.Text, "hello world")
	assert.NotContains(t, blocks[0].Text.Text, "System Instructions")
}

func TestBuildPromptBlocks_WithSystemPrompt(t *testing.T) {
	agent := &model.Agent{ID: "test-build-prompt-sys", Backend: "acp-stdio", AcpCommand: "echo"}
	backend, err := NewACPBackend(agent)
	require.NoError(t, err)

	req := ChatRequest{Prompt: "do something", SystemPrompt: "Always be helpful"}
	blocks := backend.buildPromptBlocks(req)
	require.Len(t, blocks, 1)
	require.NotNil(t, blocks[0].Text)
	assert.Contains(t, blocks[0].Text.Text, "[System Instructions: Always be helpful]")
	assert.Contains(t, blocks[0].Text.Text, "do something")
}

func TestBuildPromptBlocks_WithForkContext(t *testing.T) {
	agent := &model.Agent{ID: "test-build-prompt-fork", Backend: "acp-stdio", AcpCommand: "echo"}
	backend, err := NewACPBackend(agent)
	require.NoError(t, err)

	req := ChatRequest{
		Prompt:      "continue",
		ForkContext: "User: previous question\nAssistant: previous answer\n",
	}
	blocks := backend.buildPromptBlocks(req)
	require.Len(t, blocks, 1)
	require.NotNil(t, blocks[0].Text)
	assert.Contains(t, blocks[0].Text.Text, "previous question")
	assert.Contains(t, blocks[0].Text.Text, "continue")
}

func TestBuildPromptBlocks_WithSystemPromptAndForkContext(t *testing.T) {
	agent := &model.Agent{ID: "test-build-prompt-both", Backend: "acp-stdio", AcpCommand: "echo"}
	backend, err := NewACPBackend(agent)
	require.NoError(t, err)

	req := ChatRequest{
		Prompt:       "next step",
		ForkContext:  "history here",
		SystemPrompt: "Be concise",
	}
	blocks := backend.buildPromptBlocks(req)
	require.Len(t, blocks, 1)
	require.NotNil(t, blocks[0].Text)
	text := blocks[0].Text.Text
	// Order: fork context prepended, then system prompt prepended
	assert.Contains(t, text, "[System Instructions: Be concise]")
	assert.Contains(t, text, "history here")
	assert.Contains(t, text, "next step")
}

func TestBuildPromptBlocks_ResumeWithSystemPromptSkipped(t *testing.T) {
	// ShouldInjectSystemPrompt returns false for resume requests with
	// AssistantMessageCount > 0, so system prompt should NOT be injected
	agent := &model.Agent{ID: "test-build-prompt-resume", Backend: "acp-stdio", AcpCommand: "echo"}
	backend, err := NewACPBackend(agent)
	require.NoError(t, err)

	req := ChatRequest{
		Prompt:                "continue",
		SystemPrompt:          "Be helpful",
		Resume:                true,
		AssistantMessageCount: 1,
	}
	blocks := backend.buildPromptBlocks(req)
	require.Len(t, blocks, 1)
	require.NotNil(t, blocks[0].Text)
	assert.NotContains(t, blocks[0].Text.Text, "System Instructions")
	assert.Contains(t, blocks[0].Text.Text, "continue")
}

// ---------------------------------------------------------------------------
// EmitSessionStateEvents tests
// ---------------------------------------------------------------------------

func TestEmitSessionStateEvents_WithModeAndThinking(t *testing.T) {
	agent := &model.Agent{ID: "test-emit-state", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "test-emit-state-sid")

	// Set up registry with modes and thinking efforts
	reg := GetAgentCapabilityRegistry()
	reg.UpdateModes(agent.ID, []ModeDef{
		{ID: "code", Name: "Code"},
		{ID: "ask", Name: "Ask"},
	})
	reg.UpdateThinkingEfforts(agent.ID, []ThinkingEffortDef{
		{ID: "high", Name: "High"},
		{ID: "low", Name: "Low"},
	})
	conn.SetCurrentModeID("code")
	conn.SetCurrentThinkingEffortID("high")

	ch := make(chan StreamEvent, 64)
	conn.EmitSessionStateEvents(ch)

	events := drainStreamEvents(ch)
	// Should emit mode_update and thinking_effort_update
	eventTypes := make(map[string]bool)
	for _, e := range events {
		eventTypes[e.Type] = true
	}
	assert.True(t, eventTypes["mode_update"], "expected mode_update event")
	assert.True(t, eventTypes["thinking_effort_update"], "expected thinking_effort_update event")
}

func TestEmitSessionStateEvents_WithModelList(t *testing.T) {
	agent := &model.Agent{ID: "test-emit-model", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "test-emit-model-sid")

	reg := GetAgentCapabilityRegistry()
	reg.UpdateModels(agent.ID, []model.AgentModel{
		{ID: "claude-3.5", Name: "Claude 3.5"},
		{ID: "gpt-4o", Name: "GPT-4o"},
	})
	conn.SetCurrentModelID("claude-3.5")

	ch := make(chan StreamEvent, 64)
	conn.EmitSessionStateEvents(ch)

	events := drainStreamEvents(ch)
	eventTypes := make(map[string]bool)
	for _, e := range events {
		eventTypes[e.Type] = true
	}
	assert.True(t, eventTypes["model_list_update"], "expected model_list_update event")
}

func TestEmitSessionStateEvents_NoCapabilities(t *testing.T) {
	// When no modes/thinking/models are registered, no events should be emitted
	agent := &model.Agent{ID: "test-emit-empty", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "test-emit-empty-sid")

	ch := make(chan StreamEvent, 64)
	conn.EmitSessionStateEvents(ch)

	events := drainStreamEvents(ch)
	assert.Empty(t, events, "no events expected when no capabilities registered")
}

// ---------------------------------------------------------------------------
// CacheNewSessionState / MergeResumedSessionState — nil response
// ---------------------------------------------------------------------------

func TestCacheNewSessionState_NilResponse(t *testing.T) {
	// Covers line 35-38: early return when GetAndClearNewSessionResp returns nil
	agent := &model.Agent{ID: "test-cache-nil", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "test-cache-nil")

	// Don't set lastNewSessionResp — it's nil
	conn.CacheNewSessionState()

	// Mode should remain empty
	assert.Equal(t, "", conn.GetCurrentModeID())
}

func TestMergeResumedSessionState_NilResponse(t *testing.T) {
	// Covers line 59-62: early return when GetAndClearResumeSessionResp returns nil
	agent := &model.Agent{ID: "test-merge-nil", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "test-merge-nil")

	// Don't set lastResumeSessionResp — it's nil
	conn.MergeResumedSessionState()

	assert.Equal(t, "", conn.GetCurrentModeID())
}

// ---------------------------------------------------------------------------
// extractSessionState — newResp with no thinking effort, no model list, no stdoutFilter
// ---------------------------------------------------------------------------

func TestExtractSessionState_NewResp_NoThinkingEffort(t *testing.T) {
	// Covers line 103-105: "acp: no thinking effort from configOptions"
	agent := &model.Agent{ID: "test-no-effort", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "test-no-effort")

	modeCat := acp.SessionConfigOptionCategoryMode
	newResp := &acp.NewSessionResponse{
		Modes: &acp.SessionModeState{
			CurrentModeId:  "code",
			AvailableModes: []acp.SessionMode{{Id: "code", Name: "Code"}},
		},
		ConfigOptions: []acp.SessionConfigOption{
			{
				Select: &acp.SessionConfigOptionSelect{
					Category: &modeCat,
					Id:       "mode",
					Name:     "Mode",
				},
			},
		},
	}
	ext := conn.extractSessionState(func() (*acp.NewSessionResponse, *acp.ResumeSessionResponse) {
		return newResp, nil
	})
	assert.Empty(t, ext.effortCurrentID)
	assert.Empty(t, ext.efforts)
}

func TestExtractSessionState_NewResp_NoModelList_NoStdoutFilter(t *testing.T) {
	// Covers line 118-120: "acp: no model list from configOptions" (no stdoutFilter)
	agent := &model.Agent{ID: "test-no-model-no-filter", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "test-no-model-no-filter")
	// No stdoutFilter set

	newResp := &acp.NewSessionResponse{
		ConfigOptions: []acp.SessionConfigOption{},
	}
	ext := conn.extractSessionState(func() (*acp.NewSessionResponse, *acp.ResumeSessionResponse) {
		return newResp, nil
	})
	assert.Empty(t, ext.modelCurrentID)
	assert.Empty(t, ext.models)
}

func TestExtractSessionState_NewResp_StdoutFilterNoCache(t *testing.T) {
	// Covers line 115-117: stdoutFilter exists but GetAndClearCachedModels returns nil
	agent := &model.Agent{ID: "test-filter-no-cache", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "test-filter-no-cache")

	filter := newACPStdoutFilter(strings.NewReader(""))
	defer filter.Close()
	// Don't set cachedModels — it's nil
	conn.stdoutFilter = filter

	newResp := &acp.NewSessionResponse{
		ConfigOptions: []acp.SessionConfigOption{},
	}
	ext := conn.extractSessionState(func() (*acp.NewSessionResponse, *acp.ResumeSessionResponse) {
		return newResp, nil
	})
	assert.Empty(t, ext.modelCurrentID)
	assert.Empty(t, ext.models)
}

// ---------------------------------------------------------------------------
// applyExtractedState — preserves user's existing selections
// ---------------------------------------------------------------------------

func TestApplyExtractedState_PreservesExistingSelection(t *testing.T) {
	// When the user has already set a mode/effort/model, applyExtractedState
	// should preserve those selections over the agent's defaults.
	agent := &model.Agent{ID: "test-preserve-sel", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "test-preserve-sel")

	// Simulate user's existing selections (set by PreApply before CacheNewSessionState)
	conn.SetCurrentModeID("ask")
	conn.SetCurrentThinkingEffortID("low")
	conn.SetCurrentModelID("gpt-4o")

	ext := sessionStateExtracted{
		modeCurrentID:   "code",
		effortCurrentID: "high",
		modelCurrentID:  "claude-3.5",
		modes:           []ModeDef{{ID: "code", Name: "Code"}, {ID: "ask", Name: "Ask"}},
		efforts:         []ThinkingEffortDef{{ID: "high", Name: "High"}, {ID: "low", Name: "Low"}},
		models:          []model.AgentModel{{ID: "claude-3.5", Name: "Claude 3.5"}, {ID: "gpt-4o", Name: "GPT-4o"}},
		configState:     &ConfigOptionState{ConfigID: "mode", CurrentID: "code"},
	}
	conn.applyExtractedState(ext)

	// User's selections should be preserved
	assert.Equal(t, "ask", conn.GetCurrentModeID())
	assert.Equal(t, "low", conn.GetCurrentThinkingEffortID())
	assert.Equal(t, "gpt-4o", conn.GetCurrentModelID())
}

func TestApplyExtractedState_ConfigStateCurrentIDUpdated(t *testing.T) {
	// When user's existing mode selection differs from the agent's default,
	// configState.CurrentID should be updated to match the preserved mode.
	agent := &model.Agent{ID: "test-config-sync", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "test-config-sync")
	conn.SetCurrentModeID("ask")

	ext := sessionStateExtracted{
		modeCurrentID: "code",
		modes:         []ModeDef{{ID: "code", Name: "Code"}, {ID: "ask", Name: "Ask"}},
		configState:   &ConfigOptionState{ConfigID: "mode", CurrentID: "code"},
	}
	conn.applyExtractedState(ext)

	assert.Equal(t, "ask", conn.GetCurrentModeID())
}

// ---------------------------------------------------------------------------
// isPeerDisconnectMsg tests
// ---------------------------------------------------------------------------

func TestIsPeerDisconnectMsg_PeerDisconnected(t *testing.T) {
	assert.True(t, isPeerDisconnectMsg("peer disconnected before response"))
}

func TestIsPeerDisconnectMsg_BrokenPipe(t *testing.T) {
	assert.True(t, isPeerDisconnectMsg("write |1: broken pipe"))
}

func TestIsPeerDisconnectMsg_OtherMessage(t *testing.T) {
	assert.False(t, isPeerDisconnectMsg("timeout exceeded"))
}

func TestIsPeerDisconnectMsg_BothPatterns(t *testing.T) {
	assert.True(t, isPeerDisconnectMsg("peer disconnected and broken pipe"))
}

// ---------------------------------------------------------------------------
// isACPDeadlineMsg tests
// ---------------------------------------------------------------------------

func TestIsACPDeadlineMsg_ContextDeadlineExceeded(t *testing.T) {
	assert.True(t, isACPDeadlineMsg("context deadline exceeded"))
}

func TestIsACPDeadlineMsg_ContextDeadlineExceededInLongerMessage(t *testing.T) {
	assert.True(t, isACPDeadlineMsg("Internal error: context deadline exceeded"))
}

func TestIsACPDeadlineMsg_OtherMessage(t *testing.T) {
	assert.False(t, isACPDeadlineMsg("timeout exceeded"))
	assert.False(t, isACPDeadlineMsg("peer disconnected"))
}

// ---------------------------------------------------------------------------
// isACPPeerDisconnected tests — deadline exceeded detection
// ---------------------------------------------------------------------------

func TestIsACPPeerDisconnected_DirectDeadlineExceeded(t *testing.T) {
	assert.True(t, isACPPeerDisconnected(context.DeadlineExceeded))
}

func TestIsACPPeerDisconnected_WrappedDeadlineExceeded(t *testing.T) {
	err := fmt.Errorf("acp: session/load: %w", context.DeadlineExceeded)
	assert.True(t, isACPPeerDisconnected(err))
}

func TestIsACPPeerDisconnected_RequestErrorWithDeadlineData(t *testing.T) {
	reqErr := acp.NewInternalError(map[string]any{"error": "context deadline exceeded"})
	assert.True(t, isACPPeerDisconnected(reqErr))
}

func TestIsACPPeerDisconnected_RequestErrorWithPeerDisconnectData(t *testing.T) {
	reqErr := acp.NewInternalError(map[string]any{"error": "peer disconnected before response"})
	assert.True(t, isACPPeerDisconnected(reqErr))
}

func TestIsACPPeerDisconnected_RequestErrorWithOtherData(t *testing.T) {
	reqErr := acp.NewInternalError(map[string]any{"error": "something else"})
	assert.False(t, isACPPeerDisconnected(reqErr))
}

func TestIsACPPeerDisconnected_CancelledContext(t *testing.T) {
	// context.Canceled should NOT be treated as peer disconnect
	assert.False(t, isACPPeerDisconnected(context.Canceled))
}

// ---------------------------------------------------------------------------
// IsACPSlashCommand tests
// ---------------------------------------------------------------------------

func TestIsACPSlashCommand_ValidCommands(t *testing.T) {
	assert.True(t, IsACPSlashCommand("/reload-plugins"))
	assert.True(t, IsACPSlashCommand("/compact"))
	assert.True(t, IsACPSlashCommand("/help"))
	assert.True(t, IsACPSlashCommand("/memory"))
	assert.True(t, IsACPSlashCommand("/model"))
	assert.True(t, IsACPSlashCommand("/Reload-Plugins"))      // case-insensitive letter
	assert.True(t, IsACPSlashCommand("/reload-plugins arg1")) // with args
	assert.True(t, IsACPSlashCommand("  /compact  "))         // trimmed
}

func TestIsACPSlashCommand_InvalidCommands(t *testing.T) {
	assert.False(t, IsACPSlashCommand("hello"))        // no slash
	assert.False(t, IsACPSlashCommand("/"))            // slash only
	assert.False(t, IsACPSlashCommand("/1abc"))        // digit after slash
	assert.False(t, IsACPSlashCommand("//comment"))    // double slash
	assert.False(t, IsACPSlashCommand(""))             // empty
	assert.False(t, IsACPSlashCommand(" / not a cmd")) // slash with space
}

func TestBuildPromptBlocks_SlashCommandSkipsSystemPrompt(t *testing.T) {
	// Slash commands should NOT have system prompt prepended — ACP agents
	// detect slash commands by the leading "/" and routing depends on it.
	agent := &model.Agent{ID: "test-slash-sys", Backend: "acp-stdio", AcpCommand: "echo"}
	backend, err := NewACPBackend(agent)
	require.NoError(t, err)

	req := ChatRequest{Prompt: "/reload-plugins", SystemPrompt: "Be helpful"}
	blocks := backend.buildPromptBlocks(req)
	require.Len(t, blocks, 1)
	require.NotNil(t, blocks[0].Text)
	assert.Equal(t, "/reload-plugins", blocks[0].Text.Text)
	assert.NotContains(t, blocks[0].Text.Text, "System Instructions")
}

func TestBuildPromptBlocks_SlashCommandWithForkContext(t *testing.T) {
	// Fork context is prepended to slash commands, but the slash command
	// still needs to be at the start of the text. This is a known
	// limitation — fork context + slash command is an unlikely combination.
	agent := &model.Agent{ID: "test-slash-fork", Backend: "acp-stdio", AcpCommand: "echo"}
	backend, err := NewACPBackend(agent)
	require.NoError(t, err)

	req := ChatRequest{
		Prompt:      "/compact",
		ForkContext: "history here",
	}
	blocks := backend.buildPromptBlocks(req)
	require.Len(t, blocks, 1)
	require.NotNil(t, blocks[0].Text)
	// Fork context is prepended, so the slash command is no longer at the start
	assert.Contains(t, blocks[0].Text.Text, "history here")
	assert.Contains(t, blocks[0].Text.Text, "/compact")
}

// ---------------------------------------------------------------------------
// extractImagesFromPrompt / stripImagePathsFromTag tests
// ---------------------------------------------------------------------------

func TestImageExtOf(t *testing.T) {
	assert.Equal(t, "image/png", imageExtOf("/abs/path/pic.png"))
	assert.Equal(t, "image/jpeg", imageExtOf("/abs/path/pic.jpg"))
	assert.Equal(t, "image/jpeg", imageExtOf("/abs/path/pic.JPEG"))
	assert.Equal(t, "image/svg+xml", imageExtOf("diagram.svg"))
	assert.Equal(t, "", imageExtOf("/abs/path/readme.md"))
	assert.Equal(t, "", imageExtOf("/abs/path/noext"))
}

func TestIsLineRange(t *testing.T) {
	assert.True(t, isLineRange("10"))
	assert.True(t, isLineRange("10-20"))
	assert.True(t, isLineRange("2024")) // single line number (fileEntryLabel "path:10")
	assert.False(t, isLineRange(""))
	assert.False(t, isLineRange("abc"))
	assert.False(t, isLineRange("path.png"))
	// Malformed / non-range inputs must NOT be treated as ranges.
	assert.False(t, isLineRange("5-"))
	assert.False(t, isLineRange("5--7"))
	assert.False(t, isLineRange("10-20-30"))
	assert.False(t, isLineRange("-"))
	assert.False(t, isLineRange("a-1"))
	assert.False(t, isLineRange("10a"))
}

// writeTestImage creates a tiny real image file (any bytes) and returns its path.
func writeTestImage(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(p, []byte("fake-image-content"), 0o644))
	return p
}

func TestExtractImagesFromPrompt_CurrentFileTag(t *testing.T) {
	dir := t.TempDir()
	pic := writeTestImage(t, dir, "pic.png")
	notes := filepath.Join(dir, "notes.txt")
	require.NoError(t, os.WriteFile(notes, []byte("text"), 0o644))

	// Mixed tag: png is extracted, .txt stays in the prompt text.
	prompt := fmt.Sprintf("[Current file: %s, %s]\nlook at this", pic, notes)
	clean, images := extractImagesFromPrompt(prompt, dir, nil)
	assert.Equal(t, fmt.Sprintf("[Current file: %s]\nlook at this", notes), clean)
	require.Len(t, images, 1)
	assert.Equal(t, pic, images[0].Path)
	assert.Equal(t, "image/png", images[0].MimeType)
}

func TestExtractImagesFromPrompt_UploadedFilesTag(t *testing.T) {
	// Uploaded image is relative to workDir; all entries are images so the
	// whole tag is removed.
	proj := t.TempDir()
	a := writeTestImage(t, proj, "a.png")
	b := writeTestImage(t, proj, "b.jpg")

	prompt := "[User uploaded 2 file(s): a.png, b.jpg]\nanalyze"
	clean, images := extractImagesFromPrompt(prompt, proj, nil)
	assert.Equal(t, "analyze", strings.TrimSpace(clean))
	require.Len(t, images, 2)
	assert.Equal(t, a, images[0].Path)
	assert.Equal(t, "image/png", images[0].MimeType)
	assert.Equal(t, b, images[1].Path)
	assert.Equal(t, "image/jpeg", images[1].MimeType)
}

func TestExtractImagesFromPrompt_MixedUploadedAndNonImage(t *testing.T) {
	proj := t.TempDir()
	a := writeTestImage(t, proj, "a.png")
	csv := filepath.Join(proj, "data.csv")
	require.NoError(t, os.WriteFile(csv, []byte("c"), 0o644))

	prompt := "[User uploaded 2 file(s): a.png, data.csv]\nhi"
	clean, images := extractImagesFromPrompt(prompt, proj, nil)
	// Non-image entry remains, count updated from 2 → 1.
	assert.Equal(t, "[User uploaded 1 file(s): data.csv]\nhi", clean)
	require.Len(t, images, 1)
	assert.Equal(t, a, images[0].Path)
}

func TestExtractImagesFromPrompt_NoImagesLeavesPromptUntouched(t *testing.T) {
	prompt := "[Current file: /a/main.go]\nrefactor"
	clean, images := extractImagesFromPrompt(prompt, "", nil)
	assert.Equal(t, prompt, clean)
	assert.Len(t, images, 0)
}

func TestExtractImagesFromPrompt_NoTags(t *testing.T) {
	prompt := "just text"
	clean, images := extractImagesFromPrompt(prompt, "", nil)
	assert.Equal(t, "just text", clean)
	assert.Len(t, images, 0)
}

func TestExtractImagesFromPrompt_AppendsExtraImages(t *testing.T) {
	extra := []ImageAttachment{{Data: "base64data", MimeType: "image/png"}}
	clean, images := extractImagesFromPrompt("hello", "", extra)
	assert.Equal(t, "hello", clean)
	require.Len(t, images, 1)
	assert.Equal(t, "base64data", images[0].Data)
}

func TestExtractImagesFromPrompt_Deduplicates(t *testing.T) {
	dir := t.TempDir()
	pic := writeTestImage(t, dir, "pic.png")

	prompt := fmt.Sprintf("[Current file: %s]\n[Current file: %s]\nhi", pic, pic)
	_, images := extractImagesFromPrompt(prompt, dir, nil)
	require.Len(t, images, 1, "same image path in multiple tags should deduplicate")
}

func TestExtractImagesFromPrompt_TagWithoutTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	pic := writeTestImage(t, dir, "pic.png")

	// Tag is not followed by a newline — must still be removed entirely.
	prompt := fmt.Sprintf("[Current file: %s]hi", pic)
	clean, images := extractImagesFromPrompt(prompt, dir, nil)
	assert.Equal(t, "hi", clean)
	require.Len(t, images, 1)
	assert.Equal(t, pic, images[0].Path)
}

func TestExtractImagesFromPrompt_CurrentDirectoryTagUntouched(t *testing.T) {
	dir := t.TempDir()
	pic := writeTestImage(t, dir, "pic.png")

	// Directory tags are not image candidates and must round-trip unchanged.
	prompt := fmt.Sprintf("[Current directory: %s]\n[Current file: %s]\nhi", filepath.Join(dir, "src"), pic)
	clean, images := extractImagesFromPrompt(prompt, dir, nil)
	assert.Equal(t, fmt.Sprintf("[Current directory: %s]\nhi", filepath.Join(dir, "src")), clean)
	require.Len(t, images, 1)
}

func TestExtractImagesFromPrompt_LineRangeSuffix(t *testing.T) {
	dir := t.TempDir()
	pic := writeTestImage(t, dir, "pic.png")

	// Image path with a line-range suffix ("path:10-20") must have the suffix
	// stripped for the resolved path.
	prompt := fmt.Sprintf("[Current file: %s:10-20]\nhi", pic)
	clean, images := extractImagesFromPrompt(prompt, dir, nil)
	assert.Equal(t, "hi", clean)
	require.Len(t, images, 1)
	assert.Equal(t, pic, images[0].Path)
	assert.Equal(t, "image/png", images[0].MimeType)
}

func TestExtractImagesFromPrompt_MultipleMixedTags(t *testing.T) {
	dir := t.TempDir()
	pic := writeTestImage(t, dir, "pic.png")
	main := filepath.Join(dir, "main.go")
	require.NoError(t, os.WriteFile(main, []byte("package main"), 0o644))
	shot := writeTestImage(t, dir, "shot.jpeg")

	prompt := fmt.Sprintf("[Current file: %s, %s]\n[User uploaded 1 file(s): %s]\nanalyze", pic, main, shot)
	clean, images := extractImagesFromPrompt(prompt, dir, nil)
	assert.Equal(t, fmt.Sprintf("[Current file: %s]\nanalyze", main), clean)
	require.Len(t, images, 2)
	assert.Equal(t, pic, images[0].Path)
	assert.Equal(t, shot, images[1].Path)
}

func TestExtractImagesFromPrompt_MissingFileKeepsReference(t *testing.T) {
	// A path that does not exist cannot be inlined — the entry must stay in
	// the prompt text so the user's file reference is preserved.
	prompt := "[Current file: /nonexistent/missing.png]\nhi"
	clean, images := extractImagesFromPrompt(prompt, "", nil)
	assert.Equal(t, prompt, clean, "missing image file should leave the prompt untouched")
	assert.Len(t, images, 0)
}

func TestExtractImagesFromPrompt_OversizedFileKeepsReference(t *testing.T) {
	dir := t.TempDir()
	bigPath := filepath.Join(dir, "big.png")
	require.NoError(t, os.WriteFile(bigPath, make([]byte, maxInlineImageBytes+1), 0o644))

	prompt := fmt.Sprintf("[Current file: %s]\nhi", bigPath)
	clean, images := extractImagesFromPrompt(prompt, dir, nil)
	assert.Equal(t, prompt, clean, "oversized image file should leave the prompt untouched")
	assert.Len(t, images, 0)
}

func TestImageInlineable(t *testing.T) {
	dir := t.TempDir()
	okPath := filepath.Join(dir, "ok.png")
	require.NoError(t, os.WriteFile(okPath, []byte("x"), 0o644))
	assert.True(t, imageInlineable(okPath))
	assert.False(t, imageInlineable(filepath.Join(dir, "missing.png")))

	bigPath := filepath.Join(dir, "big.png")
	require.NoError(t, os.WriteFile(bigPath, make([]byte, maxInlineImageBytes+1), 0o644))
	assert.False(t, imageInlineable(bigPath))

	// A directory named like an image must NOT be treated as inlineable.
	dirImg := filepath.Join(dir, "assets.png")
	require.NoError(t, os.Mkdir(dirImg, 0o755))
	assert.False(t, imageInlineable(dirImg))
}

// TestExtractImagesFromPrompt_PathTraversalBlocked verifies that image paths
// escaping workDir ("..") are NOT extracted — a crafted prompt must not cause
// reading files outside the project (security boundary).
func TestExtractImagesFromPrompt_PathTraversalBlocked(t *testing.T) {
	proj := t.TempDir()
	// Secret image outside the project.
	outside := t.TempDir()
	secret := writeTestImage(t, outside, "secret.png")

	// Traversal entry pointing at the outside file.
	rel, err := filepath.Rel(proj, secret)
	require.NoError(t, err)

	prompt := fmt.Sprintf("[User uploaded 1 file(s): %s]\nhi", rel)
	clean, images := extractImagesFromPrompt(prompt, proj, nil)
	// Path stays in the text prompt, not extracted.
	assert.Equal(t, prompt, clean)
	assert.Len(t, images, 0, "path escaping workDir must not be extracted")
}

// TestExtractImagesFromPrompt_AbsolutePathOutsideWorkDirBlocked verifies that an
// absolute image path outside workDir is not extracted.
func TestExtractImagesFromPrompt_AbsolutePathOutsideWorkDirBlocked(t *testing.T) {
	proj := t.TempDir()
	outside := t.TempDir()
	secret := writeTestImage(t, outside, "secret.png")

	prompt := fmt.Sprintf("[Current file: %s]\nhi", secret)
	clean, images := extractImagesFromPrompt(prompt, proj, nil)
	assert.Equal(t, prompt, clean)
	assert.Len(t, images, 0, "absolute path outside workDir must not be extracted")
}

// TestExtractImagesFromPrompt_NoWorkDirBlocksAbsolutePath verifies that with no
// workDir, absolute image paths are not read (no containment possible).
func TestExtractImagesFromPrompt_NoWorkDirBlocksAbsolutePath(t *testing.T) {
	dir := t.TempDir()
	pic := writeTestImage(t, dir, "pic.png")

	prompt := fmt.Sprintf("[Current file: %s]\nhi", pic)
	clean, images := extractImagesFromPrompt(prompt, "", nil)
	assert.Equal(t, prompt, clean)
	assert.Len(t, images, 0, "without workDir no absolute path should be read")
}

// TestExtractImagesFromPrompt_DirectoryNamedAsImageKeptInPrompt verifies that a
// directory with an image-like name is not stripped from the prompt text.
func TestExtractImagesFromPrompt_DirectoryNamedAsImageKeptInPrompt(t *testing.T) {
	dir := t.TempDir()
	dirImg := filepath.Join(dir, "assets.png")
	require.NoError(t, os.Mkdir(dirImg, 0o755))
	notes := filepath.Join(dir, "notes.txt")
	require.NoError(t, os.WriteFile(notes, []byte("n"), 0o644))

	prompt := fmt.Sprintf("[Current file: %s, %s]\nhi", dirImg, notes)
	clean, images := extractImagesFromPrompt(prompt, dir, nil)
	// Both entries stay (dir isn't an inlineable image).
	assert.Equal(t, prompt, clean)
	assert.Len(t, images, 0)
}

// TestExtractImagesFromPrompt_FirstTagNoImages_StillScansLaterTag verifies that
// when the first same-kind tag has no images, a later same-kind tag is still
// processed (M3 regression).
func TestExtractImagesFromPrompt_FirstTagNoImages_StillScansLaterTag(t *testing.T) {
	dir := t.TempDir()
	pic := writeTestImage(t, dir, "pic.png")
	main := filepath.Join(dir, "main.go")
	require.NoError(t, os.WriteFile(main, []byte("package main"), 0o644))

	prompt := fmt.Sprintf("[Current file: %s]\n[Current file: %s]\nhi", main, pic)
	clean, images := extractImagesFromPrompt(prompt, dir, nil)
	// First tag (main.go, no image) stays; second tag (pic.png) is stripped.
	assert.Equal(t, fmt.Sprintf("[Current file: %s]\nhi", main), clean)
	require.Len(t, images, 1)
	assert.Equal(t, pic, images[0].Path)
}

// TestExtractImagesFromPrompt_EmptyPrompt verifies empty input is a no-op.
func TestExtractImagesFromPrompt_EmptyPrompt(t *testing.T) {
	clean, images := extractImagesFromPrompt("", "", nil)
	assert.Equal(t, "", clean)
	assert.Len(t, images, 0)
}

// TestImageWithinWorkDir tests the containment helper directly.
func TestImageWithinWorkDir(t *testing.T) {
	proj := t.TempDir()
	inside := writeTestImage(t, proj, "in.png")
	assert.True(t, imageWithinWorkDir(inside, proj))

	outside := t.TempDir()
	secret := writeTestImage(t, outside, "secret.png")
	assert.False(t, imageWithinWorkDir(secret, proj))

	// Traversal.
	assert.False(t, imageWithinWorkDir(filepath.Join(proj, "..", "x.png"), proj))
	// Empty workDir.
	assert.False(t, imageWithinWorkDir(inside, ""))
}

func TestBuildPromptBlocks_WithInlineImageTag_ProducesImageBlock(t *testing.T) {
	// Create a real tiny PNG so the file read path is exercised.
	dir := t.TempDir()
	pngPath := filepath.Join(dir, "pic.png")
	require.NoError(t, os.WriteFile(pngPath, []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0x00}, 0o644))

	agent := &model.Agent{ID: "test-build-prompt-imgtag", Backend: "acp-stdio", AcpCommand: "echo"}
	backend, err := NewACPBackend(agent)
	require.NoError(t, err)

	// Agent must advertise the image prompt capability for extraction to run.
	reg := resetGlobalRegistryForTest(t)
	reg.UpdatePromptImage(agent.ID, true)

	req := ChatRequest{
		Prompt:  fmt.Sprintf("[Current file: %s]\n这是什么", pngPath),
		WorkDir: dir,
	}
	blocks := backend.buildPromptBlocks(req)
	require.Len(t, blocks, 2)

	// Text block must no longer mention the image path.
	require.NotNil(t, blocks[0].Text)
	assert.NotContains(t, blocks[0].Text.Text, "pic.png")
	assert.Contains(t, blocks[0].Text.Text, "这是什么")

	// Image block carries base64-encoded content and the source path as the URI
	// so the agent can reference the original file instead of persisting a temp copy.
	require.NotNil(t, blocks[1].Image)
	assert.Equal(t, "image/png", blocks[1].Image.MimeType)
	decoded, err := base64.StdEncoding.DecodeString(blocks[1].Image.Data)
	require.NoError(t, err)
	assert.Equal(t, []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0x00}, decoded)
	require.NotNil(t, blocks[1].Image.Uri)
	absPath, aerr := filepath.Abs(pngPath)
	require.NoError(t, aerr)
	assert.Equal(t, absPath, *blocks[1].Image.Uri)
}

// TestBuildImageBlock_SetsUriPath verifies that a Path-based image gets its
// absolute path attached via the uri field (used by CodeBuddy to avoid
// persisting the base64 payload to a temp directory and presenting a duplicate
// image).
func TestBuildImageBlock_SetsUriPath(t *testing.T) {
	dir := t.TempDir()
	pngPath := writeTestImage(t, dir, "pic.png")

	block, ok := buildImageBlock(ImageAttachment{Path: pngPath, MimeType: "image/png"})
	require.True(t, ok)
	require.NotNil(t, block.Image)
	require.NotNil(t, block.Image.Uri)
	absPath, err := filepath.Abs(pngPath)
	require.NoError(t, err)
	assert.Equal(t, absPath, *block.Image.Uri)
}

// TestBuildImageBlock_DataOnly_NoUri verifies that a Data-based image
// (no local path) does not set a uri.
func TestBuildImageBlock_DataOnly_NoUri(t *testing.T) {
	block, ok := buildImageBlock(ImageAttachment{Data: "aGVsbG8=", MimeType: "image/png"})
	require.True(t, ok)
	require.NotNil(t, block.Image)
	assert.Nil(t, block.Image.Uri)
}

// TestBuildImageBlock_ExplicitURIPreserved verifies that an explicit URI wins
// over a derived path.
func TestBuildImageBlock_ExplicitURIPreserved(t *testing.T) {
	dir := t.TempDir()
	pngPath := writeTestImage(t, dir, "pic.png")

	explicit := "https://example.com/x.png"
	block, ok := buildImageBlock(ImageAttachment{Path: pngPath, MimeType: "image/png", URI: explicit})
	require.True(t, ok)
	require.NotNil(t, block.Image)
	require.NotNil(t, block.Image.Uri)
	assert.Equal(t, explicit, *block.Image.Uri)
}

func TestBuildPromptBlocks_NoImageCapability_LeavesPromptUntouched(t *testing.T) {
	// When the agent does NOT advertise the image capability, image paths stay
	// in the text prompt (the user's file reference is preserved).
	dir := t.TempDir()
	pngPath := filepath.Join(dir, "pic.png")
	require.NoError(t, os.WriteFile(pngPath, []byte("fake png"), 0o644))

	agent := &model.Agent{ID: "test-build-prompt-noimgcap", Backend: "acp-stdio", AcpCommand: "echo"}
	backend, err := NewACPBackend(agent)
	require.NoError(t, err)

	reg := resetGlobalRegistryForTest(t)
	reg.UpdatePromptImage(agent.ID, false) // agent reports no image support

	prompt := fmt.Sprintf("[Current file: %s]\n这是什么", pngPath)
	req := ChatRequest{Prompt: prompt, WorkDir: dir}
	blocks := backend.buildPromptBlocks(req)
	require.Len(t, blocks, 1)
	require.NotNil(t, blocks[0].Text)
	assert.Contains(t, blocks[0].Text.Text, "pic.png")
	assert.NotContains(t, blocks[0].Text.Text, "System Instructions")
}

func TestBuildPromptBlocks_SlashCommandSkipsImageExtraction(t *testing.T) {
	agent := &model.Agent{ID: "test-slash-imgtag", Backend: "acp-stdio", AcpCommand: "echo"}
	backend, err := NewACPBackend(agent)
	require.NoError(t, err)

	req := ChatRequest{Prompt: "/compact", Images: []ImageAttachment{{Data: "abc", MimeType: "image/png"}}}
	blocks := backend.buildPromptBlocks(req)
	require.Len(t, blocks, 1)
	require.NotNil(t, blocks[0].Text)
	assert.Equal(t, "/compact", blocks[0].Text.Text)
}

func TestBuildPromptBlocks_ExtraImagesAppended(t *testing.T) {
	agent := &model.Agent{ID: "test-build-prompt-extra", Backend: "acp-stdio", AcpCommand: "echo"}
	backend, err := NewACPBackend(agent)
	require.NoError(t, err)

	reg := resetGlobalRegistryForTest(t)
	reg.UpdatePromptImage(agent.ID, true)

	req := ChatRequest{
		Prompt: "看图",
		Images: []ImageAttachment{
			{Data: "aGVsbG8=", MimeType: "image/png"},
			{Data: "d29ybGQ=", MimeType: "image/jpeg", URI: "file:///x.jpg"},
		},
	}
	blocks := backend.buildPromptBlocks(req)
	require.Len(t, blocks, 3)
	require.NotNil(t, blocks[0].Text)
	assert.Contains(t, blocks[0].Text.Text, "看图")
	require.NotNil(t, blocks[1].Image)
	assert.Equal(t, "aGVsbG8=", blocks[1].Image.Data)
	assert.Equal(t, "image/png", blocks[1].Image.MimeType)
	require.NotNil(t, blocks[2].Image)
	assert.Equal(t, "image/jpeg", blocks[2].Image.MimeType)
	require.NotNil(t, blocks[2].Image.Uri)
	assert.Equal(t, "file:///x.jpg", *blocks[2].Image.Uri)
}

// TestBuildPromptBlocks_CumulativeImageBudgetDropsOversized verifies that when
// the cumulative base64 size of multiple images would exceed the total budget,
// later images are dropped (single-image limit is enforced separately).
func TestBuildPromptBlocks_CumulativeImageBudgetDropsOversized(t *testing.T) {
	agent := &model.Agent{ID: "test-build-prompt-budget", Backend: "acp-stdio", AcpCommand: "echo"}
	backend, err := NewACPBackend(agent)
	require.NoError(t, err)

	reg := resetGlobalRegistryForTest(t)
	reg.UpdatePromptImage(agent.ID, true)

	// One large image just under the total budget.
	big := make([]byte, (maxInlineImageBytesTotal*3)/4-1)
	req := ChatRequest{
		Prompt: "看",
		Images: []ImageAttachment{
			{Data: base64.StdEncoding.EncodeToString(big), MimeType: "image/png"},
			{Data: "c21hbGw=", MimeType: "image/png"},
		},
	}
	blocks := backend.buildPromptBlocks(req)
	require.Len(t, blocks, 2, "second image must be dropped when cumulative budget exceeded")
	require.NotNil(t, blocks[1].Image)
	// First (large) image kept; second dropped.
	assert.Equal(t, base64.StdEncoding.EncodeToString(big), blocks[1].Image.Data)
}

func TestEstimateImageSize(t *testing.T) {
	// Data-based: exact base64 length.
	sz, ok := estimateImageSize(ImageAttachment{Data: "abcd", MimeType: "image/png"})
	require.True(t, ok)
	assert.Equal(t, 4, sz)

	// Path-based: ~4/3 of file size.
	dir := t.TempDir()
	p := writeTestImage(t, dir, "pic.png")
	sz, ok = estimateImageSize(ImageAttachment{Path: p, MimeType: "image/png"})
	assert.True(t, ok)
	info, err := os.Stat(p)
	require.NoError(t, err)
	assert.Equal(t, int((info.Size()*4+2)/3), sz)

	// Missing path: not known.
	_, ok = estimateImageSize(ImageAttachment{Path: "/nonexistent/x.png", MimeType: "image/png"})
	assert.False(t, ok)

	// Empty: not known.
	_, ok = estimateImageSize(ImageAttachment{MimeType: "image/png"})
	assert.False(t, ok)
}

func TestBuildImageBlock_MissingDataOrMime(t *testing.T) {
	_, ok := buildImageBlock(ImageAttachment{Path: "", Data: "", MimeType: "image/png"})
	assert.False(t, ok)
	_, ok = buildImageBlock(ImageAttachment{Path: "", Data: "abc", MimeType: ""})
	assert.False(t, ok)
}

func TestBuildImageBlock_UnreadablePath(t *testing.T) {
	_, ok := buildImageBlock(ImageAttachment{Path: "/nonexistent/nope.png", MimeType: "image/png"})
	assert.False(t, ok)
}

func TestBuildImageBlock_OversizedFile(t *testing.T) {
	dir := t.TempDir()
	bigPath := filepath.Join(dir, "big.png")
	require.NoError(t, os.WriteFile(bigPath, make([]byte, maxInlineImageBytes+1), 0o644))
	_, ok := buildImageBlock(ImageAttachment{Path: bigPath, MimeType: "image/png"})
	assert.False(t, ok)
}

// TestEmitPromptResponseUsage_NilCachedState verifies that emitting a
// PromptResponse.Usage before any UsageUpdate notification (so cachedUsageState
// is still nil) does not panic — the regression for issue #363.
func TestEmitPromptResponseUsage_NilCachedState(t *testing.T) {
	agent := &model.Agent{ID: "test-usage-nil", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "test-usage-nil")
	require.Nil(t, conn.GetCachedUsageState())

	streamCh := make(chan StreamEvent, 8)
	cachedRead := 5
	cachedWrite := 6
	thought := 7
	usage := &acp.Usage{
		InputTokens:       10,
		OutputTokens:      20,
		TotalTokens:       30,
		CachedReadTokens:  &cachedRead,
		CachedWriteTokens: &cachedWrite,
		ThoughtTokens:     &thought,
	}

	conn.emitPromptResponseUsage(usage, streamCh)
	close(streamCh)

	var meta, usageUpdate *StreamEvent
	for ev := range streamCh {
		if ev.Type == "metadata" {
			meta = &ev
		}
		if ev.Type == "usage_update" {
			usageUpdate = &ev
		}
	}
	require.NotNil(t, meta, "metadata event should be emitted")
	require.Equal(t, 10, meta.Meta.InputTokens)
	require.Equal(t, 20, meta.Meta.OutputTokens)

	require.NotNil(t, usageUpdate, "usage_update event should be emitted")
	u := usageUpdate.Usage
	require.NotNil(t, u)
	// cachedUsageState was nil → fall back to zero values, no panic
	assert.Equal(t, 0, u.Used)
	assert.Equal(t, 0, u.Size)
	assert.Equal(t, 0.0, u.Cost)
	assert.Equal(t, "", u.Currency)
	assert.Equal(t, 10, u.InputTokens)
	assert.Equal(t, 20, u.OutputTokens)
	assert.Equal(t, 30, u.TotalTokens)
	assert.Equal(t, 5, u.CachedReadTokens)
	assert.Equal(t, 6, u.CachedWriteTokens)
	assert.Equal(t, 7, u.ThoughtTokens)

	// The state should now be cached for subsequent calls.
	assert.Equal(t, 10, conn.GetCachedUsageState().InputTokens)
}

// TestEmitPromptResponseUsage_WithCachedState verifies that when a
// cachedUsageState is already present (from a prior UsageUpdate), the
// Used/Size/Cost/Currency are preserved from it.
func TestEmitPromptResponseUsage_WithCachedState(t *testing.T) {
	agent := &model.Agent{ID: "test-usage-cached", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "test-usage-cached")
	conn.SetCachedUsageState(&UsageState{Used: 100, Size: 1000, Cost: 1.5, Currency: "USD"})

	streamCh := make(chan StreamEvent, 8)
	usage := &acp.Usage{InputTokens: 10, OutputTokens: 20, TotalTokens: 30}
	conn.emitPromptResponseUsage(usage, streamCh)
	close(streamCh)

	var usageUpdate *StreamEvent
	for ev := range streamCh {
		if ev.Type == "usage_update" {
			usageUpdate = &ev
		}
	}
	require.NotNil(t, usageUpdate)
	u := usageUpdate.Usage
	require.NotNil(t, u)
	assert.Equal(t, 100, u.Used)
	assert.Equal(t, 1000, u.Size)
	assert.Equal(t, 1.5, u.Cost)
	assert.Equal(t, "USD", u.Currency)
}

// ---------------------------------------------------------------------------
// ScheduleCommandsReEmit tests (issue #383 plugin race fix)
// ---------------------------------------------------------------------------

func TestScheduleCommandsReEmit_FiresAndEmits(t *testing.T) {
	agent := &model.Agent{ID: "test-schedule-emit", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "test-schedule-emit")

	// Pre-populate registry with commands
	GetAgentCapabilityRegistry().UpdateCommands(agent.ID, []AvailableCommandInfo{
		{Name: "compact", Description: "Compact history"},
		{Name: "brainstorm", Description: "Brainstorm ideas"},
	})

	ch := make(chan StreamEvent, 64)
	stop := conn.ScheduleCommandsReEmit(ch, 50*time.Millisecond)
	defer stop()

	// Wait for the timer to fire
	time.Sleep(100 * time.Millisecond)

	events := drainStreamEvents(ch)
	require.Len(t, events, 1)
	assert.Equal(t, "commands_update", events[0].Type)
	require.Len(t, events[0].Commands, 2)
	assert.Equal(t, "compact", events[0].Commands[0].Name)
	assert.Equal(t, "brainstorm", events[0].Commands[1].Name)
}

func TestScheduleCommandsReEmit_StopCancelsTimer(t *testing.T) {
	agent := &model.Agent{ID: "test-schedule-stop", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "test-schedule-stop")

	GetAgentCapabilityRegistry().UpdateCommands(agent.ID, []AvailableCommandInfo{
		{Name: "compact", Description: "Compact history"},
	})

	ch := make(chan StreamEvent, 64)
	stop := conn.ScheduleCommandsReEmit(ch, 50*time.Millisecond)
	stop() // Cancel immediately

	// Wait past the timer deadline
	time.Sleep(100 * time.Millisecond)

	events := drainStreamEvents(ch)
	assert.Empty(t, events, "no events expected when timer is cancelled")
}

func TestScheduleCommandsReEmit_NoCommands_NoEmit(t *testing.T) {
	agent := &model.Agent{ID: "test-schedule-nocmds", Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, "test-schedule-nocmds")
	// No commands in registry

	ch := make(chan StreamEvent, 64)
	stop := conn.ScheduleCommandsReEmit(ch, 50*time.Millisecond)
	defer stop()

	time.Sleep(100 * time.Millisecond)

	events := drainStreamEvents(ch)
	assert.Empty(t, events, "no events expected when registry has no commands")
}
