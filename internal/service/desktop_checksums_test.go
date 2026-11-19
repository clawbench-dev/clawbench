package service

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func useRealChecksumFetch(t *testing.T) {
	t.Helper()
	prev := desktopChecksumsFetch
	desktopChecksumsFetch = fetchDesktopChecksums
	t.Cleanup(func() { desktopChecksumsFetch = prev })
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

	origURL, origClient := desktopChecksumsURL, desktopChecksumsHTTPClient
	desktopChecksumsMu.Lock()
	origCache := desktopChecksumsByTag
	desktopChecksumsByTag = map[string]map[string]string{}
	desktopChecksumsMu.Unlock()
	t.Cleanup(func() {
		desktopChecksumsURL, desktopChecksumsHTTPClient = origURL, origClient
		desktopChecksumsMu.Lock()
		desktopChecksumsByTag = origCache
		desktopChecksumsMu.Unlock()
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

	origURL, origClient := desktopChecksumsURL, desktopChecksumsHTTPClient
	desktopChecksumsMu.Lock()
	origCache := desktopChecksumsByTag
	desktopChecksumsByTag = map[string]map[string]string{}
	desktopChecksumsMu.Unlock()
	t.Cleanup(func() {
		desktopChecksumsURL, desktopChecksumsHTTPClient = origURL, origClient
		desktopChecksumsMu.Lock()
		desktopChecksumsByTag = origCache
		desktopChecksumsMu.Unlock()
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
