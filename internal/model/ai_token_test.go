package model

import (
	"encoding/base64"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withAITokenKey swaps the process signing key for the duration of a test, so
// tests can exercise key mismatch and the empty-key guard without leaking state.
func withAITokenKey(t *testing.T, key string) {
	t.Helper()
	orig := aiTokenKey
	aiTokenKey = key
	t.Cleanup(func() { aiTokenKey = orig })
}

func TestSignAIToken_RoundTrips(t *testing.T) {
	withAITokenKey(t, "signing-key")
	now := time.Unix(1_700_000_000, 0)

	token := SignAIToken(now)
	require.NotEmpty(t, token)

	assert.True(t, VerifyAIToken(token, now))
}

func TestSignAIToken_ValidJustBeforeExpiry(t *testing.T) {
	withAITokenKey(t, "signing-key")
	now := time.Unix(1_700_000_000, 0)

	token := SignAIToken(now)

	// One second before the deadline the token must still be accepted; the TTL
	// has to actually cover the AI's window, not end early.
	justBefore := now.Add(AITokenTTL - time.Second)
	assert.True(t, VerifyAIToken(token, justBefore))
}

func TestVerifyAIToken_ExpiredRejected(t *testing.T) {
	withAITokenKey(t, "signing-key")
	now := time.Unix(1_700_000_000, 0)

	token := SignAIToken(now)

	assert.False(t, VerifyAIToken(token, now.Add(AITokenTTL)))
}

func TestVerifyAIToken_AtExactExpiryRejected(t *testing.T) {
	// The boundary matters: exp == now must be spent, not valid. A token is
	// valid strictly before its expiry.
	withAITokenKey(t, "signing-key")
	now := time.Unix(1_700_000_000, 0)
	exp := now.Add(AITokenTTL).Unix()

	token := encodeAIToken(exp, aiTokenKey)

	assert.False(t, VerifyAIToken(token, time.Unix(exp, 0)))
	assert.True(t, VerifyAIToken(token, time.Unix(exp-1, 0)))
}

func TestVerifyAIToken_TamperedSignatureRejected(t *testing.T) {
	withAITokenKey(t, "signing-key")
	now := time.Unix(1_700_000_000, 0)

	token := SignAIToken(now)
	expPart, sigPart, ok := strings.Cut(token, aiTokenSeparator)
	require.True(t, ok)

	// Flip one byte of the signature.
	sig, err := base64.RawURLEncoding.DecodeString(sigPart)
	require.NoError(t, err)
	sig[0] ^= 0xFF
	tampered := expPart + aiTokenSeparator + base64.RawURLEncoding.EncodeToString(sig)

	assert.False(t, VerifyAIToken(tampered, now))
}

func TestVerifyAIToken_TamperedExpiryRejected(t *testing.T) {
	// The core forgery attempt: keep a valid signature but extend the deadline.
	withAITokenKey(t, "signing-key")
	now := time.Unix(1_700_000_000, 0)

	token := SignAIToken(now)
	_, sigPart, ok := strings.Cut(token, aiTokenSeparator)
	require.True(t, ok)

	extended := now.Add(AITokenTTL * 100).Unix()
	forged := base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(extended, 10))) +
		aiTokenSeparator + sigPart

	assert.False(t, VerifyAIToken(forged, now))
}

func TestVerifyAIToken_KeyChangeInvalidatesInFlightTokens(t *testing.T) {
	// The key is per-process. Anything that replaces it — a restart, or an
	// explicit swap — must invalidate tokens already handed out.
	withAITokenKey(t, "old-key")
	now := time.Unix(1_700_000_000, 0)
	token := SignAIToken(now)
	require.True(t, VerifyAIToken(token, now))

	aiTokenKey = "new-key"
	assert.False(t, VerifyAIToken(token, now))
}

