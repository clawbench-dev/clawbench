package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"clawbench/internal/grouprouting"
	"clawbench/internal/i18n"
	"clawbench/internal/model"
	"clawbench/internal/store"
)

// createFreeGroup builds a free-mode group (no host) with the given members and
// returns the group id plus a member row id per display name.
func createFreeGroup(t *testing.T, project string, memberNames ...string) (string, map[string]string) {
	t.Helper()
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	specs := make([]GroupMemberSpec, 0, len(memberNames))
	for i, name := range memberNames {
		specs = append(specs, GroupMemberSpec{
			AgentID:     "agent-" + strings.ToLower(name),
			Backend:     "claude",
			DisplayName: name,
		})
		_ = i
	}
	groupID, hostMemberID, err := CreateGroupWithMembers(project, "群聊", "", specs)
	if err != nil {
		t.Fatalf("CreateGroupWithMembers(free): %v", err)
	}
	if hostMemberID != "" {
		t.Fatalf("free group must have no host member, got %q", hostMemberID)
	}
	members, err := ListGroupMembers(groupID)
	if err != nil {
		t.Fatalf("ListGroupMembers: %v", err)
	}
	byName := map[string]string{}
	for _, m := range members {
		byName[m.Name] = m.ID
	}
	return groupID, byName
}

// freeScriptedRunner writes a scripted reply per member (keyed by row id),
// recording the speaking order. Unlike newScriptedRunner it takes row ids.
// It is safe for concurrent use: a parallel group runs members in goroutines,
// so the shared order/counts must be mutex-guarded.
func freeScriptedRunner(t *testing.T, project, groupID string, script map[string][]string) (groupTurnRunner, *[]string) {
	t.Helper()
	order := &[]string{}
	counts := map[string]int{}
	var mu sync.Mutex
	return func(ctx context.Context, gid string, turn groupMemberTurn) groupMemberResult {
		mu.Lock()
		*order = append(*order, turn.MemberRowID)
		texts := script[turn.MemberRowID]
		i := counts[turn.MemberRowID]
		text := "(no script)"
		if i < len(texts) {
			text = texts[i]
		}
		counts[turn.MemberRowID]++
		mu.Unlock()
		_, err := AddChatMessageWithAgent(project, "claude", groupID, "assistant",
			assistantText(text), nil, false, "", turn.MemberRowID)
		if err != nil {
			return groupMemberResult{Err: err.Error()}
		}
		return groupMemberResult{}
	}, order
}

// The mode marker must be written at creation and read back.
func TestFreeGroup_ModeStoredAndRead(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/free-mode"
	groupID, _ := createFreeGroup(t, project, "A", "B")
	if got := GetGroupMode(groupID); got != GroupModeFree {
		t.Fatalf("mode=%q want %q", got, GroupModeFree)
	}
	if !IsFreeGroup(groupID) {
		t.Fatal("IsFreeGroup must be true for a free group")
	}
	if GetGroupHostMember(groupID) != "" {
		t.Fatal("a free group must have no host member")
	}
}

// A host-mode group created the usual way must report host mode.
func TestHostGroup_ModeStoredAndRead(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/host-mode"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, hostID, err := CreateGroup(project, "G", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if GetGroupMode(groupID) != GroupModeHost {
		t.Fatalf("mode=%q want %q", GetGroupMode(groupID), GroupModeHost)
	}
	if hostID == "" || GetGroupHostMember(groupID) != hostID {
		t.Fatalf("host member mismatch: hostID=%q stored=%q", hostID, GetGroupHostMember(groupID))
	}
}

// A free group needs at least two members.
func TestFreeGroup_NeedsTwoMembers(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/free-too-few"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	_, _, err := CreateGroupWithMembers(project, "群聊", "", []GroupMemberSpec{
		{AgentID: "agent-a", Backend: "claude", DisplayName: "A"},
	})
	if !errors.Is(err, ErrFreeGroupNeedsTwoMembers) {
		t.Fatalf("err=%v want ErrFreeGroupNeedsTwoMembers", err)
	}
}

// A user message with NO @-mentions makes every active member speak once, in
// roster order.
func TestFreeLoop_NoMentionSpeaksAllInRosterOrder(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/free-all"
	groupID, byName := createFreeGroup(t, project, "A", "B", "C")

	script := map[string][]string{
		byName["A"]: {"A 说"},
		byName["B"]: {"B 说"},
		byName["C"]: {"C 说"},
	}
	runner, order := freeScriptedRunner(t, project, groupID, script)

	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	_ = runGroupTurnForTest(t, o, project, "大家聊聊", nil)

	want := []string{byName["A"], byName["B"], byName["C"]}
	if len(*order) != len(want) {
		t.Fatalf("order=%v want %v", *order, want)
	}
	for i, w := range want {
		if (*order)[i] != w {
			t.Fatalf("order[%d]=%q want %q (full=%v)", i, (*order)[i], w, *order)
		}
	}
}

// A user message @-mentioning one member makes ONLY that member speak; with no
// further mentions the relay ends.
func TestFreeLoop_UserMentionSpeaksOnlyTarget(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/free-one"
	groupID, byName := createFreeGroup(t, project, "A", "B")

	script := map[string][]string{
		byName["A"]: {"A 说"},
		byName["B"]: {"B 说（不应发生）"},
	}
	runner, order := freeScriptedRunner(t, project, groupID, script)

	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	msg := `<clawbench-mention targets="` + byName["A"] + `"></clawbench-mention> 请你说说`
	_ = runGroupTurnForTest(t, o, project, msg, nil)

	if len(*order) != 1 || (*order)[0] != byName["A"] {
		t.Fatalf("only A must speak, order=%v", *order)
	}
}

