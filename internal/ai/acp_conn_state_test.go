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
