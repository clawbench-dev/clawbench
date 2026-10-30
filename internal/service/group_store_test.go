package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	// I-1: ListGroupMembers includes disabled members, so after disabling the total
	// stays 2 while the speaking count drops to 1.
	members, err := ListGroupMembers(groupID)
	if err != nil {
		t.Fatalf("ListGroupMembers: %v", err)
	}
	if len(members) != 2 {
		t.Fatalf("want 2 members, got %d", len(members))
	}
	if countSpeakingMembers(members) != 2 {
		t.Fatalf("want 2 speaking members, got %d", countSpeakingMembers(members))
	}

	if err := SetGroupMemberDisabled(groupID, m, true); err != nil {
		t.Fatalf("SetGroupMemberDisabled: %v", err)
	}
	members, _ = ListGroupMembers(groupID)
	if len(members) != 2 {
		t.Fatalf("disabling keeps the row (已禁用) -> still 2, got %d", len(members))
	}
	if active := countSpeakingMembers(members); active != 1 {
		t.Fatalf("want 1 speaking member, got %d", active)
	}
	// Re-enabling restores it — the whole point of the flag (issue #597): the row,
	// its id and its context never went away, so nothing new is created.
	if err := SetGroupMemberDisabled(groupID, m, false); err != nil {
		t.Fatalf("SetGroupMemberDisabled(false): %v", err)
	}
	members, _ = ListGroupMembers(groupID)
	if len(members) != 2 || countSpeakingMembers(members) != 2 {
		t.Fatalf("re-enabling must restore the same row: %+v", members)
	}
	for _, got := range members {
		if got.ID == m && got.Disabled {
			t.Fatal("enabled member must not stay disabled")
		}
	}
}

