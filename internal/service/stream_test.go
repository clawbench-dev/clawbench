package service

import (
	"sync"
	"testing"
	"time"

	"clawbench/internal/ai"

	"github.com/stretchr/testify/assert"
)

func TestRegisterAndGetSessionStream(t *testing.T) {
	cleanupStreams()
	sessionID := "test-session-1"

	ch := RegisterSessionStream(sessionID)

	if ch == nil {
		t.Fatal("expected non-nil channel")
	}

	// Should be able to get the same channel
	gotCh, ok := GetSessionStream(sessionID)
	if !ok {
		t.Fatal("expected to find stream for session")
	}

	// Write to original and read from got
	go func() {
		ch <- ai.StreamEvent{Type: "content", Content: "hello"}
	}()

	event := <-gotCh
	if event.Type != "content" || event.Content != "hello" {
		t.Errorf("unexpected event: %+v", event)
	}

	// Cleanup
	UnregisterSessionStream(sessionID)

	// Should no longer be available
	_, ok = GetSessionStream(sessionID)
	if ok {
		t.Fatal("expected stream to be unregistered")
	}
}

func TestGetSessionStream_NotFound(t *testing.T) {
	cleanupStreams()
	_, ok := GetSessionStream("nonexistent")
	if ok {
		t.Fatal("expected not to find stream for nonexistent session")
	}
}

func TestUnregisterSessionStream_ClosesChannel(t *testing.T) {
	cleanupStreams()
	sessionID := "test-session-close"

	ch := RegisterSessionStream(sessionID)
	UnregisterSessionStream(sessionID)

	// Reading from closed channel should return zero value with ok=false
	_, ok := <-ch
	if ok {
		t.Fatal("expected channel to be closed")
	}
}

func TestTryClaimSSEStream(t *testing.T) {
	cleanupStreams()
	defer cleanupStreams()

	sessionID := "test-claim"
	RegisterSessionStream(sessionID)
	defer UnregisterSessionStream(sessionID)

	// First claim should succeed
	assert.True(t, TryClaimSSEStream(sessionID), "first claim should succeed")

	// Second claim should fail (already claimed)
	assert.False(t, TryClaimSSEStream(sessionID), "second claim should fail")

	// Release the claim
	ReleaseSSEStream(sessionID)

	// Third claim should succeed after release
	assert.True(t, TryClaimSSEStream(sessionID), "claim after release should succeed")

	ReleaseSSEStream(sessionID)
}

func TestUnregisterSessionStream_ReleasesClaim(t *testing.T) {
	cleanupStreams()
	defer cleanupStreams()

	sessionID := "test-unregister-claim"
	RegisterSessionStream(sessionID)

	// Claim the stream
	assert.True(t, TryClaimSSEStream(sessionID))

	// Unregister should also release the claim
	UnregisterSessionStream(sessionID)

	// Re-register and claim should work
	RegisterSessionStream(sessionID)
	defer UnregisterSessionStream(sessionID)
	assert.True(t, TryClaimSSEStream(sessionID), "claim after re-register should succeed")
	ReleaseSSEStream(sessionID)
}

func TestSSEClaim_DoesNotBlockEventDelivery(t *testing.T) {
	cleanupStreams()
	defer cleanupStreams()

	sessionID := "test-claim-events"
	ch := RegisterSessionStream(sessionID)
	defer UnregisterSessionStream(sessionID)

	subCh, unsub, ok := SubscribeSessionStream(sessionID)
	assert.True(t, ok)
	defer unsub()

	// Claim and send event
	assert.True(t, TryClaimSSEStream(sessionID))
	ch <- ai.StreamEvent{Type: "content", Content: "hello"}

	select {
	case event := <-subCh:
		assert.Equal(t, "content", event.Type)
		assert.Equal(t, "hello", event.Content)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}

	ReleaseSSEStream(sessionID)
}

func TestTryClaimSSEStream_Concurrent(t *testing.T) {
	cleanupStreams()
	defer cleanupStreams()

	sessionID := "test-concurrent-claim"
	RegisterSessionStream(sessionID)
	defer UnregisterSessionStream(sessionID)

	// Multiple goroutines try to claim — exactly one should succeed
	var wg sync.WaitGroup
	claimCount := 0
	var mu sync.Mutex

	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if TryClaimSSEStream(sessionID) {
				mu.Lock()
				claimCount++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	assert.Equal(t, 1, claimCount, "exactly one goroutine should claim the stream")
	ReleaseSSEStream(sessionID)
}

func TestTryClaimSSEStream_DifferentSessions(t *testing.T) {
	cleanupStreams()
	defer cleanupStreams()

	RegisterSessionStream("session-a")
	defer UnregisterSessionStream("session-a")
	RegisterSessionStream("session-b")
	defer UnregisterSessionStream("session-b")

	// Each session can be claimed independently
	assert.True(t, TryClaimSSEStream("session-a"))
	assert.True(t, TryClaimSSEStream("session-b"))
	assert.False(t, TryClaimSSEStream("session-a")) // already claimed

	ReleaseSSEStream("session-a")
	ReleaseSSEStream("session-b")
}

func TestSubscribeSessionStream_FanOutMultipleClients(t *testing.T) {
	cleanupStreams()
	defer cleanupStreams()

	sessionID := "test-fanout"
	producer := RegisterSessionStream(sessionID)
	defer UnregisterSessionStream(sessionID)

	ch1, unsub1, ok1 := SubscribeSessionStream(sessionID)
	assert.True(t, ok1)
	defer unsub1()
	ch2, unsub2, ok2 := SubscribeSessionStream(sessionID)
	assert.True(t, ok2)
	defer unsub2()

	producer <- ai.StreamEvent{Type: "thinking", Content: "shared"}
	producer <- ai.StreamEvent{Type: "done"}

	deadline := time.After(2 * time.Second)
	gotThinking := func(ch <-chan ai.StreamEvent) bool {
		for {
			select {
			case ev, ok := <-ch:
				if !ok {
					return false
				}
				if ev.Type == "thinking" && ev.Content == "shared" {
					return true
				}
			case <-deadline:
				return false
			}
		}
	}
	assert.True(t, gotThinking(ch1), "first subscriber should receive thinking")
	assert.True(t, gotThinking(ch2), "second subscriber should receive thinking")
}

func cleanupStreams() {
	sessionStreams.Range(func(key, _ interface{}) bool {
		sessionStreams.Delete(key)
		return true
	})
	sessionSSEClaim.Range(func(key, _ interface{}) bool {
		sessionSSEClaim.Delete(key)
		return true
	})
}
