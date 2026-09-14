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

// --- verifyRegistrySignature ---
//
// The contract is warn-only: no signature condition aborts the upgrade. An
// empty return means the signature verified; anything else is the warning the
// caller surfaces to the user.

func TestVerifyRegistrySignature_ValidSignature(t *testing.T) {
	keys := newTestSigningKeys(t)
	restore := withKeysEndpoint(t, keys.keysJSON(t))
	defer restore()

	sig := keys.sign(t, "pkg", "1.2.3", "sha512-abc")
	warning := verifyRegistrySignature(context.Background(), "pkg", "1.2.3", "sha512-abc",
		[]npmSignature{sig}, npmjsRegistryBase)
	assert.Empty(t, warning, "a verified signature must produce no warning")
}

// The signature is checked against real key material, so a signature made by a
// key npm does not publish must be reported even though it is well-formed.
func TestVerifyRegistrySignature_WrongKeyWarns(t *testing.T) {
	trusted := newTestSigningKeys(t)
	attacker := newTestSigningKeys(t)
	restore := withKeysEndpoint(t, trusted.keysJSON(t))
	defer restore()

	// The attacker signs correctly but with a key npm does not publish.
	forged := attacker.sign(t, "pkg", "1.2.3", "sha512-abc")
	warning := verifyRegistrySignature(context.Background(), "pkg", "1.2.3", "sha512-abc",
		[]npmSignature{forged}, npmjsRegistryBase)
	assert.Contains(t, warning, "not among npm's published keys")
}

// The attack the trust anchor exists to catch: a registry swaps the tarball and
// supplies a matching integrity hash. It cannot produce a signature over that
// new hash. With warn-only policy the upgrade proceeds, so the warning must
// name the tampering rather than the repackaging explanation.
func TestVerifyRegistrySignature_TamperedIntegrityWarns(t *testing.T) {
	keys := newTestSigningKeys(t)
	restore := withKeysEndpoint(t, keys.keysJSON(t))
	defer restore()

	// Signed for the honest hash...
	honestSig := keys.sign(t, "pkg", "1.2.3", "sha512-honest")
	// ...but the response now advertises a different one.
	warning := verifyRegistrySignature(context.Background(), "pkg", "1.2.3", "sha512-tampered",
		[]npmSignature{honestSig}, npmjsRegistryBase)
	require.NotEmpty(t, warning, "an unverifiable signature must always warn")
	assert.Contains(t, warning, "did not verify")
	assert.Contains(t, warning, "repackages")
}

func TestVerifyRegistrySignature_TamperedVersionWarns(t *testing.T) {
	keys := newTestSigningKeys(t)
	restore := withKeysEndpoint(t, keys.keysJSON(t))
	defer restore()

	sig := keys.sign(t, "pkg", "1.2.3", "sha512-abc")
	warning := verifyRegistrySignature(context.Background(), "pkg", "9.9.9", "sha512-abc",
		[]npmSignature{sig}, npmjsRegistryBase)
	assert.NotEmpty(t, warning)
}

func TestVerifyRegistrySignature_TamperedPackageNameWarns(t *testing.T) {
	keys := newTestSigningKeys(t)
	restore := withKeysEndpoint(t, keys.keysJSON(t))
	defer restore()

	sig := keys.sign(t, "real-pkg", "1.2.3", "sha512-abc")
	warning := verifyRegistrySignature(context.Background(), "evil-pkg", "1.2.3", "sha512-abc",
		[]npmSignature{sig}, npmjsRegistryBase)
	assert.NotEmpty(t, warning)
}

func TestVerifyRegistrySignature_MalformedSignatureWarns(t *testing.T) {
	keys := newTestSigningKeys(t)
	restore := withKeysEndpoint(t, keys.keysJSON(t))
	defer restore()

	warning := verifyRegistrySignature(context.Background(), "pkg", "1.2.3", "sha512-abc",
		[]npmSignature{{KeyID: keys.keyID, Sig: "!!!not-base64!!!"}}, npmjsRegistryBase)
	assert.NotEmpty(t, warning)
}

// --- missing signature ---

// npm signs everything it publishes, so an official registry without a
// signature is anomalous. It still does not abort — the upgrade proceeds with
// the integrity check — but the warning says so plainly.
func TestVerifyRegistrySignature_OfficialRegistryWithoutSignatureWarns(t *testing.T) {
	for _, base := range []string{npmjsRegistryBase, npmMirrorRegistryBase} {
		t.Run(base, func(t *testing.T) {
			warning := verifyRegistrySignature(context.Background(), "pkg", "1.2.3", "sha512-abc", nil, base)
			assert.Contains(t, warning, "no signature")
			assert.Contains(t, warning, "unexpected")
		})
	}
}

