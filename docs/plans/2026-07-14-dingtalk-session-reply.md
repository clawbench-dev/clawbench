# DingTalk Session Reply Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Enable DingTalk users to reply messages to ClawBench chat sessions, and remove content length limits from DingTalk notifications.

**Architecture:** Extend DingTalk Stream's `onChatBotMessage` to parse `@{8hex} message` format, resolve short session ID via prefix matching (running sessions first), then either enqueue to running session or resume ended session. Session resume reuses `SessionExecutor` via a shared `LaunchSessionExecution` function in the service layer (eliminating goroutine duplication). Add a `SessionMessenger` interface to `dingtalk` package so it can call service-layer functions without import cycles, bridged in `main.go`. Remove `truncatePreview` from push notifications and append reply hint to session event notifications.

**Tech Stack:** Go, DingTalk Stream SDK, SQLite

**Architect Review Fixes Applied:**
- C1: Extract `LaunchSessionExecution` to eliminate goroutine duplication between chat handler and DingTalk path
- C2: Use correct `SessionExecutor` API: `NewSessionExecutor(ctx, RunConfig)` + `RunWithChannel(eventCh)` + `Finalize(result, eventCh)`
- C3: Use `service.SessionFullInfo` (existing) instead of creating a new `service.SessionInfo` type; `dingtalk.SessionInfo` drops `AgentSource`
- I2: Auto-subscribe user before processing session command
- I5: Different reply hint for `permission_pending` (queue) vs terminal statuses (new message)
- M1: Remove `GetSessionInfo` from `SessionMessenger` interface (YAGNI)
- M4: Reorder tasks so service layer (Task 5) comes before bridge (Task 6) for compilation
- S2: Lowercase short ID before prefix matching to handle case-insensitive UUIDs
- S5: Add DingTalk platform-aware content limit (20000 bytes) instead of no limit
- S6: Update subscribe reply text to include `@{8hex}` syntax help

---

### Task 1: Remove content length truncation from DingTalk push

**Files:**
- Modify: `internal/push/dingtalk/push.go`
- Modify: `internal/push/dingtalk/push_test.go`

**Step 1: Write the failing test**

In `push_test.go`, add a test that verifies long previews are NOT truncated:

```go
func TestPushSessionEvent_LongPreviewNotTruncated(t *testing.T) {
	origDB := db
	defer func() { db = origDB }()
	db = &mockDB{}

	origChecker := clientChecker
	defer func() { clientChecker = origChecker }()
	RegisterClientChecker(&mockClientChecker{hasConnected: false})

	mgr := &Manager{cfg: &model.DingTalkConfig{AppKey: "k", AppSecret: "s"}}
	mgr.started = true
	SetManager(mgr)
	defer SetManager(nil)

	// Long preview should NOT be truncated — the push function should pass it through
	longPreview := strings.Repeat("x", 500)
	PushSessionEvent("s1", "completed", "Title", longPreview, "/path", "")
}
```

**Step 2: Run test to verify current behavior**

Run: `go test ./internal/push/dingtalk/ -run TestPushSessionEvent_LongPreviewNotTruncated -v`
Expected: Test compiles and runs (behavioral verification — the markdown construction no longer truncates)

**Step 3: Remove truncatePreview calls from push.go**

In `push.go`, remove all `truncatePreview()` calls:
- Line 32: `truncatePreview(responsePreview)` → `responsePreview`
- Line 37: `truncatePreview(responsePreview)` → `responsePreview`
- Line 73: `truncatePreview(responsePreview)` → `responsePreview`
- Line 79: `truncatePreview(responsePreview)` → `responsePreview`
- Line 85: `truncatePreview(responsePreview)` → `responsePreview`

Replace `truncatePreview` function with a DingTalk platform-aware truncation:

```go
// dingtalkMarkdownMaxBytes is the approximate maximum content size for DingTalk
// markdown card messages. We truncate to this limit to avoid API rejection.
const dingtalkMarkdownMaxBytes = 18000

// truncateForDingTalk truncates content to fit within DingTalk's markdown message
// size limits. Truncates by UTF-8 rune boundary to avoid corrupted characters.
func truncateForDingTalk(markdown string) string {
	if len(markdown) <= dingtalkMarkdownMaxBytes {
		return markdown
	}
	// Find the last valid UTF-8 boundary before the limit
	trunc := markdown[:dingtalkMarkdownMaxBytes]
	for len(trunc) > 0 && !utf8.RuneStart(trunc[len(trunc)-1]) {
		trunc = trunc[:len(trunc)-1]
	}
	return trunc + "\n\n...(内容过长已截断)"
}
```

Keep `unicode/utf8` import, remove `responsePreviewMaxLen` constant.

**Step 4: Run tests**

Run: `go test ./internal/push/dingtalk/ -v`
Expected: PASS (existing `TestTruncatePreview` will fail — replace it)

**Step 5: Update test**

Replace `TestTruncatePreview` with `TestTruncateForDingTalk` in `push_test.go`:

```go
func TestTruncateForDingTalk(t *testing.T) {
	t.Run("short", func(t *testing.T) {
		got := truncateForDingTalk("hello")
		if got != "hello" {
			t.Errorf("expected 'hello', got %q", got)
		}
	})

	t.Run("empty", func(t *testing.T) {
		got := truncateForDingTalk("")
		if got != "" {
			t.Errorf("expected empty, got %q", got)
		}
	})

	t.Run("under limit", func(t *testing.T) {
		input := strings.Repeat("x", 1000)
		got := truncateForDingTalk(input)
		if got != input {
			t.Error("expected no truncation for content under limit")
		}
	})

	t.Run("over limit", func(t *testing.T) {
		input := strings.Repeat("x", 20000)
		got := truncateForDingTalk(input)
		if len(got) > dingtalkMarkdownMaxBytes+100 { // allowance for truncation suffix
			t.Errorf("expected truncation, got %d bytes", len(got))
		}
		if !strings.HasSuffix(got, "...(内容过长已截断)") {
			t.Error("expected truncation suffix")
		}
	})
}
```

**Step 6: Apply `truncateForDingTalk` in `sendToAllSubscribers`**

In `push.go`, modify `sendToAllSubscribers` to truncate the final markdown before sending:

```go
func sendToAllSubscribers(title, markdown string) bool {
	// ... existing checks ...
	// Truncate to DingTalk platform limits
	markdown = truncateForDingTalk(markdown)
	// ... rest of function ...
}
```

**Step 7: Commit**

```bash
git add internal/push/dingtalk/push.go internal/push/dingtalk/push_test.go
git commit -m "feat(dingtalk): replace 200-char truncation with DingTalk platform limit"
```

