package service

import (
	"path/filepath"
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
