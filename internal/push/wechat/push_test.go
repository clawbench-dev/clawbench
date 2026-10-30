package wechat

import (
	"testing"

	"clawbench/internal/push/common"

	"github.com/stretchr/testify/assert"
)

func TestPushSessionEvent_RequiresStarted(t *testing.T) {
	defer setupPushMode("wechat")()
	SetManager(nil)

	assert.False(t, PushSessionEvent("s1", "completed", "T", "p", "/p", "", ""))
	assert.False(t, PushTaskEvent("t1", "completed", "T", "p", "/p"))
	assert.False(t, PushForgeEvent("title", "body"))
	assert.False(t, PushUsageMeterEvent("title", "body"))
}

func TestPushSessionEvent_UnknownStatus(t *testing.T) {
	defer setupPushMode("wechat")()
	startedManager(t, common.SubscriberInfo{UserID: "u", ContextToken: "ctx"})

	assert.False(t, PushSessionEvent("s1", "running", "T", "p", "/p", "", ""),
		"only terminal statuses and permission_pending are pushed")
}

func TestPushTaskEvent_UnknownStatus(t *testing.T) {
	defer setupPushMode("wechat")()
	startedManager(t, common.SubscriberInfo{UserID: "u", ContextToken: "ctx"})

	assert.False(t, PushTaskEvent("t1", "unknown", "T", "p", "/p"))
}

func TestPushForgeEvent_RequiresBothParts(t *testing.T) {
	defer setupPushMode("wechat")()
	startedManager(t, common.SubscriberInfo{UserID: "u", ContextToken: "ctx"})

	assert.False(t, PushForgeEvent("", "body"))
	assert.False(t, PushForgeEvent("title", ""))
}

func TestPushUsageMeterEvent_RequiresBothParts(t *testing.T) {
	defer setupPushMode("wechat")()
	startedManager(t, common.SubscriberInfo{UserID: "u", ContextToken: "ctx"})

	assert.False(t, PushUsageMeterEvent("", "body"))
	assert.False(t, PushUsageMeterEvent("title", ""))
}

func TestFormatPermissionDetail(t *testing.T) {
	tests := []struct {
		name      string
		toolName  string
		toolInput string
		want      []string
		notWant   []string
	}{
		{
			name:     "tool name only",
			toolName: "Bash",
			want:     []string{"Bash"},
		},
		{
			name:      "command extracted",
			toolName:  "Bash",
			toolInput: `{"command":"rm -rf /tmp/x"}`,
			want:      []string{"Bash", "rm -rf /tmp/x"},
		},
		{
			name:      "file_path extracted",
			toolName:  "Write",
			toolInput: `{"file_path":"/a/b.go"}`,
			want:      []string{"Write", "/a/b.go"},
		},
		{
			name:      "path fallback",
			toolName:  "Edit",
			toolInput: `{"path":"/c/d.go"}`,
			want:      []string{"Edit", "/c/d.go"},
		},
		{
			name:      "invalid json is ignored, name survives",
			toolName:  "Bash",
			toolInput: `not json`,
			want:      []string{"Bash"},
		},
		{
			name: "empty inputs",
			want: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatPermissionDetail(tt.toolName, tt.toolInput)
			for _, w := range tt.want {
				assert.Contains(t, got, w)
			}
			// WeChat renders plain text: no Markdown markers may leak through.
			assert.NotContains(t, got, "**")
			assert.NotContains(t, got, "`")
		})
	}
}

// TestPushSessionEvent_SendsPlainText is the end-to-end shape check: a completed
// session reaches the wire as a text item with the Markdown stripped.
func TestPushSessionEvent_SendsPlainText(t *testing.T) {
	defer setupPushMode("wechat")()
	startedManager(t, common.SubscriberInfo{UserID: "u", ContextToken: "ctx"})

	mgr, bodies := newSendServer(t, `{"ret":0}`)
	SetManager(mgr)

	ok := PushSessionEvent("abcdef1234567890", "completed", "My Session", "**done** with `code`", "/proj", "", "")
	if !ok {
		t.Fatal("expected the push to succeed")
	}
	if len(*bodies) != 1 {
		t.Fatalf("expected 1 request, got %d", len(*bodies))
	}
	text := textOf((*bodies)[0])
	assert.Contains(t, text, "My Session")
	assert.Contains(t, text, "done with code", "Markdown markers must be stripped")
	assert.Contains(t, text, "abcdef12", "the reply hint must carry the short session id")
	assert.NotContains(t, text, "**")
}

// TestPushShortCircuitsAfterExpiry is the second half of the -14 contract: the
// monitor retiring is not enough, Push* must also stop trying with a dead token
// (otherwise every event spends a round trip and logs a failure).
func TestPushShortCircuitsAfterExpiry(t *testing.T) {
	defer setupPushMode("wechat")()
	startedManager(t, common.SubscriberInfo{UserID: "u", ContextToken: "ctx"})

	mgr, bodies := newSendServer(t, `{"ret":0}`)
	SetManager(mgr)

	// Simulate the -14 path: the monitor retires without going through Stop.
	mgr.retire(mgr.gen)

	assert.False(t, PushSessionEvent("s1", "completed", "T", "p", "/p", "", ""),
		"no push must be attempted once the monitor has retired")
	assert.False(t, PushTaskEvent("t1", "completed", "T", "p", "/p"))
	assert.Empty(t, *bodies, "no request must reach the network")
}

func TestJoinTitleBody(t *testing.T) {
	got := joinTitleBody("**标题**", "正文 https://example.com")
	assert.Equal(t, "标题\n\n正文 https://example.com", got,
		"the title is stripped, a bare URL in the body is untouched")

	// The real forge shape: a Markdown link, not a bare URL. Its target must
	// survive or the notification has nothing to tap.
	got = joinTitleBody("标题", "有新事件\n\n[查看详情](https://github.com/o/r/pull/42)")
	assert.Contains(t, got, "https://github.com/o/r/pull/42")
	assert.Contains(t, got, "查看详情")
}
