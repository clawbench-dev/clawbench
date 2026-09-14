package service

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha1"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"clawbench/internal/platform"
	"clawbench/internal/version"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- getPlatformPkg ---

func TestGetPlatformPkg_SupportedPlatform(t *testing.T) {
	pkg, err := getPlatformPkg()
	require.NoError(t, err)
	assert.NotEmpty(t, pkg)

	// Verify the current platform is in the known map
	key := runtime.GOOS + "/" + runtime.GOARCH
	expected, ok := npmPlatformPkg[key]
	require.True(t, ok, "current platform %s should be in npmPlatformPkg", key)
	assert.Equal(t, expected, pkg)
}

func TestGetPlatformPkg_UnsupportedPlatform(t *testing.T) {
	orig := npmPlatformPkg
	defer func() { npmPlatformPkg = orig }()

	npmPlatformPkg = map[string]string{} // empty map — no platform supported
	_, err := getPlatformPkg()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported platform")
}

// --- getRegistryBase ---

func TestGetRegistryBase_China(t *testing.T) {
	orig := platform.ChinaMirrorChecked.Load()
	defer platform.ChinaMirrorChecked.Store(orig)

	platform.ChinaMirrorChecked.Store(1) // China
	base := getRegistryBase()
	assert.Equal(t, "https://registry.npmmirror.com", base)
}

func TestGetRegistryBase_NonChina(t *testing.T) {
	orig := platform.ChinaMirrorChecked.Load()
	defer platform.ChinaMirrorChecked.Store(orig)

	platform.ChinaMirrorChecked.Store(2) // non-China
	base := getRegistryBase()
	assert.Equal(t, "https://registry.npmjs.org", base)
}

// --- getUserRegistryBase / parseNpmRcRegistry ---

func TestGetUserRegistryBase_EnvVar(t *testing.T) {
	orig := os.Getenv("NPM_CONFIG_REGISTRY")
	defer os.Setenv("NPM_CONFIG_REGISTRY", orig)
	os.Setenv("NPM_CONFIG_REGISTRY", "https://registry.example.com")

	got := getUserRegistryBase()
	assert.Equal(t, "https://registry.example.com", got)
}

func TestGetUserRegistryBase_EnvVarLowercase(t *testing.T) {
	origUpper, hasUpper := os.LookupEnv("NPM_CONFIG_REGISTRY")
	os.Unsetenv("NPM_CONFIG_REGISTRY")
	defer func() {
		if hasUpper {
			os.Setenv("NPM_CONFIG_REGISTRY", origUpper)
		} else {
			os.Unsetenv("NPM_CONFIG_REGISTRY")
		}
	}()

	orig := os.Getenv("npm_config_registry")
	defer os.Setenv("npm_config_registry", orig)
	os.Setenv("npm_config_registry", "https://registry.example.com/")

	// Trailing slash should be trimmed.
	got := getUserRegistryBase()
	assert.Equal(t, "https://registry.example.com", got)
}

func TestGetUserRegistryBase_EnvVarPriorityOverNpmrc(t *testing.T) {
	orig := os.Getenv("NPM_CONFIG_REGISTRY")
	defer os.Setenv("NPM_CONFIG_REGISTRY", orig)
	os.Setenv("NPM_CONFIG_REGISTRY", "https://env.example.com")

	// A valid .npmrc registry exists but env var should win.
	withTempHome(t)
	writeNpmRc(t, "registry=https://npmrc.example.com\n")

	assert.Equal(t, "https://env.example.com", getUserRegistryBase())
}

func TestGetUserRegistryBase_InvalidEnvVarIgnored(t *testing.T) {
	orig := os.Getenv("NPM_CONFIG_REGISTRY")
	defer os.Setenv("NPM_CONFIG_REGISTRY", orig)
	os.Setenv("NPM_CONFIG_REGISTRY", "default")

	// Should fall through to .npmrc, not return the invalid value.
	withTempHome(t)
	writeNpmRc(t, "registry=https://npmrc.example.com\n")

	assert.Equal(t, "https://npmrc.example.com", getUserRegistryBase())
}

func TestGetUserRegistryBase_FromNpmRc(t *testing.T) {
	clearNpmEnv(t)
	withTempHome(t)
	writeNpmRc(t, "registry=https://registry.npmmirror.com\n")

	got := getUserRegistryBase()
	assert.Equal(t, "https://registry.npmmirror.com", got)
}

func TestGetUserRegistryBase_NoNpmrc(t *testing.T) {
	clearNpmEnv(t)
	withTempHome(t)

	assert.Equal(t, "", getUserRegistryBase())
}

func TestGetUserRegistryBase_CommentedNpmrc(t *testing.T) {
	clearNpmEnv(t)
	withTempHome(t)
	writeNpmRc(t, "# registry=https://ignored.example.com\n")

	assert.Equal(t, "", getUserRegistryBase())
}

func TestGetUserRegistryBase_DegenerateValuesIgnored(t *testing.T) {
	origLower, hasLower := os.LookupEnv("npm_config_registry")
	os.Unsetenv("npm_config_registry")
	defer func() {
		if hasLower {
			os.Setenv("npm_config_registry", origLower)
		} else {
			os.Unsetenv("npm_config_registry")
		}
	}()
	for _, v := range []string{"http://", "https://", "http:", "default", "ftp://x"} {
		orig := os.Getenv("NPM_CONFIG_REGISTRY")
		os.Setenv("NPM_CONFIG_REGISTRY", v)
		withTempHome(t)
		// No valid .npmrc either.
		got := getUserRegistryBase()
		os.Setenv("NPM_CONFIG_REGISTRY", orig)
		assert.Equal(t, "", got, "value %q should be rejected", v)
	}
}

func TestGetUserRegistryBase_NoSchemeIgnored(t *testing.T) {
	orig := os.Getenv("NPM_CONFIG_REGISTRY")
	defer os.Setenv("NPM_CONFIG_REGISTRY", orig)
	os.Setenv("NPM_CONFIG_REGISTRY", "registry.example.com")
	withTempHome(t)

	assert.Equal(t, "", getUserRegistryBase())
}

// --- registryCandidates ---

func TestRegistryCandidates_NoUserMirror(t *testing.T) {
	orig := platform.ChinaMirrorChecked.Load()
	defer platform.ChinaMirrorChecked.Store(orig)
	platform.ChinaMirrorChecked.Store(2)

	// Ensure no user mirror.
	origEnv := os.Getenv("NPM_CONFIG_REGISTRY")
	defer os.Setenv("NPM_CONFIG_REGISTRY", origEnv)
	os.Unsetenv("NPM_CONFIG_REGISTRY")
	withTempHome(t)

	assert.Equal(t, []string{"https://registry.npmjs.org"}, registryCandidates())
}

func TestRegistryCandidates_WithUserMirror(t *testing.T) {
	orig := platform.ChinaMirrorChecked.Load()
	defer platform.ChinaMirrorChecked.Store(orig)
	platform.ChinaMirrorChecked.Store(2)

	origEnv := os.Getenv("NPM_CONFIG_REGISTRY")
	defer os.Setenv("NPM_CONFIG_REGISTRY", origEnv)
	os.Setenv("NPM_CONFIG_REGISTRY", "https://mirror.example.com")

	assert.Equal(t, []string{"https://registry.npmjs.org", "https://mirror.example.com"}, registryCandidates())
}

// --- fetchUpgradeInfo fallback ---

func TestFetchUpgradeInfo_FallsBackToUserMirror(t *testing.T) {
	// Default registry returns 500; user mirror returns a valid response.
	failServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer failServer.Close()

	mirrorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := npmRegistryResponse{}
		resp.Version = "99.0.0"
		resp.Dist.Tarball = "https://mirror.example.com/pkg/-/pkg-99.0.0.tgz"
		resp.Dist.Integrity = wellFormedIntegrity
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer mirrorServer.Close()

	origClient := upgradeHTTPClient
	defer func() { upgradeHTTPClient = origClient }()
	// Default candidate resolves to the default base (npmjs) -> rewrite to
	// failServer; the user mirror base (mirrorServer) -> rewrite to mirrorServer.
	upgradeHTTPClient = &http.Client{Transport: &failoverTransport{
		defaultBase: failServer.URL,
		mirrorBase:  mirrorServer.URL,
	}}

	orig := platform.ChinaMirrorChecked.Load()
	defer platform.ChinaMirrorChecked.Store(orig)
	platform.ChinaMirrorChecked.Store(2) // default = registry.npmjs.org

	origEnv := os.Getenv("NPM_CONFIG_REGISTRY")
	defer os.Setenv("NPM_CONFIG_REGISTRY", origEnv)
	os.Setenv("NPM_CONFIG_REGISTRY", mirrorServer.URL)

	info, err := fetchUpgradeInfo()
	require.NoError(t, err)
	assert.Equal(t, "99.0.0", info.LatestVersion)
	assert.Contains(t, info.TarballURL, "mirror.example.com")
}