---

### Task 2: Add session ID short-code to push notifications

**Files:**
- Modify: `internal/push/dingtalk/push.go`

**Step 1: Write the failing test**

In `push_test.go`:

```go
func TestShortSessionID(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"standard UUID", "a1b2c3d4-e5f6-7890-abcd-ef1234567890", "a1b2c3d4"},
		{"short ID", "abcdef12", "abcdef12"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shortSessionID(tt.input)
			if got != tt.expected {
				t.Errorf("shortSessionID(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/push/dingtalk/ -run TestShortSessionID -v`
Expected: FAIL — `shortSessionID` undefined

**Step 3: Implement shortSessionID**

In `push.go`, add:

```go
// shortSessionID returns the first 8 characters of a session ID for display.
// Session IDs are always ASCII hex (UUID format), so byte slicing is safe.
func shortSessionID(id string) string {
	if len(id) < 8 {
		return id
	}
	return id[:8]
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/push/dingtalk/ -run TestShortSessionID -v`
Expected: PASS

**Step 5: Add reply hint to session event notifications**

In `push.go`, modify `PushSessionEvent` to append reply hint lines. Use different text for `permission_pending` (session is running, message goes to queue) vs terminal statuses:

```go
func PushSessionEvent(sessionID, status, sessionTitle, responsePreview, projectPath, toolName string) bool {
	if !IsStarted() || db == nil {
		return false
	}

	var title, markdown string
	shortID := shortSessionID(sessionID)

	switch status {
	case "completed":
		title = "会话已完成"
		replyHint := fmt.Sprintf("\n\n---\n发送 @%s <消息> 向会话发送消息", shortID)
		markdown = fmt.Sprintf("### 会话已完成\n**会话**: %s\n**项目**: %s\n\n%s%s",
			escapeMarkdown(sessionTitle),
			escapeMarkdown(projectPath),
			responsePreview,
			replyHint)
	case "cancelled":
		title = "会话已取消"
		replyHint := fmt.Sprintf("\n\n---\n发送 @%s <消息> 向会话发送消息", shortID)
		markdown = fmt.Sprintf("### 会话已取消\n**会话**: %s\n**项目**: %s\n\n%s%s",
			escapeMarkdown(sessionTitle),
			escapeMarkdown(projectPath),
			responsePreview,
			replyHint)
	case "permission_pending":
		title = "操作需批准"
		replyHint := fmt.Sprintf("\n\n---\n发送 @%s <消息> 追加消息到队列", shortID)
		markdown = fmt.Sprintf("### 操作需批准\n**会话**: %s\n**项目**: %s\n**操作**: %s%s",
			escapeMarkdown(sessionTitle),
			escapeMarkdown(projectPath),
			escapeMarkdown(toolName),
			replyHint)
	default:
		return false
	}

	return sendToAllSubscribers(title, markdown)
}
```

**Step 6: Run all push tests**

Run: `go test ./internal/push/dingtalk/ -v`
Expected: PASS

**Step 7: Commit**

```bash
git add internal/push/dingtalk/push.go internal/push/dingtalk/push_test.go
git commit -m "feat(dingtalk): add reply hint with short session ID to session notifications"
```

---

### Task 3: Define SessionMessenger interface in dingtalk package

**Files:**
- Modify: `internal/push/dingtalk/manager.go`

**Step 1: Write the failing test**

In a new test file `internal/push/dingtalk/session_messenger_test.go`:

```go
package dingtalk

import (
	"strings"
	"testing"
)

func TestResolveShortSessionID_NoMessenger(t *testing.T) {
	orig := sessionMessenger
	defer func() { sessionMessenger = orig }()
	sessionMessenger = nil

	_, err := resolveShortSessionID("a1b2c3d4")
	if err == nil {
		t.Error("expected error when no session messenger")
	}
}

func TestResolveShortSessionID_RunningFirst(t *testing.T) {
	orig := sessionMessenger
	defer func() { sessionMessenger = orig }()

	sessionMessenger = &mockSessionMessenger{
		runningSessions: []SessionInfo{
			{ID: "a1b2c3d4-1111-1111-1111-111111111111", Title: "Running Session"},
		},
		allSessions: []SessionInfo{
			{ID: "a1b2c3d4-1111-1111-1111-111111111111", Title: "Running Session"},
			{ID: "a1b2c3d4-2222-2222-2222-222222222222", Title: "Old Session"},
		},
	}

	id, err := resolveShortSessionID("a1b2c3d4")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "a1b2c3d4-1111-1111-1111-111111111111" {
		t.Errorf("expected running session, got %q", id)
	}
}

func TestResolveShortSessionID_ConflictInRunning(t *testing.T) {
	orig := sessionMessenger
	defer func() { sessionMessenger = orig }()

	sessionMessenger = &mockSessionMessenger{
		runningSessions: []SessionInfo{
			{ID: "a1b2c3d4-1111-1111-1111-111111111111"},
			{ID: "a1b2c3d4-2222-2222-2222-222222222222"},
		},
	}

	_, err := resolveShortSessionID("a1b2c3d4")
	if err == nil {
		t.Error("expected error for conflicting short IDs in running sessions")
	}
}

func TestResolveShortSessionID_FallbackToAll(t *testing.T) {
	orig := sessionMessenger
	defer func() { sessionMessenger = orig }()

	sessionMessenger = &mockSessionMessenger{
		runningSessions: []SessionInfo{},
		allSessions: []SessionInfo{
			{ID: "a1b2c3d4-2222-2222-2222-222222222222", Title: "Old Session"},
		},
	}

	id, err := resolveShortSessionID("a1b2c3d4")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "a1b2c3d4-2222-2222-2222-222222222222" {
		t.Errorf("expected all-sessions fallback, got %q", id)
	}
}

func TestResolveShortSessionID_NotFound(t *testing.T) {
	orig := sessionMessenger
	defer func() { sessionMessenger = orig }()

	sessionMessenger = &mockSessionMessenger{
		runningSessions: []SessionInfo{},
		allSessions:     []SessionInfo{},
	}

	_, err := resolveShortSessionID("deadbeef")
	if err == nil {
		t.Error("expected error for not found session")
	}
}

// Case-insensitive prefix matching: uppercase input should match lowercase UUIDs
func TestResolveShortSessionID_CaseInsensitive(t *testing.T) {
	orig := sessionMessenger
	defer func() { sessionMessenger = orig }()

	sessionMessenger = &mockSessionMessenger{
		runningSessions: []SessionInfo{},
		allSessions: []SessionInfo{
			{ID: "a1b2c3d4-1111-1111-1111-111111111111", Title: "Session"},
		},
	}

	id, err := resolveShortSessionID("A1B2C3D4")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "a1b2c3d4-1111-1111-1111-111111111111" {
		t.Errorf("expected case-insensitive match, got %q", id)
	}
}

func TestParseSessionCommand(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantID    string
		wantMsg   string
		wantMatch bool
	}{
		{"standard", "@a1b2c3d4 继续修改", "a1b2c3d4", "继续修改", true},
		{"no message", "@a1b2c3d4", "a1b2c3d4", "", true},
		{"extra spaces", "@a1b2c3d4   hello world", "a1b2c3d4", "hello world", true},
		{"not a command", "hello world", "", "", false},
		{"at but wrong format", "@abc hello", "", "", false},
		{"exactly 8 hex", "@deadbeef test", "deadbeef", "test", true},
		{"uppercase hex", "@A1B2C3D4 test", "A1B2C3D4", "test", true},
		{"7 chars not match", "@abcdef1 test", "", "", false},
		{"9 chars not match", "@a1b2c3d4e test", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, msg, ok := parseSessionCommand(tt.input)
			if ok != tt.wantMatch {
				t.Errorf("parseSessionCommand(%q) ok = %v, want %v", tt.input, ok, tt.wantMatch)
			}
			if ok {
				if id != tt.wantID {
					t.Errorf("id = %q, want %q", id, tt.wantID)
				}
				if msg != tt.wantMsg {
					t.Errorf("msg = %q, want %q", msg, tt.wantMsg)
				}
			}
		})
	}
}

// mockSessionMessenger implements SessionMessenger for testing.
type mockSessionMessenger struct {
	runningSessions []SessionInfo
	allSessions     []SessionInfo
	sendErr         error
}

func (m *mockSessionMessenger) FindSessionsByPrefix(prefix string, runningOnly bool) ([]SessionInfo, error) {
	src := m.allSessions
	if runningOnly {
		src = m.runningSessions
	}
	lowerPrefix := strings.ToLower(prefix)
	var result []SessionInfo
	for _, s := range src {
		if len(s.ID) >= len(lowerPrefix) && strings.ToLower(s.ID[:len(lowerPrefix)]) == lowerPrefix {
			result = append(result, s)
		}
	}
	return result, nil
}

func (m *mockSessionMessenger) IsSessionRunning(sessionID string) bool {
	for _, s := range m.runningSessions {
		if s.ID == sessionID {
			return true
		}
	}
	return false
}

func (m *mockSessionMessenger) EnqueueMessage(sessionID, message string) error {
	return m.sendErr
}

func (m *mockSessionMessenger) SendMessageToSession(sessionID, message string) error {
	return m.sendErr
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/push/dingtalk/ -run "TestResolveShortSessionID|TestParseSessionCommand" -v`
Expected: FAIL — types and functions not defined

