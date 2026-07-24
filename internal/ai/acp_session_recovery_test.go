package ai

import (
	"context"
	"fmt"
	"testing"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
)

func TestIsACPMethodNotFound(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		assert.False(t, isACPMethodNotFound(nil))
	})

	t.Run("request_error_code_32601", func(t *testing.T) {
		err := &acp.RequestError{Code: -32601, Message: "Method not found"}
		assert.True(t, isACPMethodNotFound(err))
	})

	t.Run("wrapped_request_error", func(t *testing.T) {
		inner := &acp.RequestError{Code: -32601, Message: "Method not found"}
		err := fmt.Errorf("acp: ResumeSession failed for session abc: %w", inner)
		assert.True(t, isACPMethodNotFound(err))
	})

	t.Run("plain_message", func(t *testing.T) {
		err := fmt.Errorf(`rpc error: {"code":-32601,"message":"Method not found"}`)
		assert.True(t, isACPMethodNotFound(err))
	})

	t.Run("other_request_error", func(t *testing.T) {
		err := &acp.RequestError{Code: -32603, Message: "Internal error"}
		assert.False(t, isACPMethodNotFound(err))
	})

	t.Run("unrelated_error", func(t *testing.T) {
		assert.False(t, isACPMethodNotFound(fmt.Errorf("session not found on disk")))
	})
}

func TestACPResumeUnsupportedCache(t *testing.T) {
	clearACPResumeUnsupportedForTest()
	t.Cleanup(clearACPResumeUnsupportedForTest)

	assert.False(t, isACPResumeUnsupported("agent-grok"))
	assert.False(t, isACPResumeUnsupported(""))

	markACPResumeUnsupported("agent-grok")
	assert.True(t, isACPResumeUnsupported("agent-grok"))
	assert.False(t, isACPResumeUnsupported("agent-claude"))

	// empty agent ID is a no-op
	markACPResumeUnsupported("")
	assert.False(t, isACPResumeUnsupported(""))

	clearACPResumeUnsupportedForTest()
	assert.False(t, isACPResumeUnsupported("agent-grok"))
}

