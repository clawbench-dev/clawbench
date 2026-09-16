package ai

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	acp "github.com/coder/acp-go-sdk"

	"clawbench/internal/model"
)

// --- looksLikeAgentNoRun ---

// The production incident this guards against: CodeBuddy returned a normal
// PromptResponse (stopReason=end_turn, no transport error) for a real user
// prompt, but never called the model — zero content and zero tokens.
func TestLooksLikeAgentNoRun_EndTurnWithNoOutputAndNoTokens(t *testing.T) {
	assert.True(t, looksLikeAgentNoRun("end_turn", 0, 0, 0, 0),
		"end_turn + no output + no tokens means the model was never called")
}

// A real (if useless) model answer still bills input tokens. Only a turn that
// touched no model has both output and usage at zero, so nonzero usage must
// suppress the classification — otherwise every empty-but-real answer would be
// misreported as a connection failure and needlessly respawn the agent.
func TestLooksLikeAgentNoRun_RealEmptyAnswerKeepsEmptyReason(t *testing.T) {
	assert.False(t, looksLikeAgentNoRun("end_turn", 0, 1200, 0, 1200),
		"a billed turn that answered nothing is a genuine empty response, not a skipped turn")
}

// Any model output event (text, thinking, tool call) proves the model ran, even
// if the agent reported no token usage.
func TestLooksLikeAgentNoRun_OutputEventsDisqualify(t *testing.T) {
	assert.False(t, looksLikeAgentNoRun("end_turn", 1, 0, 0, 0))
	assert.False(t, looksLikeAgentNoRun("end_turn", 7, 0, 0, 0))
}

// Only end_turn indicates a completed turn. cancelled/refusal already have
// their own reasons and must not be reclassified.
func TestLooksLikeAgentNoRun_OnlyEndTurn(t *testing.T) {
	for _, sr := range []string{"cancelled", "refusal", "", "max_tokens"} {
		assert.False(t, looksLikeAgentNoRun(sr, 0, 0, 0, 0),
			"stop reason %q must not be treated as a skipped turn", sr)
	}
}

// Output tokens alone also indicate real work (e.g. a thinking-only turn).
func TestLooksLikeAgentNoRun_OutputTokensDisqualify(t *testing.T) {
	assert.False(t, looksLikeAgentNoRun("end_turn", 0, 0, 42, 42))
}

// --- agentNoRunError ---

func TestAgentNoRunError_Detection(t *testing.T) {
	err := &agentNoRunError{stopReason: "end_turn", wallMs: 131}
	assert.True(t, isAgentNoRun(err))
	assert.Contains(t, err.Error(), "end_turn")
	assert.Contains(t, err.Error(), "131")
	assert.Equal(t, "end_turn", err.StopReason())
}

// The typed error must not swallow unrelated failures into the retry path.
func TestAgentNoRunError_DoesNotMatchOtherErrors(t *testing.T) {
	assert.False(t, isAgentNoRun(nil))
	assert.False(t, isAgentNoRun(errConfigKilledConnection("model", "x")))
	assert.False(t, isAgentNoRun(assertErr{}))
}

// --- per-turn output tracking ---

func TestACPConn_TurnOutputCounter(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "a1", Backend: "acp-stdio"}, "s1")

	assert.Equal(t, int64(0), conn.TurnOutputEvents(), "a fresh connection has produced nothing")

	conn.RecordTurnOutput()
	conn.RecordTurnOutput()
	conn.RecordTurnOutput()
	assert.Equal(t, int64(3), conn.TurnOutputEvents())

	// A new turn must not inherit the previous turn's count, otherwise a
	// skipped turn following a productive one would look like it ran.
	conn.ResetTurnOutput()
	assert.Equal(t, int64(0), conn.TurnOutputEvents())
}

// --- promptResponseTokens ---