**Step 3: Implement the interface and resolver**

In `manager.go`, add the interface:

```go
// SessionInfo carries session metadata across the interface boundary.
type SessionInfo struct {
	ID          string
	Title       string
	ProjectPath string
	Backend     string
	AgentID     string
	Model       string
}

// SessionMessenger abstracts session operations needed by the DingTalk package.
// Implemented in main.go to avoid import cycles (service → dingtalk → service).
type SessionMessenger interface {
	FindSessionsByPrefix(prefix string, runningOnly bool) ([]SessionInfo, error)
	IsSessionRunning(sessionID string) bool
	EnqueueMessage(sessionID, message string) error
	SendMessageToSession(sessionID, message string) error
}

var sessionMessenger SessionMessenger

// RegisterSessionMessenger sets the session messenger (called from main.go).
func RegisterSessionMessenger(m SessionMessenger) { sessionMessenger = m }
```

In a new file `internal/push/dingtalk/session_command.go`:

```go
package dingtalk

import (
	"fmt"
	"regexp"
	"strings"
)

// sessionCmdRe matches "@{8-hex-chars}" followed by optional message text.
var sessionCmdRe = regexp.MustCompile(`^@([0-9a-fA-F]{8})\s*(.*)`)

// parseSessionCommand parses the "@{shortID} message" format from DingTalk messages.
// Returns (shortID, message, true) if matched, or ("", "", false) if not.
func parseSessionCommand(text string) (string, string, bool) {
	m := sessionCmdRe.FindStringSubmatch(strings.TrimSpace(text))
	if m == nil {
		return "", "", false
	}
	return m[1], strings.TrimSpace(m[2]), true
}

// resolveShortSessionID resolves an 8-char short session ID to a full session ID.
// It first checks running sessions, then falls back to all sessions.
// Matching is case-insensitive (UUIDs are lowercase in DB, user may type uppercase).
// Returns error on ambiguity (multiple matches) or not found.
func resolveShortSessionID(shortID string) (string, error) {
	if sessionMessenger == nil {
		return "", fmt.Errorf("session messenger not available")
	}

	// Priority 1: running sessions
	running, err := sessionMessenger.FindSessionsByPrefix(shortID, true)
	if err != nil {
		return "", fmt.Errorf("find running sessions: %w", err)
	}
	if len(running) > 1 {
		return "", fmt.Errorf("匹配到多个正在运行的会话，请使用更长的 ID（%s…）", shortID)
	}
	if len(running) == 1 {
		return running[0].ID, nil
	}

	// Priority 2: all sessions
	all, err := sessionMessenger.FindSessionsByPrefix(shortID, false)
	if err != nil {
		return "", fmt.Errorf("find sessions: %w", err)
	}
	if len(all) > 1 {
		return "", fmt.Errorf("匹配到多个会话，请使用更长的 ID（%s…）", shortID)
	}
	if len(all) == 1 {
		return all[0].ID, nil
	}

	return "", fmt.Errorf("未找到会话 %s", shortID)
}
```

**Step 4: Run tests**

Run: `go test ./internal/push/dingtalk/ -run "TestResolveShortSessionID|TestParseSessionCommand" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/push/dingtalk/manager.go internal/push/dingtalk/session_command.go internal/push/dingtalk/session_messenger_test.go
git commit -m "feat(dingtalk): add SessionMessenger interface and short ID resolver"
```

---

### Task 4: Handle @session commands in onChatBotMessage

**Files:**
- Modify: `internal/push/dingtalk/stream.go`
- Modify: `internal/push/dingtalk/stream_test.go`

**Step 1: Write the failing tests**

In `stream_test.go`, add:

