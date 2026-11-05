package dingtalk

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/open-dingtalk/dingtalk-stream-sdk-go/chatbot"
)

// helpWebhookServer starts a fake DingTalk webhook that records the reply
// bodies, and returns the server plus an accessor for the recorded bodies.
func helpWebhookServer(t *testing.T) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(b))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), bodies...)
	}
}

// TestHandleIncomingText_HelpRepliesWithCommandReference pins the "/help"
// route: the reply must be the shared command reference, so all three IM
// channels list the same commands.
func TestHandleIncomingText_HelpRepliesWithCommandReference(t *testing.T) {
	origDB := db
	db = nil
	t.Cleanup(func() { db = origDB })

	srv, bodies := helpWebhookServer(t)
	mgr := &Manager{}
	data := &chatbot.BotCallbackDataModel{
		ConversationType: "1",
		SenderStaffId:    "staff1",
		SenderNick:       "Tester",
		ConversationId:   "conv1",
		SessionWebhook:   srv.URL,
		Text:             chatbot.BotCallbackDataTextModel{Content: "/help"},
	}

	mgr.handleIncomingText(context.Background(), data, "staff1", "/help")

	got := bodies()
	if len(got) != 1 {
		t.Fatalf("expected exactly 1 reply, got %d", len(got))
	}
	for _, want := range []string{"/ls", "/help"} {
		if !strings.Contains(got[0], want) {
			t.Errorf("help reply does not mention %q: %s", want, got[0])
		}
	}
}

// TestHandleIncomingText_NoTargetRepliesWithSessionHint pins the untargeted
// path: with no sticky session, a plain message must NOT be delivered to a
// session but answered with the "pick a session" hint.
func TestHandleIncomingText_NoTargetRepliesWithSessionHint(t *testing.T) {
	origDB := db
	db = nil
	t.Cleanup(func() { db = origDB })

	srv, bodies := helpWebhookServer(t)
	mgr := &Manager{}
	data := &chatbot.BotCallbackDataModel{
		ConversationType: "1",
		SenderStaffId:    "staff1",
		SenderNick:       "Tester",
		ConversationId:   "conv1",
		SessionWebhook:   srv.URL,
		Text:             chatbot.BotCallbackDataTextModel{Content: "hello there"},
	}

	mgr.handleIncomingText(context.Background(), data, "staff1", "hello there")

	got := bodies()
	if len(got) != 1 {
		t.Fatalf("expected exactly 1 reply, got %d", len(got))
	}
	if !strings.Contains(got[0], "/ls") {
		t.Errorf("no-target reply does not point at /ls: %s", got[0])
	}
}