// A custom mirror may be a plain proxy with no signing support; the warning is
// worded as expected behavior rather than an anomaly.
func TestVerifyRegistrySignature_CustomMirrorWithoutSignatureWarns(t *testing.T) {
	warning := verifyRegistrySignature(context.Background(), "pkg", "1.2.3", "sha512-abc", nil,
		"https://my-nexus.internal/npm")
	assert.Contains(t, warning, "did not sign this release")
	assert.NotContains(t, warning, "unexpected")
}

// A custom mirror must not be able to launder an invalid signature: the warning
// is still raised.
func TestVerifyRegistrySignature_CustomMirrorWithBadSignatureWarns(t *testing.T) {
	keys := newTestSigningKeys(t)
	restore := withKeysEndpoint(t, keys.keysJSON(t))
	defer restore()

	sig := keys.sign(t, "pkg", "1.2.3", "sha512-abc")
	warning := verifyRegistrySignature(context.Background(), "pkg", "1.2.3", "sha512-different",
		[]npmSignature{sig}, "https://my-nexus.internal/npm")
	assert.Contains(t, warning, "did not verify")
}

func TestVerifyRegistrySignature_OfficialRegistryNoIntegrityWarns(t *testing.T) {
	warning := verifyRegistrySignature(context.Background(), "pkg", "1.2.3", "", nil, npmjsRegistryBase)
	assert.Contains(t, warning, "neither a signature nor an integrity hash")
}

func TestVerifyRegistrySignature_SignatureWithoutIntegrityWarns(t *testing.T) {
	keys := newTestSigningKeys(t)
	restore := withKeysEndpoint(t, keys.keysJSON(t))
	defer restore()

	// Without integrity the signed payload cannot be reconstructed.
	warning := verifyRegistrySignature(context.Background(), "pkg", "1.2.3", "",
		[]npmSignature{keys.sign(t, "pkg", "1.2.3", "")}, npmjsRegistryBase)
	assert.Contains(t, warning, "no integrity hash")
}

// --- keys endpoint failures ---

func TestVerifyRegistrySignature_KeysEndpointUnreachableWarns(t *testing.T) {
	origClient := upgradeHTTPClient
	defer func() { upgradeHTTPClient = origClient }()

	upgradeHTTPClient = &http.Client{Transport: &failoverTransport{keysStatus: http.StatusInternalServerError}}

	keys := newTestSigningKeys(t)
	sig := keys.sign(t, "pkg", "1.2.3", "sha512-abc")

	warning := verifyRegistrySignature(context.Background(), "pkg", "1.2.3", "sha512-abc",
		[]npmSignature{sig}, npmjsRegistryBase)
	assert.Contains(t, warning, "could not be verified")
	// The wording must state the consequence (unauthenticated) rather than
	// promising an integrity check that may not happen.
	assert.Contains(t, warning, "could not be authenticated")
}

func TestVerifyRegistrySignature_EmptyKeyListWarns(t *testing.T) {
	restore := withKeysEndpoint(t, []byte(`{"keys":[]}`))
	defer restore()

	keys := newTestSigningKeys(t)
	sig := keys.sign(t, "pkg", "1.2.3", "sha512-abc")

	warning := verifyRegistrySignature(context.Background(), "pkg", "1.2.3", "sha512-abc",
		[]npmSignature{sig}, npmjsRegistryBase)
	assert.Contains(t, warning, "could not be verified")
}

func TestVerifyRegistrySignature_KeysEndpointInvalidJSONWarns(t *testing.T) {
	restore := withKeysEndpoint(t, []byte(`not json`))
	defer restore()

	keys := newTestSigningKeys(t)
	sig := keys.sign(t, "pkg", "1.2.3", "sha512-abc")

	warning := verifyRegistrySignature(context.Background(), "pkg", "1.2.3", "sha512-abc",
		[]npmSignature{sig}, npmjsRegistryBase)
	assert.Contains(t, warning, "could not be verified")
}

