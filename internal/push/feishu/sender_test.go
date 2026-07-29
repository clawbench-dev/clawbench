package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"clawbench/internal/model"
)

// TestSendPostMessage_Success verifies the happy path: token fetch + message send.
func TestSendPostMessage_Success(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintln(w, `{"code":0,"msg":"ok","tenant_access_token":"t-success","expire":7200}`)
	}))
	defer tokenSrv.Close()

	msgSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify Authorization header
		if got := r.Header.Get("Authorization"); got != "Bearer t-success" {
			t.Errorf("expected Bearer t-success, got %q", got)
		}
		// Verify request body structure
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		if body["msg_type"] != "interactive" {
			t.Errorf("expected msg_type=interactive, got %v", body["msg_type"])
		}
		if body["receive_id"] != "ou_test_user" {
			t.Errorf("expected receive_id=ou_test_user, got %v", body["receive_id"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintln(w, `{"code":0,"msg":"ok"}`)
	}))
	defer msgSrv.Close()

	origTokenURL := feishuTokenURL
	feishuTokenURL = tokenSrv.URL
	defer func() { feishuTokenURL = origTokenURL }()

	origMsgURL := feishuMessageURL
	feishuMessageURL = msgSrv.URL
	defer func() { feishuMessageURL = origMsgURL }()

	mgr := NewManager(&model.FeishuConfig{AppID: "test", AppSecret: "test"})
	err := mgr.SendPostMessage(context.Background(), "ou_test_user", "Test Title", "Test Content")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestSendPostMessage_TokenErrorThenRetry verifies the 401 retry logic:
// first attempt returns 401, token is invalidated, second attempt succeeds.
func TestSendPostMessage_TokenErrorThenRetry(t *testing.T) {
	callCount := 0
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintln(w, `{"code":0,"msg":"ok","tenant_access_token":"t-refreshed","expire":7200}`)
	}))
	defer tokenSrv.Close()

	msgSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			// First call: return 401 to trigger retry
			w.WriteHeader(401)
			return
		}
		// Second call: success
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintln(w, `{"code":0,"msg":"ok"}`)
	}))
	defer msgSrv.Close()

	origTokenURL := feishuTokenURL
	feishuTokenURL = tokenSrv.URL
	defer func() { feishuTokenURL = origTokenURL }()

	origMsgURL := feishuMessageURL
	feishuMessageURL = msgSrv.URL
	defer func() { feishuMessageURL = origMsgURL }()

	mgr := NewManager(&model.FeishuConfig{AppID: "test", AppSecret: "test"})
	// Pre-cache a stale token — it will be invalidated on 401
	mgr.cachedToken = "t-stale"
	mgr.cachedExp = time.Now().Add(1 * time.Hour)

	err := mgr.SendPostMessage(context.Background(), "ou_test_user", "Title", "Content")
	if err != nil {
		t.Fatalf("expected retry to succeed, got error: %v", err)
	}
	if callCount != 2 {
		t.Errorf("expected 2 message API calls (initial + retry), got %d", callCount)
	}
	// Token should have been refreshed (cleared and re-fetched)
	if mgr.cachedToken != "t-refreshed" {
		t.Errorf("expected token to be refreshed, got %q", mgr.cachedToken)
	}
}

// TestSendPostMessage_NonTokenError_NoRetry verifies that non-401 errors
// are not retried.
func TestSendPostMessage_NonTokenError_NoRetry(t *testing.T) {
	callCount := 0
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintln(w, `{"code":0,"msg":"ok","tenant_access_token":"t-ok","expire":7200}`)
	}))
	defer tokenSrv.Close()

	msgSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintln(w, `{"code":99999,"msg":"some error"}`)
	}))
	defer msgSrv.Close()

	origTokenURL := feishuTokenURL
	feishuTokenURL = tokenSrv.URL
	defer func() { feishuTokenURL = origTokenURL }()

	origMsgURL := feishuMessageURL
	feishuMessageURL = msgSrv.URL
	defer func() { feishuMessageURL = origMsgURL }()

	mgr := NewManager(&model.FeishuConfig{AppID: "test", AppSecret: "test"})
	err := mgr.SendPostMessage(context.Background(), "ou_test_user", "Title", "Content")
	if err == nil {
		t.Fatal("expected error for non-zero code response")
	}
	if callCount != 1 {
		t.Errorf("expected 1 message API call (no retry for non-401), got %d", callCount)
	}
}