// A member's own @-mention relays the floor: A @s B, B speaks; B @s nobody, so
// the relay ends.
func TestFreeLoop_MemberMentionRelays(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/free-relay"
	groupID, byName := createFreeGroup(t, project, "A", "B")

	script := map[string][]string{
		byName["A"]: {`<clawbench-mention targets="B">B 你补充</clawbench-mention>`},
		byName["B"]: {"B 补充完毕"},
	}
	runner, order := freeScriptedRunner(t, project, groupID, script)

	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	msg := `<clawbench-mention targets="` + byName["A"] + `"></clawbench-mention> 开始`
	_ = runGroupTurnForTest(t, o, project, msg, nil)

	want := []string{byName["A"], byName["B"]}
	if len(*order) != len(want) {
		t.Fatalf("order=%v want %v", *order, want)
	}
	for i, w := range want {
		if (*order)[i] != w {
			t.Fatalf("order[%d]=%q want %q", i, (*order)[i], w)
		}
	}
}

// Unlimited relay: A @s B, B @s A — since a member who already spoke may be
// @-ed again, this loops until the script runs out (here B's second line does
// not @, ending it). This pins the "no dedup across the whole turn" rule.
func TestFreeLoop_AlreadySpokenMayBeMentionedAgain(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/free-loop"
	groupID, byName := createFreeGroup(t, project, "A", "B")

	script := map[string][]string{
		byName["A"]: {
			`<clawbench-mention targets="B">轮到 B</clawbench-mention>`,
			`<clawbench-mention targets="B">又轮到 B</clawbench-mention>`,
		},
		byName["B"]: {
			`<clawbench-mention targets="A">轮到 A</clawbench-mention>`,
			"B 收尾",
		},
	}
	runner, order := freeScriptedRunner(t, project, groupID, script)

	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	msg := `<clawbench-mention targets="` + byName["A"] + `"></clawbench-mention> 开始`
	_ = runGroupTurnForTest(t, o, project, msg, nil)

	want := []string{byName["A"], byName["B"], byName["A"], byName["B"]}
	if len(*order) != len(want) {
		t.Fatalf("order=%v want %v", *order, want)
	}
	for i, w := range want {
		if (*order)[i] != w {
			t.Fatalf("order[%d]=%q want %q (full=%v)", i, (*order)[i], w, *order)
		}
	}
}

// The member-speech cap applies to free mode too (mode-agnostic): the relay
// stops once the agents have spoken cap times, even though the scripted members
// would keep @-ing each other forever. This is the bound that replaced the
// "unlimited relay" behavior.
func TestFreeLoop_SpeechCapStopsRelay(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/free-cap"
	groupID, byName := createFreeGroup(t, project, "A", "B")

	// Both members ALWAYS @ the other — without a cap this never ends. Each gets
	// several scripted replies so the relay is stopped by the CAP, not by a
	// script running out (an exhausted script yields a mention-free "(no
	// script)" reply, which would end the relay naturally).
	script := map[string][]string{
		byName["A"]: {
			`<clawbench-mention targets="B">继续</clawbench-mention>`,
			`<clawbench-mention targets="B">继续</clawbench-mention>`,
			`<clawbench-mention targets="B">继续</clawbench-mention>`,
		},
		byName["B"]: {
			`<clawbench-mention targets="A">继续</clawbench-mention>`,
			`<clawbench-mention targets="A">继续</clawbench-mention>`,
			`<clawbench-mention targets="A">继续</clawbench-mention>`,
		},
	}
	runner, order := freeScriptedRunner(t, project, groupID, script)

	// Cap at 3 member speeches: the relay must stop after exactly 3.
	setGroupMaxSpeechesForTest(t, 3)

	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	msg := `<clawbench-mention targets="` + byName["A"] + `"></clawbench-mention> 开始`
	_ = runGroupTurnForTest(t, o, project, msg, nil)

	if len(*order) != 3 {
		t.Fatalf("member speeches=%d, want 3 (cap), order=%v", len(*order), *order)
	}

	// A one-time system line announces the cap.
	want := i18n.T(i18n.LocalizerForLocale(model.Language), "GroupDiscussionLimitReached")
	msgs, _ := GetMessagesBySessionIDRaw(groupID)
	found := 0
	for _, m := range msgs {
		if m.Role == "system" && strings.Contains(ExtractPlainText(m.Content), want) {
			found++
		}
	}
	if found != 1 {
		t.Fatalf("cap system line count=%d, want exactly 1", found)
	}
}