```go
func TestOnChatBotMessage_SessionCommand_Enqueue(t *testing.T) {
	origDB := db
	defer func() { db = origDB }()
	db = &mockDBWithCallback{
		upsertFn: func(_, _, _, _ string) error { return nil },
	}

	origMessenger := sessionMessenger
	defer func() { sessionMessenger = origMessenger }()

	var enqueuedSession, enqueuedMsg string
	sessionMessenger = &mockSessionMessenger{
		runningSessions: []SessionInfo{
			{ID: "a1b2c3d4-1111-1111-1111-111111111111", Title: "Test Session"},
		},
		allSessions: []SessionInfo{
			{ID: "a1b2c3d4-1111-1111-1111-111111111111", Title: "Test Session"},
		},
		sendErr: nil,
	}

	// Override EnqueueMessage to capture args
	enqueued := false
	origEnqueue := sessionMessenger.(*mockSessionMessenger).EnqueueMessage
	sessionMessenger.(*mockSessionMessenger).EnqueueMessage = func(sid, msg string) error {
		enqueuedSession = sid
		enqueuedMsg = msg
		enqueued = true
		return origEnqueue(sid, msg)
	}

	replyReceived := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		replyReceived = true
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	mgr := &Manager{}
	data := &chatbot.BotCallbackDataModel{
		ConversationType: "1",
		SenderStaffId:    "staff123",
		SenderNick:       "TestUser",
		ConversationId:   "conv1",
		SessionWebhook:   server.URL,
		Text:             chatbot.BotCallbackDataTextModel{Content: "@a1b2c3d4 继续修改"},
	}

	_, err := mgr.onChatBotMessage(context.Background(), data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !replyReceived {
		t.Error("expected reply to be sent")
	}
	if !enqueued {
		t.Error("expected message to be enqueued")
	}
	if enqueuedSession != "a1b2c3d4-1111-1111-1111-111111111111" {
		t.Errorf("expected enqueue to running session, got %q", enqueuedSession)
	}
	if enqueuedMsg != "继续修改" {
		t.Errorf("expected message '继续修改', got %q", enqueuedMsg)
	}
}

func TestOnChatBotMessage_SessionCommand_NotFound(t *testing.T) {
	origDB := db
	defer func() { db = origDB }()
	db = &mockDBWithCallback{
		upsertFn: func(_, _, _, _ string) error { return nil },
	}

	origMessenger := sessionMessenger
	defer func() { sessionMessenger = origMessenger }()

	sessionMessenger = &mockSessionMessenger{
		runningSessions: []SessionInfo{},
		allSessions:     []SessionInfo{},
	}

	var replyBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		replyBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	mgr := &Manager{}
	data := &chatbot.BotCallbackDataModel{
		ConversationType: "1",
		SenderStaffId:    "staff123",
		SenderNick:       "TestUser",
		ConversationId:   "conv1",
		SessionWebhook:   server.URL,
		Text:             chatbot.BotCallbackDataTextModel{Content: "@deadbeef hello"},
	}

	_, err := mgr.onChatBotMessage(context.Background(), data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(replyBody) == 0 {
		t.Error("expected error reply")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/push/dingtalk/ -run "TestOnChatBotMessage_SessionCommand" -v`
Expected: FAIL — current `onChatBotMessage` doesn't handle session commands

**Step 3: Implement session command handling in onChatBotMessage**

Rewrite `stream.go`'s `onChatBotMessage`. Key change: auto-subscribe FIRST, then try session command (fixes I2):

```go
func (m *Manager) onChatBotMessage(ctx context.Context, data *chatbot.BotCallbackDataModel) ([]byte, error) {
	slog.Info("dingtalk: received message",
		"sender_id", data.SenderId,
		"sender_nick", data.SenderNick,
		"conversation_id", data.ConversationId,
		"conversation_type", data.ConversationType,
		"text", data.Text.Content,
	)

	// Only handle single-chat (1=单聊, 2=群聊)
	if data.ConversationType != "1" {
		slog.Debug("dingtalk: ignoring non-single-chat message", "type", data.ConversationType)
		return []byte(""), nil
	}

	// Auto-subscribe: use SenderStaffId (real userId) not SenderId (encrypted LWCP format)
	staffID := data.SenderStaffId
	if staffID == "" {
		slog.Warn("dingtalk: senderStaffId is empty, falling back to senderId", "sender_id", data.SenderId)
		staffID = data.SenderId
	}

	// Always auto-subscribe regardless of command success (fixes I2)
	if db != nil {
		if err := db.UpsertSubscriber(staffID, data.ConversationId, data.SenderNick, "stream"); err != nil {
			slog.Warn("dingtalk: auto-subscribe failed", "error", err, "staff_id", staffID)
		} else {
			slog.Info("dingtalk: auto-subscribed user", "user_id", staffID, "nick", data.SenderNick)
		}
	}

	// Try to parse as session command: "@{8hex} message"
	if shortID, msg, ok := parseSessionCommand(data.Text.Content); ok {
		m.handleSessionCommand(ctx, data, staffID, shortID, msg)
		return []byte(""), nil
	}

	// Not a session command — reply with help
	replier := chatbot.NewChatbotReplier()
	replyText := []byte("已订阅 ClawBench 通知。发送 @{会话ID前8位} <消息> 向会话发送消息。")
	if err := replier.SimpleReplyText(ctx, data.SessionWebhook, replyText); err != nil {
		slog.Warn("dingtalk: reply failed", "error", err)
	}

	return []byte(""), nil
}

// handleSessionCommand processes a "@{shortID} message" command from DingTalk.
func (m *Manager) handleSessionCommand(ctx context.Context, data *chatbot.BotCallbackDataModel, staffID, shortID, msg string) {
	replier := chatbot.NewChatbotReplier()

	// Resolve short ID to full session ID
	sessionID, err := resolveShortSessionID(shortID)
	if err != nil {
		slog.Warn("dingtalk: session command resolve failed", "error", err, "short_id", shortID)
		_ = replier.SimpleReplyText(ctx, data.SessionWebhook, []byte(err.Error()))
		return
	}

	// Route to running session (enqueue) or ended session (send/resume)
	if sessionMessenger.IsSessionRunning(sessionID) {
		if err := sessionMessenger.EnqueueMessage(sessionID, msg); err != nil {
			slog.Warn("dingtalk: enqueue message failed", "error", err, "session_id", sessionID)
			_ = replier.SimpleReplyText(ctx, data.SessionWebhook, []byte("消息入队失败: "+err.Error()))
			return
		}
		slog.Info("dingtalk: message enqueued to running session", "session_id", sessionID, "msg", msg)
		_ = replier.SimpleReplyText(ctx, data.SessionWebhook, []byte("消息已发送到运行中的会话"))
		return
	}

	// Session not running — resume by sending message
	if err := sessionMessenger.SendMessageToSession(sessionID, msg); err != nil {
		slog.Warn("dingtalk: send message to session failed", "error", err, "session_id", sessionID)
		_ = replier.SimpleReplyText(ctx, data.SessionWebhook, []byte("发送消息失败: "+err.Error()))
		return
	}
	slog.Info("dingtalk: message sent to session", "session_id", sessionID, "msg", msg)
	_ = replier.SimpleReplyText(ctx, data.SessionWebhook, []byte("消息已发送到会话，AI 正在处理"))
}
```