// A reachable keys endpoint that does not carry the signing key is still only a
// warning: the policy is that nothing about signatures blocks an upgrade.
func TestVerifyRegistrySignature_UnknownKeyIDWarns(t *testing.T) {
	trusted := newTestSigningKeys(t)
	attacker := newTestSigningKeys(t)
	restore := withKeysEndpoint(t, trusted.keysJSON(t))
	defer restore()

	forged := attacker.sign(t, "pkg", "1.2.3", "sha512-abc")
	warning := verifyRegistrySignature(context.Background(), "pkg", "1.2.3", "sha512-abc",
		[]npmSignature{forged}, npmjsRegistryBase)
	assert.Contains(t, warning, "not among npm's published keys")
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
	warning := verifyRegistrySignature(context.Background(), "pkg", "1.2.3", "sha512-abc", sigs, npmjsRegistryBase)
	assert.Empty(t, warning, "one valid signature is enough")
}

func TestVerifyRegistrySignature_AllSignaturesInvalidWarns(t *testing.T) {
	trusted := newTestSigningKeys(t)
	restore := withKeysEndpoint(t, trusted.keysJSON(t))
	defer restore()

	sigs := []npmSignature{
		trusted.sign(t, "pkg", "1.2.3", "sha512-one"),
		trusted.sign(t, "pkg", "1.2.3", "sha512-two"),
	}
	warning := verifyRegistrySignature(context.Background(), "pkg", "1.2.3", "sha512-actual", sigs, npmjsRegistryBase)
	assert.Contains(t, warning, "did not verify")
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
// npm never signed. The upgrade is not aborted, but it must carry the warning
// so the user knows the release was not authenticated.
func TestFetchUpgradeInfo_TamperedMetadataWarns(t *testing.T) {
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

	info, err := fetchUpgradeInfoFromBase(npmjsRegistryBase, pkg, "0.1.0")
	require.NoError(t, err, "signature failures must not abort the upgrade")
	require.NotEmpty(t, info.VerificationWarning, "the tampering must be surfaced")
	assert.Contains(t, info.VerificationWarning, "did not verify")
}

// A correctly signed release must come through with no warning at all.
func TestFetchUpgradeInfo_VerifiedReleaseHasNoWarning(t *testing.T) {
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
	assert.Empty(t, info.VerificationWarning)
}

// The warning must cover a missing hash, not just signature problems.
//
// This is the regression that mattered: the client asks the user to confirm
// before starting, and it learns what to ask from this endpoint. If a missing
// hash were only discovered during the download, the install would proceed with
// no confirmation at all — silently, for exactly the mirror configuration most
// likely to omit the field.
func TestFetchUpgradeInfo_MissingHashIsReportedBeforeDownload(t *testing.T) {
	origClient := upgradeHTTPClient
	defer func() { upgradeHTTPClient = origClient }()

	pkg, err := getPlatformPkg()
	require.NoError(t, err)

	// A plain proxy: no signature, no integrity, no shasum.
	const mirror = "https://nexus.corp/repository/npm-proxy"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := npmRegistryResponse{}
		resp.Version = "99.0.0"
		resp.Dist.Tarball = mirror + "/x/-/x-99.0.0.tgz"
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	upgradeHTTPClient = &http.Client{Transport: &failoverTransport{
		mirrorBase: ts.URL,
		keysStatus: http.StatusNotFound,
	}}

	info, err := fetchUpgradeInfoFromBase(mirror, pkg, "0.1.0")
	require.NoError(t, err)

	require.NotEmpty(t, info.VerificationWarning,
		"a missing hash must be visible to /check, or the confirmation gate cannot fire")
	assert.Contains(t, info.VerificationWarning, "no integrity hash")
	assert.Empty(t, info.Integrity)
	assert.Empty(t, info.Shasum)
}

// Both reasons can apply at once; the user should see both rather than whichever
// was computed last.
func TestFetchUpgradeInfo_ReportsBothReasons(t *testing.T) {
	origClient := upgradeHTTPClient
	defer func() { upgradeHTTPClient = origClient }()

	pkg, err := getPlatformPkg()
	require.NoError(t, err)

	// An official registry that returns neither a signature nor a hash trips
	// both checks.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := npmRegistryResponse{}
		resp.Version = "99.0.0"
		resp.Dist.Tarball = npmjsRegistryBase + "/x/-/x-99.0.0.tgz"
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	upgradeHTTPClient = &http.Client{Transport: &failoverTransport{defaultBase: ts.URL}}

	info, err := fetchUpgradeInfoFromBase(npmjsRegistryBase, pkg, "0.1.0")
	require.NoError(t, err)

	// Both reasons appear: the signature check's complaint and the digest
	// resolution's.
	assert.Contains(t, info.VerificationWarning, "neither a signature nor an integrity hash")
	assert.Contains(t, info.VerificationWarning, "no integrity hash for this release")
}