// A member @-ing the human ("User") hands the floor back and ends the relay.
func TestFreeLoop_MemberMentionsUserStops(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/free-user"
	groupID, byName := createFreeGroup(t, project, "A", "B")

	script := map[string][]string{
		byName["A"]: {`<clawbench-mention targets="User">该你说了</clawbench-mention>`},
		byName["B"]: {"B 不应发言"},
	}
	runner, order := freeScriptedRunner(t, project, groupID, script)

	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	msg := `<clawbench-mention targets="` + byName["A"] + `"></clawbench-mention> 开始`
	_ = runGroupTurnForTest(t, o, project, msg, nil)

	if len(*order) != 1 || (*order)[0] != byName["A"] {
		t.Fatalf("only A must speak before the user is named, order=%v", *order)
	}
	// A system line announcing the user's turn must be written.
	msgs, _ := GetMessagesBySessionIDRaw(groupID)
	want := i18n.T(i18n.LocalizerForLocale(model.Language), "GroupYourTurn")
	found := false
	for _, m := range msgs {
		if m.Role == "system" && strings.Contains(ExtractPlainText(m.Content), want) {
			found = true
		}
	}
	if !found {
		t.Fatal("a system line announcing the user's turn must be written")
	}
}

// A self-mention is dropped silently (no infinite self-loop).
func TestFreeLoop_SelfMentionDropped(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/free-self"
	groupID, byName := createFreeGroup(t, project, "A", "B")

	script := map[string][]string{
		byName["A"]: {`<clawbench-mention targets="A">我自己接着说</clawbench-mention>`},
		byName["B"]: {"B 不应发言"},
	}
	runner, order := freeScriptedRunner(t, project, groupID, script)

	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	msg := `<clawbench-mention targets="` + byName["A"] + `"></clawbench-mention> 开始`
	_ = runGroupTurnForTest(t, o, project, msg, nil)

	if len(*order) != 1 {
		t.Fatalf("a self-mention must not re-queue the speaker, order=%v", *order)
	}
}

// An @-mention of an unknown name emits a visible system notice (and does not
// queue anyone).
func TestFreeLoop_UnknownMentionNotifies(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/free-unknown"
	groupID, byName := createFreeGroup(t, project, "A", "B")

	script := map[string][]string{
		byName["A"]: {`<clawbench-mention targets="Ghost">幽灵你在吗</clawbench-mention>`},
		byName["B"]: {"B 不应发言"},
	}
	runner, order := freeScriptedRunner(t, project, groupID, script)

	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	msg := `<clawbench-mention targets="` + byName["A"] + `"></clawbench-mention> 开始`
	_ = runGroupTurnForTest(t, o, project, msg, nil)

	if len(*order) != 1 {
		t.Fatalf("an unknown mention must not queue anyone, order=%v", *order)
	}
	msgs, _ := GetMessagesBySessionIDRaw(groupID)
	found := false
	for _, m := range msgs {
		if m.Role == "system" && strings.Contains(ExtractPlainText(m.Content), "Ghost") {
			found = true
		}
	}
	if !found {
		t.Fatal("an @ of an unknown member must write a visible system notice")
	}
}

// A user @-mentioning several members makes them speak in the order mentioned.
func TestFreeLoop_UserMentionsMultipleInOrder(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/free-multi"
	groupID, byName := createFreeGroup(t, project, "A", "B", "C")

	script := map[string][]string{
		byName["A"]: {"A 说"},
		byName["B"]: {"B 说"},
		byName["C"]: {"C 说"},
	}
	runner, order := freeScriptedRunner(t, project, groupID, script)

	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	msg := `<clawbench-mention targets="` + byName["B"] + `,` + byName["A"] + `"></clawbench-mention> 你俩说`
	_ = runGroupTurnForTest(t, o, project, msg, nil)

	want := []string{byName["B"], byName["A"]}
	if len(*order) != len(want) {
		t.Fatalf("order=%v want %v", *order, want)
	}
	for i, w := range want {
		if (*order)[i] != w {
			t.Fatalf("order[%d]=%q want %q (full=%v)", i, (*order)[i], w, *order)
		}
	}
}

// A user-mention-seeded free member must get the FREE-mode system prompt, not
// the host-mode "member" prompt. The host prompt tells the member NOT to emit
// mention tags — which would break the free relay at its first step, since the
// relay IS the mention tag. Regression: an earlier revision routed the seed
// through memberTurn (host prompt) and injected the user's mention body as a
// phantom "host directive".
func TestFreeLoop_SeedUsesFreeModePrompt(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/free-seed-prompt"
	groupID, byName := createFreeGroup(t, project, "A", "B")

	prompts := map[string]string{}
	var mu sync.Mutex
	base, _ := freeScriptedRunner(t, project, groupID, map[string][]string{
		byName["A"]: {"A 说"},
	})
	runner := func(ctx context.Context, gid string, turn groupMemberTurn) groupMemberResult {
		mu.Lock()
		prompts[turn.MemberRowID] = turn.Prompt
		mu.Unlock()
		return base(ctx, gid, turn)
	}
	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	msg := `<clawbench-mention targets="` + byName["A"] + `"></clawbench-mention> 开始`
	_ = runGroupTurnForTest(t, o, project, msg, nil)

	mu.Lock()
	defer mu.Unlock()
	p := prompts[byName["A"]]
	if !strings.Contains(p, "自由群聊") {
		t.Fatalf("a free-mode seed must carry the free-mode prompt; got %q", p)
	}
	if strings.Contains(p, "不是主持人") {
		t.Fatalf("a free-mode seed must NOT carry the host-mode member prompt; got %q", p)
	}
	if strings.Contains(p, "主持人要求你") {
		t.Fatalf("a free-mode seed must NOT receive a phantom host directive; got %q", p)
	}
}

