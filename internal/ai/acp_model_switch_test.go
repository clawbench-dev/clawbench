package ai

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
)

// ---------------------------------------------------------------------------
// Model switch dispatch: session/set_model vs the "model" config option
// ---------------------------------------------------------------------------

// withModelSwitchMethod installs a LookupACPModelSwitchMethodFn returning method
// for the given backend id (and "" otherwise), restoring the previous value.
func withModelSwitchMethod(t *testing.T, backendID, method string) {
	t.Helper()
	prev := LookupACPModelSwitchMethodFn
	LookupACPModelSwitchMethodFn = func(id string) string {
		if id == backendID {
			return method
		}
		return ""
	}
	t.Cleanup(func() { LookupACPModelSwitchMethodFn = prev })
}

// rawCallRecord is one captured CallRaw invocation.
type rawCallRecord struct {
	method string
	params map[string]any
}

// rawCallRecorder captures raw calls and returns a canned response/error.
type rawCallRecorder struct {
	mu      sync.Mutex
	calls   []rawCallRecord
	result  json.RawMessage
	err     error
	blockCh chan struct{} // when non-nil, CallRaw waits for it (or ctx) before returning
}

func (r *rawCallRecorder) hook(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if r.blockCh != nil {
		select {
		case <-r.blockCh:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	m, _ := params.(map[string]any)
	r.mu.Lock()
	r.calls = append(r.calls, rawCallRecord{method: method, params: m})
	r.mu.Unlock()
	return r.result, r.err
}

func (r *rawCallRecorder) all() []rawCallRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]rawCallRecord, len(r.calls))
	copy(out, r.calls)
	return out
}

func newConnForModelSwitchTest(t *testing.T, backendID string) *ACPConn {
	t.Helper()
	agent := &model.Agent{ID: backendID + "-agent", Backend: backendID, AcpCommand: "echo"}
	conn := newACPConn(agent, backendID+"-agent")
	conn.SetAliveForTest()
	conn.SetSessionMappingForTest(backendID+"-agent", "acp-sid-"+backendID)
	return conn
}

// A backend that declares set_model must get exactly one raw session/set_model
// with the {sessionId, modelId} params — and must NOT also send the config option.
func TestApplyModelSelection_SetModelBackend(t *testing.T) {
	withModelSwitchMethod(t, "hermes", "session/set_model")
	conn := newConnForModelSwitchTest(t, "hermes")

	rec := &rawCallRecorder{}
	conn.SetRawCallFnForTest(rec.hook)
	configRec := installConfigSendRecorder(t, conn)

	require.NoError(t, conn.applyModelSelection(context.Background(), "acp-sid-hermes", "minimax:MiniMax-M3"))

	calls := rec.all()
	require.Len(t, calls, 1, "exactly one raw call")
	assert.Equal(t, "session/set_model", calls[0].method)
	assert.Equal(t, "acp-sid-hermes", calls[0].params["sessionId"])
	assert.Equal(t, "minimax:MiniMax-M3", calls[0].params["modelId"])

	assert.Empty(t, configRec.all(), "the model must not also go through the config-option path")
}

// A backend that declares nothing keeps the historical config-option path.
func TestApplyModelSelection_DefaultBackend(t *testing.T) {
	withModelSwitchMethod(t, "other", "")
	conn := newConnForModelSwitchTest(t, "other")

	rec := &rawCallRecorder{}
	conn.SetRawCallFnForTest(rec.hook)
	configRec := installConfigSendRecorder(t, conn)

	require.NoError(t, conn.applyModelSelection(context.Background(), "acp-sid-other", "gpt-4"))

	assert.Empty(t, rec.all(), "no raw call for the default path")
	sends := configRec.all()
	require.Len(t, sends, 1)
	assert.Equal(t, "model", sends[0].configID)
	assert.Equal(t, "gpt-4", sends[0].value)
}

