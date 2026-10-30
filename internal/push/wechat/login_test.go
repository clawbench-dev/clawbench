package wechat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeILink is a minimal iLink login endpoint: it hands out QR codes and walks a
// scripted list of statuses, one per poll.
type fakeILink struct {
	mu       sync.Mutex
	statuses []string // statuses to return in order; the last one repeats
	polls    int
	qrSeq    int
}

func (f *fakeILink) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/ilink/bot/get_bot_qrcode", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.qrSeq++
		seq := f.qrSeq
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"qrcode":             "qr-" + itoa(seq),
			"qrcode_img_content": "https://liteapp.weixin.qq.com/q/abc?qrcode=qr-" + itoa(seq),
			"ret":                0,
		})
	})
	mux.HandleFunc("/ilink/bot/get_qrcode_status", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		idx := f.polls
		if idx >= len(f.statuses) {
			idx = len(f.statuses) - 1
		}
		status := f.statuses[idx]
		f.polls++
		f.mu.Unlock()

		resp := map[string]any{"status": status}
		if status == "confirmed" {
			resp["bot_token"] = "tok-from-scan"
			resp["ilink_bot_id"] = "bot-1"
			resp["baseurl"] = "https://assigned.example"
			resp["ilink_user_id"] = "user-1"
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	return mux
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// waitForQR polls until the session carries a QR payload.
func waitForQR(t *testing.T, svc *LoginService, id string) LoginSnapshot {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		snap, ok := svc.Poll(id)
		if !ok {
			t.Fatalf("session %s vanished", id)
		}
		if snap.QRURL != "" {
			return snap
		}
		if snap.State.terminal() {
			t.Fatalf("session reached terminal state %q before a QR was issued (%s)", snap.State, snap.Error)
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for a QR payload")
	return LoginSnapshot{}
}

// waitForState polls the service until the session reaches want or the deadline
// passes. Polling the snapshot rather than sleeping keeps the test free of
// arbitrary timeouts.
func waitForState(t *testing.T, svc *LoginService, id string, want LoginState) LoginSnapshot {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var last LoginSnapshot
	for time.Now().Before(deadline) {
		snap, ok := svc.Poll(id)
		if !ok {
			t.Fatalf("session %s vanished", id)
		}
		last = snap
		if snap.State == want {
			return snap
		}
		if snap.State.terminal() {
			t.Fatalf("session reached terminal state %q, want %q (%s)", snap.State, want, snap.Error)
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for state %q, last was %q", want, last.State)
	return last
}

// TestLoginService_SurvivesCallerContextCancellation is the regression guard on
// the login goroutine's lifetime.
//
// Start is called from an HTTP handler, and net/http cancels r.Context() as soon
// as the handler returns — right after the 202 is written. If the goroutine
// derived its context from that, the first FetchQRCode would be aborted
// microseconds later, the session would go straight to "cancelled", and no QR
// would ever reach the user. Every other login test passes
// context.Background(), which cannot catch this.
func TestLoginService_SurvivesCallerContextCancellation(t *testing.T) {
	fake := &fakeILink{statuses: []string{"wait", "confirmed"}}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	svc := NewLoginService(LoginServiceOptions{BaseURL: srv.URL})

	// Simulate the handler: a request-scoped context that dies on return.
	reqCtx, cancelReq := context.WithCancel(context.Background())
	snap := svc.Start(reqCtx)
	cancelReq()

	final := waitForState(t, svc, snap.ID, LoginConnected)
	assert.Equal(t, LoginConnected, final.State,
		"the login must outlive the HTTP request that started it")
	assert.NotEmpty(t, final.QRURL, "a QR must have been fetched after the request context died")
}

func TestLoginService_ConnectedPersistsThenReloads(t *testing.T) {
	fake := &fakeILink{statuses: []string{"wait", "scaned", "confirmed"}}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	var persisted []LoginResult
	var mu sync.Mutex
	var reloaded atomic.Int32
	svc := NewLoginService(LoginServiceOptions{
		BaseURL: srv.URL,
		Persist: func(r LoginResult) error {
			mu.Lock()
			persisted = append(persisted, r)
			mu.Unlock()
			return nil
		},
		Reload: func() { reloaded.Add(1) },
	})

	snap := svc.Start(context.Background())
	require.Equal(t, LoginPending, snap.State)
	// The QR is fetched by the goroutine, not by Start: Start must return
	// immediately even when the QR endpoint is slow (it has no timeout of its
	// own beyond the 8-minute login budget), so the HTTP handler cannot block.
	assert.NotEmpty(t, waitForQR(t, svc, snap.ID).QRURL,
		"the QR payload must arrive on a later poll")

	final := waitForState(t, svc, snap.ID, LoginConnected)
	assert.Equal(t, "bot-1", final.BotID)

	mu.Lock()
	gotPersisted := append([]LoginResult(nil), persisted...)
	mu.Unlock()
	require.Len(t, gotPersisted, 1, "credentials must be persisted exactly once")
	assert.Equal(t, "tok-from-scan", gotPersisted[0].BotToken)
	assert.Equal(t, "bot-1", gotPersisted[0].BotID)
	assert.Equal(t, "https://assigned.example", gotPersisted[0].BaseURL)
	assert.Equal(t, "user-1", gotPersisted[0].UserID)

	// Reload runs in the same goroutine, so it may land a moment after the state
	// flips; poll for it rather than assuming ordering.
	deadline := time.Now().Add(2 * time.Second)
	for reloaded.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	assert.Equal(t, int32(1), reloaded.Load(), "a successful persist must trigger exactly one reload")
}

// TestLoginService_PersistFailureIsNotConnected pins the ordering: the state
// only becomes connected AFTER persistence succeeds, and a failure must not
// trigger a reload (which would restart the manager with unstored credentials).
func TestLoginService_PersistFailureIsNotConnected(t *testing.T) {
	fake := &fakeILink{statuses: []string{"confirmed"}}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	reloaded := atomic.Int32{}
	svc := NewLoginService(LoginServiceOptions{
		BaseURL: srv.URL,
		Persist: func(LoginResult) error { return assert.AnError },
		Reload:  func() { reloaded.Add(1) },
	})

	snap := svc.Start(context.Background())
	final := waitForState(t, svc, snap.ID, LoginFailed)
	assert.Contains(t, final.Error, assert.AnError.Error())
	assert.Equal(t, int32(0), reloaded.Load(), "a failed persist must not reload")
}

func TestLoginService_QRRefreshIncrementsSeq(t *testing.T) {
	// A leading "wait" gives the test a stable window to observe the first QR
	// before the expiries start replacing it.
	fake := &fakeILink{statuses: []string{"wait", "expired", "expired", "wait"}}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	svc := NewLoginService(LoginServiceOptions{BaseURL: srv.URL})
	snap := svc.Start(context.Background())
	initial := waitForQR(t, svc, snap.ID)
	assert.Equal(t, 1, initial.QRSeq, "the first QR is seq 1")

	// Wait until at least one refresh has been observed.
	deadline := time.Now().Add(5 * time.Second)
	var latest LoginSnapshot
	for time.Now().Before(deadline) {
		cur, ok := svc.Poll(snap.ID)
		require.True(t, ok)
		latest = cur
		if cur.QRSeq > 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	assert.Greater(t, latest.QRSeq, 1, "an expired QR must be replaced with a new one")
	assert.NotEqual(t, initial.QRURL, latest.QRURL, "the QR payload must change on refresh")
}

// TestLoginService_StartIsIdempotent: a re-rendered settings panel calls Start
// again, and a second goroutine polling for 8 minutes with no handle would be a
// leak. Comparing IDs alone cannot prove that — two goroutines would also share
// the id of the second session — so this also pins that only ONE login flow ever
// reached the network.
func TestLoginService_StartIsIdempotent(t *testing.T) {
	fake := &fakeILink{statuses: []string{"wait"}}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	svc := NewLoginService(LoginServiceOptions{BaseURL: srv.URL})
	defer svc.CancelAll()

	first := svc.Start(context.Background())
	second := svc.Start(context.Background())
	assert.Equal(t, first.ID, second.ID, "an in-flight login must be reused")

	// Give a hypothetical second goroutine time to reach FetchQRCode.
	waitForQR(t, svc, first.ID)
	time.Sleep(200 * time.Millisecond)

	fake.mu.Lock()
	qrFetches := fake.qrSeq
	fake.mu.Unlock()
	assert.Equal(t, 1, qrFetches,
		"exactly one QR must have been fetched; a second one means a second goroutine")
}

func TestLoginService_Cancel(t *testing.T) {
	fake := &fakeILink{statuses: []string{"wait"}}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	svc := NewLoginService(LoginServiceOptions{BaseURL: srv.URL})
	snap := svc.Start(context.Background())
	svc.Cancel(snap.ID)

	final := waitForState(t, svc, snap.ID, LoginCancelled)
	assert.Equal(t, LoginCancelled, final.State)
}

func TestLoginService_CancelUnknownIsNoop(t *testing.T) {
	svc := NewLoginService(LoginServiceOptions{})
	svc.Cancel("does-not-exist") // must not panic
	_, ok := svc.Poll("does-not-exist")
	assert.False(t, ok)
}

func TestLoginService_PollUnknown(t *testing.T) {
	svc := NewLoginService(LoginServiceOptions{})
	_, ok := svc.Poll("nope")
	assert.False(t, ok, "an unknown id must be reported so the client restarts the flow")
}
