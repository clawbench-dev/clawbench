package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/push/common"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func TestExtractTextContent_Text(t *testing.T) {
	content := `{"text":"hello world"}`
	got := extractTextContent(content, "text")
	if got != "hello world" {
		t.Errorf("expected %q, got %q", "hello world", got)
	}
}

func TestExtractTextContent_Empty(t *testing.T) {
	got := extractTextContent("", "text")
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestExtractTextContent_InvalidJSON(t *testing.T) {
	got := extractTextContent("not json", "text")
	if got != "" {
		t.Errorf("expected empty for invalid JSON, got %q", got)
	}
}

func TestExtractTextContent_Post(t *testing.T) {
	content := `{"zh_cn":{"title":"My Title","content":[[{"tag":"text","text":"Hello "},{"tag":"text","text":"World"}],[{"tag":"text","text":"Second row"}]]}}`
	got := extractTextContent(content, "post")
	expected := "My Title\nHello World\nSecond row"
	if got != expected {
		t.Errorf("expected %q, got %q", expected, got)
	}
}

func TestExtractTextContent_PostNoTitle(t *testing.T) {
	content := `{"zh_cn":{"title":"","content":[[{"tag":"text","text":"Just text"}]]}}`
	got := extractTextContent(content, "post")
	expected := "Just text"
	if got != expected {
		t.Errorf("expected %q, got %q", expected, got)
	}
}

func TestExtractTextContent_PostNonTextElements(t *testing.T) {
	content := `{"zh_cn":{"title":"","content":[[{"tag":"a","text":"link"},{"tag":"text","text":"visible"}]]}}`
	got := extractTextContent(content, "post")
	expected := "visible"
	if got != expected {
		t.Errorf("expected %q, got %q", expected, got)
	}
}

func TestExtractTextContent_PostInvalidJSON(t *testing.T) {
	got := extractTextContent("not json", "post")
	if got != "" {
		t.Errorf("expected empty for invalid JSON, got %q", got)
	}
}

func TestExtractTextContent_OtherType(t *testing.T) {
	got := extractTextContent("{}", "image")
	if got != "" {
		t.Errorf("expected empty for unhandled type, got %q", got)
	}
}

func TestPtrStr(t *testing.T) {
	s := "hello"
	if got := ptrStr(&s); got != "hello" {
		t.Errorf("expected %q, got %q", "hello", got)
	}
	if got := ptrStr(nil); got != "" {
		t.Errorf("expected empty for nil, got %q", got)
	}
}

func TestOnMessageReceive_NilEvent(t *testing.T) {
	mgr := NewManager(&model.FeishuConfig{AppID: "test", AppSecret: "test"})
	event := &larkim.P2MessageReceiveV1{Event: nil}
	if err := mgr.onMessageReceive(context.TODO(), event); err != nil {
		t.Errorf("expected nil error for nil event data, got %v", err)
	}
}

func TestOnMessageReceive_NonP2P(t *testing.T) {
	mgr := NewManager(&model.FeishuConfig{AppID: "test", AppSecret: "test"})
	chatType := "group"
	senderType := "user"
	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				ChatType:    &chatType,
				ChatId:      strPtr("chat1"),
				Content:     strPtr(`{"text":"hello"}`),
				MessageType: strPtr("text"),
			},
			Sender: &larkim.EventSender{
				SenderId:   &larkim.UserId{OpenId: strPtr("ou_user1")},
				SenderType: &senderType,
			},
		},
	}
	if err := mgr.onMessageReceive(context.TODO(), event); err != nil {
		t.Errorf("expected nil error for non-p2p, got %v", err)
	}
}

func TestOnMessageReceive_AutoSubscribe(t *testing.T) {
	origDB := db
	defer func() { db = origDB }()

	var upsertedUser, upsertedSource string
	db = &mockDBWithCallback{
		upsertFn: func(userID, _, _, source string) error {
			upsertedUser = userID
			upsertedSource = source
			return nil
		},
	}

	mgr := NewManager(&model.FeishuConfig{AppID: "test", AppSecret: "test"})

	chatType := "p2p"
	senderType := "user"
	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				ChatType:    &chatType,
				ChatId:      strPtr("chat1"),
				Content:     strPtr(`{"text":"hello"}`),
				MessageType: strPtr("text"),
			},
			Sender: &larkim.EventSender{
				SenderId:   &larkim.UserId{OpenId: strPtr("ou_user1")},
				SenderType: &senderType,
			},
		},
	}

	_ = mgr.onMessageReceive(context.TODO(), event)
	if upsertedUser != "ou_user1" {
		t.Errorf("expected upsert for ou_user1, got %q", upsertedUser)
	}
	if upsertedSource != "stream" {
		t.Errorf("expected source 'stream', got %q", upsertedSource)
	}
}

