package service

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testSigningKeys is a real ECDSA P-256 keypair standing in for npm's registry
// signing key, so tests exercise the same code path as production verification.
type testSigningKeys struct {
	priv   *ecdsa.PrivateKey
	keyID  string
	pubB64 string
}

// newTestSigningKeys generates a fresh keypair.
func newTestSigningKeys(t *testing.T) *testSigningKeys {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	der, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	require.NoError(t, err)

	// npm derives keyid from the public key; the exact convention does not
	// matter here because verification looks keys up by the keyid the signature
	// advertises. Any stable, unique value works.
	sum := sha256.Sum256(der)
	return &testSigningKeys{
		priv:   priv,
		keyID:  "SHA256:" + base64.StdEncoding.EncodeToString(sum[:]),
		pubB64: base64.StdEncoding.EncodeToString(der),
	}
}

// sign returns a valid registry signature for pkg@version with integrity.
func (k *testSigningKeys) sign(t *testing.T, pkg, version, integrity string) npmSignature {
	t.Helper()

	payload := fmt.Sprintf("%s@%s:%s", pkg, version, integrity)
	digest := sha256.Sum256([]byte(payload))
	sig, err := ecdsa.SignASN1(rand.Reader, k.priv, digest[:])
	require.NoError(t, err)

	return npmSignature{
		KeyID: k.keyID,
		Sig:   base64.StdEncoding.EncodeToString(sig),
	}
}

// keysJSON renders the keys-endpoint response advertising this key.
func (k *testSigningKeys) keysJSON(t *testing.T) []byte {
	t.Helper()

	out := npmKeysResponse{Keys: []npmSigningKey{{
		KeyID:   k.keyID,
		Key:     k.pubB64,
		KeyType: npmSignatureAlgo,
	}}}
	b, err := json.Marshal(out)
	require.NoError(t, err)
	return b
}

// signedRegistryResponse builds a metadata response with a valid signature.
func signedRegistryResponse(t *testing.T, keys *testSigningKeys, pkg, version, integrity string) npmRegistryResponse {
	t.Helper()

	resp := npmRegistryResponse{}
	resp.Version = version
	resp.Dist.Tarball = npmjsRegistryBase + "/" + pkg + "/-/" + version + ".tgz"
	resp.Dist.Integrity = integrity
	resp.Dist.Signatures = []npmSignature{keys.sign(t, pkg, version, integrity)}
	return resp
}

// withKeysEndpoint points the HTTP client at a transport that answers the npm
// signing-keys endpoint with body. It returns a restore function.
func withKeysEndpoint(t *testing.T, body []byte) func() {
	t.Helper()

	origClient := upgradeHTTPClient
	upgradeHTTPClient = &http.Client{Transport: &failoverTransport{keysJSON: body}}
	return func() { upgradeHTTPClient = origClient }
}

// --- isOfficialRegistry ---

func TestIsOfficialRegistry(t *testing.T) {
	tests := []struct {
		base string
		want bool
	}{
		{npmjsRegistryBase, true},
		{npmMirrorRegistryBase, true},
		{npmjsRegistryBase + "/", true}, // trailing slash tolerated
		{"  " + npmjsRegistryBase + "  ", true},
		{"https://registry.npmjs.org.evil.example", false},
		{"https://my-nexus.internal/npm", false},
		{"http://localhost:4873", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.base, func(t *testing.T) {
			assert.Equal(t, tt.want, isOfficialRegistry(tt.base))
		})
	}
}

// --- verifyRegistrySignature: happy path ---

func TestVerifyRegistrySignature_ValidSignature(t *testing.T) {
	keys := newTestSigningKeys(t)
	restore := withKeysEndpoint(t, keys.keysJSON(t))
	defer restore()

	sig := keys.sign(t, "pkg", "1.2.3", "sha512-abc")
	_, err := verifyRegistrySignature(context.Background(), "pkg", "1.2.3", "sha512-abc",
		[]npmSignature{sig}, npmjsRegistryBase)
	assert.NoError(t, err)
}

// The signature is checked against the real key material, so a signature made
// by a different key must be rejected even though it is well-formed.
func TestVerifyRegistrySignature_WrongKeyRejected(t *testing.T) {
	trusted := newTestSigningKeys(t)
	attacker := newTestSigningKeys(t)
	restore := withKeysEndpoint(t, trusted.keysJSON(t))
	defer restore()

	// The attacker signs correctly but with a key npm does not publish.
	forged := attacker.sign(t, "pkg", "1.2.3", "sha512-abc")
	_, err := verifyRegistrySignature(context.Background(), "pkg", "1.2.3", "sha512-abc",
		[]npmSignature{forged}, npmjsRegistryBase)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not among npm's published signing keys")
}

