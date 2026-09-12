package forge

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClassifyStatus(t *testing.T) {
	tests := map[int]ErrorKind{
		401: ErrKindAuth,
		403: ErrKindAuth,
		404: ErrKindNotFound,
		429: ErrKindRateLimit,
		500: ErrKindServer,
		503: ErrKindServer,
		400: ErrKindUnknown,
	}
	for status, want := range tests {
		assert.Equal(t, want, ClassifyStatus(status), "status %d", status)
	}
}

func TestNewHTTPError(t *testing.T) {
	cause := errors.New("boom")
	err := NewHTTPError(404, "not found", cause)
	assert.Equal(t, ErrKindNotFound, err.Kind)
	assert.Equal(t, 404, err.Status)
	assert.Equal(t, "not found", err.Message)
	assert.ErrorIs(t, err, cause, "must wrap the cause")
	assert.Contains(t, err.Error(), "status 404")
	assert.Contains(t, err.Error(), "not_found")
}

func TestNewHTTPError_NoStatusInMessage(t *testing.T) {
	err := NewHTTPError(0, "transport failure", nil)
	assert.NotContains(t, err.Error(), "status", "status 0 must not appear")
	assert.Contains(t, err.Error(), "transport failure")
}

func TestNewRateLimitError(t *testing.T) {
	err := NewRateLimitError(429, 42, "slow down")
	assert.Equal(t, ErrKindRateLimit, err.Kind)
	assert.Equal(t, 42, err.RetryAfterSeconds)
	assert.True(t, IsRateLimitError(err))
	assert.False(t, IsAuthError(err))
}

func TestErrorPredicates(t *testing.T) {
	authErr := &Error{Kind: ErrKindAuth}
	rlErr := &Error{Kind: ErrKindRateLimit}
	nfErr := &Error{Kind: ErrKindNotFound}

	assert.True(t, IsAuthError(authErr))
	assert.False(t, IsAuthError(rlErr))
	assert.True(t, IsRateLimitError(rlErr))
	assert.False(t, IsRateLimitError(authErr))
	assert.True(t, IsNotFoundError(nfErr))
	assert.False(t, IsNotFoundError(authErr))
}

func TestErrorPredicates_Wrapped(t *testing.T) {
	// Predicates must see through wrapping so callers can wrap freely.
	wrapped := fmt.Errorf("context: %w", &Error{Kind: ErrKindAuth})
	assert.True(t, IsAuthError(wrapped))
}

func TestErrorPredicates_NonForgeError(t *testing.T) {
	plain := errors.New("just an error")
	assert.False(t, IsAuthError(plain))
	assert.False(t, IsRateLimitError(plain))
	assert.False(t, IsNotFoundError(plain))
}

func TestErrorUnwrap(t *testing.T) {
	cause := errors.New("root")
	err := &Error{Kind: ErrKindNetwork, Message: "m", Err: cause}
	require.ErrorIs(t, err, cause)
}
