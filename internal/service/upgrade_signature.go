package service

import (
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
)

// The npm registry signs every published version with an ECDSA P-256 key whose
// public half is published at a well-known endpoint. Verifying that signature
// is what turns the integrity hash into a real trust anchor: the hash and the
// tarball URL both come from the same registry response, so a malicious or
// compromised registry can simply supply a matching hash for a tampered
// tarball. It cannot, however, forge npm's signature over that hash.
//
// The signed payload is exactly "<name>@<version>:<dist.integrity>", per
// https://docs.npmjs.com/about-registry-signatures — so the signature binds the
// package identity and version to the integrity hash, and any change to either
// invalidates it.
const (
	// npmjsRegistryBase is the public npm registry.
	npmjsRegistryBase = "https://registry.npmjs.org"
	// npmMirrorRegistryBase is the npmmirror CDN, which mirrors npmjs metadata
	// (including its signatures) but does not serve the keys endpoint.
	npmMirrorRegistryBase = "https://registry.npmmirror.com"

	// npmKeysPath is the endpoint publishing npm's signing public keys. It is
	// always fetched from npmjs, never from the mirror that served the package
	// metadata: the anchor must be independent of the source under suspicion.
	npmKeysPath = "/-/npm/v1/keys"

	// npmSignatureAlgo is the only algorithm npm's registry signatures use.
	npmSignatureAlgo = "ecdsa-sha2-nistp256"
)

// npmSignature is one entry of dist.signatures.
type npmSignature struct {
	KeyID string `json:"keyid"`
	Sig   string `json:"sig"`
}

// npmSigningKey is one entry of the keys endpoint response.
type npmSigningKey struct {
	KeyID   string  `json:"keyid"`
	Key     string  `json:"key"`
	KeyType string  `json:"keytype"`
	Expires *string `json:"expires"`
}

type npmKeysResponse struct {
	Keys []npmSigningKey `json:"keys"`
}

// isOfficialRegistry reports whether base is a registry whose metadata is
// expected to carry npm's registry signature. Both the canonical registry and
// its CDN mirror qualify: the mirror passes npm's signatures through verbatim
// (verified against the live service), so a missing signature there is as
// suspicious as one missing from npmjs itself.
func isOfficialRegistry(base string) bool {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	return base == npmjsRegistryBase || base == npmMirrorRegistryBase
}

