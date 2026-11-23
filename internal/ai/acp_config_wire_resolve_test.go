package ai

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
)

// ---------------------------------------------------------------------------
// SetSessionConfigOption → wire config-option id resolution (issue #429)
// ---------------------------------------------------------------------------

type wireConfigSend struct {
	sessionID string
	configID  string
	value     string
}

// sentConfigRecorder records every set_config_option RPC the connection
// attempts via the test hook.
type sentConfigRecorder struct {
	mu   sync.Mutex
	sent []wireConfigSend
}

func (r *sentConfigRecorder) all() []wireConfigSend {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]wireConfigSend, len(r.sent))
	copy(out, r.sent)
	return out
}

func newLiveConnForConfigTest(id string) *ACPConn {
	agent := &model.Agent{ID: id, Backend: "acp-stdio", AcpCommand: "echo"}
	conn := newACPConn(agent, id)
	conn.SetAliveForTest()
	conn.SetSessionMappingForTest(id, "acp-sid-config-"+id)
	return conn
}

// installConfigSendRecorder replaces the SetSessionConfigOption RPC with a
// recording hook and returns the recorder.
func installConfigSendRecorder(t *testing.T, conn *ACPConn) *sentConfigRecorder {
	t.Helper()
	rec := &sentConfigRecorder{}
	conn.SetConfigOptionFnForTest(func(_ context.Context, acpSessionID, configID, value string) error {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		rec.sent = append(rec.sent, wireConfigSend{sessionID: acpSessionID, configID: configID, value: value})
		return nil
	})
	return rec
}

// The legacy "thinkingEffort" spelling and the internal "thought_level" key
// must both resolve to the agent-advertised wire id (claude-agent-acp uses
// "effort"); when nothing is advertised, the historical "thinkingEffort" id
// is sent unchanged.
func TestSetSessionConfigOption_ResolvesWireID(t *testing.T) {
	t.Run("advertised_effort_id", func(t *testing.T) {
		conn := newLiveConnForConfigTest("resolve-effort-advertised")
		// claude-agent-acp advertises its effort option as "effort".
		conn.SetWireConfigIDsForTest(map[string]string{"thought_level": "effort"})
		rec := installConfigSendRecorder(t, conn)

		conn.SetSessionConfigOption(context.Background(), "thought_level", "high")

		sends := rec.all()
		require.Len(t, sends, 1)
		assert.Equal(t, "effort", sends[0].configID, "RPC must use the agent-advertised wire id")
		assert.Equal(t, "high", sends[0].value)
		// Session cache must stay in sync with the sent value.
		assert.Equal(t, "high", conn.GetCurrentThinkingEffortID())
	})

	t.Run("legacy_spelling_maps_to_advertised_id", func(t *testing.T) {
		conn := newLiveConnForConfigTest("resolve-effort-legacy")
		conn.SetWireConfigIDsForTest(map[string]string{"thought_level": "effort"})
		rec := installConfigSendRecorder(t, conn)

		// Handler path passes the historical spelling.
		conn.SetSessionConfigOption(context.Background(), "thinkingEffort", "max")

		sends := rec.all()
		require.Len(t, sends, 1)
		assert.Equal(t, "effort", sends[0].configID)
	})

	t.Run("unadvertised_falls_back_to_hardcoded", func(t *testing.T) {
		conn := newLiveConnForConfigTest("resolve-effort-fallback")
		// No wire ids advertised → historical hardcoded id must be sent.
		rec := installConfigSendRecorder(t, conn)

		conn.SetSessionConfigOption(context.Background(), "thought_level", "high")

		sends := rec.all()
		require.Len(t, sends, 1)
		assert.Equal(t, "thinkingEffort", sends[0].configID)
	})

	t.Run("mode_and_model_categories", func(t *testing.T) {
		conn := newLiveConnForConfigTest("resolve-mode-model")
		// A hypothetical agent advertising non-default ids for all categories.
		conn.SetWireConfigIDsForTest(map[string]string{
			"mode":          "sessionMode",
			"thought_level": "effort",
			"model":         "selectedModel",
		})
		rec := installConfigSendRecorder(t, conn)
		ctx := context.Background()

		conn.SetSessionConfigOption(ctx, "mode", "plan")
		conn.SetSessionConfigOption(ctx, "thought_level", "high")
		conn.SetSessionConfigOption(ctx, "model", "claude-sonnet-4-6")

		sends := rec.all()
		require.Len(t, sends, 3)
		assert.Equal(t, "sessionMode", sends[0].configID)
		assert.Equal(t, "effort", sends[1].configID)
		assert.Equal(t, "selectedModel", sends[2].configID)
		assert.Equal(t, "plan", conn.GetCurrentModeID())
		assert.Equal(t, "high", conn.GetCurrentThinkingEffortID())
		assert.Equal(t, "claude-sonnet-4-6", conn.GetCurrentModelID())
	})

	t.Run("unknown_category_passthrough", func(t *testing.T) {
		conn := newLiveConnForConfigTest("resolve-unknown-cat")
		conn.SetWireConfigIDsForTest(map[string]string{"thought_level": "effort"})
		rec := installConfigSendRecorder(t, conn)

		conn.SetSessionConfigOption(context.Background(), "customOption", "x")

		sends := rec.all()
		require.Len(t, sends, 1)
		assert.Equal(t, "customOption", sends[0].configID, "unknown categories pass through literally")
	})
}