func TestOnMessageReceive_SessionCommand(t *testing.T) {
	origDB := db
	defer func() { db = origDB }()
	db = &mockDB{}

	origSM := sessionMessenger
	defer func() { sessionMessenger = origSM }()
	sessionMessenger = &mockSessionMessenger{
		sessions: []common.SessionInfo{
			{ID: "a1b2c3d4-e5f6-7890-abcd-ef1234567890", Title: "Test Session", ProjectPath: "/project"},
		},
		running: map[string]bool{
			"a1b2c3d4-e5f6-7890-abcd-ef1234567890": true,
		},
	}

	mgr := NewManager(&model.FeishuConfig{AppID: "test", AppSecret: "test"})

	chatType := "p2p"
	senderType := "user"
	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				ChatType:    &chatType,
				ChatId:      strPtr("chat1"),
				Content:     strPtr(`{"text":"@a1b2c3d4 hello from feishu"}`),
				MessageType: strPtr("text"),
			},
			Sender: &larkim.EventSender{
				SenderId:   &larkim.UserId{OpenId: strPtr("ou_user1")},
				SenderType: &senderType,
			},
		},
	}

	_ = mgr.onMessageReceive(context.TODO(), event)
	// Should not panic; actual send will fail (no server), but the flow completes
}

func TestOnMessageReceive_PostMessage(t *testing.T) {
	origDB := db
	defer func() { db = origDB }()

	var upsertedUser string
	db = &mockDBWithCallback{
		upsertFn: func(userID, _, _, _ string) error {
			upsertedUser = userID
			return nil
		},
	}

	mgr := NewManager(&model.FeishuConfig{AppID: "test", AppSecret: "test"})

	chatType := "p2p"
	senderType := "user"
	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				ChatType:    &chatType,
				ChatId:      strPtr("chat1"),
				Content:     strPtr(`{"zh_cn":{"title":"Hello","content":[[{"tag":"text","text":"world"}]]}}`),
				MessageType: strPtr("post"),
			},
			Sender: &larkim.EventSender{
				SenderId:   &larkim.UserId{OpenId: strPtr("ou_user2")},
				SenderType: &senderType,
			},
		},
	}

	_ = mgr.onMessageReceive(context.TODO(), event)
	if upsertedUser != "ou_user2" {
		t.Errorf("expected auto-subscribe for ou_user2, got %q", upsertedUser)
	}
}

func strPtr(s string) *string { return &s }

// mockSessionMessenger implements common.SessionMessenger for testing.
type mockSessionMessenger struct {
	sessions []common.SessionInfo
	running  map[string]bool
	sendErr  error
	SendFn   func(sid, msg string, files []model.FileEntry) error
}

func (m *mockSessionMessenger) FindSessionsByPrefix(prefix string, runningOnly bool) ([]common.SessionInfo, error) {
	// Filter by prefix like the real implementation: returning every session
	// for any prefix makes any multi-session fixture look ambiguous.
	lower := strings.ToLower(prefix)
	var out []common.SessionInfo
	for _, s := range m.sessions {
		if len(s.ID) >= len(lower) && strings.ToLower(s.ID[:len(lower)]) == lower {
			if runningOnly && !m.running[s.ID] {
				continue
			}
			out = append(out, s)
		}
	}
	return out, nil
}

func (m *mockSessionMessenger) ListRecentSessions(limit int) ([]common.SessionInfo, error) {
	if limit > len(m.sessions) {
		return m.sessions, nil
	}
	return m.sessions[:limit], nil
}

func (m *mockSessionMessenger) IsSessionRunning(sessionID string) bool {
	return m.running[sessionID]
}

func (m *mockSessionMessenger) SendMessageToSession(sid, msg string, files []model.FileEntry) error {
	if m.SendFn != nil {
		return m.SendFn(sid, msg, files)
	}
	return m.sendErr
}

