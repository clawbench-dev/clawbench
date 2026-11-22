package service

import (
	"context"
	"testing"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "clawbench/internal/ai/backends/builtin"
)

// These tests pin the message-id binding every interactive turn must carry.
//
// The built-in backend rebuilds its context from chat_history, and the turn's
// own user row is persisted BEFORE the request is built. Without
// WithCurrentMessageID the replayed history includes that row, so the model is
// handed the same question twice — once as history and once as the prompt. The
// direct-send path always passed the id; the queue drain, the enqueue-launched
// execution and the auto-continue / cruise rounds did not, which made the
// duplicate appear only on turns that started from the queue.
//
// They assert on the HISTORY rather than on an internal option field, so they
// stay honest even if the option is renamed: what matters is that the question
// being answered is not part of the replayed context.

// assertQuestionNotReplayed checks the built-in history bound took effect.
func assertQuestionNotReplayed(t *testing.T, req ai.ChatRequest, question string) {
	t.Helper()
	require.NotNil(t, req.History, "the fixture must produce a non-empty history, or the check is vacuous")
	for _, h := range req.History {
		assert.NotEqual(t, question, h.Text,
			"the turn's own user message must not be replayed as history (the model would read it twice)")
	}
}

// seedBuiltinTurnHistory stores one earlier exchange plus the current question,
// in the order the real flow uses: prior turns, then this turn's user row.
func seedBuiltinTurnHistory(t *testing.T, sessionID string) int64 {
	t.Helper()
	if _, err := store.WriteExec(
		`INSERT OR IGNORE INTO chat_sessions (id, project_id, backend, title) VALUES (?, 0, 'builtin', '')`,
		sessionID,
	); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	content := `{"blocks":[{"type":"text","text":"hello"}]}`
	for _, role := range []string{roleUser, roleAssistant} {
		_, err := AddChatMessage("/proj", builtinBackendID, sessionID, role, content, nil, false, "")
		require.NoError(t, err)
	}
	// The current question is materialized like a queued/auto turn does.
	qID, err := AddChatMessage("/proj", builtinBackendID, sessionID, roleUser, `{"blocks":[{"type":"text","text":"current question"}]}`, nil, false, "")
	require.NoError(t, err)
	return qID
}

func TestBuildChatRequest_CurrentMessageBoundExcludesQuestion(t *testing.T) {
	setupTestDBForBuiltinHistory(t)
	qID := seedBuiltinTurnHistory(t, "sess-bound")

	req := BuildChatRequest("current question", "sess-bound", "/proj", builtinBackendID, "", "", "", "", "", "/proj", false,
		WithCurrentMessageID(qID))
	assertQuestionNotReplayed(t, req, "current question")
	assert.Len(t, req.History, 2, "the earlier exchange must still be replayed")
}

// TestBuildChatRequest_EveryTurnBoundsItsOwnMessage is the guard for the missing
// bindings: a caller that omits the option replays its own question, which is
// exactly what the queue and auto-continue turns used to do.
func TestBuildChatRequest_EveryTurnBoundsItsOwnMessage(t *testing.T) {
	setupTestDBForBuiltinHistory(t)
	seedBuiltinTurnHistory(t, "sess-unbound")

	unbound := BuildChatRequest("current question", "sess-unbound", "/proj", builtinBackendID, "", "", "", "", "", "/proj", false)
	var replayed bool
	for _, h := range unbound.History {
		if h.Text == "current question" {
			replayed = true
		}
	}
	assert.True(t, replayed,
		"sanity check: without the option the question IS replayed — that is the bug the other tests detect")
}

// TestExecuteStreamRunShared_BindsMaterializedMessage pins the enqueue path.
// LaunchConfig.MessageID is the row EnqueueAndMaybeStart just materialized; the
// shared executor must forward it into the request instead of letting the
// question be replayed.
func TestExecuteStreamRunShared_BindsMaterializedMessage(t *testing.T) {
	setupTestDBForBuiltinHistory(t)
	qID := seedBuiltinTurnHistory(t, "sess-launch")

	captured := captureTurnRequest(t, func() streamRunResultShared {
		return executeStreamRunShared(context.Background(), LaunchConfig{
			SessionID:   "sess-launch",
			ProjectPath: "/proj",
			BackendName: builtinBackendID,
			Message:     "current question",
			MessageID:   qID,
		})
	})

	assertQuestionNotReplayed(t, captured, "current question")
}

