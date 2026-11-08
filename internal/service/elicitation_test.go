package service

import (
	"testing"

	"clawbench/internal/ai"
	"clawbench/internal/store"
)

// TestRespondElicitation_GroupResolvesMemberConnection mirrors the permission
// path: the frontend only knows the GROUP session id, but the member row owns
// the ACP connection that is blocked on the question.
func TestRespondElicitation_GroupResolvesMemberConnection(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/elic-group"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, _, err := CreateGroup(project, "G", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}

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

	// A holds an unrelated question; B holds the one being answered.
	clientA.RegisterPendingElicitationForTest("elic-other", "acp-A")
	answered := clientB.RegisterPendingElicitationForTest("elic-1", "acp-B")

	if err := RespondElicitation(groupID, "elic-1", map[string]any{"q0": "b"}); err != nil {
		t.Fatalf("RespondElicitation(group) = %v, want nil", err)
	}
	if clientB.HasPendingElicitation("elic-1") {
		t.Fatal("B's pending elicitation was not consumed")
	}
	if !clientA.HasPendingElicitation("elic-other") {
		t.Fatal("A's unrelated elicitation must not be consumed")
	}

	select {
	case ans := <-answered:
		if ans.Cancelled || ans.Content["q0"] != "b" {
			t.Fatalf("answer = %+v, want q0=b", ans)
		}
	default:
		t.Fatal("the member's pending channel received nothing")
	}
}

func TestRespondElicitation_GroupWithoutPendingFails(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/elic-group-empty"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, _, err := CreateGroup(project, "G", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if _, err := AddGroupMember(project, groupID, "claude", "agent-a", "A"); err != nil {
		t.Fatalf("AddGroupMember: %v", err)
	}

	if err := RespondElicitation(groupID, "elic-missing", map[string]any{"q0": "x"}); err == nil {
		t.Fatal("RespondElicitation(group) with no pending = nil, want error")
	}
}

func TestRespondElicitation_EmptyIDFails(t *testing.T) {
	if err := RespondElicitation("s1", "", nil); err == nil {
		t.Fatal("an empty elicitation id must be rejected")
	}
}
