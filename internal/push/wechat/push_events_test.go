package wechat

import (
	"strings"
	"testing"

	"clawbench/internal/push/common"
)

// pushedTexts returns the plain-text bodies the fake iLink endpoint received.
func pushedTexts(bodies []map[string]any) []string {
	out := make([]string, 0, len(bodies))
	for _, b := range bodies {
		out = append(out, textOf(b))
	}
	return out
}

// Every session terminal state has its own wording: a cancelled run must not be
// reported as completed, and a permission prompt must point at the queue
// command rather than the send command.
func TestPushSessionEvent_CoversEveryStatus(t *testing.T) {
	cases := []struct {
		status string
		want   string
	}{
		{"completed", "PushSessionCompleted"},
		{"cancelled", "PushSessionCancelled"},
		{"permission_pending", "PushActionRequired"},
	}

	for _, tc := range cases {
		t.Run(tc.status, func(t *testing.T) {
			defer setupPushMode("wechat")()
			startedManager(t, common.SubscriberInfo{UserID: "u", ContextToken: "ctx"})
			mgr, bodies := newSendServer(t, `{"ret":0}`)
			SetManager(mgr)

			if !PushSessionEvent("abcdef12", tc.status, "会话", "预览", "/p", "", "") {
				t.Fatalf("%s must be delivered", tc.status)
			}
			texts := pushedTexts(*bodies)
			if len(texts) != 1 {
				t.Fatalf("want one message, got %d", len(texts))
			}
			if !strings.Contains(texts[0], common.T(tc.want)) {
				t.Fatalf("body %q must carry %s", texts[0], tc.want)
			}
		})
	}
}

// A permission prompt carries the tool name and the reply hint that queues a
// message — the user cannot answer it from the notification otherwise.
func TestPushSessionEvent_PermissionPromptCarriesToolAndQueueHint(t *testing.T) {
	defer setupPushMode("wechat")()
	startedManager(t, common.SubscriberInfo{UserID: "u", ContextToken: "ctx"})
	mgr, bodies := newSendServer(t, `{"ret":0}`)
	SetManager(mgr)

	if !PushSessionEvent("abcdef1234567890", "permission_pending", "会话", "", "/p", "Bash", `{"command":"ls"}`) {
		t.Fatal("permission prompt must be delivered")
	}

	texts := pushedTexts(*bodies)
	if len(texts) != 1 {
		t.Fatalf("want one message, got %d", len(texts))
	}
	for _, want := range []string{"Bash", "ls", "abcdef12"} {
		if !strings.Contains(texts[0], want) {
			t.Fatalf("body %q must carry %q", texts[0], want)
		}
	}
}

func TestPushTaskEvent_CoversEveryStatus(t *testing.T) {
	cases := []struct {
		status string
		want   string
	}{
		{"running", "PushTaskStarted"},
		{"completed", "PushTaskCompleted"},
		{"failed", "PushTaskFailed"},
		{"cancelled", "PushTaskCancelled"},
	}

	for _, tc := range cases {
		t.Run(tc.status, func(t *testing.T) {
			defer setupPushMode("wechat")()
			startedManager(t, common.SubscriberInfo{UserID: "u", ContextToken: "ctx"})
			mgr, bodies := newSendServer(t, `{"ret":0}`)
			SetManager(mgr)

			if !PushTaskEvent("1", tc.status, "任务", "预览", "/p") {
				t.Fatalf("%s must be delivered", tc.status)
			}
			texts := pushedTexts(*bodies)
			if len(texts) != 1 || !strings.Contains(texts[0], common.T(tc.want)) {
				t.Fatalf("body %v must carry %s", texts, tc.want)
			}
		})
	}
}

// The forge/usage wording is owned by the service layer; this layer only strips
// Markdown and delivers. Both must actually reach the wire when complete.
func TestPushForgeAndUsageMeter_DeliverCompleteEvents(t *testing.T) {
	defer setupPushMode("wechat")()
	startedManager(t, common.SubscriberInfo{UserID: "u", ContextToken: "ctx"})
	mgr, bodies := newSendServer(t, `{"ret":0}`)
	SetManager(mgr)

	if !PushForgeEvent("**标题**", "正文") {
		t.Fatal("a complete forge event must be delivered")
	}
	if !PushUsageMeterEvent("额度提醒", "已用 80%") {
		t.Fatal("a complete usage event must be delivered")
	}

	texts := pushedTexts(*bodies)
	if len(texts) != 2 {
		t.Fatalf("want two messages, got %d", len(texts))
	}
	if !strings.Contains(texts[0], "标题") || strings.Contains(texts[0], "**") {
		t.Fatalf("markdown must be stripped from the title, got %q", texts[0])
	}
}

func TestJoinTitleBody_WithoutTitle(t *testing.T) {
	if got := joinTitleBody("", "**正文**"); got != stripMarkdownForWeChat("**正文**") {
		t.Fatalf("empty title must not leave a blank header, got %q", got)
	}
}
