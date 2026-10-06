package service

import (
	"context"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/store"
)

// A second message sent while a group turn is running must be QUEUED, not run
// as a second concurrent orchestrator: two group turns on one timeline would
// interleave member turns and produce garbage (decision #45). This test pins
// the drain entry point's contract: it runs the loop for an already-materialized
// message and returns a DrainResult WITHOUT emitting a terminal event itself
// (the drain loop owns that).
func TestRunGroupTurnDrain_DoesNotEmitTerminal(t *testing.T) {
	setupGroupDB(t)
	silenceGroupUserEmit(t)
	project := "/tmp/gorch-drain"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, hostID, err := CreateGroup(project, "G", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if _, err := AddGroupMember(project, groupID, "claude", "agent-a", "A"); err != nil {
		t.Fatalf("AddGroupMember: %v", err)
	}
	// The drain loop already materialized the user message; simulate that.
	msgID, err := AddChatMessageWithAgent(project, "codebuddy", groupID, "user", "开始讨论", nil, false, "", "")
	if err != nil {
		t.Fatalf("seed user msg: %v", err)
	}

	term := observeGroupTerminal(t)
	runner := func(ctx context.Context, gid string, turn groupMemberTurn) groupMemberResult {
		if turn.IsHost {
			_, _ = AddChatMessageWithAgent(project, "codebuddy", groupID, "assistant",
				`{"blocks":[{"type":"text","text":"`+"<clawbench-group-end/> 结束"+`"}]}`, nil, false, "", hostID)
			return groupMemberResult{}
		}
		return groupMemberResult{}
	}
	res := RunGroupTurnDrain(context.Background(), groupID, msgID, QueuedRow{Content: "开始讨论"}, runner)
	if res.Err != "" {
		t.Fatalf("drain turn must not error: %q", res.Err)
	}
	if *term != 0 {
		t.Fatalf("the drain path must NOT emit its own terminal event (the drain loop does): got %d", *term)
	}
}

// The drain path must not persist or re-announce the user message — the drain
// loop already did both when it materialized the row.
func TestRunGroupTurnDrain_DoesNotDuplicateUserMessage(t *testing.T) {
	setupGroupDB(t)
	silenceGroupUserEmit(t)
	project := "/tmp/gorch-drain-nodup"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, hostID, err := CreateGroup(project, "G", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	msgID, err := AddChatMessageWithAgent(project, "codebuddy", groupID, "user", "只此一条", nil, false, "", "")
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	runner := func(ctx context.Context, gid string, turn groupMemberTurn) groupMemberResult {
		if turn.IsHost {
			_, _ = AddChatMessageWithAgent(project, "codebuddy", groupID, "assistant",
				`{"blocks":[{"type":"text","text":"`+"<clawbench-group-end/> 结束"+`"}]}`, nil, false, "", hostID)
			return groupMemberResult{}
		}
		return groupMemberResult{}
	}
	_ = RunGroupTurnDrain(context.Background(), groupID, msgID, QueuedRow{Content: "只此一条"}, runner)

	msgs, _ := GetMessagesBySessionIDRaw(groupID)
	userCount := 0
	for _, m := range msgs {
		if m.Role == "user" {
			userCount++
		}
	}
	if userCount != 1 {
		t.Fatalf("the drain path must not add a second user message: got %d", userCount)
	}
	_ = model.FileEntry{}
}
