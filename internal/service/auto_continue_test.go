package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"clawbench/internal/store"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/ws"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The classifier is the single gate that decides whether a session gets
// resumed. Its exclusions are the feature's hardest requirement (a user who
// pressed stop must never be resumed), so every reason is asserted explicitly
// rather than trusted to a default.
func TestClassifyTurnAbnormality(t *testing.T) {
	text := []model.ContentBlock{{Type: "text", Text: "here is the answer"}}
	toolOnly := []model.ContentBlock{{Type: "tool_use", Name: "Bash", ID: "t1"}}

	warn := func(reason string) []model.ContentBlock {
		return []model.ContentBlock{{Type: blockTypeWarning, Text: "boom", Reason: reason}}
	}
	warnPlusText := func(reason string) []model.ContentBlock {
		return []model.ContentBlock{
			{Type: blockTypeWarning, Text: "boom", Reason: reason},
			{Type: "text", Text: "partial answer"},
		}
	}

	tests := []struct {
		name             string
		cancelReason     string
		receivedTerminal bool
		empty            bool
		blocks           []model.ContentBlock
		want             string
	}{
		// ── The three triggers ──
		{"crash with no terminal event", "", false, false, nil, abnormalNoTerminal},
		{"crash after partial content", "", false, false, text, abnormalNoTerminal},
		{"empty turn", "", true, true, nil, abnormalEmpty},
		{"backend exit warning", "", true, false, warn(ai.ReasonBackendExit), ai.ReasonBackendExit},
		{"parse error warning", "", true, false, warn(ai.ReasonParseError), ai.ReasonParseError},
		{"request failed warning", "", true, false, warn(ai.ReasonRequestFailed), ai.ReasonRequestFailed},
		{"refused warning", "", true, false, warn(ai.ReasonRefused), ai.ReasonRefused},
		{"agent no run warning", "", true, false, warn(ai.ReasonAgentNoRun), ai.ReasonAgentNoRun},
		// A turn that only ran tools has no answer yet — worth resuming.
		{"tool call only", "", true, false, toolOnly, ""},

		// ── Never resume a cancellation ──
		{"user cancel", cancelReasonUser, true, false, nil, ""},
		{"interrupt and send", cancelReasonInterrupt, true, false, nil, ""},
		{"generic cancel", "cancel", true, false, nil, ""},
		{"client disconnect", "disconnect", true, false, nil, ""},
		{"server restart", cancelReasonRestart, true, false, nil, ""},
		// A cancel wins even when the turn also looks crashed: the cancel
		// reason is recorded before the context is cancelled, so a turn that
		// died without a terminal event right after a user stop is still a
		// user stop.
		{"user cancel with no terminal event", cancelReasonUser, false, false, nil, ""},
		{"restart with warning block", cancelReasonRestart, true, false, warn(ai.ReasonBackendExit), ""},

		// ── Non-retryable failures ──
		{"panic", "", true, false, warn(ai.ReasonPanic), ""},
		{"timeout", "", true, false, warn(ai.ReasonTimeout), ""},
		{"agent init timeout", "", true, false, warn(ai.ReasonAgentInitTimeout), ""},
		// A signed-out agent fails identically forever; retrying writes a
		// spurious user+assistant row pair per attempt. Must stay non-retryable.
		{"auth required", "", true, false, warn(ai.ReasonAuthRequired), ""},
		{"context cancel warning", "", true, false, warn(ai.ReasonContextCancel), ""},
		{"user cancel warning reason", "", true, false, warn(ai.ReasonUserCancel), ""},
		{"disconnect warning reason", "", true, false, warn(ai.ReasonDisconnect), ""},

		// ── Clean finishes ──
		{"normal completion", "", true, false, text, ""},
		{"normal completion, no blocks", "", true, false, nil, ""},

		// ── Partial output must NOT suppress a retry ──
		// A long agentic run that dies part-way through has narrated plenty but
		// the work is unfinished. Suppressing the resume there is what left the
		// user clicking "继续" by hand, so text presence is deliberately not a
		// gate — only the cancellation/non-retryable reasons above are.
		{"backend exit after text was produced", "", true, false, warnPlusText(ai.ReasonBackendExit), ai.ReasonBackendExit},
		{"empty with text produced", "", true, false, []model.ContentBlock{
			{Type: blockTypeWarning, Text: "boom", Reason: ai.ReasonEmpty},
			{Type: "text", Text: "partial answer"},
		}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyTurnAbnormality(tt.cancelReason, tt.receivedTerminal, tt.empty, tt.blocks)
			if got != tt.want {
				t.Errorf("classifyTurnAbnormality(%q, terminal=%v, empty=%v) = %q, want %q",
					tt.cancelReason, tt.receivedTerminal, tt.empty, got, tt.want)
			}
		})
	}
}