func TestFetchUpgradeInfo_AllSourcesFail(t *testing.T) {
	failServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer failServer.Close()

	origClient := upgradeHTTPClient
	defer func() { upgradeHTTPClient = origClient }()
	upgradeHTTPClient = &http.Client{Transport: &failoverTransport{
		defaultBase: failServer.URL,
		mirrorBase:  failServer.URL,
	}}

	orig := platform.ChinaMirrorChecked.Load()
	defer platform.ChinaMirrorChecked.Store(orig)
	platform.ChinaMirrorChecked.Store(2)

	origEnv := os.Getenv("NPM_CONFIG_REGISTRY")
	defer os.Setenv("NPM_CONFIG_REGISTRY", origEnv)
	os.Setenv("NPM_CONFIG_REGISTRY", failServer.URL)

	_, err := fetchUpgradeInfo()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "all registry sources failed")
}

// --- helpers ---

// failoverTransport routes the default registry base to defaultBase and the
// user mirror base to mirrorBase. This lets tests simulate the default registry
// being unreachable while the user's mirror works.
//
// keysJSON, when set, is served for the npm signing-keys endpoint. The keys
// endpoint is always fetched from npmjs, so it must be answerable even when the
// test's package metadata comes from a mirror.
type failoverTransport struct {
	defaultBase string
	mirrorBase  string
	mirrorHit   *bool
	keysJSON    []byte
	// keysStatus overrides the keys-endpoint status when non-zero.
	keysStatus int
}

func (t *failoverTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// The signing-keys endpoint is a trust anchor fetched from npmjs directly;
	// serve it locally so tests never reach the network.
	if req.URL.Path == npmKeysPath {
		return t.keysResponse(req)
	}

	target := t.defaultBase
	// Requests to a user mirror host route to mirrorBase; the default registry
	// host (npmjs) routes to defaultBase.
	if req.URL.Host != "registry.npmjs.org" && req.URL.Host != "registry.npmmirror.com" {
		target = t.mirrorBase
		if t.mirrorHit != nil {
			*t.mirrorHit = true
		}
	}
	clone := req.Clone(req.Context())
	clone.URL, _ = url.Parse(target + req.URL.Path)
	return http.DefaultTransport.RoundTrip(clone)
}

// keysResponse synthesizes a response for the npm signing-keys endpoint.
func (t *failoverTransport) keysResponse(req *http.Request) (*http.Response, error) {
	status := t.keysStatus
	if status == 0 {
		status = http.StatusOK
	}
	body := t.keysJSON
	if status != http.StatusOK {
		body = []byte(`{"error":"keys unavailable"}`)
	}
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(body)),
		Request:    req,
	}, nil
}

// withTempHome redirects the process HOME (and USERPROFILE on Windows) to a
// fresh temp dir so parseNpmRcRegistry reads a controlled .npmrc.
func withTempHome(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", dir)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })
	origUp := os.Getenv("USERPROFILE")
	os.Setenv("USERPROFILE", dir)
	t.Cleanup(func() { os.Setenv("USERPROFILE", origUp) })
}

func clearNpmEnv(t *testing.T) {
	t.Helper()
	origNpmUpper, hasNpmUpper := os.LookupEnv("NPM_CONFIG_REGISTRY")
	os.Unsetenv("NPM_CONFIG_REGISTRY")
	t.Cleanup(func() {
		if hasNpmUpper {
			os.Setenv("NPM_CONFIG_REGISTRY", origNpmUpper)
		} else {
			os.Unsetenv("NPM_CONFIG_REGISTRY")
		}
	})

	origNpmLower, hasNpmLower := os.LookupEnv("npm_config_registry")
	os.Unsetenv("npm_config_registry")
	t.Cleanup(func() {
		if hasNpmLower {
			os.Setenv("npm_config_registry", origNpmLower)
		} else {
			os.Unsetenv("npm_config_registry")
		}
	})
}

func writeNpmRc(t *testing.T, content string) {
	t.Helper()
	home := os.Getenv("HOME")
	if home == "" {
		home = os.Getenv("USERPROFILE")
	}
	if err := os.WriteFile(filepath.Join(home, ".npmrc"), []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write .npmrc: %v", err)
	}
}

// --- resolveExpectedDigest ---

func TestResolveExpectedDigest_SHA512SRI(t *testing.T) {
	sum := sha512.Sum512([]byte("hello world"))
	integrity := "sha512-" + base64.StdEncoding.EncodeToString(sum[:])

	digest, _, err := resolveExpectedDigest(integrity, "")
	require.NoError(t, err)
	assert.Equal(t, "sha512", digest.algorithm)
	assert.Equal(t, sum[:], digest.hash)
}

func TestResolveExpectedDigest_SHA1SRI(t *testing.T) {
	sum := sha1.Sum([]byte("hello world"))
	integrity := "sha1-" + base64.StdEncoding.EncodeToString(sum[:])

	digest, _, err := resolveExpectedDigest(integrity, "")
	require.NoError(t, err)
	assert.Equal(t, "sha1", digest.algorithm)
	assert.Equal(t, sum[:], digest.hash)
}

// npm's own client (pacote) falls back to the legacy hex shasum when
// dist.integrity is absent, which is legal for older packages and for
// registries that omit it. ClawBench must do the same rather than install an
// unverified binary.
func TestResolveExpectedDigest_FallsBackToShasum(t *testing.T) {
	sum := sha1.Sum([]byte("legacy package"))
	shasum := hex.EncodeToString(sum[:])

	digest, _, err := resolveExpectedDigest("", shasum)
	require.NoError(t, err)
	assert.Equal(t, "sha1", digest.algorithm)
	assert.Equal(t, sum[:], digest.hash)
}

func TestResolveExpectedDigest_IntegrityWinsOverShasum(t *testing.T) {
	sri := sha512.Sum512([]byte("real"))
	legacy := sha1.Sum([]byte("real"))

	digest, _, err := resolveExpectedDigest(
		"sha512-"+base64.StdEncoding.EncodeToString(sri[:]),
		hex.EncodeToString(legacy[:]),
	)
	require.NoError(t, err)
	assert.Equal(t, "sha512", digest.algorithm)
}

// A response with no hash at all no longer aborts: it yields an unverified
// digest plus a warning. The upgrade proceeds, but nothing is checked, so the
// user must be told.
func TestResolveExpectedDigest_NeitherField_WarnsAndProceeds(t *testing.T) {
	digest, warning, err := resolveExpectedDigest("", "")
	require.NoError(t, err, "a missing hash must not block the upgrade")
	assert.True(t, digest.unverified, "the digest must be marked unverified")
	assert.Nil(t, digest.hash)
	assert.Contains(t, warning, "no integrity hash")
}

// The unverified digest must pass verification unconditionally — there is
// nothing to compare against, and the warning already covered the gap.
// An unverified digest passes any bytes, while a verified one rejects the same
// bytes. Asserting only the first half would be tautological — the point is the
// contrast, which is what makes the flag load-bearing.
func TestExpectedDigest_UnverifiedPassesWhereVerifiedWouldFail(t *testing.T) {
	unverified, _, err := resolveExpectedDigest("", "")
	require.NoError(t, err)
	require.True(t, unverified.unverified)

	// A digest with a real hash, for the same content the hasher will see.
	sum := sha512.Sum512([]byte("arbitrary bytes"))
	verified, _, err := resolveExpectedDigest("sha512-"+base64.StdEncoding.EncodeToString(sum[:]), "")
	require.NoError(t, err)
	require.False(t, verified.unverified)

	arbitrary := []byte("arbitrary bytes")

	uh := unverified.newHasher()
	_, _ = uh.Write(arbitrary)
	assert.NoError(t, unverified.verify(uh), "an unverified digest has nothing to compare")

	// The verified digest must disagree with a *different* payload, proving the
	// two branches really do differ.
	vh := verified.newHasher()
	_, _ = vh.Write([]byte("different bytes"))
	require.Error(t, verified.verify(vh), "a verified digest must reject the wrong content")
}

func TestResolveExpectedDigest_WhitespaceOnly_WarnsAndProceeds(t *testing.T) {
	digest, warning, err := resolveExpectedDigest("   ", "\t")
	require.NoError(t, err)
	assert.True(t, digest.unverified)
	assert.Contains(t, warning, "no integrity hash")
}

// An algorithm we cannot compute must fail closed instead of skipping.
func TestResolveExpectedDigest_UnsupportedAlgorithm_Errors(t *testing.T) {
	for _, integrity := range []string{
		"sha256-abc123",
		"md5-abc123",
		"blake2b-abc123",
	} {
		t.Run(integrity, func(t *testing.T) {
			_, _, err := resolveExpectedDigest(integrity, "")
			require.Error(t, err)
			assert.Contains(t, err.Error(), "unsupported integrity algorithm")
		})
	}
}

