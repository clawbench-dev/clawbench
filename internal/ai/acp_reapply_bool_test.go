package ai

import (
	"context"
	"testing"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
)

// TestReapplyConfigAfterResume_BooleanExtraUsesBooleanWireForm is the regression
// guard for "Multitask 关掉后，恢复会话又自己打开了".
//
// Measured against a live codebuddy: session/set_config_option with
// `value: "false"` on the `multitask` option is refused with -32602
// ("Invalid value type for config option multitask: expected boolean"), so the
// resume path's re-apply silently failed and the agent kept its own default.
// The turn-start path already types the request from the advertised option
// (applyExtraConfigOptionsAtTurn); the resume path must do the same.
func TestReapplyConfigAfterResume_BooleanExtraUsesBooleanWireForm(t *testing.T) {
	agent := &model.Agent{ID: "test-reapply-bool", Backend: "codebuddy", AcpCommand: "echo"}
	conn := newACPConn(agent, "test-reapply-bool")
	conn.SetAliveForTest()
	conn.SetSessionMappingForTest("test-reapply-bool", "acp-sid-bool")
	conn.SetExtraConfigOptions([]ExtraConfigOption{
		{ConfigID: "multitask", Type: "boolean"},
		{ConfigID: "sandbox", Type: "select"},
	})

	var got []acp.SetSessionConfigOptionRequest
	conn.SetConfigOptionRequestFnForTest(func(_ context.Context, _ string, req acp.SetSessionConfigOptionRequest) error {
		got = append(got, req)
		return nil
	})

	conn.mu.Lock()
	conn.reapplyConfigAfterResume(context.Background(), "acp-sid-bool", cachedConfigSnapshot{
		extras: map[string]string{"multitask": "false", "sandbox": "false"},
	})
	conn.mu.Unlock()

	require.Len(t, got, 2, "both extras must be re-applied")

	// Sorted order: multitask, sandbox.
	mt := got[0]
	require.NotNil(t, mt.Boolean, "multitask is boolean: the request must use the typed boolean variant")
	assert.False(t, mt.Boolean.Value)
	assert.Equal(t, "multitask", string(mt.Boolean.ConfigId))
	assert.Nil(t, mt.ValueId, "a boolean option must not go out as a string")

	sb := got[1]
	require.NotNil(t, sb.ValueId, "sandbox is a select option: it keeps the string variant")
	assert.Equal(t, acp.SessionConfigValueId("false"), sb.ValueId.Value)
	assert.Equal(t, "sandbox", string(sb.ValueId.ConfigId))
	assert.Nil(t, sb.Boolean)
}

// TestReapplyConfigAfterResume_NonBooleanValueForBooleanOptionIsSkipped pins the
// fail-safe: a stored value that cannot be expressed as a boolean must not be
// re-sent on every resume (it would fail every time), and must not fall back to
// the string form either.
func TestReapplyConfigAfterResume_NonBooleanValueForBooleanOptionIsSkipped(t *testing.T) {
	agent := &model.Agent{ID: "test-reapply-bool-bad", Backend: "codebuddy", AcpCommand: "echo"}
	conn := newACPConn(agent, "test-reapply-bool-bad")
	conn.SetAliveForTest()
	conn.SetSessionMappingForTest("test-reapply-bool-bad", "acp-sid-bool-bad")
	conn.SetExtraConfigOptions([]ExtraConfigOption{{ConfigID: "multitask", Type: "boolean"}})

	var got []acp.SetSessionConfigOptionRequest
	conn.SetConfigOptionRequestFnForTest(func(_ context.Context, _ string, req acp.SetSessionConfigOptionRequest) error {
		got = append(got, req)
		return nil
	})

	conn.mu.Lock()
	conn.reapplyConfigAfterResume(context.Background(), "acp-sid-bool-bad", cachedConfigSnapshot{
		extras: map[string]string{"multitask": "on"},
	})
	conn.mu.Unlock()

	assert.Empty(t, got, "a non-boolean value for a boolean option must not be sent at all")
}