// The budget is expressed as "retries already spent", and BOTH callers must
// agree on that convention — the drain loop passes the attempt it is about to
// make minus one, and the scheduler passes attempt-1 for the same reason. An
// off-by-one here changes how many tokens a user's configured budget costs.
func TestAutoContinueAttemptsAllowed_RetryCountSemantics(t *testing.T) {
	// max_retries=2 must permit exactly two retries: retries 0 and 1 already
	// spent, refusing once 2 are spent.
	for spent, want := range map[int]bool{0: true, 1: true, 2: false, 3: false} {
		if got := AutoContinueAttemptsAllowed(spent, 2); got != want {
			t.Errorf("AutoContinueAttemptsAllowed(spent=%d, max=2) = %v, want %v", spent, got, want)
		}
	}
	// max_retries=0 means the feature is on but no retry may run.
	if AutoContinueAttemptsAllowed(0, 0) {
		t.Error("max_retries=0 must not permit a retry")
	}
}

// Regression for the reported case: a long agentic run that narrated many
// steps and was refused part-way through (with a tool call still in flight)
// must still be resumed. The earlier "no readable text yet" gate suppressed
// exactly this, leaving the user to click 继续 by hand.
func TestClassifyTurnAbnormality_LongRunRefusedMidWorkIsResumed(t *testing.T) {
	blocks := []model.ContentBlock{
		{Type: blockTypeThinking, Text: "planning the change"},
		{Type: "text", Text: "I'll implement all four fixes. Let me first read the files."},
		{Type: "tool_use", Name: "Read", ID: "t1", Done: true},
		{Type: "text", Text: "Now implementing. Starting with the preload."},
		{Type: "tool_use", Name: "Edit", ID: "t2", Done: true},
		{Type: "text", Text: "Critical finding — the E2E test caught a regression."},
		// The turn was cut off while this call was still running.
		{Type: "tool_use", Name: "Bash", ID: "t3", Done: false},
		{Type: blockTypeWarning, Text: "AI request refused by the agent", Reason: ai.ReasonRefused},
	}

	got := classifyTurnAbnormality("", true, false, blocks)
	if got != ai.ReasonRefused {
		t.Errorf("classifyTurnAbnormality = %q, want %q (a refused long run must be resumed)",
			got, ai.ReasonRefused)
	}
}

func TestAutoContinueAttemptsAllowed(t *testing.T) {
	tests := []struct {
		name    string
		attempt int
		max     int
		want    bool
	}{
		{"default allows first", 0, 3, true},
		{"default allows third", 2, 3, true},
		{"default stops at limit", 3, 3, false},
		{"zero retries never runs", 0, 0, false},
		{"unlimited well below ceiling", 10, -1, true},
		{"unlimited stops at ceiling", autoContinueAttemptCeiling, -1, false},
		{"unlimited stops past ceiling", autoContinueAttemptCeiling + 5, -1, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AutoContinueAttemptsAllowed(tt.attempt, tt.max); got != tt.want {
				t.Errorf("AutoContinueAttemptsAllowed(%d, %d) = %v, want %v",
					tt.attempt, tt.max, got, tt.want)
			}
		})
	}
}