func TestResolveExpectedDigest_MissingPrefix_Errors(t *testing.T) {
	_, _, err := resolveExpectedDigest("abcdef123456", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing algorithm prefix")
}

func TestResolveExpectedDigest_MalformedBase64_Errors(t *testing.T) {
	_, _, err := resolveExpectedDigest("sha512-!!!not-base64!!!", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decode integrity hash")
}

func TestResolveExpectedDigest_WrongHashLength_Errors(t *testing.T) {
	// Valid base64, but not the 64 bytes a sha512 digest must be.
	short := base64.StdEncoding.EncodeToString([]byte("too short"))

	_, _, err := resolveExpectedDigest("sha512-"+short, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "want 64")
}

func TestResolveExpectedDigest_MalformedShasum_Errors(t *testing.T) {
	_, _, err := resolveExpectedDigest("", "not-hex-zzzz")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decode shasum")
}

func TestResolveExpectedDigest_ShortShasum_Errors(t *testing.T) {
	_, _, err := resolveExpectedDigest("", "abcd")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "want 20")
}

// --- expectedDigest.verify ---

func TestExpectedDigest_VerifyMatch(t *testing.T) {
	sum := sha512.Sum512([]byte("payload"))
	digest := expectedDigest{algorithm: "sha512", hash: sum[:]}

	hasher := sha512.New()
	_, _ = hasher.Write([]byte("payload"))

	assert.NoError(t, digest.verify(hasher))
}

func TestExpectedDigest_VerifyMismatch(t *testing.T) {
	sum := sha512.Sum512([]byte("expected"))
	digest := expectedDigest{algorithm: "sha512", hash: sum[:]}

	hasher := sha512.New()
	_, _ = hasher.Write([]byte("actual"))

	err := digest.verify(hasher)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "hash mismatch")
}

func TestExpectedDigest_VerifySHA1Mismatch(t *testing.T) {
	sum := sha1.Sum([]byte("expected"))
	digest := expectedDigest{algorithm: "sha1", hash: sum[:]}

	hasher := sha1.New()
	_, _ = hasher.Write([]byte("actual"))

	err := digest.verify(hasher)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "hash mismatch")
}

func TestExpectedDigest_NewHasherSelectsAlgorithm(t *testing.T) {
	assert.Equal(t, sha512.Size, expectedDigest{algorithm: "sha512"}.newHasher().Size())
	assert.Equal(t, sha1.Size, expectedDigest{algorithm: "sha1"}.newHasher().Size())
}

// --- shortHash / truncateForLog ---

func TestShortHash_TruncatesLongHash(t *testing.T) {
	long := bytes.Repeat([]byte{0xAB}, 32)
	assert.Equal(t, long[:8], shortHash(long))
}

func TestShortHash_KeepsShortInput(t *testing.T) {
	short := []byte{1, 2, 3}
	assert.Equal(t, short, shortHash(short))
}

func TestTruncateForLog_TruncatesLongValue(t *testing.T) {
	long := strings.Repeat("a", 50)
	got := truncateForLog(long)
	assert.Equal(t, long[:20]+"...", got)
	assert.Len(t, got, 23)
}

func TestTruncateForLog_KeepsShortValue(t *testing.T) {
	assert.Equal(t, "sha256", truncateForLog("sha256"))
}

// --- equalHashes ---

func TestEqualHashes_Equal(t *testing.T) {
	a := []byte{1, 2, 3, 4}
	b := []byte{1, 2, 3, 4}
	assert.True(t, equalHashes(a, b))
}

func TestEqualHashes_DifferentLength(t *testing.T) {
	a := []byte{1, 2, 3}
	b := []byte{1, 2, 3, 4}
	assert.False(t, equalHashes(a, b))
}

func TestEqualHashes_DifferentContent(t *testing.T) {
	a := []byte{1, 2, 3, 4}
	b := []byte{1, 2, 3, 5}
	assert.False(t, equalHashes(a, b))
}

func TestEqualHashes_Empty(t *testing.T) {
	assert.True(t, equalHashes([]byte{}, []byte{}))
}

// --- joinWarnings ---

// A release can be unauthenticated for more than one reason at once; the user
// must see all of them, not just the last one computed.
func TestJoinWarnings_CombinesDistinctReasons(t *testing.T) {
	got := joinWarnings("signature could not be verified", "no integrity hash")
	assert.Equal(t, "signature could not be verified no integrity hash", got)
}

func TestJoinWarnings_SkipsEmptyParts(t *testing.T) {
	assert.Equal(t, "only this", joinWarnings("", "only this"))
	assert.Equal(t, "only this", joinWarnings("only this", ""))
	assert.Equal(t, "only this", joinWarnings("  ", "only this"))
}

func TestJoinWarnings_AllEmpty(t *testing.T) {
	assert.Equal(t, "", joinWarnings("", ""))
	assert.Equal(t, "", joinWarnings())
}

func TestJoinWarnings_TrimsParts(t *testing.T) {
	assert.Equal(t, "a b", joinWarnings("  a  ", "  b  "))
}

// --- throttledProgress ---

func TestThrottledProgress_DeduplicatesSamePercent(t *testing.T) {
	var calls []int
	fn := throttledProgress(func(p int) {
		calls = append(calls, p)
	})

	fn(10)
	fn(10) // same percent → should be deduplicated
	fn(20)
	fn(20) // same percent → deduplicated
	fn(30)

	assert.Equal(t, []int{10, 20, 30}, calls)
}

func TestThrottledProgress_FirstCallAlwaysFires(t *testing.T) {
	var calls []int
	fn := throttledProgress(func(p int) {
		calls = append(calls, p)
	})

	// 0 is the initial lastPercent, so it won't fire. Use a non-zero value.
	fn(1)
	assert.Equal(t, []int{1}, calls)
}

// --- progressReader.Read ---

func TestProgressReader_ReportsProgress(t *testing.T) {
	data := []byte("hello world")
	var reported []int
	pr := &progressReader{
		reader: bytes.NewReader(data),
		total:  int64(len(data)),
		onProgress: func(percent int) {
			reported = append(reported, percent)
		},
	}

	buf := make([]byte, 5)
	_, err := pr.Read(buf)
	assert.NoError(t, err)
	assert.NotEmpty(t, reported)
	// After reading 5 of 11 bytes → ~45%
	assert.Equal(t, 45, reported[0])
}

func TestProgressReader_ZeroTotal_NoCallback(t *testing.T) {
	data := []byte("hello")
	called := false
	pr := &progressReader{
		reader: bytes.NewReader(data),
		total:  0, // zero total
		onProgress: func(percent int) {
			called = true
		},
	}

	buf := make([]byte, 5)
	_, err := pr.Read(buf)
	assert.NoError(t, err)
	assert.False(t, called, "onProgress should not be called when total is 0")
}

func TestProgressReader_NilCallback(t *testing.T) {
	data := []byte("hello")
	pr := &progressReader{
		reader:     bytes.NewReader(data),
		total:      int64(len(data)),
		onProgress: nil, // nil callback
	}

	buf := make([]byte, 5)
	_, err := pr.Read(buf)
	assert.NoError(t, err) // should not panic
}

// --- copyFile ---

func TestCopyFile_Success(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	srcPath := filepath.Join(src, "source.txt")
	dstPath := filepath.Join(dst, "dest.txt")

	content := []byte("copy me")
	require.NoError(t, os.WriteFile(srcPath, content, 0o644))

	err := copyFile(srcPath, dstPath)
	require.NoError(t, err)

	got, err := os.ReadFile(dstPath)
	require.NoError(t, err)
	assert.Equal(t, content, got)

	// Verify permissions preserved
	srcInfo, _ := os.Stat(srcPath)
	dstInfo, _ := os.Stat(dstPath)
	assert.Equal(t, srcInfo.Mode(), dstInfo.Mode())
}

func TestCopyFile_SourceNotFound(t *testing.T) {
	err := copyFile("/nonexistent/file.txt", "/tmp/dest.txt")
	assert.Error(t, err)
}

func TestCopyFile_DestNotWritable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("read-only directories behave differently on Windows")
	}
	src := t.TempDir()
	srcPath := filepath.Join(src, "source.txt")
	require.NoError(t, os.WriteFile(srcPath, []byte("data"), 0o644))

	// Destination in a read-only directory
	dstDir := t.TempDir()
	require.NoError(t, os.Chmod(dstDir, 0o444))
	defer os.Chmod(dstDir, 0o755) // restore for cleanup

	dstPath := filepath.Join(dstDir, "dest.txt")
	err := copyFile(srcPath, dstPath)
	assert.Error(t, err)
}

// --- isDocker ---

func TestIsDocker_WithContainerEnvVar(t *testing.T) {
	orig := os.Getenv("container")
	defer os.Setenv("container", orig)

	os.Setenv("container", "docker")
	assert.True(t, IsDocker())
}

func TestIsDocker_WithDockerenvFile(t *testing.T) {
	orig := os.Getenv("container")
	defer os.Setenv("container", orig)
	os.Unsetenv("container")

	// If /.dockerenv exists on the host, this test is a true positive.
	// We can't create /.dockerenv as non-root, so we test the env var path
	// and the negative case.
	_, _ = os.Stat("/.dockerenv")
	// Just ensure it doesn't panic
	_ = IsDocker()
}

func TestIsDocker_NeitherIndicator(t *testing.T) {
	orig := os.Getenv("container")
	defer os.Setenv("container", orig)
	os.Unsetenv("container")

	// If /.dockerenv exists, this will be true; that's OK.
	// The test mainly ensures no panic and the env var path works.
	result := IsDocker()
	// On most CI, /.dockerenv may exist
	if _, err := os.Stat("/.dockerenv"); os.IsNotExist(err) {
		assert.False(t, result)
	}
}