// Dedup and the unsupported-config circuit breaker must key on the resolved
// wire id so a rejection under one spelling cannot be bypassed by sending
// under another (the original issue #429 failure mode), and so the agent's
// "effort" option is not sent repeatedly as if it were always fresh.
func TestSetSessionConfigOption_WireIDDedup(t *testing.T) {
	t.Run("same_value_skipped_across_spellings", func(t *testing.T) {
		conn := newLiveConnForConfigTest("dedup-effort")
		conn.SetWireConfigIDsForTest(map[string]string{"thought_level": "effort"})
		rec := installConfigSendRecorder(t, conn)
		ctx := context.Background()

		conn.SetSessionConfigOption(ctx, "thought_level", "high")
		conn.SetSessionConfigOption(ctx, "thinkingEffort", "high")

		sends := rec.all()
		require.Len(t, sends, 1, "same resolved value+id must dedup across spellings")
	})

	t.Run("changed_value_sent_again", func(t *testing.T) {
		conn := newLiveConnForConfigTest("dedup-effort-change")
		conn.SetWireConfigIDsForTest(map[string]string{"thought_level": "effort"})
		rec := installConfigSendRecorder(t, conn)
		ctx := context.Background()

		conn.SetSessionConfigOption(ctx, "thinkingEffort", "high")
		conn.SetSessionConfigOption(ctx, "thinkingEffort", "low")

		sends := rec.all()
		require.Len(t, sends, 2)
		assert.Equal(t, []string{"high", "low"}, []string{sends[0].value, sends[1].value})
	})
}

func TestSetSessionConfigOption_UnsupportedKeyedOnWireID(t *testing.T) {
	conn := newLiveConnForConfigTest("unsupported-effort")
	conn.SetWireConfigIDsForTest(map[string]string{"thought_level": "effort"})

	// Mark the resolved wire id as rejected by the agent (as setSessionConfigOption
	// does on "Unknown config option"). A later send under the legacy spelling
	// must also be suppressed — it resolves to the same wire id.
	conn.lastSetConfigMu.Lock()
	conn.unsupportedConfigs = map[string]bool{"effort": true}
	conn.lastSetConfigMu.Unlock()
	rec := installConfigSendRecorder(t, conn)

	conn.SetSessionConfigOption(context.Background(), "thinkingEffort", "max")

	assert.Empty(t, rec.all(), "unsupported (wire-id-keyed) config must not be re-sent under any spelling")
}

// A boolean option must use the boolean wire variant and share the same
// dedup/cache bookkeeping as a select. The test hook records the string form,
// which is also what the session cache stores.
func TestSetSessionConfigOptionBool(t *testing.T) {
	conn := newLiveConnForConfigTest("bool-config")
	rec := installConfigSendRecorder(t, conn)

	conn.SetSessionConfigOptionBool(context.Background(), "fast-mode", true)
	sent := rec.all()
	require.Len(t, sent, 1)
	assert.Equal(t, "fast-mode", sent[0].configID)
	assert.Equal(t, "true", sent[0].value)
	assert.Equal(t, "true", conn.GetCurrentSelection("fast-mode"))

	// The same value again is deduped.
	conn.SetSessionConfigOptionBool(context.Background(), "fast-mode", true)
	assert.Len(t, rec.all(), 1)

	conn.SetSessionConfigOptionBool(context.Background(), "fast-mode", false)
	sent = rec.all()
	require.Len(t, sent, 2)
	assert.Equal(t, "false", sent[1].value)
	assert.Equal(t, "false", conn.GetCurrentSelection("fast-mode"))
}

