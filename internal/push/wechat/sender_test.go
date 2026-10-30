package wechat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/push/common"
)

// newSendServer starts a fake iLink endpoint that records the request bodies it
// receives, and returns the manager wired to it.
func newSendServer(t *testing.T, respBody string) (*Manager, *[]map[string]any) {
	t.Helper()

	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		bodies = append(bodies, body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(respBody))
	}))
	t.Cleanup(srv.Close)

	mgr := NewManager(&model.WeChatConfig{BotToken: "tok", BaseURL: srv.URL})
	mgr.started = true
	mgr.client = newTestClient(srv.URL)
	return mgr, &bodies
}

func TestSendText_SendsTextItem(t *testing.T) {
	mgr, bodies := newSendServer(t, `{"ret":0}`)

	err := mgr.SendText(context.Background(), "user@im.wechat", "hello", "ctx-1")
	if err != nil {
		t.Fatalf("SendText failed: %v", err)
	}
	if len(*bodies) != 1 {
		t.Fatalf("expected 1 request, got %d", len(*bodies))
	}

	msg, ok := (*bodies)[0]["msg"].(map[string]any)
	if !ok {
		t.Fatalf("request has no msg object: %#v", (*bodies)[0])
	}
	if got := msg["to_user_id"]; got != "user@im.wechat" {
		t.Errorf("to_user_id = %v, want user@im.wechat", got)
	}
	if got := msg["context_token"]; got != "ctx-1" {
		t.Errorf("context_token = %v, want ctx-1", got)
	}
	if got := msg["message_type"]; got != float64(2) {
		t.Errorf("message_type = %v, want 2 (bot)", got)
	}

	items, ok := msg["item_list"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("expected exactly one item, got %#v", msg["item_list"])
	}
	item := items[0].(map[string]any)
	if item["type"] != float64(1) {
		t.Errorf("item type = %v, want 1 (text)", item["type"])
	}
	textItem := item["text_item"].(map[string]any)
	if textItem["text"] != "hello" {
		t.Errorf("text = %v, want hello", textItem["text"])
	}
}

// TestSendText_RefusesEmptyToken pins the fast-fail: the platform rejects a
// message without a context token, so we must not spend a round trip on it.
func TestSendText_RefusesEmptyToken(t *testing.T) {
	mgr, bodies := newSendServer(t, `{"ret":0}`)

	if err := mgr.SendText(context.Background(), "u", "hi", ""); err == nil {
		t.Fatal("expected an error for an empty context token")
	}
	if len(*bodies) != 0 {
		t.Fatalf("no request must be sent, got %d", len(*bodies))
	}
}

func TestSendText_RefusesEmptyInputs(t *testing.T) {
	mgr, bodies := newSendServer(t, `{"ret":0}`)

	if err := mgr.SendText(context.Background(), "", "hi", "ctx"); err == nil {
		t.Error("expected an error for an empty user id")
	}
	if err := mgr.SendText(context.Background(), "u", "", "ctx"); err == nil {
		t.Error("expected an error for an empty body")
	}
	if len(*bodies) != 0 {
		t.Fatalf("no request must be sent, got %d", len(*bodies))
	}
}

func TestSendText_NotStarted(t *testing.T) {
	mgr := NewManager(&model.WeChatConfig{BotToken: "tok"})
	err := mgr.SendText(context.Background(), "u", "hi", "ctx")
	if err == nil {
		t.Fatal("expected ErrNotStarted")
	}
}

func TestSendText_PropagatesAPIError(t *testing.T) {
	mgr, _ := newSendServer(t, `{"ret":-1,"errcode":40001,"errmsg":"bad token"}`)

	if err := mgr.SendText(context.Background(), "u", "hi", "ctx"); err == nil {
		t.Fatal("expected an API error to surface")
	}
}

// TestSendToAllSubscribers_SkipsEmptyToken is the load-bearing new behavior:
// a subscriber with no context token is unreachable by protocol, so it must be
// skipped rather than attempted (and must not make the call report success).
func TestSendToAllSubscribers_SkipsEmptyToken(t *testing.T) {
	defer setupPushMode("wechat")()
	startedManager(
		t,
		common.SubscriberInfo{UserID: "no-token"},
		common.SubscriberInfo{UserID: "has-token", ContextToken: "ctx-1"},
	)

	mgr, bodies := newSendServer(t, `{"ret":0}`)
	SetManager(mgr)

	if !sendToAllSubscribers("hello") {
		t.Fatal("expected at least one successful send")
	}
	if len(*bodies) != 1 {
		t.Fatalf("expected exactly one request (the reachable subscriber), got %d", len(*bodies))
	}
	msg := (*bodies)[0]["msg"].(map[string]any)
	if msg["to_user_id"] != "has-token" {
		t.Errorf("sent to %v, want has-token", msg["to_user_id"])
	}
}

// TestSendToAllSubscribers_AllUnreachable makes sure "nobody could be reached"
// reports false, so callers do not delete the pending event and silently lose
// the notification.
func TestSendToAllSubscribers_AllUnreachable(t *testing.T) {
	defer setupPushMode("wechat")()
	startedManager(t, common.SubscriberInfo{UserID: "no-token"})

	mgr, bodies := newSendServer(t, `{"ret":0}`)
	SetManager(mgr)

	if sendToAllSubscribers("hello") {
		t.Fatal("no subscriber had a token; the call must report false")
	}
	if len(*bodies) != 0 {
		t.Fatalf("no request must be sent, got %d", len(*bodies))
	}
}

func TestSendToAllSubscribers_PushModeGate(t *testing.T) {
	defer setupPushMode("native")()
	startedManager(t, common.SubscriberInfo{UserID: "u", ContextToken: "ctx"})

	mgr, bodies := newSendServer(t, `{"ret":0}`)
	SetManager(mgr)

	if sendToAllSubscribers("hello") {
		t.Fatal("push must be gated on push_mode == wechat")
	}
	if len(*bodies) != 0 {
		t.Fatalf("no request must be sent, got %d", len(*bodies))
	}
}

func TestSendToAllSubscribers_SuppressedWhenClientOnline(t *testing.T) {
	defer setupPushMode("wechat")()
	startedManager(t, common.SubscriberInfo{UserID: "u", ContextToken: "ctx"})

	origChecker := clientChecker
	defer func() { clientChecker = origChecker }()
	RegisterClientChecker(&mockClientChecker{hasConnected: true})

	mgr, bodies := newSendServer(t, `{"ret":0}`)
	SetManager(mgr)

	if sendToAllSubscribers("hello") {
		t.Fatal("push must be suppressed while a client is connected")
	}
	if len(*bodies) != 0 {
		t.Fatalf("no request must be sent, got %d", len(*bodies))
	}
}