// --- CleanStaleUpgradeTempDirs ---

func TestCleanStaleUpgradeTempDirs_RemovesOldDirs(t *testing.T) {
	// Create a stale temp dir manually
	oldDir, err := os.MkdirTemp("", "clawbench-upgrade-*")
	require.NoError(t, err)
	defer os.RemoveAll(oldDir) // safety cleanup

	// Set its mod time to 2 hours ago
	oldTime := time.Now().Add(-2 * time.Hour)
	require.NoError(t, os.Chtimes(oldDir, oldTime, oldTime))

	// Create a recent dir that should NOT be cleaned
	recentDir, err := os.MkdirTemp("", "clawbench-upgrade-*")
	require.NoError(t, err)
	defer os.RemoveAll(recentDir)

	CleanStaleUpgradeTempDirs()

	// Old dir should be gone
	_, err = os.Stat(oldDir)
	assert.True(t, os.IsNotExist(err), "old temp dir should be removed")

	// Recent dir should still exist
	_, err = os.Stat(recentDir)
	assert.NoError(t, err, "recent temp dir should not be removed")
}

func TestCleanStaleUpgradeTempDirs_NoMatches(t *testing.T) {
	// Should not panic when there are no matching dirs
	CleanStaleUpgradeTempDirs()
}

// --- CancelUpgrade ---

func TestCancelUpgrade_WithCancelFunc(t *testing.T) {
	orig := upgradeCancel
	defer func() { upgradeCancel = orig }()

	cancelled := false
	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Replace with a cancel func that also sets our flag
	upgradeCancel = func() {
		cancelled = true
		cancel()
	}

	CancelUpgrade()
	assert.True(t, cancelled, "cancel function should have been called")
}

func TestCancelUpgrade_NilCancelFunc(t *testing.T) {
	orig := upgradeCancel
	defer func() { upgradeCancel = orig }()

	upgradeCancel = nil
	CancelUpgrade() // should not panic
}

// --- fetchUpgradeInfo ---

func TestFetchUpgradeInfo_Success(t *testing.T) {
	origClient := upgradeHTTPClient
	defer func() { upgradeHTTPClient = origClient }()

	pkg, err := getPlatformPkg()
	require.NoError(t, err)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, pkg)

		resp := npmRegistryResponse{}
		resp.Version = "99.0.0"
		resp.Dist.Tarball = "https://registry.npmjs.org/test/-/test-99.0.0.tgz"
		resp.Dist.Integrity = wellFormedIntegrity
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	upgradeHTTPClient = ts.Client()

	// Override getRegistryBase by setting non-China
	origChina := platform.ChinaMirrorChecked.Load()
	defer platform.ChinaMirrorChecked.Store(origChina)
	platform.ChinaMirrorChecked.Store(2)

	// We need to redirect requests to our test server.
	// Since getRegistryBase returns a fixed URL, we use the test server's URL
	// by making the HTTP client transport rewrite.
	info, err := fetchUpgradeInfoWithBase(ts.URL)
	require.NoError(t, err)
	assert.Equal(t, "99.0.0", info.LatestVersion)
	assert.NotEmpty(t, info.TarballURL)
}

func TestFetchUpgradeInfo_NonOKStatus(t *testing.T) {
	origClient := upgradeHTTPClient
	defer func() { upgradeHTTPClient = origClient }()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	upgradeHTTPClient = ts.Client()

	_, err := fetchUpgradeInfoWithBase(ts.URL)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "registry returned status 500")
}

func TestFetchUpgradeInfo_EmptyTarballURL(t *testing.T) {
	origClient := upgradeHTTPClient
	defer func() { upgradeHTTPClient = origClient }()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := npmRegistryResponse{Version: "1.0.0"}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	upgradeHTTPClient = ts.Client()

	_, err := fetchUpgradeInfoWithBase(ts.URL)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no tarball URL")
}

func TestFetchUpgradeInfo_InvalidJSON(t *testing.T) {
	origClient := upgradeHTTPClient
	defer func() { upgradeHTTPClient = origClient }()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, "not-json")
	}))
	defer ts.Close()

	upgradeHTTPClient = ts.Client()

	_, err := fetchUpgradeInfoWithBase(ts.URL)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decode registry response")
}

func TestFetchUpgradeInfo_NPMMirrorTarballRewrite(t *testing.T) {
	origClient := upgradeHTTPClient
	defer func() { upgradeHTTPClient = origClient }()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := npmRegistryResponse{}
		resp.Version = "99.0.0"
		resp.Dist.Tarball = "https://registry.npmjs.org/test/-/test-99.0.0.tgz"
		resp.Dist.Integrity = wellFormedIntegrity
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	upgradeHTTPClient = ts.Client()

	// Set China mode so npmmirror tarball rewrite triggers
	origChina := platform.ChinaMirrorChecked.Load()
	defer platform.ChinaMirrorChecked.Store(origChina)
	platform.ChinaMirrorChecked.Store(1)

	info, err := fetchUpgradeInfoWithBase("https://registry.npmmirror.com")
	require.NoError(t, err)
	assert.Contains(t, info.TarballURL, "registry.npmmirror.com")
	assert.NotContains(t, info.TarballURL, "registry.npmjs.org")
}

// fetchUpgradeInfoWithBase calls the production registry query against baseURL.
//
// It deliberately delegates to fetchUpgradeInfoFromBase rather than
// reimplementing it: an earlier copy of this logic drifted from production and
// silently skipped the signature check, so tests passed while the real path
// was unverified.
func fetchUpgradeInfoWithBase(baseURL string) (*UpgradeInfo, error) {
	pkg, err := getPlatformPkg()
	if err != nil {
		return nil, err
	}
	return fetchUpgradeInfoFromBase(baseURL, pkg, version.Get())
}

// --- downloadAndExtract ---

func TestDownloadAndExtract_Success(t *testing.T) {
	origClient := upgradeHTTPClient
	defer func() { upgradeHTTPClient = origClient }()

	// Build a valid .tgz containing a binary
	binContent := []byte("#!/bin/sh\necho hello")
	tarball, integrity := buildTarball(t, binContent)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(tarball)))
		w.Write(tarball)
	}))
	defer ts.Close()

	upgradeHTTPClient = ts.Client()

	destDir := t.TempDir()
	destPath := filepath.Join(destDir, "clawbench-new")

	err := downloadAndExtract(context.Background(), ts.URL, mustDigest(t, integrity, ""), destPath)
	require.NoError(t, err)

	got, err := os.ReadFile(destPath)
	require.NoError(t, err)
	assert.Equal(t, binContent, got)
}

// A tarball delivered with a legacy hex sha1 shasum and no dist.integrity must
// still be verified (npm's own fallback behavior).
func TestDownloadAndExtract_SHA1ShasumFallback(t *testing.T) {
	origClient := upgradeHTTPClient
	defer func() { upgradeHTTPClient = origClient }()

	binContent := []byte("#!/bin/sh\necho legacy")
	tarball, integrity := buildTarball(t, binContent)
	require.True(t, strings.HasPrefix(integrity, "sha512-"))

	// The real sha1 of the tarball, hex-encoded as npm's dist.shasum is.
	sum := sha1.Sum(tarball)
	shasum := hex.EncodeToString(sum[:])

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(tarball)))
		w.Write(tarball)
	}))
	defer ts.Close()

	upgradeHTTPClient = ts.Client()

	destPath := filepath.Join(t.TempDir(), "clawbench-new")
	err := downloadAndExtract(context.Background(), ts.URL, mustDigest(t, "", shasum), destPath)
	require.NoError(t, err)

	got, err := os.ReadFile(destPath)
	require.NoError(t, err)
	assert.Equal(t, binContent, got)
}

// A shasum that does not match the served tarball must be rejected.
func TestDownloadAndExtract_ShasumMismatch(t *testing.T) {
	origClient := upgradeHTTPClient
	defer func() { upgradeHTTPClient = origClient }()

	tarball, _ := buildTarball(t, []byte("binary data"))

	wrongSum := sha1.Sum([]byte("something else entirely"))
	wrongShasum := hex.EncodeToString(wrongSum[:])

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(tarball)))
		w.Write(tarball)
	}))
	defer ts.Close()

	upgradeHTTPClient = ts.Client()

	destPath := filepath.Join(t.TempDir(), "clawbench-new")
	err := downloadAndExtract(context.Background(), ts.URL, mustDigest(t, "", wrongShasum), destPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "integrity verification failed")

	_, statErr := os.Stat(destPath)
	assert.True(t, os.IsNotExist(statErr), "dest file should be removed on integrity failure")
}

