package service

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func useRealChecksumFetch(t *testing.T) {
	t.Helper()
	prev := desktopChecksumsFetch
	desktopChecksumsFetch = fetchDesktopChecksums
	t.Cleanup(func() { desktopChecksumsFetch = prev })
}

// isolateDesktopChecksumsCache gives the test its own empty cache — both the
// successful-result map and the negative one — and restores the process maps on
// cleanup. Without the negative map, a tag one test failed to fetch would be
// served from the retry window in the next test that uses the same tag.
func isolateDesktopChecksumsCache(t *testing.T) {
	t.Helper()
	desktopChecksumsMu.Lock()
	origCache, origRetry := desktopChecksumsByTag, desktopChecksumsRetry
	desktopChecksumsByTag = map[string]map[string]string{}
	desktopChecksumsRetry = map[string]time.Time{}
	desktopChecksumsMu.Unlock()
	t.Cleanup(func() {
		desktopChecksumsMu.Lock()
		desktopChecksumsByTag, desktopChecksumsRetry = origCache, origRetry
		desktopChecksumsMu.Unlock()
	})
}

func TestParseSHA256SUMS(t *testing.T) {
	const hash = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	body := strings.Join([]string{
		hash + "  full.zip",
		"not-a-hash  ignored.zip",
		strings.ToUpper(hash) + "  uppercase.zip",
		hash + "  extra-field.zip trailing",
		"",
		hash + "  fused.zip",
	}, "\n")

	got, err := parseSHA256SUMS(strings.NewReader(body))
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"full.zip":  hash,
		"fused.zip": hash,
	}, got)
}

func TestDesktopChecksums_FetchesOnceAndFilters(t *testing.T) {
	useRealChecksumFetch(t)
	const tag = "v1.2.3"
	const full = "clawbench-desktop-linux-x64-v1.2.3.zip"
	const fused = "clawbench-desktop-windows-x64-local-v1.2.3.zip"
	const hash = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.URL.Path != "/"+tag {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(hash + "  " + full + "\n" + hash + "  " + fused + "\n"))
	}))
	defer srv.Close()

	isolateDesktopChecksumsCache(t)
	origURL, origClient := desktopChecksumsURL, desktopChecksumsHTTPClient
	t.Cleanup(func() {
		desktopChecksumsURL, desktopChecksumsHTTPClient = origURL, origClient
	})

	desktopChecksumsURL = func(gotTag string) string { return srv.URL + "/" + gotTag }
	desktopChecksumsHTTPClient = srv.Client()

	fullMap := desktopPackageChecksums(tag)
	assert.Equal(t, map[string]string{full: hash}, fullMap)

	fusedMap := FusedDesktopChecksums(tag)
	assert.Equal(t, map[string]string{fused: hash}, fusedMap)

	// Both calls share the per-tag cache.
	assert.Equal(t, 1, hits)
}

func TestDesktopChecksums_Non200IsCachedAsMissing(t *testing.T) {
	useRealChecksumFetch(t)
	const tag = "v9.9.9"
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	isolateDesktopChecksumsCache(t)
	origURL, origClient := desktopChecksumsURL, desktopChecksumsHTTPClient
	t.Cleanup(func() {
		desktopChecksumsURL, desktopChecksumsHTTPClient = origURL, origClient
	})

	desktopChecksumsURL = func(gotTag string) string { return srv.URL + "/" + gotTag }
	desktopChecksumsHTTPClient = srv.Client()

	assert.Nil(t, desktopChecksums(tag))
	assert.Nil(t, desktopChecksums(tag))
	assert.Equal(t, 1, hits, "a failed fetch must also be cached for the tag")
}

func TestDesktopChecksums_EmptyTagDoesNotFetch(t *testing.T) {
	assert.Nil(t, desktopChecksums(""))
}

// isolateDesktopChecksums points the manifest fetch at a test server and gives
// the test its own empty cache, restoring both on cleanup.
func isolateDesktopChecksums(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	useRealChecksumFetch(t)
	srv := httptest.NewServer(handler)
	isolateDesktopChecksumsCache(t)
	origURL, origClient := desktopChecksumsURL, desktopChecksumsHTTPClient
	t.Cleanup(func() {
		srv.Close()
		desktopChecksumsURL, desktopChecksumsHTTPClient = origURL, origClient
	})
	desktopChecksumsURL = func(tag string) string { return srv.URL + "/" + tag }
	desktopChecksumsHTTPClient = srv.Client()
	return srv
}