// The selection is applied on every turn, so an unchanged value must be deduped
// (the set_model RPC can cost seconds).
func TestApplyModelViaSetModel_Dedup(t *testing.T) {
	withModelSwitchMethod(t, "hermes", "session/set_model")
	conn := newConnForModelSwitchTest(t, "hermes")
	rec := &rawCallRecorder{}
	conn.SetRawCallFnForTest(rec.hook)

	require.NoError(t, conn.applyModelSelection(context.Background(), "acp-sid-hermes", "m1"))
	require.NoError(t, conn.applyModelSelection(context.Background(), "acp-sid-hermes", "m1"))
	assert.Len(t, rec.all(), 1, "unchanged model must not be re-sent")

	require.NoError(t, conn.applyModelSelection(context.Background(), "acp-sid-hermes", "m2"))
	assert.Len(t, rec.all(), 2, "a changed model must be sent")
}

// A successful switch records the value (dedup baseline) and updates the cached
// current model so WS model state / GET /api/ai/chat report the user's choice.
func TestApplyModelViaSetModel_SuccessUpdatesState(t *testing.T) {
	withModelSwitchMethod(t, "hermes", "session/set_model")
	conn := newConnForModelSwitchTest(t, "hermes")
	conn.SetRawCallFnForTest((&rawCallRecorder{}).hook)

	require.NoError(t, conn.applyModelSelection(context.Background(), "acp-sid-hermes", "minimax:MiniMax-M3"))

	conn.lastSetConfigMu.Lock()
	last := conn.lastSetModel
	conn.lastSetConfigMu.Unlock()
	assert.Equal(t, "minimax:MiniMax-M3", last)
	assert.Equal(t, "minimax:MiniMax-M3", conn.GetCurrentModelID())
}

// A failed switch must NOT record the value, so the next turn retries instead of
// being permanently pinned to a model the user never got.
func TestApplyModelViaSetModel_FailureNotRecorded(t *testing.T) {
	withModelSwitchMethod(t, "hermes", "session/set_model")
	conn := newConnForModelSwitchTest(t, "hermes")

	rec := &rawCallRecorder{err: &RawRPCError{Code: -32602, Message: "Invalid params"}}
	conn.SetRawCallFnForTest(rec.hook)

	err := conn.applyModelSelection(context.Background(), "acp-sid-hermes", "copilot:gpt-5.4")
	require.Error(t, err)

	conn.lastSetConfigMu.Lock()
	last := conn.lastSetModel
	conn.lastSetConfigMu.Unlock()
	assert.Empty(t, last, "a failed switch must not be recorded as applied")

	// The retry happens because nothing was recorded.
	require.Error(t, conn.applyModelSelection(context.Background(), "acp-sid-hermes", "copilot:gpt-5.4"))
	assert.Len(t, rec.all(), 2, "the failed model is retried on the next turn")
}

// CallRaw has no timeout of its own and this call runs before the stall
// watchdog exists, so a hung agent must still be bounded.
func TestApplyModelViaSetModel_Timeout(t *testing.T) {
	prev := modelSwitchTimeout
	modelSwitchTimeout = 50 * time.Millisecond
	t.Cleanup(func() { modelSwitchTimeout = prev })

	withModelSwitchMethod(t, "hermes", "session/set_model")
	conn := newConnForModelSwitchTest(t, "hermes")

	blocked := make(chan struct{})
	// Safety net: if the production code ever loses its timeout, the hook would
	// block forever. This hook releases after a hard cap and returns a distinct
	// error, so the test FAILS fast instead of hanging the suite.
	conn.SetRawCallFnForTest(func(ctx context.Context, _ string, _ any) (json.RawMessage, error) {
		select {
		case <-blocked:
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(3 * time.Second):
			return nil, errors.New("hook was never unblocked (production timeout missing?)")
		}
		return nil, nil
	})

	start := time.Now()
	err := conn.applyModelSelection(context.Background(), "acp-sid-hermes", "m1")
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, elapsed, 2*time.Second, "the call must be bounded by the switch timeout")
}

