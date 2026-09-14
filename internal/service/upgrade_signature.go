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
// identity and integrity hash.
//
// Policy:
//   - A signature that is present must verify, against a key published by npm.
//     A present-but-invalid signature is never tolerated: it means the metadata
//     was tampered with, or the package was re-signed by someone else.
//   - A missing signature is fatal for official registries (npm signs
//     everything published since 2023) but only a warning for a custom mirror,
//     which may be a plain proxy with no signing support. The caller still has
//     the integrity check in that case, so the upgrade degrades rather than
//     breaks.
//
// Integrity is required to reconstruct the signed payload; without it the
// signature cannot be checked at all.
func verifyRegistrySignature(ctx context.Context, pkg, version, integrity string, sigs []npmSignature, registryBase string) error {
	if integrity == "" {
		if len(sigs) > 0 {
			return errors.New("registry supplied signatures but no dist.integrity, so the signature cannot be checked")
		}
		if isOfficialRegistry(registryBase) {
			return fmt.Errorf("official registry %s returned neither a signature nor dist.integrity", registryBase)
		}
		return nil
	}

	if len(sigs) == 0 {
		if isOfficialRegistry(registryBase) {
			return fmt.Errorf("official registry %s returned no dist.signatures for %s@%s; "+
				"refusing to install from unauthenticated metadata", registryBase, pkg, version)
		}
		slog.Warn("upgrade: registry provided no signature — falling back to integrity check only",
			"registry", registryBase, "package", pkg, "version", version)
		return nil
	}

	keys, err := fetchNpmSigningKeys(ctx)
	if err != nil {
		// The anchor is what makes the signature meaningful. Treating an
		// unreachable keys endpoint as "skip" would let an attacker who can
		// block that one request downgrade the upgrade to integrity-only and
		// walk straight past this check, so it is fatal.
		return fmt.Errorf("cannot verify registry signature: npm signing keys unavailable: %w", err)
	}

	payload := fmt.Sprintf("%s@%s:%s", pkg, version, integrity)

	// sigs is non-empty here, so the loop always runs and every iteration
	// either records a failure or returns nil — lastErr is set if we fall out.
	var lastErr error
	for _, sig := range sigs {
		key, ok := findSigningKey(keys, sig.KeyID)
		if !ok {
			lastErr = fmt.Errorf("signature uses key %s, which is not among npm's published signing keys", sig.KeyID)
			continue
		}
		if err := verifyECDSASignature(key, sig.Sig, payload); err != nil {
			lastErr = err
			continue
		}
		slog.Info("upgrade: registry signature verified", "keyid", key.KeyID, "package", pkg, "version", version)
		return nil
	}

	return fmt.Errorf("registry signature verification failed: %w", lastErr)
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
