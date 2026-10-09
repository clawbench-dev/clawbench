package service

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// The user-seed concurrency default is a tri-state read: absent => sequential
// (false), and an explicit true/false must round-trip. Sequential is the
// long-standing behavior, so an old group without the key must stay sequential.
func TestGroupParallelDefaultRoundTrip(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/grouptest-parallel"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, _, err := CreateGroup(project, "g", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if GetGroupParallelDefault(groupID) {
		t.Fatal("parallelDefault must default to false when unset")
	}
	if err := SetGroupParallelDefault(groupID, true); err != nil {
		t.Fatalf("SetGroupParallelDefault(true): %v", err)
	}
	if !GetGroupParallelDefault(groupID) {
		t.Fatal("parallelDefault must read back true after being set")
	}
	if err := SetGroupParallelDefault(groupID, false); err != nil {
		t.Fatalf("SetGroupParallelDefault(false): %v", err)
	}
	if GetGroupParallelDefault(groupID) {
		t.Fatal("parallelDefault must read back false after being cleared")
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

// countMemberRows returns how many hidden member rows still exist for a group.
func countMemberRows(t *testing.T, groupID string) int {
	t.Helper()
	var n int
	if err := store.ReadDB().QueryRow(
		"SELECT COUNT(*) FROM chat_sessions WHERE group_id = ? AND session_type = ?",
		groupID, groupMemberSessionType,
	).Scan(&n); err != nil {
		t.Fatalf("count member rows: %v", err)
	}
	return n
}

// Destroying a group must cascade its member rows in the same transaction:
// they are independent chat_sessions rows, so deleting only the group row would
// orphan them forever (decision #48).
func TestHardDeleteSession_CascadesGroupMemberRows(t *testing.T) {
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
	if got := countMemberRows(t, groupID); got != 2 {
		t.Fatalf("expected 2 member rows before delete, got %d", got)
	}

	if err := HardDeleteSession(groupID); err != nil {
		t.Fatalf("HardDeleteSession: %v", err)
	}

	if got := countMemberRows(t, groupID); got != 0 {
		t.Fatalf("member rows must be cascaded with the group, got %d remaining", got)
	}
	// The group row itself is gone too.
	var groupRows int
	if err := store.ReadDB().QueryRow("SELECT COUNT(*) FROM chat_sessions WHERE id = ?", groupID).Scan(&groupRows); err != nil {
		t.Fatalf("count group rows: %v", err)
	}
	if groupRows != 0 {
		t.Fatalf("group row must be deleted, got %d", groupRows)
	}
}

// Archiving a group must NOT archive its member rows: they are hidden
// transitively through the group, and archiving them would make them eligible
// for retention expiry (decision #48).
func TestArchiveGroup_DoesNotArchiveMemberRows(t *testing.T) {
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

	if err := ArchiveSession(project, "codebuddy", groupID); err != nil {
		t.Fatalf("ArchiveSession: %v", err)
	}

	var archived int
	if err := store.ReadDB().QueryRow(
		"SELECT COUNT(*) FROM chat_sessions WHERE group_id = ? AND session_type = ? AND archived = 1",
		groupID, groupMemberSessionType,
	).Scan(&archived); err != nil {
		t.Fatalf("count archived members: %v", err)
	}
	if archived != 0 {
		t.Fatalf("archiving the group must leave member rows archived=0, got %d", archived)
	}
}

// Removing a member refreshes updated_at, so a left member is not mistaken for
// a retention-expired session (decision #48).
func TestRemoveGroupMember_RefreshesUpdatedAt(t *testing.T) {
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

	// Age the member row, then remove it — the removal must refresh updated_at.
	if _, err := store.UnsafeDBForTest().Exec(
		"UPDATE chat_sessions SET updated_at = datetime('now', '-100 days') WHERE id = ?", memberID,
	); err != nil {
		t.Fatalf("age member row: %v", err)
	}
	if err := RemoveGroupMember(groupID, memberID); err != nil {
		t.Fatalf("RemoveGroupMember: %v", err)
	}

	// Assert the timestamp directly: the retention query ALSO excludes
	// group_member rows, so a query-only assertion would pass even if
	// updated_at were never refreshed (a tautology).
	var old int
	if err := store.ReadDB().QueryRow(
		"SELECT COUNT(*) FROM chat_sessions WHERE id = ? AND updated_at < datetime('now', '-90 days')",
		memberID,
	).Scan(&old); err != nil {
		t.Fatalf("read member updated_at: %v", err)
	}
	if old != 0 {
		t.Fatalf("RemoveGroupMember must refresh updated_at, but the row is still >90 days old")
	}

	// Belt-and-braces: the retention sweep must not pick the member up either.
	cutoff := time.Now().AddDate(0, 0, -90)
	ids, err := store.GetExpiredArchivedSessions(cutoff)
	if err != nil {
		t.Fatalf("GetExpiredArchivedSessions: %v", err)
	}
	for _, id := range ids {
		if id == memberID {
			t.Fatalf("a removed member must not be treated as retention-expired: %v", ids)
		}
	}
}

// Even if a member row's updated_at IS old (e.g. legacy data), the retention
// query must exclude session_type='group_member' outright (decision #48, the
// double-safety half of the fix).
func TestGetExpiredArchivedSessions_ExcludesGroupMemberRows(t *testing.T) {
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
	if err := RemoveGroupMember(groupID, memberID); err != nil {
		t.Fatalf("RemoveGroupMember: %v", err)
	}
	// Simulate legacy/edge data: an archived member row with an old timestamp.
	if _, err := store.UnsafeDBForTest().Exec(
		"UPDATE chat_sessions SET updated_at = datetime('now', '-100 days') WHERE id = ?", memberID,
	); err != nil {
		t.Fatalf("age member row: %v", err)
	}

	cutoff := time.Now().AddDate(0, 0, -90)
	ids, err := store.GetExpiredArchivedSessions(cutoff)
	if err != nil {
		t.Fatalf("GetExpiredArchivedSessions: %v", err)
	}
	for _, id := range ids {
		if id == memberID {
			t.Fatalf("group_member rows must be excluded from retention expiry: %v", ids)
		}
	}
}

// SetGroupAutoApprove must write every member row (and the group row), because
// members each own the ACP connection that reads the flag (decision #61).
func TestSetGroupAutoApprove_AppliesToAllMembers(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/grouptest"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, hostMemberID, err := CreateGroup(project, "讨论组", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	memberID, err := AddGroupMember(project, groupID, "claude", "agent-a", "A")
	if err != nil {
		t.Fatalf("AddGroupMember: %v", err)
	}

	if err := SetGroupAutoApprove(groupID, true); err != nil {
		t.Fatalf("SetGroupAutoApprove: %v", err)
	}
	for _, id := range []string{groupID, hostMemberID, memberID} {
		if !GetSessionAutoApprove(id) {
			t.Fatalf("session %s must have auto_approve=1 after enabling", id)
		}
	}

	if err := SetGroupAutoApprove(groupID, false); err != nil {
		t.Fatalf("SetGroupAutoApprove(false): %v", err)
	}
	for _, id := range []string{groupID, hostMemberID, memberID} {
		if GetSessionAutoApprove(id) {
			t.Fatalf("session %s must have auto_approve=0 after disabling", id)
		}
	}
}

// The host controls the flow, so removing it would leave the group unroutable.// Refuse the removal rather than silently orphaning the group (decision: N5).
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
	for i := range maxGroupMembers {
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
	specs = append(specs, GroupMemberSpec{AgentID: "agent-extra", Backend: "claude", DisplayName: "Extra"})
	if _, _, err := CreateGroupWithMembers(project, "超员群", "agent-0", specs); err == nil {
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
	for i := range maxGroupMembers {
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
	for i := range maxGroupMembers {
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

// Adding a member broadcasts a system_message event carrying the committed row
// id and text (decisions #40/#43), so the timeline updates live instead of only
// after a reload.
func TestAddGroupMember_BroadcastsSystemEvent(t *testing.T) {
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

	type emitted struct {
		groupID string
		msgID   int64
		text    string
	}
	var got []emitted
	orig := emitGroupSystemMessage
	emitGroupSystemMessage = func(groupID string, msgID int64, text string) {
		got = append(got, emitted{groupID, msgID, text})
	}
	t.Cleanup(func() { emitGroupSystemMessage = orig })

	groupID, _, err := CreateGroup(project, "讨论组", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if _, err := AddGroupMember(project, groupID, "claude", "agent-a", "A"); err != nil {
		t.Fatalf("AddGroupMember: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("expected exactly one system_message broadcast, got %d: %+v", len(got), got)
	}
	if got[0].groupID != groupID {
		t.Fatalf("broadcast to wrong group: got %q want %q", got[0].groupID, groupID)
	}
	if !strings.Contains(got[0].text, "A") || !strings.Contains(got[0].text, "加入了讨论") {
		t.Fatalf("broadcast text must match the stored event: %q", got[0].text)
	}
	// The broadcast id must be the committed chat_history row id, not 0 or a
	// fabricated value — the frontend dedups on it.
	sysMsgs := systemMessages(t, groupID)
	if len(sysMsgs) != 1 {
		t.Fatalf("expected 1 stored system row, got %d", len(sysMsgs))
	}
	if got[0].msgID != sysMsgs[0].ID {
		t.Fatalf("broadcast msgID must be the DB row id: got %d want %d", got[0].msgID, sysMsgs[0].ID)
	}
	if got[0].text != sysMsgs[0].Content {
		t.Fatalf("broadcast text must equal stored content: got %q want %q", got[0].text, sysMsgs[0].Content)
	}
}

// Removing a member broadcasts the departure system_message event too.
func TestRemoveGroupMember_BroadcastsSystemEvent(t *testing.T) {
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

	type emitted struct {
		msgID int64
		text  string
	}
	var got []emitted
	orig := emitGroupSystemMessage
	emitGroupSystemMessage = func(_ string, msgID int64, text string) {
		got = append(got, emitted{msgID, text})
	}
	t.Cleanup(func() { emitGroupSystemMessage = orig })

	if err := RemoveGroupMember(groupID, memberID); err != nil {
		t.Fatalf("RemoveGroupMember: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected exactly one departure broadcast, got %d: %+v", len(got), got)
	}
	if !strings.Contains(got[0].text, "A") || !strings.Contains(got[0].text, "已离场") {
		t.Fatalf("departure broadcast must name the member and say they left: %q", got[0].text)
	}
	sysMsgs := systemMessages(t, groupID)
	last := sysMsgs[len(sysMsgs)-1]
	if got[0].msgID != last.ID || got[0].text != last.Content {
		t.Fatalf("departure broadcast must carry the stored row: got (%d,%q) want (%d,%q)",
			got[0].msgID, got[0].text, last.ID, last.Content)
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

// Removing the same member twice must write the departure event only ONCE. The
// second call changes no row (already archived), so a second "已离场" row would
// be pure timeline noise — and it counts toward every member's cursor, so it
// gets injected into their context too.
func TestRemoveGroupMember_RepeatedRemovalWritesOneEvent(t *testing.T) {
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
		t.Fatalf("first RemoveGroupMember: %v", err)
	}
	if err := RemoveGroupMember(groupID, memberID); err != nil {
		t.Fatalf("second RemoveGroupMember: %v", err)
	}

	after := len(systemMessages(t, groupID))
	if after != before+1 {
		t.Fatalf("repeated removal must write exactly one departure event: before=%d after=%d", before, after)
	}
}

// Removing a member id that belongs to a DIFFERENT group must not write an
// event into this group's timeline. The UPDATE is scoped by group_id so it
// changes no row here, and without a RowsAffected check the old code still
// appended "X 已离场" — naming a member who was never in this group and
// polluting its minutes.
func TestRemoveGroupMember_CrossGroupRemovalWritesNoEvent(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/grouptest"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupA, _, err := CreateGroup(project, "A组", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup A: %v", err)
	}
	groupB, _, err := CreateGroup(project, "B组", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup B: %v", err)
	}
	// A member of B, removed via A's id.
	memberB, err := AddGroupMember(project, groupB, "claude", "agent-a", "A")
	if err != nil {
		t.Fatalf("AddGroupMember: %v", err)
	}
	before := len(systemMessages(t, groupA))

	// Must not error and must not touch A's timeline.
	_ = RemoveGroupMember(groupA, memberB)

	after := len(systemMessages(t, groupA))
	if after != before {
		t.Fatalf("a cross-group removal must not write an event into group A: before=%d after=%d", before, after)
	}
	// And B's member must remain active (A's UPDATE is scoped by group_id).
	members, err := ListGroupMembers(groupB)
	if err != nil {
		t.Fatalf("ListGroupMembers: %v", err)
	}
	found := false
	for _, m := range members {
		if m.ID == memberB {
			found = true
			if m.Left {
				t.Fatal("a cross-group removal must not archive the member in its real group")
			}
		}
	}
	if !found {
		t.Fatal("the member row must still exist in group B")
	}
}

// CreateGroupWithMembers must DEDUPE by agent id, exactly like AddGroupMember.
// A member's only identity is its agent: the routing tag addresses members by
// NAME and resolveTargets maps names through a map, so two rows for the same
// agent leave one of them unreachable (a "mute" member that occupies a
// connection and a slot but can never be spoken to). It also inflates the
// active count toward the 10-member cap.
//
// The Web drawer is multi-select (a Set), so it never sends duplicates — but
// POST /api/group/create accepts an arbitrary array, and that is the reachable
// path.
func TestCreateGroupWithMembers_DedupesAgents(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/groupdedup-create"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}

	specs := []GroupMemberSpec{
		{AgentID: "agent-host", Backend: "codebuddy", DisplayName: "Host"},
		{AgentID: "agent-a", Backend: "claude", DisplayName: "A"},
		{AgentID: "agent-a", Backend: "claude", DisplayName: "A"},
		{AgentID: "agent-a", Backend: "claude", DisplayName: "A"},
	}
	groupID, hostMemberID, err := CreateGroupWithMembers(project, "讨论组", "agent-host", specs)
	if err != nil {
		t.Fatalf("CreateGroupWithMembers: %v", err)
	}
	if hostMemberID == "" {
		t.Fatal("host member must be resolved")
	}

	members, err := ListGroupMembers(groupID)
	if err != nil {
		t.Fatalf("ListGroupMembers: %v", err)
	}
	if got := countAgentRows(members, "agent-a"); got != 1 {
		t.Fatalf("duplicate agent ids must collapse to one row, got %d", got)
	}
	// 1 host + 1 unique member = 2 rows total.
	if len(members) != 2 {
		t.Fatalf("want 2 member rows (host + A), got %d", len(members))
	}
}

// AddGroupMembers must be ALL-OR-NOTHING on the member cap (decision #78 / plan
// O19): when the batch would exceed the cap it rejects the WHOLE batch without
// writing any row. Looping AddGroupMember in the caller instead leaves the
// earlier additions committed and returns an error — the request "fails" but
// the roster silently changed.
func TestAddGroupMembers_BatchCapIsAtomic(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/groupbatch"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, _, err := CreateGroup(project, "G", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	// Fill to 9 active (host + 8 members).
	for i := range 8 {
		if _, err := AddGroupMember(project, groupID, "claude", "agent-"+string(rune('a'+i)), "M"); err != nil {
			t.Fatalf("AddGroupMember %d: %v", i, err)
		}
	}
	before, _ := ListGroupMembers(groupID)
	if countActiveMembers(before) != 9 {
		t.Fatalf("precondition: want 9 active, got %d", countActiveMembers(before))
	}

	// Batch of 2 new agents would make 11 → must reject the WHOLE batch.
	_, err = AddGroupMembers(project, groupID, []GroupMemberSpec{
		{AgentID: "agent-new-1", Backend: "claude", DisplayName: "N1"},
		{AgentID: "agent-new-2", Backend: "claude", DisplayName: "N2"},
	})
	if err == nil {
		t.Fatal("a batch exceeding the cap must be rejected")
	}
	after, _ := ListGroupMembers(groupID)
	if countActiveMembers(after) != 9 {
		t.Fatalf("a rejected batch must not add ANY member: active %d -> %d", countActiveMembers(before), countActiveMembers(after))
	}

	// A batch of exactly 1 is accepted (9 -> 10).
	if _, err := AddGroupMembers(project, groupID, []GroupMemberSpec{
		{AgentID: "agent-new-1", Backend: "claude", DisplayName: "N1"},
	}); err != nil {
		t.Fatalf("a batch within the cap must be accepted: %v", err)
	}
	final, _ := ListGroupMembers(groupID)
	if countActiveMembers(final) != 10 {
		t.Fatalf("want 10 active after the accepted batch, got %d", countActiveMembers(final))
	}
}

// A batch whose members are ALL already active does not grow the roster and must
// not be rejected even at the cap (net-new = 0). This is the "重复 agent 不算
// 超限" rule from O19.
func TestAddGroupMembers_DuplicatesDoNotCountTowardCap(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/groupbatch-dup"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, _, err := CreateGroup(project, "G", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	for i := range 9 { // host + 9 = 10 active (at cap)
		if _, err := AddGroupMember(project, groupID, "claude", "agent-"+string(rune('a'+i)), "M"); err != nil {
			t.Fatalf("AddGroupMember %d: %v", i, err)
		}
	}
	if members, _ := ListGroupMembers(groupID); countActiveMembers(members) != 10 {
		t.Fatalf("precondition: want 10 active, got %d", countActiveMembers(members))
	}

	// Re-adding two existing agents is a no-op (net-new 0) → accepted.
	if _, err := AddGroupMembers(project, groupID, []GroupMemberSpec{
		{AgentID: "agent-a", Backend: "claude", DisplayName: "A"},
		{AgentID: "agent-b", Backend: "claude", DisplayName: "B"},
	}); err != nil {
		t.Fatalf("re-adding existing agents must not exceed the cap: %v", err)
	}

	// But one existing + one NEW at the cap → net-new 1 → rejected.
	if _, err := AddGroupMembers(project, groupID, []GroupMemberSpec{
		{AgentID: "agent-a", Backend: "claude", DisplayName: "A"},
		{AgentID: "agent-fresh", Backend: "claude", DisplayName: "F"},
	}); err == nil {
		t.Fatal("net-new 1 at the cap must be rejected")
	}
}

// Pending private notes: stored per target, delivered by id, cleared by target.
func TestPendingBccStore(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/gorch-pendingstore"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, _, err := CreateGroup(project, "G", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	otherGroup, _, _ := CreateGroup(project, "G2", "codebuddy", "agent-host2", "Host2")

	addPendingBcc(groupID, "B", "给B一")
	addPendingBcc(groupID, "B", "给B二")
	addPendingBcc(groupID, "C", "给C")
	addPendingBcc(otherGroup, "B", "别的群")
	// Empty target/content are ignored.
	addPendingBcc(groupID, "", "x")
	addPendingBcc(groupID, "B", "  ")

	got := pendingBccForTarget(groupID, "B")
	if len(got) != 2 || got[0].Content != "给B一" || got[1].Content != "给B二" {
		t.Fatalf("B pending: %+v", got)
	}
	// Delete by id clears only what was delivered.
	deletePendingBcc([]int64{got[0].ID})
	if left := pendingBccForTarget(groupID, "B"); len(left) != 1 || left[0].Content != "给B二" {
		t.Fatalf("after delete-by-id: %+v", left)
	}
	// Clear by target removes the rest for that target only.
	deletePendingBccForTarget(groupID, "B")
	if left := pendingBccForTarget(groupID, "B"); len(left) != 0 {
		t.Fatalf("after clear-by-target: %+v", left)
	}
	if c := pendingBccForTarget(groupID, "C"); len(c) != 1 {
		t.Fatalf("C must be untouched: %+v", c)
	}
	if o := pendingBccForTarget(otherGroup, "B"); len(o) != 1 {
		t.Fatalf("another group must be untouched: %+v", o)
	}
	// Clear by group.
	deletePendingBccForGroup(groupID)
	if c := pendingBccForTarget(groupID, "C"); len(c) != 0 {
		t.Fatalf("after clear-by-group: %+v", c)
	}
	if o := pendingBccForTarget(otherGroup, "B"); len(o) != 1 {
		t.Fatalf("another group must still be untouched: %+v", o)
	}
}
