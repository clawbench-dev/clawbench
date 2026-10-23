package ai

import (
	"os"
	"strings"
	"testing"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
)

// ---------------------------------------------------------------------------
// Cancelled-turn usage
//
// A prompt cancelled mid-turn never receives its PromptResponse, so the
// metadata event that normally carries the turn's tokens and credit is never
// emitted. The agent has already billed for every model call it made, and the
// per-turn accumulator still holds those figures — they must be forwarded
// before the accumulator is discarded.
//
// Measured on a production DB before this: 1672 cancelled assistant messages,
// 1287 with an all-zero metadata row and only 11 carrying real usage.
// ---------------------------------------------------------------------------

// TestEmitCancelledTurnUsage_EmitsAccumulatedUsage is the core regression: a
// turn with accumulated model-call usage must produce a metadata event.
func TestEmitCancelledTurnUsage_EmitsAccumulatedUsage(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	conn := &ACPConn{agent: &model.Agent{ID: "cb", Backend: "codebuddy"}}

	// Two model calls billed before the cancel landed (the shape CodeBuddy
	// reports: one usage_update per call, summed by the accumulator).
	for _, call := range []struct {
		msgID  string
		prompt int
		credit float64
	}{
		{"msg-1", 38229, 1.93},
		{"msg-2", 38328, 1.92},
	} {
		conn.mergeMetaExtraction(extractCodeBuddyMeta(map[string]any{
			metaKeyCodeBuddyMessageID: call.msgID,
			metaKeyCodeBuddyRequestID: "req-cancel",
			metaKeyCodeBuddyUsage: map[string]any{
				"prompt_tokens":     call.prompt,
				"completion_tokens": 10,
				"total_tokens":      call.prompt + 10,
				"credit":            call.credit,
			},
		}))
	}

	conn.emitCancelledTurnUsage(ch)
	close(ch)

	var got *StreamEvent
	for ev := range ch {
		if ev.Type == "metadata" {
			got = &ev
		}
	}
	require.NotNil(t, got, "a cancelled turn that consumed usage must emit metadata")
	require.NotNil(t, got.Meta)
	assert.Equal(t, 38229+38328, got.Meta.InputTokens,
		"the turn total is the sum over every billed model call")
	assert.Equal(t, 20, got.Meta.OutputTokens)
	assert.InDelta(t, 1.93+1.92, got.Meta.Credit, 1e-9)
	assert.Equal(t, string(acp.StopReasonCancelled), got.Meta.StopReason,
		"the record must say the turn was cancelled, not look like a truncated success")
}

// A turn cancelled before any model call consumed nothing. Emitting an
// all-zero metadata row would only inflate the stats panel's message count.
func TestEmitCancelledTurnUsage_SilentWhenNothingConsumed(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	conn := &ACPConn{agent: &model.Agent{ID: "cb", Backend: "codebuddy"}}

	conn.emitCancelledTurnUsage(ch)
	close(ch)

	for ev := range ch {
		assert.NotEqual(t, "metadata", ev.Type,
			"a cancel with no observed usage must not emit a metadata event")
	}
}

// The accumulator is consumed: the next turn must not inherit this one's usage.
func TestEmitCancelledTurnUsage_ClearsAccumulator(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	conn := &ACPConn{agent: &model.Agent{ID: "cb", Backend: "codebuddy"}}
	conn.mergeMetaExtraction(extractCodeBuddyMeta(map[string]any{
		metaKeyCodeBuddyMessageID: "msg-1",
		"usage":                   map[string]any{"prompt_tokens": 500, "completion_tokens": 5},
	}))

	conn.emitCancelledTurnUsage(ch)
	close(ch)

	assert.Nil(t, conn.getMetaAccum(),
		"the cancelled turn's usage must not leak into the next turn")
}

// A cancelled turn still identifies itself: without promoting its requestId the
// replay filter keeps the PREVIOUS turn's baseline.
func TestEmitCancelledTurnUsage_RecordsCompletedRequestID(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	conn := &ACPConn{agent: &model.Agent{ID: "cb", Backend: "codebuddy"}}
	conn.setLastCompletedRequestID("prev-turn-rid")
	conn.mergeMetaExtraction(extractCodeBuddyMeta(map[string]any{
		metaKeyCodeBuddyMessageID: "msg-1",
		metaKeyCodeBuddyRequestID: "cancelled-turn-rid",
		"usage":                   map[string]any{"prompt_tokens": 500, "completion_tokens": 5},
	}))

	conn.emitCancelledTurnUsage(ch)
	close(ch)

	assert.Equal(t, "cancelled-turn-rid", conn.getLastCompletedRequestID())
}

// ---------------------------------------------------------------------------
// MergeMetadata
// ---------------------------------------------------------------------------