func (m *mockSessionMessenger) GetSessionInfo(sessionID string) (common.SessionInfo, error) {
	for _, s := range m.sessions {
		if s.ID == sessionID {
			return s, nil
		}
	}
	return common.SessionInfo{}, fmt.Errorf("session %s not found", sessionID)
}

// ============================================================================
// handleSessionList tests
// ============================================================================

func TestHandleSessionList_NilMessenger(t *testing.T) {
	origSM := sessionMessenger
	defer func() { sessionMessenger = origSM }()
	sessionMessenger = nil

	mgr := NewManager(&model.FeishuConfig{AppID: "test", AppSecret: "test"})
	// Should not panic
	mgr.handleSessionList(context.Background(), "ou_user1")
}

func TestHandleSessionList_EmptySessions(t *testing.T) {
	origSM := sessionMessenger
	defer func() { sessionMessenger = origSM }()
	sessionMessenger = &mockSessionMessenger{sessions: []common.SessionInfo{}}

	mgr := NewManager(&model.FeishuConfig{AppID: "test", AppSecret: "test"})
	// Should not panic — sends "暂无会话" message
	mgr.handleSessionList(context.Background(), "ou_user1")
}

func TestHandleSessionList_WithSessions(t *testing.T) {
	origSM := sessionMessenger
	defer func() { sessionMessenger = origSM }()
	sessionMessenger = &mockSessionMessenger{
		sessions: []common.SessionInfo{
			{ID: "a1b2c3d4-1111-1111-1111-111111111111", Title: "Test Session", ProjectPath: "/project"},
			{ID: "b2c3d4e5-2222-2222-2222-222222222222", Title: "", ProjectPath: "/other"},
		},
		running: map[string]bool{
			"a1b2c3d4-1111-1111-1111-111111111111": true,
		},
	}

	mgr := NewManager(&model.FeishuConfig{AppID: "test", AppSecret: "test"})
	// Should not panic — sends session list
	mgr.handleSessionList(context.Background(), "ou_user1")
}

func TestHandleSessionList_ListError(t *testing.T) {
	origSM := sessionMessenger
	defer func() { sessionMessenger = origSM }()
	sessionMessenger = &mockSessionMessengerListError{}

	mgr := NewManager(&model.FeishuConfig{AppID: "test", AppSecret: "test"})
	// Should not panic — sends error message
	mgr.handleSessionList(context.Background(), "ou_user1")
}

// mockSessionMessengerListError returns an error from ListRecentSessions.
type mockSessionMessengerListError struct{}

func (m *mockSessionMessengerListError) FindSessionsByPrefix(_ string, _ bool) ([]common.SessionInfo, error) {
	return nil, nil
}

func (m *mockSessionMessengerListError) ListRecentSessions(_ int) ([]common.SessionInfo, error) {
	return nil, fmt.Errorf("db error")
}
func (m *mockSessionMessengerListError) IsSessionRunning(_ string) bool { return false }
func (m *mockSessionMessengerListError) SendMessageToSession(_, _ string, _ []model.FileEntry) error {
	return nil
}

func (m *mockSessionMessengerListError) GetSessionInfo(string) (common.SessionInfo, error) {
	return common.SessionInfo{}, fmt.Errorf("not found")
}

// ============================================================================
// deliverToSession tests
// ============================================================================

func TestHandleSessionCommand_ResolveFails(t *testing.T) {
	origSM := sessionMessenger
	defer func() { sessionMessenger = origSM }()
	sessionMessenger = &mockSessionMessenger{sessions: []common.SessionInfo{}}

	mgr := NewManager(&model.FeishuConfig{AppID: "test", AppSecret: "test"})
	// Should not panic — sends error message
	mgr.deliverToSession(context.Background(), "ou_user1",
		common.Route{Kind: common.RouteToSession, ShortID: "deadbeef", Message: "hello"})
}

func TestHandleSessionCommand_NotRunning(t *testing.T) {
	origSM := sessionMessenger
	defer func() { sessionMessenger = origSM }()
	sessionMessenger = &mockSessionMessenger{
		sessions: []common.SessionInfo{
			{ID: "a1b2c3d4-1111-1111-1111-111111111111", Title: "Test", ProjectPath: "/proj"},
		},
		running: map[string]bool{},
	}

	mgr := NewManager(&model.FeishuConfig{AppID: "test", AppSecret: "test"})
	// Session not running → SendMessageToSession path
	mgr.deliverToSession(context.Background(), "ou_user1",
		common.Route{Kind: common.RouteToSession, ShortID: "a1b2c3d4", Message: "hello"})
}