// Dedup must count a member still WAITING later in the SAME group: when the
// user's tag names A and B together and A @s B, B must not be queued twice.
// Regression: appendFreeTargets only saw the outer queue (not the current
// group's remaining turns), so B spoke twice ([A,B,B] instead of [A,B]).
func TestFreeLoop_PeerMentionWithinGroupNotDuplicated(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/free-peer-dedup"
	groupID, byName := createFreeGroup(t, project, "A", "B")

	script := map[string][]string{
		byName["A"]: {`<clawbench-mention targets="B">B 你补充</clawbench-mention>`},
		byName["B"]: {"B 说完了"},
	}
	runner, order := freeScriptedRunner(t, project, groupID, script)

	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	// One tag naming BOTH A and B (A first). A @s B while B is still waiting in
	// the same group.
	msg := `<clawbench-mention targets="` + byName["A"] + `,` + byName["B"] + `"></clawbench-mention> 你俩说`
	_ = runGroupTurnForTest(t, o, project, msg, nil)

	if len(*order) != 2 {
		t.Fatalf("expected each member once, got order=%v", *order)
	}
	counts := map[string]int{}
	for _, id := range *order {
		counts[id]++
	}
	if counts[byName["B"]] != 1 {
		t.Fatalf("B must speak exactly once, got %d (order=%v)", counts[byName["B"]], *order)
	}
}

// A free-mode member's private note is delivered to its target on the target's
// next turn (the same group_pending_bcc contract host mode uses).
func TestFreeLoop_PrivateNoteDeliveredToTarget(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/free-note"
	groupID, byName := createFreeGroup(t, project, "A", "B")

	script := map[string][]string{
		byName["A"]: {
			`<clawbench-mention targets="B">B 你补充</clawbench-mention><clawbench-mention targets="B" private>SECRET_FOR_B</clawbench-mention>`,
		},
		byName["B"]: {"B 补充完毕"},
	}
	base, _ := freeScriptedRunner(t, project, groupID, script)
	var bPrompt string
	runner := func(ctx context.Context, gid string, turn groupMemberTurn) groupMemberResult {
		if turn.MemberRowID == byName["B"] {
			bPrompt = turn.Prompt
		}
		return base(ctx, gid, turn)
	}

	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	msg := `<clawbench-mention targets="` + byName["A"] + `"></clawbench-mention> 开始`
	_ = runGroupTurnForTest(t, o, project, msg, nil)

	if !strings.Contains(bPrompt, "SECRET_FOR_B") {
		t.Fatalf("B must receive A's private note; got %q", bPrompt)
	}
}

// A free-mode member's own @-mention is NOT delivered as a note to anyone else
// (a public mention is a floor hand-off, not a private note).
func TestFreeLoop_PublicMentionNotDeliveredAsNote(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/free-nonote"
	groupID, byName := createFreeGroup(t, project, "A", "B")

	script := map[string][]string{
		byName["A"]: {`<clawbench-mention targets="B">PUBLIC_DIRECTIVE</clawbench-mention>`},
		byName["B"]: {"B 说完"},
	}
	base, _ := freeScriptedRunner(t, project, groupID, script)
	var bPrompt string
	runner := func(ctx context.Context, gid string, turn groupMemberTurn) groupMemberResult {
		if turn.MemberRowID == byName["B"] {
			bPrompt = turn.Prompt
		}
		return base(ctx, gid, turn)
	}

	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	msg := `<clawbench-mention targets="` + byName["A"] + `"></clawbench-mention> 开始`
	_ = runGroupTurnForTest(t, o, project, msg, nil)

	// The public directive reaches B through the timeline (readable @name
	// prose), never through the private-note channel. The private-note header
	// (主持人密送给你) is what distinguishes the two, NOT the word 密送 — the
	// free-mode system prompt itself documents the 密送 feature, so asserting on
	// the bare word would always fail.
	if strings.Contains(bPrompt, "主持人密送给你") {
		t.Fatalf("a public mention must not be delivered as a private note; got %q", bPrompt)
	}
	if !strings.Contains(bPrompt, "PUBLIC_DIRECTIVE") {
		t.Fatalf("the public directive must still reach B via the timeline; got %q", bPrompt)
	}
}

// A private note to an unknown target is dropped (it could never be delivered),
// rather than stored forever in group_pending_bcc.
func TestFreeLoop_UnknownNoteTargetNotStored(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/free-unknown-note"
	groupID, byName := createFreeGroup(t, project, "A", "B")

	script := map[string][]string{
		byName["A"]: {`<clawbench-mention targets="Ghost" private>SECRET_GHOST</clawbench-mention>`},
		byName["B"]: {"B 说"},
	}
	runner, _ := freeScriptedRunner(t, project, groupID, script)

	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	msg := `<clawbench-mention targets="` + byName["A"] + `"></clawbench-mention> 开始`
	_ = runGroupTurnForTest(t, o, project, msg, nil)

	if got := pendingBccForTarget(groupID, "Ghost"); len(got) != 0 {
		t.Fatalf("a note to an unknown target must not be stored; got %v", got)
	}
}