// The prompt is localized from the server-side language, because this runs with
// no HTTP request in scope. Both supported languages must resolve to a real
// translation rather than falling back to the message key.
func TestAutoContinuePromptLocalized(t *testing.T) {
	original := model.Language
	t.Cleanup(func() { model.Language = original })

	for _, tc := range []struct{ lang, want string }{
		{"zh", "继续"},
		{"en", "Continue"},
	} {
		model.Language = tc.lang
		if got := AutoContinuePrompt(); got != tc.want {
			t.Errorf("AutoContinuePrompt() with language %q = %q, want %q", tc.lang, got, tc.want)
		}
	}
}

// ── Runner factory ─────────────────────────────────────────────────────────
//
// The runner owns the gating and the cancel-aware delay shared by both
// interactive entry points. Its delay is stubbed out so the tests do not
// actually wait 3 seconds.

func withInstantAutoContinueSleep(t *testing.T) {
	t.Helper()
	prev := autoContinueSleep
	autoContinueSleep = func(ctx context.Context, d time.Duration) bool {
		select {
		case <-ctx.Done():
			return false
		default:
			return true
		}
	}
	t.Cleanup(func() { autoContinueSleep = prev })
}

func TestAutoContinueRunner_Disabled(t *testing.T) {
	setupDrainTest(t)
	sessionID := "runner-disabled"
	setupDrainSession(t, sessionID)
	SubmitRunForTest(t, sessionID)

	prevEnabled := model.ChatAutoContinueEnabled
	model.ChatAutoContinueEnabled = false
	t.Cleanup(func() { model.ChatAutoContinueEnabled = prevEnabled })

	called := false
	runner := NewAutoContinueRunner(AutoContinueRunnerConfig{
		Ctx:         context.Background(),
		SessionID:   sessionID,
		ProjectPath: "/test",
		BackendName: "codebuddy",
		RunTurn:     func(prompt string) DrainResult { called = true; return DrainResult{} },
	})

	_, ok := runner(1, DrainResult{AbnormalReason: abnormalEmpty})
	assert.False(t, ok, "disabled feature must refuse the retry")
	assert.False(t, called, "no turn may run when the feature is off")
}

func TestAutoContinueRunner_RespectsMaxRetries(t *testing.T) {
	setupDrainTest(t)
	sessionID := "runner-max-retries"
	setupDrainSession(t, sessionID)
	SubmitRunForTest(t, sessionID)
	withInstantAutoContinueSleep(t)

	prevEnabled, prevMax := model.ChatAutoContinueEnabled, model.ChatAutoContinueMaxRetries
	model.ChatAutoContinueEnabled, model.ChatAutoContinueMaxRetries = true, 2
	t.Cleanup(func() {
		model.ChatAutoContinueEnabled, model.ChatAutoContinueMaxRetries = prevEnabled, prevMax
	})

	turns := 0
	runner := NewAutoContinueRunner(AutoContinueRunnerConfig{
		Ctx:         context.Background(),
		SessionID:   sessionID,
		ProjectPath: "/test",
		BackendName: "codebuddy",
		RunTurn: func(prompt string) DrainResult {
			turns++
			return DrainResult{AbnormalReason: abnormalEmpty}
		},
	})

	// The drain loop passes attempt=1 for the first try, so the runner receives
	// attempts 1 and 2 and must refuse 3.
	assert.True(t, mustRun(t, runner, 1))
	assert.True(t, mustRun(t, runner, 2))
	assert.False(t, mustRun(t, runner, 3), "attempt 3 exceeds max_retries=2")
	assert.Equal(t, 2, turns)
}

func mustRun(t *testing.T, runner func(int, DrainResult) (DrainResult, bool), attempt int) bool {
	t.Helper()
	_, ok := runner(attempt, DrainResult{AbnormalReason: abnormalEmpty})
	return ok
}

