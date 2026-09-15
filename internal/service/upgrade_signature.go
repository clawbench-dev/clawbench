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
	"sort"
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

// Verification issue codes.
//
// These codes, not the human-readable messages, are the identity of a
// verification problem: the client echoes them back on start and the service
// compares them to decide whether the user consented to *this* situation.
//
// Codes must therefore be stable across two independent registry fetches for
// the same logical problem. That rules out encoding anything transport- or
// routing-dependent — the raw error text, the registry base that answered, and
// which of several equivalent branches matched. Those belong in the message,
// which is presentation only and may change freely.
const (
	// The registry supplied a signature but no integrity hash, so the signed
	// payload cannot be reconstructed.
	VerifyIssueSignatureWithoutIntegrity = "signature_without_integrity"

	// The registry returned neither a signature nor an integrity hash.
	VerifyIssueNeitherSignatureNorIntegrity = "neither_signature_nor_integrity"

	// The registry did not sign this release. Deliberately not split by whether
	// the registry is official: which registry answered depends on the fallback
	// order and on transient reachability, so splitting would make the code
	// unstable. The message still says which case it was.
	VerifyIssueSignatureMissing = "signature_missing"

	// npm's signing keys could not be fetched, so the signature could not be
	// checked at all.
	VerifyIssueSignatureKeysUnreachable = "signature_keys_unreachable"

	// A signature was present and did not verify against npm's published keys.
	VerifyIssueSignatureInvalid = "signature_invalid"

	// The registry supplied no integrity hash, so the download cannot be
	// checked for corruption or tampering.
	VerifyIssueNoIntegrityHash = "no_integrity_hash"
)

// verificationIssue pairs a stable code with the message shown to the user.
type verificationIssue struct {
	Code    string
	Message string
}

// verificationFingerprint renders issues as the value the client echoes back
// and the service compares. Sorting and de-duplicating makes it independent of
// the order issues were discovered in, so adding a check later cannot silently
// invalidate existing acknowledgments.
func verificationFingerprint(issues []verificationIssue) string {
	if len(issues) == 0 {
		return ""
	}
	codes := make([]string, 0, len(issues))
	for _, issue := range issues {
		codes = append(codes, issue.Code)
	}
	sort.Strings(codes)

	unique := codes[:0]
	for i, code := range codes {
		if i == 0 || code != codes[i-1] {
			unique = append(unique, code)
		}
	}
	return strings.Join(unique, ",")
}

// verificationMessages joins the human-readable messages, in the order the
// checks produced them.
func verificationMessages(issues []verificationIssue) string {
	msgs := make([]string, 0, len(issues))
	for _, issue := range issues {
		if msg := strings.TrimSpace(issue.Message); msg != "" {
			msgs = append(msgs, msg)
		}
	}
	return strings.Join(msgs, " ")
}

// verifyRegistrySignature checks npm's registry signature over the package
// identity and integrity hash, and returns the issues that prevented
// verification (empty when the signature verified).
//
// It never fails the upgrade. This makes the signature an advisory check, not a
// gate, and the consequence should be stated plainly: a registry that omits the
// signature field entirely is indistinguishable here from one that has none to
// offer, so a mirror determined to serve modified content can do so by simply
// not signing. What the check still catches is tampering with a response that
// does carry a signature (for example a proxy that passes npm's signatures
// through), and a stale signature that a naive mirror failed to strip.
//
// The messages keep the downgrade visible: the caller surfaces them and the
// user decides whether to proceed. Refusing outright would strand users whose
// network cannot reach npmjs, or whose mirror legitimately repackages tarballs.
//
// The returned codes are what the acknowledgment is compared against, so they
// must not vary with how the request happened to fail — see the codes above.
//
// An empty return means the signature was verified. The one check that does
// still abort an upgrade lives in downloadAndExtract: a tarball whose bytes do
// not match the expected hash is a corrupt or wrong download, not merely an
// unauthenticated one.
//
// Integrity is required to reconstruct the signed payload; without it the
// signature cannot be checked at all.
func verifyRegistrySignature(ctx context.Context, pkg, version, integrity string, sigs []npmSignature, registryBase string) []verificationIssue {
	if integrity == "" {
		if len(sigs) > 0 {
			slog.Warn("upgrade: signatures present but dist.integrity missing",
				"registry", registryBase, "package", pkg, "version", version)
			return []verificationIssue{{
				Code: VerifyIssueSignatureWithoutIntegrity,
				Message: fmt.Sprintf("The registry %s supplied a signature but no integrity hash, so the "+
					"release could not be authenticated.", registryBase),
			}}
		}
		if isOfficialRegistry(registryBase) {
			slog.Warn("upgrade: official registry returned neither signature nor integrity",
				"registry", registryBase, "package", pkg, "version", version)
			return []verificationIssue{{
				Code: VerifyIssueNeitherSignatureNorIntegrity,
				Message: fmt.Sprintf("The registry %s returned neither a signature nor an integrity hash, so "+
					"this release could not be authenticated.", registryBase),
			}}
		}
		return nil
	}

	if len(sigs) == 0 {
		slog.Warn("upgrade: registry provided no signature",
			"registry", registryBase, "package", pkg, "version", version)
		msg := fmt.Sprintf("The registry %s did not sign this release, so it could not be "+
			"authenticated against npm's signing keys.", registryBase)
		if isOfficialRegistry(registryBase) {
			// npm signs everything it publishes, so this is anomalous for an
			// official registry and worth saying so plainly. The code stays the
			// same either way — which registry answered is routing, not reason.
			msg = fmt.Sprintf("The official registry %s returned no signature for this release. npm signs "+
				"every published version, so this is unexpected — the release could not be authenticated.",
				registryBase)
		}
		return []verificationIssue{{Code: VerifyIssueSignatureMissing, Message: msg}}
	}

	keys, err := fetchNpmSigningKeys(ctx)
	if err != nil {
		slog.Warn("upgrade: cannot reach npm signing keys — signature not verified",
			"error", err, "package", pkg, "version", version)
		// Deliberately excludes the underlying error text: the code is compared
		// across two separate requests, and a transport error's message varies
		// between attempts (timeout, refused, DNS). The detail is in the log.
		return []verificationIssue{{
			Code: VerifyIssueSignatureKeysUnreachable,
			Message: "The release signature could not be verified because npm's signing keys were " +
				"unreachable, so the release could not be authenticated.",
		}}
	}

	payload := fmt.Sprintf("%s@%s:%s", pkg, version, integrity)

	// sigs is non-empty here, so the loop always runs and every iteration
	// either records a failure or returns nil — lastErr is set if we fall out.
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
		return nil
	}

	slog.Warn("upgrade: registry signature verification failed",
		"registry", registryBase, "package", pkg, "version", version, "error", lastErr)
	return []verificationIssue{{
		Code: VerifyIssueSignatureInvalid,
		Message: fmt.Sprintf("The release signature from %s did not verify (%v), so the release could not be "+
			"authenticated. A mirror that repackages the tarball changes its integrity hash and thereby "+
			"invalidates the original signature, but a tampered or substituted release produces the same "+
			"result — the two cannot be told apart from here.", registryBase, lastErr),
	}}
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