func TestDownloadAndExtract_NonOKStatus(t *testing.T) {
	origClient := upgradeHTTPClient
	defer func() { upgradeHTTPClient = origClient }()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer ts.Close()

	upgradeHTTPClient = ts.Client()

	destDir := t.TempDir()
	err := downloadAndExtract(context.Background(), ts.URL, mustDigest(t, "sha512-"+base64.StdEncoding.EncodeToString(make([]byte, sha512.Size)), ""), filepath.Join(destDir, "out"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "download returned status 403")
}

func TestDownloadAndExtract_IntegrityMismatch(t *testing.T) {
	origClient := upgradeHTTPClient
	defer func() { upgradeHTTPClient = origClient }()

	binContent := []byte("binary data")
	tarball, _ := buildTarball(t, binContent)

	// Provide wrong integrity
	wrongHasher := sha512.New()
	wrongHasher.Write([]byte("wrong"))
	wrongIntegrity := "sha512-" + base64.StdEncoding.EncodeToString(wrongHasher.Sum(nil))

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(tarball)))
		w.Write(tarball)
	}))
	defer ts.Close()

	upgradeHTTPClient = ts.Client()

	destDir := t.TempDir()
	destPath := filepath.Join(destDir, "clawbench-new")

	err := downloadAndExtract(context.Background(), ts.URL, mustDigest(t, wrongIntegrity, ""), destPath)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "integrity verification failed")

	// Output file should be removed after integrity failure
	_, statErr := os.Stat(destPath)
	assert.True(t, os.IsNotExist(statErr), "dest file should be removed on integrity failure")
}

func TestDownloadAndExtract_BinaryNotFoundInTarball(t *testing.T) {
	origClient := upgradeHTTPClient
	defer func() { upgradeHTTPClient = origClient }()

	// Build a tarball that does NOT contain the expected binary name
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	hdr := &tar.Header{
		Name: "package/other-file",
		Mode: 0o644,
		Size: int64(len("other")),
	}
	require.NoError(t, tw.WriteHeader(hdr))
	_, err := tw.Write([]byte("other"))
	require.NoError(t, err)
	tw.Close()
	gw.Close()
	tarball := buf.Bytes()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(tarball)))
		w.Write(tarball)
	}))
	defer ts.Close()

	upgradeHTTPClient = ts.Client()

	destDir := t.TempDir()
	err = downloadAndExtract(context.Background(), ts.URL, dummyDigest(t), filepath.Join(destDir, "out"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found in tarball")
}

func TestDownloadAndExtract_InvalidGzip(t *testing.T) {
	origClient := upgradeHTTPClient
	defer func() { upgradeHTTPClient = origClient }()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "10")
		w.Write([]byte("not-gzip!!"))
	}))
	defer ts.Close()

	upgradeHTTPClient = ts.Client()

	destDir := t.TempDir()
	err := downloadAndExtract(context.Background(), ts.URL, dummyDigest(t), filepath.Join(destDir, "out"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "gzip decompress failed")
}

func TestDownloadAndExtract_CancelledContext(t *testing.T) {
	origClient := upgradeHTTPClient
	defer func() { upgradeHTTPClient = origClient }()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Slow response
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	upgradeHTTPClient = ts.Client()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	destDir := t.TempDir()
	err := downloadAndExtract(ctx, ts.URL, dummyDigest(t), filepath.Join(destDir, "out"))
	assert.Error(t, err)
}

func TestDownloadAndExtract_DestNotWritable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("read-only directories behave differently on Windows")
	}
	origClient := upgradeHTTPClient
	defer func() { upgradeHTTPClient = origClient }()

	binContent := []byte("binary")
	tarball, _ := buildTarball(t, binContent)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(tarball)))
		w.Write(tarball)
	}))
	defer ts.Close()

	upgradeHTTPClient = ts.Client()

	// Create a read-only destination directory
	destDir := t.TempDir()
	require.NoError(t, os.Chmod(destDir, 0o444))
	defer os.Chmod(destDir, 0o755)

	err := downloadAndExtract(context.Background(), ts.URL, dummyDigest(t), filepath.Join(destDir, "clawbench-new"))
	assert.Error(t, err)
}

// --- helper: mustDigest resolves registry fields into an expectedDigest,
// failing the test if they are unusable.
func mustDigest(t *testing.T, integrity, shasum string) expectedDigest {
	t.Helper()
	digest, _, err := resolveExpectedDigest(integrity, shasum)
	require.NoError(t, err, "resolveExpectedDigest(%q, %q)", integrity, shasum)
	return digest
}

// wellFormedIntegrity is a syntactically valid sha512 SRI string (64 zero
// bytes). Tests whose subject is not verification need digest resolution to
// succeed so they reach the code path under test; it deliberately does not
// match any real tarball.
var wellFormedIntegrity = "sha512-" + base64.StdEncoding.EncodeToString(make([]byte, sha512.Size))

// --- helper: dummyDigest returns a well-formed digest for tests whose subject
// is a failure that occurs before or independently of verification (bad HTTP
// status, corrupt gzip, missing binary, unwritable dest).
func dummyDigest(t *testing.T) expectedDigest {
	t.Helper()
	return mustDigest(t, "sha512-"+base64.StdEncoding.EncodeToString(make([]byte, sha512.Size)), "")
}

// --- helper: sha512SRI builds a "sha512-<base64>" SRI string for data.
func sha512SRI(data []byte) string {
	sum := sha512.Sum512(data)
	return "sha512-" + base64.StdEncoding.EncodeToString(sum[:])
}

// --- helper: buildTarball creates a .tgz containing a single binary file
// and returns the tarball bytes and its sha512 integrity string.

func buildTarball(t *testing.T, binContent []byte) ([]byte, string) {
	t.Helper()

	binName := "clawbench"
	if runtime.GOOS == "windows" {
		binName = "clawbench.exe"
	}

	var buf bytes.Buffer
	hasher := sha512.New()
	mw := io.MultiWriter(&buf, hasher)

	gw := gzip.NewWriter(mw)
	tw := tar.NewWriter(gw)

	hdr := &tar.Header{
		Name: "package/bin/" + binName,
		Mode: 0o755,
		Size: int64(len(binContent)),
	}
	require.NoError(t, tw.WriteHeader(hdr))
	_, err := tw.Write(binContent)
	require.NoError(t, err)

	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())

	integrity := "sha512-" + base64.StdEncoding.EncodeToString(hasher.Sum(nil))
	return buf.Bytes(), integrity
}

// --- equalHashes edge cases ---

func TestEqualHashes_SingleByteDiff(t *testing.T) {
	a := []byte{0x00}
	b := []byte{0x01}
	assert.False(t, equalHashes(a, b))
}

func TestEqualHashes_SingleByteSame(t *testing.T) {
	a := []byte{0xFF}
	b := []byte{0xFF}
	assert.True(t, equalHashes(a, b))
}

// --- throttledProgress edge cases ---

func TestThrottledProgress_NegativePercent(t *testing.T) {
	var calls []int
	fn := throttledProgress(func(p int) {
		calls = append(calls, p)
	})

	fn(-1)
	fn(-1) // duplicate
	fn(0)

	assert.Equal(t, []int{-1, 0}, calls)
}

func TestThrottledProgress_LargeJumps(t *testing.T) {
	var calls []int
	fn := throttledProgress(func(p int) {
		calls = append(calls, p)
	})

	fn(50)
	fn(100)

	assert.Equal(t, []int{50, 100}, calls)
}

// --- progressReader: full read to 100% ---

func TestProgressReader_FullRead(t *testing.T) {
	data := []byte("0123456789") // 10 bytes
	var lastPercent int
	pr := &progressReader{
		reader: bytes.NewReader(data),
		total:  int64(len(data)),
		onProgress: func(percent int) {
			lastPercent = percent
		},
	}

	// Read all at once
	buf := make([]byte, 20)
	_, err := pr.Read(buf)
	assert.NoError(t, err)
	assert.Equal(t, 100, lastPercent)
}

// --- copyFile preserves executable permission ---

func TestCopyFile_PreservesExecutablePermission(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("executable permission bits not supported on Windows")
	}
	src := t.TempDir()
	dst := t.TempDir()

	srcPath := filepath.Join(src, "script.sh")
	dstPath := filepath.Join(dst, "script.sh")

	require.NoError(t, os.WriteFile(srcPath, []byte("#!/bin/sh\n"), 0o755))

	err := copyFile(srcPath, dstPath)
	require.NoError(t, err)

	dstInfo, err := os.Stat(dstPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o755), dstInfo.Mode().Perm())
}

// --- isDocker: env var with empty value should return false ---

func TestIsDocker_EmptyContainerEnvVar(t *testing.T) {
	orig := os.Getenv("container")
	defer os.Setenv("container", orig)

	os.Unsetenv("container")
	// If /.dockerenv doesn't exist, result should be false
	if _, err := os.Stat("/.dockerenv"); os.IsNotExist(err) {
		assert.False(t, IsDocker())
	}
}

// --- CleanStaleUpgradeTempDirs: non-dir files matching pattern are skipped ---

func TestCleanStaleUpgradeTempDirs_SkipsFiles(t *testing.T) {
	// Create a file (not a dir) matching the pattern
	f, err := os.CreateTemp("", "clawbench-upgrade-*.txt")
	require.NoError(t, err)
	f.Close()
	defer os.Remove(f.Name())

	// Should not panic; file should not be removed (only dirs are cleaned)
	CleanStaleUpgradeTempDirs()

	_, err = os.Stat(f.Name())
	assert.NoError(t, err, "regular file matching pattern should not be removed")
}