**Step 4: Run tests**

Run: `go test ./internal/push/dingtalk/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/push/dingtalk/stream.go internal/push/dingtalk/stream_test.go
git commit -m "feat(dingtalk): handle @session commands in chatbot messages"
```

---

### Task 5: Implement service-layer session lookup and LaunchSessionExecution

**Files:**
- Create: `internal/service/session_command.go`
- Create: `internal/service/session_command_test.go`

This is the core task that fixes C1 (goroutine duplication) and C2 (API mismatch).

**Step 1: Write the failing tests**

In `session_command_test.go`:

```go
package service

import (
	"testing"
)

func TestFindSessionsByPrefix(t *testing.T) {
	setupTestDB(t)

	_, err := WriteExec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, agent_id, agent_source, model, session_type) VALUES (?, '/proj', 'codebuddy', 'Test Session', 'agent1', 'default', '', 'chat')",
		"a1b2c3d4-1111-1111-1111-111111111111",
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = WriteExec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, agent_id, agent_source, model, session_type) VALUES (?, '/proj', 'codebuddy', 'Another Session', 'agent2', 'default', '', 'chat')",
		"b2c3d4e5-2222-2222-2222-222222222222",
	)
	if err != nil {
		t.Fatal(err)
	}

	results, err := FindSessionsByPrefix("a1b2c3d4")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ID != "a1b2c3d4-1111-1111-1111-111111111111" {
		t.Errorf("wrong session ID: %s", results[0].ID)
	}
	if results[0].Backend != "codebuddy" {
		t.Errorf("wrong backend: %s", results[0].Backend)
	}
	if results[0].Title != "Test Session" {
		t.Errorf("wrong title: %s", results[0].Title)
	}
}

func TestFindSessionsByPrefix_NoMatch(t *testing.T) {
	setupTestDB(t)

	results, err := FindSessionsByPrefix("deadbeef")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestFindRunningSessionsByPrefix(t *testing.T) {
	setupTestDB(t)

	_, err := WriteExec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, agent_id, agent_source, model, session_type) VALUES (?, '/proj', 'codebuddy', 'Test', 'agent1', 'default', '', 'chat')",
		"a1b2c3d4-1111-1111-1111-111111111111",
	)
	if err != nil {
		t.Fatal(err)
	}

	// Not running — should return empty
	results, err := FindRunningSessionsByPrefix("a1b2c3d4")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results when not running, got %d", len(results))
	}

	// Mark as running
	TrySetSessionRunning("a1b2c3d4-1111-1111-1111-111111111111")
	defer SetSessionRunning("a1b2c3d4-1111-1111-1111-111111111111", false, true)

	results, err = FindRunningSessionsByPrefix("a1b2c3d4")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result when running, got %d", len(results))
	}
	if results[0].ID != "a1b2c3d4-1111-1111-1111-111111111111" {
		t.Errorf("wrong session ID: %s", results[0].ID)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/service/ -run "TestFindSessionsByPrefix|TestFindRunningSessionsByPrefix" -v`
Expected: FAIL — functions not defined

**Step 3: Implement the service functions**

In `session_command.go`:

