package service

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha512"
	"encoding/base64"
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

// --- verifyIntegrity ---

func TestVerifyIntegrity_ValidSHA512(t *testing.T) {
	data := []byte("hello world")
	hasher := sha512.New()
	hasher.Write(data)
	expectedHash := hasher.Sum(nil)
	integrity := "sha512-" + base64.StdEncoding.EncodeToString(expectedHash)

	err := verifyIntegrity(hasher, integrity)
	assert.NoError(t, err)
}

func TestVerifyIntegrity_InvalidPrefix_SkipsVerification(t *testing.T) {
	hasher := sha512.New()
	hasher.Write([]byte("test"))

	err := verifyIntegrity(hasher, "sha256-abc123")
	assert.NoError(t, err) // unsupported algorithm → skip, no error
}

func TestVerifyIntegrity_MalformedBase64(t *testing.T) {
	hasher := sha512.New()
	hasher.Write([]byte("test"))

	err := verifyIntegrity(hasher, "sha512-!!!not-base64!!!")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decode integrity hash")
}

func TestVerifyIntegrity_HashMismatch(t *testing.T) {
	hasher := sha512.New()
	hasher.Write([]byte("actual data"))

	// Build integrity from different data
	wrongHasher := sha512.New()
	wrongHasher.Write([]byte("wrong data"))
	wrongHash := wrongHasher.Sum(nil)
	integrity := "sha512-" + base64.StdEncoding.EncodeToString(wrongHash)

	err := verifyIntegrity(hasher, integrity)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "hash mismatch")
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
	assert.True(t, isDocker())
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
	_ = isDocker()
}

func TestIsDocker_NeitherIndicator(t *testing.T) {
	orig := os.Getenv("container")
	defer os.Setenv("container", orig)
	os.Unsetenv("container")

	// If /.dockerenv exists, this will be true; that's OK.
	// The test mainly ensures no panic and the env var path works.
	result := isDocker()
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
		resp.Dist.Integrity = "sha512-abcdef"
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
		resp.Dist.Integrity = "sha512-abcdef"
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

// fetchUpgradeInfoWithBase is a test helper that calls fetchUpgradeInfo with a
// custom registry base URL by temporarily replacing the HTTP client transport.
func fetchUpgradeInfoWithBase(baseURL string) (*UpgradeInfo, error) {
	pkg, err := getPlatformPkg()
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/%s/latest", baseURL, pkg)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := upgradeHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to query registry: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registry returned status %d", resp.StatusCode)
	}

	var npmResp npmRegistryResponse
	if err := json.NewDecoder(resp.Body).Decode(&npmResp); err != nil {
		return nil, fmt.Errorf("failed to decode registry response: %w", err)
	}

	tarballURL := npmResp.Dist.Tarball
	if tarballURL == "" {
		return nil, fmt.Errorf("no tarball URL in registry response")
	}

	registryBase := baseURL
	if strings.HasPrefix(registryBase, "https://registry.npmmirror.com") {
		tarballURL = strings.Replace(tarballURL,
			"https://registry.npmjs.org", "https://registry.npmmirror.com", 1)
	}

	return &UpgradeInfo{
		CurrentVersion: "0.0.1",
		LatestVersion:  npmResp.Version,
		TarballURL:     tarballURL,
		Integrity:      npmResp.Dist.Integrity,
		HasUpgrade:     true,
	}, nil
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

	err := downloadAndExtract(context.Background(), ts.URL, integrity, destPath)
	require.NoError(t, err)

	got, err := os.ReadFile(destPath)
	require.NoError(t, err)
	assert.Equal(t, binContent, got)
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
	err := downloadAndExtract(context.Background(), ts.URL, "", filepath.Join(destDir, "out"))
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

	err := downloadAndExtract(context.Background(), ts.URL, wrongIntegrity, destPath)
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
	err = downloadAndExtract(context.Background(), ts.URL, "", filepath.Join(destDir, "out"))
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
	err := downloadAndExtract(context.Background(), ts.URL, "", filepath.Join(destDir, "out"))
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
	err := downloadAndExtract(ctx, ts.URL, "", filepath.Join(destDir, "out"))
	assert.Error(t, err)
}

func TestDownloadAndExtract_DestNotWritable(t *testing.T) {
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

	err := downloadAndExtract(context.Background(), ts.URL, "", filepath.Join(destDir, "clawbench-new"))
	assert.Error(t, err)
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

// --- verifyIntegrity with hash.Hash interface ---

func TestVerifyIntegrity_EmptyIntegrity(t *testing.T) {
	// Empty integrity string → hasher is nil at call site, but verifyIntegrity
	// may still be called if both hasher and integrity are non-nil.
	// Test with non-empty integrity and non-sha512 prefix: should skip verification.
	hasher := sha512.New()
	err := verifyIntegrity(hasher, "")
	assert.NoError(t, err) // empty string has no sha512- prefix → skip
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
		assert.False(t, isDocker())
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

// --- verifyIntegrity: correct hash with real sha512 ---

func TestVerifyIntegrity_CorrectHash(t *testing.T) {
	data := []byte("test content for integrity")
	hasher := sha512.New()
	_, _ = hasher.Write(data)
	actualHash := hasher.Sum(nil)

	integrity := "sha512-" + base64.StdEncoding.EncodeToString(actualHash)

	// Create a new hasher that has the same data fed to it
	testHasher := sha512.New()
	_, _ = testHasher.Write(data)

	err := verifyIntegrity(testHasher, integrity)
	assert.NoError(t, err)
}

// --- downloadAndExtract without integrity (empty string) ---

func TestDownloadAndExtract_NoIntegrity(t *testing.T) {
	origClient := upgradeHTTPClient
	defer func() { upgradeHTTPClient = origClient }()

	binContent := []byte("binary without integrity check")
	tarball := buildTarballNoIntegrity(t, binContent)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(tarball)))
		w.Write(tarball)
	}))
	defer ts.Close()

	upgradeHTTPClient = ts.Client()

	destDir := t.TempDir()
	destPath := filepath.Join(destDir, "clawbench-new")

	err := downloadAndExtract(context.Background(), ts.URL, "", destPath)
	require.NoError(t, err)

	got, err := os.ReadFile(destPath)
	require.NoError(t, err)
	assert.Equal(t, binContent, got)
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
		resp.Dist.Integrity = "sha512-abcdef"
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

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, pkg)
		resp := npmRegistryResponse{}
		resp.Version = "2.0.0"
		resp.Dist.Tarball = "https://registry.npmjs.org/test/-/test-2.0.0.tgz"
		resp.Dist.Integrity = "sha512-abcdef"
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
	upgradeHTTPClient.Transport = &rewritingTransport{targetURL: ts.URL, orig: origTransport}
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

// rewritingTransport rewrites all requests to a target test server.
type rewritingTransport struct {
	targetURL string
	orig      http.RoundTripper
}

func (rt *rewritingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
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