func TestHandleSessionCommand_Running(t *testing.T) {
	origSM := sessionMessenger
	defer func() { sessionMessenger = origSM }()
	sessionMessenger = &mockSessionMessenger{
		sessions: []common.SessionInfo{
			{ID: "a1b2c3d4-1111-1111-1111-111111111111", Title: "Test", ProjectPath: "/proj"},
		},
		running: map[string]bool{
			"a1b2c3d4-1111-1111-1111-111111111111": true,
		},
	}

	mgr := NewManager(&model.FeishuConfig{AppID: "test", AppSecret: "test"})
	// Session running → EnqueueMessage path
	mgr.deliverToSession(context.Background(), "ou_user1",
		common.Route{Kind: common.RouteToSession, ShortID: "a1b2c3d4", Message: "hello"})
}

// ============================================================================
// onMessageReceive edge cases
// ============================================================================

func TestOnMessageReceive_NilMessage(t *testing.T) {
	mgr := NewManager(&model.FeishuConfig{AppID: "test", AppSecret: "test"})
	senderType := "user"
	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Message: nil,
			Sender: &larkim.EventSender{
				SenderId:   &larkim.UserId{OpenId: strPtr("ou_user1")},
				SenderType: &senderType,
			},
		},
	}
	if err := mgr.onMessageReceive(context.TODO(), event); err != nil {
		t.Errorf("expected nil error for nil message, got %v", err)
	}
}

func TestOnMessageReceive_NilSender(t *testing.T) {
	mgr := NewManager(&model.FeishuConfig{AppID: "test", AppSecret: "test"})
	chatType := "p2p"
	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				ChatType:    &chatType,
				Content:     strPtr(`{"text":"hello"}`),
				MessageType: strPtr("text"),
			},
			Sender: nil,
		},
	}
	if err := mgr.onMessageReceive(context.TODO(), event); err != nil {
		t.Errorf("expected nil error for nil sender, got %v", err)
	}
}

func TestOnMessageReceive_EmptyOpenID(t *testing.T) {
	mgr := NewManager(&model.FeishuConfig{AppID: "test", AppSecret: "test"})
	chatType := "p2p"
	senderType := "user"
	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				ChatType:    &chatType,
				Content:     strPtr(`{"text":"hello"}`),
				MessageType: strPtr("text"),
			},
			Sender: &larkim.EventSender{
				SenderId:   &larkim.UserId{OpenId: strPtr("")},
				SenderType: &senderType,
			},
		},
	}
	if err := mgr.onMessageReceive(context.TODO(), event); err != nil {
		t.Errorf("expected nil error for empty open_id, got %v", err)
	}
}

func TestOnMessageReceive_NilSenderId(t *testing.T) {
	mgr := NewManager(&model.FeishuConfig{AppID: "test", AppSecret: "test"})
	chatType := "p2p"
	senderType := "user"
	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				ChatType:    &chatType,
				Content:     strPtr(`{"text":"hello"}`),
				MessageType: strPtr("text"),
			},
			Sender: &larkim.EventSender{
				SenderId:   nil,
				SenderType: &senderType,
			},
		},
	}
	if err := mgr.onMessageReceive(context.TODO(), event); err != nil {
		t.Errorf("expected nil error for nil sender id, got %v", err)
	}
}

func TestOnMessageReceive_NoCommand_NoStickyTarget(t *testing.T) {
	origDB := db
	defer func() { db = origDB }()
	// mockDB reports no recorded target, so a plain message cannot be routed.
	db = &mockDB{}

	origSM := sessionMessenger
	defer func() { sessionMessenger = origSM }()
	sent := false
	sessionMessenger = &mockSessionMessenger{
		sessions: []common.SessionInfo{
			{ID: "abc12345-1111-1111-1111-111111111111", Title: "Recent Session", ProjectPath: "/proj"},
		},
		SendFn: func(string, string, []model.FileEntry) error {
			sent = true
			return nil
		},
	}

	mgr := NewManager(&model.FeishuConfig{AppID: "test", AppSecret: "test"})

	event := textEvent("just a message")
	_ = mgr.onMessageReceive(context.TODO(), event)

	if sent {
		t.Error("a message with no sticky target must not be sent to a session")
	}
}

