package feishu

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/push/common"
)

// ---------------------------------------------------------------------------
// Reply-text builders
// ---------------------------------------------------------------------------

// TestDownloadFailureText pins the three shapes: the size-limit case names the
// configured cap, multiple failures are counted, and a single failure is plain.
func TestDownloadFailureText(t *testing.T) {
	mgr := NewManager(&model.FeishuConfig{AppID: "t", AppSecret: "t"})

	t.Run("too large names the cap", func(t *testing.T) {
		got := mgr.downloadFailureText(downloadOutcome{tooLarge: true, failed: 3})
		if !strings.Contains(got, "大小上限") {
			t.Errorf("size-limit reply should name the cap, got %q", got)
		}
	})

	t.Run("multiple failures are counted", func(t *testing.T) {
		got := mgr.downloadFailureText(downloadOutcome{failed: 2})
		if !strings.Contains(got, "（2 个）") {
			t.Errorf("reply should count the failures, got %q", got)
		}
	})

	t.Run("single failure is plain", func(t *testing.T) {
		got := mgr.downloadFailureText(downloadOutcome{failed: 1})
		if got != "文件下载失败，未发送。" {
			t.Errorf("reply = %q, want the plain single-failure form", got)
		}
	})
}

// TestAttachmentSentText covers each optional note: the multi-file count, the
// partial-failure warning and the truncation warning (which names both the cap
// and how many actually went).
func TestAttachmentSentText(t *testing.T) {
	mgr := NewManager(&model.FeishuConfig{AppID: "t", AppSecret: "t"})
	const sid = "abc12345-1111-1111-1111-111111111111"

	t.Run("single file has no count", func(t *testing.T) {
		got := mgr.attachmentSentText(sid, "My Session", 1, downloadOutcome{sent: 1})
		if strings.Contains(got, "共") {
			t.Errorf("a single file must not print a count, got %q", got)
		}
	})

	t.Run("multiple files are counted", func(t *testing.T) {
		got := mgr.attachmentSentText(sid, "My Session", 3, downloadOutcome{sent: 3})
		if !strings.Contains(got, "（共 3 个文件）") {
			t.Errorf("reply should count the files, got %q", got)
		}
	})

	t.Run("partial failure is warned", func(t *testing.T) {
		got := mgr.attachmentSentText(sid, "My Session", 2, downloadOutcome{sent: 2, failed: 1})
		if !strings.Contains(got, "有 1 个文件下载失败") {
			t.Errorf("reply should warn about the failed file, got %q", got)
		}
	})

	t.Run("truncation is warned with the cap", func(t *testing.T) {
		got := mgr.attachmentSentText(sid, "My Session", 5, downloadOutcome{sent: 5, truncated: true})
		if !strings.Contains(got, "附件数量超过上限") {
			t.Errorf("reply should warn about truncation, got %q", got)
		}
		if !strings.Contains(got, "仅发送前 5 个") {
			t.Errorf("reply should say how many were sent, got %q", got)
		}
	})
}

// ---------------------------------------------------------------------------
// parseInbound / parsePost
// ---------------------------------------------------------------------------

// TestParseInbound_MalformedPostReturnsNothing: a "post" whose content is not a
// valid post object must yield no text and no media (rather than a zero-value
// message that would later be sent as empty).
func TestParseInbound_MalformedPostReturnsNothing(t *testing.T) {
	text, body, media := parseInbound(msgTypePost, "{not json")
	if text != "" || body != "" || media != nil {
		t.Errorf("malformed post = (%q, %q, %v), want all empty", text, body, media)
	}
}

// TestParsePost_MultiRowJoinsWithNewline covers the row separator: two
// text-bearing rows are joined by a single newline, and an image-only row
// contributes no blank line.
func TestParsePost_MultiRowJoinsWithNewline(t *testing.T) {
	content := `{"zh_cn":{"title":"","content":[[{"tag":"text","text":"first"}],[{"tag":"img","image_key":"k1"}],[{"tag":"text","text":"second"}]]}}`
	text, body, keys, ok := parsePost(content)
	if !ok {
		t.Fatal("expected a valid post")
	}
	if text != "first\nsecond" {
		t.Errorf("text = %q, want the rows joined by a newline", text)
	}
	if body != "first\nsecond" {
		t.Errorf("body = %q, want the rows joined by a newline", body)
	}
	if len(keys) != 1 || keys[0] != "k1" {
		t.Errorf("image keys = %v, want [k1]", keys)
	}
}

// ---------------------------------------------------------------------------
// handleIncomingWithMedia routing branches
// ---------------------------------------------------------------------------

