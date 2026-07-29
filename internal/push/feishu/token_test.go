package feishu

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"clawbench/internal/model"
)

func TestToken_Cached(t *testing.T) {
	mgr := &Manager{cfg: &model.FeishuConfig{AppID: "test", AppSecret: "test"}}
	mgr.cachedToken = "t-test-token"
	mgr.cachedExp = time.Now().Add(2 * time.Hour)

	token, err := mgr.getAccessToken(testContext(t))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "t-test-token" {
		t.Errorf("expected cached token, got %q", token)
	}
}

func TestToken_Expired(t *testing.T) {
	mgr := &Manager{cfg: &model.FeishuConfig{AppID: "test", AppSecret: "test"}}
	mgr.cachedToken = "t-old-token"
	mgr.cachedExp = time.Now().Add(-10 * time.Minute)

	// Without a real server, this will fail — but that's expected.
	// We just verify invalidateToken works.
	mgr.invalidateToken()
	if mgr.cachedToken != "" {
		t.Error("expected token to be cleared after invalidation")
	}
}

func TestInvalidateToken(t *testing.T) {
	mgr := &Manager{cfg: &model.FeishuConfig{AppID: "test", AppSecret: "test"}}
	mgr.cachedToken = "t-some-token"
	mgr.cachedExp = time.Now().Add(1 * time.Hour)

	mgr.invalidateToken()

	if mgr.cachedToken != "" {
		t.Error("expected token to be empty after invalidation")
	}
	if !mgr.cachedExp.IsZero() {
		t.Error("expected expiration to be zero after invalidation")
	}
}

func TestInvalidateToken_GlobalFallback(t *testing.T) {
	// When no manager is set, global InvalidateToken should not panic
	SetManager(nil)
	InvalidateToken() // should not panic

	// When a manager is set, it should delegate to the instance
	mgr := &Manager{cfg: &model.FeishuConfig{AppID: "test", AppSecret: "test"}}
	mgr.cachedToken = "t-fallback"
	mgr.cachedExp = time.Now().Add(1 * time.Hour)
	SetManager(mgr)
	defer SetManager(nil)

	InvalidateToken()
	if mgr.cachedToken != "" {
		t.Error("expected token to be cleared via global InvalidateToken")
	}
}

func TestIsTokenError(t *testing.T) {
	tests := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{fmt.Errorf("feishu: token expired (401)"), true},
		{fmt.Errorf("feishu: send fetch: connection refused"), false},
		{fmt.Errorf("some 401 error"), true},
	}
	for _, tt := range tests {
		if got := isTokenError(tt.err); got != tt.want {
			t.Errorf("isTokenError(%v) = %v, want %v", tt.err, got, tt.want)
		}
	}
}

// TestGetAccessToken_RefreshWithServer verifies that getAccessToken
// fetches a new token from the server when the cache is empty/expired.
func TestGetAccessToken_RefreshWithServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request method and content type
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintln(w, `{"code":0,"msg":"ok","tenant_access_token":"t-refreshed","expire":7200}`)
	}))
	defer srv.Close()

	origURL := feishuTokenURL
	feishuTokenURL = srv.URL
	defer func() { feishuTokenURL = origURL }()

	mgr := NewManager(&model.FeishuConfig{AppID: "test", AppSecret: "test"})
	// No cached token → should fetch from server
	token, err := mgr.getAccessToken(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "t-refreshed" {
		t.Errorf("expected t-refreshed, got %q", token)
	}
	// Verify token was cached
	if mgr.cachedToken != "t-refreshed" {
		t.Errorf("expected cached token t-refreshed, got %q", mgr.cachedToken)
	}
	// Verify expiration was set
	if mgr.cachedExp.IsZero() {
		t.Error("expected cached expiration to be set")
	}
}