// verifyRegistrySignature checks npm's registry signature over the package
// identity and integrity hash, and returns a human-readable warning describing
// why verification could not be completed.
//
// It never fails the upgrade. This makes the signature an advisory check, not a
// gate, and the consequence should be stated plainly: a registry that omits the
// signature field entirely is indistinguishable here from one that has none to
// offer, so a mirror determined to serve modified content can do so by simply
// not signing. What the check still catches is tampering with a response that
// does carry a signature (for example a proxy that passes npm's signatures
// through), and a stale signature that a naive mirror failed to strip.
//
// The warning is what keeps the downgrade visible: the caller surfaces it and
// the user decides whether to proceed. Refusing outright would strand users
// whose network cannot reach npmjs, or whose mirror legitimately repackages
// tarballs.
//
// An empty return means the signature was verified. The one check that does
// still abort an upgrade lives in downloadAndExtract: a tarball whose bytes do
// not match the expected hash is a corrupt or wrong download, not merely an
// unauthenticated one.
//
// Integrity is required to reconstruct the signed payload; without it the
// signature cannot be checked at all.
func verifyRegistrySignature(ctx context.Context, pkg, version, integrity string, sigs []npmSignature, registryBase string) string {
	if integrity == "" {
		if len(sigs) > 0 {
			slog.Warn("upgrade: signatures present but dist.integrity missing",
				"registry", registryBase, "package", pkg, "version", version)
			return fmt.Sprintf("The registry %s supplied a signature but no integrity hash, so the "+
				"release could not be authenticated.", registryBase)
		}
		if isOfficialRegistry(registryBase) {
			slog.Warn("upgrade: official registry returned neither signature nor integrity",
				"registry", registryBase, "package", pkg, "version", version)
			return fmt.Sprintf("The registry %s returned neither a signature nor an integrity hash, so "+
				"this release could not be authenticated.", registryBase)
		}
		return ""
	}

	if len(sigs) == 0 {
		slog.Warn("upgrade: registry provided no signature",
			"registry", registryBase, "package", pkg, "version", version)
		if isOfficialRegistry(registryBase) {
			// npm signs everything it publishes, so this is anomalous for an
			// official registry and worth saying so plainly.
			return fmt.Sprintf("The official registry %s returned no signature for this release. npm signs "+
				"every published version, so this is unexpected — the release could not be authenticated.",
				registryBase)
		}
		return fmt.Sprintf("The registry %s did not sign this release, so it could not be "+
			"authenticated against npm's signing keys.", registryBase)
	}

	keys, err := fetchNpmSigningKeys(ctx)
	if err != nil {
		slog.Warn("upgrade: cannot reach npm signing keys — signature not verified",
			"error", err, "package", pkg, "version", version)
		// Deliberately excludes the underlying error text. The caller compares
		// this warning verbatim against the one the client confirmed, so it must
		// depend only on the metadata, not on *how* the request failed — a
		// transport error's message varies between attempts (timeout, refused,
		// DNS), which would make the confirmation impossible to satisfy for the
		// very users this downgrade exists to serve. The detail is in the log.
		return "The release signature could not be verified because npm's signing keys were " +
			"unreachable, so the release could not be authenticated."
	}

	payload := fmt.Sprintf("%s@%s:%s", pkg, version, integrity)

	// sigs is non-empty here, so the loop always runs and every iteration
	// either records a failure or returns "" — lastErr is set if we fall out.
	var lastErr error
	for _, sig := range sigs {
		key, ok := findSigningKey(keys, sig.KeyID)
		if !ok {
			lastErr = fmt.Errorf("its signing key %s is not among npm's published keys", sig.KeyID)
			continue
		}
		if err := verifyECDSASignature(key, sig.Sig, payload); err != nil {
			lastErr = err
			continue
		}
		slog.Info("upgrade: registry signature verified", "keyid", key.KeyID, "package", pkg, "version", version)
		return ""
	}

	slog.Warn("upgrade: registry signature verification failed",
		"registry", registryBase, "package", pkg, "version", version, "error", lastErr)
	return fmt.Sprintf("The release signature from %s did not verify (%v), so the release could not be "+
		"authenticated. A mirror that repackages the tarball changes its integrity hash and thereby "+
		"invalidates the original signature, but a tampered or substituted release produces the same "+
		"result — the two cannot be told apart from here.", registryBase, lastErr)
}

// findSigningKey returns the key matching keyid.
func findSigningKey(keys []npmSigningKey, keyID string) (npmSigningKey, bool) {
	for _, k := range keys {
		if k.KeyID == keyID {
			return k, true
		}
	}
	return npmSigningKey{}, false
}

// verifyECDSASignature verifies sig (base64 DER) over payload using key.
//
// A key's expiry is deliberately not enforced: a signature made while the key
// was valid remains valid afterwards, and npm's own client verifies against
// whatever keys the endpoint currently publishes. Rejecting on expiry would
// break upgrades for packages signed before a rotation.
func verifyECDSASignature(key npmSigningKey, sigB64, payload string) error {
	if key.KeyType != "" && key.KeyType != npmSignatureAlgo {
		return fmt.Errorf("signing key %s uses unsupported algorithm %q", key.KeyID, key.KeyType)
	}

	der, err := base64.StdEncoding.DecodeString(key.Key)
	if err != nil {
		return fmt.Errorf("failed to decode signing key %s: %w", key.KeyID, err)
	}
	parsed, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return fmt.Errorf("failed to parse signing key %s: %w", key.KeyID, err)
	}
	pub, ok := parsed.(*ecdsa.PublicKey)
	if !ok {
		return fmt.Errorf("signing key %s is not an ECDSA key", key.KeyID)
	}

	sig, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil {
		return fmt.Errorf("failed to decode signature: %w", err)
	}

	digest := sha256.Sum256([]byte(payload))
	if !ecdsa.VerifyASN1(pub, digest[:], sig) {
		return fmt.Errorf("signature does not match %s", payload)
	}
	return nil
}

// fetchNpmSigningKeys retrieves npm's published signing keys from the canonical
// registry, independent of whichever registry served the package metadata.
func fetchNpmSigningKeys(ctx context.Context) ([]npmSigningKey, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, npmjsRegistryBase+npmKeysPath, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create keys request: %w", err)
	}

	resp, err := upgradeHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("keys request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("keys endpoint returned status %d", resp.StatusCode)
	}

	var out npmKeysResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("failed to decode keys response: %w", err)
	}
	if len(out.Keys) == 0 {
		return nil, errors.New("keys endpoint published no keys")
	}
	return out.Keys, nil
}