// A media message whose text is "/ls" must list sessions instead of downloading
// the attachment into a session.
func TestHandleIncomingWithMedia_ListSessionsShortCircuits(t *testing.T) {
	mediaTestServer(t, "image-bytes")
	mgr, _ := stickyTestEnv(t, "abc12345-1111-1111-1111-111111111111", stickySessions)
	mgr.cachedToken = "test-tenant-token"
	mgr.cachedExp = time.Now().Add(2 * time.Hour)

	sent := make(chan string, 1)
	sessionMessenger.(*mockSessionMessenger).SendFn = func(_ string, msg string, _ []model.FileEntry) error {
		sent <- msg
		return nil
	}

	// "/ls" routes to the session list, so the media is never downloaded/sent.
	mgr.handleIncomingWithMedia(context.Background(), "ou_user1", "/ls", "/ls",
		[]mediaRef{{key: "img_1", resType: msgTypeImage, filename: "image.png"}}, "om_msg_1")

	select {
	case msg := <-sent:
		t.Fatalf("a /ls media message must not send to a session, got %q", msg)
	case <-time.After(300 * time.Millisecond):
	}
}

// A media message with no resolvable target must reply with the /ls hint rather
// than downloading into nowhere.
func TestHandleIncomingWithMedia_NoTargetRepliesHint(t *testing.T) {
	mediaTestServer(t, "image-bytes")
	// Sticky is empty → routing yields RouteNoTarget.
	mgr, _ := stickyTestEnv(t, "", stickySessions)
	mgr.cachedToken = "test-tenant-token"
	mgr.cachedExp = time.Now().Add(2 * time.Hour)

	sent := make(chan string, 1)
	sessionMessenger.(*mockSessionMessenger).SendFn = func(_ string, msg string, _ []model.FileEntry) error {
		sent <- msg
		return nil
	}

	mgr.handleIncomingWithMedia(context.Background(), "ou_user1", "hi", "hi",
		[]mediaRef{{key: "img_1", resType: msgTypeImage, filename: "image.png"}}, "om_msg_1")

	select {
	case msg := <-sent:
		t.Fatalf("an untargeted media message must not send to a session, got %q", msg)
	case <-time.After(300 * time.Millisecond):
	}
}

// A media message whose explicitly named session no longer exists must report
// the failure instead of downloading and then failing to send.
func TestHandleIncomingWithMedia_SessionUnavailable(t *testing.T) {
	mediaTestServer(t, "image-bytes")
	// Sticky exists so routing resolves, but the named short ID does not.
	mgr, _ := stickyTestEnv(t, "abc12345-1111-1111-1111-111111111111", stickySessions)
	mgr.cachedToken = "test-tenant-token"
	mgr.cachedExp = time.Now().Add(2 * time.Hour)

	sent := make(chan string, 1)
	sessionMessenger.(*mockSessionMessenger).SendFn = func(_ string, msg string, _ []model.FileEntry) error {
		sent <- msg
		return nil
	}

	mgr.handleIncomingWithMedia(context.Background(), "ou_user1", "@deadbeef hi", "@deadbeef hi",
		[]mediaRef{{key: "img_1", resType: msgTypeImage, filename: "image.png"}}, "om_msg_1")

	select {
	case msg := <-sent:
		t.Fatalf("media for an unavailable session must not be sent, got %q", msg)
	case <-time.After(300 * time.Millisecond):
	}
}

// mockResolveThenFail finds the session by prefix (so routing succeeds) but
// fails to load it afterwards — the race where a session is archived between
// resolution and the ProjectPath lookup.
type mockResolveThenFail struct {
	found common.SessionInfo
}

func (m *mockResolveThenFail) FindSessionsByPrefix(_ string, _ bool) ([]common.SessionInfo, error) {
	return []common.SessionInfo{m.found}, nil
}

func (m *mockResolveThenFail) ListRecentSessions(_ int) ([]common.SessionInfo, error) {
	return nil, nil
}

func (m *mockResolveThenFail) IsSessionRunning(_ string) bool { return false }

func (m *mockResolveThenFail) SendMessageToSession(_, _ string, _ []model.FileEntry) error {
	return nil
}

func (m *mockResolveThenFail) GetSessionInfo(string) (common.SessionInfo, error) {
	return common.SessionInfo{}, fmt.Errorf("session archived")
}