// TestGetAccessToken_ExpiredTokenRefreshes verifies that an expired cached token
// triggers a refresh from the server.
func TestGetAccessToken_ExpiredTokenRefreshes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintln(w, `{"code":0,"msg":"ok","tenant_access_token":"t-new","expire":3600}`)
	}))
	defer srv.Close()

	origURL := feishuTokenURL
	feishuTokenURL = srv.URL
	defer func() { feishuTokenURL = origURL }()

	mgr := NewManager(&model.FeishuConfig{AppID: "test", AppSecret: "test"})
	// Set expired cached token
	mgr.cachedToken = "t-old"
	mgr.cachedExp = time.Now().Add(-1 * time.Hour)

	token, err := mgr.getAccessToken(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "t-new" {
		t.Errorf("expected refreshed token t-new, got %q", token)
	}
}

// TestGetAccessToken_DefaultExpiry verifies that when the server returns
// expire=0, the default 2-hour expiry is used.
func TestGetAccessToken_DefaultExpiry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintln(w, `{"code":0,"msg":"ok","tenant_access_token":"t-default-exp","expire":0}`)
	}))
	defer srv.Close()

	origURL := feishuTokenURL
	feishuTokenURL = srv.URL
	defer func() { feishuTokenURL = origURL }()

	mgr := NewManager(&model.FeishuConfig{AppID: "test", AppSecret: "test"})
	token, err := mgr.getAccessToken(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "t-default-exp" {
		t.Errorf("expected t-default-exp, got %q", token)
	}
	// Expiry should be approximately 2 hours from now
	expDiff := time.Until(mgr.cachedExp)
	if expDiff < 1*time.Hour || expDiff > 3*time.Hour {
		t.Errorf("expected expiry ~2h, got %v", expDiff)
	}
}

// TestGetAccessToken_ServerError verifies that an error code from the server
// is properly returned.
func TestGetAccessToken_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintln(w, `{"code":40014,"msg":"invalid app_id"}`)
	}))
	defer srv.Close()

	origURL := feishuTokenURL
	feishuTokenURL = srv.URL
	defer func() { feishuTokenURL = origURL }()

	mgr := NewManager(&model.FeishuConfig{AppID: "bad", AppSecret: "bad"})
	_, err := mgr.getAccessToken(context.Background())
	if err == nil {
		t.Fatal("expected error for server error response")
	}
	if mgr.cachedToken != "" {
		t.Error("expected cached token to be empty on error")
	}
}

// TestGetAccessToken_ConnectionError verifies network errors are propagated.
func TestGetAccessToken_ConnectionError(t *testing.T) {
	origURL := feishuTokenURL
	feishuTokenURL = "http://127.0.0.1:1" // unreachable
	defer func() { feishuTokenURL = origURL }()

	mgr := NewManager(&model.FeishuConfig{AppID: "test", AppSecret: "test"})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := mgr.getAccessToken(ctx)
	if err == nil {
		t.Fatal("expected error for connection failure")
	}
}

// TestGetAccessToken_InvalidJSONResponse verifies error handling when
// the server returns non-JSON.
func TestGetAccessToken_InvalidJSONResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = fmt.Fprint(w, "not json")
	}))
	defer srv.Close()

	origURL := feishuTokenURL
	feishuTokenURL = srv.URL
	defer func() { feishuTokenURL = origURL }()

	mgr := NewManager(&model.FeishuConfig{AppID: "test", AppSecret: "test"})
	_, err := mgr.getAccessToken(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
}

// TestGetAccessToken_CacheNearExpiry verifies that a token near expiry
// (within tokenRefreshBuffer) is refreshed.
func TestGetAccessToken_CacheNearExpiry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintln(w, `{"code":0,"msg":"ok","tenant_access_token":"t-early-refresh","expire":7200}`)
	}))
	defer srv.Close()

	origURL := feishuTokenURL
	feishuTokenURL = srv.URL
	defer func() { feishuTokenURL = origURL }()

	mgr := NewManager(&model.FeishuConfig{AppID: "test", AppSecret: "test"})
	// Set token that expires within the 5-minute buffer
	mgr.cachedToken = "t-near-expiry"
	mgr.cachedExp = time.Now().Add(3 * time.Minute) // less than tokenRefreshBuffer (5 min)

	token, err := mgr.getAccessToken(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "t-early-refresh" {
		t.Errorf("expected early refresh token, got %q", token)
	}
}
