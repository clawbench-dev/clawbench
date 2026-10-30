package wechat

import (
	"context"
	"errors"
	"strings"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/push/common"

	ilink "github.com/openilink/openilink-sdk-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// textOf extracts the text item from a recorded request body.
func textOf(body map[string]any) string {
	msg, ok := body["msg"].(map[string]any)
	if !ok {
		return ""
	}
	items, ok := msg["item_list"].([]any)
	if !ok {
		return ""
	}
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if ti, ok := item["text_item"].(map[string]any); ok {
			if s, ok := ti["text"].(string); ok {
				return s
			}
		}
	}
	return ""
}

func TestParseInbound_Table(t *testing.T) {
	media := func() *ilink.CDNMedia {
		return &ilink.CDNMedia{EncryptQueryParam: "q", AESKey: "k", EncryptType: 1}
	}

	tests := []struct {
		name      string
		items     []ilink.MessageItem
		wantText  string
		wantMedia []string
	}{
		{
			name:     "text only",
			items:    []ilink.MessageItem{{Type: ilink.ItemText, TextItem: &ilink.TextItem{Text: "@deadbeef hi"}}},
			wantText: "@deadbeef hi",
		},
		{
			name: "text plus image",
			items: []ilink.MessageItem{
				{Type: ilink.ItemText, TextItem: &ilink.TextItem{Text: "看这个"}},
				{Type: ilink.ItemImage, ImageItem: &ilink.ImageItem{Media: media()}},
			},
			wantText:  "看这个",
			wantMedia: []string{"image"},
		},
		{
			name:      "image only",
			items:     []ilink.MessageItem{{Type: ilink.ItemImage, ImageItem: &ilink.ImageItem{Media: media()}}},
			wantText:  "",
			wantMedia: []string{"image"},
		},
		{
			name: "file with name",
			items: []ilink.MessageItem{{
				Type:     ilink.ItemFile,
				FileItem: &ilink.FileItem{Media: media(), FileName: "report.pdf", Len: "2048"},
			}},
			wantMedia: []string{"file"},
		},
		{
			name: "voice with transcription becomes text",
			items: []ilink.MessageItem{{
				Type:      ilink.ItemVoice,
				VoiceItem: &ilink.VoiceItem{Text: "语音转文字"},
			}},
			wantText: "语音转文字",
		},
		{
			name: "voice without transcription yields nothing",
			items: []ilink.MessageItem{{
				Type:      ilink.ItemVoice,
				VoiceItem: &ilink.VoiceItem{Media: media()},
			}},
			wantText: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := ilink.WeixinMessage{MessageType: ilink.MsgTypeUser, FromUserID: "u", ItemList: tt.items}
			text, refs := parseInbound(msg)
			if text != tt.wantText {
				t.Errorf("text = %q, want %q", text, tt.wantText)
			}
			var kinds []string
			for _, r := range refs {
				kinds = append(kinds, r.kind)
			}
			if strings.Join(kinds, ",") != strings.Join(tt.wantMedia, ",") {
				t.Errorf("media = %v, want %v", kinds, tt.wantMedia)
			}
		})
	}
}

// TestParseInbound_QuoteGoesLast is the guard on the routing contract: a quoted
// message must not be prepended, because SessionCmdRe is ^-anchored and a prefix
// would push a leading "@{shortID}" off the start of the string.
func TestParseInbound_QuoteGoesLast(t *testing.T) {
	msg := ilink.WeixinMessage{
		MessageType: ilink.MsgTypeUser,
		ItemList: []ilink.MessageItem{{
			Type:     ilink.ItemText,
			TextItem: &ilink.TextItem{Text: "@deadbeef 继续"},
			RefMsg: &ilink.RefMessage{
				Title:       "上一轮",
				MessageItem: &ilink.MessageItem{Type: ilink.ItemText, TextItem: &ilink.TextItem{Text: "原始问题"}},
			},
		}},
	}

	text, _ := parseInbound(msg)
	if !strings.HasPrefix(text, "@deadbeef 继续") {
		t.Fatalf("the command must stay at the start, got %q", text)
	}
	if !strings.Contains(text, common.T("PushQuoteLabel")) || !strings.Contains(text, "原始问题") {
		t.Errorf("the quoted body must survive, got %q", text)
	}
	// And the routing parse must still find the target.
	if shortID, rest, ok := common.ParseSessionCommand(text); !ok || shortID != "deadbeef" {
		t.Errorf("ParseSessionCommand(%q) = (%q, %q, %v), want deadbeef", text, shortID, rest, ok)
	}
}