// Removing a member drops the private note awaiting it (it can never be
// delivered once the member has left).
func TestRemoveGroupMember_DropsPendingNotes(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/free-remove-note"
	groupID, byName := createFreeGroup(t, project, "A", "B")

	addPendingBcc(groupID, "B", "SECRET_FOR_B")
	if got := pendingBccForTarget(groupID, "B"); len(got) != 1 {
		t.Fatalf("setup: pending note not stored, got %v", got)
	}
	if err := RemoveGroupMember(groupID, byName["B"]); err != nil {
		t.Fatalf("RemoveGroupMember: %v", err)
	}
	if got := pendingBccForTarget(groupID, "B"); len(got) != 0 {
		t.Fatalf("a departed member's pending note must be dropped; got %v", got)
	}
}

// A note addressed (by ROW ID) to a member who has ALREADY left must not be
// stored: the member can never speak again, so it would sit in
// group_pending_bcc forever. byID includes left members (only byName excludes
// them), so the guard has to be explicit — without it, resolving a user's
// id-targeted note through byID would resurrect the unbounded-growth bug.
func TestStoreRouteNotes_DropsNoteToLeftMemberByID(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/free-note-left"
	groupID, byName := createFreeGroup(t, project, "A", "B")
	leftID := byName["B"]
	if err := RemoveGroupMember(groupID, leftID); err != nil {
		t.Fatalf("RemoveGroupMember: %v", err)
	}

	members, err := ListGroupMembers(groupID)
	if err != nil {
		t.Fatalf("ListGroupMembers: %v", err)
	}
	byID, byNameMap := memberLookups(members)
	route := grouprouting.Parse(`<clawbench-mention targets="` + leftID + `" private>给离场者</clawbench-mention>`)
	storeRouteNotes(groupID, route, groupUserTargetID, byID, byNameMap)

	if got := pendingBccForTarget(groupID, "B"); len(got) != 0 {
		t.Fatalf("a note to a left member must not be stored; got %v", got)
	}
}

// The same path MUST still store a note to an ACTIVE member (guard the fix does
// not over-reject).
func TestStoreRouteNotes_StoresNoteToActiveMemberByID(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/free-note-active"
	groupID, byName := createFreeGroup(t, project, "A", "B")
	activeID := byName["B"]

	members, _ := ListGroupMembers(groupID)
	byID, byNameMap := memberLookups(members)
	route := grouprouting.Parse(`<clawbench-mention targets="` + activeID + `" private>给在场者</clawbench-mention>`)
	storeRouteNotes(groupID, route, groupUserTargetID, byID, byNameMap)

	got := pendingBccForTarget(groupID, "B")
	if len(got) != 1 || got[0].Content != "给在场者" {
		t.Fatalf("an active member's id-targeted note must be stored; got %v", got)
	}
}

// The free-mode member prompt must teach the @ syntax and that NOT @-ing ends
// the relay.
func TestBuildFreeMemberSystemPrompt(t *testing.T) {
	p := BuildFreeMemberSystemPrompt([]HostMemberInfo{{Name: "A"}, {Name: "B"}}, "A")
	if !strings.Contains(p, "clawbench-mention") {
		t.Fatalf("free prompt must teach the mention tag; got %q", p)
	}
	if !strings.Contains(p, "不要输出任何 mention 标签") && !strings.Contains(p, "不需要") {
		t.Fatalf("free prompt must state that not @-ing ends the relay; got %q", p)
	}
	if !strings.Contains(p, "B") {
		t.Fatalf("free prompt must list other members; got %q", p)
	}
	// A member must not be told about itself.
	if strings.Contains(p, "本群其他成员：A") {
		t.Fatalf("free prompt must exclude self from the roster; got %q", p)
	}
}

// A free group's `/cb-*` command injection runs on the FIRST speaker only.
func TestFreeLoop_CommandInjectionOnFirstSpeakerOnly(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/free-cmd"
	groupID, byName := createFreeGroup(t, project, "A", "B")

	script := map[string][]string{
		byName["A"]: {"A 说"},
		byName["B"]: {"B 说"},
	}
	base, _ := freeScriptedRunner(t, project, groupID, script)
	prompts := map[string]string{}
	runner := func(ctx context.Context, gid string, turn groupMemberTurn) groupMemberResult {
		prompts[turn.MemberRowID] = turn.Prompt
		return base(ctx, gid, turn)
	}

	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	prev := SetRenderGroupCommandFn(func(rawMsg, projectPath, sessionID string) (string, error) {
		return "INJECTED_COMMAND", nil
	})
	defer SetRenderGroupCommandFn(prev)
	_ = runGroupTurnForTest(t, o, project, "大家聊聊", nil)

	if !strings.Contains(prompts[byName["A"]], "INJECTED_COMMAND") {
		t.Fatalf("the first speaker must carry the command injection; got %q", prompts[byName["A"]])
	}
	if strings.Contains(prompts[byName["B"]], "INJECTED_COMMAND") {
		t.Fatalf("only the first speaker may carry the command injection; got %q", prompts[byName["B"]])
	}
}

// A host-mode group must NOT honor human @-mentions (decision #47): the host
// still routes.
func TestHostMode_IgnoresHumanMention(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/host-ignores-mention"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, hostID, err := CreateGroup(project, "G", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	mA, _ := AddGroupMember(project, groupID, "claude", "agent-a", "A")
	mB, _ := AddGroupMember(project, groupID, "claude", "agent-b", "B")

	// The host routes to B; the user @-mentioned A, which must be ignored.
	script := map[string][]string{
		hostID: {`<clawbench-mention targets="B">请 B 表态</clawbench-mention>`, `<clawbench-group-end/> 结束`},
		mA:     {"A 说（不应发生）"},
		mB:     {"B 说"},
	}
	runner, order := newScriptedRunner(t, groupID, project, script)

	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	msg := `<clawbench-mention targets="` + mA + `"></clawbench-mention> 我说`
	_ = runGroupTurnForTest(t, o, project, msg, nil)

	for _, id := range *order {
		if id == mA {
			t.Fatalf("host mode must ignore the human @-mention of A, order=%v", *order)
		}
	}
}

