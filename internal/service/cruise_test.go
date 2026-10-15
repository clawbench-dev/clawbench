package service

import (
	"context"
	"strings"
	"sync"
	"testing"

	"clawbench/internal/ai"
	"clawbench/internal/grouprouting"
	"clawbench/internal/i18n"
	"clawbench/internal/model"
	"clawbench/internal/summarize"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Single-chat auto-cruise ──────────────────────────────────────────────
//
// Single chat has ONE "hand the floor back" point: the reply finishing. Cruise
// turns that into a confirmation round, looping until the AI declares
// <clawbench-goal-satisfied/> (or the round cap is hit). See cruise.go.
//
// The STRICT rule is the feature's hardest requirement: a tag in the user's own
// turn must NOT end the run — only a CONFIRMATION round's tag does. Every test
// below pins that distinction.

// cruiseTestSession creates a single-chat session in the full-schema test DB.
func cruiseTestSession(t *testing.T, project string) string {
	t.Helper()
	sid, err := CreateSession(project, "codebuddy", "Cruise", "", "", "default", "chat")
	require.NoError(t, err)
	t.Cleanup(func() { SetSessionRunning(sid, false, true) })
	return sid
}

// runCruiseLoop drives RunDrainLoop with a real CruiseContinue hook backed by a
// scripted sequence of confirmation-round outcomes, and returns how many rounds
// ran plus the terminal result. It proves the drain loop actually invokes
// CruiseContinue repeatedly (and resets its budget on a real dequeue) rather
// than only the runner being correct in isolation.
func runCruiseLoop(t *testing.T, sessionID string, first DrainResult, rounds []DrainResult) int {
	t.Helper()
	var mu sync.Mutex
	idx := 0
	ran := 0
	cfg := DrainConfig{
		SessionID: sessionID,
		CruiseContinue: func(attempt int, prev DrainResult) (DrainResult, bool) {
			mu.Lock()
			defer mu.Unlock()
			if attempt > 1 && prev.GoalSatisfied {
				// A satisfied confirmation round: stop, but this invocation
				// runs no turn, so it does not count.
				return prev, false
			}
			if idx >= len(rounds) {
				return prev, false
			}
			next := rounds[idx]
			idx++
			ran++
			return next, true
		},
		MarkDoneAndSendFinal: func(ai.StreamEvent) {},
	}
	RunDrainLoop(cfg, first)
	return ran
}

// TestCruiseRunner_DisabledStops verifies that with the switch OFF the runner
// refuses to continue immediately.
func TestCruiseRunner_DisabledStops(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/cruise-single-off"
	sid := cruiseTestSession(t, project)

	calls := 0
	hook := NewCruiseRunner(CruiseRunnerConfig{
		Ctx:       context.Background(),
		SessionID: sid,
		RunTurn: func(string) DrainResult {
			calls++
			return DrainResult{}
		},
	})
	_, ok := hook(1, DrainResult{MsgID: 1})
	assert.False(t, ok, "cruise off must refuse to continue")
	assert.Equal(t, 0, calls, "no turn must run when cruise is off")
}

// TestCruiseRunner_PreviousConfirmSatisfiedStops verifies that once a
// CONFIRMATION round (attempt>1) declared the goal met, the runner stops and
// writes the cruise-complete system line.
func TestCruiseRunner_PreviousConfirmSatisfiedStops(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/cruise-single-satisfied"
	sid := cruiseTestSession(t, project)
	require.NoError(t, SetSessionCruise(sid, true))

	calls := 0
	hook := NewCruiseRunner(CruiseRunnerConfig{
		Ctx:         context.Background(),
		SessionID:   sid,
		ProjectPath: project,
		RunTurn:     func(string) DrainResult { calls++; return DrainResult{} },
	})
	// attempt=2 means round 1 already ran; its result declared satisfied.
	_, ok := hook(2, DrainResult{MsgID: 5, GoalSatisfied: true})
	assert.False(t, ok, "a satisfied confirmation round must stop the loop")
	assert.Equal(t, 0, calls, "no further turn after a satisfied confirmation")

	// The "task confirmed complete" line must be written.
	msgs, err := GetMessagesBySessionIDRaw(sid)
	require.NoError(t, err)
	want := i18n.T(i18n.LocalizerForLocale(model.Language), "CruiseSatisfied", map[string]any{"Rounds": 1})
	found := false
	for _, m := range msgs {
		if m.Role == "system" && strings.Contains(ExtractPlainText(m.Content), want) {
			found = true
		}
	}
	assert.True(t, found, "cruise-complete system line must be written (got %q)", want)
}

// TestCruiseRunner_UserTurnTagIgnoredEvenWhenSatisfied verifies the strict rule
// at the boundary: attempt==1 with prev.GoalSatisfied=true must NOT stop — the
// runner runs a confirmation round instead.
func TestCruiseRunner_UserTurnTagIgnoredEvenWhenSatisfied(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/cruise-single-usertag"
	sid := cruiseTestSession(t, project)
	require.NoError(t, SetSessionCruise(sid, true))

	// The loop must be running for the runner to proceed.
	_, created := TryClaimSessionRun(sid)
	require.True(t, created)
	defer FinishSessionRun(sid)

	calls := 0
	hook := NewCruiseRunner(CruiseRunnerConfig{
		Ctx:         context.Background(),
		SessionID:   sid,
		ProjectPath: project,
		RunTurn: func(prompt string) DrainResult {
			calls++
			// The confirmation prompt must carry the shared marker.
			assert.Contains(t, prompt, "[自动巡航·完成确认]")
			return DrainResult{MsgID: 6}
		},
	})
	next, ok := hook(1, DrainResult{MsgID: 5, GoalSatisfied: true})
	assert.True(t, ok, "attempt==1 must never stop, even with a goal-satisfied tag")
	assert.Equal(t, 1, calls, "exactly one confirmation round must run")
	assert.Equal(t, int64(6), next.MsgID)
}

// TestCruiseRunner_CapStops verifies the round budget: once GetGroupMaxSpeeches()
// confirmation rounds have run, the runner stops and writes the cap line.
func TestCruiseRunner_CapStops(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/cruise-single-cap"
	sid := cruiseTestSession(t, project)
	require.NoError(t, SetSessionCruise(sid, true))

	prevCap := model.ChatGroupMaxSpeeches
	model.ChatGroupMaxSpeeches = 3
	t.Cleanup(func() { model.ChatGroupMaxSpeeches = prevCap })

	calls := 0
	hook := NewCruiseRunner(CruiseRunnerConfig{
		Ctx:         context.Background(),
		SessionID:   sid,
		ProjectPath: project,
		RunTurn:     func(string) DrainResult { calls++; return DrainResult{} },
	})
	// attempt=4 means 3 rounds already ran (== the cap) → stop.
	_, ok := hook(4, DrainResult{MsgID: 9})
	assert.False(t, ok, "the round cap must stop the loop")
	assert.Equal(t, 0, calls)

	msgs, err := GetMessagesBySessionIDRaw(sid)
	require.NoError(t, err)
	want := i18n.T(i18n.LocalizerForLocale(model.Language), "CruiseMaxRounds")
	found := false
	for _, m := range msgs {
		if m.Role == "system" && strings.Contains(ExtractPlainText(m.Content), want) {
			found = true
		}
	}
	assert.True(t, found, "cap system line must be written")
}

// TestCruiseRunner_WritesConfirmingLineAndRunsTurn verifies the happy path of a
// confirmation round: the "checking" line is written, the previous bubble is
// finalized, and the confirmation turn runs.
func TestCruiseRunner_WritesConfirmingLineAndRunsTurn(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/cruise-single-confirm"
	sid := cruiseTestSession(t, project)
	require.NoError(t, SetSessionCruise(sid, true))

	_, created := TryClaimSessionRun(sid)
	require.True(t, created)
	defer FinishSessionRun(sid)

	finalized := int64(0)
	origFinalize := emitTurnFinalize
	emitTurnFinalize = func(_ string, msgID int64) { finalized = msgID }
	t.Cleanup(func() { emitTurnFinalize = origFinalize })

	calls := 0
	hook := NewCruiseRunner(CruiseRunnerConfig{
		Ctx:         context.Background(),
		SessionID:   sid,
		ProjectPath: project,
		RunTurn:     func(string) DrainResult { calls++; return DrainResult{MsgID: 7} },
	})
	_, ok := hook(1, DrainResult{MsgID: 42})
	assert.True(t, ok)
	assert.Equal(t, 1, calls)
	assert.Equal(t, int64(42), finalized, "the previous turn's bubble must be finalized")

	msgs, err := GetMessagesBySessionIDRaw(sid)
	require.NoError(t, err)
	want := i18n.T(i18n.LocalizerForLocale(model.Language), "CruiseConfirming")
	n := 0
	for _, m := range msgs {
		if m.Role == "system" && strings.Contains(ExtractPlainText(m.Content), want) {
			n++
		}
	}
	assert.Equal(t, 1, n, "the confirming system line must be written once")
}

// TestShouldCruiseContinue_Gates verifies the loop-level guards: a nil hook, a
// non-clean turn, or a stopped session must all refuse to cruise.
func TestShouldCruiseContinue_Gates(t *testing.T) {
	setupDrainTest(t)
	sid := "cruise-gate-session"
	setupDrainSession(t, sid)
	SubmitRunForTest(t, sid)

	hook := func(int, DrainResult) (DrainResult, bool) { return DrainResult{}, true }

	// Nil hook → never cruise.
	assert.False(t, shouldCruiseContinue(DrainConfig{SessionID: sid}, DrainResult{}))

	cfg := DrainConfig{SessionID: sid, CruiseContinue: hook}
	// A clean turn → cruise.
	assert.True(t, shouldCruiseContinue(cfg, DrainResult{}))
	// A cancelled turn → no cruise.
	assert.False(t, shouldCruiseContinue(cfg, DrainResult{CancelReason: cancelReasonUser}))
	// An errored turn → no cruise.
	assert.False(t, shouldCruiseContinue(cfg, DrainResult{Err: "boom"}))
	// An empty turn → no cruise.
	assert.False(t, shouldCruiseContinue(cfg, DrainResult{Empty: true}))
	// An abnormal turn → no cruise (auto-continue's job).
	assert.False(t, shouldCruiseContinue(cfg, DrainResult{AbnormalReason: abnormalNoTerminal}))

	// A stopped session → no cruise.
	FinishSessionRun(sid)
	assert.False(t, shouldCruiseContinue(cfg, DrainResult{}), "a stopped session must not cruise")
}

// TestCruiseLoop_RunsConfirmationRoundsUntilSatisfied verifies the drain loop
// integration: a clean first turn triggers confirmation rounds until one of
// them declares the goal met, then the loop terminates.
func TestCruiseLoop_RunsConfirmationRoundsUntilSatisfied(t *testing.T) {
	setupDrainTest(t)
	sid := "cruise-loop-satisfied"
	setupDrainSession(t, sid)
	SubmitRunForTest(t, sid)

	rounds := []DrainResult{
		{MsgID: 2},                      // round 1: not satisfied
		{MsgID: 3, GoalSatisfied: true}, // round 2: satisfied
		{MsgID: 4},                      // must NOT run
	}
	ran := runCruiseLoop(t, sid, DrainResult{MsgID: 1}, rounds)
	// Round 1 runs (returns round1). Loop re-enters: attempt=2, prev=round1
	// (not satisfied) → runs round 2. Loop re-enters: attempt=3, prev=round2
	// (satisfied) → stops. So exactly 2 confirmation rounds run.
	assert.Equal(t, 2, ran, "the loop must stop after the satisfied confirmation round")
}

// TestCruiseLoop_AbnormalTurnNotCruised verifies an abnormally-terminated first
// turn is NOT wrapped in a confirmation round (auto-continue's territory).
func TestCruiseLoop_AbnormalTurnNotCruised(t *testing.T) {
	setupDrainTest(t)
	sid := "cruise-loop-abnormal"
	setupDrainSession(t, sid)
	SubmitRunForTest(t, sid)

	rounds := []DrainResult{{MsgID: 2, GoalSatisfied: true}}
	ran := runCruiseLoop(t, sid, DrainResult{MsgID: 1, AbnormalReason: abnormalNoTerminal}, rounds)
	assert.Equal(t, 0, ran, "an abnormal turn must not enter cruise")
}

// TestCruiseLoop_UserTurnTagDoesNotEndOnFirstAttempt verifies the strict rule at
// the loop level: even when the FIRST (user) turn carries goal-satisfied, the
// loop still runs a confirmation round (attempt 1 never stops on prev).
func TestCruiseLoop_UserTurnTagDoesNotEndOnFirstAttempt(t *testing.T) {
	setupDrainTest(t)
	sid := "cruise-loop-strict"
	setupDrainSession(t, sid)
	SubmitRunForTest(t, sid)

	rounds := []DrainResult{{MsgID: 2}} // one confirmation round, not satisfied
	ran := runCruiseLoop(t, sid, DrainResult{MsgID: 1, GoalSatisfied: true}, rounds)
	assert.Equal(t, 1, ran, "the user turn's tag must not end the run — a confirmation round must still run")
}

// TestGoalSatisfied_ParsedFromFinalAnswer verifies the parse rule runTurnFinalize
// uses: the completion tag in the LAST answer counts, a tag buried before the
// last tool call does not.
func TestGoalSatisfied_ParsedFromFinalAnswer(t *testing.T) {
	blocks := []model.ContentBlock{
		{Type: "text", Text: "working on it"},
		{Type: eventTypeToolUse, Name: "Bash", ID: "t1"},
		{Type: "text", Text: "all done <clawbench-goal-satisfied/>"},
	}
	assert.True(t, grouprouting.Parse(summarize.ExtractLastAnswerFromBlocks(blocks)).GoalSatisfied,
		"a tag in the final answer must be detected")

	buried := []model.ContentBlock{
		{Type: "text", Text: "done <clawbench-goal-satisfied/>"},
		{Type: eventTypeToolUse, Name: "Bash", ID: "t1"},
		{Type: "text", Text: "still working"},
	}
	assert.False(t, grouprouting.Parse(summarize.ExtractLastAnswerFromBlocks(buried)).GoalSatisfied,
		"a tag before the last tool call must not count")
}