// TestExecuteStreamRunShared_FallsBackToNewestRow covers the launchers that
// cannot know the id (restart resume, queue reaper): the newest row IS the
// turn's own message, so it is still excluded.
func TestExecuteStreamRunShared_FallsBackToNewestRow(t *testing.T) {
	setupTestDBForBuiltinHistory(t)
	qID := seedBuiltinTurnHistory(t, "sess-fallback")

	// The fallback must read back exactly the row the enqueue path would have
	// passed, otherwise the two launchers answer different prompts.
	require.Equal(t, qID, LastChatMessageID("sess-fallback"))

	captured := captureTurnRequest(t, func() streamRunResultShared {
		return executeStreamRunShared(context.Background(), LaunchConfig{
			SessionID:   "sess-fallback",
			ProjectPath: "/proj",
			BackendName: builtinBackendID,
			Message:     "current question",
		})
	})

	assertQuestionNotReplayed(t, captured, "current question")
}

func TestLastChatMessageID_EmptySessionIsZero(t *testing.T) {
	setupTestDBForBuiltinHistory(t)
	assert.Zero(t, LastChatMessageID("no-such-session"))
}

// captureTurnRequest runs one shared execution and hands back the ChatRequest it
// built, recorded at the point it reaches the backend.
//
// Stubbing runTurn's result would assert on the caller, not on the request the
// backend sees — and this bug lives exactly in that handoff. So the turn is
// allowed to run and only its request is recorded, through the same seam a
// backend registry lookup happens on.
func captureTurnRequest(t *testing.T, run func() streamRunResultShared) ai.ChatRequest {
	t.Helper()
	prev := ai.NewBackendForAgentWithTransportFn
	t.Cleanup(func() { ai.NewBackendForAgentWithTransportFn = prev })

	var captured ai.ChatRequest
	ai.NewBackendForAgentWithTransportFn = func(backendType, agentID, transport string) (ai.AIBackend, error) {
		return &requestCapturingBackend{onExecute: func(req ai.ChatRequest) { captured = req }}, nil
	}

	run()
	return captured
}

// requestCapturingBackend records the request it is asked to execute and then
// fails the turn immediately: the assertions are about the request alone, and a
// failing backend keeps the test from depending on any real streaming behavior.
type requestCapturingBackend struct {
	onExecute func(ai.ChatRequest)
}

func (b *requestCapturingBackend) Name() string { return builtinBackendID }

func (b *requestCapturingBackend) ExecuteStream(_ context.Context, req ai.ChatRequest) (<-chan ai.StreamEvent, error) {
	b.onExecute(req)
	ch := make(chan ai.StreamEvent)
	close(ch)
	return ch, nil
}

var _ = model.Agent{}

// The turn's own attachments must reach the backend, or an image the user just
// sent is only a path in the prompt: the model answers the path and nothing
// shows it saw a picture. Pinned on the enqueue path, which builds its own
// prompt and is the one most likely to lose a field.
func TestExecuteStreamRunShared_CarriesTurnFiles(t *testing.T) {
	setupTestDBForBuiltinHistory(t)
	qID := seedBuiltinTurnHistory(t, "sess-files-launch")

	captured := captureTurnRequest(t, func() streamRunResultShared {
		return executeStreamRunShared(context.Background(), LaunchConfig{
			SessionID:   "sess-files-launch",
			ProjectPath: "/proj",
			BackendName: builtinBackendID,
			Message:     "current question",
			MessageID:   qID,
			Files:       []model.FileEntry{{Path: "/proj/.clawbench/uploads/a.png"}},
		})
	})

	require.Len(t, captured.Files, 1, "the attachment must reach the request")
	assert.Equal(t, "/proj/.clawbench/uploads/a.png", captured.Files[0].Path)
}

// The option itself, independent of any entry point.
func TestBuildChatRequest_CarriesTurnFiles(t *testing.T) {
	setupTestDBForBuiltinHistory(t)
	files := []model.FileEntry{{Path: "/proj/a.png"}}

	req := BuildChatRequest("hi", "sess-files-opt", "/proj", builtinBackendID, "", "", "", "", "", "/proj", true,
		WithTurnFiles(files))
	require.Len(t, req.Files, 1)
	assert.Equal(t, "/proj/a.png", req.Files[0].Path)
}
