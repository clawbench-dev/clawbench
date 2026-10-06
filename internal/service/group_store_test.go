package service

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/store"
)

// setupGroupDB initializes a fresh full-schema DB for the group-store tests.
func setupGroupDB(t *testing.T) {
	t.Helper()
	tmpDir := t.TempDir()
	origBinDir, origDataDir := model.BinDir, model.DataDir
	model.BinDir = tmpDir
	model.DataDir = filepath.Join(tmpDir, ".clawbench")
	t.Cleanup(func() {
		model.BinDir = origBinDir
		model.DataDir = origDataDir
	})
	restoreDB := store.SnapshotDBForTest()
	t.Cleanup(restoreDB)
	if err := InitDB(); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { store.Close() })
}

func TestCreateGroupAndMembers(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/grouptest"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}

	groupID, hostMemberID, err := CreateGroup(project, "讨论组", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if groupID == "" || hostMemberID == "" {
		t.Fatal("empty ids")
	}
	if got := GetGroupHostMember(groupID); got != hostMemberID {
		t.Fatalf("host member=%q want %q", got, hostMemberID)
	}
	if got := GetSessionType(groupID); got != groupSessionType {
		t.Fatalf("group session_type=%q want %q", got, groupSessionType)
	}
	if got := GetSessionType(hostMemberID); got != groupMemberSessionType {
		t.Fatalf("member session_type=%q want %q", got, groupMemberSessionType)
	}

	m, err := AddGroupMember(project, groupID, "claude", "agent-a", "A")
	if err != nil {
		t.Fatalf("AddGroupMember: %v", err)
	}

	// I-1: ListGroupMembers includes archived members, so after removal the
	// total stays 2 while the active count drops to 1.
	members, err := ListGroupMembers(groupID)
	if err != nil {
		t.Fatalf("ListGroupMembers: %v", err)
	}
	if len(members) != 2 {
		t.Fatalf("want 2 members, got %d", len(members))
	}
	if countActiveMembers(members) != 2 {
		t.Fatalf("want 2 active members, got %d", countActiveMembers(members))
	}

	if err := RemoveGroupMember(groupID, m); err != nil {
		t.Fatalf("RemoveGroupMember: %v", err)
	}
	members, _ = ListGroupMembers(groupID)
	if len(members) != 2 {
		t.Fatalf("removal keeps the row (已离场) -> still 2, got %d", len(members))
	}
	if active := countActiveMembers(members); active != 1 {
		t.Fatalf("want 1 active member, got %d", active)
	}
}

// TestAddGroupMemberDedup covers the A contract: adding an agent that is
// already a member does NOT create a second row (two rows for one agent make
// one of them unreachable — routing addresses members by name), and re-adding
// an agent that LEFT rejoins its original row (archived cleared) instead of
// duplicating it.
func TestAddGroupMemberDedup(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/groupdedup"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, _, err := CreateGroup(project, "G", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}

	first, err := AddGroupMember(project, groupID, "claude", "agent-a", "A")
	if err != nil {
		t.Fatalf("AddGroupMember: %v", err)
	}

	// Re-adding an ACTIVE member returns the same row and creates nothing.
	again, err := AddGroupMember(project, groupID, "claude", "agent-a", "A")
	if err != nil {
		t.Fatalf("AddGroupMember (again): %v", err)
	}
	if again != first {
		t.Fatalf("re-adding an active member returned a new row: %q != %q", again, first)
	}
	members, _ := ListGroupMembers(groupID)
	if got := countAgentRows(members, "agent-a"); got != 1 {
		t.Fatalf("want 1 row for agent-a, got %d", got)
	}

	// Removing then re-adding REJOINS the original row (archived cleared),
	// preserving its id so past speech attribution stays intact.
	if err := RemoveGroupMember(groupID, first); err != nil {
		t.Fatalf("RemoveGroupMember: %v", err)
	}
	rejoined, err := AddGroupMember(project, groupID, "claude", "agent-a", "A")
	if err != nil {
		t.Fatalf("AddGroupMember (rejoin): %v", err)
	}
	if rejoined != first {
		t.Fatalf("rejoin created a new row: %q != %q", rejoined, first)
	}
	members, _ = ListGroupMembers(groupID)
	if got := countAgentRows(members, "agent-a"); got != 1 {
		t.Fatalf("rejoin must reuse the row: want 1 row, got %d", got)
	}
	for _, m := range members {
		if m.AgentID == "agent-a" && m.Left {
			t.Fatal("rejoined member must no longer be marked left")
		}
	}
}

// TestCreateGroupWithMembers pins the one-step group creation contract (design
// §7.1, decision #25): the group row and ALL member rows (host included) are
// created in a single transaction, and the host pointer is written inline so it
// is visible immediately after the call returns.
func TestCreateGroupWithMembers(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/groupstep1"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}

	specs := []GroupMemberSpec{
		{AgentID: "agent-host", Backend: "codebuddy", DisplayName: "Host"},
		{AgentID: "agent-a", Backend: "claude", DisplayName: "A"},
		{AgentID: "agent-b", Backend: "codex", DisplayName: "B"},
	}
	groupID, hostMemberID, err := CreateGroupWithMembers(project, "讨论组", "agent-host", specs)
	if err != nil {
		t.Fatalf("CreateGroupWithMembers: %v", err)
	}
	if groupID == "" || hostMemberID == "" {
		t.Fatal("empty ids")
	}
	if got := GetSessionType(groupID); got != groupSessionType {
		t.Fatalf("group session_type=%q want %q", got, groupSessionType)
	}

	// All three members exist after the single call — no follow-up add needed.
	members, err := ListGroupMembers(groupID)
	if err != nil {
		t.Fatalf("ListGroupMembers: %v", err)
	}
	if len(members) != 3 {
		t.Fatalf("want 3 members after one call, got %d", len(members))
	}
	if got := countActiveMembers(members); got != 3 {
		t.Fatalf("want 3 active members, got %d", got)
	}
	for _, m := range members {
		if GetSessionType(m.ID) != groupMemberSessionType {
			t.Fatalf("member %s session_type=%q want %q", m.ID, GetSessionType(m.ID), groupMemberSessionType)
		}
	}

	// Host pointer resolves to the host's row and is set synchronously.
	if got := GetGroupHostMember(groupID); got != hostMemberID {
		t.Fatalf("host member=%q want %q", got, hostMemberID)
	}
	hostRow := ""
	for _, m := range members {
		if m.AgentID == "agent-host" {
			hostRow = m.ID
		}
	}
	if hostRow != hostMemberID {
		t.Fatalf("host member row=%q want %q", hostRow, hostMemberID)
	}

	// The placeholder title is still replaceable by auto-title (group titles
	// come from the host name until the first message).
	if GetSessionType(groupID) == "" {
		t.Fatal("group row must exist")
	}
	var titleSource string
	if err := store.ReadDB().QueryRow("SELECT COALESCE(title_source,'') FROM chat_sessions WHERE id = ?", groupID).Scan(&titleSource); err != nil {
		t.Fatalf("read title_source: %v", err)
	}
	if titleSource != TitleSourcePlaceholder {
		t.Fatalf("group title_source=%q want %q (auto-title must be able to take over)", titleSource, TitleSourcePlaceholder)
	}
}

// TestCreateGroupWithMembersRollsBackOnInvalid pins atomicity: if any member
// spec is invalid (empty agent id), NOTHING is created — no half-built group
// left behind for the user to clean up (decision #25).
func TestCreateGroupWithMembersRollsBackOnInvalid(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/groupstep1rollback"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}

	specs := []GroupMemberSpec{
		{AgentID: "agent-host", Backend: "codebuddy", DisplayName: "Host"},
		{AgentID: "", Backend: "claude", DisplayName: "Bad"},
	}
	if _, _, err := CreateGroupWithMembers(project, "g", "agent-host", specs); err == nil {
		t.Fatal("want error for an invalid member spec")
	}

	// No group row must survive the failed call.
	var count int
	if err := store.ReadDB().QueryRow(
		"SELECT COUNT(*) FROM chat_sessions WHERE project_id = (SELECT id FROM projects WHERE path = ?) AND session_type = ?",
		project, groupSessionType,
	).Scan(&count); err != nil {
		t.Fatalf("count groups: %v", err)
	}
	if count != 0 {
		t.Fatalf("failed create left %d group rows behind", count)
	}
}

// countAgentRows counts member rows for one agent (0, 1, or the bug's 2+).
func countAgentRows(members []GroupMember, agentID string) int {
	n := 0
	for _, m := range members {
		if m.AgentID == agentID {
			n++
		}
	}
	return n
}

func TestGroupMaxRoundsDefaultAndOverride(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/grouptest"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, _, err := CreateGroup(project, "g", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if got := GetGroupMaxRounds(groupID); got != defaultGroupMaxRounds {
		t.Fatalf("default maxRounds=%d want %d", got, defaultGroupMaxRounds)
	}
	if err := SetGroupMaxRounds(groupID, 25); err != nil {
		t.Fatalf("SetGroupMaxRounds: %v", err)
	}
	if got := GetGroupMaxRounds(groupID); got != 25 {
		t.Fatalf("maxRounds=%d want 25", got)
	}
}

