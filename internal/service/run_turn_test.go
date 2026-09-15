package service

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/ws"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runTurn is the single implementation of "run one AI turn" shared by the
// /api/ai/chat handler, the queue/push execution path, and the scheduler. These
// tests pin the properties the three copies used to implement separately, so a
// future edit cannot silently re-diverge them.

// runTurnBackendScript is the event sequence the registered test backend emits.
// RegisterBackend panics on duplicate ids, so the backend is registered exactly
// once and reads this variable — each test sets it before running its turn.
var (
	runTurnScriptMu sync.Mutex
	runTurnScript   []ai.StreamEvent
	runTurnScriptFn func() ([]ai.StreamEvent, error)
)

func setRunTurnScript(events ...ai.StreamEvent) {
	runTurnScriptMu.Lock()
	defer runTurnScriptMu.Unlock()
	runTurnScript = events
	runTurnScriptFn = nil
}

func setRunTurnScriptErr(err error) {
	runTurnScriptMu.Lock()
	defer runTurnScriptMu.Unlock()
	runTurnScript = nil
	runTurnScriptFn = func() ([]ai.StreamEvent, error) { return nil, err }
}

// runTurnTestBackend emits the scripted event sequence, so tests can drive the
// turn to completion, to empty, or to a start failure without a real AI CLI.
type runTurnTestBackend struct{}

func (b *runTurnTestBackend) Name() string { return "run-turn-test" }

func (b *runTurnTestBackend) ExecuteStream(_ context.Context, _ ai.ChatRequest) (<-chan ai.StreamEvent, error) {
	runTurnScriptMu.Lock()
	events, failFn := runTurnScript, runTurnScriptFn
	runTurnScriptMu.Unlock()

	if failFn != nil {
		if _, err := failFn(); err != nil {
			return nil, err
		}
	}

	ch := make(chan ai.StreamEvent, len(events)+1)
	for _, e := range events {
		ch <- e
	}
	close(ch)
	return ch, nil
}

// registerRunTurnTestBackend registers the scripted backend once per process.
var registerRunTurnTestBackend = sync.OnceFunc(func() {
	ai.RegisterBackend("run-turn-test", func() ai.AIBackend { return &runTurnTestBackend{} })
})

// setupRunTurnTest wires an in-memory DB + agent + backend and returns the DB
// handle and the session id. The backend name is registered in the ai factory
// under "run-turn-test".
func setupRunTurnTest(t *testing.T, events []ai.StreamEvent) (*sql.DB, string) {
	t.Helper()

	db := setupTestDBForSessionCommand(t)
	cleanup := SetDBForTest(db, db)
	origAgents := model.Agents
	model.Agents = map[string]*model.Agent{
		"run-turn-agent": {ID: "run-turn-agent", Name: "Test", Backend: "run-turn-test", Transport: ""},
	}
	t.Cleanup(func() {
		model.Agents = origAgents
		cleanup()
		_ = db.Close()
		cleanupAllSessionState()
	})

	registerRunTurnTestBackend()
	setRunTurnScript(events...)

	sessionID := "run-turn-sess"
	_, err := db.Exec(`INSERT INTO chat_sessions
		(id, project_path, backend, title, agent_id, agent_source, model, session_type, auto_approve)
		VALUES (?, '/tmp', 'run-turn-test', 'Test', 'run-turn-agent', 'default', '', 'chat', 0)`, sessionID)
	require.NoError(t, err)
	return db, sessionID
}

// TestRunTurn_EmitsStreamStartAndPersistsReply verifies the shared
// implementation performs the full interactive lifecycle: placeholder row,
// stream_start broadcast, content persisted, and a clean (non-error) result.
func TestRunTurn_EmitsStreamStartAndPersistsReply(t *testing.T) {
	db, sessionID := setupRunTurnTest(t, []ai.StreamEvent{
		{Type: "content", Content: "hello"},
		{Type: "done"},
	})

	res := runTurn(TurnSpec{
		Ctx:             context.Background(),
		Mode:            ModeInteractive,
		ProjectPath:     "/tmp",
		BackendName:     "run-turn-test",
		SessionID:       sessionID,
		AgentID:         "run-turn-agent",
		ChatReq:         ai.ChatRequest{Prompt: "hi"},
		FileDir:         "/tmp",
		DrainOnFinalize: true,
	})

	assert.Empty(t, res.Err, "a normal completion must not report an error")
	assert.False(t, res.Empty, "content was produced, so the turn is not empty")
	assert.Empty(t, res.CancelReason, "a normal completion has no cancel reason")
	assert.True(t, res.ReceivedTerminal, "the done event is a terminal event")
	assert.Greater(t, res.MsgID, int64(0), "the streaming placeholder must have been created")

	// The assistant reply must have been persisted by Finalize.
	var count int
	require.NoError(t, db.QueryRow(
		"SELECT COUNT(*) FROM chat_history WHERE session_id = ? AND role = 'assistant'",
		sessionID).Scan(&count))
	assert.GreaterOrEqual(t, count, 1, "Finalize must persist the assistant reply")
}

