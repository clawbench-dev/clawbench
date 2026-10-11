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

// TestAddGroupMemberCreatesNewInstance covers the multi-instance contract:
// adding the same agent again creates a SECOND independent row (the same agent
// may appear several times in one group), and re-adding an agent that LEFT also
// creates a new row (there is no rejoin). Each row's display name comes from
// the caller; uniqueness across a batch is assigned by AddGroupMembers.
func TestAddGroupMemberCreatesNewInstance(t *testing.T) {
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

	// Re-adding the SAME agent creates a distinct row (a second instance).
	second, err := AddGroupMember(project, groupID, "claude", "agent-a", "A 2")
	if err != nil {
		t.Fatalf("AddGroupMember (second): %v", err)
	}
	if second == first {
		t.Fatalf("adding the same agent twice must create two rows, got the same id %q", second)
	}
	members, _ := ListGroupMembers(groupID)
	if got := countAgentRows(members, "agent-a"); got != 2 {
		t.Fatalf("want 2 rows for agent-a, got %d", got)
	}

	// Removing then re-adding creates ANOTHER new row (no rejoin): the removed
	// row stays archived, a fresh instance is added.
	if err := RemoveGroupMember(groupID, first); err != nil {
		t.Fatalf("RemoveGroupMember: %v", err)
	}
	rejoined, err := AddGroupMember(project, groupID, "claude", "agent-a", "A 3")
	if err != nil {
		t.Fatalf("AddGroupMember (after remove): %v", err)
	}
	if rejoined == first {
		t.Fatal("re-adding after removal must create a NEW row, not reactivate the old one")
	}
	members, _ = ListGroupMembers(groupID)
	if got := countAgentRows(members, "agent-a"); got != 3 {
		t.Fatalf("want 3 rows for agent-a (2 active + 1 archived), got %d", got)
	}
	// The first row must remain archived (its past speech stays attributed).
	for _, m := range members {
		if m.ID == first && !m.Left {
			t.Fatal("the removed row must stay archived (no rejoin)")
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

// TestCreateGroupWithMembers_AppliesAgentAutoApproveDefault pins WARN-102: when
// a group is created with members, each member's auto_approve flag is initialized
// from the agent's AutoApprove default (same as AddGroupMembers). Without this
// the create path skips applyAgentAutoApproveDefault while the add-member path
// applies it, so a newly created group member behaves differently from one added
// later despite having the same agent.
func TestCreateGroupWithMembers_AppliesAgentAutoApproveDefault(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/grouptest-autoapprove"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}

	// Override model.Agents so GetAgent returns known AutoApprove values.
	origAgents := model.Agents
	model.Agents = map[string]*model.Agent{
		"auto-yes": {ID: "auto-yes", Name: "AutoYes", AutoApprove: true},
		"auto-no":  {ID: "auto-no", Name: "AutoNo", AutoApprove: false},
	}
	defer func() { model.Agents = origAgents }()

	specs := []GroupMemberSpec{
		{AgentID: "auto-yes", Backend: "claude", DisplayName: "AutoYes"},
		{AgentID: "auto-no", Backend: "claude", DisplayName: "AutoNo"},
	}
	groupID, _, err := CreateGroupWithMembers(project, "AutoApprove Test", "auto-yes", specs)
	if err != nil {
		t.Fatalf("CreateGroupWithMembers: %v", err)
	}

	members, err := ListGroupMembers(groupID)
	if err != nil {
		t.Fatalf("ListGroupMembers: %v", err)
	}
	if len(members) != 2 {
		t.Fatalf("want 2 members, got %d", len(members))
	}

	for _, m := range members {
		got := GetSessionAutoApprove(m.ID)
		want := m.AgentID == "auto-yes"
		if got != want {
			t.Fatalf("member %s (agent %s) auto_approve=%v want %v", m.ID, m.AgentID, got, want)
		}
	}
}

// TestAddGroupMembers_AppliesAgentAutoApproveDefault pins the add-member path:
// each newly added member's auto_approve flag is initialized from the agent's
// AutoApprove default, same as the create path. This is the case Erin caught
// as a regression (the post-commit applyAgentAutoApproveDefault call had been
// incorrectly removed from AddGroupMembers, leaving the add path with no
// auto-approve initialization while the create path had it added).
func TestAddGroupMembers_AppliesAgentAutoApproveDefault(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/grouptest-add-autoapprove"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}

	origAgents := model.Agents
	model.Agents = map[string]*model.Agent{
		"auto-yes": {ID: "auto-yes", Name: "AutoYes", AutoApprove: true},
		"auto-no":  {ID: "auto-no", Name: "AutoNo", AutoApprove: false},
	}
	defer func() { model.Agents = origAgents }()

	groupID, _, err := CreateGroup(project, "G", "codebuddy", "codebuddy", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}

	specs := []GroupMemberSpec{
		{AgentID: "auto-yes", Backend: "claude", DisplayName: "AutoYes"},
		{AgentID: "auto-no", Backend: "claude", DisplayName: "AutoNo"},
	}
	addedIDs, err := AddGroupMembers(project, groupID, specs)
	if err != nil {
		t.Fatalf("AddGroupMembers: %v", err)
	}
	if len(addedIDs) != 2 {
		t.Fatalf("want 2 added members, got %d", len(addedIDs))
	}

	// Map agent id → member id for assertions.
	idByAgent := map[string]string{}
	members, err := ListGroupMembers(groupID)
	if err != nil {
		t.Fatalf("ListGroupMembers: %v", err)
	}
	for _, m := range members {
		idByAgent[m.AgentID] = m.ID
	}

	for _, agentID := range []string{"auto-yes", "auto-no"} {
		got := GetSessionAutoApprove(idByAgent[agentID])
		want := agentID == "auto-yes"
		if got != want {
			t.Fatalf("member %s auto_approve=%v want %v", agentID, got, want)
		}
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

// The member-speech cap is a GLOBAL setting (chat.group_max_speeches), not a
// per-group one. It reads model.ChatGroupMaxSpeeches and falls back to the
// default when that is non-positive (a value set directly on the global var in
// an edge/test path must not silently mean "stop immediately").
func TestGroupMaxSpeechesReadsGlobal(t *testing.T) {
	orig := model.ChatGroupMaxSpeeches
	t.Cleanup(func() { model.ChatGroupMaxSpeeches = orig })

	model.ChatGroupMaxSpeeches = 42
	if got := GetGroupMaxSpeeches(); got != 42 {
		t.Fatalf("GetGroupMaxSpeeches()=%d want 42", got)
	}

	// A non-positive global falls back to the default (never 0 / negative).
	model.ChatGroupMaxSpeeches = 0
	if got := GetGroupMaxSpeeches(); got != defaultGroupMaxSpeeches {
		t.Fatalf("GetGroupMaxSpeeches()=%d want default %d", got, defaultGroupMaxSpeeches)
	}
	model.ChatGroupMaxSpeeches = -5
	if got := GetGroupMaxSpeeches(); got != defaultGroupMaxSpeeches {
		t.Fatalf("GetGroupMaxSpeeches()=%d want default %d", got, defaultGroupMaxSpeeches)
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

// The cap counts INSTANCES: filling the roster to the cap and then adding any
// further instance (even of an agent already present) must be refused. There is
// no rejoin, so a removed member frees a slot for a NEW instance only.
func TestAddGroupMember_CapCountsInstances(t *testing.T) {
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
	// At the cap, another instance of an EXISTING agent is refused (it is a new
	// member, so it would exceed the cap).
	if _, err := AddGroupMember(project, groupID, "claude", "agent-0", "M0 2"); err == nil {
		t.Fatal("adding another instance at the cap must be refused")
	}
	// A member leaves: a slot frees up and a NEW instance fits.
	leaver := mustMemberID(t, groupID, "agent-5")
	if err := RemoveGroupMember(groupID, leaver); err != nil {
		t.Fatalf("RemoveGroupMember: %v", err)
	}
	if _, err := AddGroupMember(project, groupID, "claude", "agent-new", "New"); err != nil {
		t.Fatalf("filling the freed slot: %v", err)
	}
	// Back at the cap, one more is refused again.
	if _, err := AddGroupMember(project, groupID, "claude", "agent-0", "M0 2"); err == nil {
		t.Fatal("adding at the cap must be refused (it would exceed the cap)")
	}
}

func mustMemberID(t *testing.T, groupID, agentID string) string {
	t.Helper()
	members, err := ListGroupMembers(groupID)
	if err != nil {
		t.Fatalf("ListGroupMembers: %v", err)
	}
	for _, m := range members {
		if m.AgentID == agentID {
			return m.ID
		}
	}
	t.Fatalf("member for agent %q not found", agentID)
	return ""
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

// CreateGroupWithMembers must KEEP duplicate agents: the same agent may be
// added several times, each an independent member (a separate row, a separate
// connection) with its OWN unique display name. The first instance keeps the
// bare base name; duplicates draw distinct pool names, so name-based routing
// stays unambiguous ("A", "Alice", "Bob").
func TestCreateGroupWithMembers_KeepsDuplicateAgents(t *testing.T) {
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
	// Every duplicate is kept: 1 host + 3 instances of agent-a = 4 rows.
	if len(members) != 4 {
		t.Fatalf("want 4 member rows (host + 3x A), got %d", len(members))
	}
	if got := countAgentRows(members, "agent-a"); got != 3 {
		t.Fatalf("duplicate agent ids must each get their own row, got %d", got)
	}
	// Names must be unique. agent-a is MULTI-instance, so NONE keeps the bare
	// "A" — all three draw pool names ("A 1" is never produced).
	names := map[string]bool{}
	for _, m := range members {
		if m.AgentID != "agent-a" {
			continue
		}
		if names[m.Name] {
			t.Fatalf("duplicate member name %q", m.Name)
		}
		names[m.Name] = true
	}
	for _, want := range []string{groupNamePool[0], groupNamePool[1], groupNamePool[2]} {
		if !names[want] {
			t.Fatalf("missing expected instance name %q (got %v)", want, names)
		}
	}
	if names["A"] || names["A 1"] {
		t.Fatalf(`multi-instance agents must not keep the bare name or "A 1" (got %v)`, names)
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
	// Fill to (cap - 1) active: host + (cap-2) members.
	for i := range maxGroupMembers - 2 {
		if _, err := AddGroupMember(project, groupID, "claude", "agent-"+string(rune('a'+i)), "M"); err != nil {
			t.Fatalf("AddGroupMember %d: %v", i, err)
		}
	}
	before, _ := ListGroupMembers(groupID)
	if countActiveMembers(before) != maxGroupMembers-1 {
		t.Fatalf("precondition: want %d active, got %d", maxGroupMembers-1, countActiveMembers(before))
	}

	// Batch of 2 new instances would exceed the cap → must reject the WHOLE batch.
	_, err = AddGroupMembers(project, groupID, []GroupMemberSpec{
		{AgentID: "agent-new-1", Backend: "claude", DisplayName: "N1"},
		{AgentID: "agent-new-2", Backend: "claude", DisplayName: "N2"},
	})
	if err == nil {
		t.Fatal("a batch exceeding the cap must be rejected")
	}
	after, _ := ListGroupMembers(groupID)
	if countActiveMembers(after) != maxGroupMembers-1 {
		t.Fatalf("a rejected batch must not add ANY member: active %d -> %d", countActiveMembers(before), countActiveMembers(after))
	}

	// A batch of exactly 1 is accepted (cap-1 -> cap).
	if _, err := AddGroupMembers(project, groupID, []GroupMemberSpec{
		{AgentID: "agent-new-1", Backend: "claude", DisplayName: "N1"},
	}); err != nil {
		t.Fatalf("a batch within the cap must be accepted: %v", err)
	}
	final, _ := ListGroupMembers(groupID)
	if countActiveMembers(final) != maxGroupMembers {
		t.Fatalf("want %d active after the accepted batch, got %d", maxGroupMembers, countActiveMembers(final))
	}
}

// Every spec in a batch is a NEW instance, so duplicates of an already-present
// agent DO consume slots (the cap counts instances). A batch that would push the
// active count over the cap is rejected as a whole.
func TestAddGroupMembers_EverySpecConsumesSlot(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/groupbatch-dup"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, _, err := CreateGroup(project, "G", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	for i := range maxGroupMembers - 1 { // host + (cap-1) = at cap
		if _, err := AddGroupMember(project, groupID, "claude", "agent-"+string(rune('a'+i)), "M"); err != nil {
			t.Fatalf("AddGroupMember %d: %v", i, err)
		}
	}
	if members, _ := ListGroupMembers(groupID); countActiveMembers(members) != maxGroupMembers {
		t.Fatalf("precondition: want %d active, got %d", maxGroupMembers, countActiveMembers(members))
	}

	// Adding another instance of an ALREADY-present agent is a new member and
	// must be rejected at the cap.
	if _, err := AddGroupMembers(project, groupID, []GroupMemberSpec{
		{AgentID: "agent-a", Backend: "claude", DisplayName: "A"},
	}); err == nil {
		t.Fatal("adding an instance at the cap must be rejected")
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

// groupUserTarget() is config-driven: it honors chat.user_nickname and falls
// back to the language-neutral default when unset.
func TestGroupUserTarget_ConfigDriven(t *testing.T) {
	orig := model.ChatUserNickname
	t.Cleanup(func() { model.ChatUserNickname = orig })

	model.ChatUserNickname = ""
	if got := groupUserTarget(); got != "User" {
		t.Fatalf("empty config must fall back to the default, got %q", got)
	}
	model.ChatUserNickname = "老板"
	if got := groupUserTarget(); got != "老板" {
		t.Fatalf("configured nickname must win, got %q", got)
	}
	// Defensive trim: a hand-edited config.yaml bypasses the PATCH
	// normalization, but the parser trims targets before lookup — an untrimmed
	// value here would make the user unreachable.
	model.ChatUserNickname = "  老板  "
	if got := groupUserTarget(); got != "老板" {
		t.Fatalf("surrounding whitespace must be trimmed, got %q", got)
	}
	model.ChatUserNickname = "   "
	if got := groupUserTarget(); got != "User" {
		t.Fatalf("all-whitespace must fall back to the default, got %q", got)
	}
}

// A nickname change migrates pending notes keyed by the old user name so an
// undelivered note is not orphaned.
func TestRenameUserPendingBcc(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/gorch-renamebcc"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, _, err := CreateGroup(project, "G", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	addPendingBcc(groupID, "User", "给你的")
	addPendingBcc(groupID, "A", "给A的")

	RenameUserPendingBcc("User", "老板")

	if left := pendingBccForTarget(groupID, "User"); len(left) != 0 {
		t.Fatalf("the old-name rows must be migrated away: %+v", left)
	}
	migrated := pendingBccForTarget(groupID, "老板")
	if len(migrated) != 1 || migrated[0].Content != "给你的" {
		t.Fatalf("the note must move to the new name: %+v", migrated)
	}
	if a := pendingBccForTarget(groupID, "A"); len(a) != 1 {
		t.Fatalf("other targets must be untouched: %+v", a)
	}
	// A no-op rename (same name / empty) must not error or move anything.
	RenameUserPendingBcc("老板", "老板")
	RenameUserPendingBcc("", "x")
	if m := pendingBccForTarget(groupID, "老板"); len(m) != 1 {
		t.Fatalf("no-op renames must leave the rows in place: %+v", m)
	}
}

// GroupMemberSpeakersForGroups resolves a message's chat_history.agent_id back
// to the member's display name. Unlike GroupMembersForGroups it must INCLUDE
// left (archived) members — a departed member's past speech still has to render
// as their own name rather than a generic "Assistant". It is also the
// speaker-resolution companion used by the share/RAG paths, so the empty-input,
// dedup and multi-group paths matter.
func TestGroupMemberSpeakersForGroups(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/gorch-speakers"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}

	groupA, hostA, err := CreateGroup(project, "A 组", "codebuddy", "agent-host-a", "HostA")
	if err != nil {
		t.Fatalf("CreateGroup A: %v", err)
	}
	mA1, _ := AddGroupMember(project, groupA, "claude", "agent-a1", "A1")
	mA2, _ := AddGroupMember(project, groupA, "claude", "agent-a2", "A2")

	groupB, _, err := CreateGroup(project, "B 组", "codebuddy", "agent-host-b", "HostB")
	if err != nil {
		t.Fatalf("CreateGroup B: %v", err)
	}
	mB1, _ := AddGroupMember(project, groupB, "claude", "agent-b1", "B1")

	// Empty input must short-circuit without touching the DB.
	empty, err := GroupMemberSpeakersForGroups(nil)
	if err != nil {
		t.Fatalf("empty input must not error: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("empty input must yield an empty map, got %+v", empty)
	}
	// All-empty ids collapse to no ids and still return an empty map.
	if got, err := GroupMemberSpeakersForGroups([]string{"", ""}); err != nil || len(got) != 0 {
		t.Fatalf("all-empty ids must yield an empty map without error, got %+v err=%v", got, err)
	}

	// Duplicate ids must be de-duplicated (the query would otherwise build a
	// redundant IN list, and the result must still be correct).
	got, err := GroupMemberSpeakersForGroups([]string{groupA, groupA, "", groupB})
	if err != nil {
		t.Fatalf("GroupMemberSpeakersForGroups: %v", err)
	}
	if got[groupA][hostA] != "HostA" || got[groupA][mA1] != "A1" || got[groupA][mA2] != "A2" {
		t.Fatalf("group A speakers wrong: %+v", got[groupA])
	}
	if got[groupB][mB1] != "B1" {
		t.Fatalf("group B speakers wrong: %+v", got[groupB])
	}
	if len(got[groupA]) != 3 {
		t.Fatalf("group A must have exactly 3 members, got %d (%+v)", len(got[groupA]), got[groupA])
	}

	// A member who LEFT must still resolve (include-left rule): their earlier
	// messages keep their name instead of degrading to a generic assistant.
	if err := RemoveGroupMember(groupA, mA2); err != nil {
		t.Fatalf("RemoveGroupMember: %v", err)
	}
	got, err = GroupMemberSpeakersForGroups([]string{groupA})
	if err != nil {
		t.Fatalf("GroupMemberSpeakersForGroups after removal: %v", err)
	}
	if got[groupA][mA2] != "A2" {
		t.Fatalf("a left member must still resolve its name, got %+v", got[groupA])
	}

	// An unknown group id is simply absent from the map (no error).
	got, err = GroupMemberSpeakersForGroups([]string{"does-not-exist"})
	if err != nil {
		t.Fatalf("unknown group must not error: %v", err)
	}
	if _, ok := got["does-not-exist"]; ok {
		t.Fatalf("unknown group must be absent from the map, got %+v", got)
	}
}

// ListGroupMembers 读回成员自身的头像列；GroupMembersForGroups 预览也带头像。
func TestListGroupMembers_ReturnsAvatar(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/group-avatar-list"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, hostID, err := CreateGroup(project, "G", "codebuddy", "codebuddy", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	const svg = "<svg xmlns=\"http://www.w3.org/2000/svg\"></svg>"
	if _, err := store.WriteExec("UPDATE chat_sessions SET agent_avatar = ? WHERE id = ?", svg, hostID); err != nil {
		t.Fatalf("set avatar: %v", err)
	}
	members, err := ListGroupMembers(groupID)
	if err != nil {
		t.Fatalf("ListGroupMembers: %v", err)
	}
	if len(members) == 0 {
		t.Fatal("ListGroupMembers returned no members")
	}
	if members[0].Avatar != svg {
		t.Fatalf("member avatar = %q, want %q", members[0].Avatar, svg)
	}
	previews, err := GroupMembersForGroups([]string{groupID})
	if err != nil {
		t.Fatalf("GroupMembersForGroups: %v", err)
	}
	if len(previews[groupID]) == 0 {
		t.Fatal("GroupMembersForGroups returned no previews")
	}
	if previews[groupID][0].Avatar != svg {
		t.Fatalf("preview avatar = %q, want %q", previews[groupID][0].Avatar, svg)
	}
}