// TestAddGroupMemberCreatesNewInstance covers the multi-instance contract:
// adding the same agent again creates a SECOND independent row (the same agent
// may appear several times in one group), and adding an agent whose OTHER
// instance is disabled still creates a new row (adding never reuses a row — a
// disabled member is revived with SetGroupMemberDisabled instead). Each row's display
// name comes from the caller; uniqueness across a batch is assigned by
// AddGroupMembers.
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

	// Disabling then adding creates ANOTHER new row: "add" always means a new
	// instance, and the disabled row keeps its own history and cursor.
	if err := SetGroupMemberDisabled(groupID, first, true); err != nil {
		t.Fatalf("SetGroupMemberDisabled: %v", err)
	}
	added, err := AddGroupMember(project, groupID, "claude", "agent-a", "A 3")
	if err != nil {
		t.Fatalf("AddGroupMember (after disable): %v", err)
	}
	if added == first {
		t.Fatal("adding a member must create a NEW row, not revive a disabled one")
	}
	members, _ = ListGroupMembers(groupID)
	if got := countAgentRows(members, "agent-a"); got != 3 {
		t.Fatalf("want 3 rows for agent-a (2 speaking + 1 disabled), got %d", got)
	}
	// The first row must stay disabled (its past speech stays attributed, and only
	// an explicit enable brings it back).
	for _, m := range members {
		if m.ID == first && !m.Disabled {
			t.Fatal("the disabled row must stay disabled (adding does not revive it)")
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
	if got := countSpeakingMembers(members); got != 3 {
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

// The auto-cruise switch is a tri-state read: absent => off, and an explicit
// true/false must round-trip. Cruise is opt-in, so an old group without the key
// must stay non-cruising.
func TestGroupCruiseRoundTrip(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/grouptest-cruise"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, _, err := CreateGroup(project, "g", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if GetSessionCruise(groupID) {
		t.Fatal("cruise must default to false when unset")
	}
	if err := SetSessionCruise(groupID, true); err != nil {
		t.Fatalf("SetSessionCruise(true): %v", err)
	}
	if !GetSessionCruise(groupID) {
		t.Fatal("cruise must read back true after being set")
	}
	// The two switches must not clobber each other (they share context_state).
	if err := SetGroupParallelDefault(groupID, true); err != nil {
		t.Fatalf("SetGroupParallelDefault(true): %v", err)
	}
	if !GetSessionCruise(groupID) {
		t.Fatal("setting parallelDefault must not clear cruise")
	}
	if err := SetSessionCruise(groupID, false); err != nil {
		t.Fatalf("SetSessionCruise(false): %v", err)
	}
	if GetSessionCruise(groupID) {
		t.Fatal("cruise must read back false after being cleared")
	}
	if !GetGroupParallelDefault(groupID) {
		t.Fatal("clearing cruise must not clear parallelDefault")
	}
}

// TestGroupMembersForGroups pins the batch preview used by the session list:
// one call covers many groups, disabled members are excluded, group ids with no
// speaking members are absent, and an empty input never touches the DB.
func TestGroupMembersForGroups(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/grouptest"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}

	// Group A: host + two members; one of the two is then disabled.
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
	if err := SetGroupMemberDisabled(gA, leftMember, true); err != nil {
		t.Fatalf("SetGroupMemberDisabled: %v", err)
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

	// A: host + B survive; the disabled member is excluded.
	aMembers := got[gA]
	if len(aMembers) != 2 {
		t.Fatalf("group A preview: want 2 speaking members, got %d", len(aMembers))
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
		t.Fatal("disabled member must not appear in the list preview")
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

// Disabling a member refreshes updated_at, so a long-disabled member is not mistaken
// for a retention-expired session (decision #48).
func TestSetGroupMemberDisabled_RefreshesUpdatedAt(t *testing.T) {
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

	// Age the member row, then disable it — the toggle must refresh updated_at.
	if _, err := store.UnsafeDBForTest().Exec(
		"UPDATE chat_sessions SET updated_at = datetime('now', '-100 days') WHERE id = ?", memberID,
	); err != nil {
		t.Fatalf("age member row: %v", err)
	}
	if err := SetGroupMemberDisabled(groupID, memberID, true); err != nil {
		t.Fatalf("SetGroupMemberDisabled: %v", err)
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
		t.Fatalf("SetGroupMemberDisabled must refresh updated_at, but the row is still >90 days old")
	}

	// Belt-and-braces: the retention sweep must not pick the member up either.
	cutoff := time.Now().AddDate(0, 0, -90)
	ids, err := store.GetExpiredArchivedSessions(cutoff)
	if err != nil {
		t.Fatalf("GetExpiredArchivedSessions: %v", err)
	}
	for _, id := range ids {
		if id == memberID {
			t.Fatalf("a disabled member must not be treated as retention-expired: %v", ids)
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
	// Simulate legacy data from before disabling existed: an ARCHIVED member row
	// with an old timestamp. The retention sweep must still skip it, because its
	// past speech is attributed by its row id.
	if _, err := store.UnsafeDBForTest().Exec(
		"UPDATE chat_sessions SET archived = 1, updated_at = datetime('now', '-100 days') WHERE id = ?", memberID,
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

// The host controls the flow, so disabling it would leave the group unroutable.
// Refuse the disable rather than silently orphaning the group (decision: N5).
// The LAST speaking member cannot be disabled: with nobody on the roster the
// group can never run another turn, which is the same dead end the old
// "remove the last member" had. Re-asserting an ALREADY disabled member is still
// a no-op success (there is nothing left to lose).
func TestSetGroupMemberDisabled_RefusesLastSpeaker(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/grouptest-last"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	// A FREE group (no host), so the host guard cannot mask the last-member guard.
	groupID, byName := createFreeGroup(t, project, "A", "B")

	if err := SetGroupMemberDisabled(groupID, byName["B"], true); err != nil {
		t.Fatalf("disabling one of two members must work: %v", err)
	}
	if err := SetGroupMemberDisabled(groupID, byName["A"], true); !errors.Is(err, ErrCannotDisableLastMember) {
		t.Fatalf("disabling the last speaker must be refused with ErrCannotDisableLastMember, got %v", err)
	}
	// The refusal must not have half-applied.
	for _, m := range mustListMembers(t, groupID) {
		if m.ID == byName["A"] && m.Disabled {
			t.Fatal("a refused disable must leave the last speaker enabled")
		}
	}
	// Re-asserting an already disabled member stays a no-op success.
	if err := SetGroupMemberDisabled(groupID, byName["B"], true); err != nil {
		t.Fatalf("re-disabling an already disabled member must be a no-op success: %v", err)
	}
	// Enabling it back gives the group two speakers again, so A may be disabled.
	if err := SetGroupMemberDisabled(groupID, byName["B"], false); err != nil {
		t.Fatalf("re-enabling must work: %v", err)
	}
	if err := SetGroupMemberDisabled(groupID, byName["A"], true); err != nil {
		t.Fatalf("disabling A with two speakers must work: %v", err)
	}
}

// A member id belonging to a DIFFERENT group resolves to no row: the update is a
// documented no-op, not a "last speaker" error.
func TestSetGroupMemberDisabled_ForeignMemberIsNoOp(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/grouptest-foreign"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupA, byNameA := createFreeGroup(t, project, "A", "B")
	_ = groupA
	groupB, _ := createFreeGroup(t, "/tmp/grouptest-foreign-b", "C", "D")

	if err := SetGroupMemberDisabled(groupB, byNameA["A"], true); err != nil {
		t.Fatalf("a member id from another group must be a no-op success, got %v", err)
	}
	for _, m := range mustListMembers(t, groupA) {
		if m.Disabled {
			t.Fatalf("a foreign disable must not touch any row, got %+v", m)
		}
	}
}

func TestSetGroupMemberDisabled_RefusesHost(t *testing.T) {
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

	// Disabling the host must fail...
	if err := SetGroupMemberDisabled(groupID, hostMemberID, true); !errors.Is(err, ErrCannotDisableHost) {
		t.Fatalf("disabling the host must be refused with ErrCannotDisableHost, got %v", err)
	}
	// ...and must NOT have disabled the host row.
	for _, m := range mustListMembers(t, groupID) {
		if m.ID == hostMemberID && m.Disabled {
			t.Fatal("host row must stay enabled after a refused disable")
		}
	}

	// Disabling a normal member still works (the guard must not over-reach).
	if err := SetGroupMemberDisabled(groupID, other, true); err != nil {
		t.Fatalf("disabling a non-host member must still work: %v", err)
	}
}

// The host guard is evaluated on every call, not just the first: a repeated disable
// of the host must keep being refused (never a silent success).
func TestSetGroupMemberDisabled_HostGuardIsStable(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/grouptest"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, hostMemberID, err := CreateGroup(project, "讨论组", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if err := SetGroupMemberDisabled(groupID, hostMemberID, true); !errors.Is(err, ErrCannotDisableHost) {
		t.Fatalf("first disable of the host: err=%v want ErrCannotDisableHost", err)
	}
	if err := SetGroupMemberDisabled(groupID, hostMemberID, true); !errors.Is(err, ErrCannotDisableHost) {
		t.Fatalf("second disable of the host must also be refused, got %v", err)
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
// further instance (even of an agent already present) must be refused. Adding is
// always a NEW instance — reviving a disabled member is SetGroupMemberDisabled, which
// does not go through this path — so a disabled member frees a slot for a new one.
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
	// A member is disabled: a slot frees up (it spawns nothing while silent) and a
	// NEW instance fits.
	leaver := mustMemberID(t, groupID, "agent-5")
	if err := SetGroupMemberDisabled(groupID, leaver, true); err != nil {
		t.Fatalf("SetGroupMemberDisabled: %v", err)
	}
	if _, err := AddGroupMember(project, groupID, "claude", "agent-new", "New"); err != nil {
		t.Fatalf("filling the freed slot: %v", err)
	}
	// Back at the cap, one more is refused again.
	if _, err := AddGroupMember(project, groupID, "claude", "agent-0", "M0 2"); err == nil {
		t.Fatal("adding at the cap must be refused (it would exceed the cap)")
	}
}

// Re-enabling is cap-checked: disabled members do not occupy a slot, so without the
// check a user could park disabled agents, fill every freed slot with new members,
// and then enable everything — putting more than MaxGroupMembers members on the
// mic in one turn (every member is its own ACP subprocess, which is exactly what
// the cap bounds).
func TestSetGroupMemberDisabled_EnableRespectsCap(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/grouptest-enable-cap"
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

	// Disable one member: it stops occupying a slot, so a NEW instance fits.
	disabledID := mustMemberID(t, groupID, "agent-3")
	if err := SetGroupMemberDisabled(groupID, disabledID, true); err != nil {
		t.Fatalf("SetGroupMemberDisabled: %v", err)
	}
	if _, err := AddGroupMember(project, groupID, "claude", "agent-new", "New"); err != nil {
		t.Fatalf("filling the freed slot: %v", err)
	}

	// The roster is at the cap again, so re-enabling the parked member would put
	// one member too many scheduled to speak.
	if err := SetGroupMemberDisabled(groupID, disabledID, false); !errors.Is(err, ErrGroupMemberLimit) {
		t.Fatalf("re-enabling past the cap must be refused with ErrGroupMemberLimit, got %v", err)
	}
	// ...and it must still be disabled (the refusal must not half-apply).
	for _, m := range mustListMembers(t, groupID) {
		if m.ID == disabledID && !m.Disabled {
			t.Fatal("a refused enable must leave the member disabled")
		}
	}

	// Disabling it again is an idempotent no-op success (already disabled).
	if err := SetGroupMemberDisabled(groupID, disabledID, true); err != nil {
		t.Fatalf("re-disabling must stay a no-op success: %v", err)
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

// Membership system rows are localized at WRITE time from model.Language (they
// are persisted to chat_history AND injected into every member's context as
// "[系统] <text>", so they must be readable sentences, never raw keys).
func TestGroupMemberSystemEvents_LocalizedAtWriteTime(t *testing.T) {
	origLang := model.Language
	defer func() { model.Language = origLang }()

	for _, tc := range []struct {
		lang     string
		joined   string
		disabled string
		enabled  string
	}{
		{lang: "zh", joined: "加入了讨论", disabled: "已禁用", enabled: "已启用"},
		{lang: "en", joined: "joined the discussion", disabled: "was disabled", enabled: "was enabled"},
	} {
		model.Language = tc.lang
		setupGroupDB(t)
		project := "/tmp/grouptest-lang-" + tc.lang
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
		if err := SetGroupMemberDisabled(groupID, memberID, true); err != nil {
			t.Fatalf("SetGroupMemberDisabled(true): %v", err)
		}
		if err := SetGroupMemberDisabled(groupID, memberID, false); err != nil {
			t.Fatalf("SetGroupMemberDisabled(false): %v", err)
		}

		joined := false
		disabled := false
		enabled := false
		for _, m := range systemMessages(t, groupID) {
			// A raw key would mean the row was stored untranslated.
			if strings.Contains(m.Content, "GroupMemberJoined") || strings.Contains(m.Content, "GroupMemberDisabled") || strings.Contains(m.Content, "GroupMemberEnabled") {
				t.Fatalf("%s: system row leaked a raw i18n key: %q", tc.lang, m.Content)
			}
			if strings.Contains(m.Content, tc.joined) {
				joined = true
			}
			if strings.Contains(m.Content, tc.disabled) {
				disabled = true
			}
			if strings.Contains(m.Content, tc.enabled) {
				enabled = true
			}
		}
		if !joined {
			t.Errorf("%s: no joined event matching %q", tc.lang, tc.joined)
		}
		if !disabled {
			t.Errorf("%s: no disable event matching %q", tc.lang, tc.disabled)
		}
		if !enabled {
			t.Errorf("%s: no enable event matching %q", tc.lang, tc.enabled)
		}
	}
}

// Disabling a member writes a disable system event.
func TestSetGroupMemberDisabled_WritesSystemEvent(t *testing.T) {
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

	if err := SetGroupMemberDisabled(groupID, memberID, true); err != nil {
		t.Fatalf("SetGroupMemberDisabled: %v", err)
	}
	sysMsgs := systemMessages(t, groupID)
	if len(sysMsgs) != before+1 {
		t.Fatalf("expected one more system event after disabling: before=%d after=%d", before, len(sysMsgs))
	}
	last := sysMsgs[len(sysMsgs)-1].Content
	if !strings.Contains(last, "A") || !strings.Contains(last, "已禁用") {
		t.Fatalf("disable event must name the member and say they were disabled: %q", last)
	}

	// Re-enabling writes the counterpart line, so the timeline explains the return.
	if err := SetGroupMemberDisabled(groupID, memberID, false); err != nil {
		t.Fatalf("SetGroupMemberDisabled(false): %v", err)
	}
	sysMsgs = systemMessages(t, groupID)
	last = sysMsgs[len(sysMsgs)-1].Content
	if !strings.Contains(last, "A") || !strings.Contains(last, "已启用") {
		t.Fatalf("enable event must name the member and say they were enabled: %q", last)
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

// Disabling a member broadcasts the disable system_message event too.
func TestSetGroupMemberDisabled_BroadcastsSystemEvent(t *testing.T) {
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

	if err := SetGroupMemberDisabled(groupID, memberID, true); err != nil {
		t.Fatalf("SetGroupMemberDisabled: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected exactly one disable broadcast, got %d: %+v", len(got), got)
	}
	if !strings.Contains(got[0].text, "A") || !strings.Contains(got[0].text, "已禁用") {
		t.Fatalf("disable broadcast must name the member and say they were disabled: %q", got[0].text)
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

// ── 更换主持人 (issue #608) ──────────────────────────────────────────────

// Changing the host must move ALL THREE things together: the pointer the
// orchestrator reads, the group row's mirrored identity (backend/agent_id, which
// the session list falls back to), and the timeline line every member sees.
// Testing only the pointer would pass while the roster icon still named the old
// host — that drift is exactly what the mirrored columns exist to prevent.
func TestChangeGroupHost_SwapsPointerIdentityAndTimeline(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/grouptest"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, oldHostID, err := CreateGroup(project, "讨论组", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	memberID, err := AddGroupMember(project, groupID, "claude", "agent-a", "A")
	if err != nil {
		t.Fatalf("AddGroupMember: %v", err)
	}
	before := len(systemMessages(t, groupID))

	if err := ChangeGroupHost(project, groupID, memberID); err != nil {
		t.Fatalf("ChangeGroupHost: %v", err)
	}
	if got := GetGroupHostMember(groupID); got != memberID {
		t.Fatalf("host pointer = %q, want %q", got, memberID)
	}
	if oldHostID == memberID {
		t.Fatal("fixture broken: old host and new host are the same member")
	}

	// The group row mirrors the host, so the swap must carry the identity over.
	var backend, agentID string
	if err := store.ReadDB().QueryRow(
		"SELECT backend, agent_id FROM chat_sessions WHERE id = ?", groupID,
	).Scan(&backend, &agentID); err != nil {
		t.Fatalf("read group row: %v", err)
	}
	if backend != "claude" || agentID != "agent-a" {
		t.Fatalf("group row identity = (%q,%q), want (claude,agent-a)", backend, agentID)
	}

	sysMsgs := systemMessages(t, groupID)
	if len(sysMsgs) != before+1 {
		t.Fatalf("expected one host-change system row: before=%d after=%d", before, len(sysMsgs))
	}
	last := sysMsgs[len(sysMsgs)-1].Content
	if !strings.Contains(last, "A") || !strings.Contains(last, "主持人") {
		t.Fatalf("host-change event must name the new host: %q", last)
	}
}

// Naming the CURRENT host is a no-op: no write, and crucially no second
// timeline line (every line counts toward each member's cursor and is injected
// into its next turn).
func TestChangeGroupHost_IsIdempotent(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/grouptest"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, hostID, err := CreateGroup(project, "讨论组", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if _, err := AddGroupMember(project, groupID, "claude", "agent-a", "A"); err != nil {
		t.Fatalf("AddGroupMember: %v", err)
	}
	before := len(systemMessages(t, groupID))

	if err := ChangeGroupHost(project, groupID, hostID); err != nil {
		t.Fatalf("ChangeGroupHost(current host): %v", err)
	}
	if after := len(systemMessages(t, groupID)); after != before {
		t.Fatalf("re-asserting the current host wrote %d extra system rows", after-before)
	}
}

// A free group has no host; promoting one would silently change its routing
// model (and its 密送 rules), so the call is refused rather than guessed.
func TestChangeGroupHost_RefusesFreeGroup(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/grouptest"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, _, err := CreateGroupWithMembers(project, "自由群", "", []GroupMemberSpec{
		{AgentID: "agent-a", Backend: "claude"},
		{AgentID: "agent-b", Backend: "claude"},
	})
	if err != nil {
		t.Fatalf("CreateGroupWithMembers: %v", err)
	}
	members, err := ListGroupMembers(groupID)
	if err != nil || len(members) != 2 {
		t.Fatalf("ListGroupMembers: %v (%d)", err, len(members))
	}
	if err := ChangeGroupHost(project, groupID, members[0].ID); !errors.Is(err, ErrGroupNotHostMode) {
		t.Fatalf("ChangeGroupHost on free group = %v, want ErrGroupNotHostMode", err)
	}
	if got := GetGroupHostMember(groupID); got != "" {
		t.Fatalf("free group must still have no host, got %q", got)
	}
}

// A member of ANOTHER group (or an unknown id) cannot be promoted: the pointer
// would name a row the orchestrator's roster does not contain.
func TestChangeGroupHost_RefusesForeignMember(t *testing.T) {
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
	otherGroup, _, err := CreateGroup(project, "另一个群", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup(other): %v", err)
	}
	foreignID, err := AddGroupMember(project, otherGroup, "claude", "agent-b", "B")
	if err != nil {
		t.Fatalf("AddGroupMember(other): %v", err)
	}

	for _, id := range []string{foreignID, "no-such-member"} {
		if err := ChangeGroupHost(project, groupID, id); !errors.Is(err, ErrHostNotMember) {
			t.Fatalf("ChangeGroupHost(%q) = %v, want ErrHostNotMember", id, err)
		}
	}
}

// A disabled member is off the schedule, so it must not become the router.
func TestChangeGroupHost_RefusesDisabledMember(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/grouptest"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, hostID, err := CreateGroup(project, "讨论组", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	memberID, err := AddGroupMember(project, groupID, "claude", "agent-a", "A")
	if err != nil {
		t.Fatalf("AddGroupMember: %v", err)
	}
	if err := SetGroupMemberDisabled(groupID, memberID, true); err != nil {
		t.Fatalf("SetGroupMemberDisabled: %v", err)
	}

	if err := ChangeGroupHost(project, groupID, memberID); !errors.Is(err, ErrHostDisabled) {
		t.Fatalf("ChangeGroupHost(disabled) = %v, want ErrHostDisabled", err)
	}
	if got := GetGroupHostMember(groupID); got != hostID {
		t.Fatalf("host must not change on a refused call: got %q want %q", got, hostID)
	}
}

// The disable guard reads the pointer LIVE, so after a swap the OLD host becomes
// an ordinary member (disable-able) and the NEW one inherits the guard. Pinning
// this catches a guard that cached the host at creation.
func TestChangeGroupHost_MovesTheDisableGuard(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/grouptest"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, oldHostID, err := CreateGroup(project, "讨论组", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	memberID, err := AddGroupMember(project, groupID, "claude", "agent-a", "A")
	if err != nil {
		t.Fatalf("AddGroupMember: %v", err)
	}
	if err := ChangeGroupHost(project, groupID, memberID); err != nil {
		t.Fatalf("ChangeGroupHost: %v", err)
	}

	if err := SetGroupMemberDisabled(groupID, memberID, true); !errors.Is(err, ErrCannotDisableHost) {
		t.Fatalf("new host must not be disable-able: %v", err)
	}
	if err := SetGroupMemberDisabled(groupID, oldHostID, true); err != nil {
		t.Fatalf("old host must become an ordinary member: %v", err)
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

// Disabling the same member twice must write the disable event only ONCE. The second
// call changes no row (already disabled), so a second "已禁用" row would be pure
// timeline noise — and it counts toward every member's cursor, so it gets
// injected into their context too.
func TestSetGroupMemberDisabled_RepeatedDisableWritesOneEvent(t *testing.T) {
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

	if err := SetGroupMemberDisabled(groupID, memberID, true); err != nil {
		t.Fatalf("first SetGroupMemberDisabled: %v", err)
	}
	if err := SetGroupMemberDisabled(groupID, memberID, true); err != nil {
		t.Fatalf("second SetGroupMemberDisabled: %v", err)
	}

	after := len(systemMessages(t, groupID))
	if after != before+1 {
		t.Fatalf("repeated disable must write exactly one disable event: before=%d after=%d", before, after)
	}
}

// Disabling a member id that belongs to a DIFFERENT group must not write an event
// into this group's timeline. The UPDATE is scoped by group_id so it changes no
// row here; without a RowsAffected check the code would still append
// "X 已禁用" — naming a member who was never in this group and polluting its
// minutes.
func TestSetGroupMemberDisabled_CrossGroupWritesNoEvent(t *testing.T) {
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
	// A member of B, disabled via A's id.
	memberB, err := AddGroupMember(project, groupB, "claude", "agent-a", "A")
	if err != nil {
		t.Fatalf("AddGroupMember: %v", err)
	}
	before := len(systemMessages(t, groupA))

	// Must not error and must not touch A's timeline.
	_ = SetGroupMemberDisabled(groupA, memberB, true)

	after := len(systemMessages(t, groupA))
	if after != before {
		t.Fatalf("a cross-group disable must not write an event into group A: before=%d after=%d", before, after)
	}
	// And B's member must stay enabled (A's UPDATE is scoped by group_id).
	members, err := ListGroupMembers(groupB)
	if err != nil {
		t.Fatalf("ListGroupMembers: %v", err)
	}
	found := false
	for _, m := range members {
		if m.ID == memberB {
			found = true
			if m.Disabled {
				t.Fatal("a cross-group disable must not disable the member in its real group")
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
	// Names must be unique: the first keeps the bare name, duplicates draw the
	// next unused pool names ("A 1" is never produced).
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
	for _, want := range []string{"A", groupNamePool[0], groupNamePool[1]} {
		if !names[want] {
			t.Fatalf("missing expected instance name %q (got %v)", want, names)
		}
	}
	if names["A 1"] {
		t.Fatal(`"A 1" must never be produced`)
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
	if countSpeakingMembers(before) != maxGroupMembers-1 {
		t.Fatalf("precondition: want %d active, got %d", maxGroupMembers-1, countSpeakingMembers(before))
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
	if countSpeakingMembers(after) != maxGroupMembers-1 {
		t.Fatalf("a rejected batch must not add ANY member: active %d -> %d", countSpeakingMembers(before), countSpeakingMembers(after))
	}

	// A batch of exactly 1 is accepted (cap-1 -> cap).
	if _, err := AddGroupMembers(project, groupID, []GroupMemberSpec{
		{AgentID: "agent-new-1", Backend: "claude", DisplayName: "N1"},
	}); err != nil {
		t.Fatalf("a batch within the cap must be accepted: %v", err)
	}
	final, _ := ListGroupMembers(groupID)
	if countSpeakingMembers(final) != maxGroupMembers {
		t.Fatalf("want %d active after the accepted batch, got %d", maxGroupMembers, countSpeakingMembers(final))
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
	if members, _ := ListGroupMembers(groupID); countSpeakingMembers(members) != maxGroupMembers {
		t.Fatalf("precondition: want %d active, got %d", maxGroupMembers, countSpeakingMembers(members))
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

	addPendingBcc(groupID, "B", "给B一", bccRoleHost, "Host")
	addPendingBcc(groupID, "B", "给B二", bccRoleHost, "Host")
	addPendingBcc(groupID, "C", "给C", bccRoleHost, "Host")
	addPendingBcc(otherGroup, "B", "别的群", bccRoleHost, "Host2")
	// Empty target/content are ignored.
	addPendingBcc(groupID, "", "x", bccRoleHost, "Host")
	addPendingBcc(groupID, "B", "  ", bccRoleHost, "Host")

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
	addPendingBcc(groupID, "User", "给你的", bccRoleHost, "Host")
	addPendingBcc(groupID, "A", "给A的", bccRoleHost, "Host")

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

	// A member who was DISABLED must still resolve (include-disabled rule): their
	// earlier messages keep their name instead of degrading to a generic
	// assistant.
	if err := SetGroupMemberDisabled(groupA, mA2, true); err != nil {
		t.Fatalf("SetGroupMemberDisabled: %v", err)
	}
	got, err = GroupMemberSpeakersForGroups([]string{groupA})
	if err != nil {
		t.Fatalf("GroupMemberSpeakersForGroups after disabling: %v", err)
	}
	if got[groupA][mA2] != "A2" {
		t.Fatalf("a disabled member must still resolve its name, got %+v", got[groupA])
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

// SetMemberAvatar is the service-level write used by the avatar endpoint. It is
// exercised here (not only through the handler) because Go coverage is
// per-package: a function called solely from a handler test leaves this package
// at 0% for those lines.
func TestSetMemberAvatar_ScopedWrite(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/group-set-avatar"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, hostID, err := CreateGroup(project, "G", "codebuddy", "codebuddy", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	const svg = "<svg xmlns=\"http://www.w3.org/2000/svg\"></svg>"

	// A member id in this group updates exactly one row.
	n, err := SetMemberAvatar(groupID, hostID, svg)
	if err != nil {
		t.Fatalf("SetMemberAvatar: %v", err)
	}
	if n != 1 {
		t.Fatalf("SetMemberAvatar affected %d rows, want 1", n)
	}
	members, err := ListGroupMembers(groupID)
	if err != nil {
		t.Fatalf("ListGroupMembers: %v", err)
	}
	if len(members) != 1 || members[0].Avatar != svg {
		t.Fatalf("stored avatar = %+v, want %q", members, svg)
	}

	// A member id that belongs to ANOTHER group matches no row: the WHERE clause
	// pins group_id, so the write is a no-op and the count is 0.
	otherGroup, _, err := CreateGroup(project, "Other", "codebuddy", "codebuddy", "Host")
	if err != nil {
		t.Fatalf("CreateGroup other: %v", err)
	}
	n, err = SetMemberAvatar(otherGroup, hostID, svg)
	if err != nil {
		t.Fatalf("SetMemberAvatar cross-group: %v", err)
	}
	if n != 0 {
		t.Fatalf("cross-group write affected %d rows, want 0", n)
	}

	// An ordinary (non-member) session has no agent_avatar meaning: the
	// session_type guard means its id matches no member row.
	plainID, err := CreateSession(project, "codebuddy", "plain", "codebuddy", "", "default", "chat")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	n, err = SetMemberAvatar(groupID, plainID, svg)
	if err != nil {
		t.Fatalf("SetMemberAvatar plain session: %v", err)
	}
	if n != 0 {
		t.Fatalf("plain-session write affected %d rows, want 0", n)
	}
}

// IsGroupMemberSession distinguishes a hidden member row from a group timeline
// row and an ordinary chat. It gates operations that make no sense on a member
// row (e.g. renaming a session), so the three cases matter.
func TestIsGroupMemberSession(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/group-member-session"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, hostID, err := CreateGroup(project, "G", "codebuddy", "codebuddy", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if !IsGroupMemberSession(hostID) {
		t.Fatalf("the host member row %q must be a member session", hostID)
	}
	if IsGroupMemberSession(groupID) {
		t.Fatalf("the group timeline row %q must NOT be a member session", groupID)
	}
	plainID, err := CreateSession(project, "codebuddy", "plain", "codebuddy", "", "default", "chat")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if IsGroupMemberSession(plainID) {
		t.Fatalf("an ordinary chat %q must NOT be a member session", plainID)
	}
	if IsGroupMemberSession("does-not-exist") {
		t.Fatal("an unknown session id must NOT be a member session")
	}
}

// AddGroupMembers must treat a batch whose specs all carry an EMPTY agent id as
// a no-op: the defensive filter drops them and the function returns without
// touching the DB (no phantom member rows, no error).
func TestAddGroupMembers_AllEmptySpecsNoOp(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/group-empty-specs"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, _, err := CreateGroup(project, "G", "codebuddy", "codebuddy", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	before, err := ListGroupMembers(groupID)
	if err != nil {
		t.Fatalf("ListGroupMembers: %v", err)
	}

	ids, err := AddGroupMembers(project, groupID, []GroupMemberSpec{{AgentID: ""}, {AgentID: ""}})
	if err != nil {
		t.Fatalf("all-empty specs must not error: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("all-empty specs must return no ids, got %v", ids)
	}
	after, err := ListGroupMembers(groupID)
	if err != nil {
		t.Fatalf("ListGroupMembers after: %v", err)
	}
	if len(after) != len(before) {
		t.Fatalf("all-empty specs must add no rows: before=%d after=%d", len(before), len(after))
	}
}

// TestGroupMemberContexts covers the member context query that backs the group
// member list's per-member usage chip and its detail panel:
//   - usage/model come from the MEMBER row (a member turn's RunConfig.SessionID
//     is that row, so its context_state is where the executor persists usage);
//   - a removed member is included (its row still exists, archived);
//   - a malformed context_state degrades to nil usage instead of failing the
//     whole roster.
func TestGroupMemberContexts(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/grouptest-ctx"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, hostMemberID, err := CreateGroup(project, "ctx", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	otherID, err := AddGroupMember(project, groupID, "claude", "agent-a", "A")
	if err != nil {
		t.Fatalf("AddGroupMember: %v", err)
	}

	// The host reports usage; the other member never did (CLI transport).
	PatchContextStateMerge(hostMemberID, map[string]string{
		"usage": `{"used":12000,"size":200000}`,
	})
	if err := UpdateSessionModel(otherID, "claude-sonnet-4"); err != nil {
		t.Fatalf("UpdateSessionModel: %v", err)
	}

	got, err := GroupMemberContexts(groupID)
	if err != nil {
		t.Fatalf("GroupMemberContexts: %v", err)
	}
	require.Len(t, got, 2)

	host := got[hostMemberID]
	require.NotNil(t, host.Usage, "host usage must be parsed from context_state")
	assert.Equal(t, 12000, host.Usage.Used)
	assert.Equal(t, 200000, host.Usage.Size)

	other := got[otherID]
	assert.Nil(t, other.Usage, "a member that never reported usage must be nil, not zero-valued")
	assert.Equal(t, "claude-sonnet-4", other.Model)

	// A disabled member keeps its row (and its context window — that is exactly
	// what may need compacting while it is disabled), so its context is still
	// returned.
	require.NoError(t, SetGroupMemberDisabled(groupID, otherID, true))
	got, err = GroupMemberContexts(groupID)
	require.NoError(t, err)
	require.Contains(t, got, otherID, "disabled members must still resolve")
}

// TestGroupMemberContexts_MalformedJSONIsNotFatal pins the degradation policy:
// one unreadable context_state must not take down the whole roster (the sheet
// would then show no members at all instead of one member without a chip).
func TestGroupMemberContexts_MalformedJSONIsNotFatal(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/grouptest-ctx-bad"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, hostMemberID, err := CreateGroup(project, "ctx", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if _, err := store.WriteExec("UPDATE chat_sessions SET context_state = '{not json' WHERE id = ?", hostMemberID); err != nil {
		t.Fatalf("corrupt context_state: %v", err)
	}

	got, err := GroupMemberContexts(groupID)
	require.NoError(t, err, "a corrupt row must not fail the query")
	require.Contains(t, got, hostMemberID)
	assert.Nil(t, got[hostMemberID].Usage)
}

// TestCompactGroupMemberGuards pins the pre-flight rejections. The happy path
// requires a live ACP agent, but the guards are what a user hits in practice and
// they must run BEFORE any backend is created.
func TestCompactGroupMemberGuards(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/grouptest-compact"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, hostMemberID, err := CreateGroup(project, "c", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	otherID, err := AddGroupMember(project, groupID, "claude", "agent-a", "A")
	if err != nil {
		t.Fatalf("AddGroupMember: %v", err)
	}

	// Empty ids.
	if _, err := CompactGroupMember(context.Background(), "", hostMemberID); !errors.Is(err, ErrGroupMemberNotFound) {
		t.Fatalf("empty group id: err=%v", err)
	}
	// Member of a DIFFERENT group.
	otherGroup, _, err := CreateGroup(project, "c2", "codebuddy", "agent-host", "Host2")
	if err != nil {
		t.Fatalf("CreateGroup 2: %v", err)
	}
	if _, err := CompactGroupMember(context.Background(), otherGroup, hostMemberID); !errors.Is(err, ErrGroupMemberNotFound) {
		t.Fatalf("cross-group member: err=%v", err)
	}
	// A DISABLED member is NOT rejected: disabling only takes it off the schedule, and
	// its context window is still a legitimate compaction target.
	if err := SetGroupMemberDisabled(groupID, otherID, true); err != nil {
		t.Fatalf("SetGroupMemberDisabled: %v", err)
	}
	if _, err := CompactGroupMember(context.Background(), groupID, otherID); errors.Is(err, ErrGroupMemberNotFound) {
		t.Fatalf("a disabled member must not be reported as not-found: err=%v", err)
	}
	// CLI transport (the test agents default to CLI) has no command channel.
	_, err = CompactGroupMember(context.Background(), groupID, hostMemberID)
	if !errors.Is(err, ErrCompactNeedsACP) {
		t.Fatalf("cli member: err=%v", err)
	}
}

// TestCompactGroupMember_RejectsWhileGroupRuns pins the ordering guard: a group
// turn owns every member connection, so compaction must be refused rather than
// interleaving a second prompt on one ACP session.
func TestCompactGroupMember_RejectsWhileGroupRuns(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/grouptest-compact-busy"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, hostMemberID, err := CreateGroup(project, "c", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	SetSessionRunning(groupID, true, true)
	t.Cleanup(func() { SetSessionRunning(groupID, false, true) })

	if _, err := CompactGroupMember(context.Background(), groupID, hostMemberID); !errors.Is(err, ErrGroupRunning) {
		t.Fatalf("running group: err=%v", err)
	}
}

// TestGroupHasCompactingMember pins the gate the chat handler uses to hold a
// send while a member compaction is in flight.
//
// Why it matters: a compaction drives the member's ACP connection directly. A
// group turn starting meanwhile would send a SECOND prompt on that connection —
// the agent answers "Session is busy" (the user's message fails), and
// RegisterSession/promptCancel are overwritten, silently dropping the
// compaction's events and leaving one prompt uncancellable.
func TestGroupHasCompactingMember(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/grouptest-compacting"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, hostMemberID, err := CreateGroup(project, "c", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	otherGroup, _, err := CreateGroup(project, "c2", "codebuddy", "agent-host", "Host2")
	if err != nil {
		t.Fatalf("CreateGroup 2: %v", err)
	}

	if GroupHasCompactingMember(groupID) {
		t.Fatal("no compaction in flight, gate must be open")
	}
	if IsGroupMemberCompacting(hostMemberID) {
		t.Fatal("no compaction in flight for this member")
	}

	// Simulate an in-flight compaction (the real path needs a live ACP agent).
	if !beginCompaction(groupID, hostMemberID) {
		t.Fatal("beginCompaction must succeed on a free slot")
	}
	t.Cleanup(func() { endCompaction(hostMemberID) })

	if !IsGroupMemberCompacting(hostMemberID) {
		t.Fatal("member must report compacting")
	}
	if !GroupHasCompactingMember(groupID) {
		t.Fatal("owning group must report a compacting member")
	}
	// Another group must stay open: the gate is per-group, not global.
	if GroupHasCompactingMember(otherGroup) {
		t.Fatal("an unrelated group must not be gated")
	}
	// A second compaction on the same member is refused.
	if beginCompaction(groupID, hostMemberID) {
		t.Fatal("beginCompaction must refuse a member that is already compacting")
	}
}

// TestWakeQueuedAfterCompaction pins the no-op half of the post-compaction
// wake: an empty queue must not claim the session (claiming it would mark the
// group "running" with nothing to consume, and the next send would queue behind
// a phantom run).
//
// The non-empty half launches a real group consumer goroutine, which races this
// test's DB teardown; that launch path is EnsureConsumer's own contract (see
// queue_reaper_test.go), so it is not re-tested through this seam.
func TestWakeQueuedAfterCompaction(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/grouptest-wake"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, _, err := CreateGroup(project, "c", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}

	wakeQueuedAfterCompaction(groupID)
	if IsSessionRunning(groupID) {
		t.Fatal("an empty queue must not start a runner")
	}
	if got := GetQueuedCount(groupID); got != 0 {
		t.Fatalf("queued count = %d, want 0", got)
	}
}

// TestCompactionSystemRows pins the two markers a member compaction leaves on
// the group timeline.
//
// Why they exist: compaction silently rewrites what that member remembers, so
// the discussion needs a marker explaining why its earlier detail stopped being
// available. They go through the SAME channel as membership rows — a
// role='system' row, rendered as a centered line and injected to members as
// "[系统] …" — never as speech.
func TestCompactionSystemRows(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/grouptest-compact-rows"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, hostMemberID, err := CreateGroup(project, "c", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}

	before := &ai.UsageState{Used: 320000, Size: 1000000}
	after := &ai.UsageState{Used: 30000, Size: 1000000}
	writeCompactionSystemMessage(project, groupID, "Host", hostMemberID, "GroupMemberCompactionStarted", nil)
	writeCompactionSystemMessage(project, groupID, "Host", hostMemberID, "GroupMemberCompactionDone",
		map[string]any{"Before": usagePercentLabel(before), "After": usagePercentLabel(after)})

	msgs, err := GetMessagesBySessionIDRaw(groupID)
	require.NoError(t, err)
	var systemRows []string
	for _, m := range msgs {
		if m.Role == "system" {
			systemRows = append(systemRows, ExtractPlainText(m.Content))
		}
	}
	require.Len(t, systemRows, 2, "compaction must leave exactly a start/finish pair")

	// Both rows name the member (so a group with several members is unambiguous)
	// and are readable sentences, not i18n keys.
	for _, row := range systemRows {
		assert.Contains(t, row, "Host", "the row must name the member: %q", row)
		assert.NotContains(t, row, "GroupMemberCompaction", "a key leaked into the timeline: %q", row)
	}
	// The finish row reports the actual effect, which is the point of the pair.
	assert.Contains(t, systemRows[1], "32%", systemRows[1])
	assert.Contains(t, systemRows[1], "3%", systemRows[1])
}

// TestUsagePercentLabel pins the "unknown is not 0%" rule used by the finish row.
func TestUsagePercentLabel(t *testing.T) {
	assert.Equal(t, "—", usagePercentLabel(nil))
	assert.Equal(t, "—", usagePercentLabel(&ai.UsageState{Used: 100, Size: 0}))
	assert.Equal(t, "0%", usagePercentLabel(&ai.UsageState{Used: 0, Size: 1000}))
	assert.Equal(t, "32%", usagePercentLabel(&ai.UsageState{Used: 320000, Size: 1000000}))
	assert.Equal(t, "100%", usagePercentLabel(&ai.UsageState{Used: 1000, Size: 1000}))
}

// The per-target pending-note queue is bounded: an idle target that never
// speaks would otherwise grow it without limit. On overflow the OLDEST note is
// evicted (a stale directive is less useful than the one just written).
func TestPendingBccQueueIsBounded(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/gorch-bcc-cap"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, _, err := CreateGroup(project, "G", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}

	for i := range maxPendingBccPerTarget + 5 {
		addPendingBcc(groupID, "B", fmt.Sprintf("note-%d", i), bccRoleHost, "Host")
	}
	got := pendingBccForTarget(groupID, "B")
	if len(got) != maxPendingBccPerTarget {
		t.Fatalf("queue must be capped at %d, got %d", maxPendingBccPerTarget, len(got))
	}
	// The oldest five were evicted; the newest survives.
	if got[0].Content != "note-5" {
		t.Fatalf("the oldest notes must be evicted, first=%q", got[0].Content)
	}
	last := fmt.Sprintf("note-%d", maxPendingBccPerTarget+4)
	if got[len(got)-1].Content != last {
		t.Fatalf("the newest note must survive, last=%q", got[len(got)-1].Content)
	}
}

// A queue that is ALREADY over the cap must converge on the next write, not
// stay over it forever.
//
// This is the upgrade path, and it is not hypothetical: the cap was introduced
// in the same change that added it, so an install upgrading from a build
// without any cap can start above 64. An implementation that evicts exactly one
// row per insert holds the line at the cap but never recovers — the queue would
// stay at its historical high-water mark minus nothing. The test therefore seeds
// the overflow DIRECTLY (bypassing addPendingBcc, which is the only writer that
// enforces the cap) and asserts a single add brings it back to the cap.
func TestPendingBccQueueConvergesFromOverflow(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/gorch-bcc-converge"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, _, err := CreateGroup(project, "G", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}

	const seeded = maxPendingBccPerTarget + 40
	for i := range seeded {
		if _, err := store.WriteExec(
			`INSERT INTO group_pending_bcc (group_id, target_name, content, sender_role, sender_name) VALUES (?, ?, ?, ?, ?)`,
			groupID, "B", fmt.Sprintf("legacy-%d", i), bccRoleHost, "Host",
		); err != nil {
			t.Fatalf("seed legacy row: %v", err)
		}
	}
	if got := pendingBccForTarget(groupID, "B"); len(got) != seeded {
		t.Fatalf("precondition: expected %d seeded rows, got %d", seeded, len(got))
	}

	addPendingBcc(groupID, "B", "newest", bccRoleHost, "Host")

	got := pendingBccForTarget(groupID, "B")
	if len(got) != maxPendingBccPerTarget {
		t.Fatalf("an over-cap queue must converge to %d in one add, got %d", maxPendingBccPerTarget, len(got))
	}
	if got[len(got)-1].Content != "newest" {
		t.Fatalf("the new note must survive eviction, last=%q", got[len(got)-1].Content)
	}
	// Eviction is oldest-first: the survivors are the tail of the seeded run.
	wantFirst := fmt.Sprintf("legacy-%d", seeded-maxPendingBccPerTarget+1)
	if got[0].Content != wantFirst {
		t.Fatalf("eviction must drop the OLDEST rows, first=%q want %q", got[0].Content, wantFirst)
	}
}

// The sender columns are recorded so the delivery prompt can attribute the
// note. An empty role (a row written before the columns existed) is left empty
// here and rendered as the host wording by pendingBccHeader.
func TestPendingBccStoresSenderAttribution(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/gorch-bcc-sender"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, _, err := CreateGroup(project, "G", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}

	addPendingBcc(groupID, "B", "来自A", bccRoleMember, "A")
	addPendingBcc(groupID, "B", "来自用户", bccRoleUser, "")

	got := pendingBccForTarget(groupID, "B")
	if len(got) != 2 {
		t.Fatalf("expected two notes, got %v", got)
	}
	if got[0].SenderRole != bccRoleMember || got[0].SenderName != "A" {
		t.Fatalf("member attribution lost: %+v", got[0])
	}
	if got[1].SenderRole != bccRoleUser || got[1].SenderName != "" {
		t.Fatalf("user attribution lost: %+v", got[1])
	}
}

// Empty ids are a programming error, not "the group has no host": refusing them
// keeps a blank pointer from being written.
func TestChangeGroupHost_RejectsEmptyIDs(t *testing.T) {
	assert.ErrorIs(t, ChangeGroupHost("/proj/a", "", "m1"), ErrHostNotMember)
	assert.ErrorIs(t, ChangeGroupHost("/proj/a", "g1", ""), ErrHostNotMember)
}

// A database failure while promoting a host must not be reported as "that id is
// not a member" (400) — the user cannot fix an outage by picking someone else.
func TestChangeGroupHost_DatabaseFailureIsNotAMembershipError(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	// Deliberately missing agent_name/agent_id/backend: the GROUP lookup succeeds
	// (so we reach the member query) but the member query fails with a schema
	// error, which must surface as a failure — not as "that id is not a member",
	// which the handler would answer with a 400 the user cannot act on.
	_, err = db.Exec(`CREATE TABLE chat_sessions (
		id TEXT PRIMARY KEY,
		session_type TEXT,
		context_state TEXT,
		updated_at DATETIME
	)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO chat_sessions (id, session_type, context_state) VALUES ('g1', 'group', '{}')`)
	require.NoError(t, err)
	restore := store.SetDBForTest(db, db)
	t.Cleanup(restore)

	err = ChangeGroupHost("/proj/a", "g1", "m1")
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrHostNotMember)
	assert.NotErrorIs(t, err, ErrGroupNotHostMode)
	assert.Contains(t, err.Error(), "no such column")
}