// A cancel that lands during the delay must abort the retry: launching a turn
// against a cancelled context would resurrect a session the user stopped.
func TestAutoContinueRunner_CancelledDuringDelay(t *testing.T) {
	setupDrainTest(t)
	sessionID := "runner-cancel-during-delay"
	setupDrainSession(t, sessionID)
	SubmitRunForTest(t, sessionID)

	prevEnabled := model.ChatAutoContinueEnabled
	model.ChatAutoContinueEnabled = true
	t.Cleanup(func() { model.ChatAutoContinueEnabled = prevEnabled })

	// Simulate a cancel arriving while the runner waits.
	prev := autoContinueSleep
	autoContinueSleep = func(ctx context.Context, d time.Duration) bool { return false }
	t.Cleanup(func() { autoContinueSleep = prev })

	called := false
	runner := NewAutoContinueRunner(AutoContinueRunnerConfig{
		Ctx:         context.Background(),
		SessionID:   sessionID,
		ProjectPath: "/test",
		BackendName: "codebuddy",
		RunTurn:     func(prompt string) DrainResult { called = true; return DrainResult{} },
	})

	_, ok := runner(1, DrainResult{AbnormalReason: abnormalEmpty})
	assert.False(t, ok, "a cancelled wait must refuse the retry")
	assert.False(t, called, "no turn may run after the wait was cancelled")
}

// A context that is already cancelled must not even start the wait.
func TestAutoContinueRunner_AlreadyCancelledContext(t *testing.T) {
	setupDrainTest(t)
	sessionID := "runner-already-cancelled"
	setupDrainSession(t, sessionID)
	SubmitRunForTest(t, sessionID)
	withInstantAutoContinueSleep(t)

	prevEnabled := model.ChatAutoContinueEnabled
	model.ChatAutoContinueEnabled = true
	t.Cleanup(func() { model.ChatAutoContinueEnabled = prevEnabled })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	called := false
	runner := NewAutoContinueRunner(AutoContinueRunnerConfig{
		Ctx:         ctx,
		SessionID:   sessionID,
		ProjectPath: "/test",
		BackendName: "codebuddy",
		RunTurn:     func(prompt string) DrainResult { called = true; return DrainResult{} },
	})

	_, ok := runner(1, DrainResult{AbnormalReason: abnormalEmpty})
	assert.False(t, ok)
	assert.False(t, called)
}

// The persisted message must be a real user row carrying the localized prompt.
// It is written directly to chat_history (not queued), so the frontend renders
// it inline exactly like a user typing the same text.
func TestAutoContinueRunner_PersistsRealUserMessage(t *testing.T) {
	setupDrainTest(t)
	sessionID := "runner-persists"
	setupDrainSession(t, sessionID)
	SubmitRunForTest(t, sessionID)
	withInstantAutoContinueSleep(t)

	prevEnabled, prevLang := model.ChatAutoContinueEnabled, model.Language
	prevMax := model.ChatAutoContinueMaxRetries
	model.ChatAutoContinueEnabled, model.Language, model.ChatAutoContinueMaxRetries = true, "en", 3
	t.Cleanup(func() {
		model.ChatAutoContinueEnabled, model.Language = prevEnabled, prevLang
		model.ChatAutoContinueMaxRetries = prevMax
	})

	var gotPrompt string
	runner := NewAutoContinueRunner(AutoContinueRunnerConfig{
		Ctx:         context.Background(),
		SessionID:   sessionID,
		ProjectPath: "/test",
		BackendName: "codebuddy",
		RunTurn: func(prompt string) DrainResult {
			gotPrompt = prompt
			return DrainResult{}
		},
	})

	require.True(t, mustRun(t, runner, 1))
	assert.Equal(t, "Continue", gotPrompt)

	var role, content string
	err := store.ReadDB().QueryRow(
		"SELECT role, content FROM chat_history WHERE session_id = ? ORDER BY id DESC LIMIT 1",
		sessionID,
	).Scan(&role, &content)
	require.NoError(t, err)
	assert.Equal(t, "user", role, "the continue message must be a real user message")
	assert.Equal(t, "Continue", content, "the persisted text must be the localized prompt")

	// It is a real chat_history row, NOT a queued one — a queued message would
	// render in the queue panel instead of inline.
	assert.Equal(t, 0, GetQueuedCount(sessionID), "the continue message must not be queued")
}

// ── Per-turn finalize on resume ────────────────────────────────────────────
//
// Resuming a failed turn starts a NEW turn, so the failed turn's bubble must be
// closed by a stream_finalize BEFORE the resume runs. Without it the frontend
// keeps the failed bubble's spinner until the whole run's terminal `done`
// sweeps every streaming bubble (or the user switches sessions) — the reported
// "backend error message still shows a loading indicator until the run ends".