func TestParseInbound_FileWithoutName(t *testing.T) {
	msg := ilink.WeixinMessage{
		MessageType: ilink.MsgTypeUser,
		ItemList: []ilink.MessageItem{{
			Type:     ilink.ItemFile,
			FileItem: &ilink.FileItem{Media: &ilink.CDNMedia{EncryptQueryParam: "q"}, Len: "not-a-number"},
		}},
	}
	_, refs := parseInbound(msg)
	if len(refs) != 1 {
		t.Fatalf("expected 1 media ref, got %d", len(refs))
	}
	if refs[0].filename != "attachment" {
		t.Errorf("filename = %q, want the fallback", refs[0].filename)
	}
	if refs[0].declaredSize != 0 {
		t.Errorf("declaredSize = %d, want 0 for an unparseable length", refs[0].declaredSize)
	}
}

func TestOnInbound_IgnoresNon1to1(t *testing.T) {
	origDB := db
	defer func() { db = origDB }()
	mock := &mockDB{}
	db = mock

	mgr := NewManager(&model.WeChatConfig{BotToken: "tok"})
	SetManager(mgr)
	defer SetManager(nil)

	// A bot echo must not be processed (it would loop), and a group message has
	// no addressable outbound path.
	mgr.onInbound(context.Background(), ilink.WeixinMessage{
		MessageType: ilink.MsgTypeBot,
		FromUserID:  "u",
		ItemList:    []ilink.MessageItem{{Type: ilink.ItemText, TextItem: &ilink.TextItem{Text: "echo"}}},
	})
	mgr.onInbound(context.Background(), ilink.WeixinMessage{
		MessageType: ilink.MsgTypeUser,
		FromUserID:  "u",
		GroupID:     "g1",
		ItemList:    []ilink.MessageItem{{Type: ilink.ItemText, TextItem: &ilink.TextItem{Text: "hi"}}},
	})

	if n := mock.tokenCount(); n != 0 {
		t.Errorf("no subscriber should have been recorded, got %d", n)
	}
}

// TestOnInbound_PersistsContextToken is the core of the restart-survival
// requirement: every inbound message must write the token to the DB, because the
// SDK's cache dies with the process.
func TestOnInbound_PersistsContextToken(t *testing.T) {
	origDB := db
	defer func() { db = origDB }()
	mock := &mockDB{}
	db = mock

	mgr := NewManager(&model.WeChatConfig{BotToken: "tok"})
	SetManager(mgr)
	defer SetManager(nil)

	// An unsupported payload is the simplest path that still runs the
	// subscribe-and-persist block without needing a live session messenger.
	mgr.onInbound(context.Background(), ilink.WeixinMessage{
		MessageType:  ilink.MsgTypeUser,
		FromUserID:   "u1",
		SessionID:    "sess-1",
		ContextToken: "ctx-9",
		ItemList:     []ilink.MessageItem{{Type: ilink.ItemVideo}},
	})

	if got := mock.tokenFor("u1"); got != "ctx-9" {
		t.Errorf("context token = %q, want ctx-9", got)
	}
}