// --- Sticky session routing ---

func textEvent(text string) *larkim.P2MessageReceiveV1 {
	chatType := "p2p"
	senderType := "user"
	return &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				ChatType:    &chatType,
				ChatId:      strPtr("chat1"),
				MessageId:   strPtr("om_msg_1"),
				Content:     strPtr(`{"text":` + strconv.Quote(text) + `}`),
				MessageType: strPtr("text"),
			},
			Sender: &larkim.EventSender{
				SenderId:   &larkim.UserId{OpenId: strPtr("ou_user1")},
				SenderType: &senderType,
			},
		},
	}
}

// stickyTestEnv wires a mock DB with a recorded target and a capturing messenger.
func stickyTestEnv(t *testing.T, sticky string, sessions []common.SessionInfo) (*Manager, *[]string) {
	t.Helper()

	setCalls := new([]string)
	origDB := db
	t.Cleanup(func() { db = origDB })
	db = &mockDBWithCallback{
		upsertFn:  func(_, _, _, _ string) error { return nil },
		getLastFn: func(string) (string, error) { return sticky, nil },
		setLastFn: func(_, sessionID string) error {
			*setCalls = append(*setCalls, sessionID)
			return nil
		},
	}

	origSM := sessionMessenger
	t.Cleanup(func() { sessionMessenger = origSM })
	sessionMessenger = &mockSessionMessenger{sessions: sessions}

	return NewManager(&model.FeishuConfig{AppID: "test", AppSecret: "test"}), setCalls
}

var stickySessions = []common.SessionInfo{
	{ID: "abc12345-1111-1111-1111-111111111111", Title: "Sticky Session", ProjectPath: "/proj"},
	{ID: "deadbeef-2222-2222-2222-222222222222", Title: "Other", ProjectPath: "/proj"},
}

// A plain message with a recorded target must go to that session.
func TestOnMessageReceive_PlainTextGoesToStickySession(t *testing.T) {
	mgr, _ := stickyTestEnv(t, "abc12345-1111-1111-1111-111111111111", stickySessions)

	var sentID, sentMsg string
	sessionMessenger.(*mockSessionMessenger).SendFn = func(sid, msg string, _ []model.FileEntry) error {
		sentID, sentMsg = sid, msg
		return nil
	}

	_ = mgr.onMessageReceive(context.TODO(), textEvent("run the tests"))

	if sentID != "abc12345-1111-1111-1111-111111111111" {
		t.Errorf("sent to %q, want the sticky session", sentID)
	}
	if sentMsg != "run the tests" {
		t.Errorf("sent message = %q, want 'run the tests'", sentMsg)
	}
}

// An explicit "@{shortID}" overrides the sticky target and becomes the new one.
func TestOnMessageReceive_ExplicitTargetOverridesAndUpdatesSticky(t *testing.T) {
	mgr, setCalls := stickyTestEnv(t, "abc12345-1111-1111-1111-111111111111", stickySessions)

	var sentID string
	sessionMessenger.(*mockSessionMessenger).SendFn = func(sid, _ string, _ []model.FileEntry) error {
		sentID = sid
		return nil
	}

	_ = mgr.onMessageReceive(context.TODO(), textEvent("@deadbeef hi"))

	if sentID != "deadbeef-2222-2222-2222-222222222222" {
		t.Errorf("sent to %q, want the explicitly named session", sentID)
	}
	if len(*setCalls) != 1 || (*setCalls)[0] != "deadbeef-2222-2222-2222-222222222222" {
		t.Errorf("sticky target updates = %v, want one write of the explicit session", *setCalls)
	}
}

// A failed send must not move the sticky target.
func TestOnMessageReceive_FailedSendDoesNotUpdateSticky(t *testing.T) {
	mgr, setCalls := stickyTestEnv(t, "abc12345-1111-1111-1111-111111111111", stickySessions)

	sessionMessenger.(*mockSessionMessenger).SendFn = func(string, string, []model.FileEntry) error {
		return fmt.Errorf("session gone")
	}

	_ = mgr.onMessageReceive(context.TODO(), textEvent("@deadbeef hi"))

	if len(*setCalls) != 0 {
		t.Errorf("sticky target must not move on a failed send, got %v", *setCalls)
	}
}