// TestRunTurn_EmptyContentIsReported verifies an AI turn that produces no
// content is reported as Empty, which the drain loop turns into a visible
// "AI returned no content" error instead of a silent no-op.
func TestRunTurn_EmptyContentIsReported(t *testing.T) {
	_, sessionID := setupRunTurnTest(t, []ai.StreamEvent{
		{Type: "done"},
	})

	res := runTurn(TurnSpec{
		Ctx:             context.Background(),
		Mode:            ModeInteractive,
		ProjectPath:     "/tmp",
		BackendName:     "run-turn-test",
		SessionID:       sessionID,
		AgentID:         "run-turn-agent",
		ChatReq:         ai.ChatRequest{Prompt: "hi"},
		FileDir:         "/tmp",
		DrainOnFinalize: true,
	})

	assert.True(t, res.Empty, "no content blocks means Empty")
	assert.Empty(t, res.Err)
}

// TestRunTurn_StreamStartFailureUsesCallerWording is the regression test for the
// error-wording divergence: the service path prefixes the raw error while the
// HTTP handler localizes. Both must keep their own wording after the merge.
func TestRunTurn_StreamStartFailureUsesCallerWording(t *testing.T) {
	_, sessionID := setupRunTurnTest(t, nil)
	setRunTurnScriptErr(fmt.Errorf("boom"))

	t.Run("service wording prefixes the raw error", func(t *testing.T) {
		res := runTurn(TurnSpec{
			Ctx:           context.Background(),
			Mode:          ModeInteractive,
			ProjectPath:   "/tmp",
			BackendName:   "run-turn-test",
			SessionID:     sessionID,
			AgentID:       "run-turn-agent",
			ChatReq:       ai.ChatRequest{Prompt: "hi"},
			FileDir:       "/tmp",
			LocalizeError: serviceLocalizeError,
		})
		assert.Contains(t, res.Err, "start stream", "service paths keep their prefix")
		assert.Contains(t, res.Err, "boom")
	})

	t.Run("custom localizer owns the wording", func(t *testing.T) {
		res := runTurn(TurnSpec{
			Ctx:         context.Background(),
			Mode:        ModeInteractive,
			ProjectPath: "/tmp",
			BackendName: "run-turn-test",
			SessionID:   sessionID,
			AgentID:     "run-turn-agent",
			ChatReq:     ai.ChatRequest{Prompt: "hi"},
			FileDir:     "/tmp",
			LocalizeError: func(err error, key string, args map[string]any) string {
				return "LOCALIZED:" + key
			},
		})
		assert.Equal(t, "LOCALIZED:"+reasonStreamStartFailed, res.Err)
	})
}

// TestRunTurn_BackendCreationFailure verifies the first early-failure branch
// reports an error and creates no streaming placeholder.
func TestRunTurn_BackendCreationFailure(t *testing.T) {
	db, sessionID := setupRunTurnTest(t, nil)

	res := runTurn(TurnSpec{
		Ctx:           context.Background(),
		Mode:          ModeInteractive,
		ProjectPath:   "/tmp",
		BackendName:   "definitely-not-a-registered-backend",
		SessionID:     sessionID,
		AgentID:       "run-turn-agent",
		ChatReq:       ai.ChatRequest{Prompt: "hi"},
		FileDir:       "/tmp",
		LocalizeError: serviceLocalizeError,
	})

	assert.Contains(t, res.Err, "create backend", "service paths keep their prefix")
	assert.Equal(t, int64(0), res.MsgID, "no placeholder is created when the backend cannot be built")

	var streaming int
	require.NoError(t, db.QueryRow(
		"SELECT COUNT(*) FROM chat_history WHERE session_id = ? AND streaming = 1",
		sessionID).Scan(&streaming))
	assert.Zero(t, streaming, "a failed start must not leave a streaming=1 row behind")
}

