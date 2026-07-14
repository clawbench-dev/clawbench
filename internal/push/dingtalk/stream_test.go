package dingtalk

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

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
	mergeFn  func(users []string)
	getFn    func() ([]SubscriberInfo, error)
	upsertFn func(userID, conversationID, userName, source string) error
	deleteFn func(userID string) error
}

func (m *mockDBWithCallback) MergeConfigSubscribers(users []string) {
	if m.mergeFn != nil {
		m.mergeFn(users)
	}
}

func (m *mockDBWithCallback) GetSubscribers() ([]SubscriberInfo, error) {
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

var errTestFailure = fmt.Errorf("test failure")

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
		EnqueueMessageFn: func(sid, msg string) error {
			enqueuedSession = sid
			enqueuedMsg = msg
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
		runningSessions: []SessionInfo{},
		allSessions:     []SessionInfo{},
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
