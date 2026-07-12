package dingtalk

import (
	"context"
	"testing"
	"time"
)

func TestTokenInvalidate(t *testing.T) {
	// Set some cached values
	tokenMu.Lock()
	cachedToken = "test-token"
	cachedExp = time.Now().Add(2 * time.Hour)
	tokenMu.Unlock()

	// Invalidate
	InvalidateToken()

	tokenMu.RLock()
	token := cachedToken
	tokenMu.RUnlock()

	if token != "" {
		t.Errorf("expected empty token after invalidation, got %q", token)
	}
}

func TestGetAccessToken_Cached(t *testing.T) {
	// Pre-populate a valid cached token
	mgr := &Manager{}

	tokenMu.Lock()
	cachedToken = "cached-test-token"
	cachedExp = time.Now().Add(2 * time.Hour)
	tokenMu.Unlock()
	defer func() {
		tokenMu.Lock()
		cachedToken = ""
		cachedExp = time.Time{}
		tokenMu.Unlock()
	}()

	token, err := mgr.getAccessToken(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "cached-test-token" {
		t.Errorf("expected cached token, got %q", token)
	}
}