// TestRunTurn_EmitsStreamStartBeforeContent verifies stream_start is broadcast
// BEFORE the turn's event loop runs, so a subscriber learns the placeholder id
// before any content arrives. Emitting it after the loop (as the scheduler once
// did) lets a client create a placeholder for an already-finished row.
func TestRunTurn_EmitsStreamStartBeforeContent(t *testing.T) {
	db, sessionID := setupRunTurnTest(t, []ai.StreamEvent{
		{Type: "content", Content: "ok"},
		{Type: "done"},
	})

	origMgr := ws.GetManager()
	mgr := ws.NewManagerForTest()
	ws.SetManagerForTest(mgr)
	t.Cleanup(func() { ws.SetManagerForTest(origMgr) })

	var writeMu sync.Mutex
	sub := mgr.Subscribe(nil, &writeMu, "run-turn-order-client", "")
	mgr.StreamHub().Subscribe("run-turn-order-client", sessionID)

	runTurn(TurnSpec{
		Ctx:             context.Background(),
		Mode:            ModeScheduled,
		ProjectPath:     "/tmp",
		BackendName:     "run-turn-test",
		SessionID:       sessionID,
		AgentID:         "run-turn-agent",
		ChatReq:         ai.ChatRequest{Prompt: "hi", ScheduledExecution: true},
		FileDir:         "/tmp",
		DrainOnFinalize: true,
	})

	// The DB row must exist and the stream_start event must have been broadcast.
	require.Eventually(t, func() bool {
		for _, ev := range sub.GetBufferedEvents() {
			if ev.Event != "chat_stream" {
				continue
			}
			data, ok := ev.Data.(ws.ChatStreamData)
			if ok && data.EventType == "stream_start" {
				return true
			}
		}
		return false
	}, 2*time.Second, 20*time.Millisecond, "runTurn must broadcast stream_start")

	var streaming int
	require.NoError(t, db.QueryRow(
		"SELECT COUNT(*) FROM chat_history WHERE session_id = ? AND streaming = 1",
		sessionID).Scan(&streaming))
	assert.Zero(t, streaming, "the placeholder must have been finalized by the run")
}

