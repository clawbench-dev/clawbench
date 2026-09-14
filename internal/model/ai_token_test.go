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

// withSavedCookieToken saves and restores the global signing key so token tests
// do not leak state into each other or into other tests in this package.
func withSavedCookieToken(t *testing.T, key string) {
	t.Helper()
	orig := CookieToken
	CookieToken = key
	t.Cleanup(func() { CookieToken = orig })
}

func TestSignAIToken_RoundTrips(t *testing.T) {
	withSavedCookieToken(t, "signing-key")
	now := time.Unix(1_700_000_000, 0)

	token := SignAIToken(now)
	require.NotEmpty(t, token)

	assert.True(t, VerifyAIToken(token, now))
}

func TestSignAIToken_ValidJustBeforeExpiry(t *testing.T) {
	withSavedCookieToken(t, "signing-key")
	now := time.Unix(1_700_000_000, 0)

	token := SignAIToken(now)

	// One second before the deadline the token must still be accepted; the TTL
	// has to actually cover the AI's window, not end early.
	justBefore := now.Add(AITokenTTL - time.Second)
	assert.True(t, VerifyAIToken(token, justBefore))
}

func TestVerifyAIToken_ExpiredRejected(t *testing.T) {
	withSavedCookieToken(t, "signing-key")
	now := time.Unix(1_700_000_000, 0)

	token := SignAIToken(now)

	assert.False(t, VerifyAIToken(token, now.Add(AITokenTTL)))
}

func TestVerifyAIToken_AtExactExpiryRejected(t *testing.T) {
	// The boundary matters: exp == now must be spent, not valid. A token is
	// valid strictly before its expiry.
	withSavedCookieToken(t, "signing-key")
	now := time.Unix(1_700_000_000, 0)
	exp := now.Add(AITokenTTL).Unix()

	token := encodeAIToken(exp, CookieToken)

	assert.False(t, VerifyAIToken(token, time.Unix(exp, 0)))
	assert.True(t, VerifyAIToken(token, time.Unix(exp-1, 0)))
}

func TestVerifyAIToken_TamperedSignatureRejected(t *testing.T) {
	withSavedCookieToken(t, "signing-key")
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
	withSavedCookieToken(t, "signing-key")
	now := time.Unix(1_700_000_000, 0)

	token := SignAIToken(now)
	_, sigPart, ok := strings.Cut(token, aiTokenSeparator)
	require.True(t, ok)

	extended := now.Add(AITokenTTL * 100).Unix()
	forged := base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(extended, 10))) +
		aiTokenSeparator + sigPart

	assert.False(t, VerifyAIToken(forged, now))
}

func TestVerifyAIToken_RotatedKeyRejected(t *testing.T) {
	// Rotating CookieToken (what the password-change handler does) must
	// invalidate every in-flight token.
	withSavedCookieToken(t, "old-key")
	now := time.Unix(1_700_000_000, 0)
	token := SignAIToken(now)
	require.True(t, VerifyAIToken(token, now))

	CookieToken = "new-key"
	assert.False(t, VerifyAIToken(token, now))
}

func TestSignAIToken_NoKeyReturnsEmpty(t *testing.T) {
	// With no configured key there is nothing to sign with, so signing must
	// fail closed rather than emit a token anyone could forge.
	withSavedCookieToken(t, "")
	now := time.Unix(1_700_000_000, 0)

	assert.Empty(t, SignAIToken(now))
}

func TestVerifyAIToken_NoKeyRejectsEverything(t *testing.T) {
	// A token signed under a real key must not verify once the key is cleared.
	withSavedCookieToken(t, "signing-key")
	now := time.Unix(1_700_000_000, 0)
	token := SignAIToken(now)
	require.NotEmpty(t, token)

	CookieToken = ""
	assert.False(t, VerifyAIToken(token, now))
}

func TestVerifyAIToken_ForgedWithEmptyKeyRejected(t *testing.T) {
	// The attack the empty-key guard exists for: an attacker knows the format,
	// so with no configured key they can compute the HMAC themselves. Accepting
	// an empty key would make every token forgeable.
	withSavedCookieToken(t, "")
	now := time.Unix(1_700_000_000, 0)

	forged := encodeAIToken(now.Add(AITokenTTL).Unix(), "")

	assert.False(t, VerifyAIToken(forged, now))
}

func TestVerifyAIToken_MalformedRejected(t *testing.T) {
	withSavedCookieToken(t, "signing-key")
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
	withSavedCookieToken(t, "signing-key")
	now := time.Unix(1_700_000_000, 0)

	assert.Equal(t, SignAIToken(now), SignAIToken(now))
}

func TestSignAIToken_DifferentExpiryDifferentToken(t *testing.T) {
	withSavedCookieToken(t, "signing-key")
	now := time.Unix(1_700_000_000, 0)

	assert.NotEqual(t, SignAIToken(now), SignAIToken(now.Add(time.Minute)))
}

func TestVerifyAIToken_OtherKeyTokenRejected(t *testing.T) {
	// A token signed by a different ClawBench instance (different cookie token)
	// must not be accepted here.
	now := time.Unix(1_700_000_000, 0)

	withSavedCookieToken(t, "instance-a")
	foreign := SignAIToken(now)

	CookieToken = "instance-b"
	assert.False(t, VerifyAIToken(foreign, now))
}