func TestDeliverToSession_SuccessMovesStickyTarget(t *testing.T) {
	origDB := db
	defer func() { db = origDB }()
	mock := &mockDB{}
	db = mock

	messenger := &mockSessionMessenger{
		sessions: []common.SessionInfo{{ID: "abcdef1234567890", Title: "T"}},
	}
	origMessenger := sessionMessenger
	defer func() { sessionMessenger = origMessenger }()
	sessionMessenger = messenger

	mgr := NewManager(&model.WeChatConfig{BotToken: "tok"})
	SetManager(mgr)
	defer SetManager(nil)

	mgr.deliverToSession(context.Background(), "u1", "ctx",
		common.Route{Kind: common.RouteToSession, ShortID: "abcdef12", Message: "hello"})

	if messenger.sentSessionID != "abcdef1234567890" {
		t.Errorf("sent to %q, want abcdef1234567890", messenger.sentSessionID)
	}
	if messenger.sentMessage != "hello" {
		t.Errorf("message = %q, want hello", messenger.sentMessage)
	}
	if mock.lastSessionID != "abcdef1234567890" {
		t.Errorf("sticky target = %q, want abcdef1234567890", mock.lastSessionID)
	}
}

// TestDeliverToSession_FailureKeepsStickyTarget pins the "failed send does not
// move the target" contract: otherwise the user's next prefix-less message would
// silently go into a session that just rejected it.
func TestDeliverToSession_FailureKeepsStickyTarget(t *testing.T) {
	origDB := db
	defer func() { db = origDB }()
	mock := &mockDB{lastSessionID: "previous-session"}
	db = mock

	messenger := &mockSessionMessenger{
		sessions: []common.SessionInfo{{ID: "abcdef1234567890", Title: "T"}},
		sendErr:  errSend,
	}
	origMessenger := sessionMessenger
	defer func() { sessionMessenger = origMessenger }()
	sessionMessenger = messenger

	mgr := NewManager(&model.WeChatConfig{BotToken: "tok"})
	SetManager(mgr)
	defer SetManager(nil)

	mgr.deliverToSession(context.Background(), "u1", "ctx",
		common.Route{Kind: common.RouteToSession, ShortID: "abcdef12", Message: "hello"})

	if mock.lastSessionID != "previous-session" {
		t.Errorf("sticky target moved to %q on a failed send", mock.lastSessionID)
	}
}

var errSend = &sendError{}

type sendError struct{}

func (e *sendError) Error() string { return "send failed" }

// TestOnInbound_VoiceWithoutTranscriptionReplies is the user-facing half of the
// "voice without transcription yields nothing" case: dropping the message
// silently would look identical to a parser bug, so the user must be told.
func TestOnInbound_VoiceWithoutTranscriptionReplies(t *testing.T) {
	origDB := db
	defer func() { db = origDB }()
	db = &mockDB{}

	mgr, bodies := newSendServer(t, `{"ret":0}`)
	SetManager(mgr)
	defer SetManager(nil)

	mgr.onInbound(context.Background(), ilink.WeixinMessage{
		MessageType:  ilink.MsgTypeUser,
		FromUserID:   "u1",
		ContextToken: "ctx",
		ItemList: []ilink.MessageItem{{
			Type:      ilink.ItemVoice,
			VoiceItem: &ilink.VoiceItem{Media: &ilink.CDNMedia{}},
		}},
	})

	require.Len(t, *bodies, 1, "the user must get a reply, not silence")
	assert.Contains(t, textOf((*bodies)[0]), "voice",
		"the reply must name the unsupported type")
}

func TestHandleIncomingText_NoTargetReplies(t *testing.T) {
	origDB := db
	defer func() { db = origDB }()
	db = &mockDB{}

	mgr, bodies := newSendServer(t, `{"ret":0}`)
	SetManager(mgr)
	defer SetManager(nil)

	mgr.handleIncomingText(context.Background(), "u1", "ctx", "随便一句话")

	if len(*bodies) != 1 {
		t.Fatalf("expected one reply, got %d", len(*bodies))
	}
	got := textOf((*bodies)[0])
	if got != common.T("PushNoSessionSelected") {
		t.Errorf("reply = %q, want the /ls hint", got)
	}
}

// ── /show /stop /new ────────────────────────────────────────────────────────
//
// These three commands used to be ordinary text on this channel: the router
// grew the routes and WeChat had no branch for them, so without handlers a
// message that used to reach the model would vanish instead.

// installMessenger swaps the package-level messenger for one test.
func installMessenger(t *testing.T, m *mockSessionMessenger) *mockDB {
	t.Helper()
	origDB := db
	t.Cleanup(func() { db = origDB })
	mock := &mockDB{}
	db = mock

	origMessenger := sessionMessenger
	t.Cleanup(func() { sessionMessenger = origMessenger })
	sessionMessenger = m
	return mock
}

