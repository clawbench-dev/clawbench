package wechat

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/push/common"

	ilink "github.com/openilink/openilink-sdk-go"
	"github.com/stretchr/testify/assert"
)

// cdnServer serves AES-128-ECB encrypted bytes, the format DownloadMedia expects.
func cdnServer(t *testing.T, plaintext []byte) (*httptest.Server, string) {
	t.Helper()

	key := []byte("0123456789abcdef")
	ciphertext, err := ilink.EncryptAESECB(plaintext, key)
	if err != nil {
		t.Fatalf("encrypt fixture: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(ciphertext)
	}))
	t.Cleanup(srv.Close)

	// base64(raw 16 bytes) is the image encoding.
	return srv, base64.StdEncoding.EncodeToString(key)
}

func TestDownloadOne_ImageUsesFallbackName(t *testing.T) {
	srv, aesKey := cdnServer(t, []byte("PNGDATA"))
	projectPath := t.TempDir()

	mgr := NewManager(&model.WeChatConfig{BotToken: "tok"})
	mgr.started = true
	mgr.client = newTestClient("http://unused")

	ref := mediaRef{
		kind:     "image",
		filename: "image.png",
		item: ilink.MessageItem{
			Type: ilink.ItemImage,
			ImageItem: &ilink.ImageItem{
				Media: &ilink.CDNMedia{FullURL: srv.URL, AESKey: aesKey, EncryptType: 1},
			},
		},
	}

	entry, err := mgr.downloadOne(context.Background(), ref, projectPath, common.AttachmentMaxBytes())
	if err != nil {
		t.Fatalf("downloadOne failed: %v", err)
	}
	if filepath.Ext(entry.Path) != ".png" {
		t.Errorf("saved as %q, want a .png", entry.Path)
	}
	got, err := os.ReadFile(filepath.Join(projectPath, entry.Path))
	if err != nil {
		t.Fatalf("read saved file: %v", err)
	}
	if string(got) != "PNGDATA" {
		t.Errorf("content = %q, want the decrypted payload", got)
	}
}

func TestDownloadOne_FileUsesProvidedName(t *testing.T) {
	srv, aesKey := cdnServer(t, []byte("PDFDATA"))
	projectPath := t.TempDir()

	mgr := NewManager(&model.WeChatConfig{BotToken: "tok"})
	mgr.started = true
	mgr.client = newTestClient("http://unused")

	ref := mediaRef{
		kind:     "file",
		filename: "report.pdf",
		item: ilink.MessageItem{
			Type: ilink.ItemFile,
			FileItem: &ilink.FileItem{
				Media:    &ilink.CDNMedia{FullURL: srv.URL, AESKey: aesKey, EncryptType: 1},
				FileName: "report.pdf",
			},
		},
	}

	entry, err := mgr.downloadOne(context.Background(), ref, projectPath, common.AttachmentMaxBytes())
	if err != nil {
		t.Fatalf("downloadOne failed: %v", err)
	}
	if filepath.Base(entry.Path) != "report.pdf" {
		t.Errorf("saved as %q, want report.pdf", filepath.Base(entry.Path))
	}
}

// TestDownloadAll_RejectsDeclaredOversize pins the pre-download check: the SDK
// buffers the whole payload in memory, so the only thing standing between a huge
// attachment and a huge allocation is refusing on the declared size.
func TestDownloadAll_RejectsDeclaredOversize(t *testing.T) {
	mgr := NewManager(&model.WeChatConfig{BotToken: "tok"})
	mgr.started = true

	huge := common.AttachmentMaxBytes() + 1
	refs := []mediaRef{{
		kind:         "file",
		filename:     "big.bin",
		declaredSize: huge,
		item: ilink.MessageItem{
			Type:     ilink.ItemFile,
			FileItem: &ilink.FileItem{Media: &ilink.CDNMedia{EncryptQueryParam: "q", AESKey: "k"}},
		},
	}}

	entries, out := mgr.downloadAll(context.Background(), refs, t.TempDir())
	if len(entries) != 0 {
		t.Fatalf("expected no entries, got %d", len(entries))
	}
	if out.failed != 1 || !out.tooLarge {
		t.Errorf("outcome = %+v, want 1 failed with tooLarge", out)
	}
}