var _ = model.FileEntry{}

// A `mode="parallel"` mention makes its members run CONCURRENTLY from one shared
// snapshot: neither sees the other's output this round. The decisive assertion
// is that both members' prompts were built before either turn produced output —
// i.e. member B's injected context does NOT contain member A's reply, even
// though A also spoke this round.
func TestFreeLoop_ParallelGroupMembersDoNotSeeEachOther(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/free-parallel"
	groupID, byName := createFreeGroup(t, project, "A", "B")

	prompts := map[string]string{}
	var mu sync.Mutex
	base, _ := freeScriptedRunner(t, project, groupID, map[string][]string{
		byName["A"]: {"A 的回答"},
		byName["B"]: {"B 的回答"},
	})
	runner := func(ctx context.Context, gid string, turn groupMemberTurn) groupMemberResult {
		mu.Lock()
		prompts[turn.MemberRowID] = turn.Prompt
		mu.Unlock()
		return base(ctx, gid, turn)
	}

	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	// The user @-mentions A and B in ONE parallel tag → they run concurrently.
	msg := `<clawbench-mention targets="` + byName["A"] + `,` + byName["B"] + `" mode="parallel">同时回答</clawbench-mention> 开始`
	_ = runGroupTurnForTest(t, o, project, msg, nil)

	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(prompts[byName["A"]], "同时回答") || !strings.Contains(prompts[byName["B"]], "同时回答") {
		t.Fatalf("both members must receive the parallel group's instruction; A=%q B=%q",
			prompts[byName["A"]], prompts[byName["B"]])
	}
	if strings.Contains(prompts[byName["B"]], "A 的回答") {
		t.Fatalf("a parallel member must NOT see a sibling's output this round; B prompt=%q", prompts[byName["B"]])
	}
	if strings.Contains(prompts[byName["A"]], "B 的回答") {
		t.Fatalf("a parallel member must NOT see a sibling's output this round; A prompt=%q", prompts[byName["A"]])
	}
}

// A parallel group that names the human user is SPLIT (P7): the AI members keep
// the tag's `parallel` mode and run CONCURRENTLY (they do not see each other's
// output), then the floor is handed to the user and the round ends. The
// decisive assertions are that the AI members really ran in parallel (peak >= 2)
// and that neither saw a sibling's output this round — a mere "everyone spoke"
// check would also pass under the old sequential downgrade.
func TestFreeLoop_ParallelWithUserRunsMembersConcurrently(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/free-parallel-user"
	groupID, byName := createFreeGroup(t, project, "A", "B")

	var mu sync.Mutex
	cur, peak := 0, 0
	prompts := map[string]string{}
	base, order := freeScriptedRunner(t, project, groupID, map[string][]string{
		byName["A"]: {"A 的回答"},
		byName["B"]: {"B 的回答"},
	})
	runner := func(ctx context.Context, gid string, turn groupMemberTurn) groupMemberResult {
		mu.Lock()
		cur++
		if cur > peak {
			peak = cur
		}
		prompts[turn.MemberRowID] = turn.Prompt
		mu.Unlock()
		// Hold briefly so overlaps are actually observable.
		time.Sleep(20 * time.Millisecond)
		r := base(ctx, gid, turn)
		mu.Lock()
		cur--
		mu.Unlock()
		return r
	}

	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	// User named LAST, with the AI members in one parallel tag.
	msg := `<clawbench-mention targets="` + byName["A"] + `,` + byName["B"] + `,User" mode="parallel">同时</clawbench-mention> 开始`
	_ = runGroupTurnForTest(t, o, project, msg, nil)

	mu.Lock()
	defer mu.Unlock()
	// Both AI members spoke, and the user sentinel never reached the runner.
	seen := map[string]bool{}
	for _, id := range *order {
		if id == groupUserTargetID {
			t.Fatalf("the user sentinel must never be handed to the runner; order=%v", *order)
		}
		seen[id] = true
	}
	if !seen[byName["A"]] || !seen[byName["B"]] {
		t.Fatalf("both AI members must speak; order=%v", *order)
	}
	if peak < 2 {
		t.Fatalf("the AI members must run concurrently (peak >= 2), got %d — the group was downgraded to sequential", peak)
	}
	if strings.Contains(prompts[byName["B"]], "A 的回答") || strings.Contains(prompts[byName["A"]], "B 的回答") {
		t.Fatalf("parallel members must not see a sibling's output; A=%q B=%q",
			prompts[byName["A"]], prompts[byName["B"]])
	}
}