func TestHandleShowLastReply_ReturnsAnswerWithRunningNote(t *testing.T) {
	messenger := &mockSessionMessenger{
		sessions:    []common.SessionInfo{{ID: "abcdef1234567890", Title: "T"}},
		running:     map[string]bool{"abcdef1234567890": true},
		lastReply:   "the full answer",
		lastReplyOK: true,
	}
	mock := installMessenger(t, messenger)
	mock.lastSessionID = "abcdef1234567890"

	mgr, bodies := newSendServer(t, `{"ret":0}`)
	SetManager(mgr)
	defer SetManager(nil)

	mgr.handleIncomingText(context.Background(), "u1", "ctx", "/show")

	require.Len(t, *bodies, 1)
	got := textOf((*bodies)[0])
	assert.Contains(t, got, "the full answer")
	// The text belongs to the PREVIOUS turn (the streaming row is excluded), so
	// reading it as the answer to the question just asked would be wrong.
	// WeChat is plain text: the template's `**上一条**` must arrive without the
	// asterisks, which is what stripMarkdownForWeChat is for.
	assert.Contains(t, got, stripMarkdownForWeChat(common.T("PushShowRunning")),
		"a running turn must be called out")
}

func TestHandleShowLastReply_EmptyReplySaysSo(t *testing.T) {
	messenger := &mockSessionMessenger{
		sessions:    []common.SessionInfo{{ID: "abcdef1234567890", Title: "T"}},
		lastReplyOK: false,
	}
	mock := installMessenger(t, messenger)
	mock.lastSessionID = "abcdef1234567890"

	mgr, bodies := newSendServer(t, `{"ret":0}`)
	SetManager(mgr)
	defer SetManager(nil)

	mgr.handleIncomingText(context.Background(), "u1", "ctx", "/show")

	require.Len(t, *bodies, 1)
	assert.Contains(t, textOf((*bodies)[0]), common.T("PushShowEmpty"))
}

// A single-character argument must not resolve to some arbitrary session: /show
// can read a conversation's whole answer, so the target needs the same shape
// check as @短ID.
func TestHandleShowLastReply_RejectsMalformedArgument(t *testing.T) {
	messenger := &mockSessionMessenger{
		sessions: []common.SessionInfo{{ID: "abcdef1234567890", Title: "T"}},
	}
	installMessenger(t, messenger)

	mgr, bodies := newSendServer(t, `{"ret":0}`)
	SetManager(mgr)
	defer SetManager(nil)

	mgr.handleIncomingText(context.Background(), "u1", "ctx", "/show a")

	require.Len(t, *bodies, 1)
	got := textOf((*bodies)[0])
	assert.NotContains(t, got, "the full answer")
	assert.Contains(t, got, common.T("PushInvalidSessionID", map[string]any{"ID": "a"}))
}

func TestHandleStopSession_ReportsClearedCount(t *testing.T) {
	messenger := &mockSessionMessenger{
		sessions:    []common.SessionInfo{{ID: "abcdef1234567890", Title: "T"}},
		stopRunning: true,
		stopCleared: 3,
	}
	mock := installMessenger(t, messenger)
	mock.lastSessionID = "abcdef1234567890"

	mgr, bodies := newSendServer(t, `{"ret":0}`)
	SetManager(mgr)
	defer SetManager(nil)

	mgr.handleIncomingText(context.Background(), "u1", "ctx", "/stop")

	require.Len(t, *bodies, 1)
	got := textOf((*bodies)[0])
	assert.Contains(t, got, "3", "the cleared count is the only evidence the queue was touched")
	want := common.T("PushStoppedRunning", map[string]any{
		common.TemplateLabel: common.FormatSessionLabel("abcdef1234567890", "T"),
		common.TemplateCount: 3,
	})
	assert.Equal(t, stripMarkdownForWeChat(want), got)
}