// TestDownloadAll_PartialFailureKeepsGoodOnes: one bad attachment must not
// discard a message that also carried a valid one.
func TestDownloadAll_PartialFailureKeepsGoodOnes(t *testing.T) {
	srv, aesKey := cdnServer(t, []byte("OK"))
	projectPath := t.TempDir()

	mgr := NewManager(&model.WeChatConfig{BotToken: "tok"})
	mgr.started = true
	mgr.client = newTestClient("http://unused")

	refs := []mediaRef{
		{
			kind: "image", filename: "image.png",
			item: ilink.MessageItem{
				Type:      ilink.ItemImage,
				ImageItem: &ilink.ImageItem{Media: &ilink.CDNMedia{FullURL: srv.URL, AESKey: aesKey}},
			},
		},
		{
			kind: "image", filename: "broken.png",
			item: ilink.MessageItem{
				Type: ilink.ItemImage,
				// No URL and no query param: resolution fails.
				ImageItem: &ilink.ImageItem{Media: &ilink.CDNMedia{AESKey: aesKey}},
			},
		},
	}

	entries, out := mgr.downloadAll(context.Background(), refs, projectPath)
	if len(entries) != 1 {
		t.Fatalf("expected 1 successful entry, got %d", len(entries))
	}
	if out.failed != 1 || out.sent != 1 {
		t.Errorf("outcome = %+v, want 1 sent and 1 failed", out)
	}
}

func TestDownloadAll_TruncatesToCap(t *testing.T) {
	mgr := NewManager(&model.WeChatConfig{BotToken: "tok"})
	mgr.started = true
	mgr.client = newTestClient("http://unused")

	// One more than the cap, all broken so nothing is actually downloaded.
	n := common.AttachmentMaxFiles() + 1
	refs := make([]mediaRef, 0, n)
	for range n {
		refs = append(refs, mediaRef{
			kind: "image", filename: "image.png",
			item: ilink.MessageItem{
				Type:      ilink.ItemImage,
				ImageItem: &ilink.ImageItem{Media: &ilink.CDNMedia{}},
			},
		})
	}

	_, out := mgr.downloadAll(context.Background(), refs, t.TempDir())
	if !out.truncated {
		t.Error("expected the outcome to report truncation")
	}
	if out.failed != common.AttachmentMaxFiles() {
		t.Errorf("attempted %d, want the cap %d", out.failed, common.AttachmentMaxFiles())
	}
}

// TestDownloadAll_ExactlyAtCapIsNotTruncated pins the boundary: flipping the
// comparison to >= would silently drop the last attachment of a message that
// fits exactly, and the +1 fixture above cannot tell the two apart.
func TestDownloadAll_ExactlyAtCapIsNotTruncated(t *testing.T) {
	mgr := NewManager(&model.WeChatConfig{BotToken: "tok"})
	mgr.started = true
	mgr.client = newTestClient("http://unused")

	n := common.AttachmentMaxFiles()
	refs := make([]mediaRef, 0, n)
	for range n {
		refs = append(refs, mediaRef{
			kind: "image", filename: "image.png",
			item: ilink.MessageItem{
				Type:      ilink.ItemImage,
				ImageItem: &ilink.ImageItem{Media: &ilink.CDNMedia{}},
			},
		})
	}

	_, out := mgr.downloadAll(context.Background(), refs, t.TempDir())
	assert.False(t, out.truncated, "exactly at the cap must not be reported as truncated")
	assert.Equal(t, n, out.failed, "every attachment must be attempted")
}

// ── handleIncomingWithMedia ────────────────────────────────────────────────

// withSessionMessenger installs a fake messenger for one test.
func withSessionMessenger(t *testing.T, m common.SessionMessenger) {
	t.Helper()
	prev := sessionMessenger
	RegisterSessionMessenger(m)
	t.Cleanup(func() { RegisterSessionMessenger(prev) })
}

// A slash command that arrives with an attachment must be answered, not
// silently swallowed: the command never routes to a session, so forwarding the
// attachment would drop the command word.
func TestHandleIncomingWithMedia_CommandCannotCarryAttachment(t *testing.T) {
	defer setupPushMode("wechat")()
	startedManager(t)
	mgr, bodies := newSendServer(t, `{"ret":0}`)
	SetManager(mgr)

	mgr.handleIncomingWithMedia(context.Background(), "u1", "ctx-1", "/new", []mediaRef{{kind: "image"}})

	texts := pushedTexts(*bodies)
	if len(texts) != 1 {
		t.Fatalf("want one reply, got %d", len(texts))
	}
	if !strings.Contains(texts[0], "new") {
		t.Fatalf("the reply must name the command, got %q", texts[0])
	}
}

// With no sticky session and no explicit target there is nowhere to put the
// files, so the user gets the "/ls" hint instead of a silent drop.
func TestHandleIncomingWithMedia_NoTargetHints(t *testing.T) {
	defer setupPushMode("wechat")()
	startedManager(t)
	mgr, bodies := newSendServer(t, `{"ret":0}`)
	SetManager(mgr)

	mgr.handleIncomingWithMedia(context.Background(), "u1", "ctx-1", "看看这个", []mediaRef{{kind: "image"}})

	if len(*bodies) != 1 {
		t.Fatalf("want one reply, got %d", len(*bodies))
	}
}