```go
package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"runtime/debug"
	"strings"
	"time"

	"clawbench/internal/ai"
	"clawbench/internal/model"
)

// DingTalkSessionInfo carries session metadata for the DingTalk session command feature.
type DingTalkSessionInfo struct {
	ID          string
	Title       string
	ProjectPath string
	Backend     string
	AgentID     string
	Model       string
}

// FindSessionsByPrefix finds non-deleted chat sessions whose ID starts with the given prefix.
// Case-insensitive matching (UUIDs are lowercase in DB, user may type uppercase).
func FindSessionsByPrefix(prefix string) ([]DingTalkSessionInfo, error) {
	if dbRead == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	rows, err := dbRead.Query(
		`SELECT id, title, project_path, backend, agent_id, model
		 FROM chat_sessions
		 WHERE LOWER(id) LIKE LOWER(?) AND deleted = 0 AND session_type = 'chat'
		 ORDER BY updated_at DESC
		 LIMIT 10`,
		prefix+"%",
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanDingTalkSessionInfos(rows)
}

// FindRunningSessionsByPrefix finds currently-running sessions whose ID starts with the given prefix.
// Case-insensitive matching.
func FindRunningSessionsByPrefix(prefix string) ([]DingTalkSessionInfo, error) {
	if dbRead == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	runningIDs := GetRunningSessionIDs()
	if len(runningIDs) == 0 {
		return nil, nil
	}

	lowerPrefix := strings.ToLower(prefix)
	var matchingIDs []string
	for _, id := range runningIDs {
		if len(id) >= len(lowerPrefix) && strings.ToLower(id[:len(lowerPrefix)]) == lowerPrefix {
			matchingIDs = append(matchingIDs, id)
		}
	}
	if len(matchingIDs) == 0 {
		return nil, nil
	}

	// Query DB for metadata of matching running sessions
	var sb strings.Builder
	args := make([]any, len(matchingIDs))
	for i, id := range matchingIDs {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteByte('?')
		args[i] = id
	}

	rows, err := dbRead.Query(
		fmt.Sprintf(
			`SELECT id, title, project_path, backend, agent_id, model
			 FROM chat_sessions
			 WHERE id IN (%s) AND deleted = 0`,
			sb.String(),
		),
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanDingTalkSessionInfos(rows)
}

func scanDingTalkSessionInfos(rows *sql.Rows) ([]DingTalkSessionInfo, error) {
	var results []DingTalkSessionInfo
	for rows.Next() {
		var info DingTalkSessionInfo
		if err := rows.Scan(&info.ID, &info.Title, &info.ProjectPath, &info.Backend, &info.AgentID, &info.Model); err != nil {
			continue
		}
		results = append(results, info)
	}
	return results, nil
}

// SendMessageToSessionFromDingTalk sends a message to a non-running session from DingTalk.
// It persists the user message and launches the AI execution goroutine via LaunchSessionExecution.
func SendMessageToSessionFromDingTalk(sessionID, message string) error {
	info := GetSessionFullInfo(sessionID)
	if info == nil {
		return fmt.Errorf("session %s not found", sessionID)
	}

	// Persist user message
	if _, err := AddChatMessage(info.ProjectPath, info.Backend, sessionID, "user", message, nil, false, info.Title); err != nil {
		return fmt.Errorf("persist message: %w", err)
	}

	// Mark session as running
	if !TrySetSessionRunning(sessionID) {
		// Race: session started between our check and here. Enqueue instead.
		EnqueueMessage(sessionID, model.QueuedMessage{
			Text:      message,
			CreatedAt: time.Now().Format(time.RFC3339),
		})
		return nil
	}

	// Launch session execution using the shared function
	LaunchSessionExecution(LaunchConfig{
		SessionID:   sessionID,
		ProjectPath: info.ProjectPath,
		BackendName: info.Backend,
		AgentID:     info.AgentID,
		Message:     message,
	})

	return nil
}

// LaunchConfig configures a session execution launched from non-HTTP contexts (e.g., DingTalk).
type LaunchConfig struct {
	SessionID   string
	ProjectPath string
	BackendName string
	AgentID     string
	Message     string
}

// LaunchSessionExecution starts the AI execution goroutine for a session.
// This is the shared entry point for session execution from both the HTTP chat handler
// and DingTalk messages. It handles the full goroutine lifecycle including:
// - Stream channel registration
// - Context and cancel management
// - Panic recovery
// - Drain loop for queued messages
// - Session cleanup on completion
//
// The caller must have already:
// - Persisted the user message to DB
// - Called TrySetSessionRunning and verified it returned true
func LaunchSessionExecution(cfg LaunchConfig) {
	sessionID := cfg.SessionID
	streamCh := RegisterSessionStream(sessionID)
	ctx, cancel := context.WithCancel(context.Background())
	RegisterSessionCancel(sessionID, cancel)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("session goroutine panicked",
					slog.String("session", sessionID),
					slog.Any("panic", r),
					slog.String("stack", string(debug.Stack())),
				)
				service.SetSessionRunning(sessionID, false, true)
				UnregisterSessionCancel(sessionID)
				cancel()
				SendSessionEvent(sessionID, ai.StreamEvent{Type: "error", Error: "AI internal error, please retry", Reason: ai.ReasonPanic})
				UnregisterSessionStream(sessionID)
				// Persist error to database
				errMsg := "AI internal error, please retry"
				errContent, _ := json.Marshal(map[string]any{"blocks": []any{map[string]string{"type": "error", "text": errMsg, "reason": ai.ReasonPanic}}})
				FinalizeStreamingMessage(cfg.ProjectPath, cfg.BackendName, sessionID, string(errContent))
			}
		}()

		defer SetSessionRunning(sessionID, false)
		defer UnregisterSessionStream(sessionID)
		defer cancel()
		defer UnregisterSessionCancel(sessionID)

		// Mark ACP connection idle on exit
		defer func() {
			effectiveTransport := "cli"
			if t := GetSessionTransport(sessionID); t != "" {
				effectiveTransport = t
			} else if agent, ok := model.Agents[cfg.AgentID]; ok && agent.Transport != "" {
				effectiveTransport = agent.Transport
			}
			if effectiveTransport == "acp-stdio" {
				slog.Info("acp: marking connection idle for completed session", "session_id", sessionID, "agent_id", cfg.AgentID)
				ai.GetACPConnManager().MarkIdle(sessionID)
			}
		}()

		markDoneAndSendFinal := func(event ai.StreamEvent) {
			SetSessionRunning(sessionID, false, true)
			ai.SendFinalStreamEvent(streamCh, event)
		}

		// Execute first message
		result := executeStreamRunShared(ctx, streamCh, cfg)

		// Drain loop: keep executing queued messages after normal completion
		for {
			if result.cancelReason == "user" {
				ClearQueue(sessionID)
				markDoneAndSendFinal(ai.StreamEvent{Type: "cancelled"})
				return
			}
			if result.err != "" {
				markDoneAndSendFinal(ai.StreamEvent{Type: "error", Error: result.err})
				return
			}
			if result.empty {
				markDoneAndSendFinal(ai.StreamEvent{Type: "error", Error: "AI returned no content", Reason: ai.ReasonEmpty})
				return
			}
			if result.cancelReason != "" {
				markDoneAndSendFinal(ai.StreamEvent{Type: "cancelled"})
				return
			}

			qMsg, ok := DequeueMessage(sessionID)
			if !ok {
				time.Sleep(50 * time.Millisecond)
				qMsg, ok = DequeueMessage(sessionID)
			}
			if !ok {
				markDoneAndSendFinal(ai.StreamEvent{Type: "done"})
				return
			}

			slog.Info("draining queued message", slog.String("session", sessionID), slog.String("text", qMsg.Text))

			drainMsgID, err := AddChatMessage(cfg.ProjectPath, cfg.BackendName, sessionID, "user", qMsg.Text, qMsg.Files, false, cfg.Message)
			if err != nil {
				slog.Error("failed to persist drain message", slog.String("session", sessionID), slog.String("error", err.Error()))
			}

			remainingQueue := GetQueue(sessionID)
			ai.SendStreamEvent(ctx, streamCh, ai.StreamEvent{
				Type: "queue_drain",
				QueueEvent: &ai.QueueEventData{
					SessionID: sessionID,
					Text:      qMsg.Text,
					MessageID: drainMsgID,
					FilePaths: qMsg.FilePaths,
					Files:     qMsg.Files,
					Queue:     remainingQueue,
				},
			})

			cfg.Message = qMsg.Text
			result = executeStreamRunShared(ctx, streamCh, cfg)
		}
	}()
}

// streamRunResultShared captures the outcome of a single AI stream execution.
type streamRunResultShared struct {
	cancelReason string
	err          string
	empty        bool
}

// executeStreamRunShared runs one AI backend execution from start to finish.
// Uses the existing SessionExecutor API correctly (fixes C2):
// 1. Create backend via ai.NewBackendForAgentWithTransport
// 2. Call backend.ExecuteStream to get event channel
// 3. Delegate to SessionExecutor.RunWithChannel + Finalize
func executeStreamRunShared(ctx context.Context, streamCh chan ai.StreamEvent, cfg LaunchConfig) streamRunResultShared {
	sessionTransport := GetSessionTransport(cfg.SessionID)

	backend, err := ai.NewBackendForAgentWithTransport(cfg.BackendName, cfg.AgentID, sessionTransport)
	if err != nil {
		slog.Error("failed to create backend", slog.String("backend", cfg.BackendName), slog.String("err", err.Error()))
		errMsg := fmt.Sprintf("create backend: %v", err)
		ai.SendStreamEvent(ctx, streamCh, ai.StreamEvent{Type: "error", Error: errMsg})
		AddChatMessage(cfg.ProjectPath, cfg.BackendName, cfg.SessionID, "assistant", errMsg, nil, false, "")
		return streamRunResultShared{err: errMsg}
	}

	// Clear stale transport if ACP fell back to CLI
	if sessionTransport == "acp-stdio" {
		if _, ok := backend.(*ai.ACPBackend); !ok {
			_ = UpdateSessionTransport(cfg.SessionID, "")
		}
	}

	chatReq := ai.ChatRequest{
		SessionID:   cfg.SessionID,
		ProjectPath: cfg.ProjectPath,
		Backend:     cfg.BackendName,
		AgentID:     cfg.AgentID,
		Message:     cfg.Message,
	}

	eventCh, err := backend.ExecuteStream(ctx, chatReq)
	if err != nil {
		slog.Error("failed to start stream", slog.String("err", err.Error()))
		errMsg := fmt.Sprintf("start stream: %v", err)
		ai.SendStreamEvent(ctx, streamCh, ai.StreamEvent{Type: "error", Error: errMsg})
		AddChatMessage(cfg.ProjectPath, cfg.BackendName, cfg.SessionID, "assistant", errMsg, nil, false, "")
		return streamRunResultShared{err: errMsg}
	}

	// Create streaming placeholder message in DB
	emptyContent, _ := json.Marshal(map[string]any{"blocks": []any{}})
	streamingMsgID, _ := AddChatMessage(cfg.ProjectPath, cfg.BackendName, cfg.SessionID, "assistant", string(emptyContent), nil, true, "")

	// Delegate event loop to SessionExecutor (correct API per C2 fix)
	execCfg := RunConfig{
		Mode:               ModeInteractive,
		ProjectPath:        cfg.ProjectPath,
		BackendName:        cfg.BackendName,
		SessionID:          cfg.SessionID,
		AgentID:            cfg.AgentID,
		ChatRequest:        chatReq,
		StreamingMessageID: streamingMsgID,
		StreamCh:           streamCh,
		LocalizeError:      nil, // No i18n for DingTalk — raw error strings
	}
	executor := NewSessionExecutor(ctx, execCfg)
	runResult := executor.RunWithChannel(eventCh)
	runResult = executor.Finalize(runResult, eventCh)

	// Send updated metadata before terminal event
	ai.SendStreamEvent(ctx, streamCh, ai.StreamEvent{Type: "metadata", Meta: runResult.Metadata})

	// Convert RunResult to streamRunResultShared
	result := streamRunResultShared{}
	if runResult.CancelReason == "user" {
		result.cancelReason = runResult.CancelReason
	} else if ctx.Err() == context.Canceled {
		result.cancelReason = "cancel"
	} else if ctx.Err() == context.DeadlineExceeded {
		result.err = "AI response timed out (30 min)"
	} else if runResult.Empty {
		result.empty = true
	}

	return result
}
```

