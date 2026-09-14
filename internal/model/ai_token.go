package model

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"regexp"
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

// aiTokenKey signs AI tokens. It is generated fresh per process and never
// persisted.
//
// Why ephemeral: a token only has to survive from prompt injection until the AI
// issues its first curl — seconds, within one process lifetime. A restart kills
// the AI subprocess that holds the old prompt anyway, so the only consequence
// is that a stale token stops verifying; the injected instructions already tell
// the AI to report 401 and ask the user to re-run the command. Nothing needs to
// be read back at startup, so there is no persistence and no rotation path.
//
// Why not CookieToken: that value is the session cookie the server hands to
// browsers and is written to {DataDir}/cookie-token (whose comment even calls
// it "not secret"). Sharing it as the signing key would mean any cookie leak is
// also the ability to mint AI tokens. A per-process random key has no
// persisted copy at all, so its blast radius is one process lifetime.
var aiTokenKey = GenerateRandomToken(32)

// SignAIToken returns a token valid until now+AITokenTTL.
//
// The token is stateless: it carries its own expiry and an HMAC over it, so
// verification needs no server-side map and no cleanup goroutine.
func SignAIToken(now time.Time) string {
	if aiTokenKey == "" {
		return ""
	}
	exp := now.Add(AITokenTTL).Unix()
	return encodeAIToken(exp, aiTokenKey)
}

// VerifyAIToken reports whether token is well-formed, correctly signed, and
// within its validity window at now.
//
// Every failure mode returns false: empty token, malformed structure, bad
// base64, signature mismatch, expiry, and an expiry implausibly far in the
// future. Signature comparison is constant-time so a caller cannot learn a
// valid signature byte by byte.
func VerifyAIToken(token string, now time.Time) bool {
	// aiTokenKey is always populated by GenerateRandomToken, so the empty-key
	// branch is unreachable today. It is kept as fail-closed defense: without
	// it, an empty key would sign and verify consistently, silently turning
	// "no key configured" into "every token accepted".
	if token == "" || aiTokenKey == "" {
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
	// Upper bound as well as lower. The TTL is a property of SignAIToken, so
	// without this check a holder of the signing key could mint a token valid
	// for years and the "30 minute" limit would be decorative. Clamping here
	// means even a leaked key yields at most one TTL of access.
	if exp > now.Add(AITokenTTL).Unix() {
		return false
	}
	want := aiTokenSignature(exp, aiTokenKey)
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

// aiTokenHeaderPattern matches an injected "X-ClawBench-AI-Token: <value>"
// occurrence so the value can be redacted without the caller knowing it.
var aiTokenHeaderPattern = regexp.MustCompile(regexp.QuoteMeta(AITokenHeader) + `:\s*\S+`)

// RedactAIToken removes any AI token from a string destined for a log.
//
// The token is injected into the AI's prompt as
// "X-ClawBench-AI-Token: <value>", and some backends pass the prompt as a
// command-line argument. Those arguments are logged (internal/ai), so the raw
// token would otherwise land in {LogDir}/logs/ in plaintext — readable by
// anyone who can read the data dir, and valid from loopback for a TTL.
//
// Redaction keys off the header name rather than a caller-supplied value, so a
// new log site cannot leak the token by forgetting to pass it in.
func RedactAIToken(s string) string {
	return aiTokenHeaderPattern.ReplaceAllString(s, AITokenHeader+": [redacted]")
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
