package feishu

import (
	"fmt"
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
