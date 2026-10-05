package service

import (
	"testing"

	"clawbench/internal/ai"
)

func TestApplyMemberResumeOverrides_CLI(t *testing.T) {
	req := ai.ChatRequest{SessionID: "member-1", Resume: false, HasConversationHistory: false}
	applyMemberResumeOverrides(&req, "ext-9", false /*isACP*/)
	if req.SessionID != "ext-9" || !req.Resume || !req.HasConversationHistory {
		t.Fatalf("cli: %+v", req)
	}
	if req.AssistantMessageCount == 0 {
		t.Fatal("cli: AssistantMessageCount must be >0 when resuming")
	}
	// No ext id -> fresh, session id unchanged.
	req = ai.ChatRequest{SessionID: "member-1"}
	applyMemberResumeOverrides(&req, "", false)
	if req.SessionID != "member-1" || req.Resume || req.HasConversationHistory {
		t.Fatalf("cli fresh: %+v", req)
	}
}

func TestApplyMemberResumeOverrides_ACPKeepsPoolKey(t *testing.T) {
	req := ai.ChatRequest{SessionID: "member-1", Resume: false, HasConversationHistory: false}
	applyMemberResumeOverrides(&req, "acp-sid-7", true /*isACP*/)
	if req.SessionID != "member-1" {
		t.Fatalf("acp must keep pool key, got %q", req.SessionID)
	}
	if !req.Resume || !req.HasConversationHistory {
		t.Fatalf("acp resume/history must be true: %+v", req)
	}
}
