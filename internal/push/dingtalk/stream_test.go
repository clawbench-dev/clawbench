package dingtalk

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/push/common"

	"github.com/open-dingtalk/dingtalk-stream-sdk-go/chatbot"
)

func TestOnChatBotMessage_SingleChat_AutoSubscribe(t *testing.T) {
	origDB := db
	defer func() { db = origDB }()

	upsertCalled := false
	db = &mockDBWithCallback{
		upsertFn: func(userID, conversationID, userName, source string) error {
			upsertCalled = true
			if userID != "staff123" {
				t.Errorf("expected userID staff123, got %s", userID)
			}
			if conversationID != "conv1" {
				t.Errorf("expected conversationID conv1, got %s", conversationID)
			}
			if userName != "TestUser" {
				t.Errorf("expected userName TestUser, got %s", userName)
			}
			if source != "stream" {
				t.Errorf("expected source stream, got %s", source)
			}
			return nil
		},
	}

	// Set up a fake webhook server for the reply
	replyReceived := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		replyReceived = true
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	mgr := &Manager{}
	data := &chatbot.BotCallbackDataModel{
		ConversationType: "1",
		SenderId:         "sender_encrypted",
		SenderStaffId:    "staff123",
		SenderNick:       "TestUser",
		ConversationId:   "conv1",
		SessionWebhook:   server.URL,
		Text:             chatbot.BotCallbackDataTextModel{Content: "hello"},
	}

	result, err := mgr.onChatBotMessage(context.Background(), data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty byte slice result, got %q", string(result))
	}
	if !upsertCalled {
		t.Error("expected UpsertSubscriber to be called")
	}
	if !replyReceived {
		t.Error("expected reply to be sent via webhook")
	}
}

func TestOnChatBotMessage_GroupChat_Ignored(t *testing.T) {
	origDB := db
	defer func() { db = origDB }()

	upsertCalled := false
	db = &mockDBWithCallback{
		upsertFn: func(_, _, _, _ string) error {
			upsertCalled = true
			return nil
		},
	}

	mgr := &Manager{}
	data := &chatbot.BotCallbackDataModel{
		ConversationType: "2", // group chat
		SenderId:         "sender_encrypted",
		SenderStaffId:    "staff123",
		SenderNick:       "TestUser",
		ConversationId:   "conv1",
		Text:             chatbot.BotCallbackDataTextModel{Content: "hello"},
	}

	result, err := mgr.onChatBotMessage(context.Background(), data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty result for group chat, got %q", string(result))
	}
	if upsertCalled {
		t.Error("UpsertSubscriber should not be called for group chat")
	}
}