// CodeBuddy reports no PromptResponse.Usage at all (has_usage=false) and carries
// its counters on session/update _meta. Reading only the Usage field would make
// a perfectly normal CodeBuddy turn look like it billed nothing.
func TestPromptResponseTokens_FallsBackToAccumulatedMeta(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "codebuddy", Backend: "codebuddy"}, "s1")
	conn.mergeMetaExtraction(&metaExtraction{
		Usage: &metaTokenUsage{InputTokens: 216509, OutputTokens: 664, TotalTokens: 217173, Present: true},
	})

	resp := acp.PromptResponse{StopReason: acp.StopReasonEndTurn}
	in, out, total := promptResponseTokens(resp, conn)
	assert.Equal(t, 216509, in)
	assert.Equal(t, 664, out)
	assert.Equal(t, 217173, total)

	// With real usage present the turn must not be classified as skipped.
	assert.False(t, looksLikeAgentNoRun(string(resp.StopReason), 0, in, out, total))
}

// The PromptResponse usage wins when it carries more information than the
// accumulator (e.g. an agent that reports both).
func TestPromptResponseTokens_PrefersLargerSource(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "a1", Backend: "acp-stdio"}, "s1")
	conn.mergeMetaExtraction(&metaExtraction{
		Usage: &metaTokenUsage{InputTokens: 10, OutputTokens: 2, TotalTokens: 12, Present: true},
	})

	resp := acp.PromptResponse{
		StopReason: acp.StopReasonEndTurn,
		Usage:      &acp.Usage{InputTokens: 500, OutputTokens: 40},
	}
	in, out, total := promptResponseTokens(resp, conn)
	assert.Equal(t, 500, in)
	assert.Equal(t, 40, out)
	assert.Equal(t, 540, total)
}

func TestPromptResponseTokens_EmptyWhenNothingReported(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "a1", Backend: "acp-stdio"}, "s1")
	in, out, total := promptResponseTokens(acp.PromptResponse{}, conn)
	assert.Equal(t, 0, in)
	assert.Equal(t, 0, out)
	assert.Equal(t, 0, total)
}

// peekMetaAccumUsage must not clear the accumulator — emitPromptTailMetadata
// consumes it afterwards to persist the turn's metadata.
func TestPeekMetaAccumUsage_DoesNotConsume(t *testing.T) {
	conn := newACPConn(&model.Agent{ID: "codebuddy", Backend: "codebuddy"}, "s1")
	conn.mergeMetaExtraction(&metaExtraction{
		Usage: &metaTokenUsage{InputTokens: 100, OutputTokens: 5, TotalTokens: 105, Present: true},
	})

	in, out, total := conn.peekMetaAccumUsage()
	assert.Equal(t, 100, in)
	assert.Equal(t, 5, out)
	assert.Equal(t, 105, total)

	// Still available for the metadata event.
	require.NotNil(t, conn.getAndClearMetaAccum(), "peek must leave the accumulator intact")
	in2, _, _ := conn.peekMetaAccumUsage()
	assert.Equal(t, 0, in2, "the accumulator is empty only after the explicit clear")
}

// assertErr is a plain error for negative-matching tests.
type assertErr struct{}

func (assertErr) Error() string { return "some other failure" }

// --- promptText ---

func TestPromptText_ConcatenatesTextBlocks(t *testing.T) {
	blocks := []acp.ContentBlock{
		acp.TextBlock("hello "),
		{Image: &acp.ContentBlockImage{Data: "x", MimeType: "image/png"}},
		acp.TextBlock("world"),
	}
	assert.Equal(t, "hello world", promptText(blocks))
}

func TestPromptText_EmptyForNoTextBlocks(t *testing.T) {
	assert.Equal(t, "", promptText(nil))
	assert.Equal(t, "", promptText([]acp.ContentBlock{
		{Image: &acp.ContentBlockImage{Data: "x", MimeType: "image/png"}},
	}))
}

// A slash command must be recognized from the built prompt so the
// no-model-output check can exempt it: ACP agents route slash commands to their
// own CommandExecutor, so "the model never ran" is expected there and must not
// trigger a respawn.
func TestPromptText_SlashCommandDetection(t *testing.T) {
	cmdBlocks := []acp.ContentBlock{acp.TextBlock("/compact")}
	assert.True(t, IsACPSlashCommand(promptText(cmdBlocks)),
		"a slash command must be exempt from the no-model-output respawn")

	normalBlocks := []acp.ContentBlock{acp.TextBlock("不能直接取消掉indexed，然后让它自然重建，会有什么问题吗")}
	assert.False(t, IsACPSlashCommand(promptText(normalBlocks)),
		"a real user prompt must remain eligible for the respawn")
}