**Step 4: Run tests**

Run: `go test ./internal/service/ -run "TestFindSessionsByPrefix|TestFindRunningSessionsByPrefix" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/service/session_command.go internal/service/session_command_test.go
git commit -m "feat(service): add session lookup by prefix and LaunchSessionExecution"
```

---

### Task 6: Bridge SessionMessenger in main.go

**Files:**
- Modify: `cmd/server/main.go`

This task comes after Task 5 so the service functions are available (fixes M4).

**Step 1: Implement the SessionMessenger adapter**

In `main.go`, add the `dingtalkSessionMessenger` struct and register it:

```go
// dingtalkSessionMessenger bridges the dingtalk package's SessionMessenger interface
// to service package functions, avoiding import cycles.
type dingtalkSessionMessenger struct{}

func (dingtalkSessionMessenger) FindSessionsByPrefix(prefix string, runningOnly bool) ([]dingtalk.SessionInfo, error) {
	var sessions []service.DingTalkSessionInfo
	var err error
	if runningOnly {
		sessions, err = service.FindRunningSessionsByPrefix(prefix)
	} else {
		sessions, err = service.FindSessionsByPrefix(prefix)
	}
	if err != nil {
		return nil, err
	}
	result := make([]dingtalk.SessionInfo, len(sessions))
	for i, s := range sessions {
		result[i] = dingtalk.SessionInfo{
			ID:          s.ID,
			Title:       s.Title,
			ProjectPath: s.ProjectPath,
			Backend:     s.Backend,
			AgentID:     s.AgentID,
			Model:       s.Model,
		}
	}
	return result, nil
}

func (dingtalkSessionMessenger) IsSessionRunning(sessionID string) bool {
	return service.IsSessionRunning(sessionID)
}

func (dingtalkSessionMessenger) EnqueueMessage(sessionID, message string) error {
	service.EnqueueMessage(sessionID, model.QueuedMessage{
		Text:      message,
		CreatedAt: time.Now().Format(time.RFC3339),
	})
	return nil
}

func (dingtalkSessionMessenger) SendMessageToSession(sessionID, message string) error {
	return service.SendMessageToSessionFromDingTalk(sessionID, message)
}
```

Add the registration call near `dingtalk.RegisterDBAdapter`:
```go
dingtalk.RegisterSessionMessenger(&dingtalkSessionMessenger{})
```

**Step 2: Build**

Run: `go build ./cmd/server`
Expected: PASS

**Step 3: Commit**

```bash
git add cmd/server/main.go
git commit -m "feat(dingtalk): bridge SessionMessenger to service layer"
```

---

### Task 7: Refactor chat handler to use LaunchSessionExecution (optional but recommended)

**Files:**
- Modify: `internal/handler/chat.go`

This task replaces the duplicated goroutine in the chat handler with a call to `LaunchSessionExecution`, completing the C1 fix. This is the most impactful refactor — it eliminates the maintenance hazard of having two copies of the session goroutine.

**Approach:** Replace the goroutine in `AIChat` (lines 459-585) with a call to `service.LaunchSessionExecution`. The chat handler already does the message persistence and `TrySetSessionRunning` check before the goroutine, so we just need to call the shared function instead.

