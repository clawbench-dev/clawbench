package service

import (
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/store"
)

// withTestAgents registers a temporary agent map for the duration of the test.
func withTestAgents(t *testing.T, agents map[string]*model.Agent) {
	t.Helper()
	orig := model.Agents
	model.Agents = agents
	t.Cleanup(func() { model.Agents = orig })
}

// TestAddGroupMembers_AssignsUniqueNames covers the naming rule for a batch:
// the first instance keeps the bare agent name, later instances draw the next
// unused pool name, and " 1" is never produced. A second batch continues the
// ordinal (monotonic, never reusing a taken pool name).
func TestAddGroupMembers_AssignsUniqueNames(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/group-names"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	withTestAgents(t, map[string]*model.Agent{
		"codebuddy": {ID: "codebuddy", Name: "Host", Backend: "codebuddy"},
		"claude":    {ID: "claude", Name: "Claude", Backend: "claude"},
	})

	groupID, _, err := CreateGroup(project, "G", "codebuddy", "codebuddy", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}

	// First batch: three instances of the same agent.
	if _, err := AddGroupMembers(project, groupID, []GroupMemberSpec{
		{AgentID: "claude", Backend: "claude"},
		{AgentID: "claude", Backend: "claude"},
		{AgentID: "claude", Backend: "claude"},
	}); err != nil {
		t.Fatalf("AddGroupMembers: %v", err)
	}
	names := memberNames(t, groupID, "claude")
	assertNames(t, names, "Claude", groupNamePool[0], groupNamePool[1])

	// Second batch: continues from the existing rows, drawing the next unused
	// pool name (never reusing a taken one).
	if _, err := AddGroupMembers(project, groupID, []GroupMemberSpec{
		{AgentID: "claude", Backend: "claude"},
	}); err != nil {
		t.Fatalf("AddGroupMembers (second): %v", err)
	}
	names = memberNames(t, groupID, "claude")
	assertNames(t, names, "Claude", groupNamePool[0], groupNamePool[1], groupNamePool[2])
}

// TestAddGroupMembers_OrdinalNotReusedAfterRemoval covers D6: removing an
// instance does not free its ordinal, because member rows are only archived
// (the count includes them). A later add continues past the removed ordinal.
func TestAddGroupMembers_OrdinalNotReusedAfterRemoval(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/group-names-reuse"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	withTestAgents(t, map[string]*model.Agent{
		"codebuddy": {ID: "codebuddy", Name: "Host", Backend: "codebuddy"},
		"claude":    {ID: "claude", Name: "Claude", Backend: "claude"},
	})

	groupID, _, err := CreateGroup(project, "G", "codebuddy", "codebuddy", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	ids, err := AddGroupMembers(project, groupID, []GroupMemberSpec{
		{AgentID: "claude", Backend: "claude"},
		{AgentID: "claude", Backend: "claude"},
		{AgentID: "claude", Backend: "claude"},
	})
	if err != nil {
		t.Fatalf("AddGroupMembers: %v", err)
	}
	// 删除的是第 2 个实例（池名 groupNamePool[0]）。再加一个不得复用该池名，
	// 取下一个未占用池名（被删实例仍在 taken 中，故跳到 groupNamePool[2]）。
	if err := RemoveGroupMember(groupID, ids[1]); err != nil {
		t.Fatalf("RemoveGroupMember: %v", err)
	}
	if _, err := AddGroupMembers(project, groupID, []GroupMemberSpec{
		{AgentID: "claude", Backend: "claude"},
	}); err != nil {
		t.Fatalf("AddGroupMembers (after removal): %v", err)
	}
	active := activeMemberNames(t, groupID, "claude")
	assertNames(t, active, "Claude", groupNamePool[1], groupNamePool[2])
}

// TestAddGroupMembers_NameCollisionWithUserNickname covers the uniqueness
// fallback: if the derived name collides with the reserved user nickname, the
// ordinal is advanced until unique.
func TestAddGroupMembers_NameCollisionWithUserNickname(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/group-names-collide"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	origNick := model.ChatUserNickname
	model.ChatUserNickname = "Claude"
	t.Cleanup(func() { model.ChatUserNickname = origNick })

	withTestAgents(t, map[string]*model.Agent{
		"codebuddy": {ID: "codebuddy", Name: "Host", Backend: "codebuddy"},
		"claude":    {ID: "claude", Name: "Claude", Backend: "claude"},
	})

	groupID, _, err := CreateGroup(project, "G", "codebuddy", "codebuddy", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	// The bare name "Claude" is taken by the user, so the first instance must
	// bump to "Claude 2".
	if _, err := AddGroupMembers(project, groupID, []GroupMemberSpec{
		{AgentID: "claude", Backend: "claude"},
	}); err != nil {
		t.Fatalf("AddGroupMembers: %v", err)
	}
	names := memberNames(t, groupID, "claude")
	assertNames(t, names, "Claude 2")
}

// memberNames returns the names of ALL rows (archived included) for an agent.
func memberNames(t *testing.T, groupID, agentID string) []string {
	t.Helper()
	members, err := ListGroupMembers(groupID)
	if err != nil {
		t.Fatalf("ListGroupMembers: %v", err)
	}
	var out []string
	for _, m := range members {
		if m.AgentID == agentID {
			out = append(out, m.Name)
		}
	}
	return out
}

// activeMemberNames returns the names of ACTIVE (non-archived) rows for an agent.
func activeMemberNames(t *testing.T, groupID, agentID string) []string {
	t.Helper()
	members, err := ListGroupMembers(groupID)
	if err != nil {
		t.Fatalf("ListGroupMembers: %v", err)
	}
	var out []string
	for _, m := range members {
		if m.AgentID == agentID && !m.Left {
			out = append(out, m.Name)
		}
	}
	return out
}

// assertNames asserts the names EXACTLY, in roster order. Order matters: the
// ordinal must land on the right instance (a swap of "Claude 2"/"Claude 3" onto
// the wrong rows would otherwise pass a set comparison).
func assertNames(t *testing.T, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("names = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("names = %v, want %v (mismatch at index %d)", got, want, i)
		}
	}
}

// TestCreateGroupWithMembers_DuplicateHostTakesFirstInstance pins D4: when the
// host agent appears several times, the host pointer is the FIRST instance.
func TestCreateGroupWithMembers_DuplicateHostTakesFirstInstance(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/group-host-first"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	specs := []GroupMemberSpec{
		{AgentID: "codebuddy", Backend: "codebuddy"},
		{AgentID: "codebuddy", Backend: "codebuddy"},
		{AgentID: "claude", Backend: "claude"},
	}
	groupID, hostMemberID, err := CreateGroupWithMembers(project, "G", "codebuddy", specs)
	if err != nil {
		t.Fatalf("CreateGroupWithMembers: %v", err)
	}
	// The first codebuddy instance (in roster order) must be the host.
	members, err := ListGroupMembers(groupID)
	if err != nil {
		t.Fatalf("ListGroupMembers: %v", err)
	}
	var firstHostAgent string
	for _, m := range members {
		if m.AgentID == "codebuddy" {
			firstHostAgent = m.ID
			break
		}
	}
	if hostMemberID != firstHostAgent {
		t.Fatalf("host pointer = %q, want the first codebuddy instance %q", hostMemberID, firstHostAgent)
	}
}