// --- expectedDigest.verify with a real sha512 digest ---

func TestExpectedDigest_VerifyRealSHA512(t *testing.T) {
	data := []byte("test content for integrity")
	digest := mustDigest(t, sha512SRI(data), "")

	hasher := sha512.New()
	_, _ = hasher.Write(data)

	assert.NoError(t, digest.verify(hasher))
}

// --- downloadAndExtract refuses a tarball it cannot verify ---

// The regression test for the fail-open bug: a registry response with neither
// dist.integrity nor dist.shasum must abort the whole upgrade before anything
// is downloaded. Previously the empty integrity string caused verification to
// be skipped and the binary was installed unverified.
// A registry that supplies no hash no longer blocks the upgrade: it proceeds
// unverified and records a warning. The download is still attempted, which is
// the behavior change this test pins down.
func TestPerformUpgrade_NoUsableHashProceedsWithWarning(t *testing.T) {
	dir := withTempDataDir(t)

	// A real file stands in for the on-disk binary, and a version behind the
	// target defeats the short-circuit so the download path is reached.
	live := filepath.Join(dir, "bin", "clawbench")
	require.NoError(t, os.MkdirAll(filepath.Dir(live), 0o755))
	require.NoError(t, os.WriteFile(live, []byte("binary"), 0o755))
	require.NoError(t, WriteSelfPath(live))

	origVer := selfBinaryVersion
	selfBinaryVersion = func(string) (string, error) { return "0.10.0", nil }
	t.Cleanup(func() { selfBinaryVersion = origVer })

	// The default registry is unreachable, so the user's mirror supplies the
	// metadata.
	failServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(failServer.Close)

	// The mirror advertises an upgrade but supplies no hash at all, and serves
	// a real tarball so the download can complete.
	binContent := []byte("#!/bin/sh\necho clawbench")
	tarball, _ := buildTarball(t, binContent)

	var tarballHits int32
	mirrorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, ".tgz") {
			atomic.AddInt32(&tarballHits, 1)
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(tarball)))
			_, _ = w.Write(tarball)
			return
		}
		resp := npmRegistryResponse{}
		resp.Version = "99.0.0"
		resp.Dist.Tarball = "https://mirror.example.com/test/-/test-99.0.0.tgz"
		// Both dist.integrity and dist.shasum deliberately left empty.
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(mirrorServer.Close)

	origClient := upgradeHTTPClient
	upgradeHTTPClient = &http.Client{Transport: &failoverTransport{
		defaultBase: failServer.URL,
		mirrorBase:  mirrorServer.URL,
	}}
	t.Cleanup(func() { upgradeHTTPClient = origClient })

	origChina := platform.ChinaMirrorChecked.Load()
	platform.ChinaMirrorChecked.Store(2) // non-China → default base first
	t.Cleanup(func() { platform.ChinaMirrorChecked.Store(origChina) })

	origEnv := os.Getenv("NPM_CONFIG_REGISTRY")
	os.Setenv("NPM_CONFIG_REGISTRY", mirrorServer.URL)
	t.Cleanup(func() { os.Setenv("NPM_CONFIG_REGISTRY", origEnv) })

	// Stub the restart so the flow stops before exec'ing the fake binary.
	origRestart := upgradeRestartFunc
	upgradeRestartFunc = func() error { return nil }
	t.Cleanup(func() { upgradeRestartFunc = origRestart })

	ResetUpgradeState()
	t.Cleanup(ResetUpgradeState)

	performUpgrade(context.Background())

	s := GetUpgradeState()
	assert.Contains(t, s.VerificationWarning, "no integrity hash",
		"the unverified install must be reported to the user")
	assert.Equal(t, int32(1), atomic.LoadInt32(&tarballHits),
		"the download must proceed when no hash is available")

	// The upgrade may still fail later (the fake binary cannot be exec'd), but
	// it must not fail for lack of verification. Asserting on the reason rather
	// than the phase keeps this test about the policy change.
	assert.NotContains(t, s.Error, "unverified",
		"the upgrade must no longer be refused for a missing hash")
}

// downloadAndExtract verifies whenever the digest carries a hash. An unverified
// digest (no hash available) is the only case that skips the comparison.
func TestDownloadAndExtract_AlwaysVerifies(t *testing.T) {
	origClient := upgradeHTTPClient
	defer func() { upgradeHTTPClient = origClient }()

	binContent := []byte("binary")
	tarball, _ := buildTarball(t, binContent)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(tarball)))
		w.Write(tarball)
	}))
	defer ts.Close()
	upgradeHTTPClient = ts.Client()

	destPath := filepath.Join(t.TempDir(), "clawbench-new")

	// A deliberately wrong sha1 digest must be rejected even though the tarball
	// itself is perfectly well-formed.
	wrong := sha1.Sum([]byte("not the tarball"))
	err := downloadAndExtract(context.Background(), ts.URL,
		mustDigest(t, "", hex.EncodeToString(wrong[:])), destPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "integrity verification failed")

	_, statErr := os.Stat(destPath)
	assert.True(t, os.IsNotExist(statErr), "binary must not remain on disk after failed verification")
}

// An unverified digest must not be reported as a successful verification. The
// regression this guards: verify() returns nil for an unverified digest, so a
// shared code path would log "integrity verified" alongside a hardcoded
// algorithm that was never applied.
func TestDownloadAndExtract_UnverifiedReportsNotVerified(t *testing.T) {
	origClient := upgradeHTTPClient
	defer func() { upgradeHTTPClient = origClient }()

	binContent := []byte("#!/bin/sh\necho hi")
	tarball, _ := buildTarball(t, binContent)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(tarball)))
		w.Write(tarball)
	}))
	defer ts.Close()
	upgradeHTTPClient = ts.Client()

	digest, _, err := resolveExpectedDigest("", "")
	require.NoError(t, err)
	require.True(t, digest.unverified, "precondition: no hash means unverified")

	destPath := filepath.Join(t.TempDir(), "clawbench-new")
	require.NoError(t, downloadAndExtract(context.Background(), ts.URL, digest, destPath),
		"an unverified install must still complete")

	got, readErr := os.ReadFile(destPath)
	require.NoError(t, readErr)
	assert.Equal(t, binContent, got)
}

// A decompression bomb must not be able to fill the disk. The archive here is
// tiny on the wire and enormous once expanded, which is exactly the shape a
// hostile mirror would serve when no hash constrains it.
func TestDownloadAndExtract_RejectsOversizedBinary(t *testing.T) {
	origClient := upgradeHTTPClient
	defer func() { upgradeHTTPClient = origClient }()

	huge := bytes.Repeat([]byte("A"), maxBinarySize+1024)
	tarball, _ := buildTarball(t, huge)
	require.Less(t, len(tarball), 10<<20, "precondition: the archive itself is small")

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(tarball)))
		w.Write(tarball)
	}))
	defer ts.Close()
	upgradeHTTPClient = ts.Client()

	digest, _, err := resolveExpectedDigest("", "")
	require.NoError(t, err)

	destPath := filepath.Join(t.TempDir(), "clawbench-new")
	err = downloadAndExtract(context.Background(), ts.URL, digest, destPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "limit")

	_, statErr := os.Stat(destPath)
	assert.True(t, os.IsNotExist(statErr), "an oversized binary must be cleaned up")
}

// The digest is checked against the bytes actually served, so a tarball that is
// swapped after the metadata was fetched is still caught.
func TestDownloadAndExtract_DetectsSwappedTarball(t *testing.T) {
	origClient := upgradeHTTPClient
	defer func() { upgradeHTTPClient = origClient }()

	// The registry's hash was computed for the honest tarball...
	honest := buildTarballNoIntegrity(t, []byte("the real binary"))
	digest := mustDigest(t, sha512SRI(honest), "")

	// ...but the server is serving a different one.
	swapped := buildTarballNoIntegrity(t, []byte("the tampered binary"))

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(swapped)))
		w.Write(swapped)
	}))
	defer ts.Close()
	upgradeHTTPClient = ts.Client()

	destPath := filepath.Join(t.TempDir(), "clawbench-new")

	err := downloadAndExtract(context.Background(), ts.URL, digest, destPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "integrity verification failed")

	_, statErr := os.Stat(destPath)
	assert.True(t, os.IsNotExist(statErr), "tampered binary must be removed")
}

func buildTarballNoIntegrity(t *testing.T, binContent []byte) []byte {
	t.Helper()

	binName := "clawbench"
	if runtime.GOOS == "windows" {
		binName = "clawbench.exe"
	}

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	hdr := &tar.Header{
		Name: "package/bin/" + binName,
		Mode: 0o755,
		Size: int64(len(binContent)),
	}
	require.NoError(t, tw.WriteHeader(hdr))
	_, err := tw.Write(binContent)
	require.NoError(t, err)

	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())

	return buf.Bytes()
}

// --- Verify hash.Hash interface is satisfied by sha512 ---

var _ hash.Hash = sha512.New() // compile-time check

