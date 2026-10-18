package ai

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
)

// TestSetSessionConfigOption_RejectedValueIsNotRecorded pins the issue #552 fix
// for the "display says LOW but the agent runs something else" bug.
//
// OpenCode answers -32602 "Invalid params: effort not found: low" when the
// selected model does not offer that level. The old code recorded the value
// anyway (markConfigSet + UpdateCachedCurrent), which had two consequences:
//
//  1. GET /api/ai/chat and the WS re-emit reported a level the agent never
//     accepted, so the UI lied about what was running;
//  2. shouldSetConfig's dedup then suppressed every retry, so the wrong value
//     could never be corrected for the rest of the session.
//
// The test drives the value through the real dispatch (SetSessionConfigOption),
// not the predicate, because the bug was in the caller's reaction to the error.
func TestSetSessionConfigOption_RejectedValueIsNotRecorded(t *testing.T) {
	rejected := errors.New(`Invalid params: effort not found: low`)

	conn := NewACPConnForTest(&model.Agent{ID: "reject-effort", Backend: "opencode"}, "sid-reject")
	conn.SetSessionMappingForTest("sid-reject", "acp-sid-reject")
	conn.alive = true
	conn.conn = newLiveConnForTest(t)
	conn.SetConfigOptionFnForTest(func(context.Context, string, string, string) error {
		return rejected
	})

	conn.SetSessionConfigOption(context.Background(), "thought_level", "low")

	assert.Equal(t, "", conn.GetCurrentThinkingEffortID(),
		"a rejected level must not become the cached current value")
	assert.True(t, conn.shouldSetConfig("thinkingEffort", "low"),
		"a rejected level must stay retryable (dedup must not swallow it)")
}

// TestSetSessionConfigOption_AcceptedValueIsRecorded is the positive control:
// the same path must still record a value the agent accepted, or the fix would
// simply disable caching for every config option.
func TestSetSessionConfigOption_AcceptedValueIsRecorded(t *testing.T) {
	conn := NewACPConnForTest(&model.Agent{ID: "accept-effort", Backend: "opencode"}, "sid-accept")
	conn.SetSessionMappingForTest("sid-accept", "acp-sid-accept")
	conn.alive = true
	conn.conn = newLiveConnForTest(t)
	conn.SetConfigOptionFnForTest(func(context.Context, string, string, string) error {
		return nil
	})

	conn.SetSessionConfigOption(context.Background(), "thought_level", "high")

	assert.Equal(t, "high", conn.GetCurrentThinkingEffortID())
	assert.False(t, conn.shouldSetConfig("thinkingEffort", "high"),
		"an accepted value must be deduped, or every turn re-sends it")
}

// TestSetSessionConfigOption_OtherErrorsStillRecorded documents the boundary:
// only a VALUE rejection stops the recording. A transport failure (timeout,
// dropped peer) keeps the old behavior, because the value may well have been
// applied and re-sending it on every turn would be pure overhead.
func TestSetSessionConfigOption_OtherErrorsStillRecorded(t *testing.T) {
	conn := NewACPConnForTest(&model.Agent{ID: "timeout-effort", Backend: "opencode"}, "sid-timeout")
	conn.SetSessionMappingForTest("sid-timeout", "acp-sid-timeout")
	conn.alive = true
	conn.conn = newLiveConnForTest(t)
	conn.SetConfigOptionFnForTest(func(context.Context, string, string, string) error {
		return errors.New("i/o timeout")
	})

	conn.SetSessionConfigOption(context.Background(), "thought_level", "high")

	assert.Equal(t, "high", conn.GetCurrentThinkingEffortID(),
		"a non-value failure keeps the cached value (it may have been applied)")
}

// TestIsInvalidConfigValue pins the predicate's narrowness. Matching every
// -32602 would swallow genuine protocol errors; matching only the agent's own
// wording would silently miss rewordings that still mean "this value is not
// available for this model".
func TestIsInvalidConfigValue(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"opencode effort", errors.New("Invalid params: effort not found: low"), true},
		{"reworded but same meaning", errors.New(`invalid params: value "minimal" not found`), true},
		{"unknown config option", errors.New("Unknown config option: thinkingEffort"), false},
		{"generic protocol error", errors.New("Invalid params"), false},
		{"timeout", errors.New("i/o timeout"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isInvalidConfigValue(tc.err))
		})
	}
}

// TestSetSessionConfigOption_RejectedValueKeepsRetryPossible walks the whole
// sequence a real model switch produces: pick a level, get it rejected, then
// switch back to a model that supports it. The second attempt must reach the
// agent — otherwise the rejected value would poison the session permanently.
func TestSetSessionConfigOption_RejectedValueKeepsRetryPossible(t *testing.T) {
	conn := NewACPConnForTest(&model.Agent{ID: "retry-effort", Backend: "opencode"}, "sid-retry")
	conn.SetSessionMappingForTest("sid-retry", "acp-sid-retry")
	conn.alive = true
	conn.conn = newLiveConnForTest(t)

	var attempts []string
	fail := true
	conn.SetConfigOptionFnForTest(func(_ context.Context, _, _, value string) error {
		attempts = append(attempts, value)
		if fail {
			return errors.New("Invalid params: effort not found: " + value)
		}
		return nil
	})

	conn.SetSessionConfigOption(context.Background(), "thought_level", "low")
	require.Len(t, attempts, 1)

	// Model changed; the same level is now valid.
	fail = false
	conn.SetSessionConfigOption(context.Background(), "thought_level", "low")

	require.Len(t, attempts, 2, "the retry must reach the agent after a rejection")
	assert.Equal(t, "low", conn.GetCurrentThinkingEffortID())
}

// TestSetConfigOptionWithCrashCheck_RejectedValueIsNotRecorded pins the SAME
// guard on the pre-prompt path (the one every turn actually uses).
//
// The per-turn apply goes through setConfigOptionWithCrashCheck, not
// SetSessionConfigOption. Recording a rejected value there would mean the
// session reports a level the agent refused AND dedup suppresses the retry for
// the rest of the session — the "chip says LOW, agent runs something else" bug
// (issue #552) on the primary code path.
func TestSetConfigOptionWithCrashCheck_RejectedValueIsNotRecorded(t *testing.T) {
	conn := NewACPConnForTest(&model.Agent{ID: "turn-reject", Backend: "opencode"}, "sid-turn-reject")
	conn.SetSessionMappingForTest("sid-turn-reject", "acp-sid-turn-reject")
	conn.alive = true
	conn.conn = newLiveConnForTest(t)
	conn.SetConfigOptionFnForTest(func(context.Context, string, string, string) error {
		return errors.New("Invalid params: effort not found: low")
	})

	// A rejected VALUE must not fail the turn (the agent keeps its own default).
	require.NoError(t, conn.setConfigOptionWithCrashCheck(context.Background(), "acp-sid-turn-reject",
		configOptionSpec{configID: "effort", value: "low", label: "thinking_effort"}))

	assert.True(t, conn.shouldSetConfig("effort", "low"),
		"a rejected value must stay retryable on the per-turn path too")
}