// A resume must emit exactly one stream_finalize carrying the failed turn's
// finalized row id, and it must fire BEFORE the resume turn runs (so the
// spinner clears as the new turn starts, not after it finishes).
func TestAutoContinueRunner_EmitsPerTurnFinalizeBeforeResume(t *testing.T) {
	setupDrainTest(t)
	sessionID := "runner-emits-finalize"
	setupDrainSession(t, sessionID)
	SubmitRunForTest(t, sessionID)
	withInstantAutoContinueSleep(t)

	prevEnabled := model.ChatAutoContinueEnabled
	prevMax := model.ChatAutoContinueMaxRetries
	model.ChatAutoContinueEnabled, model.ChatAutoContinueMaxRetries = true, 3
	t.Cleanup(func() {
		model.ChatAutoContinueEnabled, model.ChatAutoContinueMaxRetries = prevEnabled, prevMax
	})

	origFinalize := emitTurnFinalize
	var calls []int64
	emitTurnFinalize = func(_ string, msgID int64) { calls = append(calls, msgID) }
	t.Cleanup(func() { emitTurnFinalize = origFinalize })

	var finalizeSeenBeforeTurn bool
	runner := NewAutoContinueRunner(AutoContinueRunnerConfig{
		Ctx:         context.Background(),
		SessionID:   sessionID,
		ProjectPath: "/test",
		BackendName: "codebuddy",
		RunTurn: func(prompt string) DrainResult {
			// The finalize must already have been emitted when the resume runs.
			finalizeSeenBeforeTurn = len(calls) == 1
			return DrainResult{}
		},
	})

	ok := mustRunWithMsgID(t, runner, 1, 4242)

	assert.True(t, ok)
	assert.Equal(t, []int64{4242}, calls,
		"the failed turn's row id must be finalized exactly once before resuming")
	assert.True(t, finalizeSeenBeforeTurn,
		"stream_finalize must precede the resume turn so the spinner clears immediately")
}

// A failed turn that never opened a row (msgID 0) must not emit a finalize:
// there is no bubble to close, and a 0-id finalize is a no-op on the frontend.
// This exercises the REAL seam (not a stub), because the guard lives inside it.
func TestEmitTurnFinalize_ZeroMsgIDIsNoOp(t *testing.T) {
	setupDrainTest(t)
	sessionID := "emit-finalize-zero"

	mgr := ws.GetManager()
	clientID := "emit-finalize-observer"
	var writeMu sync.Mutex
	sub := mgr.Subscribe(nil, &writeMu, clientID, "")
	mgr.StreamHub().Subscribe(clientID, sessionID)

	// 0 (and negative) ids must emit nothing.
	emitTurnFinalize(sessionID, 0)
	emitTurnFinalize(sessionID, -1)
	assert.Empty(t, sub.GetBufferedEvents(),
		"a failed turn with no row must not emit a stream_finalize")

	// A positive id emits exactly one stream_finalize carrying that id.
	emitTurnFinalize(sessionID, 4242)
	require.Len(t, sub.GetBufferedEvents(), 1)
	data, ok := sub.GetBufferedEvents()[0].Data.(ws.ChatStreamData)
	require.True(t, ok, "the emitted event must be a chat_stream frame")
	assert.Equal(t, "stream_finalize", data.EventType)
	payload, ok := data.Payload.(map[string]any)
	require.True(t, ok, "stream_finalize payload must be a map")
	assert.EqualValues(t, 4242, payload["message_id"])
}

// mustRunWithMsgID is mustRun with a carried MsgID on prev, so the finalize
// contract (which reads prev.MsgID) can be exercised.
func mustRunWithMsgID(t *testing.T, runner func(int, DrainResult) (DrainResult, bool), attempt int, msgID int64) bool {
	t.Helper()
	_, ok := runner(attempt, DrainResult{AbnormalReason: abnormalEmpty, MsgID: msgID})
	return ok
}