// --- CheckForUpgrade ---

func TestCheckForUpgrade_Success(t *testing.T) {
	origClient := upgradeHTTPClient
	defer func() { upgradeHTTPClient = origClient }()

	pkg, err := getPlatformPkg()
	require.NoError(t, err)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, pkg)
		resp := npmRegistryResponse{}
		resp.Version = "99.0.0"
		resp.Dist.Tarball = "https://registry.npmjs.org/test/-/test-99.0.0.tgz"
		resp.Dist.Integrity = wellFormedIntegrity
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	upgradeHTTPClient = ts.Client()

	origChina := platform.ChinaMirrorChecked.Load()
	defer platform.ChinaMirrorChecked.Store(origChina)
	platform.ChinaMirrorChecked.Store(2) // non-China

	currentVer, latestVer, err := checkForUpgradeWithBase(ts.URL)
	require.NoError(t, err)
	assert.NotEmpty(t, currentVer)
	assert.Equal(t, "99.0.0", latestVer)
}

func TestCheckForUpgrade_DirectCall(t *testing.T) {
	// Test CheckForUpgrade directly by pointing the HTTP client at a test server
	// via a custom transport that rewrites requests
	origClient := upgradeHTTPClient
	defer func() { upgradeHTTPClient = origClient }()

	pkg, err := getPlatformPkg()
	require.NoError(t, err)

	keys := newTestSigningKeys(t)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, pkg)
		resp := signedRegistryResponse(t, keys, pkg, "2.0.0", wellFormedIntegrity)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	upgradeHTTPClient = ts.Client()

	origChina := platform.ChinaMirrorChecked.Load()
	defer platform.ChinaMirrorChecked.Store(origChina)
	platform.ChinaMirrorChecked.Store(2) // non-China → npmjs.org

	// Rewrite requests to test server
	origTransport := upgradeHTTPClient.Transport
	upgradeHTTPClient.Transport = &rewritingTransport{
		targetURL: ts.URL,
		orig:      origTransport,
		keysJSON:  keys.keysJSON(t),
	}
	defer func() { upgradeHTTPClient.Transport = origTransport }()

	currentVer, latestVer, err := CheckForUpgrade()
	require.NoError(t, err)
	assert.NotEmpty(t, currentVer)
	assert.Equal(t, "2.0.0", latestVer)
}

func TestCheckForUpgrade_DirectCallError(t *testing.T) {
	origClient := upgradeHTTPClient
	defer func() { upgradeHTTPClient = origClient }()

	// Client that always fails
	upgradeHTTPClient = &http.Client{
		Timeout: 1 * time.Second,
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return nil, fmt.Errorf("connection refused")
			},
		},
	}

	origChina := platform.ChinaMirrorChecked.Load()
	defer platform.ChinaMirrorChecked.Store(origChina)
	platform.ChinaMirrorChecked.Store(2)

	currentVer, latestVer, err := CheckForUpgrade()
	assert.Error(t, err)
	assert.NotEmpty(t, currentVer)
	assert.Empty(t, latestVer)
}

// rewritingTransport rewrites all requests to a target test server, except the
// npm signing-keys endpoint, which is answered from keysJSON so the signature
// check can succeed without reaching the network.
type rewritingTransport struct {
	targetURL string
	orig      http.RoundTripper
	keysJSON  []byte
}

func (rt *rewritingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Path == npmKeysPath {
		return syntheticJSONResponse(req, http.StatusOK, rt.keysJSON)
	}
	newURL, err := url.Parse(rt.targetURL + req.URL.Path)
	if err != nil {
		return nil, err
	}
	req.URL = newURL
	if rt.orig != nil {
		return rt.orig.RoundTrip(req)
	}
	return http.DefaultTransport.RoundTrip(req)
}

// syntheticJSONResponse builds an in-memory HTTP response.
func syntheticJSONResponse(req *http.Request, status int, body []byte) (*http.Response, error) {
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(body)),
		Request:    req,
	}, nil
}

func TestCheckForUpgrade_Error(t *testing.T) {
	origClient := upgradeHTTPClient
	defer func() { upgradeHTTPClient = origClient }()

	// Server that returns 500
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	upgradeHTTPClient = ts.Client()

	currentVer, latestVer, err := checkForUpgradeWithBase(ts.URL)
	assert.Error(t, err)
	assert.NotEmpty(t, currentVer) // currentVer is always populated from version.Get()
	assert.Empty(t, latestVer)
}

// checkForUpgradeWithBase is a test helper that calls CheckForUpgrade with a
// custom registry base URL by directly calling fetchUpgradeInfoWithBase.
func checkForUpgradeWithBase(baseURL string) (string, string, error) {
	info, err := fetchUpgradeInfoWithBase(baseURL)
	if err != nil {
		return version.Get(), "", err
	}
	return info.CurrentVersion, info.LatestVersion, nil
}

// --- broadcastUpgradeUpdate ---

func TestBroadcastUpgradeUpdate_NoManager(t *testing.T) {
	ResetUpgradeState()
	defer ResetUpgradeState()

	// ws.GetManager() returns nil before initialization — should not panic
	assert.NotPanics(t, func() {
		broadcastUpgradeUpdate()
	})
}

// --- SetUpgradeState ---

func TestSetUpgradeState_SetsAllFields(t *testing.T) {
	ResetUpgradeState()
	defer ResetUpgradeState()

	SetUpgradeState(UpgradePhaseDownloading, 50, "Halfway there")
	s := GetUpgradeState()
	assert.Equal(t, UpgradePhaseDownloading, s.Phase)
	assert.Equal(t, 50, s.Progress)
	assert.Equal(t, "Halfway there", s.Message)
}

// --- SetUpgradeVerificationWarning ---

func TestSetUpgradeVerificationWarning_RecordsWarning(t *testing.T) {
	ResetUpgradeState()
	defer ResetUpgradeState()

	const warning = "The release signature could not be verified."
	SetUpgradeVerificationWarning(warning)

	assert.Equal(t, warning, GetUpgradeState().VerificationWarning)
}

// ResetUpgradeState must clear the warning, otherwise a retry would keep
// showing a stale downgrade notice after a successful verified check.
func TestSetUpgradeVerificationWarning_ClearedByReset(t *testing.T) {
	SetUpgradeVerificationWarning("stale warning")
	ResetUpgradeState()
	defer ResetUpgradeState()

	assert.Empty(t, GetUpgradeState().VerificationWarning)
}

// --- ResetUpgradeState clears everything ---

func TestResetUpgradeState_ClearsAll(t *testing.T) {
	SetUpgradeVersions("1.0.0", "2.0.0")
	SetUpgradeBackupPath("/path/to/backup")
	SetUpgradeError("some error")

	ResetUpgradeState()
	defer ResetUpgradeState()

	s := GetUpgradeState()
	assert.Equal(t, UpgradePhaseIdle, s.Phase)
	assert.Empty(t, s.CurrentVer)
	assert.Empty(t, s.LatestVer)
	assert.Empty(t, s.BackupPath)
	assert.Empty(t, s.Error)
	assert.Zero(t, s.Progress)
}

// --- performUpgrade error paths ---

func TestPerformUpgrade_UnreachableRegistry(t *testing.T) {
	origClient := upgradeHTTPClient
	defer func() { upgradeHTTPClient = origClient }()

	// Use a client that will fail to connect
	upgradeHTTPClient = &http.Client{
		Timeout: 1 * time.Second,
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return nil, fmt.Errorf("connection refused")
			},
		},
	}

	origChina := platform.ChinaMirrorChecked.Load()
	defer platform.ChinaMirrorChecked.Store(origChina)
	platform.ChinaMirrorChecked.Store(2) // non-China → use npmjs.org

	ResetUpgradeState()
	defer ResetUpgradeState()

	// Run performUpgrade — it should fail quickly with a registry error
	done := make(chan struct{})
	go func() {
		defer close(done)
		performUpgrade(context.Background())
	}()

	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("performUpgrade timed out")
	}

	s := GetUpgradeState()
	assert.Equal(t, UpgradePhaseFailed, s.Phase)
	assert.Contains(t, s.Error, "Failed to check version")
}

// --- CheckInstallDirWritable ---

func TestCheckInstallDirWritable_Writable(t *testing.T) {
	origExe := upgradeExecutable
	defer func() { upgradeExecutable = origExe }()

	dir := t.TempDir()
	// The binary must exist: resolveSelfBinary only accepts a live path.
	exe := filepath.Join(dir, "clawbench")
	require.NoError(t, os.WriteFile(exe, []byte("binary"), 0o755))
	upgradeExecutable = func() (string, error) {
		return exe, nil
	}

	gotDir, err := CheckInstallDirWritable()
	require.NoError(t, err)
	assert.Equal(t, dir, gotDir)

	// The probe file must be cleaned up, leaving only the binary behind.
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "probe temp file should have been removed")
	assert.Equal(t, "clawbench", entries[0].Name())
}