func TestSignAIToken_NoKeyReturnsEmpty(t *testing.T) {
	// With no configured key there is nothing to sign with, so signing must
	// fail closed rather than emit a token anyone could forge.
	withAITokenKey(t, "")
	now := time.Unix(1_700_000_000, 0)

	assert.Empty(t, SignAIToken(now))
}

func TestVerifyAIToken_NoKeyRejectsEverything(t *testing.T) {
	// A token signed under a real key must not verify once the key is cleared.
	withAITokenKey(t, "signing-key")
	now := time.Unix(1_700_000_000, 0)
	token := SignAIToken(now)
	require.NotEmpty(t, token)

	aiTokenKey = ""
	assert.False(t, VerifyAIToken(token, now))
}

func TestVerifyAIToken_ForgedWithEmptyKeyRejected(t *testing.T) {
	// The attack the empty-key guard exists for: an attacker knows the format,
	// so with no configured key they can compute the HMAC themselves. Accepting
	// an empty key would make every token forgeable.
	withAITokenKey(t, "")
	now := time.Unix(1_700_000_000, 0)

	forged := encodeAIToken(now.Add(AITokenTTL).Unix(), "")

	assert.False(t, VerifyAIToken(forged, now))
}

func TestVerifyAIToken_MalformedRejected(t *testing.T) {
	withAITokenKey(t, "signing-key")
	now := time.Unix(1_700_000_000, 0)
	valid := SignAIToken(now)
	expPart, sigPart, ok := strings.Cut(valid, aiTokenSeparator)
	require.True(t, ok)

	cases := map[string]string{
		"empty":                "",
		"no separator":         expPart,
		"empty expiry":         aiTokenSeparator + sigPart,
		"empty signature":      expPart + aiTokenSeparator,
		"non base64 expiry":    "!!!." + sigPart,
		"non base64 signature": expPart + ".!!!",
		"non numeric expiry": base64.RawURLEncoding.EncodeToString([]byte("not-a-number")) +
			aiTokenSeparator + sigPart,
		"bare key": "signing-key",
		"junk":     "a.b.c",
	}

	for name, token := range cases {
		t.Run(name, func(t *testing.T) {
			assert.False(t, VerifyAIToken(token, now))
		})
	}
}

func TestSignAIToken_DeterministicForSameExpiry(t *testing.T) {
	// Statelessness: no nonce, no server-side record. Two tokens issued at the
	// same instant are identical, which is what lets verification avoid a store.
	withAITokenKey(t, "signing-key")
	now := time.Unix(1_700_000_000, 0)

	assert.Equal(t, SignAIToken(now), SignAIToken(now))
}

func TestSignAIToken_DifferentExpiryDifferentToken(t *testing.T) {
	withAITokenKey(t, "signing-key")
	now := time.Unix(1_700_000_000, 0)

	assert.NotEqual(t, SignAIToken(now), SignAIToken(now.Add(time.Minute)))
}

func TestVerifyAIToken_OtherProcessKeyRejected(t *testing.T) {
	// Each process generates its own key, so a token minted by another process
	// (e.g. before a restart) must not be accepted here.
	now := time.Unix(1_700_000_000, 0)

	withAITokenKey(t, "process-a")
	foreign := SignAIToken(now)

	aiTokenKey = "process-b"
	assert.False(t, VerifyAIToken(foreign, now))
}

// --- exp ceiling: a leaked key must not yield a long-lived credential ---

// The TTL is a property of SignAIToken, so a key holder can hand-craft an
// arbitrary expiry. Verification must clamp it, otherwise "30 minutes" is
// decorative: anyone with the key mints a token valid for years.
func TestVerifyAIToken_RejectsExpiryBeyondTTL(t *testing.T) {
	withAITokenKey(t, "leaked-key")
	now := time.Unix(1_700_000_000, 0)

	// Forge a token expiring far in the future, correctly signed with the key.
	forged := encodeAIToken(now.AddDate(10, 0, 0).Unix(), aiTokenKey)

	assert.False(t, VerifyAIToken(forged, now),
		"an expiry beyond the TTL must be rejected even when correctly signed")
}