// A file message downloads and sends an attachment-only message (empty text)
// to the sticky session.
func TestOnMessageReceive_FileGoesToStickySessionAsAttachment(t *testing.T) {
	mediaTestServer(t, "attachment-bytes")

	project := t.TempDir()
	sessions := []common.SessionInfo{
		{ID: "abc12345-1111-1111-1111-111111111111", Title: "Sticky Session", ProjectPath: project},
	}
	mgr, _ := stickyTestEnv(t, "abc12345-1111-1111-1111-111111111111", sessions)
	mgr.cachedToken = "test-tenant-token"
	mgr.cachedExp = time.Now().Add(2 * time.Hour)

	sentCh := make(chan struct {
		sid   string
		msg   string
		files []model.FileEntry
	}, 1)
	sessionMessenger.(*mockSessionMessenger).SendFn = func(sid, msg string, files []model.FileEntry) error {
		sentCh <- struct {
			sid   string
			msg   string
			files []model.FileEntry
		}{sid, msg, files}
		return nil
	}

	chatType := "p2p"
	senderType := "user"
	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				ChatType:    &chatType,
				ChatId:      strPtr("chat1"),
				MessageId:   strPtr("om_msg_1"),
				MessageType: strPtr("file"),
				Content:     strPtr(`{"file_key":"file_abc","file_name":"报告.pdf"}`),
			},
			Sender: &larkim.EventSender{
				SenderId:   &larkim.UserId{OpenId: strPtr("ou_user1")},
				SenderType: &senderType,
			},
		},
	}

	if err := mgr.onMessageReceive(context.TODO(), event); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case got := <-sentCh:
		if got.sid != "abc12345-1111-1111-1111-111111111111" {
			t.Errorf("sent to %q, want the sticky session", got.sid)
		}
		if got.msg != "" {
			t.Errorf("attachment message text = %q, want empty", got.msg)
		}
		if len(got.files) != 1 {
			t.Fatalf("files = %d, want 1", len(got.files))
		}
		if got.files[0].Path != ".clawbench/uploads/报告.pdf" {
			t.Errorf("file path = %q", got.files[0].Path)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the attachment to be sent")
	}
}

// An image message is downloaded with the image resource type and sent as an
// attachment to the sticky session.
func TestOnMessageReceive_ImageGoesToStickySessionAsAttachment(t *testing.T) {
	mediaTestServer(t, "png-bytes")

	project := t.TempDir()
	sessions := []common.SessionInfo{
		{ID: "abc12345-1111-1111-1111-111111111111", Title: "Sticky Session", ProjectPath: project},
	}
	mgr, _ := stickyTestEnv(t, "abc12345-1111-1111-1111-111111111111", sessions)
	mgr.cachedToken = "test-tenant-token"
	mgr.cachedExp = time.Now().Add(2 * time.Hour)

	sentCh := make(chan []model.FileEntry, 1)
	sessionMessenger.(*mockSessionMessenger).SendFn = func(_ string, _ string, files []model.FileEntry) error {
		sentCh <- files
		return nil
	}

	chatType := "p2p"
	senderType := "user"
	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				ChatType:    &chatType,
				ChatId:      strPtr("chat1"),
				MessageId:   strPtr("om_msg_1"),
				MessageType: strPtr("image"),
				Content:     strPtr(`{"image_key":"img_xyz"}`),
			},
			Sender: &larkim.EventSender{
				SenderId:   &larkim.UserId{OpenId: strPtr("ou_user1")},
				SenderType: &senderType,
			},
		},
	}

	if err := mgr.onMessageReceive(context.TODO(), event); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case files := <-sentCh:
		if len(files) != 1 {
			t.Fatalf("files = %d, want 1", len(files))
		}
		if files[0].Path != ".clawbench/uploads/image.png" {
			t.Errorf("file path = %q, want .clawbench/uploads/image.png", files[0].Path)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the image to be sent")
	}
}