// ---------------------------------------------------------------------------
// Turn-start application of agent-declared config options (issue #746)
// ---------------------------------------------------------------------------

// A pick must reach the agent before the prompt, using the id the agent itself
// advertised and the right RPC form: a boolean option sent as a string value is
// rejected. Options this model does not advertise are skipped — a pick can
// outlive the model it was made under, and sending it would fail every turn.
func TestApplyExtraConfigOptionsAtTurn_SendsAdvertisedOnly(t *testing.T) {
	conn := newLiveConnForConfigTest("prompt-extra")
	conn.SetExtraConfigOptions([]ExtraConfigOption{
		{
			ConfigID: "fast-mode", Name: "Fast mode", Category: "model_config", Type: "select", CurrentID: "off",
			Values: []ConfigOptionValue{{ID: "off"}, {ID: "on"}},
		},
		{ConfigID: "fast-toggle", Name: "Fast toggle", Category: "model_config", Type: "boolean", CurrentID: "false"},
	})
	rec := installConfigSendRecorder(t, conn)

	err := conn.applyExtraConfigOptionsAtTurn(context.Background(), "acp-sid", map[string]string{
		"fast-mode":   "on",
		"fast-toggle": "true",
		"gone-option": "x", // not advertised for this model
	})
	require.NoError(t, err)

	sent := rec.all()
	require.Len(t, sent, 2, "only advertised options may be sent")
	assert.Equal(t, "fast-mode", sent[0].configID)
	assert.Equal(t, "on", sent[0].value)
	assert.Equal(t, "fast-toggle", sent[1].configID)
	assert.Equal(t, "true", sent[1].value)

	// The session cache follows immediately, so the drawer shows the pick
	// without waiting for the agent's notification.
	assert.Equal(t, "on", conn.GetCurrentSelection("fast-mode"))
	assert.Equal(t, "true", conn.GetCurrentSelection("fast-toggle"))
}

// The same pick applied twice must cost one RPC: every turn runs this path, and
// re-sending an unchanged value would add a round trip to each turn.
func TestApplyExtraConfigOptionsAtTurn_SkipsUnchanged(t *testing.T) {
	conn := newLiveConnForConfigTest("prompt-extra-dedup")
	conn.SetExtraConfigOptions([]ExtraConfigOption{
		{ConfigID: "fast-mode", Name: "Fast mode", Category: "model_config", Type: "select", CurrentID: "off"},
	})
	rec := installConfigSendRecorder(t, conn)

	picks := map[string]string{"fast-mode": "on"}
	require.NoError(t, conn.applyExtraConfigOptionsAtTurn(context.Background(), "acp-sid", picks))
	require.NoError(t, conn.applyExtraConfigOptionsAtTurn(context.Background(), "acp-sid", picks))

	assert.Len(t, rec.all(), 1, "an unchanged value must not be re-sent")
}

// A brand-new session may reach Prompt with no list on the connection (the
// session/new response omitted configOptions, or a resume did). The agent-level
// cache already knows what this model offers, so the pick must still be applied
// — otherwise a value inherited from a previous session is never sent and the
// drawer shows one thing while the agent runs another (issue #746).
func TestApplyExtraConfigOptionsAtTurn_FallsBackToRegistry(t *testing.T) {
	agent := &model.Agent{ID: "prompt-extra-fallback", Backend: "codex", AcpCommand: "codex"}
	conn := newACPConn(agent, "prompt-extra-fallback-sid")
	conn.SetAliveForTest()
	conn.SetSessionMappingForTest("prompt-extra-fallback-sid", "acp-sid")
	conn.UpdateCachedCurrent("model", "m1")

	reg := GetAgentCapabilityRegistry()
	reg.ResetForTest(agent.ID)
	t.Cleanup(func() { reg.ResetForTest(agent.ID) })
	reg.UpdateExtraConfigOptions(agent.ID, "m1", []ExtraConfigOption{
		{ConfigID: "fast-mode", Name: "Fast mode", Category: "model_config", Type: "select"},
	})

	rec := installConfigSendRecorder(t, conn)
	require.NoError(t, conn.applyExtraConfigOptionsAtTurn(context.Background(), "acp-sid", map[string]string{"fast-mode": "on"}))

	sent := rec.all()
	require.Len(t, sent, 1)
	assert.Equal(t, "fast-mode", sent[0].configID)
	assert.Equal(t, "on", sent[0].value)
}