func TestDesktopChecksums_ManifestWithoutUsableLinesIsMissing(t *testing.T) {
	isolateDesktopChecksums(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("garbage\nnot-a-hash  file.zip\n"))
	})
	assert.Nil(t, desktopChecksums("v2.0.0"))
}

func TestDesktopChecksums_UnreachableIsMissing(t *testing.T) {
	// The URL is well formed but nothing is listening: the network-error
	// branch rather than a bad status.
	srv := isolateDesktopChecksums(t, http.NotFound)
	srv.Close()
	assert.Nil(t, desktopChecksums("v2.0.1"))
}

func TestDesktopChecksums_InvalidURLIsMissing(t *testing.T) {
	isolateDesktopChecksums(t, http.NotFound)
	desktopChecksumsURL = func(string) string { return "://not-a-url" }
	assert.Nil(t, desktopChecksums("v2.0.2"))
}

func TestDesktopPackageChecksums_NoMatchingAssetsIsNil(t *testing.T) {
	const hash = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	isolateDesktopChecksums(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(hash + "  unrelated.zip\n"))
	})
	assert.Nil(t, desktopPackageChecksums("v2.0.3"))
	assert.Nil(t, FusedDesktopChecksums("v2.0.3"))
}

// A failure is not an answer about the release: GitHub was unreachable, or the
// 3s deadline expired. Remembering it forever would silently disable the
// checksum on every later download for the rest of the process's life.
func TestDesktopChecksums_FailedFetchIsRetriedAfterTheWindow(t *testing.T) {
	const tag = "v3.0.0"
	const hash = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	hits := 0
	isolateDesktopChecksums(t, func(w http.ResponseWriter, _ *http.Request) {
		hits++
		if hits == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(hash + "  full.zip\n"))
	})
	// Collapse the window: it exists so an offline server does not pay the 3s
	// timeout on every request, not to make the failure permanent.
	prevInterval := desktopChecksumsRetryInterval
	desktopChecksumsRetryInterval = 0
	t.Cleanup(func() { desktopChecksumsRetryInterval = prevInterval })

	assert.Nil(t, desktopChecksums(tag), "the first fetch failed")
	assert.Equal(t, map[string]string{"full.zip": hash}, desktopChecksums(tag),
		"the retry must fetch again instead of returning the cached failure")
	assert.Equal(t, 2, hits)
}

// The other half of the contract: inside the window the failure IS remembered.
// Without this the retry map buys nothing — an offline server would pay the 3s
// timeout on every single download request, which is the cost the caching was
// there to avoid in the first place.
func TestDesktopChecksums_FailedFetchIsNotRetriedInsideTheWindow(t *testing.T) {
	const tag = "v3.0.1"
	hits := 0
	isolateDesktopChecksums(t, func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusBadGateway)
	})
	// Long enough that the test cannot outlive it; only "not yet" is asserted.
	prevInterval := desktopChecksumsRetryInterval
	desktopChecksumsRetryInterval = time.Hour
	t.Cleanup(func() { desktopChecksumsRetryInterval = prevInterval })

	for range 3 {
		assert.Nil(t, desktopChecksums(tag))
	}
	assert.Equal(t, 1, hits, "the failure must be remembered for the window, not refetched per call")
}

// A success must clear the failed attempt, or a tag that failed once and then
// succeeded would keep being re-fetched until the window lapsed even though the
// answer is already cached.
func TestDesktopChecksums_SuccessClearsTheRetryMarker(t *testing.T) {
	const tag = "v3.0.2"
	const hash = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	hits := 0
	isolateDesktopChecksums(t, func(w http.ResponseWriter, _ *http.Request) {
		hits++
		if hits == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(hash + "  full.zip\n"))
	})
	prevInterval := desktopChecksumsRetryInterval
	desktopChecksumsRetryInterval = 0
	t.Cleanup(func() { desktopChecksumsRetryInterval = prevInterval })

	assert.Nil(t, desktopChecksums(tag))
	require.Equal(t, map[string]string{"full.zip": hash}, desktopChecksums(tag))

	// Restore a real window: the success is cached forever, so the now-cleared
	// retry marker must not let another fetch through.
	desktopChecksumsRetryInterval = time.Hour
	for range 3 {
		assert.Equal(t, map[string]string{"full.zip": hash}, desktopChecksums(tag))
	}
	assert.Equal(t, 2, hits, "a cached success must be served without another fetch")
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }

func TestParseSHA256SUMS_ReaderError(t *testing.T) {
	_, err := parseSHA256SUMS(failingReader{})
	require.Error(t, err)
}