// A drained metadata event may only fill gaps. Substituting it would drop
// WallMs and the session-level model/transport buildResult already wrote.
func TestMergeMetadata_FillsEmptyFieldsOnly(t *testing.T) {
	dst := &Metadata{Model: "kept", WallMs: 1234, InputTokens: 100}
	src := &Metadata{
		Model:        "ignored",
		WallMs:       9999,
		InputTokens:  0, // a drained event with no input must not erase the recorded one
		OutputTokens: 50,
		Credit:       2.5,
	}

	MergeMetadata(dst, src)

	assert.Equal(t, "kept", dst.Model)
	assert.Equal(t, 1234, dst.WallMs)
	assert.Equal(t, 100, dst.InputTokens, "a zero in src must never erase dst")
	assert.Equal(t, 50, dst.OutputTokens)
	assert.InDelta(t, 2.5, dst.Credit, 1e-9)
}

// A cumulative cost must travel with its flag: the ledger diffs it against the
// previous turn's total, and storing it without the flag counts it once per
// message.
func TestMergeMetadata_CumulativeCostCarriesFlag(t *testing.T) {
	dst := &Metadata{}
	src := &Metadata{CostUSD: 1.75, CostSessionTotal: 4.5, CostCumulative: true}

	MergeMetadata(dst, src)

	assert.InDelta(t, 1.75, dst.CostUSD, 1e-9)
	assert.InDelta(t, 4.5, dst.CostSessionTotal, 1e-9)
	assert.True(t, dst.CostCumulative,
		"a filled cumulative amount without the flag would be stored verbatim")
}

func TestMergeMetadata_NilSafe(t *testing.T) {
	MergeMetadata(nil, &Metadata{InputTokens: 1})
	MergeMetadata(&Metadata{}, nil)
}

// The helper being correct is not enough — it must be CALLED on the cancel
// path. Deleting the call site leaves every unit test above green (they invoke
// the helper directly), so a source guard is the only thing that catches it.
func TestPrompt_CancelPathEmitsUsage(t *testing.T) {
	src, err := os.ReadFile("acp_conn_prompt.go")
	if err != nil {
		t.Fatalf("read source: %v", err)
	}
	body := string(src)

	// The cancel branch must call the helper BEFORE returning ctx.Err(), or the
	// turn's usage never reaches the ledger.
	cancelIdx := strings.Index(body, `slog.Info("acp conn: prompt cancelled"`)
	if cancelIdx < 0 {
		t.Fatal("the prompt-cancel branch is gone; this guard needs updating")
	}
	rest := body[cancelIdx:]
	end := strings.Index(rest, "return ctx.Err()")
	if end < 0 {
		t.Fatal("the prompt-cancel branch no longer returns ctx.Err()")
	}
	if !strings.Contains(rest[:end], "c.emitCancelledTurnUsage(streamCh)") {
		t.Fatal("the prompt-cancel branch must call emitCancelledTurnUsage; " +
			"without it a cancelled turn's tokens and credit are never persisted")
	}
}

// Agents that bill in money (Claude, OpenCode) carry the turn's cost on the
// final usage_update, not on the accumulator. A cancelled turn must still
// record it — and must record it as CUMULATIVE so the ledger diffs it against
// the previous turn instead of storing the running total per message.
func TestEmitCancelledTurnUsage_RecordsCachedCost(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	conn := &ACPConn{agent: &model.Agent{ID: "cl", Backend: "claude"}}
	conn.SetCachedUsageState(&UsageState{Used: 100, Size: 200000, Cost: 0.1568, Currency: "USD"})

	conn.emitCancelledTurnUsage(ch)
	close(ch)

	var got *StreamEvent
	for ev := range ch {
		if ev.Type == "metadata" {
			got = &ev
		}
	}
	require.NotNil(t, got, "a cancelled turn that accrued cost must emit metadata")
	assert.InDelta(t, 0.1568, got.Meta.CostUSD, 1e-9)
	assert.True(t, got.Meta.CostCumulative,
		"ACP cost is a session running total; without the flag the ledger sums it per message")
	assert.InDelta(t, 0.1568, got.Meta.CostSessionTotal, 1e-9)
	assert.Equal(t, string(acp.StopReasonCancelled), got.Meta.StopReason)
}

// CodeBuddy repurposes the same cost field to mean credit. It must not reach
// CostUSD (which feeds chat_metadata.cost_usd) — that would record a credit
// figure as dollars.
func TestEmitCancelledTurnUsage_CodeBuddyCostIsNotMoney(t *testing.T) {
	ch := make(chan StreamEvent, 10)
	conn := &ACPConn{agent: &model.Agent{ID: "cb", Backend: "codebuddy"}}
	conn.SetCachedUsageState(&UsageState{Used: 100, Size: 200000, Cost: 11.15})

	conn.emitCancelledTurnUsage(ch)
	close(ch)

	for ev := range ch {
		if ev.Type == "metadata" {
			assert.Zero(t, ev.Meta.CostUSD,
				"CodeBuddy's cost field carries credit; it must not be persisted as money")
		}
	}
}
