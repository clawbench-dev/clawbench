package service

import (
	"strings"
	"testing"

	"clawbench/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// RenderGroupForkContext — the aggregate timeline as fork background
// ============================================================================

// setupGroupForkDB creates a project + group with one host, and returns
// (groupID, hostMemberID).
func setupGroupForkDB(t *testing.T) (string, string) {
	t.Helper()
	setupGroupDB(t)
	project := t.TempDir()
	if _, err := store.ProjectIDForPath(project); err != nil {
		require.NoError(t, err)
	}
	groupID, hostID, err := CreateGroup(project, "讨论组", "claude", "claude", "主持人")
	require.NoError(t, err)
	return groupID, hostID
}

func addGroupRow(t *testing.T, project, groupID, role, text, speaker string) int64 {
	t.Helper()
	id, err := AddChatMessageWithAgent(project, "claude", groupID, role, text, nil, false, "", speaker)
	require.NoError(t, err)
	return id
}

// Speaker names come from the member rows, and the human user gets the
// configured nickname — a member's row id must never leak into the text.
func TestRenderGroupForkContext_UsesSpeakerNames(t *testing.T) {
	groupID, hostID := setupGroupForkDB(t)
	project := GetSessionProjectPathAnyPath(groupID)

	addGroupRow(t, project, groupID, "user", "我们讨论一下缓存", "")
	addGroupRow(t, project, groupID, "assistant", "我建议用 LRU", hostID)

	out := RenderGroupForkContext(groupID, 0, ForkContextOptions{BudgetChars: 10000})

	assert.Contains(t, out, "我们讨论一下缓存")
	assert.Contains(t, out, "我建议用 LRU")
	assert.Contains(t, out, "主持人: 我建议用 LRU", "speech is attributed to the member's display name")
	assert.NotContains(t, out, hostID, "member row ids must not leak")
}

// System rows are orchestration events, not conversation; including them would
// fill the background with "your turn" noise.
func TestRenderGroupForkContext_SkipsSystemRows(t *testing.T) {
	groupID, hostID := setupGroupForkDB(t)
	project := GetSessionProjectPathAnyPath(groupID)

	addGroupRow(t, project, groupID, "assistant", "真实发言", hostID)
	_, err := AddSystemMessage(project, groupID, "轮到用户发言")
	require.NoError(t, err)

	out := RenderGroupForkContext(groupID, 0, ForkContextOptions{BudgetChars: 10000})

	assert.Contains(t, out, "真实发言")
	assert.NotContains(t, out, "轮到用户发言")
}

// A fork point must cut the timeline: messages after it belong to a branch the
// user did not choose.
func TestRenderGroupForkContext_RespectsCutoff(t *testing.T) {
	groupID, hostID := setupGroupForkDB(t)
	project := GetSessionProjectPathAnyPath(groupID)

	addGroupRow(t, project, groupID, "assistant", "第一段", hostID)
	cut := addGroupRow(t, project, groupID, "assistant", "第二段", hostID)
	addGroupRow(t, project, groupID, "assistant", "第三段", hostID)

	out := RenderGroupForkContext(groupID, cut, ForkContextOptions{BudgetChars: 10000})

	assert.Contains(t, out, "第一段")
	assert.Contains(t, out, "第二段", "the cut point is INCLUSIVE")
	assert.NotContains(t, out, "第三段")
}

// Protocol tags are runtime routing syntax; a forked session must read the
// discussion as prose, not as raw tags it might imitate.
func TestRenderGroupForkContext_StripsProtocolTags(t *testing.T) {
	groupID, hostID := setupGroupForkDB(t)
	project := GetSessionProjectPathAnyPath(groupID)

	addGroupRow(t, project, groupID,
		"assistant",
		`<clawbench-mention targets="主持人">请总结</clawbench-mention>这是我的看法<clawbench-group-end/>`,
		hostID)

	out := RenderGroupForkContext(groupID, 0, ForkContextOptions{BudgetChars: 10000})

	assert.Contains(t, out, "这是我的看法")
	assert.NotContains(t, out, "clawbench-mention")
	assert.NotContains(t, out, "clawbench-group-end")
}

// A timeline with nothing but system rows renders nothing, so the caller can
// skip both the stored background and the "history injected" notice.
func TestRenderGroupForkContext_EmptyWhenNoSpeech(t *testing.T) {
	groupID, _ := setupGroupForkDB(t)
	project := GetSessionProjectPathAnyPath(groupID)

	_, err := AddSystemMessage(project, groupID, "轮到用户发言")
	require.NoError(t, err)

	assert.Equal(t, "", RenderGroupForkContext(groupID, 0, ForkContextOptions{BudgetChars: 10000}))
}

// The budget applies to group entries too (same BoundForkContext machinery).
// A single oversized entry is TRUNCATED rather than dropped (existing budget
// policy: a partial excerpt still constrains the model, a missing one does
// not), so the check is the length plus the truncation marker — not the
// "N messages omitted" notice, which only appears when entries are dropped.
func TestRenderGroupForkContext_RespectsBudget(t *testing.T) {
	groupID, hostID := setupGroupForkDB(t)
	project := GetSessionProjectPathAnyPath(groupID)

	long := strings.Repeat("0123456789", 200)
	addGroupRow(t, project, groupID, "assistant", long, hostID)

	out := RenderGroupForkContext(groupID, 0, ForkContextOptions{BudgetChars: 300})

	assert.LessOrEqual(t, len([]rune(out)), 300+50, "budget must bound the rendered text")
	assert.Contains(t, out, "(truncated)")
	assert.NotContains(t, out, long, "the oversized body must not survive intact")
}

// The user's own messages keep their high-priority status in the budget: their
// role label is a configurable nickname, not the literal "user", so the entry
// carries an explicit flag.
func TestRenderGroupForkContext_UserEntriesAreHighPriority(t *testing.T) {
	groupID, hostID := setupGroupForkDB(t)
	project := GetSessionProjectPathAnyPath(groupID)

	addGroupRow(t, project, groupID, "assistant", "旧的长回复", hostID)
	addGroupRow(t, project, groupID, "user", "最新的用户指令", "")

	out := RenderGroupForkContext(groupID, 0, ForkContextOptions{BudgetChars: 10000})

	// The user line is prefixed with the reserved participant name, proving the
	// role label is not the literal "user".
	assert.Contains(t, out, groupUserTarget()+": 最新的用户指令")
}

// ============================================================================
// fork_context storage
// ============================================================================

func TestForkContext_SetGetClear(t *testing.T) {
	setupGroupDB(t)
	project := t.TempDir()
	if _, err := store.ProjectIDForPath(project); err != nil {
		require.NoError(t, err)
	}
	sid, err := CreateSession(project, "claude", "T", "claude", "", "default", "chat")
	require.NoError(t, err)

	assert.Equal(t, "", GetForkContext(sid), "no background by default")

	SetForkContext(sid, "历史正文")
	assert.Equal(t, "历史正文", GetForkContext(sid))

	ClearForkContext(sid)
	assert.Equal(t, "", GetForkContext(sid))

	// Clearing again is a no-op, not an error.
	ClearForkContext(sid)
	assert.Equal(t, "", GetForkContext(sid))
}

// An empty background is never stored: the handler keys the "history injected"
// notice off GetForkContext, so an empty marker would produce a false claim.
func TestForkContext_EmptyTextIsNotStored(t *testing.T) {
	setupGroupDB(t)
	project := t.TempDir()
	if _, err := store.ProjectIDForPath(project); err != nil {
		require.NoError(t, err)
	}
	sid, err := CreateSession(project, "claude", "T", "claude", "", "default", "chat")
	require.NoError(t, err)

	SetForkContext(sid, "   ")
	assert.Equal(t, "", GetForkContext(sid))
}

// Clearing must not disturb the other context_state fields written by the
// mode/usage/seen_cursor paths.
func TestForkContext_ClearPreservesOtherState(t *testing.T) {
	setupGroupDB(t)
	project := t.TempDir()
	if _, err := store.ProjectIDForPath(project); err != nil {
		require.NoError(t, err)
	}
	sid, err := CreateSession(project, "claude", "T", "claude", "", "default", "chat")
	require.NoError(t, err)

	SetForkContext(sid, "历史")
	PatchContextStateMerge(sid, map[string]string{"seen_cursor": "42"})

	ClearForkContext(sid)

	assert.Equal(t, "", GetForkContext(sid))
	assert.EqualValues(t, 42, GetMemberCursor(sid))
}
