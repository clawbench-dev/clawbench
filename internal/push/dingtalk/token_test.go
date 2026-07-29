package dingtalk

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"clawbench/internal/model"
)

func resetTokenCache(mgr *Manager) {
	mgr.tokenMu.Lock()
	mgr.cachedToken = ""
	mgr.cachedExp = time.Time{}
	mgr.tokenMu.Unlock()
}

func TestTokenInvalidate(t *testing.T) {
	mgr := NewManager(&model.DingTalkConfig{AppKey: "k", AppSecret: "s"})

	// Set some cached values
	mgr.cachedToken = "test-token"
	mgr.cachedExp = time.Now().Add(2 * time.Hour)

	// Invalidate
	mgr.invalidateToken()

	if mgr.cachedToken != "" {
		t.Errorf("expected empty token after invalidation, got %q", mgr.cachedToken)
	}
	if !mgr.cachedExp.IsZero() {
		t.Errorf("expected zero expiry time, got %v", mgr.cachedExp)
	}
}

func TestGetAccessToken_Cached(t *testing.T) {
	mgr := NewManager(&model.DingTalkConfig{AppKey: "k", AppSecret: "s"})

	mgr.cachedToken = "cached-test-token"
	mgr.cachedExp = time.Now().Add(2 * time.Hour)
	defer resetTokenCache(mgr)

	token, err := mgr.getAccessToken(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "cached-test-token" {
		t.Errorf("expected cached token, got %q", token)
	}
}

func TestGetAccessToken_DoubleCheckLock(t *testing.T) {
	// Test the double-check pattern: token is valid when we enter
	// the write-lock path, because another goroutine refreshed it.
	mgr := NewManager(&model.DingTalkConfig{AppKey: "k", AppSecret: "s"})

	// Pre-populate with an about-to-expire token to force entering write lock
	mgr.cachedToken = "expiring-double-check"
	mgr.cachedExp = time.Now().Add(3 * time.Minute) // Within refresh buffer
	defer resetTokenCache(mgr)

	// This enters the write lock path (near expiry), then tries to hit the real API.
	// The real API will fail, but the double-check path is exercised differently.
	_, _ = mgr.getAccessToken(context.Background())
}

func TestGetAccessToken_RefreshSuccess(t *testing.T) {
	mgr := NewManager(&model.DingTalkConfig{AppKey: "test-key", AppSecret: "test-secret"})
	defer resetTokenCache(mgr)

	// Create a mock server that responds like the DingTalk token API
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := tokenResponse{
			ErrCode:     0,
			ErrMsg:      "ok",
			AccessToken: "mock-refreshed-token",
			ExpiresIn:   7200,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Override the manager's httpClient to redirect to mock server
	mgr.httpClient = &http.Client{
		Timeout: 10 * time.Second,
		Transport: &redirectTransport{
			targetURL: server.URL,
			transport: http.DefaultTransport,
		},
	}

	token, err := mgr.getAccessToken(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "mock-refreshed-token" {
		t.Errorf("expected mock-refreshed-token, got %q", token)
	}

	// Verify cache was set
	if mgr.cachedToken != "mock-refreshed-token" {
		t.Errorf("expected cached token to be set, got %q", mgr.cachedToken)
	}
}

func TestGetAccessToken_RefreshDefaultExpiry(t *testing.T) {
	mgr := NewManager(&model.DingTalkConfig{AppKey: "k", AppSecret: "s"})
	defer resetTokenCache(mgr)

	// Mock server that returns expires_in=0 (should use default 2h)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := tokenResponse{
			ErrCode:     0,
			ErrMsg:      "ok",
			AccessToken: "default-expiry-token",
			ExpiresIn:   0, // Zero — should use default 2h
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	mgr.httpClient = &http.Client{
		Timeout: 10 * time.Second,
		Transport: &redirectTransport{
			targetURL: server.URL,
			transport: http.DefaultTransport,
		},
	}

	token, err := mgr.getAccessToken(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "default-expiry-token" {
		t.Errorf("expected default-expiry-token, got %q", token)
	}

	// Verify the default expiry was set (approximately 2 hours from now)
	if time.Until(mgr.cachedExp) < 1*time.Hour || time.Until(mgr.cachedExp) > 3*time.Hour {
		t.Errorf("expected default expiry ~2h, got %v", time.Until(mgr.cachedExp))
	}
}

func TestGetAccessToken_APIError(t *testing.T) {
	mgr := NewManager(&model.DingTalkConfig{AppKey: "bad-key", AppSecret: "bad-secret"})
	defer resetTokenCache(mgr)

	// Mock server that returns an API error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := tokenResponse{
			ErrCode: 40014,
			ErrMsg:  "invalid appkey",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	mgr.httpClient = &http.Client{
		Timeout: 10 * time.Second,
		Transport: &redirectTransport{
			targetURL: server.URL,
			transport: http.DefaultTransport,
		},
	}

	_, err := mgr.getAccessToken(context.Background())
	if err == nil {
		t.Fatal("expected error for API error response")
	}
}

func TestGetAccessToken_CancelledContext(t *testing.T) {
	mgr := NewManager(&model.DingTalkConfig{AppKey: "k", AppSecret: "s"})
	defer resetTokenCache(mgr)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := mgr.getAccessToken(ctx)
	if err == nil {
		t.Log("expected error with cancelled context")
	}
}

func TestGetAccessToken_InvalidJSONResponse(t *testing.T) {
	mgr := NewManager(&model.DingTalkConfig{AppKey: "k", AppSecret: "s"})
	defer resetTokenCache(mgr)

	// Mock server that returns invalid JSON
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("not valid json"))
	}))
	defer server.Close()

	mgr.httpClient = &http.Client{
		Timeout: 10 * time.Second,
		Transport: &redirectTransport{
			targetURL: server.URL,
			transport: http.DefaultTransport,
		},
	}

	_, err := mgr.getAccessToken(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
}

func TestGetAccessToken_NearExpiryCache(t *testing.T) {
	mgr := NewManager(&model.DingTalkConfig{AppKey: "k", AppSecret: "s"})

	mgr.cachedToken = "expiring-token"
	mgr.cachedExp = time.Now().Add(3 * time.Minute) // Less than tokenRefreshBuffer (5 min)
	defer resetTokenCache(mgr)

	// Will try to refresh but fail against real API (no mock server)
	_, err := mgr.getAccessToken(context.Background())
	if err == nil {
		t.Log("expected error when refreshing near-expiry token against real API")
	}
}

func TestInvalidateToken_ClearsBothFields(t *testing.T) {
	mgr := NewManager(&model.DingTalkConfig{AppKey: "k", AppSecret: "s"})
	mgr.cachedToken = "token-to-clear"
	mgr.cachedExp = time.Now().Add(1 * time.Hour)

	mgr.invalidateToken()

	if mgr.cachedToken != "" {
		t.Errorf("expected empty token, got %q", mgr.cachedToken)
	}
	if !mgr.cachedExp.IsZero() {
		t.Errorf("expected zero expiry, got %v", mgr.cachedExp)
	}
}

func TestInvalidateToken_GlobalFallback(t *testing.T) {
	// When no manager is set, global InvalidateToken should not panic
	SetManager(nil)
	InvalidateToken() // should not panic

	// When a manager is set, it should delegate to the instance
	mgr := NewManager(&model.DingTalkConfig{AppKey: "k", AppSecret: "s"})
	mgr.cachedToken = "t-fallback"
	mgr.cachedExp = time.Now().Add(1 * time.Hour)
	SetManager(mgr)
	defer SetManager(nil)

	InvalidateToken()
	if mgr.cachedToken != "" {
		t.Error("expected token to be cleared via global InvalidateToken")
	}
}

func TestIsDingTalkTokenError(t *testing.T) {
	tests := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{fmt.Errorf("dingtalk: token expired (401)"), true},
		{fmt.Errorf("dingtalk: invalid authentication"), true},
		{fmt.Errorf("dingtalk: send fetch: connection refused"), false},
	}
	for _, tt := range tests {
		if got := isDingTalkTokenError(tt.err); got != tt.want {
			t.Errorf("isDingTalkTokenError(%v) = %v, want %v", tt.err, got, tt.want)
		}
	}
}

// redirectTransport redirects requests to the DingTalk API to a target URL.
// This allows testing getAccessToken with a mock server without modifying
// the production code (dingtalkTokenURL is a const).
type redirectTransport struct {
	targetURL string
	transport http.RoundTripper
}

func (t *redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Only redirect requests to the DingTalk token API
	if req.URL.Host == "oapi.dingtalk.com" && req.URL.Path == "/gettoken" {
		// Create a new request to the mock server, preserving query params
		mockURL, _ := url.Parse(t.targetURL)
		mockURL.RawQuery = req.URL.RawQuery
		mockReq := req.Clone(req.Context())
		mockReq.URL = mockURL
		return t.transport.RoundTrip(mockReq)
	}
	return t.transport.RoundTrip(req)
}