// The boundary: exactly one TTL ahead is still valid; one second beyond is not.
func TestVerifyAIToken_AcceptsExactlyTTLRejectsBeyond(t *testing.T) {
	withAITokenKey(t, "leaked-key")
	now := time.Unix(1_700_000_000, 0)
	atLimit := now.Add(AITokenTTL).Unix()

	assert.True(t, VerifyAIToken(encodeAIToken(atLimit, aiTokenKey), now),
		"exp == now+TTL must be accepted")
	assert.False(t, VerifyAIToken(encodeAIToken(atLimit+1, aiTokenKey), now),
		"exp == now+TTL+1 must be rejected")
}

// SignAIToken itself must always land inside the accepted window, so the
// ceiling can never reject a token the server legitimately issued.
func TestSignAIToken_AlwaysWithinAcceptedWindow(t *testing.T) {
	withAITokenKey(t, "signing-key")
	for _, offset := range []time.Duration{0, time.Second, time.Hour, 24 * time.Hour} {
		now := time.Unix(1_700_000_000, 0).Add(offset)
		token := SignAIToken(now)
		assert.Truef(t, VerifyAIToken(token, now),
			"a freshly signed token must verify (offset %s)", offset)
	}
}

// The signing key must not be the session cookie token: those are different
// secrets with different lifetimes.
func TestAITokenKey_IsIndependentOfCookieToken(t *testing.T) {
	withAITokenKey(t, "ai-key")
	origCookie := CookieToken
	defer func() { CookieToken = origCookie }()
	CookieToken = "session-cookie-value"

	now := time.Unix(1_700_000_000, 0)
	token := SignAIToken(now)
	require.True(t, VerifyAIToken(token, now))

	// A token forged with the session cookie as the key must NOT verify.
	forgedWithCookie := encodeAIToken(now.Add(time.Minute).Unix(), CookieToken)
	assert.False(t, VerifyAIToken(forgedWithCookie, now),
		"CookieToken must not be usable as the AI token signing key")
}

// aiTokenKey must be non-empty in a real process (it is generated at init).
func TestAITokenKey_IsPopulatedByDefault(t *testing.T) {
	assert.NotEmpty(t, aiTokenKey,
		"the process must generate a signing key at startup")
	assert.Len(t, aiTokenKey, 64, "32 random bytes hex-encoded")
}

// --- Redaction: the token must never reach a log file ---

func TestRedactAIToken_RemovesInjectedToken(t *testing.T) {
	token := SignAIToken(time.Unix(1_700_000_000, 0))
	require.NotEmpty(t, token)

	// This is the shape injected into the prompt, which some backends pass as
	// a command-line argument that then gets logged in full.
	prompt := "Base URL: http://localhost:20000\n" +
		"Auth: send the header \"" + AITokenHeader + ": " + token + "\" on every request.\n"

	got := RedactAIToken(prompt)

	assert.NotContains(t, got, token, "the raw token must not survive redaction")
	assert.Contains(t, got, AITokenHeader, "the header name is not secret and stays")
	assert.Contains(t, got, "[redacted]")
}

func TestRedactAIToken_LeavesTokenFreeTextAlone(t *testing.T) {
	// Ordinary log lines must pass through untouched.
	in := "Base URL: http://localhost:20000\nEndpoints:\nGET /api/tasks\n"
	assert.Equal(t, in, RedactAIToken(in))
}

func TestRedactAIToken_HandlesMultipleOccurrences(t *testing.T) {
	tok1 := SignAIToken(time.Unix(1_700_000_000, 0))
	tok2 := SignAIToken(time.Unix(1_700_001_000, 0))

	in := AITokenHeader + ": " + tok1 + " and again " + AITokenHeader + ": " + tok2
	got := RedactAIToken(in)

	assert.NotContains(t, got, tok1)
	assert.NotContains(t, got, tok2)
}
