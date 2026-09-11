package forge

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLimiter_AdmitsUpToBurstImmediately(t *testing.T) {
	l := NewLimiter(1, 3, 4) // 1 rps, burst 3
	l.now = func() time.Time { return time.Unix(0, 0) }

	ctx := context.Background()
	for i := 0; i < 3; i++ {
		require.NoError(t, l.Wait(ctx, "github.com"), "burst slot %d must be admitted", i)
		l.Release()
	}
}

func TestLimiter_BlocksWhenBucketEmpty(t *testing.T) {
	l := NewLimiter(1, 1, 4)
	now := time.Unix(0, 0)
	l.now = func() time.Time { return now }

	ctx := context.Background()
	require.NoError(t, l.Wait(ctx, "github.com"))
	l.Release()

	// The next request must wait; with a cancelled context it returns promptly
	// rather than blocking forever.
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	err := l.Wait(cancelled, "github.com")
	assert.Error(t, err, "an empty bucket must block and honour context cancellation")
}

func TestLimiter_GlobalConcurrencyCap(t *testing.T) {
	l := NewLimiter(100, 100, 1) // ample tokens, concurrency 1
	l.now = func() time.Time { return time.Unix(0, 0) }

	ctx := context.Background()
	require.NoError(t, l.Wait(ctx, "host-a"))
	// The single slot is held; a second Wait must block.
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	assert.Error(t, l.Wait(cancelled, "host-b"), "the concurrency cap must block a second in-flight request")

	// After releasing, the slot is free again.
	l.Release()
	require.NoError(t, l.Wait(ctx, "host-b"))
	l.Release()
}

func TestLimiter_ObserveRateLimitWithRetryAfter(t *testing.T) {
	l := NewLimiter(100, 100, 4)
	base := time.Unix(1000, 0)
	l.now = func() time.Time { return base }

	l.ObserveRateLimit("github.com", 30)

	remaining := l.BackoffRemaining("github.com")
	assert.Equal(t, 30*time.Second, remaining, "Retry-After must set the backoff window")

	// A different host is unaffected.
	assert.Zero(t, l.BackoffRemaining("gitlab.com"))
}

func TestLimiter_ObserveRateLimitWithoutRetryAfterGrowsBackoff(t *testing.T) {
	l := NewLimiter(100, 100, 4)
	base := time.Unix(1000, 0)
	l.now = func() time.Time { return base }
	l.randFloat = func() float64 { return 0.5 } // no jitter

	l.ObserveRateLimit("h", 0)
	first := l.BackoffRemaining("h")
	assert.Greater(t, first, time.Duration(0))

	l.ObserveRateLimit("h", 0)
	second := l.BackoffRemaining("h")
	assert.Greater(t, second, first, "consecutive rate limits must back off further")
}

func TestLimiter_ObserveSuccessResetsBackoffCounter(t *testing.T) {
	l := NewLimiter(100, 100, 4)
	base := time.Unix(1000, 0)
	l.now = func() time.Time { return base }
	l.randFloat = func() float64 { return 0.5 }

	l.ObserveRateLimit("h", 0)
	l.ObserveRateLimit("h", 0)
	grown := l.BackoffRemaining("h")

	l.ObserveSuccess("h")
	l.ObserveRateLimit("h", 0)
	assert.Less(t, l.BackoffRemaining("h"), grown, "a success must reset the backoff growth")
}

func TestLimiter_BackoffCapped(t *testing.T) {
	l := NewLimiter(100, 100, 4)
	base := time.Unix(1000, 0)
	l.now = func() time.Time { return base }
	l.randFloat = func() float64 { return 1.0 } // max jitter

	// A huge Retry-After must still be capped.
	l.ObserveRateLimit("h", 100000)
	assert.LessOrEqual(t, l.BackoffRemaining("h"), 5*time.Minute)
}

func TestLimiter_WaitHonoursBackoff(t *testing.T) {
	l := NewLimiter(100, 100, 4)
	base := time.Unix(1000, 0)
	current := base
	l.now = func() time.Time { return current }

	l.ObserveRateLimit("h", 60)

	// While parked, a Wait with an already-cancelled context must not proceed.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.Error(t, l.Wait(ctx, "h"), "a host in backoff must not be admitted")

	// Once the backoff expires it is admitted.
	current = base.Add(61 * time.Second)
	require.NoError(t, l.Wait(context.Background(), "h"))
	l.Release()
}

func TestLimiter_WrapError(t *testing.T) {
	l := NewLimiter(100, 100, 4)
	l.now = func() time.Time { return time.Unix(1000, 0) }

	// A rate-limit error parks the host and wraps the error.
	err := l.WrapError("h", NewRateLimitError(429, 10, "slow down"))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrRateLimited)
	assert.Greater(t, l.BackoffRemaining("h"), time.Duration(0))

	// A non-rate-limit error is passed through untouched.
	plain := assert.AnError
	assert.Equal(t, plain, l.WrapError("h2", plain))

	// Success clears state.
	assert.NoError(t, l.WrapError("h3", nil))
}