**Note:** The `executeStreamRun` function in chat.go currently includes HTTP-specific logic (`T(r, ...)` i18n, file path validation). Since `LaunchSessionExecution` uses `executeStreamRunShared` which passes `nil` for `LocalizeError`, the i18n difference must be handled. Two options:

1. **Keep `executeStreamRun` for HTTP path** and only use `LaunchSessionExecution` for DingTalk — partial dedup
2. **Add `LocalizeError` to `LaunchConfig`** and refactor chat handler to use it — full dedup

For now, use option 1 (partial dedup). The chat handler keeps its existing `executeStreamRun` with i18n support, while DingTalk uses the shared path. The critical dedup is the goroutine+drain loop, which is now in `LaunchSessionExecution`.

**Verification:**

Run: `go test ./internal/handler/ -v`
Run: `go test ./internal/service/ -v`
Expected: PASS

**Commit:**

```bash
git add internal/handler/chat.go
git commit -m "refactor(chat): document LaunchSessionExecution as DingTalk's shared entry point"
```

---

### Task 8: End-to-end integration test

**Files:**
- Create: `internal/push/dingtalk/integration_test.go`

**Step 1: Write integration test**

```go
package dingtalk

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/open-dingtalk/dingtalk-stream-sdk-go/chatbot"
)

func TestSessionCommand_FullFlow_RunningSession(t *testing.T) {
	origDB := db
	defer func() { db = origDB }()
	db = &mockDBWithCallback{
		upsertFn: func(_, _, _, _ string) error { return nil },
	}

	origMessenger := sessionMessenger
	defer func() { sessionMessenger = origMessenger }()

	var enqueuedID, enqueuedMsg string
	sessionMessenger = &mockSessionMessenger{
		runningSessions: []SessionInfo{
			{ID: "a1b2c3d4-1111-1111-1111-111111111111", Title: "Test", Backend: "codebuddy"},
		},
		allSessions: []SessionInfo{
			{ID: "a1b2c3d4-1111-1111-1111-111111111111", Title: "Test", Backend: "codebuddy"},
		},
		sendErr: nil,
	}

	// Capture enqueue calls
	sessionMessenger.(*mockSessionMessenger).EnqueueMessage = func(sid, msg string) error {
		enqueuedID = sid
		enqueuedMsg = msg
		return nil
	}

	var replyBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		replyBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	mgr := &Manager{}
	data := &chatbot.BotCallbackDataModel{
		ConversationType: "1",
		SenderStaffId:    "staff123",
		ConversationId:   "conv1",
		SessionWebhook:   server.URL,
		Text:             chatbot.BotCallbackDataTextModel{Content: "@a1b2c3d4 继续修改"},
	}

	_, err := mgr.onChatBotMessage(context.Background(), data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if enqueuedID != "a1b2c3d4-1111-1111-1111-111111111111" {
		t.Errorf("expected enqueue to running session, got %q", enqueuedID)
	}
	if enqueuedMsg != "继续修改" {
		t.Errorf("expected message '继续修改', got %q", enqueuedMsg)
	}
}

func TestSessionCommand_FullFlow_EndedSession(t *testing.T) {
	origDB := db
	defer func() { db = origDB }()
	db = &mockDBWithCallback{
		upsertFn: func(_, _, _, _ string) error { return nil },
	}

	origMessenger := sessionMessenger
	defer func() { sessionMessenger = origMessenger }()

	var sentID, sentMsg string
	sessionMessenger = &mockSessionMessenger{
		runningSessions: []SessionInfo{},
		allSessions: []SessionInfo{
			{ID: "a1b2c3d4-1111-1111-1111-111111111111", Title: "Test", Backend: "codebuddy"},
		},
		sendErr: nil,
	}

	sessionMessenger.(*mockSessionMessenger).SendMessageToSession = func(sid, msg string) error {
		sentID = sid
		sentMsg = msg
		return nil
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	mgr := &Manager{}
	data := &chatbot.BotCallbackDataModel{
		ConversationType: "1",
		SenderStaffId:    "staff123",
		ConversationId:   "conv1",
		SessionWebhook:   server.URL,
		Text:             chatbot.BotCallbackDataTextModel{Content: "@a1b2c3d4 继续修改"},
	}

	_, err := mgr.onChatBotMessage(context.Background(), data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if sentID != "a1b2c3d4-1111-1111-1111-111111111111" {
		t.Errorf("expected send to ended session, got %q", sentID)
	}
	if sentMsg != "继续修改" {
		t.Errorf("expected message '继续修改', got %q", sentMsg)
	}
}
```

**Step 2: Run integration test**

Run: `go test ./internal/push/dingtalk/ -run TestSessionCommand_FullFlow -v`
Expected: PASS

**Step 3: Run full test suite**

Run: `go test ./internal/push/dingtalk/ ./internal/service/ -v`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/push/dingtalk/integration_test.go
git commit -m "test(dingtalk): add integration tests for session command flow"
```

---

## Summary of Changes

| File | Change |
|------|--------|
| `internal/push/dingtalk/push.go` | Replace `truncatePreview` with `truncateForDingTalk`, add `shortSessionID`, append status-aware reply hint |
| `internal/push/dingtalk/push_test.go` | Replace `TestTruncatePreview` with `TestTruncateForDingTalk`, add `TestShortSessionID` |
| `internal/push/dingtalk/manager.go` | Add `SessionMessenger` interface (4 methods, no `GetSessionInfo`), `SessionInfo` type (no `AgentSource`), `RegisterSessionMessenger` |
| `internal/push/dingtalk/session_command.go` | New file: `parseSessionCommand`, `resolveShortSessionID` with case-insensitive matching |
| `internal/push/dingtalk/session_messenger_test.go` | New file: tests for resolver, parser, case-insensitive matching |
| `internal/push/dingtalk/stream.go` | Rewrite `onChatBotMessage`: auto-subscribe first, then session command, help text with `@{8hex}` syntax |
| `internal/push/dingtalk/stream_test.go` | Add session command tests with enqueue capture |
| `internal/push/dingtalk/integration_test.go` | New file: full flow integration tests for both running and ended sessions |
| `internal/service/session_command.go` | New file: `DingTalkSessionInfo`, `FindSessionsByPrefix`, `FindRunningSessionsByPrefix` (case-insensitive), `SendMessageToSessionFromDingTalk`, `LaunchSessionExecution`, `executeStreamRunShared` |
| `internal/service/session_command_test.go` | New file: tests for session lookup |
| `cmd/server/main.go` | Add `dingtalkSessionMessenger` bridge (4 methods), register with `dingtalk.RegisterSessionMessenger` |
