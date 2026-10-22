//go:build integration

package ai

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
)

// TestCodebuddyACP_TeamAgentCardTitleProbe reproduces the user-visible defect
// end to end: run a real team spawn, feed the stream events through
// AccumulateBlock (exactly what SessionExecutor does), and dump the resulting
// blocks.
//
// Question it answers: does the Agent spawn card end up with
// display_name="general-purpose" (the subagent TYPE) while the member's actual
// name is available on the same block?
//
// Run:
//
//	go test -v -run 'TestCodebuddyACP_TeamAgentCardTitleProbe' -tags integration \
//	    -timeout 600s ./internal/ai/
func TestCodebuddyACP_TeamAgentCardTitleProbe(t *testing.T) {
	ctx, cancel := contextWithTimeout(t, 540*time.Second)
	defer cancel()

	setup := startTeamProbe(t, ctx)
	defer setup.stopEventPump()

	if _, err := setup.prompt(ctx, 180*time.Second, teamProbeCreatePrompt); err != nil {
		t.Logf("NOTE: create prompt error: %v", err)
	}
	time.Sleep(2 * time.Second)

	prevEvents := len(setup.streamEvents())
	if _, err := setup.prompt(ctx, 300*time.Second, teamProbeSpawnPrompt); err != nil {
		t.Logf("NOTE: spawn prompt error: %v", err)
	}
	time.Sleep(3 * time.Second)

	// Replay the stream through the real accumulator, in wire order.
	var blocks []model.ContentBlock
	for _, ev := range setup.streamEvents()[prevEvents:] {
		switch ev.Type {
		case "content", "thinking", "thinking_done", "tool_use", "tool_result":
			AccumulateBlock(&blocks, ev)
		}
	}

	t.Logf("=== accumulated blocks: %d ===", len(blocks))
	var agentCards int
	for i := range blocks {
		b := &blocks[i]
		if b.Type != "tool_use" {
			continue
		}
		input, _ := json.Marshal(b.Input)
		t.Logf("  name=%q display_name=%q member=%q member_color=%q parent=%q id=%s",
			b.Name, b.DisplayName, b.MemberName, b.MemberColor, b.ParentToolCallID, b.ID)
		if b.Name == "Agent" || b.Name == "@probe-alpha" || b.Name == "@probe-beta" {
			agentCards++
		}
		_ = input
	}

	t.Logf("=== Agent-family cards: %d ===", agentCards)
	require.NotZero(t, agentCards, "the spawn phase must produce Agent cards")
}