// The split must hold regardless of WHERE User sits in the parallel tag: the
// members run concurrently and the user is always handed the floor last. A User
// turn ends the round, so if User were allowed to run first the members after
// it would be silently dropped.
func TestFreeLoop_ParallelUserFirstStillSpeaksMembers(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/free-parallel-user-first"
	groupID, byName := createFreeGroup(t, project, "A", "B")

	var mu sync.Mutex
	cur, peak := 0, 0
	base, order := freeScriptedRunner(t, project, groupID, map[string][]string{
		byName["A"]: {"A 的回答"},
		byName["B"]: {"B 的回答"},
	})
	runner := func(ctx context.Context, gid string, turn groupMemberTurn) groupMemberResult {
		mu.Lock()
		cur++
		if cur > peak {
			peak = cur
		}
		mu.Unlock()
		time.Sleep(20 * time.Millisecond)
		r := base(ctx, gid, turn)
		mu.Lock()
		cur--
		mu.Unlock()
		return r
	}
	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	// User named FIRST, then the two members.
	msg := `<clawbench-mention targets="User,` + byName["A"] + `,` + byName["B"] + `" mode="parallel">同时</clawbench-mention> 开始`
	_ = runGroupTurnForTest(t, o, project, msg, nil)

	mu.Lock()
	defer mu.Unlock()
	seen := map[string]bool{}
	for _, id := range *order {
		if id == groupUserTargetID {
			t.Fatalf("the user sentinel must never be handed to the runner; order=%v", *order)
		}
		seen[id] = true
	}
	if !seen[byName["A"]] || !seen[byName["B"]] {
		t.Fatalf("both AI members must speak even when User is named first; order=%v", *order)
	}
	if peak < 2 {
		t.Fatalf("the AI members must still run concurrently (peak >= 2) when User is named first, got %d", peak)
	}
}

// A parallel group larger than the launch cap runs in batches: peak concurrency
// never exceeds maxParallelLaunches. This bounds how many agent processes run
// at once, while the snapshot-then-launch invariant (every prompt fixed before
// any launch) keeps the members mutually non-referencing.
func TestFreeLoop_ParallelRespectsLaunchCap(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/free-parallel-cap"
	// One more member than the cap, so batching must actually trigger.
	names := []string{"A", "B", "C", "D"}
	groupID, byName := createFreeGroup(t, project, names...)

	script := map[string][]string{}
	for _, n := range names {
		script[byName[n]] = []string{n + " 的回答"}
	}

	var mu sync.Mutex
	cur, peak := 0, 0
	prompts := map[string]string{}
	base, _ := freeScriptedRunner(t, project, groupID, script)
	runner := func(ctx context.Context, gid string, turn groupMemberTurn) groupMemberResult {
		mu.Lock()
		cur++
		if cur > peak {
			peak = cur
		}
		prompts[turn.MemberRowID] = turn.Prompt
		mu.Unlock()
		// Hold briefly so overlaps are actually observable.
		time.Sleep(20 * time.Millisecond)
		r := base(ctx, gid, turn)
		mu.Lock()
		cur--
		mu.Unlock()
		return r
	}

	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	msg := `<clawbench-mention targets="` + byName["A"] + `,` + byName["B"] + `,` + byName["C"] + `,` + byName["D"] + `" mode="parallel">同时回答</clawbench-mention> 开始`
	_ = runGroupTurnForTest(t, o, project, msg, nil)

	mu.Lock()
	defer mu.Unlock()
	if peak > maxParallelLaunches {
		t.Fatalf("peak concurrency %d exceeded the launch cap %d", peak, maxParallelLaunches)
	}
	if peak < 2 {
		t.Fatalf("expected real concurrency (peak >= 2), got %d — the parallel path did not run concurrently", peak)
	}
	// Even the last batch must not have seen an earlier sibling's output: all
	// prompts came from the same group-start snapshot.
	for _, n := range names {
		p := prompts[byName[n]]
		if !strings.Contains(p, "同时回答") {
			t.Fatalf("%s did not receive the instruction; prompt=%q", n, p)
		}
		for _, other := range names {
			if other != n && strings.Contains(p, other+" 的回答") {
				t.Fatalf("%s saw sibling %s's output (batching broke the snapshot invariant); prompt=%q", n, other, p)
			}
		}
	}
}

// peakConcurrencyRunner wraps a base runner, recording peak simultaneous
// executions and each member's prompt. Members hold briefly so overlaps are
// observable — a sequential group can never reach peak >= 2. The returned
// prompts map is shared by reference and read after the turn completes.
func peakConcurrencyRunner(t *testing.T, project, groupID string, script map[string][]string) (groupTurnRunner, *[]string, *int, map[string]string) {
	t.Helper()
	var mu sync.Mutex
	cur, peak := 0, 0
	prompts := map[string]string{}
	base, order := freeScriptedRunner(t, project, groupID, script)
	runner := func(ctx context.Context, gid string, turn groupMemberTurn) groupMemberResult {
		mu.Lock()
		cur++
		if cur > peak {
			peak = cur
		}
		prompts[turn.MemberRowID] = turn.Prompt
		mu.Unlock()
		time.Sleep(20 * time.Millisecond)
		r := base(ctx, gid, turn)
		mu.Lock()
		cur--
		mu.Unlock()
		return r
	}
	return runner, order, &peak, prompts
}