// A file message with no sticky target must not be downloaded or sent.
func TestOnMessageReceive_FileWithNoStickyTarget(t *testing.T) {
	mediaTestServer(t, "x")
	mgr, _ := stickyTestEnv(t, "", nil)

	sent := false
	sessionMessenger.(*mockSessionMessenger).SendFn = func(string, string, []model.FileEntry) error {
		sent = true
		return nil
	}

	chatType := "p2p"
	senderType := "user"
	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				ChatType:    &chatType,
				ChatId:      strPtr("chat1"),
				MessageId:   strPtr("om_msg_1"),
				MessageType: strPtr("file"),
				Content:     strPtr(`{"file_key":"file_abc","file_name":"a.txt"}`),
			},
			Sender: &larkim.EventSender{
				SenderId:   &larkim.UserId{OpenId: strPtr("ou_user1")},
				SenderType: &senderType,
			},
		},
	}

	_ = mgr.onMessageReceive(context.TODO(), event)

	// Give the (should-be-absent) goroutine a chance to run.
	time.Sleep(100 * time.Millisecond)
	if sent {
		t.Error("a file with no target session must not be sent")
	}
}

// fileEvent builds a p2p file event carrying the given content JSON.
func fileEvent(content string) *larkim.P2MessageReceiveV1 {
	chatType := "p2p"
	senderType := "user"
	return &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				ChatType:    &chatType,
				ChatId:      strPtr("chat1"),
				MessageId:   strPtr("om_msg_1"),
				MessageType: strPtr("file"),
				Content:     strPtr(content),
			},
			Sender: &larkim.EventSender{
				SenderId:   &larkim.UserId{OpenId: strPtr("ou_user1")},
				SenderType: &senderType,
			},
		},
	}
}

// replyCapture points feishuMessageURL at a stub that records the card content
// of the next SendPostMessage call, so tests can assert on what the user was
// actually told. feishuMessageURL is package state, so it is restored on cleanup.
//
// The handler runs on an httptest server goroutine while the test goroutine
// polls, so the captured value is guarded by a mutex.
func replyCapture(t *testing.T) *replyBox {
	t.Helper()
	box := new(replyBox)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		box.set(fmt.Sprint(body["content"]))
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintln(w, `{"code":0,"msg":"ok"}`)
	}))
	t.Cleanup(srv.Close)

	origURL := feishuMessageURL
	feishuMessageURL = srv.URL
	t.Cleanup(func() { feishuMessageURL = origURL })

	return box
}

// replyBox is a mutex-guarded string written by the stub server goroutine and
// read by the test goroutine.
type replyBox struct {
	mu  sync.Mutex
	val string
}

func (b *replyBox) set(s string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.val = s
}

func (b *replyBox) get() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.val
}

// A file whose sticky session has since been archived/deleted must tell the
// user to re-pick rather than downloading into nowhere.
func TestOnMessageReceive_FileWithUnavailableStickySession(t *testing.T) {
	mediaTestServer(t, "x")
	reply := replyCapture(t)

	mgr, _ := stickyTestEnv(t, "deadbeef00000000", stickySessions)
	mgr.cachedToken = "test-tenant-token"
	mgr.cachedExp = time.Now().Add(2 * time.Hour)

	sent := false
	sessionMessenger.(*mockSessionMessenger).SendFn = func(string, string, []model.FileEntry) error {
		sent = true
		return nil
	}

	_ = mgr.onMessageReceive(context.TODO(), fileEvent(`{"file_key":"file_abc","file_name":"a.txt"}`))

	got := waitForReplyText(t, reply)
	if sent {
		t.Error("a file whose sticky session is gone must not be sent anywhere")
	}
	if !strings.Contains(got, "/ls") {
		t.Errorf("reply should point at /ls, got %s", got)
	}
}