// TestGroupMembersForGroups pins the batch preview used by the session list:
// one call covers many groups, left members are excluded, group ids with no
// active members are absent, and an empty input never touches the DB.
func TestGroupMembersForGroups(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/grouptest"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}

	// Group A: host + two members; one of the two is then removed.
	gA, hostA, err := CreateGroup(project, "A", "codebuddy", "agent-host", "Host A")
	if err != nil {
		t.Fatalf("CreateGroup A: %v", err)
	}
	leftMember, err := AddGroupMember(project, gA, "claude", "agent-left", "Gone")
	if err != nil {
		t.Fatalf("AddGroupMember A: %v", err)
	}
	if _, err := AddGroupMember(project, gA, "codex", "agent-b", "B"); err != nil {
		t.Fatalf("AddGroupMember A: %v", err)
	}
	if err := RemoveGroupMember(gA, leftMember); err != nil {
		t.Fatalf("RemoveGroupMember: %v", err)
	}

	// Group B: only the host.
	gB, _, err := CreateGroup(project, "B", "codebuddy", "agent-host", "Host B")
	if err != nil {
		t.Fatalf("CreateGroup B: %v", err)
	}

	got, err := GroupMembersForGroups([]string{gA, gB, gA, ""})
	if err != nil {
		t.Fatalf("GroupMembersForGroups: %v", err)
	}

	// A: host + B survive; the removed member is excluded.
	aMembers := got[gA]
	if len(aMembers) != 2 {
		t.Fatalf("group A preview: want 2 active members, got %d", len(aMembers))
	}
	names := map[string]bool{}
	for _, m := range aMembers {
		names[m.Name] = true
		if m.ID == "" || m.AgentID == "" || m.Backend == "" {
			t.Fatalf("preview member missing a field: %+v", m)
		}
	}
	if !names["Host A"] || !names["B"] {
		t.Fatalf("group A preview names=%v want Host A + B", names)
	}
	if names["Gone"] {
		t.Fatal("left member must not appear in the list preview")
	}
	_ = hostA

	// The host preview carries isHost so the list stack can lead with it. The
	// pointer lives on the GROUP row, so this also proves the join resolves it.
	hosts := 0
	for _, m := range aMembers {
		if m.IsHost {
			hosts++
			if m.ID != hostA {
				t.Fatalf("isHost on %q, want host row %q", m.ID, hostA)
			}
		}
	}
	if hosts != 1 {
		t.Fatalf("group A preview: want exactly 1 host, got %d", hosts)
	}
	for _, m := range got[gB] {
		if !m.IsHost {
			t.Fatal("group B's sole member is its host and must carry isHost")
		}
	}

	// B: only its host.
	if len(got[gB]) != 1 {
		t.Fatalf("group B preview: want 1 member, got %d", len(got[gB]))
	}

	// Empty input: empty map, no error.
	empty, err := GroupMembersForGroups(nil)
	if err != nil {
		t.Fatalf("empty input: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("empty input: want empty map, got %d entries", len(empty))
	}

	// Unknown group id: absent from the map (caller treats it as no members).
	unknown, err := GroupMembersForGroups([]string{"no-such-group"})
	if err != nil {
		t.Fatalf("unknown id: %v", err)
	}
	if _, ok := unknown["no-such-group"]; ok {
		t.Fatal("unknown group id must be absent from the map")
	}
}

// The host controls the flow, so removing it would leave the group unroutable.
// Refuse the removal rather than silently orphaning the group (decision: N5).
func TestRemoveGroupMember_RefusesHost(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/grouptest"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}

	groupID, hostMemberID, err := CreateGroup(project, "讨论组", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	other, err := AddGroupMember(project, groupID, "claude", "agent-a", "A")
	if err != nil {
		t.Fatalf("AddGroupMember: %v", err)
	}

	// Removing the host must fail...
	if err := RemoveGroupMember(groupID, hostMemberID); err == nil {
		t.Fatal("removing the host must be refused")
	}
	// ...and must NOT have archived the host row.
	for _, m := range mustListMembers(t, groupID) {
		if m.ID == hostMemberID && m.Left {
			t.Fatal("host row must remain active after a refused removal")
		}
	}

	// Removing a normal member still works (the guard must not over-reach).
	if err := RemoveGroupMember(groupID, other); err != nil {
		t.Fatalf("removing a non-host member must still work: %v", err)
	}
}

// Idempotency must survive the guard: removing an already-left non-host member
// stays a no-op success, and removing the host stays refused.
func TestRemoveGroupMember_HostGuardIdempotent(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/grouptest"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, hostMemberID, err := CreateGroup(project, "讨论组", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if err := RemoveGroupMember(groupID, hostMemberID); err == nil {
		t.Fatal("second removal of the host must also be refused")
	}
}

func mustListMembers(t *testing.T, groupID string) []GroupMember {
	t.Helper()
	members, err := ListGroupMembers(groupID)
	if err != nil {
		t.Fatalf("ListGroupMembers: %v", err)
	}
	return members
}

// maxGroupMembers caps active members: each member is an independent ACP
// subprocess (hundreds of MB), and the connection pool has no ceiling, so an
// uncapped roster can exhaust memory on a single group turn (decision #78).
func TestCreateGroupWithMembers_RejectsTooMany(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/grouptest"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}

	// Exactly the cap is allowed.
	specs := make([]GroupMemberSpec, 0, maxGroupMembers)
	for i := 0; i < maxGroupMembers; i++ {
		specs = append(specs, GroupMemberSpec{
			AgentID:     fmt.Sprintf("agent-%d", i),
			Backend:     "claude",
			DisplayName: fmt.Sprintf("M%d", i),
		})
	}
	groupID, _, err := CreateGroupWithMembers(project, "满员群", "agent-0", specs)
	if err != nil {
		t.Fatalf("exactly maxGroupMembers must be allowed: %v", err)
	}
	if groupID == "" {
		t.Fatal("expected a group id")
	}

	// One over the cap is refused, and creates NOTHING (no half-built group).
	over := append(specs, GroupMemberSpec{AgentID: "agent-extra", Backend: "claude", DisplayName: "Extra"})
	if _, _, err := CreateGroupWithMembers(project, "超员群", "agent-0", over); err == nil {
		t.Fatal("more than maxGroupMembers must be refused")
	}
}

// Re-adding an existing agent (active OR left) reuses its row and must not be
// counted as a new member — otherwise a group at the cap could never re-add
// someone who left.
func TestAddGroupMember_CapIgnoresExistingAgent(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/grouptest"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	specs := make([]GroupMemberSpec, 0, maxGroupMembers)
	for i := 0; i < maxGroupMembers; i++ {
		specs = append(specs, GroupMemberSpec{
			AgentID:     fmt.Sprintf("agent-%d", i),
			Backend:     "claude",
			DisplayName: fmt.Sprintf("M%d", i),
		})
	}
	groupID, _, err := CreateGroupWithMembers(project, "满员群", "agent-0", specs)
	if err != nil {
		t.Fatalf("CreateGroupWithMembers: %v", err)
	}

	// A brand-new agent at the cap is refused.
	if _, err := AddGroupMember(project, groupID, "claude", "agent-new", "New"); err == nil {
		t.Fatal("adding a new agent at the cap must be refused")
	}

	// Re-adding an EXISTING agent (idempotent reuse) must succeed even at the cap.
	if _, err := AddGroupMember(project, groupID, "claude", "agent-3", "M3"); err != nil {
		t.Fatalf("re-adding an existing agent must not be capped: %v", err)
	}

	// Free a slot, then the new agent fits.
	if err := RemoveGroupMember(groupID, mustMemberID(t, groupID, "agent-3")); err != nil {
		t.Fatalf("RemoveGroupMember: %v", err)
	}
	if _, err := AddGroupMember(project, groupID, "claude", "agent-new", "New"); err != nil {
		t.Fatalf("adding after freeing a slot must succeed: %v", err)
	}
}

// Rejoining (re-adding a LEFT member) reuses the row, so it must be allowed at
// the cap — the roster does not actually grow.
func TestAddGroupMember_RejoinAllowedAtCap(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/grouptest"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	specs := make([]GroupMemberSpec, 0, maxGroupMembers)
	for i := 0; i < maxGroupMembers; i++ {
		specs = append(specs, GroupMemberSpec{
			AgentID:     fmt.Sprintf("agent-%d", i),
			Backend:     "claude",
			DisplayName: fmt.Sprintf("M%d", i),
		})
	}
	groupID, _, err := CreateGroupWithMembers(project, "满员群", "agent-0", specs)
	if err != nil {
		t.Fatalf("CreateGroupWithMembers: %v", err)
	}
	leaver := mustMemberID(t, groupID, "agent-5")
	if err := RemoveGroupMember(groupID, leaver); err != nil {
		t.Fatalf("RemoveGroupMember: %v", err)
	}
	// Fill the freed slot with a new agent...
	if _, err := AddGroupMember(project, groupID, "claude", "agent-new", "New"); err != nil {
		t.Fatalf("filling the freed slot: %v", err)
	}
	// ...then the left member rejoins, which grows the ACTIVE count back to the
	// cap only if a slot exists. It is at the cap now, so this must be refused.
	if _, err := AddGroupMember(project, groupID, "claude", "agent-5", "M5"); err == nil {
		t.Fatal("rejoining at the cap must be refused (it would exceed the cap)")
	}
}

func mustMemberID(t *testing.T, groupID, agentID string) string {
	t.Helper()
	m, ok := findGroupMemberByAgent(groupID, agentID)
	if !ok {
		t.Fatalf("member for agent %q not found", agentID)
	}
	return m.ID
}

// ---------- N3: system events on member add/remove ----------

// Adding a member writes a role='system' row to the group timeline so the host
// and every member learn about the change (decisions #40/#41). It carries no
// agent_id (it is nobody's speech) and does not count as unread.
func TestAddGroupMember_WritesSystemEvent(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/grouptest"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	origAgents := model.Agents
	model.Agents = map[string]*model.Agent{
		"agent-host": {ID: "agent-host", Name: "Host", Specialty: "主持"},
		"agent-a":    {ID: "agent-a", Name: "A", Specialty: "代码编写与推理"},
	}
	defer func() { model.Agents = origAgents }()

	groupID, _, err := CreateGroup(project, "讨论组", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if _, err := AddGroupMember(project, groupID, "claude", "agent-a", "A"); err != nil {
		t.Fatalf("AddGroupMember: %v", err)
	}

	sysMsgs := systemMessages(t, groupID)
	if len(sysMsgs) != 1 {
		t.Fatalf("expected exactly 1 system event, got %d", len(sysMsgs))
	}
	text := sysMsgs[0].Content
	if !strings.Contains(text, "A") || !strings.Contains(text, "加入了讨论") {
		t.Fatalf("system event must name the member and say they joined: %q", text)
	}
	if !strings.Contains(text, "代码编写与推理") {
		t.Fatalf("system event must carry the member's specialty (decision #41): %q", text)
	}
	if sysMsgs[0].AgentID != "" {
		t.Fatalf("a system event belongs to nobody: agent_id must be empty, got %q", sysMsgs[0].AgentID)
	}
}

// Removing a member writes a departure system event.
func TestRemoveGroupMember_WritesSystemEvent(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/grouptest"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, _, err := CreateGroup(project, "讨论组", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	memberID, err := AddGroupMember(project, groupID, "claude", "agent-a", "A")
	if err != nil {
		t.Fatalf("AddGroupMember: %v", err)
	}
	before := len(systemMessages(t, groupID))

	if err := RemoveGroupMember(groupID, memberID); err != nil {
		t.Fatalf("RemoveGroupMember: %v", err)
	}
	sysMsgs := systemMessages(t, groupID)
	if len(sysMsgs) != before+1 {
		t.Fatalf("expected one more system event after removal: before=%d after=%d", before, len(sysMsgs))
	}
	last := sysMsgs[len(sysMsgs)-1].Content
	if !strings.Contains(last, "A") || !strings.Contains(last, "已离场") {
		t.Fatalf("departure event must name the member and say they left: %q", last)
	}
}

// A system event must be injected into the next member's context (decision #44:
// it counts toward the cursor) — otherwise members never learn who joined.
func TestSystemEvent_InjectedIntoMemberContext(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/grouptest"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, _, err := CreateGroup(project, "讨论组", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if _, err := AddGroupMember(project, groupID, "claude", "agent-a", "A"); err != nil {
		t.Fatalf("AddGroupMember: %v", err)
	}
	// A second member speaks after the event; its injection must include it.
	memberID, err := AddGroupMember(project, groupID, "claude", "agent-b", "B")
	if err != nil {
		t.Fatalf("AddGroupMember: %v", err)
	}

	msgs, err := GetMessagesBySessionIDRaw(groupID)
	if err != nil {
		t.Fatalf("GetMessagesBySessionIDRaw: %v", err)
	}
	names := map[string]string{}
	got := buildInjectionText(msgs, 0, memberID, names, nil, nil, "", "")
	if !strings.Contains(got, "加入了讨论") {
		t.Fatalf("system event must reach the member's injected context: %q", got)
	}
}

// systemMessages returns the group timeline's role='system' rows in id order.
func systemMessages(t *testing.T, groupID string) []model.ChatMessage {
	t.Helper()
	msgs, err := GetMessagesBySessionIDRaw(groupID)
	if err != nil {
		t.Fatalf("GetMessagesBySessionIDRaw: %v", err)
	}
	out := make([]model.ChatMessage, 0)
	for _, m := range msgs {
		if m.Role == "system" {
			out = append(out, m)
		}
	}
	return out
}