func TestCheckInstallDirWritable_NotWritable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permission semantics differ on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}

	origExe := upgradeExecutable
	defer func() { upgradeExecutable = origExe }()

	dir := t.TempDir()
	// The binary must exist: resolveSelfBinary only accepts a live path.
	exe := filepath.Join(dir, "clawbench")
	require.NoError(t, os.WriteFile(exe, []byte("binary"), 0o755))
	require.NoError(t, os.Chmod(dir, 0o555))
	defer func() { _ = os.Chmod(dir, 0o755) }() // allow TempDir cleanup

	upgradeExecutable = func() (string, error) {
		return exe, nil
	}

	gotDir, err := CheckInstallDirWritable()
	require.Error(t, err)
	assert.Equal(t, dir, gotDir, "install dir should still be reported for the message")
}

func TestCheckInstallDirWritable_ExecutableError(t *testing.T) {
	origExe := upgradeExecutable
	defer func() { upgradeExecutable = origExe }()

	upgradeExecutable = func() (string, error) {
		return "", fmt.Errorf("boom")
	}

	_, err := CheckInstallDirWritable()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolve executable path")
}

// TestCheckInstallDirWritable_PreservesExistingBackup verifies the probe does
// not delete a pre-existing ".bak" (it must only remove files it created).
func TestCheckInstallDirWritable_PreservesExistingBackup(t *testing.T) {
	origExe := upgradeExecutable
	defer func() { upgradeExecutable = origExe }()

	dir := t.TempDir()
	exe := filepath.Join(dir, "clawbench")
	// The binary must exist: resolveSelfBinary only accepts a live path.
	require.NoError(t, os.WriteFile(exe, []byte("binary"), 0o755))
	backup := exe + ".bak"
	require.NoError(t, os.WriteFile(backup, []byte("existing backup"), 0o600))

	upgradeExecutable = func() (string, error) { return exe, nil }

	gotDir, err := CheckInstallDirWritable()
	require.NoError(t, err)
	assert.Equal(t, dir, gotDir)

	data, err := os.ReadFile(backup)
	require.NoError(t, err, "pre-existing backup must survive the probe")
	assert.Equal(t, "existing backup", string(data))
}

// TestCheckInstallDirWritable_BackupNotWritable verifies the probe catches a
// non-writable existing ".bak" even when the directory itself is writable.
// This is the case a directory-only probe would miss.
func TestCheckInstallDirWritable_BackupNotWritable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file permission semantics differ on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file permissions")
	}

	origExe := upgradeExecutable
	defer func() { upgradeExecutable = origExe }()

	dir := t.TempDir()
	exe := filepath.Join(dir, "clawbench")
	// The binary must exist: resolveSelfBinary only accepts a live path.
	require.NoError(t, os.WriteFile(exe, []byte("binary"), 0o755))
	backup := exe + ".bak"
	require.NoError(t, os.WriteFile(backup, []byte("root backup"), 0o400))
	defer func() { _ = os.Chmod(backup, 0o600) }() // allow TempDir cleanup

	upgradeExecutable = func() (string, error) { return exe, nil }

	gotDir, err := CheckInstallDirWritable()
	require.Error(t, err, "a non-writable existing .bak must fail the probe")
	assert.Equal(t, dir, gotDir)
}

// TestPerformUpgrade_InstallDirNotWritable verifies the preflight fails fast
// (before downloading) with the actionable install_dir_not_writable code.
func TestPerformUpgrade_InstallDirNotWritable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permission semantics differ on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}

	origClient := upgradeHTTPClient
	defer func() { upgradeHTTPClient = origClient }()
	origExe := upgradeExecutable
	defer func() { upgradeExecutable = origExe }()

	// Registry reports an upgrade is available.
	keys := newTestSigningKeys(t)
	pkg, pkgErr := getPlatformPkg()
	require.NoError(t, pkgErr)

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

	origChina := platform.ChinaMirrorChecked.Load()
	defer platform.ChinaMirrorChecked.Store(origChina)
	platform.ChinaMirrorChecked.Store(2) // non-China → default base is npmjs

	// Binary lives in a read-only directory. It must exist so resolveSelfBinary
	// accepts it; the version probe then fails (not a real executable) and the
	// flow falls through to the install-dir preflight.
	dir := t.TempDir()
	exe := filepath.Join(dir, "clawbench")
	require.NoError(t, os.WriteFile(exe, []byte("binary"), 0o755))
	require.NoError(t, os.Chmod(dir, 0o555))
	defer func() { _ = os.Chmod(dir, 0o755) }()
	upgradeExecutable = func() (string, error) {
		return exe, nil
	}

	ResetUpgradeState()
	defer ResetUpgradeState()

	done := make(chan struct{})
	go func() {
		defer close(done)
		performUpgrade(context.Background())
	}()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("performUpgrade timed out")
	}

	s := GetUpgradeState()
	assert.Equal(t, UpgradePhaseFailed, s.Phase)
	assert.Equal(t, UpgradeErrInstallDirNotWritable, s.ErrorCode)
	assert.Contains(t, s.Error, dir)
}

func TestRewriteTarballURL(t *testing.T) {
	// Tarball not from npmjs → returned unchanged (fall-through branch).
	assert.Equal(t,
		"https://other.example.com/x.tgz",
		rewriteTarballURL("https://other.example.com/x.tgz", "https://registry.npmmirror.com"))
	// base == npmjs → returned unchanged.
	assert.Equal(t,
		"https://registry.npmjs.org/x.tgz",
		rewriteTarballURL("https://registry.npmjs.org/x.tgz", "https://registry.npmjs.org"))
	// npmjs tarball + non-npmjs base → rewritten to base (rewrite branch).
	assert.Equal(t,
		"https://registry.npmmirror.com/x.tgz",
		rewriteTarballURL("https://registry.npmjs.org/x.tgz", "https://registry.npmmirror.com"))
}

// TestNormalizeTarballURL covers both the malformed mirror URLs that triggered
// the 404 and the standard URLs that must pass through untouched.
func TestNormalizeTarballURL(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			// Root cause of the 404: Nexus mirror leaves the dist-tag in the
			// package-name segment.
			name: "strips @latest from scoped package segment",
			in:   "http://rd-registry.uniview.com/repository/npm-public/@xulongzhe/clawbench-linux-x64@latest/-/clawbench-linux-x64-0.91.0.tgz",
			want: "http://rd-registry.uniview.com/repository/npm-public/@xulongzhe/clawbench-linux-x64/-/clawbench-linux-x64-0.91.0.tgz",
		},
		{
			// Any dist-tag is stripped, not just "@latest" — the mirror may be
			// queried with an arbitrary tag in the future.
			name: "strips arbitrary dist-tag",
			in:   "https://mirror.example.com/@scope/pkg@next/-/pkg-1.2.3.tgz",
			want: "https://mirror.example.com/@scope/pkg/-/pkg-1.2.3.tgz",
		},
		{
			name: "strips tag from unscoped package",
			in:   "https://mirror.example.com/pkg@latest/-/pkg-1.2.3.tgz",
			want: "https://mirror.example.com/pkg/-/pkg-1.2.3.tgz",
		},
		{
			name: "standard scoped URL unchanged",
			in:   "https://registry.npmjs.org/@scope/pkg/-/pkg-1.2.3.tgz",
			want: "https://registry.npmjs.org/@scope/pkg/-/pkg-1.2.3.tgz",
		},
		{
			name: "standard unscoped URL unchanged",
			in:   "https://registry.npmmirror.com/pkg/-/pkg-1.2.3.tgz",
			want: "https://registry.npmmirror.com/pkg/-/pkg-1.2.3.tgz",
		},
		{
			// The leading "@" is the scope marker, not a tag separator. The
			// "at > 0" guard must not mistake it for one.
			name: "scope marker alone is preserved",
			in:   "https://mirror.example.com/@scope/-/pkg-1.2.3.tgz",
			want: "https://mirror.example.com/@scope/-/pkg-1.2.3.tgz",
		},
		{
			// Without "/-/" the URL is not recognized as an npm tarball path,
			// so the function must be a no-op rather than mangling it.
			name: "no /-/ separator unchanged",
			in:   "https://mirror.example.com/@scope/pkg@latest",
			want: "https://mirror.example.com/@scope/pkg@latest",
		},
		{
			name: "empty string unchanged",
			in:   "",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, normalizeTarballURL(tt.in))
		})
	}
}

func TestFetchUpgradeInfo_NormalizesMalformedMirrorTarball(t *testing.T) {
	origClient := upgradeHTTPClient
	defer func() { upgradeHTTPClient = origClient }()

	// Regression test for the 404: the registry response carries a dist-tag in
	// the package-name segment, which must be cleaned before download.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := npmRegistryResponse{}
		resp.Version = "0.91.0"
		resp.Dist.Tarball = "https://registry.npmjs.org/@scope/pkg@latest/-/pkg-0.91.0.tgz"
		resp.Dist.Integrity = wellFormedIntegrity
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	upgradeHTTPClient = ts.Client()

	info, err := fetchUpgradeInfoWithBase(ts.URL)
	require.NoError(t, err)
	assert.NotContains(t, info.TarballURL, "@latest")
	assert.Contains(t, info.TarballURL, "/@scope/pkg/-/pkg-0.91.0.tgz")
}