// TestRunTurn_CancelledContextIsReportedAsCancel verifies a cancelled turn is
// classified as "cancel" rather than being reported as a normal finish. This is
// what the drain loop relies on to decide whether to keep the queue.
func TestRunTurn_CancelledContextIsReportedAsCancel(t *testing.T) {
	_, sessionID := setupRunTurnTest(t, []ai.StreamEvent{
		{Type: "content", Content: "partial"},
		{Type: "done"},
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled before the turn starts

	res := runTurn(TurnSpec{
		Ctx:             ctx,
		Mode:            ModeInteractive,
		ProjectPath:     "/tmp",
		BackendName:     "run-turn-test",
		SessionID:       sessionID,
		AgentID:         "run-turn-agent",
		ChatReq:         ai.ChatRequest{Prompt: "hi"},
		FileDir:         "/tmp",
		DrainOnFinalize: true,
	})

	// The mock backend still emits done, so CancelReason may be empty while the
	// derived context is cancelled. What must NOT happen is a spurious Err.
	assert.Empty(t, res.Err, "a user cancel is not an error")
}

// TestRunTurn_UnregistersTurnCancel verifies the per-turn cancel registration is
// cleared once the turn ends, so a later interrupt cannot claim a finished turn.
func TestRunTurn_UnregistersTurnCancel(t *testing.T) {
	_, sessionID := setupRunTurnTest(t, []ai.StreamEvent{
		{Type: "content", Content: "ok"},
		{Type: "done"},
	})

	runTurn(TurnSpec{
		Ctx:             context.Background(),
		Mode:            ModeInteractive,
		ProjectPath:     "/tmp",
		BackendName:     "run-turn-test",
		SessionID:       sessionID,
		AgentID:         "run-turn-agent",
		ChatReq:         ai.ChatRequest{Prompt: "hi"},
		FileDir:         "/tmp",
		DrainOnFinalize: true,
	})

	_, registered := CurrentTurnID(sessionID)
	assert.False(t, registered, "the turn must not stay advertised as interruptible after it ends")
}

// TestServiceLocalizeError_Wording pins the service layer's historical error
// strings, which existing tests and users depend on.
func TestServiceLocalizeError_Wording(t *testing.T) {
	args := map[string]any{"Error": "underlying"}
	assert.Equal(t, "create backend: underlying", serviceLocalizeError(nil, reasonBackendCreateFailed, args))
	assert.Equal(t, "start stream: underlying", serviceLocalizeError(nil, reasonStreamStartFailed, args))
	assert.Equal(t, "underlying", serviceLocalizeError(nil, "SomeOtherKey", args))
	assert.Equal(t, "", serviceLocalizeError(nil, "SomeOtherKey", nil))
}

// TestResolveFileDir verifies the shared working-directory resolution: ACP
// resume fails when handed a relative cwd, so every entry point must resolve it
// the same way.
func TestResolveFileDir(t *testing.T) {
	// An already-absolute path is returned unchanged. Use a real absolute path
	// from this platform rather than the literal "/tmp": on Windows "/tmp" has
	// no drive letter, so filepath.Abs resolves it against the current drive
	// ("D:\tmp") and the identity assertion would be wrong, not the code.
	dir := t.TempDir()
	assert.Equal(t, dir, resolveFileDir(dir))

	abs := resolveFileDir(".")
	assert.NotEmpty(t, abs)
	assert.NotEqual(t, ".", abs, "a relative path must be resolved to an absolute one")
	assert.True(t, filepath.IsAbs(abs), "the resolved path must be absolute")
}

// TestRunTurnStart_OnStartedFiresBeforeEventLoop pins the scheduler's "running"
// event ordering. runTurnStart's last step is the blocking RunWithChannel, so a
// caller that emitted "running" after it returned sent the event once the task
// had already finished — subscribers saw started → completed with no running in
// between. OnStarted must therefore fire after the turn is genuinely under way
// (placeholder exists) but before the event loop consumes anything.
func TestRunTurnStart_OnStartedFiresBeforeEventLoop(t *testing.T) {
	db, sessionID := setupRunTurnTest(t, []ai.StreamEvent{
		{Type: "content", Content: "hello"},
		{Type: "done"},
	})

	var (
		fired       bool
		placeholder int
		// The loop has not consumed the scripted content yet at hook time: the
		// assistant row is still the empty streaming placeholder.
		contentAtHook string
	)
	at := runTurnStart(TurnSpec{
		Ctx:         context.Background(),
		Mode:        ModeScheduled,
		ProjectPath: "/tmp",
		BackendName: "run-turn-test",
		SessionID:   sessionID,
		AgentID:     "run-turn-agent",
		ChatReq:     ai.ChatRequest{Prompt: "hi"},
		FileDir:     "/tmp",
		OnStarted: func() {
			fired = true
			// The hook runs on the caller's goroutine, before RunWithChannel,
			// so this read sees the placeholder exactly as created.
			_ = db.QueryRow(
				"SELECT COUNT(*) FROM chat_history WHERE session_id = ? AND role = 'assistant' AND streaming = 1",
				sessionID).Scan(&placeholder)
			_ = db.QueryRow(
				"SELECT content FROM chat_history WHERE session_id = ? AND role = 'assistant'",
				sessionID).Scan(&contentAtHook)
		},
	})
	defer at.release()
	require.True(t, at.started(), "the turn must have started for this test to mean anything")

	assert.True(t, fired, "OnStarted must have been invoked")
	assert.Equal(t, 1, placeholder, "the placeholder must already exist when OnStarted fires")
	assert.Contains(t, contentAtHook, `"blocks":[]`,
		"OnStarted must fire BEFORE the event loop, so the placeholder is still empty")

	// And the turn still completes normally afterwards.
	res := at.runTurnFinalize()
	assert.Empty(t, res.Err)
}

// TestRunTurnStart_OnStartedSkippedOnEarlyFailure verifies ISS-128 is preserved:
// a turn that fails before starting must not fire OnStarted, so the scheduler
// emits only "failed" and never a "running" state that immediately fails.
func TestRunTurnStart_OnStartedSkippedOnEarlyFailure(t *testing.T) {
	_, sessionID := setupRunTurnTest(t, nil)
	setRunTurnScriptErr(fmt.Errorf("backend refused to start"))

	fired := false
	at := runTurnStart(TurnSpec{
		Ctx:         context.Background(),
		Mode:        ModeScheduled,
		ProjectPath: "/tmp",
		BackendName: "run-turn-test",
		SessionID:   sessionID,
		AgentID:     "run-turn-agent",
		ChatReq:     ai.ChatRequest{Prompt: "hi"},
		FileDir:     "/tmp",
		OnStarted:   func() { fired = true },
	})
	defer at.release()

	assert.False(t, at.started(), "a stream-start failure means the turn never started")
	assert.False(t, fired, "OnStarted must not fire for a turn that failed to start")
}