// This is the attack the trust anchor exists to stop: a malicious registry
// swaps the tarball and supplies a matching integrity hash. It cannot produce a
// signature over that new hash.
func TestVerifyRegistrySignature_TamperedIntegrityRejected(t *testing.T) {
	keys := newTestSigningKeys(t)
	restore := withKeysEndpoint(t, keys.keysJSON(t))
	defer restore()

	// Signed for the honest hash...
	honestSig := keys.sign(t, "pkg", "1.2.3", "sha512-honest")
	// ...but the response now advertises a different one.
	_, err := verifyRegistrySignature(context.Background(), "pkg", "1.2.3", "sha512-tampered",
		[]npmSignature{honestSig}, npmjsRegistryBase)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "signature verification failed")
}

func TestVerifyRegistrySignature_TamperedVersionRejected(t *testing.T) {
	keys := newTestSigningKeys(t)
	restore := withKeysEndpoint(t, keys.keysJSON(t))
	defer restore()

	sig := keys.sign(t, "pkg", "1.2.3", "sha512-abc")
	_, err := verifyRegistrySignature(context.Background(), "pkg", "9.9.9", "sha512-abc",
		[]npmSignature{sig}, npmjsRegistryBase)
	require.Error(t, err)
}

func TestVerifyRegistrySignature_TamperedPackageNameRejected(t *testing.T) {
	keys := newTestSigningKeys(t)
	restore := withKeysEndpoint(t, keys.keysJSON(t))
	defer restore()

	sig := keys.sign(t, "real-pkg", "1.2.3", "sha512-abc")
	_, err := verifyRegistrySignature(context.Background(), "evil-pkg", "1.2.3", "sha512-abc",
		[]npmSignature{sig}, npmjsRegistryBase)
	require.Error(t, err)
}

func TestVerifyRegistrySignature_MalformedSignatureRejected(t *testing.T) {
	keys := newTestSigningKeys(t)
	restore := withKeysEndpoint(t, keys.keysJSON(t))
	defer restore()

	_, err := verifyRegistrySignature(context.Background(), "pkg", "1.2.3", "sha512-abc",
		[]npmSignature{{KeyID: keys.keyID, Sig: "!!!not-base64!!!"}}, npmjsRegistryBase)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decode signature")
}

// --- verifyRegistrySignature: missing signature policy ---

// npm signs everything it publishes, so metadata from an official registry
// without a signature means the response did not come from npm.
func TestVerifyRegistrySignature_OfficialRegistryWithoutSignatureRejected(t *testing.T) {
	for _, base := range []string{npmjsRegistryBase, npmMirrorRegistryBase} {
		t.Run(base, func(t *testing.T) {
			_, err := verifyRegistrySignature(context.Background(), "pkg", "1.2.3", "sha512-abc", nil, base)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "no dist.signatures")
		})
	}
}

// A custom mirror may be a plain proxy with no signing support; the upgrade
// degrades to integrity-only rather than breaking, but the caller is handed a
// warning so the user can be told the install is unauthenticated.
func TestVerifyRegistrySignature_CustomMirrorWithoutSignatureAllowedWithWarning(t *testing.T) {
	warning, err := verifyRegistrySignature(context.Background(), "pkg", "1.2.3", "sha512-abc", nil,
		"https://my-nexus.internal/npm")
	assert.NoError(t, err)
	assert.Contains(t, warning, "did not sign this release")
}

// A custom mirror must not be able to launder an invalid signature.
func TestVerifyRegistrySignature_CustomMirrorWithBadSignatureRejected(t *testing.T) {
	keys := newTestSigningKeys(t)
	restore := withKeysEndpoint(t, keys.keysJSON(t))
	defer restore()

	sig := keys.sign(t, "pkg", "1.2.3", "sha512-abc")
	_, err := verifyRegistrySignature(context.Background(), "pkg", "1.2.3", "sha512-different",
		[]npmSignature{sig}, "https://my-nexus.internal/npm")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "signature verification failed")
}

func TestVerifyRegistrySignature_OfficialRegistryNoIntegrityRejected(t *testing.T) {
	_, err := verifyRegistrySignature(context.Background(), "pkg", "1.2.3", "", nil, npmjsRegistryBase)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "neither a signature nor dist.integrity")
}

func TestVerifyRegistrySignature_SignatureWithoutIntegrityRejected(t *testing.T) {
	keys := newTestSigningKeys(t)
	restore := withKeysEndpoint(t, keys.keysJSON(t))
	defer restore()

	// Without integrity the signed payload cannot be reconstructed.
	_, err := verifyRegistrySignature(context.Background(), "pkg", "1.2.3", "",
		[]npmSignature{keys.sign(t, "pkg", "1.2.3", "")}, npmjsRegistryBase)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no dist.integrity")
}