// TestHandleIncomingWithMedia_ResolveSucceedsThenSessionGone covers the window
// between routing and the ProjectPath lookup: the user must be told the session
// is unavailable rather than having the attachment downloaded into nowhere.
func TestHandleIncomingWithMedia_ResolveSucceedsThenSessionGone(t *testing.T) {
	mediaTestServer(t, "image-bytes")
	box := replyCapture(t)

	mgr, _ := stickyTestEnv(t, "", stickySessions)
	mgr.cachedToken = "test-tenant-token"
	mgr.cachedExp = time.Now().Add(2 * time.Hour)
	origSM := sessionMessenger
	t.Cleanup(func() { sessionMessenger = origSM })
	sessionMessenger = &mockResolveThenFail{
		found: common.SessionInfo{ID: "abc12345-1111-1111-1111-111111111111", Title: "Gone"},
	}

	sent := make(chan string, 1)
	// The failure path must not reach a send at all.
	mgr.handleIncomingWithMedia(context.Background(), "ou_user1", "@abc12345 hi", "@abc12345 hi",
		[]mediaRef{{key: "img_1", resType: msgTypeImage, filename: "image.png"}}, "om_msg_1")

	select {
	case msg := <-sent:
		t.Fatalf("media for a vanished session must not be sent, got %q", msg)
	case <-time.After(300 * time.Millisecond):
	}

	got := waitForReplyText(t, box)
	if !strings.Contains(got, "会话不可用") {
		t.Errorf("reply should say the session is unavailable, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// downloadAll truncation
// ---------------------------------------------------------------------------

// TestDownloadAll_TruncatesToCap: a post may embed more images than the cap, and
// the excess must be dropped (with the outcome flagged) before any download.
func TestDownloadAll_TruncatesToCap(t *testing.T) {
	orig := model.UploadMaxFiles
	model.UploadMaxFiles = 1
	t.Cleanup(func() { model.UploadMaxFiles = orig })

	mediaTestServer(t, "image-bytes")
	mgr := newMediaManager(t)

	refs := []mediaRef{
		{key: "img_1", resType: msgTypeImage, filename: "a.png"},
		{key: "img_2", resType: msgTypeImage, filename: "b.png"},
	}
	entries, out := mgr.downloadAll(context.Background(), refs, "om_msg_1", t.TempDir())

	if len(entries) != 1 {
		t.Errorf("entries = %d, want the cap of 1", len(entries))
	}
	if !out.truncated {
		t.Error("outcome should flag truncation")
	}
	if out.sent != 1 {
		t.Errorf("outcome.sent = %d, want 1", out.sent)
	}
}

// TestDownloadAll_PartialFailureStillSendsTheRest: one bad reference must not
// discard the attachments that did download.
func TestDownloadAll_PartialFailureStillSendsTheRest(t *testing.T) {
	mediaTestServer(t, "image-bytes")
	mgr := newMediaManager(t)

	refs := []mediaRef{
		{key: "img_ok", resType: msgTypeImage, filename: "a.png"},
		{key: "", resType: msgTypeImage, filename: "b.png"}, // empty key → download error
	}
	entries, out := mgr.downloadAll(context.Background(), refs, "om_msg_1", t.TempDir())

	if len(entries) != 1 {
		t.Errorf("entries = %d, want the one good download", len(entries))
	}
	if out.failed != 1 {
		t.Errorf("outcome.failed = %d, want 1", out.failed)
	}
}

// ---------------------------------------------------------------------------
// End-to-end: truncation reply reaches the user
// ---------------------------------------------------------------------------

// A post with more images than the cap must deliver the capped set and tell the
// user the rest were skipped.
func TestOnMessageReceive_PostOverCapWarnsUser(t *testing.T) {
	orig := model.UploadMaxFiles
	model.UploadMaxFiles = 1
	t.Cleanup(func() { model.UploadMaxFiles = orig })

	mediaTestServer(t, "image-bytes")
	box := replyCapture(t)
	project := t.TempDir()
	sessions := []common.SessionInfo{
		{ID: "abc12345-1111-1111-1111-111111111111", Title: "Sticky Session", ProjectPath: project},
	}
	mgr, _ := stickyTestEnv(t, "abc12345-1111-1111-1111-111111111111", sessions)
	mgr.cachedToken = "test-tenant-token"
	mgr.cachedExp = time.Now().Add(2 * time.Hour)

	sentCh := make(chan int, 1)
	sessionMessenger.(*mockSessionMessenger).SendFn = func(_ string, _ string, files []model.FileEntry) error {
		sentCh <- len(files)
		return nil
	}

	event := postEvent(`{"zh_cn":{"title":"","content":[[{"tag":"img","image_key":"i1"}],[{"tag":"img","image_key":"i2"}]]}}`)
	if err := mgr.onMessageReceive(context.TODO(), event); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case n := <-sentCh:
		if n != 1 {
			t.Errorf("delivered files = %d, want the cap of 1", n)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the capped attachment to be sent")
	}

	got := waitForReplyText(t, box)
	if !strings.Contains(got, "附件数量超过上限") {
		t.Errorf("reply should warn about truncation, got %q", got)
	}
}