// With the "并发执行" switch ON, the user @-naming SEVERAL members (which the
// cards serialize as SEPARATE tags) must merge into ONE parallel group: they
// run concurrently and none sees a sibling's output this round. Without the
// switch the same message is sequential — so this pins that the switch, not the
// tag shape, decides.
func TestFreeLoop_ParallelSwitchMergesUserMentions(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/free-switch-merge"
	groupID, byName := createFreeGroup(t, project, "A", "B")
	if err := SetGroupParallelDefault(groupID, true); err != nil {
		t.Fatalf("SetGroupParallelDefault: %v", err)
	}

	runner, order, peak, prompts := peakConcurrencyRunner(t, project, groupID, map[string][]string{
		byName["A"]: {"A 的回答"},
		byName["B"]: {"B 的回答"},
	})
	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	// Two SEPARATE tags (exactly what the member cards serialize to) — the
	// switch must still merge them into one parallel group.
	msg := `<clawbench-mention targets="` + byName["A"] + `"></clawbench-mention> ` +
		`<clawbench-mention targets="` + byName["B"] + `"></clawbench-mention> 同时回答`
	_ = runGroupTurnForTest(t, o, project, msg, nil)

	if *peak < 2 {
		t.Fatalf("the switch must merge the user's mentions into one parallel group (peak >= 2), got %d", *peak)
	}
	if len(*order) != 2 {
		t.Fatalf("both members must speak exactly once, order=%v", *order)
	}
	if strings.Contains(prompts[byName["A"]], "B 的回答") || strings.Contains(prompts[byName["B"]], "A 的回答") {
		t.Fatalf("parallel members must not see a sibling's output; A=%q B=%q",
			prompts[byName["A"]], prompts[byName["B"]])
	}
}

// With the switch ON and NO @-mentions, the whole active roster is one parallel
// group (previously one SEQUENTIAL group).
func TestFreeLoop_ParallelSwitchNoMentionRunsAllConcurrently(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/free-switch-all"
	groupID, byName := createFreeGroup(t, project, "A", "B", "C")
	if err := SetGroupParallelDefault(groupID, true); err != nil {
		t.Fatalf("SetGroupParallelDefault: %v", err)
	}

	runner, order, peak, _ := peakConcurrencyRunner(t, project, groupID, map[string][]string{
		byName["A"]: {"A 的回答"},
		byName["B"]: {"B 的回答"},
		byName["C"]: {"C 的回答"},
	})
	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	_ = runGroupTurnForTest(t, o, project, "大家同时说", nil)

	if *peak < 2 {
		t.Fatalf("the switch must run the whole roster concurrently (peak >= 2), got %d", *peak)
	}
	if len(*order) != 3 {
		t.Fatalf("all three members must speak, order=%v", *order)
	}
}

// The switch must NOT override an AGENT's own `mode="parallel"`: with the
// switch OFF, a member's parallel tag still runs concurrently. (The switch
// governs only what the USER seeds.) Here the user seeds A (sequential); A's
// OWN reply @-names B and C with mode="parallel", which must run them
// concurrently regardless of the switch.
func TestFreeLoop_ParallelSwitchOffDoesNotOverrideAgentMode(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/free-switch-agent"
	groupID, byName := createFreeGroup(t, project, "A", "B", "C")
	// Switch stays at its default (false).

	runner, _, peak, prompts := peakConcurrencyRunner(t, project, groupID, map[string][]string{
		byName["A"]: {`<clawbench-mention targets="` + byName["B"] + `,` + byName["C"] + `" mode="parallel">你们同时说</clawbench-mention>`},
		byName["B"]: {"B 的回答"},
		byName["C"]: {"C 的回答"},
	})
	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	// The user seeds ONLY A; the parallel tag comes from A's own output.
	msg := `<clawbench-mention targets="` + byName["A"] + `"></clawbench-mention> 开始`
	_ = runGroupTurnForTest(t, o, project, msg, nil)

	if *peak < 2 {
		t.Fatalf("an agent's own mode=parallel must still run concurrently with the switch off, got peak %d", *peak)
	}
	if strings.Contains(prompts[byName["C"]], "B 的回答") || strings.Contains(prompts[byName["B"]], "C 的回答") {
		t.Fatalf("parallel members must not see a sibling's output; B=%q C=%q",
			prompts[byName["B"]], prompts[byName["C"]])
	}
}

// With the switch OFF, the user @-naming several members stays SEQUENTIAL (the
// long-standing behavior): the second member sees the first's output.
func TestFreeLoop_ParallelSwitchOffUserMentionsSequential(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/free-switch-off"
	groupID, byName := createFreeGroup(t, project, "A", "B")

	runner, order, peak, prompts := peakConcurrencyRunner(t, project, groupID, map[string][]string{
		byName["A"]: {"A 的回答"},
		byName["B"]: {"B 的回答"},
	})
	o := NewGroupOrchestrator(groupID)
	o.runTurn = runner
	msg := `<clawbench-mention targets="` + byName["A"] + `"></clawbench-mention> ` +
		`<clawbench-mention targets="` + byName["B"] + `"></clawbench-mention> 依次说`
	_ = runGroupTurnForTest(t, o, project, msg, nil)

	if *peak != 1 {
		t.Fatalf("with the switch off the user's mentions must stay sequential (peak == 1), got %d", *peak)
	}
	if len(*order) != 2 || (*order)[0] != byName["A"] || (*order)[1] != byName["B"] {
		t.Fatalf("sequential order must be preserved, order=%v", *order)
	}
	if !strings.Contains(prompts[byName["B"]], "A 的回答") {
		t.Fatalf("a sequential member must see the previous speaker's output; B=%q", prompts[byName["B"]])
	}
}
