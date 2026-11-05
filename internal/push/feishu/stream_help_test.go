package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"clawbench/internal/model"
)

// newHelpServers starts a fake Feishu token endpoint and message endpoint and
// points the package-level URLs at them. The recorded message bodies are
// returned so a test can assert what the user actually receives.
func newHelpServers(t *testing.T) func() []string {
	t.Helper()

	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintln(w, `{"code":0,"msg":"ok","tenant_access_token":"t-help","expire":7200}`)
	}))
	t.Cleanup(tokenSrv.Close)

	var mu sync.Mutex
	var bodies []string
	msgSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		raw, _ := json.Marshal(body)
		mu.Lock()
		bodies = append(bodies, string(raw))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintln(w, `{"code":0,"msg":"ok"}`)
	}))
	t.Cleanup(msgSrv.Close)

	origTokenURL := feishuTokenURL
	feishuTokenURL = tokenSrv.URL
	t.Cleanup(func() { feishuTokenURL = origTokenURL })

	origMsgURL := feishuMessageURL
	feishuMessageURL = msgSrv.URL
	t.Cleanup(func() { feishuMessageURL = origMsgURL })

	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), bodies...)
	}
}

// TestHandleIncomingText_HelpRepliesWithCommandReference pins the "/help"
// route: the card body must be the shared command reference, so all three IM
// channels list the same commands.
func TestHandleIncomingText_HelpRepliesWithCommandReference(t *testing.T) {
	origDB := db
	db = nil
	t.Cleanup(func() { db = origDB })

	bodies := newHelpServers(t)
	mgr := NewManager(&model.FeishuConfig{AppID: "test", AppSecret: "test"})

	mgr.handleIncomingText(context.Background(), "ou_user", "/help", "/help")

	got := bodies()
	if len(got) != 1 {
		t.Fatalf("expected exactly 1 message, got %d", len(got))
	}
	for _, want := range []string{"/ls", "/help"} {
		if !strings.Contains(got[0], want) {
			t.Errorf("help message does not mention %q: %s", want, got[0])
		}
	}
}
