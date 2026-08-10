package ai

import (
	"strings"
	"testing"

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
