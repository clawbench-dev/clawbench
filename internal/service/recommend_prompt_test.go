package service

import (
	"encoding/json"
	"strings"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/store"
)

// TestAssistantConclusion_IncludesAskQuestion ensures the assistant message
// injected into the recommendation prompt also carries AskUserQuestion cards so
// the AI can recommend one of the options.
func TestAssistantConclusion_IncludesAskQuestion(t *testing.T) {
	content := `{"blocks":[
		{"type":"text","text":"I need your choice."},
		{"type":"tool_use","name":"AskUserQuestion","input":{"questions":[
			{"header":"Approach","multiSelect":false,"question":"Which approach?","options":[
				{"label":"Fast","description":"quick but risky"},
				{"label":"Safe","description":"slower"}
			]}
		]}}
	]}`
	out := assistantConclusionFromBlocks("", parseBlocksForTest(t, content))
	if !strings.Contains(out, "I need your choice.") {
		t.Fatalf("expected conclusion text preserved, got: %q", out)
	}
	if !strings.Contains(out, "Which approach?") {
		t.Fatalf("expected ask question included, got: %q", out)
	}
	if !strings.Contains(out, "Fast") || !strings.Contains(out, "Safe") {
		t.Fatalf("expected ask options included, got: %q", out)
	}
	if !strings.Contains(out, "quick but risky") {
		t.Fatalf("expected option description included, got: %q", out)
	}
}

// TestAssistantConclusion_NoAskQuestion stays unchanged when no card is present.
func TestAssistantConclusion_NoAskQuestion(t *testing.T) {
	content := `{"blocks":[{"type":"text","text":"Here is the plan."},{"type":"text","text":"Let me know."}]}`
	out := assistantConclusionFromBlocks("", parseBlocksForTest(t, content))
	if strings.Contains(out, "Question:") {
		t.Fatalf("unexpected ask-question marker, got: %q", out)
	}
	if !strings.Contains(out, "Here is the plan.") {
		t.Fatalf("expected conclusion, got: %q", out)
	}
}

// TestAssistantConclusion_SingleChatKeepsLiteralTag is the 58879 regression:
// a single-chat reply that merely DISCUSSES the tag syntax (an unclosed literal
// `<clawbench-mention` in prose) must keep its full text. The old unconditional
// strip dropped the whole tail (summary cut 1551 → 1258 chars).
func TestAssistantConclusion_SingleChatKeepsLiteralTag(t *testing.T) {
	blocks := []model.ContentBlock{
		{Type: "text", Text: "用户的消息里写 `<clawbench-mention private>` 不会被落库，后面的内容必须保留"},
	}
	got := AssistantConclusion("some-single-chat-session", blocks)
	if !strings.Contains(got, "后面的内容必须保留") {
		t.Fatalf("single-chat tail was truncated: %q", got)
	}
	if !strings.Contains(got, "clawbench-mention") {
		t.Fatalf("single-chat literal tag was stripped: %q", got)
	}
}

// TestAssistantConclusion_UnknownSessionKeepsLiteralTag: an unknown/empty
// session id is NOT a group, so the text is returned verbatim. This is the
// fail-safe default — only a confirmed group timeline strips.
func TestAssistantConclusion_UnknownSessionKeepsLiteralTag(t *testing.T) {
	blocks := []model.ContentBlock{
		{Type: "text", Text: "写 `<clawbench-mention private>` 是语法示例，后面的内容必须保留"},
	}
	got := AssistantConclusion("", blocks)
	if !strings.Contains(got, "后面的内容必须保留") {
		t.Fatalf("unknown-session tail was truncated: %q", got)
	}
}

// TestAssistantConclusion_GroupSessionStripsPrivateNote pins the group half of
// the policy: a group timeline's private note must never reach the summary /
// preview / TTS text (fail-closed), while public prose survives.
func TestAssistantConclusion_GroupSessionStripsPrivateNote(t *testing.T) {
	db, teardown := setupTestDBForChatSummary(t)
	defer teardown()

	_, err := db.Exec(
		"INSERT INTO chat_sessions (id, project_id, backend, title, session_type) VALUES (?, ?, ?, ?, ?)",
		"grp-1", 1, "claude", "G", store.SessionTypeGroup,
	)
	if err != nil {
		t.Fatalf("insert group session: %v", err)
	}

	blocks := []model.ContentBlock{
		{Type: "text", Text: "公开表态 <clawbench-mention targets=\"A\" private>只有A能看到的秘密</clawbench-mention> 结束"},
	}
	got := AssistantConclusion("grp-1", blocks)
	if strings.Contains(got, "只有A能看到的秘密") {
		t.Fatalf("group private note leaked: %q", got)
	}
	if strings.Contains(got, "clawbench-mention") {
		t.Fatalf("group protocol tag leaked: %q", got)
	}
	if !strings.Contains(got, "公开表态") || !strings.Contains(got, "结束") {
		t.Fatalf("group public prose lost: %q", got)
	}
}

func parseBlocksForTest(t *testing.T, content string) []model.ContentBlock {
	t.Helper()
	var wrapper struct {
		Blocks []model.ContentBlock `json:"blocks"`
	}
	if err := json.Unmarshal([]byte(content), &wrapper); err != nil {
		t.Fatalf("parse blocks: %v", err)
	}
	return wrapper.Blocks
}

// TestQuickCommandDetails_OmitsLabel verifies only the command body is injected
// (no "label: " prefix), so the recommendation recommends the actual command.
func TestQuickCommandDetails_OmitsLabel(t *testing.T) {
	items := []ChatQuickSendItem{
		{Label: "生成测试", Command: "run tests"},
		{Label: "", Command: "  run lint  "},
		{Label: "空命令", Command: "   "},
	}
	out := quickCommandDetails(items)
	want := []string{"run tests", "run lint"}
	if len(out) != len(want) {
		t.Fatalf("expected %d commands, got %d: %v", len(want), len(out), out)
	}
	for i := range want {
		if out[i] != want[i] {
			t.Fatalf("command[%d] = %q, want %q", i, out[i], want[i])
		}
	}
}
