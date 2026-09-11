package forge

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"time"
)

// ErrRateLimited is returned by Limiter.Wait when the per-host bucket cannot
// admit a request within the caller's context.
var ErrRateLimited = errors.New("forge: rate limited")

// Limiter enforces a per-host request budget with a global concurrency cap, and
// applies exponential backoff (with jitter) when the platform reports a rate
// limit. It exists because a naive fixed-interval poller will exhaust the
// platform quota as soon as several repositories are bound.
type Limiter struct {
	mu      sync.Mutex
	buckets map[string]*tokenBucket
	// sem caps the number of in-flight requests across all hosts.
	sem chan struct{}
	// backoffUntil records a host's cooldown after a 429/403.
	backoffUntil map[string]time.Time
	// consecutive counts consecutive rate-limit hits per host so the backoff
	// can grow; it is reset on success.
	consecutive map[string]int
	// defaultRPS / defaultBurst seed lazily created per-host buckets.
	defaultRPS   float64
	defaultBurst int
	now          func() time.Time
	// randFloat is injectable so jitter is deterministic in tests.
	randFloat func() float64
}

// NewLimiter builds a limiter with the given per-host rate (requests per
// second) and burst, plus a global concurrency cap.
func NewLimiter(perSecond float64, burst, maxConcurrent int) *Limiter {
	if perSecond <= 0 {
		perSecond = 1
	}
	if burst <= 0 {
		burst = 1
	}
	if maxConcurrent <= 0 {
		maxConcurrent = 4
	}
	return &Limiter{
		buckets:      make(map[string]*tokenBucket),
		sem:          make(chan struct{}, maxConcurrent),
		backoffUntil: make(map[string]time.Time),
		consecutive:  make(map[string]int),
		defaultRPS:   perSecond,
		defaultBurst: burst,
		now:          time.Now,
		randFloat:    rand.Float64,
	}
}

// tokenBucket is a classic token bucket refilled continuously.
type tokenBucket struct {
	tokens   float64
	capacity float64
	rate     float64 // tokens per second
	last     time.Time
}

func (b *tokenBucket) take(now time.Time) (wait time.Duration, ok bool) {
	elapsed := now.Sub(b.last).Seconds()
	if elapsed > 0 {
		b.tokens = minFloat(b.capacity, b.tokens+elapsed*b.rate)
		b.last = now
	}
	if b.tokens >= 1 {
		b.tokens--
		return 0, true
	}
	// Time until one token is available.
	needed := (1 - b.tokens) / b.rate
	return time.Duration(needed * float64(time.Second)), false
}

// Wait blocks until the host has budget for a request, the host is out of
// backoff, and a global concurrency slot is free. It returns an error when the
// context is cancelled first.
func (l *Limiter) Wait(ctx context.Context, host string) error {
	// Respect any active backoff first.
	for {
		l.mu.Lock()
		until, hasBackoff := l.backoffUntil[host]
		now := l.now()
		if hasBackoff && now.Before(until) {
			wait := until.Sub(now)
			l.mu.Unlock()
			if err := sleepCtx(ctx, wait); err != nil {
				return err
			}
			continue
		}
		if hasBackoff {
			delete(l.backoffUntil, host)
		}

		bucket := l.buckets[host]
		if bucket == nil {
			// Default budget is filled by the limiter's construction rate; the
			// bucket is created lazily with the same parameters.
			bucket = &tokenBucket{tokens: float64(l.defaultBurst), capacity: float64(l.defaultBurst), rate: l.defaultRPS, last: now}
			l.buckets[host] = bucket
		}
		wait, ok := bucket.take(now)
		l.mu.Unlock()

		if ok {
			break
		}
		if err := sleepCtx(ctx, wait); err != nil {
			return err
		}
	}

	// Acquire a global concurrency slot.
	select {
	case l.sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Release frees a global concurrency slot. It must be called after Wait returns
// nil, typically via defer.
func (l *Limiter) Release() {
	select {
	case <-l.sem:
	default:
	}
}

// ObserveRateLimit puts a host into exponential backoff after a rate-limit
// response. retryAfter (seconds) from the platform wins when provided; otherwise
// the backoff doubles on each consecutive hit, capped, with jitter.
func (l *Limiter) ObserveRateLimit(host string, retryAfterSeconds int) {
	l.mu.Lock()
	defer l.mu.Unlock()

	var wait time.Duration
	if retryAfterSeconds > 0 {
		wait = time.Duration(retryAfterSeconds) * time.Second
	} else {
		l.consecutive[host]++
		base := time.Duration(1<<minInt(l.consecutive[host], 6)) * time.Second // 2s..64s
		// Jitter ±20% to avoid synchronized retries across repos.
		jitter := 1 + (l.randFloat()*0.4 - 0.2)
		wait = time.Duration(float64(base) * jitter)
	}
	// Cap the backoff so a host can never be parked indefinitely.
	if wait > 5*time.Minute {
		wait = 5 * time.Minute
	}
	until := l.now().Add(wait)
	if cur, ok := l.backoffUntil[host]; !ok || until.After(cur) {
		l.backoffUntil[host] = until
	}
}

// ObserveSuccess clears a host's consecutive-failure counter and any active
// backoff: a successful request proves the host is healthy again.
func (l *Limiter) ObserveSuccess(host string) {
	l.mu.Lock()
	delete(l.consecutive, host)
	delete(l.backoffUntil, host)
	l.mu.Unlock()
}

// BackoffRemaining reports how long a host is parked, or zero when it is free.
func (l *Limiter) BackoffRemaining(host string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	until, ok := l.backoffUntil[host]
	if !ok {
		return 0
	}
	if d := until.Sub(l.now()); d > 0 {
		return d
	}
	return 0
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// sleepCtx sleeps for d or until ctx is done.
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// WrapError converts a classified forge error into a limiter observation so the
// caller does not have to inspect the error itself.
func (l *Limiter) WrapError(host string, err error) error {
	if err == nil {
		l.ObserveSuccess(host)
		return nil
	}
	var fe *Error
	if errors.As(err, &fe) {
		if fe.Kind == ErrKindRateLimit {
			l.ObserveRateLimit(host, fe.RetryAfterSeconds)
			return fmt.Errorf("%w: %w", ErrRateLimited, err)
		}
	}
	return err
}