// --- keys endpoint failures downgrade to a warning ---
//
// Blocking the keys endpoint is a network-level attack, whereas refusing to
// upgrade strands every user whose network cannot reach npmjs at all (the
// mainland-China mirror case). These cases therefore downgrade to the integrity
// check, and the returned warning is what keeps the downgrade visible rather
// than silent.

func TestVerifyRegistrySignature_KeysEndpointUnreachableDowngradesWithWarning(t *testing.T) {
	origClient := upgradeHTTPClient
	defer func() { upgradeHTTPClient = origClient }()

	upgradeHTTPClient = &http.Client{Transport: &failoverTransport{keysStatus: http.StatusInternalServerError}}

	keys := newTestSigningKeys(t)
	sig := keys.sign(t, "pkg", "1.2.3", "sha512-abc")

	warning, err := verifyRegistrySignature(context.Background(), "pkg", "1.2.3", "sha512-abc",
		[]npmSignature{sig}, npmjsRegistryBase)
	require.NoError(t, err, "an unreachable keys endpoint must not block the upgrade")
	assert.Contains(t, warning, "could not be verified")
	assert.Contains(t, warning, "integrity hash only")
}

func TestVerifyRegistrySignature_EmptyKeyListDowngradesWithWarning(t *testing.T) {
	restore := withKeysEndpoint(t, []byte(`{"keys":[]}`))
	defer restore()

	keys := newTestSigningKeys(t)
	sig := keys.sign(t, "pkg", "1.2.3", "sha512-abc")

	warning, err := verifyRegistrySignature(context.Background(), "pkg", "1.2.3", "sha512-abc",
		[]npmSignature{sig}, npmjsRegistryBase)
	require.NoError(t, err)
	assert.Contains(t, warning, "could not be verified")
}

func TestVerifyRegistrySignature_KeysEndpointInvalidJSONDowngradesWithWarning(t *testing.T) {
	restore := withKeysEndpoint(t, []byte(`not json`))
	defer restore()

	keys := newTestSigningKeys(t)
	sig := keys.sign(t, "pkg", "1.2.3", "sha512-abc")

	warning, err := verifyRegistrySignature(context.Background(), "pkg", "1.2.3", "sha512-abc",
		[]npmSignature{sig}, npmjsRegistryBase)
	require.NoError(t, err)
	assert.Contains(t, warning, "could not be verified")
}

// A reachable keys endpoint that simply does not carry the signing key must
// still be fatal: nothing was unreachable, so the signature is genuinely
// unverifiable rather than merely unavailable.
func TestVerifyRegistrySignature_UnknownKeyIDStillRejected(t *testing.T) {
	trusted := newTestSigningKeys(t)
	attacker := newTestSigningKeys(t)
	restore := withKeysEndpoint(t, trusted.keysJSON(t))
	defer restore()

	forged := attacker.sign(t, "pkg", "1.2.3", "sha512-abc")
	_, err := verifyRegistrySignature(context.Background(), "pkg", "1.2.3", "sha512-abc",
		[]npmSignature{forged}, npmjsRegistryBase)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not among npm's published signing keys")
}

// --- multiple signatures ---

// A response may carry several signatures during a key rotation; one valid
// signature is enough.
func TestVerifyRegistrySignature_AcceptsAnyValidSignature(t *testing.T) {
	trusted := newTestSigningKeys(t)
	stale := newTestSigningKeys(t)
	restore := withKeysEndpoint(t, trusted.keysJSON(t))
	defer restore()

	sigs := []npmSignature{
		stale.sign(t, "pkg", "1.2.3", "sha512-abc"),   // key not published
		trusted.sign(t, "pkg", "1.2.3", "sha512-abc"), // valid
	}
	_, err := verifyRegistrySignature(context.Background(), "pkg", "1.2.3", "sha512-abc", sigs, npmjsRegistryBase)
	assert.NoError(t, err)
}

func TestVerifyRegistrySignature_AllSignaturesInvalidRejected(t *testing.T) {
	trusted := newTestSigningKeys(t)
	restore := withKeysEndpoint(t, trusted.keysJSON(t))
	defer restore()

	sigs := []npmSignature{
		trusted.sign(t, "pkg", "1.2.3", "sha512-one"),
		trusted.sign(t, "pkg", "1.2.3", "sha512-two"),
	}
	_, err := verifyRegistrySignature(context.Background(), "pkg", "1.2.3", "sha512-actual", sigs, npmjsRegistryBase)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "signature verification failed")
}

// --- verifyECDSASignature / findSigningKey ---