func TestHandleIncomingWithMedia_NoMessengerExplains(t *testing.T) {
	defer setupPushMode("wechat")()
	startedManager(t)
	withSessionMessenger(t, nil)
	mgr, bodies := newSendServer(t, `{"ret":0}`)
	SetManager(mgr)

	mgr.handleIncomingWithMedia(context.Background(), "u1", "ctx-1", "看看这个", []mediaRef{{kind: "image"}})

	if len(*bodies) != 1 {
		t.Fatalf("want one reply, got %d", len(*bodies))
	}
}

// The sticky session may be gone (deleted, archived): say so instead of
// downloading into a path that no longer exists.
func TestHandleIncomingWithMedia_ResolveFailureExplains(t *testing.T) {
	defer setupPushMode("wechat")()
	startedManager(t)
	db.(*mockDB).lastSessionID = "deadbeef"
	withSessionMessenger(t, &mockSessionMessenger{})
	mgr, bodies := newSendServer(t, `{"ret":0}`)
	SetManager(mgr)

	mgr.handleIncomingWithMedia(context.Background(), "u1", "ctx-1", "看看这个", []mediaRef{{kind: "image"}})

	if len(*bodies) != 1 {
		t.Fatalf("want one reply, got %d", len(*bodies))
	}
}

// An oversized attachment is rejected before it is buffered; the user must be
// told the reason (the configured cap), not just "failed".
func TestHandleIncomingWithMedia_AllDownloadsFailedExplains(t *testing.T) {
	defer setupPushMode("wechat")()
	startedManager(t)
	db.(*mockDB).lastSessionID = "abcdef12"
	withSessionMessenger(t, &mockSessionMessenger{sessions: []common.SessionInfo{{ID: "abcdef12", Title: "T", ProjectPath: t.TempDir()}}})
	mgr, bodies := newSendServer(t, `{"ret":0}`)
	SetManager(mgr)

	ref := mediaRef{kind: "image", declaredSize: common.AttachmentMaxBytes() + 1}
	mgr.handleIncomingWithMedia(context.Background(), "u1", "ctx-1", "看看这个", []mediaRef{ref})

	texts := pushedTexts(*bodies)
	if len(texts) != 1 {
		t.Fatalf("want one reply, got %d", len(texts))
	}
	if !strings.Contains(texts[0], common.T("PushFileTooLarge", map[string]any{
		"MaxMB": common.AttachmentMaxBytes() / (1024 * 1024),
	})) {
		t.Fatalf("the size-limit reason must be shown, got %q", texts[0])
	}
}

func TestHandleIncomingWithMedia_SendFailureExplains(t *testing.T) {
	defer setupPushMode("wechat")()
	startedManager(t)
	db.(*mockDB).lastSessionID = "abcdef12"
	project := t.TempDir()
	withSessionMessenger(t, &mockSessionMessenger{
		sessions: []common.SessionInfo{{ID: "abcdef12", Title: "T", ProjectPath: project}},
		sendErr:  errors.New("session busy"),
	})
	mgr, bodies := newSendServer(t, `{"ret":0}`)
	SetManager(mgr)

	srv, aesKey := cdnServer(t, []byte("PNGDATA"))
	ref := mediaRef{
		kind:     "image",
		filename: "image.png",
		item: ilink.MessageItem{
			Type:      ilink.ItemImage,
			ImageItem: &ilink.ImageItem{Media: &ilink.CDNMedia{FullURL: srv.URL, AESKey: aesKey, EncryptType: 1}},
		},
	}

	mgr.handleIncomingWithMedia(context.Background(), "u1", "ctx-1", "看看这个", []mediaRef{ref})

	texts := pushedTexts(*bodies)
	if len(texts) != 1 || !strings.Contains(texts[0], "session busy") {
		t.Fatalf("the send error must be surfaced, got %v", texts)
	}
}

