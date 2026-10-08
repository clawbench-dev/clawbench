package service

import (
	"testing"

	"clawbench/internal/ai"
	"clawbench/internal/store"
)

// TestRespondPermission_GroupResolvesMemberConnection pins the group-chat
// approval path. The frontend only knows the GROUP session id — that is the
// timeline it is subscribed to and the id it echoes back — but every member
// runs on its own ACP connection keyed by the MEMBER ROW id. Before the fix,
// RespondPermission looked the connection up by the group id, found none, and
// returned "session not running", surfaced to the UI as 404 PermissionNotFound.
// The member's turn then stayed blocked on its pending permission until the
// stall watchdog or the user cancelled the turn.
//
// Auto-approve never hit this: it reads the member row id directly from the
// turn's ChatRequest, so its key already matched the connection.
func TestRespondPermission_GroupResolvesMemberConnection(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/perm-group"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, _, err := CreateGroup(project, "G", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}

	// Two members, each with its own connection keyed by the member row id.
	mA, err := AddGroupMember(project, groupID, "claude", "agent-a", "A")
	if err != nil {
		t.Fatalf("AddGroupMember A: %v", err)
	}
	mB, err := AddGroupMember(project, groupID, "claude", "agent-b", "B")
	if err != nil {
		t.Fatalf("AddGroupMember B: %v", err)
	}

	mgr := ai.GetACPConnManager()
	clientA := ai.NewClawBenchACPClient()
	connA := &ai.ACPConn{}
	connA.SetClientForTest(clientA)
	connA.SetSessionMappingForTest(mA, "acp-A")
	mgr.SetConnForTest(mA, connA)
	defer mgr.CloseConn(mA)

	clientB := ai.NewClawBenchACPClient()
	connB := &ai.ACPConn{}
	connB.SetClientForTest(clientB)
	connB.SetSessionMappingForTest(mB, "acp-B")
	mgr.SetConnForTest(mB, connB)
	defer mgr.CloseConn(mB)

	// A holds an unrelated pending request (different tool call). B holds the one
	// the user is answering. The resolver must pick B and leave A untouched.
	clientA.RegisterPendingPermissionForTest(ai.PermissionKey("acp-A", "other-tc"), &ai.PendingPermissionForTest{})
	clientB.RegisterPendingPermissionForTest(ai.PermissionKey("acp-B", "tc-1"), &ai.PendingPermissionForTest{})

	// The frontend names the GROUP, not the member.
	if err := RespondPermission(groupID, "perm_tc-1", "allow", false); err != nil {
		t.Fatalf("RespondPermission(group) = %v, want nil", err)
	}
	if clientB.HasPendingPermission(ai.PermissionKey("acp-B", "tc-1")) {
		t.Fatal("B's pending permission was not consumed")
	}
	if !clientA.HasPendingPermission(ai.PermissionKey("acp-A", "other-tc")) {
		t.Fatal("A's unrelated pending permission must not be consumed")
	}
}

// TestRespondPermission_GroupWithoutPendingStillFails pins that the group
// fallback does not turn a genuinely-missing permission into a success: a group
// whose members hold no matching pending request must still report not-found so
// the handler keeps returning 404.
func TestRespondPermission_GroupWithoutPendingStillFails(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/perm-group-miss"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, _, err := CreateGroup(project, "G", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	mA, err := AddGroupMember(project, groupID, "claude", "agent-a", "A")
	if err != nil {
		t.Fatalf("AddGroupMember: %v", err)
	}

	mgr := ai.GetACPConnManager()
	client := ai.NewClawBenchACPClient()
	conn := &ai.ACPConn{}
	conn.SetClientForTest(client)
	conn.SetSessionMappingForTest(mA, "acp-A")
	mgr.SetConnForTest(mA, conn)
	defer mgr.CloseConn(mA)

	// No pending registered on the member.
	if err := RespondPermission(groupID, "perm_tc-1", "allow", false); err == nil {
		t.Fatal("RespondPermission(group) with no pending = nil, want error")
	}
}
