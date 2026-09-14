package model

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strconv"
	"strings"
	"time"
)

const (
	// AITokenHeader is the request header an AI subprocess uses to authenticate
	// against the local HTTP API. A custom header is used instead of a cookie so
	// the token never collides with the real session cookie (clawbench_session)
	// and does not need ScopedCookieName's port prefixing.
	AITokenHeader = "X-ClawBench-AI-Token" //nolint:gosec // G101: a header name, not a credential

	// AITokenTTL bounds how long a signed token stays valid. It is deliberately
	// a fixed constant rather than a config field: the token only needs to
	// survive from prompt injection until the AI issues its first curl, which is
	// seconds. 30 minutes matches defaultACPStallTimeout /
	// defaultStreamIdleTimeout (internal/ai), so a token cannot expire in the
	// middle of a long-running turn.
	AITokenTTL = 30 * time.Minute
)

// aiTokenSeparator splits the encoded expiry from the encoded signature.
const aiTokenSeparator = "."

// SignAIToken returns a token valid until now+AITokenTTL.
//
// The token is stateless: it carries its own expiry and an HMAC over it, so
// verification needs no server-side map and no cleanup goroutine. The signing
// key is CookieToken, which means rotating the cookie token (as the password
// change handler does) invalidates every in-flight AI token at once.
//
// Returns "" when no signing key is configured. A caller must never accept a
// token it could not have produced.
func SignAIToken(now time.Time) string {
	if CookieToken == "" {
		return ""
	}
	exp := now.Add(AITokenTTL).Unix()
	return encodeAIToken(exp, CookieToken)
}

// VerifyAIToken reports whether token is well-formed, correctly signed, and
// not yet expired at now.
//
// Every failure mode returns false: empty or unconfigured signing key, malformed
// structure, bad base64, signature mismatch, and expiry. Signature comparison is
// constant-time so a caller cannot learn a valid signature byte by byte.
func VerifyAIToken(token string, now time.Time) bool {
	if token == "" || CookieToken == "" {
		return false
	}
	expPart, sigPart, ok := strings.Cut(token, aiTokenSeparator)
	if !ok {
		return false
	}
	expRaw, err := base64.RawURLEncoding.DecodeString(expPart)
	if err != nil {
		return false
	}
	exp, err := strconv.ParseInt(string(expRaw), 10, 64)
	if err != nil {
		return false
	}
	// A token is valid strictly before its expiry: at exp == now it is spent.
	if now.Unix() >= exp {
		return false
	}
	want := aiTokenSignature(exp, CookieToken)
	got, err := base64.RawURLEncoding.DecodeString(sigPart)
	if err != nil {
		return false
	}
	return hmac.Equal(got, want)
}

// encodeAIToken builds "base64url(exp).base64url(HMAC(exp))".
func encodeAIToken(exp int64, key string) string {
	expPart := base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(exp, 10)))
	sigPart := base64.RawURLEncoding.EncodeToString(aiTokenSignature(exp, key))
	return expPart + aiTokenSeparator + sigPart
}

// aiTokenSignature returns the raw HMAC over the expiry. The expiry is the only
// signed field, so the token is not bound to a session or user — by design: the
// token is short-lived, loopback-only, and scoped by the project cookie the AI
// sends alongside it.
func aiTokenSignature(exp int64, key string) []byte {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(strconv.FormatInt(exp, 10)))
	return mac.Sum(nil)
}
