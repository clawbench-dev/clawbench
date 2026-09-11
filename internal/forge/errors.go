package forge

import (
	"errors"
	"fmt"
)

// ErrorKind classifies a forge error so callers (UI, poller) can react
// appropriately — e.g. "go fix your token" vs "wait and retry".
type ErrorKind string

const (
	// ErrKindAuth means the credential is missing, invalid, expired or lacks
	// scope. The user must act.
	ErrKindAuth ErrorKind = "auth"
	// ErrKindRateLimit means the platform throttled the request. Retry later.
	ErrKindRateLimit ErrorKind = "rate_limit"
	// ErrKindNotFound means the item/repo does not exist or is not visible to
	// the credential.
	ErrKindNotFound ErrorKind = "not_found"
	// ErrKindNetwork means a transport-level failure (DNS, dial, TLS, timeout).
	ErrKindNetwork ErrorKind = "network"
	// ErrKindServer means the platform returned 5xx.
	ErrKindServer ErrorKind = "server"
	// ErrKindUnsupported means the platform cannot satisfy the request.
	ErrKindUnsupported ErrorKind = "unsupported"
	// ErrKindUnknown is the fallback.
	ErrKindUnknown ErrorKind = "unknown"
)

// Error is a classified forge error.
type Error struct {
	Kind ErrorKind
	// Status is the HTTP status code when applicable (0 otherwise).
	Status int
	// RetryAfterSeconds is set when the platform supplied a Retry-After hint.
	RetryAfterSeconds int
	// Message is a human-readable description.
	Message string
	// Err is the wrapped cause.
	Err error
}

func (e *Error) Error() string {
	if e.Status != 0 {
		return fmt.Sprintf("forge %s error (status %d): %s", e.Kind, e.Status, e.Message)
	}
	return fmt.Sprintf("forge %s error: %s", e.Kind, e.Message)
}

func (e *Error) Unwrap() error { return e.Err }

// ClassifyStatus maps an HTTP status to an ErrorKind.
func ClassifyStatus(status int) ErrorKind {
	switch {
	case status == 401 || status == 403:
		// GitHub/GitLab both use 403 for rate limiting as well as permission
		// denial; callers that have header access should prefer the explicit
		// rate-limit signal (see NewRateLimitError).
		return ErrKindAuth
	case status == 404:
		return ErrKindNotFound
	case status == 429:
		return ErrKindRateLimit
	case status >= 500:
		return ErrKindServer
	default:
		return ErrKindUnknown
	}
}

// NewHTTPError builds a classified error from an HTTP status.
func NewHTTPError(status int, message string, cause error) *Error {
	return &Error{
		Kind:    ClassifyStatus(status),
		Status:  status,
		Message: message,
		Err:     cause,
	}
}

// NewRateLimitError builds a rate-limit error with an optional Retry-After hint.
func NewRateLimitError(status, retryAfterSeconds int, message string) *Error {
	return &Error{
		Kind:              ErrKindRateLimit,
		Status:            status,
		RetryAfterSeconds: retryAfterSeconds,
		Message:           message,
	}
}

// IsAuthError reports whether err is an authentication/authorization failure.
func IsAuthError(err error) bool {
	var fe *Error
	return errors.As(err, &fe) && fe.Kind == ErrKindAuth
}

// IsRateLimitError reports whether err is a rate-limit failure.
func IsRateLimitError(err error) bool {
	var fe *Error
	return errors.As(err, &fe) && fe.Kind == ErrKindRateLimit
}

// IsNotFoundError reports whether err is a not-found failure.
func IsNotFoundError(err error) bool {
	var fe *Error
	return errors.As(err, &fe) && fe.Kind == ErrKindNotFound
}