// The happy path: text and attachment reach the session as one turn, and only
// then does the sticky target move.
func TestHandleIncomingWithMedia_SendsFilesAndText(t *testing.T) {
	defer setupPushMode("wechat")()
	startedManager(t)
	db.(*mockDB).lastSessionID = "abcdef12"
	project := t.TempDir()
	messenger := &mockSessionMessenger{
		sessions: []common.SessionInfo{{ID: "abcdef12", Title: "T", ProjectPath: project}},
	}
	withSessionMessenger(t, messenger)
	mgr, bodies := newSendServer(t, `{"ret":0}`)
	SetManager(mgr)

	srv, aesKey := cdnServer(t, []byte("PNGDATA"))
	ref := mediaRef{
		kind:     "image",
		filename: "image.png",
		item: ilink.MessageItem{
			Type:      ilink.ItemImage,
			ImageItem: &ilink.ImageItem{Media: &ilink.CDNMedia{FullURL: srv.URL, AESKey: aesKey, EncryptType: 1}},
		},
	}

	mgr.handleIncomingWithMedia(context.Background(), "u1", "ctx-1", "看看这个", []mediaRef{ref})

	if messenger.sentSessionID != "abcdef12" {
		t.Fatalf("sent to %q, want the sticky session", messenger.sentSessionID)
	}
	if messenger.sentMessage != "看看这个" {
		t.Fatalf("message = %q, want the user's text", messenger.sentMessage)
	}
	if len(messenger.sentFiles) != 1 {
		t.Fatalf("want one file, got %d", len(messenger.sentFiles))
	}
	if texts := pushedTexts(*bodies); len(texts) != 1 {
		t.Fatalf("want one confirmation, got %v", texts)
	}
}

// ── reply wording ──────────────────────────────────────────────────────────

func TestDownloadFailureText_NamesTheReason(t *testing.T) {
	mgr := NewManager(&model.WeChatConfig{BotToken: "tok"})

	if got := mgr.downloadFailureText(downloadOutcome{tooLarge: true}); !strings.Contains(got, common.T("PushFileTooLarge", map[string]any{
		"MaxMB": common.AttachmentMaxBytes() / (1024 * 1024),
	})) {
		t.Fatalf("size limit must be named, got %q", got)
	}
	if got := mgr.downloadFailureText(downloadOutcome{failed: 2}); !strings.Contains(got, common.T("PushFilesDownloadFailed", map[string]any{"Count": 2})) {
		t.Fatalf("the count must be named, got %q", got)
	}
	if got := mgr.downloadFailureText(downloadOutcome{failed: 1}); got != common.T("PushFileDownloadFailed") {
		t.Fatalf("a single failure uses the singular wording, got %q", got)
	}
}

func TestAttachmentSentText_NotesPartialFailuresAndTruncation(t *testing.T) {
	mgr := NewManager(&model.WeChatConfig{BotToken: "tok"})

	got := mgr.attachmentSentText("abcdef1234567890", "会话", 3, downloadOutcome{failed: 1, truncated: true})
	if !strings.Contains(got, common.T("PushFilesCount", map[string]any{"Count": 3})) {
		t.Fatalf("the file count must be shown, got %q", got)
	}
	if !strings.Contains(got, common.T("PushPartialDownloadFailed", map[string]any{"Count": 1})) {
		t.Fatalf("partial failure must be shown, got %q", got)
	}
	if !strings.Contains(got, common.T("PushAttachmentsTruncated", map[string]any{
		"Max": common.AttachmentMaxFiles(), "Sent": 3,
	})) {
		t.Fatalf("truncation must be shown, got %q", got)
	}

	// A single file with no problems gets no extra lines.
	if got := mgr.attachmentSentText("abcdef1234567890", "会话", 1, downloadOutcome{}); strings.Contains(got, "\n\n") {
		t.Fatalf("a clean send needs no extra lines, got %q", got)
	}
}

func TestMediaOf_PicksTheDescriptorPerKind(t *testing.T) {
	img := &ilink.CDNMedia{FullURL: "img"}
	file := &ilink.CDNMedia{FullURL: "file"}
	video := &ilink.CDNMedia{FullURL: "video"}

	if got := mediaOf(ilink.MessageItem{Type: ilink.ItemImage, ImageItem: &ilink.ImageItem{Media: img}}); got != img {
		t.Fatal("image descriptor must be returned")
	}
	if got := mediaOf(ilink.MessageItem{Type: ilink.ItemFile, FileItem: &ilink.FileItem{Media: file}}); got != file {
		t.Fatal("file descriptor must be returned")
	}
	if got := mediaOf(ilink.MessageItem{Type: ilink.ItemVideo, VideoItem: &ilink.VideoItem{Media: video}}); got != video {
		t.Fatal("video descriptor must be returned")
	}
	// A malformed item (right type, missing payload) must not panic.
	if got := mediaOf(ilink.MessageItem{Type: ilink.ItemImage}); got != nil {
		t.Fatal("a missing payload yields no descriptor")
	}
	if got := mediaOf(ilink.MessageItem{Type: ilink.ItemText}); got != nil {
		t.Fatal("a text item carries no media")
	}
}