func TestHandleStopSession_IdleIsDistinctFromFailure(t *testing.T) {
	messenger := &mockSessionMessenger{
		sessions:    []common.SessionInfo{{ID: "abcdef1234567890", Title: "T"}},
		stopRunning: false,
	}
	mock := installMessenger(t, messenger)
	mock.lastSessionID = "abcdef1234567890"

	mgr, bodies := newSendServer(t, `{"ret":0}`)
	SetManager(mgr)
	defer SetManager(nil)

	mgr.handleIncomingText(context.Background(), "u1", "ctx", "/stop")

	require.Len(t, *bodies, 1)
	// Idle and failed must not look the same: this reply is the only signal that
	// nothing was running.
	want := common.T("PushStoppedIdle", map[string]any{
		common.TemplateLabel: common.FormatSessionLabel("abcdef1234567890", "T"),
		common.TemplateCount: 0,
	})
	assert.Equal(t, stripMarkdownForWeChat(want), textOf((*bodies)[0]))
}

func TestHandleNewSession_ListsProjectsWhenNoArgument(t *testing.T) {
	messenger := &mockSessionMessenger{
		projects: []common.ProjectRef{
			{Index: 1, Path: "/proj/a", Exists: true},
			{Index: 2, Path: "/proj/b", Exists: true},
		},
	}
	installMessenger(t, messenger)

	mgr, bodies := newSendServer(t, `{"ret":0}`)
	SetManager(mgr)
	defer SetManager(nil)

	mgr.handleIncomingText(context.Background(), "u1", "ctx", "/new")

	require.Len(t, *bodies, 1)
	got := textOf((*bodies)[0])
	assert.Contains(t, got, "/proj/a")
	assert.Contains(t, got, "/proj/b")
	assert.Empty(t, messenger.createPath, "a bare /new must not create anything")
}

func TestHandleNewSession_CreatesAndMovesStickyTarget(t *testing.T) {
	messenger := &mockSessionMessenger{
		projects:  []common.ProjectRef{{Index: 1, Path: "/proj/a", Exists: true}},
		createdID: "new-session-id",
	}
	mock := installMessenger(t, messenger)

	mgr, bodies := newSendServer(t, `{"ret":0}`)
	SetManager(mgr)
	defer SetManager(nil)

	mgr.handleIncomingText(context.Background(), "u1", "ctx", "/new 1")

	require.Len(t, *bodies, 1)
	assert.Equal(t, "/proj/a", messenger.createPath)
	assert.Equal(t, "new-session-id", mock.lastSessionID,
		"a successful create must become the sticky target")
}

func TestHandleNewSession_FailedCreateKeepsStickyTarget(t *testing.T) {
	messenger := &mockSessionMessenger{
		projects:  []common.ProjectRef{{Index: 1, Path: "/proj/a", Exists: true}},
		createErr: errors.New("create failed"),
	}
	mock := installMessenger(t, messenger)
	mock.lastSessionID = "previous"

	mgr, bodies := newSendServer(t, `{"ret":0}`)
	SetManager(mgr)
	defer SetManager(nil)

	mgr.handleIncomingText(context.Background(), "u1", "ctx", "/new 1")

	require.Len(t, *bodies, 1)
	assert.Equal(t, "previous", mock.lastSessionID,
		"a failed create must not move the target")
}

// A command carries no message text, so attachments would be delivered with an
// empty body and the command word would be lost. Refuse instead of sending.
func TestHandleIncomingWithMedia_CommandWithAttachmentIsRefused(t *testing.T) {
	messenger := &mockSessionMessenger{
		sessions: []common.SessionInfo{{ID: "abcdef1234567890", Title: "T"}},
	}
	installMessenger(t, messenger)

	mgr, bodies := newSendServer(t, `{"ret":0}`)
	SetManager(mgr)
	defer SetManager(nil)

	mgr.handleIncomingWithMedia(context.Background(), "u1", "ctx", "/new 1",
		[]mediaRef{{kind: "image"}})

	require.Len(t, *bodies, 1)
	assert.Contains(t, textOf((*bodies)[0]), "/new")
	assert.Empty(t, messenger.sentSessionID, "nothing may be sent to a session")
	assert.Empty(t, messenger.createPath, "the command must not run either")
}