// The resume path must use the declared method too, else a respawn silently
// reverts the model. It runs under c.mu, so it must unlock around the raw call —
// this test would deadlock (or race-detect) otherwise.
func TestReapplyModelSelection_SetModelBackend(t *testing.T) {
	withModelSwitchMethod(t, "hermes", "session/set_model")
	conn := newConnForModelSwitchTest(t, "hermes")

	rec := &rawCallRecorder{}
	conn.SetRawCallFnForTest(rec.hook)
	configRec := installConfigSendRecorder(t, conn)

	conn.mu.Lock()
	conn.reapplyConfigAfterResume(context.Background(), "acp-sid-hermes", cachedConfigSnapshot{
		mode:  "code",
		model: "minimax:MiniMax-M3",
	})
	conn.mu.Unlock()

	calls := rec.all()
	require.Len(t, calls, 1)
	assert.Equal(t, "session/set_model", calls[0].method)
	assert.Equal(t, "minimax:MiniMax-M3", calls[0].params["modelId"])
	// mode still re-applies through the config path; model must NOT.
	for _, s := range configRec.all() {
		assert.NotEqual(t, "model", s.configID, "model must not be re-applied via the config-option path")
	}

	conn.lastSetConfigMu.Lock()
	defer conn.lastSetConfigMu.Unlock()
	assert.Equal(t, "minimax:MiniMax-M3", conn.lastSetModel)
}

// The default backend's resume path still re-applies the model via the config
// option (regression guard for the existing behavior).
func TestReapplyModelSelection_DefaultBackend(t *testing.T) {
	withModelSwitchMethod(t, "other", "")
	conn := newConnForModelSwitchTest(t, "other")

	rec := &rawCallRecorder{}
	conn.SetRawCallFnForTest(rec.hook)
	configRec := installConfigSendRecorder(t, conn)

	conn.mu.Lock()
	conn.reapplyConfigAfterResume(context.Background(), "acp-sid-other", cachedConfigSnapshot{model: "gpt-4"})
	conn.mu.Unlock()

	assert.Empty(t, rec.all())
	sends := configRec.all()
	require.Len(t, sends, 1)
	assert.Equal(t, "model", sends[0].configID)
	assert.Equal(t, "gpt-4", sends[0].value)
}

// ---------------------------------------------------------------------------
// Error surfacing (raw channel → user-visible warning text)
// ---------------------------------------------------------------------------

// Hermes puts the actionable cause in data.details. The frontend renders only
// the text for a request_failed warning, so details must be folded into it.
func TestACPRawRPCErrorText_PrefersDetails(t *testing.T) {
	err := &RawRPCError{
		Code:    -32602,
		Message: "Invalid params",
		Data:    json.RawMessage(`{"details":"GitHub Copilot is not connected: add one with hermes auth add github-copilot"}`),
	}

	text := acpErrorText(err)
	assert.Contains(t, text, "GitHub Copilot is not connected")
	assert.Contains(t, text, "hermes auth add github-copilot")
	assert.Contains(t, text, "-32602")

	code, httpStatus := acpErrorDetails(err)
	assert.Equal(t, -32602, code)
	assert.Equal(t, 0, httpStatus)
}

// Without a details payload the generic message is still surfaced with its code.
func TestACPRawRPCErrorText_FallsBackToMessage(t *testing.T) {
	err := &RawRPCError{Code: -32603, Message: "Session is busy"}
	assert.Equal(t, "ACP error -32603: Session is busy", acpErrorText(err))
	code, _ := acpErrorDetails(err)
	assert.Equal(t, -32603, code)
}

// A wrapped raw error is still recognized (errors.As).
func TestACPRawRPCErrorText_Wrapped(t *testing.T) {
	inner := &RawRPCError{Code: -32602, Message: "Invalid params"}
	err := errors.Join(errors.New("acp: set_model failed"), inner)
	assert.Contains(t, acpErrorText(err), "-32602")
	code, _ := acpErrorDetails(err)
	assert.Equal(t, -32602, code)
}
