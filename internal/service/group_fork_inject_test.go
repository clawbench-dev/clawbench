package service

import (
	"context"
	"strings"
	"testing"

	"clawbench/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Fork background injection into group speakers
// ============================================================================
//
// The contract: EVERY speaker that has not yet produced a successful turn gets
// the background, and a speaker that has spoken successfully never gets it
// again. The per-member seen_cursor is the "already digested" marker (it only
// advances on a clean turn), so no extra state is involved.

// seedForkBackground writes the background a fork would have stored on the group
// row.
func seedForkBackground(t *testing.T, groupID, text string) {
	t.Helper()
	SetForkContext(groupID, text)
}

func TestGroupForkBackground_AllFirstSpeakersReceiveIt(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/gorch-forkbg-all"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, hostID, err := CreateGroup(project, "G", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	mA, err := AddGroupMember(project, groupID, "claude", "agent-a", "A")
	if err != nil {
		t.Fatalf("AddGroupMember: %v", err)
	}

	const marker = "FORK_BACKGROUND_MARKER"
	seedForkBackground(t, groupID, marker+"\n\n")

	type seen struct{ id, prompt string }
	var prompts []seen

	script := map[string][]string{
		hostID: {`<clawbench-mention targets="A">请表态</clawbench-mention>`, `<clawbench-group-end/> 结束`},
		mA:     {"A 的观点"},
	}
	base, _ := newScriptedRunner(t, groupID, project, script)
	runner := func(ctx context.Context, gid string, turn groupMemberTurn) groupMemberResult {
		prompts = append(prompts, seen{turn.MemberRowID, turn.Prompt})
		return base(ctx, gid, turn)
	}

	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	_ = runGroupTurnForTest(t, o, project, "开始讨论", nil)

	// Both the host and the member speak for the first time, so BOTH must carry
	// the background. This is the decision that replaced "first speaker only".
	got := map[string]int{}
	for _, p := range prompts {
		if strings.Contains(p.prompt, marker) {
			got[p.id]++
		}
	}
	if got[hostID] == 0 {
		t.Fatalf("host's first prompt must carry the background; prompts=%+v", prompts)
	}
	if got[mA] == 0 {
		t.Fatalf("member's first prompt must carry the background; prompts=%+v", prompts)
	}
}

// Once a speaker has spoken successfully its cursor advances, so a later turn in
// the SAME group must not re-inject the background.
func TestGroupForkBackground_NotReinjectedAfterSuccess(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/gorch-forkbg-once"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, hostID, err := CreateGroup(project, "G", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}

	const marker = "FORK_BACKGROUND_MARKER"
	seedForkBackground(t, groupID, marker+"\n\n")

	var prompts []string
	script := map[string][]string{
		hostID: {`<clawbench-group-end/> 第一轮结束`, `<clawbench-group-end/> 第二轮结束`},
	}
	base, _ := newScriptedRunner(t, groupID, project, script)
	runner := func(ctx context.Context, gid string, turn groupMemberTurn) groupMemberResult {
		prompts = append(prompts, turn.Prompt)
		return base(ctx, gid, turn)
	}

	o1 := NewGroupOrchestrator(groupID)
	o1.runTurn = runner
	_ = runGroupTurnForTest(t, o1, project, "第一轮", nil)
	o2 := NewGroupOrchestrator(groupID)
	o2.runTurn = runner
	_ = runGroupTurnForTest(t, o2, project, "第二轮", nil)

	if len(prompts) < 2 {
		t.Fatalf("expected at least two host turns, got %d", len(prompts))
	}
	if !strings.Contains(prompts[0], marker) {
		t.Fatalf("the first host prompt must carry the background")
	}
	if strings.Contains(prompts[1], marker) {
		t.Fatalf("a host that already spoke successfully must not receive the background again")
	}
}

// A failed first turn must NOT consume the background: the cursor only advances
// on a clean turn (decision #69), so the speaker's retry still carries it.
//
// Tested at the snapshot/execute seam rather than through a full drain: a failed
// HOST turn falls back to a DIFFERENT member (hostFailureFallback), so a full
// round never retries the same speaker and could not express this rule.
func TestGroupForkBackground_SurvivesFailedFirstTurn(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/gorch-forkbg-fail"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, hostID, err := CreateGroup(project, "G", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}

	const marker = "FORK_BACKGROUND_MARKER"
	seedForkBackground(t, groupID, marker+"\n\n")

	// A real turn always has at least the user's message on the timeline, which
	// is what makes the high-water mark non-zero (a zero cursor target would
	// leave the speaker looking un-consumed forever).
	if _, err := AddChatMessageWithAgent(project, "codebuddy", groupID, "user", "开始讨论", nil, false, "", ""); err != nil {
		t.Fatalf("seed user message: %v", err)
	}

	o := NewGroupOrchestrator(groupID)
	s, err := o.prepareTurn(groupID)
	if err != nil {
		t.Fatalf("prepareTurn: %v", err)
	}
	defer clearGroupMembersActive(s.activeIDs)

	turn := speakerTurn{member: GroupMember{ID: hostID, Name: s.names[hostID]}}

	// First attempt: the prompt carries the background, then the turn FAILS.
	p1 := o.snapshotTurn(s, turn, GroupTimelineHighWater(groupID))
	if !strings.Contains(p1.prompt, marker) {
		t.Fatalf("the first prompt must carry the background")
	}
	failing := func(ctx context.Context, gid string, t groupMemberTurn) groupMemberResult {
		return groupMemberResult{Err: "backend exploded"}
	}
	s.runner = failing
	o.executeTurn(context.Background(), s, p1)

	// Second attempt for the SAME speaker: still carries it (cursor unchanged).
	p2 := o.snapshotTurn(s, turn, GroupTimelineHighWater(groupID))
	if !strings.Contains(p2.prompt, marker) {
		t.Fatalf("the retry must still carry the background: a failed turn consumes nothing")
	}

	// A clean turn advances the cursor, so the next snapshot drops the background.
	ok := func(ctx context.Context, gid string, t groupMemberTurn) groupMemberResult {
		return groupMemberResult{}
	}
	s.runner = ok
	o.executeTurn(context.Background(), s, p2)

	p3 := o.snapshotTurn(s, turn, GroupTimelineHighWater(groupID))
	if strings.Contains(p3.prompt, marker) {
		t.Fatalf("after a successful turn the speaker must not receive the background again")
	}
}

// An ordinary group (no fork) must not have a background injected at all.
func TestGroupForkBackground_OrdinaryGroupHasNone(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/gorch-forkbg-none"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, hostID, err := CreateGroup(project, "G", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}

	var prompts []string
	script := map[string][]string{hostID: {`<clawbench-group-end/> 结束`}}
	base, _ := newScriptedRunner(t, groupID, project, script)
	runner := func(ctx context.Context, gid string, turn groupMemberTurn) groupMemberResult {
		prompts = append(prompts, turn.Prompt)
		return base(ctx, gid, turn)
	}

	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	_ = runGroupTurnForTest(t, o, project, "普通讨论", nil)

	if len(prompts) == 0 {
		t.Fatal("precondition: the host must have spoken")
	}
	for _, p := range prompts {
		if strings.Contains(p, "conversation history from before this session") {
			t.Fatalf("an ordinary group must not receive fork background: %q", p)
		}
	}
}

// ============================================================================
// Single-chat target: read gate + clear on success
// ============================================================================
//
// The gate must survive a failed first turn. An assistant-count check cannot do
// that (a failed turn writes an assistant WARNING row), which is why the read
// gate is SessionHasRealAssistantContent and the "consumed" marker is a
// ClearForkContext call after a clean turn.

func TestBuildChatRequest_InjectsForkBackgroundOnFirstTurn(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	_, err := store.WriteExec(
		"INSERT INTO chat_sessions (id, project_id, backend, title, agent_id, agent_source, model, session_type) VALUES (?, 1, 'codebuddy', 'T', '', 'default', '', 'chat')",
		"sess-bg",
	)
	require.NoError(t, err)
	SetForkContext("sess-bg", "历史正文XYZ")

	req := BuildChatRequest("新问题", "sess-bg", "/proj", "codebuddy", "", "", "", "", "", "/proj", false)
	assert.Contains(t, req.ForkContext, "历史正文XYZ")
}

// A session that already produced real AI content never gets the background.
func TestBuildChatRequest_NoInjectionAfterRealReply(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	_, err := store.WriteExec(
		"INSERT INTO chat_sessions (id, project_id, backend, title, agent_id, agent_source, model, session_type) VALUES (?, 1, 'codebuddy', 'T', '', 'default', '', 'chat')",
		"sess-bg2",
	)
	require.NoError(t, err)
	SetForkContext("sess-bg2", "历史正文XYZ")
	_, err = AddChatMessageWithAgent("/proj", "codebuddy", "sess-bg2", "assistant",
		`{"blocks":[{"type":"text","text":"真正的回复"}]}`, nil, false, "", "")
	require.NoError(t, err)
	// An external session id makes the LEGACY copied-rows fork branch decline to
	// run (it requires an empty ext id), so this test exercises the background
	// gate itself rather than passing because another branch short-circuited it.
	require.NoError(t, UpdateExternalSessionID("sess-bg2", "ext-bg2"))

	req := BuildChatRequest("新问题", "sess-bg2", "/proj", "codebuddy", "", "", "", "", "", "/proj", false)
	// The legacy copied-rows fork path may still build its own envelope from the
	// session's history; what must NOT happen is the stored background being
	// appended on top of it.
	assert.NotContains(t, req.ForkContext, "历史正文XYZ")
}

// A failed first turn writes an assistant WARNING row. The background must still
// be injected on the retry — this is the case an assistant-count gate breaks.
func TestBuildChatRequest_InjectsAfterFailedFirstTurn(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	_, err := store.WriteExec(
		"INSERT INTO chat_sessions (id, project_id, backend, title, agent_id, agent_source, model, session_type) VALUES (?, 1, 'codebuddy', 'T', '', 'default', '', 'chat')",
		"sess-bg3",
	)
	require.NoError(t, err)
	SetForkContext("sess-bg3", "历史正文XYZ")
	// What persistTurnWarning writes: an assistant row containing ONLY a warning.
	_, err = AddChatMessageWithAgent("/proj", "codebuddy", "sess-bg3", "assistant",
		`{"blocks":[{"type":"warning","text":"backend exploded"}]}`, nil, false, "", "")
	require.NoError(t, err)

	req := BuildChatRequest("重试", "sess-bg3", "/proj", "codebuddy", "", "", "", "", "", "/proj", false)
	assert.Contains(t, req.ForkContext, "历史正文XYZ",
		"a warning-only row is not a successful turn, so the background must survive")
}

// Once a real reply exists, ClearForkContext removes the stored text (the
// marker that the background has been consumed).
func TestClearForkContext_EndsInjection(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()

	_, err := store.WriteExec(
		"INSERT INTO chat_sessions (id, project_id, backend, title, agent_id, agent_source, model, session_type) VALUES (?, 1, 'codebuddy', 'T', '', 'default', '', 'chat')",
		"sess-bg4",
	)
	require.NoError(t, err)
	SetForkContext("sess-bg4", "历史正文XYZ")

	ClearForkContext("sess-bg4")

	req := BuildChatRequest("下一轮", "sess-bg4", "/proj", "codebuddy", "", "", "", "", "", "/proj", false)
	assert.Empty(t, req.ForkContext)
}