func TestVerifyECDSASignature_RejectsNonECDSAKey(t *testing.T) {
	keys := newTestSigningKeys(t)
	key := npmSigningKey{
		KeyID:   keys.keyID,
		Key:     keys.pubB64,
		KeyType: "rsa-sha256",
	}
	err := verifyECDSASignature(key, "AAAA", "payload")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported algorithm")
}

func TestVerifyECDSASignature_RejectsUndecodableKey(t *testing.T) {
	err := verifyECDSASignature(npmSigningKey{KeyID: "k", Key: "!!!not-base64!!!"}, "AAAA", "payload")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decode signing key")
}

func TestVerifyECDSASignature_RejectsGarbageKeyMaterial(t *testing.T) {
	// Valid base64, but not a parseable public key.
	key := npmSigningKey{KeyID: "k", Key: base64.StdEncoding.EncodeToString([]byte("garbage"))}
	err := verifyECDSASignature(key, "AAAA", "payload")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse signing key")
}

// A valid PKIX key that is not ECDSA (here: RSA) must be rejected rather than
// type-asserted blindly.
func TestVerifyECDSASignature_RejectsNonECDSAPublicKey(t *testing.T) {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	der, err := x509.MarshalPKIXPublicKey(&rsaKey.PublicKey)
	require.NoError(t, err)

	key := npmSigningKey{
		KeyID:   "k",
		Key:     base64.StdEncoding.EncodeToString(der),
		KeyType: npmSignatureAlgo, // keytype lies; the key material decides
	}
	err = verifyECDSASignature(key, "AAAA", "payload")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is not an ECDSA key")
}

// A transport-level failure fetching the keys must be fatal, not skipped.
func TestFetchNpmSigningKeys_TransportError(t *testing.T) {
	origClient := upgradeHTTPClient
	defer func() { upgradeHTTPClient = origClient }()

	upgradeHTTPClient = &http.Client{Transport: errorTransport{err: errors.New("dial tcp: refused")}}

	_, err := fetchNpmSigningKeys(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "keys request failed")
}

// errorTransport always fails, standing in for an unreachable keys endpoint.
type errorTransport struct{ err error }

func (e errorTransport) RoundTrip(*http.Request) (*http.Response, error) { return nil, e.err }

func TestFindSigningKey(t *testing.T) {
	keys := []npmSigningKey{{KeyID: "a"}, {KeyID: "b"}}

	got, ok := findSigningKey(keys, "b")
	require.True(t, ok)
	assert.Equal(t, "b", got.KeyID)

	_, ok = findSigningKey(keys, "missing")
	assert.False(t, ok)
}

// --- end-to-end through fetchUpgradeInfoFromBase ---

func TestFetchUpgradeInfo_VerifiesSignature(t *testing.T) {
	origClient := upgradeHTTPClient
	defer func() { upgradeHTTPClient = origClient }()

	pkg, err := getPlatformPkg()
	require.NoError(t, err)

	keys := newTestSigningKeys(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := signedRegistryResponse(t, keys, pkg, "99.0.0", wellFormedIntegrity)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	upgradeHTTPClient = &http.Client{Transport: &failoverTransport{
		defaultBase: ts.URL,
		keysJSON:    keys.keysJSON(t),
	}}

	info, err := fetchUpgradeInfoFromBase(npmjsRegistryBase, pkg, "0.1.0")
	require.NoError(t, err)
	assert.Equal(t, "99.0.0", info.LatestVersion)
}

// The end-to-end form of the tampering attack: metadata claiming a hash that
// npm never signed must not yield upgrade info at all.
func TestFetchUpgradeInfo_RejectsTamperedMetadata(t *testing.T) {
	origClient := upgradeHTTPClient
	defer func() { upgradeHTTPClient = origClient }()

	pkg, err := getPlatformPkg()
	require.NoError(t, err)

	keys := newTestSigningKeys(t)
	// Signature covers a different integrity than the response advertises.
	tampered := npmRegistryResponse{}
	tampered.Version = "99.0.0"
	tampered.Dist.Tarball = npmjsRegistryBase + "/x/-/x-99.0.0.tgz"
	tampered.Dist.Integrity = wellFormedIntegrity
	tampered.Dist.Signatures = []npmSignature{keys.sign(t, pkg, "99.0.0", "sha512-something-else")}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(tampered)
	}))
	defer ts.Close()

	upgradeHTTPClient = &http.Client{Transport: &failoverTransport{
		defaultBase: ts.URL,
		keysJSON:    keys.keysJSON(t),
	}}

	_, err = fetchUpgradeInfoFromBase(npmjsRegistryBase, pkg, "0.1.0")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "signature verification failed")
}
