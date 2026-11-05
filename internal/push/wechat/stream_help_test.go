package wechat

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// TestHandleIncomingText_HelpRepliesWithCommandReference pins the "/help"
// route: WeChat renders no Markdown, so the shared command reference must reach
// the user without literal "**" markers.
func TestHandleIncomingText_HelpRepliesWithCommandReference(t *testing.T) {
	origDB := db
	db = nil
	t.Cleanup(func() { db = origDB })

	mgr, bodies := newSendServer(t, `{"ret":0}`)
	mgr.handleIncomingText(context.Background(), "user@im.wechat", "ctx-1", "/help")

	if len(*bodies) != 1 {
		t.Fatalf("expected exactly 1 request, got %d", len(*bodies))
	}
	raw, _ := json.Marshal((*bodies)[0])
	got := string(raw)
	for _, want := range []string{"/ls", "/help"} {
		if !strings.Contains(got, want) {
			t.Errorf("help reply does not mention %q: %s", want, got)
		}
	}
	if strings.Contains(got, "**") {
		t.Errorf("wechat reply still carries Markdown emphasis markers: %s", got)
	}
}