func TestOnChatBotMessage_EmptySenderStaffId_Fallback(t *testing.T) {
	origDB := db
	defer func() { db = origDB }()

	var capturedUserID string
	db = &mockDBWithCallback{
		upsertFn: func(userID, _, _, _ string) error {
			capturedUserID = userID
			return nil
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	mgr := &Manager{}
	data := &chatbot.BotCallbackDataModel{
		ConversationType: "1",
		SenderId:         "fallback_sender_id",
		SenderStaffId:    "", // empty — should fallback to SenderId
		SenderNick:       "TestUser",
		ConversationId:   "conv1",
		SessionWebhook:   server.URL,
		Text:             chatbot.BotCallbackDataTextModel{Content: "hello"},
	}

	_, err := mgr.onChatBotMessage(context.Background(), data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedUserID != "fallback_sender_id" {
		t.Errorf("expected fallback to SenderId 'fallback_sender_id', got %q", capturedUserID)
	}
}

func TestOnChatBotMessage_NilDB(t *testing.T) {
	origDB := db
	defer func() { db = origDB }()

	db = nil

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	mgr := &Manager{}
	data := &chatbot.BotCallbackDataModel{
		ConversationType: "1",
		SenderId:         "sender1",
		SenderStaffId:    "staff1",
		SenderNick:       "TestUser",
		ConversationId:   "conv1",
		SessionWebhook:   server.URL,
		Text:             chatbot.BotCallbackDataTextModel{Content: "hello"},
	}

	// Should not panic when db is nil
	result, err := mgr.onChatBotMessage(context.Background(), data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty result, got %q", string(result))
	}
}

func TestOnChatBotMessage_UpsertError(t *testing.T) {
	origDB := db
	defer func() { db = origDB }()

	db = &mockDBWithCallback{
		upsertFn: func(_, _, _, _ string) error {
			return errTestFailure
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	mgr := &Manager{}
	data := &chatbot.BotCallbackDataModel{
		ConversationType: "1",
		SenderId:         "sender1",
		SenderStaffId:    "staff1",
		SenderNick:       "TestUser",
		ConversationId:   "conv1",
		SessionWebhook:   server.URL,
		Text:             chatbot.BotCallbackDataTextModel{Content: "hello"},
	}

	// Should not panic when upsert fails
	result, err := mgr.onChatBotMessage(context.Background(), data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty result, got %q", string(result))
	}
}

func TestOnChatBotMessage_ReplyFailure(t *testing.T) {
	origDB := db
	defer func() { db = origDB }()

	db = &mockDBWithCallback{
		upsertFn: func(_, _, _, _ string) error { return nil },
	}

	mgr := &Manager{}
	data := &chatbot.BotCallbackDataModel{
		ConversationType: "1",
		SenderId:         "sender1",
		SenderStaffId:    "staff1",
		SenderNick:       "TestUser",
		ConversationId:   "conv1",
		SessionWebhook:   "http://127.0.0.1:1/nonexistent", // will fail to connect
		Text:             chatbot.BotCallbackDataTextModel{Content: "hello"},
	}

	// Should not panic when reply fails
	result, err := mgr.onChatBotMessage(context.Background(), data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty result, got %q", string(result))
	}
}

// mockDBWithCallback is a mock DingtalkDB with optional callback functions.
type mockDBWithCallback struct {
	mergeFn   func(users []string)
	getFn     func() ([]common.SubscriberInfo, error)
	upsertFn  func(userID, conversationID, userName, source string) error
	deleteFn  func(userID string) error
	getLastFn func(userID string) (string, error)
	setLastFn func(userID, sessionID string) error
}

func (m *mockDBWithCallback) MergeConfigSubscribers(users []string) {
	if m.mergeFn != nil {
		m.mergeFn(users)
	}
}

func (m *mockDBWithCallback) GetSubscribers() ([]common.SubscriberInfo, error) {
	if m.getFn != nil {
		return m.getFn()
	}
	return nil, nil
}

func (m *mockDBWithCallback) UpsertSubscriber(userID, conversationID, userName, source string) error {
	if m.upsertFn != nil {
		return m.upsertFn(userID, conversationID, userName, source)
	}
	return nil
}

func (m *mockDBWithCallback) DeleteSubscriber(userID string) error {
	if m.deleteFn != nil {
		return m.deleteFn(userID)
	}
	return nil
}

func (m *mockDBWithCallback) GetLastSessionID(userID string) (string, error) {
	if m.getLastFn != nil {
		return m.getLastFn(userID)
	}
	return "", nil
}

func (m *mockDBWithCallback) SetLastSessionID(userID, sessionID string) error {
	if m.setLastFn != nil {
		return m.setLastFn(userID, sessionID)
	}
	return nil
}

var errTestFailure = fmt.Errorf("test failure")

func TestOnChatBotMessage_SessionCommand_NotFound(t *testing.T) {
	origDB := db
	defer func() { db = origDB }()
	db = &mockDBWithCallback{
		upsertFn: func(_, _, _, _ string) error { return nil },
	}

	origMessenger := sessionMessenger
	defer func() { sessionMessenger = origMessenger }()

	sessionMessenger = &mockSessionMessenger{
		runningSessions: []common.SessionInfo{},
		allSessions:     []common.SessionInfo{},
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

func TestOnChatBotMessage_SessionCommand_AutoSubscribe(t *testing.T) {
	origDB := db
	defer func() { db = origDB }()

	var upsertedID, upsertedSource string
	db = &mockDBWithCallback{
		upsertFn: func(userID, _, _, source string) error {
			upsertedID = userID
			upsertedSource = source
			return nil
		},
	}

	origMessenger := sessionMessenger
	defer func() { sessionMessenger = origMessenger }()

	sessionMessenger = &mockSessionMessenger{
		runningSessions: []common.SessionInfo{},
		allSessions:     []common.SessionInfo{},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	// User should be auto-subscribed even though session command failed
	if upsertedID != "staff123" {
		t.Errorf("expected auto-subscribe with staff123, got %q", upsertedID)
	}
	if upsertedSource != "stream" {
		t.Errorf("expected source 'stream', got %q", upsertedSource)
	}
}

func TestOnChatBotMessage_SessionCommand_EndedSession(t *testing.T) {
	origDB := db
	defer func() { db = origDB }()
	db = &mockDBWithCallback{
		upsertFn: func(_, _, _, _ string) error { return nil },
	}

	origMessenger := sessionMessenger
	defer func() { sessionMessenger = origMessenger }()

	var sentID, sentMsg string
	sessionMessenger = &mockSessionMessenger{
		runningSessions: []common.SessionInfo{},
		allSessions: []common.SessionInfo{
			{ID: "a1b2c3d4-1111-1111-1111-111111111111", Title: "Ended Session"},
		},
		SendMessageFn: func(sid, msg string, _ []model.FileEntry) error {
			sentID = sid
			sentMsg = msg
			return nil
		},
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
	if sentID != "a1b2c3d4-1111-1111-1111-111111111111" {
		t.Errorf("expected send to ended session, got %q", sentID)
	}
	if sentMsg != "继续修改" {
		t.Errorf("expected message '继续修改', got %q", sentMsg)
	}
}

func TestOnChatBotMessage_SessionList(t *testing.T) {
	origDB := db
	defer func() { db = origDB }()
	db = &mockDBWithCallback{
		upsertFn: func(_, _, _, _ string) error { return nil },
	}

	origMessenger := sessionMessenger
	defer func() { sessionMessenger = origMessenger }()

	sessionMessenger = &mockSessionMessenger{
		runningSessions: []common.SessionInfo{
			{ID: "a1b2c3d4-1111-1111-1111-111111111111", Title: "Running Session"},
		},
		allSessions: []common.SessionInfo{
			{ID: "a1b2c3d4-1111-1111-1111-111111111111", Title: "Running Session"},
			{ID: "b2c3d4e5-2222-2222-2222-222222222222", Title: "Old Session"},
		},
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
		Text:             chatbot.BotCallbackDataTextModel{Content: "/ls"},
	}

	_, err := mgr.onChatBotMessage(context.Background(), data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(replyBody) == 0 {
		t.Error("expected reply with session list")
	}
	body := string(replyBody)
	if !strings.Contains(body, "@a1b2c3d4") {
		t.Error("expected session list to contain @a1b2c3d4")
	}
	if !strings.Contains(body, "Running Session") {
		t.Error("expected session list to contain 'Running Session'")
	}
	if !strings.Contains(body, "🟢") {
		t.Error("expected running session to be marked with running indicator")
	}
}

func TestOnChatBotMessage_SessionCommand_SendToNotRunningSession_Fails(t *testing.T) {
	origDB := db
	defer func() { db = origDB }()
	db = &mockDBWithCallback{
		upsertFn: func(_, _, _, _ string) error { return nil },
	}

	origMessenger := sessionMessenger
	defer func() { sessionMessenger = origMessenger }()

	sessionMessenger = &mockSessionMessenger{
		runningSessions: []common.SessionInfo{},
		allSessions: []common.SessionInfo{
			{ID: "a1b2c3d4-1111-1111-1111-111111111111", Title: "Ended Session"},
		},
		SendMessageFn: func(sid, msg string, _ []model.FileEntry) error {
			return fmt.Errorf("session gone")
		},
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
		Text:             chatbot.BotCallbackDataTextModel{Content: "@a1b2c3d4 test"},
	}

	_, err := mgr.onChatBotMessage(context.Background(), data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	body := string(replyBody)
	if !strings.Contains(body, "发送消息失败") {
		t.Errorf("expected send failure reply, got %q", body)
	}
}

func TestOnChatBotMessage_SessionList_NilMessenger(t *testing.T) {
	origDB := db
	defer func() { db = origDB }()
	db = &mockDBWithCallback{
		upsertFn: func(_, _, _, _ string) error { return nil },
	}

	origMessenger := sessionMessenger
	defer func() { sessionMessenger = origMessenger }()
	sessionMessenger = nil

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
		Text:             chatbot.BotCallbackDataTextModel{Content: "/ls"},
	}

	_, err := mgr.onChatBotMessage(context.Background(), data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	body := string(replyBody)
	if !strings.Contains(body, "暂无可用会话") {
		t.Errorf("expected '暂无可用会话' reply when messenger is nil, got %q", body)
	}
}

func TestOnChatBotMessage_SessionList_ListError(t *testing.T) {
	origDB := db
	defer func() { db = origDB }()
	db = &mockDBWithCallback{
		upsertFn: func(_, _, _, _ string) error { return nil },
	}

	origMessenger := sessionMessenger
	defer func() { sessionMessenger = origMessenger }()

	sessionMessenger = &mockSessionMessenger{
		listErr: fmt.Errorf("db error"),
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
		Text:             chatbot.BotCallbackDataTextModel{Content: "/ls"},
	}

	_, err := mgr.onChatBotMessage(context.Background(), data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	body := string(replyBody)
	if !strings.Contains(body, "获取会话列表失败") {
		t.Errorf("expected list error reply, got %q", body)
	}
}

func TestOnChatBotMessage_SessionList_EmptySessions(t *testing.T) {
	origDB := db
	defer func() { db = origDB }()
	db = &mockDBWithCallback{
		upsertFn: func(_, _, _, _ string) error { return nil },
	}

	origMessenger := sessionMessenger
	defer func() { sessionMessenger = origMessenger }()

	sessionMessenger = &mockSessionMessenger{
		runningSessions: []common.SessionInfo{},
		allSessions:     []common.SessionInfo{},
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
		Text:             chatbot.BotCallbackDataTextModel{Content: "/ls"},
	}

	_, err := mgr.onChatBotMessage(context.Background(), data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	body := string(replyBody)
	if !strings.Contains(body, "暂无会话") {
		t.Errorf("expected empty sessions reply, got %q", body)
	}
}

func TestOnChatBotMessage_SessionList_NoProjectAndNoTitle(t *testing.T) {
	origDB := db
	defer func() { db = origDB }()
	db = &mockDBWithCallback{
		upsertFn: func(_, _, _, _ string) error { return nil },
	}

	origMessenger := sessionMessenger
	defer func() { sessionMessenger = origMessenger }()

	sessionMessenger = &mockSessionMessenger{
		runningSessions: []common.SessionInfo{},
		allSessions: []common.SessionInfo{
			{ID: "a1b2c3d4-1111-1111-1111-111111111111", Title: "", ProjectPath: ""},
			{ID: "b2c3d4e5-2222-2222-2222-222222222222", Title: "Has Title", ProjectPath: "/home/project"},
		},
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
		Text:             chatbot.BotCallbackDataTextModel{Content: "/ls"},
	}

	_, err := mgr.onChatBotMessage(context.Background(), data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	body := string(replyBody)
	if !strings.Contains(body, "（无项目）") {
		t.Error("expected '（无项目）' for session with empty project path")
	}
	if !strings.Contains(body, "（无标题）") {
		t.Error("expected '（无标题）' for session with empty title")
	}
	if !strings.Contains(body, "Has Title") {
		t.Error("expected 'Has Title' in output")
	}
}

func TestOnChatBotMessage_SessionCommand_SendToNotRunningSession_Success(t *testing.T) {
	origDB := db
	defer func() { db = origDB }()
	db = &mockDBWithCallback{
		upsertFn: func(_, _, _, _ string) error { return nil },
	}

	origMessenger := sessionMessenger
	defer func() { sessionMessenger = origMessenger }()

	var sentID, sentMsg string
	sessionMessenger = &mockSessionMessenger{
		runningSessions: []common.SessionInfo{},
		allSessions: []common.SessionInfo{
			{ID: "a1b2c3d4-1111-1111-1111-111111111111", Title: "Idle Session"},
		},
		SendMessageFn: func(sid, msg string, _ []model.FileEntry) error {
			sentID = sid
			sentMsg = msg
			return nil
		},
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
		Text:             chatbot.BotCallbackDataTextModel{Content: "@a1b2c3d4 hello"},
	}

	_, err := mgr.onChatBotMessage(context.Background(), data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !replyReceived {
		t.Error("expected reply to be sent")
	}
	if sentID != "a1b2c3d4-1111-1111-1111-111111111111" {
		t.Errorf("expected send to idle session, got %q", sentID)
	}
	if sentMsg != "hello" {
		t.Errorf("expected message 'hello', got %q", sentMsg)
	}
}

// --- Sticky session routing ---

// stickyTestEnv wires a mock DB + messenger + reply-capturing webhook for the
// sticky-session tests. It returns the manager, the captured reply body, and
// the recorded last-session-id writes.
func stickyTestEnv(t *testing.T, sticky string, sessions []common.SessionInfo) (*Manager, *[]byte, *[]string) {
	t.Helper()

	var setCalls []string
	origDB := db
	t.Cleanup(func() { db = origDB })
	db = &mockDBWithCallback{
		upsertFn:  func(_, _, _, _ string) error { return nil },
		getLastFn: func(string) (string, error) { return sticky, nil },
		setLastFn: func(_, sessionID string) error {
			setCalls = append(setCalls, sessionID)
			return nil
		},
	}

	origMessenger := sessionMessenger
	t.Cleanup(func() { sessionMessenger = origMessenger })
	sessionMessenger = &mockSessionMessenger{allSessions: sessions}

	reply := new([]byte)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*reply, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	return &Manager{}, reply, &setCalls
}

func stickyData(webhook, text string) *chatbot.BotCallbackDataModel {
	return &chatbot.BotCallbackDataModel{
		ConversationType: "1",
		SenderStaffId:    "staff123",
		SenderNick:       "TestUser",
		ConversationId:   "conv1",
		SessionWebhook:   webhook,
		Text:             chatbot.BotCallbackDataTextModel{Content: text},
	}
}

var stickySessions = []common.SessionInfo{
	{ID: "a1b2c3d4-1111-1111-1111-111111111111", Title: "Sticky Session", ProjectPath: "/p"},
	{ID: "deadbeef-2222-2222-2222-222222222222", Title: "Other", ProjectPath: "/p"},
}

// A plain message with a recorded target must go to that session, not reply
// with the session list (the pre-change behavior).
func TestOnChatBotMessage_PlainTextGoesToStickySession(t *testing.T) {
	mgr, reply, _ := stickyTestEnv(t, "a1b2c3d4-1111-1111-1111-111111111111", stickySessions)

	var sentID, sentMsg string
	sessionMessenger.(*mockSessionMessenger).SendMessageFn = func(sid, msg string, _ []model.FileEntry) error {
		sentID, sentMsg = sid, msg
		return nil
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*reply, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	if _, err := mgr.onChatBotMessage(context.Background(), stickyData(server.URL, "run the tests")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if sentID != "a1b2c3d4-1111-1111-1111-111111111111" {
		t.Errorf("sent to %q, want the sticky session", sentID)
	}
	if sentMsg != "run the tests" {
		t.Errorf("sent message = %q, want 'run the tests'", sentMsg)
	}
}

// A first-time sender (no recorded target) must be told how to pick, and the
// message must not be sent anywhere.
func TestOnChatBotMessage_NoStickyTargetAsksForSession(t *testing.T) {
	mgr, reply, _ := stickyTestEnv(t, "", stickySessions)

	sent := false
	sessionMessenger.(*mockSessionMessenger).SendMessageFn = func(string, string, []model.FileEntry) error {
		sent = true
		return nil
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*reply, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	if _, err := mgr.onChatBotMessage(context.Background(), stickyData(server.URL, "hello")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if sent {
		t.Error("a message with no target session must not be sent")
	}
	if !strings.Contains(string(*reply), "/ls") {
		t.Errorf("reply should point at /ls, got %s", string(*reply))
	}
}

// An explicit "@{shortID}" must override the sticky target and become the new
// sticky target after a successful send.
func TestOnChatBotMessage_ExplicitTargetOverridesAndUpdatesSticky(t *testing.T) {
	mgr, _, setCalls := stickyTestEnv(t, "a1b2c3d4-1111-1111-1111-111111111111", stickySessions)

	var sentID string
	sessionMessenger.(*mockSessionMessenger).SendMessageFn = func(sid, _ string, _ []model.FileEntry) error {
		sentID = sid
		return nil
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	if _, err := mgr.onChatBotMessage(context.Background(), stickyData(server.URL, "@deadbeef hi")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if sentID != "deadbeef-2222-2222-2222-222222222222" {
		t.Errorf("sent to %q, want the explicitly named session", sentID)
	}
	if len(*setCalls) != 1 || (*setCalls)[0] != "deadbeef-2222-2222-2222-222222222222" {
		t.Errorf("sticky target updates = %v, want one write of the explicit session", *setCalls)
	}
}

// A failed send must not move the sticky target, or the user's next unprefixed
// message would silently go to a session that just rejected one.
func TestOnChatBotMessage_FailedSendDoesNotUpdateSticky(t *testing.T) {
	mgr, _, setCalls := stickyTestEnv(t, "a1b2c3d4-1111-1111-1111-111111111111", stickySessions)

	sessionMessenger.(*mockSessionMessenger).SendMessageFn = func(string, string, []model.FileEntry) error {
		return fmt.Errorf("session gone")
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	if _, err := mgr.onChatBotMessage(context.Background(), stickyData(server.URL, "@deadbeef hi")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(*setCalls) != 0 {
		t.Errorf("sticky target must not move on a failed send, got %v", *setCalls)
	}
}

// A file message downloads and sends an attachment-only message (empty text)
// to the sticky session.
func TestOnChatBotMessage_FileGoesToStickySessionAsAttachment(t *testing.T) {
	body := "attachment-bytes"
	srv, _ := mediaTestServer(t, body, http.StatusOK)

	project := t.TempDir()
	sessions := []common.SessionInfo{
		{ID: "a1b2c3d4-1111-1111-1111-111111111111", Title: "Sticky Session", ProjectPath: project},
	}
	mgr, _, _ := stickyTestEnv(t, "a1b2c3d4-1111-1111-1111-111111111111", sessions)
	mgr.httpClient = srv.Client()
	mgr.cfg = &model.DingTalkConfig{AppKey: "test-app-key"}
	mgr.cachedToken = "test-access-token"
	mgr.cachedExp = time.Now().Add(2 * time.Hour)

	sentCh := make(chan struct {
		sid   string
		msg   string
		files []model.FileEntry
	}, 1)
	sessionMessenger.(*mockSessionMessenger).SendMessageFn = func(sid, msg string, files []model.FileEntry) error {
		sentCh <- struct {
			sid   string
			msg   string
			files []model.FileEntry
		}{sid, msg, files}
		return nil
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	data := stickyData(server.URL, "")
	data.Msgtype = "file"
	data.Content = map[string]any{"downloadCode": "code-1", "fileName": "报告.pdf"}

	if _, err := mgr.onChatBotMessage(context.Background(), data); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The download runs in a goroutine so the ack is not delayed.
	select {
	case got := <-sentCh:
		if got.sid != "a1b2c3d4-1111-1111-1111-111111111111" {
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

// A file message with no sticky target must not download anything.
func TestOnChatBotMessage_FileWithNoStickyTargetAsksForSession(t *testing.T) {
	srv, gotCode := mediaTestServer(t, "x", http.StatusOK)
	mgr, reply, _ := stickyTestEnv(t, "", nil)
	mgr.httpClient = srv.Client()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*reply, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	data := stickyData(server.URL, "")
	data.Msgtype = "file"
	data.Content = map[string]any{"downloadCode": "code-1", "fileName": "a.txt"}

	if _, err := mgr.onChatBotMessage(context.Background(), data); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Give the (should-be-absent) goroutine a chance to run.
	time.Sleep(100 * time.Millisecond)
	if *gotCode != "" {
		t.Errorf("must not download without a target session, got code %q", *gotCode)
	}
	if !strings.Contains(string(*reply), "/ls") {
		t.Errorf("reply should point at /ls, got %s", string(*reply))
	}
}

// A file whose sticky session has since been archived/deleted must tell the
// user to re-pick rather than downloading into nowhere.
func TestOnChatBotMessage_FileWithUnavailableStickySession(t *testing.T) {
	srv, gotCode := mediaTestServer(t, "x", http.StatusOK)
	mgr, reply, _ := stickyTestEnv(t, "deadbeef00000000", stickySessions)
	mgr.httpClient = srv.Client()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*reply, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	data := stickyData(server.URL, "")
	data.Msgtype = "file"
	data.Content = map[string]any{"downloadCode": "code-1", "fileName": "a.txt"}

	if _, err := mgr.onChatBotMessage(context.Background(), data); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	waitForReply(t, reply)
	if *gotCode != "" {
		t.Errorf("must not download when the sticky session is gone, got code %q", *gotCode)
	}
	if !strings.Contains(string(*reply), "/ls") {
		t.Errorf("reply should point at /ls, got %s", string(*reply))
	}
}

// A media message arriving with no session messenger wired up must produce a
// visible reply instead of failing silently.
func TestOnChatBotMessage_FileWithNoMessenger(t *testing.T) {
	mgr, reply, _ := stickyTestEnv(t, "a1b2c3d4-1111-1111-1111-111111111111", stickySessions)
	sessionMessenger = nil

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*reply, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	data := stickyData(server.URL, "")
	data.Msgtype = "file"
	data.Content = map[string]any{"downloadCode": "code-1", "fileName": "a.txt"}

	if _, err := mgr.onChatBotMessage(context.Background(), data); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	waitForReply(t, reply)
	if !strings.Contains(string(*reply), "会话服务不可用") {
		t.Errorf("reply should say the session service is unavailable, got %s", string(*reply))
	}
}

// A download failure must be reported to the user and must not send an
// attachment to the session.
func TestOnChatBotMessage_FileDownloadFailureReplies(t *testing.T) {
	// A 403 from the blob endpoint makes DownloadAttachment fail.
	srv, _ := mediaTestServer(t, "denied", http.StatusForbidden)
	project := t.TempDir()
	sessions := []common.SessionInfo{
		{ID: "a1b2c3d4-1111-1111-1111-111111111111", Title: "Sticky Session", ProjectPath: project},
	}
	mgr, reply, _ := stickyTestEnv(t, "a1b2c3d4-1111-1111-1111-111111111111", sessions)
	mgr.httpClient = srv.Client()
	mgr.cfg = &model.DingTalkConfig{AppKey: "test-app-key"}
	mgr.cachedToken = "test-access-token"
	mgr.cachedExp = time.Now().Add(2 * time.Hour)

	sent := make(chan []model.FileEntry, 1)
	sessionMessenger.(*mockSessionMessenger).SendMessageFn = func(_, _ string, files []model.FileEntry) error {
		sent <- files
		return nil
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*reply, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	data := stickyData(server.URL, "")
	data.Msgtype = "file"
	data.Content = map[string]any{"downloadCode": "code-1", "fileName": "a.txt"}

	if _, err := mgr.onChatBotMessage(context.Background(), data); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	waitForReply(t, reply)
	if !strings.Contains(string(*reply), "下载失败") {
		t.Errorf("reply should report the download failure, got %s", string(*reply))
	}
	select {
	case files := <-sent:
		t.Errorf("a failed download must not send an attachment, got %d files", len(files))
	case <-time.After(200 * time.Millisecond):
	}
}

// An oversized file must produce the size-specific message rather than the
// generic download-failure text.
func TestOnChatBotMessage_FileTooLargeRepliesWithSizeLimit(t *testing.T) {
	orig := model.UploadMaxSizeMB
	model.UploadMaxSizeMB = 1
	t.Cleanup(func() { model.UploadMaxSizeMB = orig })

	srv, _ := mediaTestServer(t, strings.Repeat("a", 2*1024*1024), http.StatusOK)
	sessions := []common.SessionInfo{
		{ID: "a1b2c3d4-1111-1111-1111-111111111111", Title: "Sticky Session", ProjectPath: t.TempDir()},
	}
	mgr, reply, _ := stickyTestEnv(t, "a1b2c3d4-1111-1111-1111-111111111111", sessions)
	mgr.httpClient = srv.Client()
	mgr.cfg = &model.DingTalkConfig{AppKey: "test-app-key"}
	mgr.cachedToken = "test-access-token"
	mgr.cachedExp = time.Now().Add(2 * time.Hour)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*reply, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	data := stickyData(server.URL, "")
	data.Msgtype = "file"
	data.Content = map[string]any{"downloadCode": "code-1", "fileName": "big.bin"}

	if _, err := mgr.onChatBotMessage(context.Background(), data); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	waitForReply(t, reply)
	if !strings.Contains(string(*reply), "大小上限") {
		t.Errorf("reply should mention the size limit, got %s", string(*reply))
	}
}

// A send failure after a successful download must be reported, and the sticky
// target must not move.
func TestOnChatBotMessage_FileSendFailureReplies(t *testing.T) {
	srv, _ := mediaTestServer(t, "bytes", http.StatusOK)
	sessions := []common.SessionInfo{
		{ID: "a1b2c3d4-1111-1111-1111-111111111111", Title: "Sticky Session", ProjectPath: t.TempDir()},
	}
	mgr, reply, setCalls := stickyTestEnv(t, "a1b2c3d4-1111-1111-1111-111111111111", sessions)
	mgr.httpClient = srv.Client()
	mgr.cfg = &model.DingTalkConfig{AppKey: "test-app-key"}
	mgr.cachedToken = "test-access-token"
	mgr.cachedExp = time.Now().Add(2 * time.Hour)

	sessionMessenger.(*mockSessionMessenger).SendMessageFn = func(_, _ string, _ []model.FileEntry) error {
		return fmt.Errorf("session busy")
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*reply, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	data := stickyData(server.URL, "")
	data.Msgtype = "file"
	data.Content = map[string]any{"downloadCode": "code-1", "fileName": "a.txt"}

	if _, err := mgr.onChatBotMessage(context.Background(), data); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	waitForReply(t, reply)
	if !strings.Contains(string(*reply), "发送文件失败") {
		t.Errorf("reply should report the send failure, got %s", string(*reply))
	}
	if len(*setCalls) != 0 {
		t.Errorf("a failed send must not update the sticky target, got %v", *setCalls)
	}
}

// waitForReply blocks until the async media handler has written its reply.
func waitForReply(t *testing.T, reply *[]byte) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if len(*reply) > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for a reply")
}