// A media message arriving with no session messenger wired up must tell the
// user rather than failing silently.
func TestOnMessageReceive_FileWithNoMessenger(t *testing.T) {
	mediaTestServer(t, "x")
	reply := replyCapture(t)

	mgr, _ := stickyTestEnv(t, "abc12345-1111-1111-1111-111111111111", stickySessions)
	mgr.cachedToken = "test-tenant-token"
	mgr.cachedExp = time.Now().Add(2 * time.Hour)
	sessionMessenger = nil

	if err := mgr.onMessageReceive(context.TODO(), fileEvent(`{"file_key":"file_abc","file_name":"a.txt"}`)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := waitForReplyText(t, reply)
	if !strings.Contains(got, "会话服务不可用") {
		t.Errorf("reply should say the session service is unavailable, got %s", got)
	}
}

// A download failure must be reported to the user and must not send an
// attachment to the session.
func TestOnMessageReceive_FileDownloadFailure(t *testing.T) {
	// Point the resource endpoint at a failing server.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"code":1,"msg":"denied"}`))
	}))
	t.Cleanup(srv.Close)
	origBase := feishuOpenBaseURL
	feishuOpenBaseURL = srv.URL
	t.Cleanup(func() { feishuOpenBaseURL = origBase })

	reply := replyCapture(t)
	sessions := []common.SessionInfo{
		{ID: "abc12345-1111-1111-1111-111111111111", Title: "Sticky Session", ProjectPath: t.TempDir()},
	}
	mgr, _ := stickyTestEnv(t, "abc12345-1111-1111-1111-111111111111", sessions)
	mgr.cachedToken = "test-tenant-token"
	mgr.cachedExp = time.Now().Add(2 * time.Hour)

	sent := false
	sessionMessenger.(*mockSessionMessenger).SendFn = func(string, string, []model.FileEntry) error {
		sent = true
		return nil
	}

	_ = mgr.onMessageReceive(context.TODO(), fileEvent(`{"file_key":"file_abc","file_name":"a.txt"}`))

	got := waitForReplyText(t, reply)
	if sent {
		t.Error("a failed download must not send an attachment")
	}
	if !strings.Contains(got, "下载失败") {
		t.Errorf("reply should report the download failure, got %s", got)
	}
}

// An oversized file must be reported with the size-specific message and must
// not reach the session.
func TestOnMessageReceive_FileTooLarge(t *testing.T) {
	orig := model.UploadMaxSizeMB
	model.UploadMaxSizeMB = 1
	t.Cleanup(func() { model.UploadMaxSizeMB = orig })

	mediaTestServer(t, strings.Repeat("a", 2*1024*1024))
	reply := replyCapture(t)
	sessions := []common.SessionInfo{
		{ID: "abc12345-1111-1111-1111-111111111111", Title: "Sticky Session", ProjectPath: t.TempDir()},
	}
	mgr, _ := stickyTestEnv(t, "abc12345-1111-1111-1111-111111111111", sessions)
	mgr.cachedToken = "test-tenant-token"
	mgr.cachedExp = time.Now().Add(2 * time.Hour)

	sent := false
	sessionMessenger.(*mockSessionMessenger).SendFn = func(string, string, []model.FileEntry) error {
		sent = true
		return nil
	}

	_ = mgr.onMessageReceive(context.TODO(), fileEvent(`{"file_key":"file_abc","file_name":"big.bin"}`))

	got := waitForReplyText(t, reply)
	if sent {
		t.Error("an oversized attachment must not be sent")
	}
	if !strings.Contains(got, "大小上限") {
		t.Errorf("reply should mention the size limit, got %s", got)
	}
}

// A send failure after a successful download must be reported and must not
// move the sticky target.
func TestOnMessageReceive_FileSendFailure(t *testing.T) {
	mediaTestServer(t, "bytes")
	reply := replyCapture(t)
	sessions := []common.SessionInfo{
		{ID: "abc12345-1111-1111-1111-111111111111", Title: "Sticky Session", ProjectPath: t.TempDir()},
	}
	mgr, setCalls := stickyTestEnv(t, "abc12345-1111-1111-1111-111111111111", sessions)
	mgr.cachedToken = "test-tenant-token"
	mgr.cachedExp = time.Now().Add(2 * time.Hour)

	sessionMessenger.(*mockSessionMessenger).SendFn = func(string, string, []model.FileEntry) error {
		return fmt.Errorf("session busy")
	}

	_ = mgr.onMessageReceive(context.TODO(), fileEvent(`{"file_key":"file_abc","file_name":"a.txt"}`))

	got := waitForReplyText(t, reply)
	if !strings.Contains(got, "发送文件失败") {
		t.Errorf("reply should report the send failure, got %s", got)
	}
	if len(*setCalls) != 0 {
		t.Errorf("a failed send must not update the sticky target, got %v", *setCalls)
	}
}

// waitForReplyText blocks until the async media handler has sent its reply.
func waitForReplyText(t *testing.T, box *replyBox) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if s := box.get(); s != "" {
			return s
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for a reply")
	return ""
}
