package feishu

import (
	"context"
	"fmt"
	"testing"

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
	enqueued []string
}

func (m *mockSessionMessenger) FindSessionsByPrefix(_ string, _ bool) ([]common.SessionInfo, error) {
	return m.sessions, nil
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

func (m *mockSessionMessenger) EnqueueMessage(sessionID, message string) error {
	m.enqueued = append(m.enqueued, sessionID+":"+message)
	return nil
}

func (m *mockSessionMessenger) ClearQueue(_ string) {}

func (m *mockSessionMessenger) SendMessageToSession(_, _ string) error {
	return nil
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
func (m *mockSessionMessengerListError) EnqueueMessage(_, _ string) error {
	return nil
}
func (m *mockSessionMessengerListError) ClearQueue(_ string) {}
func (m *mockSessionMessengerListError) SendMessageToSession(_, _ string) error {
	return nil
}

// ============================================================================
// handleSessionCommand tests
// ============================================================================

func TestHandleSessionCommand_ResolveFails(t *testing.T) {
	origSM := sessionMessenger
	defer func() { sessionMessenger = origSM }()
	sessionMessenger = &mockSessionMessenger{sessions: []common.SessionInfo{}}

	mgr := NewManager(&model.FeishuConfig{AppID: "test", AppSecret: "test"})
	// Should not panic — sends error message
	mgr.handleSessionCommand(context.Background(), "ou_user1", "deadbeef", "hello")
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
	mgr.handleSessionCommand(context.Background(), "ou_user1", "a1b2c3d4", "hello")
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
	mgr.handleSessionCommand(context.Background(), "ou_user1", "a1b2c3d4", "hello")
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

func TestOnMessageReceive_NoCommand_SessionList(t *testing.T) {
	origDB := db
	defer func() { db = origDB }()
	db = &mockDB{}

	origSM := sessionMessenger
	defer func() { sessionMessenger = origSM }()
	sessionMessenger = &mockSessionMessenger{
		sessions: []common.SessionInfo{
			{ID: "abc12345-1111-1111-1111-111111111111", Title: "Recent Session", ProjectPath: "/proj"},
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
				Content:     strPtr(`{"text":"just a message"}`),
				MessageType: strPtr("text"),
			},
			Sender: &larkim.EventSender{
				SenderId:   &larkim.UserId{OpenId: strPtr("ou_user1")},
				SenderType: &senderType,
			},
		},
	}

	_ = mgr.onMessageReceive(context.TODO(), event)
	// Should not panic; should call handleSessionList since no @ prefix
}