func TestMergeResumedSessionState_FromLoadSessionResponse(t *testing.T) {
	agent := &model.Agent{ID: "test-merge-load", Backend: "grok", AcpCommand: "grok agent stdio"}
	conn := newACPConn(agent, "test-merge-load")

	modeCat := acp.SessionConfigOptionCategoryMode
	loadResp := &acp.LoadSessionResponse{
		Modes: &acp.SessionModeState{
			CurrentModeId: "code",
			AvailableModes: []acp.SessionMode{
				{Id: "code", Name: "Code"},
				{Id: "plan", Name: "Plan"},
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
	conn.lastLoadSessionResp = loadResp
	conn.mu.Unlock()

	conn.MergeResumedSessionState()

	assert.Equal(t, "code", conn.GetCurrentModeID())
	// Load response must be cleared after merge
	assert.Nil(t, conn.GetAndClearLoadSessionResp())
	assert.Nil(t, conn.GetAndClearResumeSessionResp())
}

func TestMergeResumedSessionState_LoadPreservesUserSelection(t *testing.T) {
	agent := &model.Agent{ID: "test-merge-load-preserve", Backend: "grok", AcpCommand: "grok agent stdio"}
	conn := newACPConn(agent, "test-merge-load-preserve")
	conn.SetCurrentModeID("plan") // user selection re-applied after recovery

	loadResp := &acp.LoadSessionResponse{
		Modes: &acp.SessionModeState{
			CurrentModeId: "code", // agent default differs from user
			AvailableModes: []acp.SessionMode{
				{Id: "code", Name: "Code"},
				{Id: "plan", Name: "Plan"},
			},
		},
	}
	conn.mu.Lock()
	conn.lastLoadSessionResp = loadResp
	conn.mu.Unlock()

	conn.MergeResumedSessionState()
	assert.Equal(t, "plan", conn.GetCurrentModeID(), "user selection must win over LoadSession default")
}

func TestAliveAfterUserCancel(t *testing.T) {
	assert.True(t, aliveAfterUserCancel(true), "healthy peer must stay alive after cancel")
	assert.False(t, aliveAfterUserCancel(false), "dead peer must be marked dead after cancel")
}

func TestApplyUserCancelLiveness_MarksDeadWhenPeerNil(t *testing.T) {
	agent := &model.Agent{ID: "test-cancel-keep", Backend: "claude", AcpCommand: "echo"}
	conn := newACPConn(agent, "test-cancel-keep")
	conn.alive = true
	conn.applyUserCancelLiveness("sid-1")
	assert.False(t, conn.alive, "nil peer must mark dead")
}

func TestAgentPrefersLoadSessionRecovery(t *testing.T) {
	assert.True(t, agentPrefersLoadSessionRecovery("grok", "grok agent stdio"))
	assert.True(t, agentPrefersLoadSessionRecovery("Grok", "something"))
	assert.True(t, agentPrefersLoadSessionRecovery("custom", "grok agent stdio"))
	assert.False(t, agentPrefersLoadSessionRecovery("claude", "npx claude-agent-acp"))
	assert.False(t, agentPrefersLoadSessionRecovery("codebuddy", "codebuddy --acp"))
	assert.False(t, agentPrefersLoadSessionRecovery("", ""))
}

func TestPrefersLoadSessionRecovery_GrokAndCache(t *testing.T) {
	clearACPResumeUnsupportedForTest()
	t.Cleanup(clearACPResumeUnsupportedForTest)

	assert.False(t, prefersLoadSessionRecovery(nil))
	assert.True(t, prefersLoadSessionRecovery(&model.Agent{
		ID: "g1", Backend: "grok", AcpCommand: "grok agent stdio",
	}))
	assert.False(t, prefersLoadSessionRecovery(&model.Agent{
		ID: "c1", Backend: "claude", AcpCommand: "claude acp",
	}))

	markACPResumeUnsupported("c1")
	assert.True(t, prefersLoadSessionRecovery(&model.Agent{
		ID: "c1", Backend: "claude", AcpCommand: "claude acp",
	}), "cached resume-unsupported must prefer load")
}

func TestRecoverPriorSession_ResumeSuccess(t *testing.T) {
	clearACPResumeUnsupportedForTest()
	t.Cleanup(clearACPResumeUnsupportedForTest)

	agent := &model.Agent{ID: "test-resume-ok", Backend: "claude", AcpCommand: "echo"}
	conn := newACPConn(agent, "test-resume-ok")
	conn.alive = true

	var resumeCalled, loadCalled bool
	conn.SetResumeSessionFnForTest(func(ctx context.Context, cwd, acpSID string) (acp.ResumeSessionResponse, error) {
		resumeCalled = true
		assert.Equal(t, "acp-sid-1", acpSID)
		assert.Equal(t, "/proj", cwd)
		return acp.ResumeSessionResponse{
			Modes: &acp.SessionModeState{CurrentModeId: "code"},
		}, nil
	})
	conn.SetLoadSessionFnForTest(func(ctx context.Context, cwd, acpSID string) (acp.LoadSessionResponse, error) {
		loadCalled = true
		return acp.LoadSessionResponse{}, nil
	})

	conn.mu.Lock()
	err := conn.recoverPriorSession(context.Background(), "/proj", "acp-sid-1", cachedConfigSnapshot{mode: "code"})
	conn.mu.Unlock()

	require.NoError(t, err)
	assert.True(t, resumeCalled)
	assert.False(t, loadCalled, "LoadSession must not run when ResumeSession succeeds")
	assert.Equal(t, "acp-sid-1", conn.AcpSID())
	assert.NotNil(t, conn.GetAndClearResumeSessionResp())
}

func TestRecoverPriorSession_GrokSkipsResumeGoesDirectToLoad(t *testing.T) {
	clearACPResumeUnsupportedForTest()
	t.Cleanup(clearACPResumeUnsupportedForTest)

	agent := &model.Agent{ID: "grok-direct-load", Backend: "grok", AcpCommand: "grok agent stdio"}
	conn := newACPConn(agent, "grok-direct-load")
	conn.alive = true

	var resumeCalled, loadCalled bool
	conn.SetResumeSessionFnForTest(func(ctx context.Context, cwd, acpSID string) (acp.ResumeSessionResponse, error) {
		resumeCalled = true
		return acp.ResumeSessionResponse{}, nil
	})
	conn.SetLoadSessionFnForTest(func(ctx context.Context, cwd, acpSID string) (acp.LoadSessionResponse, error) {
		loadCalled = true
		assert.Equal(t, "acp-sid-grok-direct", acpSID)
		return acp.LoadSessionResponse{
			Modes: &acp.SessionModeState{CurrentModeId: "code"},
		}, nil
	})

	conn.mu.Lock()
	err := conn.recoverPriorSession(context.Background(), "/proj", "acp-sid-grok-direct", cachedConfigSnapshot{})
	conn.mu.Unlock()

	require.NoError(t, err)
	assert.False(t, resumeCalled, "Grok must not attempt ResumeSession")
	assert.True(t, loadCalled, "Grok must go straight to LoadSession")
	assert.True(t, isACPResumeUnsupported(agent.ID))
	assert.Equal(t, "acp-sid-grok-direct", conn.AcpSID())
	assert.NotNil(t, conn.GetAndClearLoadSessionResp())
}

func TestRecoverPriorSession_MethodNotFoundFallsBackToLoad(t *testing.T) {
	clearACPResumeUnsupportedForTest()
	t.Cleanup(clearACPResumeUnsupportedForTest)

	agentID := "test-resume-mnf-fallback"
	agent := &model.Agent{ID: agentID, Backend: "claude", AcpCommand: "claude acp"}
	conn := newACPConn(agent, "test-resume-mnf-fallback")
	conn.alive = true
	client := NewClawBenchACPClient()
	client.connRef = conn
	conn.client = client

	var resumeCalls, loadCalls int
	conn.SetResumeSessionFnForTest(func(ctx context.Context, cwd, acpSID string) (acp.ResumeSessionResponse, error) {
		resumeCalls++
		return acp.ResumeSessionResponse{}, &acp.RequestError{Code: -32601, Message: "Method not found"}
	})
	conn.SetLoadSessionFnForTest(func(ctx context.Context, cwd, acpSID string) (acp.LoadSessionResponse, error) {
		loadCalls++
		assert.Equal(t, "acp-sid-mnf", acpSID)
		client.loadSessionBufMu.Lock()
		client.loadSessionBuf = append(client.loadSessionBuf, acp.SessionNotification{
			SessionId: acp.SessionId(acpSID),
			Update:    acp.UpdateAgentMessageText("replayed history"),
		})
		client.loadSessionBufMu.Unlock()
		return acp.LoadSessionResponse{
			Modes: &acp.SessionModeState{CurrentModeId: "code"},
		}, nil
	})

	conn.mu.Lock()
	err := conn.recoverPriorSession(context.Background(), "/proj", "acp-sid-mnf", cachedConfigSnapshot{})
	conn.mu.Unlock()

	require.NoError(t, err)
	assert.Equal(t, 1, resumeCalls)
	assert.Equal(t, 1, loadCalls)
	assert.True(t, isACPResumeUnsupported(agentID))
	assert.Equal(t, "acp-sid-mnf", conn.AcpSID())
	assert.True(t, conn.alive)
	assert.False(t, conn.loadSessionActive.Load())
	assert.Empty(t, client.GetAndClearLoadSessionBuf())
	assert.NotNil(t, conn.GetAndClearLoadSessionResp())
}

func TestRecoverPriorSession_SkipsResumeWhenKnownUnsupported(t *testing.T) {
	clearACPResumeUnsupportedForTest()
	t.Cleanup(clearACPResumeUnsupportedForTest)

	agentID := "test-skip-resume-agent"
	markACPResumeUnsupported(agentID)

	agent := &model.Agent{ID: agentID, Backend: "grok", AcpCommand: "echo"}
	conn := newACPConn(agent, "test-skip-resume")
	conn.alive = true

	var resumeCalled bool
	conn.SetResumeSessionFnForTest(func(ctx context.Context, cwd, acpSID string) (acp.ResumeSessionResponse, error) {
		resumeCalled = true
		return acp.ResumeSessionResponse{}, nil
	})
	conn.SetLoadSessionFnForTest(func(ctx context.Context, cwd, acpSID string) (acp.LoadSessionResponse, error) {
		return acp.LoadSessionResponse{}, nil
	})

	conn.mu.Lock()
	err := conn.recoverPriorSession(context.Background(), "/tmp", "acp-sid-1", cachedConfigSnapshot{})
	conn.mu.Unlock()

	require.NoError(t, err)
	assert.False(t, resumeCalled, "known-unsupported agents must skip ResumeSession")
	assert.Equal(t, "acp-sid-1", conn.AcpSID())
}

func TestRecoverPriorSession_NonMethodNotFoundDoesNotFallback(t *testing.T) {
	clearACPResumeUnsupportedForTest()
	t.Cleanup(clearACPResumeUnsupportedForTest)

	agent := &model.Agent{ID: "test-resume-real-fail", Backend: "claude", AcpCommand: "echo"}
	conn := newACPConn(agent, "test-resume-real-fail")
	conn.alive = true

	var loadCalled bool
	conn.SetResumeSessionFnForTest(func(ctx context.Context, cwd, acpSID string) (acp.ResumeSessionResponse, error) {
		return acp.ResumeSessionResponse{}, &acp.RequestError{Code: -32603, Message: "session corrupt"}
	})
	conn.SetLoadSessionFnForTest(func(ctx context.Context, cwd, acpSID string) (acp.LoadSessionResponse, error) {
		loadCalled = true
		return acp.LoadSessionResponse{}, nil
	})

	conn.mu.Lock()
	err := conn.recoverPriorSession(context.Background(), "/tmp", "acp-sid-x", cachedConfigSnapshot{})
	conn.mu.Unlock()

	require.Error(t, err)
	assert.False(t, loadCalled, "must not fall back to LoadSession for non-method-not-found errors")
	assert.False(t, conn.alive, "real resume failures mark connection dead")
	assert.False(t, isACPResumeUnsupported(agent.ID))
}

func TestRecoverPriorSession_LoadFallbackAlsoFails(t *testing.T) {
	clearACPResumeUnsupportedForTest()
	t.Cleanup(clearACPResumeUnsupportedForTest)

	agent := &model.Agent{ID: "test-both-fail", Backend: "claude", AcpCommand: "echo"}
	conn := newACPConn(agent, "test-both-fail")
	conn.alive = true

	conn.SetResumeSessionFnForTest(func(ctx context.Context, cwd, acpSID string) (acp.ResumeSessionResponse, error) {
		return acp.ResumeSessionResponse{}, &acp.RequestError{Code: -32601, Message: "Method not found"}
	})
	conn.SetLoadSessionFnForTest(func(ctx context.Context, cwd, acpSID string) (acp.LoadSessionResponse, error) {
		return acp.LoadSessionResponse{}, fmt.Errorf("Path not found")
	})

	conn.mu.Lock()
	err := conn.recoverPriorSession(context.Background(), "/tmp", "missing-sid", cachedConfigSnapshot{})
	conn.mu.Unlock()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "LoadSession")
	assert.True(t, isACPResumeUnsupported(agent.ID))
	assert.False(t, conn.alive)
}

func TestRecoverPriorSession_GrokLoadFails(t *testing.T) {
	clearACPResumeUnsupportedForTest()
	t.Cleanup(clearACPResumeUnsupportedForTest)

	agent := &model.Agent{ID: "test-grok-load-fail", Backend: "grok", AcpCommand: "grok agent stdio"}
	conn := newACPConn(agent, "test-grok-load-fail")
	conn.alive = true

	var resumeCalled bool
	conn.SetResumeSessionFnForTest(func(ctx context.Context, cwd, acpSID string) (acp.ResumeSessionResponse, error) {
		resumeCalled = true
		return acp.ResumeSessionResponse{}, nil
	})
	conn.SetLoadSessionFnForTest(func(ctx context.Context, cwd, acpSID string) (acp.LoadSessionResponse, error) {
		return acp.LoadSessionResponse{}, fmt.Errorf("Path not found")
	})

	conn.mu.Lock()
	err := conn.recoverPriorSession(context.Background(), "/tmp", "missing-sid", cachedConfigSnapshot{})
	conn.mu.Unlock()

	require.Error(t, err)
	assert.False(t, resumeCalled)
	assert.Contains(t, err.Error(), "LoadSession")
	assert.False(t, conn.alive)
}

func TestLoadSessionLocked_KeepReplayForExplicitImport(t *testing.T) {
	agent := &model.Agent{ID: "test-load-import", Backend: "claude", AcpCommand: "echo"}
	conn := newACPConn(agent, "test-load-import")
	client := NewClawBenchACPClient()
	client.connRef = conn
	conn.client = client

	conn.SetLoadSessionFnForTest(func(ctx context.Context, cwd, acpSID string) (acp.LoadSessionResponse, error) {
		client.loadSessionBufMu.Lock()
		client.loadSessionBuf = append(client.loadSessionBuf, acp.SessionNotification{
			SessionId: acp.SessionId(acpSID),
			Update:    acp.UpdateAgentMessageText("import me"),
		})
		client.loadSessionBufMu.Unlock()
		return acp.LoadSessionResponse{}, nil
	})

	conn.mu.Lock()
	err := conn.loadSessionLocked(context.Background(), "/tmp", "import-sid", false)
	conn.mu.Unlock()

	require.NoError(t, err)
	assert.True(t, conn.loadSessionActive.Load(), "explicit import keeps loadSessionActive for handler")
	assert.Len(t, client.GetAndClearLoadSessionBuf(), 1)
}

func TestRecoverViaResumeSession_MethodNotFoundKeepsAlive(t *testing.T) {
	agent := &model.Agent{ID: "test-resume-mnf-alive", Backend: "grok", AcpCommand: "echo"}
	conn := newACPConn(agent, "test-resume-mnf-alive")
	conn.alive = true

	conn.SetResumeSessionFnForTest(func(ctx context.Context, cwd, acpSID string) (acp.ResumeSessionResponse, error) {
		return acp.ResumeSessionResponse{}, &acp.RequestError{Code: -32601, Message: "Method not found"}
	})

	conn.mu.Lock()
	err := conn.recoverViaResumeSession(context.Background(), "/tmp", "sid", cachedConfigSnapshot{})
	conn.mu.Unlock()

	require.Error(t, err)
	assert.True(t, isACPMethodNotFound(err))
	assert.True(t, conn.alive, "method-not-found must not mark connection dead")
}