// TestSendPostMessageOnce_TokenFetchFails verifies that a token fetch error
// is properly propagated.
func TestSendPostMessageOnce_TokenFetchFails(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintln(w, `{"code":40014,"msg":"invalid app_id"}`)
	}))
	defer tokenSrv.Close()

	origTokenURL := feishuTokenURL
	feishuTokenURL = tokenSrv.URL
	defer func() { feishuTokenURL = origTokenURL }()

	mgr := NewManager(&model.FeishuConfig{AppID: "bad_id", AppSecret: "bad_secret"})
	err := mgr.sendPostMessageOnce(context.Background(), "ou_test_user", "Title", "Content")
	if err == nil {
		t.Fatal("expected error when token fetch fails")
	}
	if !contains(err.Error(), "get token") {
		t.Errorf("expected error to mention 'get token', got %q", err.Error())
	}
}

// TestSendPostMessageOnce_ConnectionError verifies network errors are wrapped.
func TestSendPostMessageOnce_ConnectionError(t *testing.T) {
	origTokenURL := feishuTokenURL
	feishuTokenURL = "http://127.0.0.1:1" // unreachable port
	defer func() { feishuTokenURL = origTokenURL }()

	mgr := NewManager(&model.FeishuConfig{AppID: "test", AppSecret: "test"})
	// Use short timeout to avoid test hanging
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := mgr.sendPostMessageOnce(ctx, "ou_test_user", "Title", "Content")
	if err == nil {
		t.Fatal("expected error for connection failure")
	}
}

// TestIsTokenError_VariousCases tests the isTokenError function edge cases.
func TestIsTokenError_VariousCases(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"exact 401 sentinel", fmt.Errorf("feishu: token expired (401)"), true},
		{"401 in message", fmt.Errorf("some 401 error"), true},
		{"no 401", fmt.Errorf("connection refused"), false},
		{"empty error", fmt.Errorf(""), false},
		{"code 401 at end", fmt.Errorf("error: HTTP 401"), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isTokenError(tt.err); got != tt.want {
				t.Errorf("isTokenError() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestSendPostMessageOnce_401Response verifies that a 401 HTTP response
// returns the sentinel error that triggers retry.
func TestSendPostMessageOnce_401Response(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintln(w, `{"code":0,"msg":"ok","tenant_access_token":"t-ok","expire":7200}`)
	}))
	defer tokenSrv.Close()

	msgSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
	}))
	defer msgSrv.Close()

	origTokenURL := feishuTokenURL
	feishuTokenURL = tokenSrv.URL
	defer func() { feishuTokenURL = origTokenURL }()

	origMsgURL := feishuMessageURL
	feishuMessageURL = msgSrv.URL
	defer func() { feishuMessageURL = origMsgURL }()

	mgr := NewManager(&model.FeishuConfig{AppID: "test", AppSecret: "test"})
	err := mgr.sendPostMessageOnce(context.Background(), "ou_test_user", "Title", "Content")
	if err == nil {
		t.Fatal("expected error for 401 response")
	}
	if !isTokenError(err) {
		t.Errorf("expected isTokenError to be true for 401 response, got error: %v", err)
	}
}

// TestSendPostMessageOnce_InvalidJSONResponse verifies error handling when
// the message API returns non-JSON.
func TestSendPostMessageOnce_InvalidJSONResponse(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintln(w, `{"code":0,"msg":"ok","tenant_access_token":"t-ok","expire":7200}`)
	}))
	defer tokenSrv.Close()

	msgSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = fmt.Fprint(w, "not json")
	}))
	defer msgSrv.Close()

	origTokenURL := feishuTokenURL
	feishuTokenURL = tokenSrv.URL
	defer func() { feishuTokenURL = origTokenURL }()

	origMsgURL := feishuMessageURL
	feishuMessageURL = msgSrv.URL
	defer func() { feishuMessageURL = origMsgURL }()

	mgr := NewManager(&model.FeishuConfig{AppID: "test", AppSecret: "test"})
	err := mgr.sendPostMessageOnce(context.Background(), "ou_test_user", "Title", "Content")
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
}

// TestSendPostMessage_CancelledContext verifies context cancellation is respected.
func TestSendPostMessage_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	mgr := NewManager(&model.FeishuConfig{AppID: "test", AppSecret: "test"})
	err := mgr.SendPostMessage(ctx, "ou_test_user", "Title", "Content")
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

// contains checks if s contains substr.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || substr == "" ||
		(s != "" && substr != "" && stringContains(s, substr)))
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
