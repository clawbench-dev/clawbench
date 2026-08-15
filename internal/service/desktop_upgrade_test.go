package service

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"clawbench/internal/platform"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetchDesktopLatestFrom(t *testing.T) {
	orig := upgradeHTTPClient
	defer func() { upgradeHTTPClient = orig }()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pkg := r.URL.Path
		version := ""
		tarball := ""
		switch pkg {
		case "/@xulongzhe/clawbench-desktop-win32-x64/latest":
			version = "0.1.0"
			tarball = "https://registry.npmjs.org/@xulongzhe/clawbench-desktop-win32-x64/-/win-0.1.0.tgz"
		case "/@xulongzhe/clawbench-desktop-darwin-arm64/latest":
			version = "0.1.0"
			tarball = "https://registry.npmjs.org/@xulongzhe/clawbench-desktop-darwin-arm64/-/mac-0.1.0.tgz"
		case "/@xulongzhe/clawbench-desktop-linux-x64/latest":
			version = "0.2.0"
			tarball = "https://registry.npmjs.org/@xulongzhe/clawbench-desktop-linux-x64/-/linux-0.2.0.tgz"
		default:
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"version":"` + version + `","dist":{"tarball":"` + tarball + `","integrity":""}}`))
	}))
	defer ts.Close()

	upgradeHTTPClient = ts.Client()
	res, err := fetchDesktopLatestFrom(ts.URL)
	require.NoError(t, err)
	require.NotNil(t, res)
	// Version is the max across platforms.
	assert.Equal(t, "0.2.0", res.Version)
	// All three requested platforms present; tarballs rewritten from npmjs base to ts.URL.
	require.Contains(t, res.Downloads, "win32-x64")
	require.Contains(t, res.Downloads, "darwin-arm64")
	require.Contains(t, res.Downloads, "linux-x64")
	assert.Contains(t, res.Downloads["win32-x64"], ts.URL)
	assert.Contains(t, res.Downloads["darwin-arm64"], ts.URL)
	assert.Contains(t, res.Downloads["linux-x64"], ts.URL)
}

func TestFetchDesktopLatestFrom_NewRequestError(t *testing.T) {
	// base with an invalid URL makes http.NewRequestWithContext fail on the
	// first platform, returning the error.
	orig := upgradeHTTPClient
	defer func() { upgradeHTTPClient = orig }()

	_, err := fetchDesktopLatestFrom("http://exa mple.com")
	require.Error(t, err)
}

func TestFetchDesktopLatestFrom_DoError(t *testing.T) {
	// Client that always fails → upgradeHTTPClient.Do returns error on the
	// first platform.
	orig := upgradeHTTPClient
	defer func() { upgradeHTTPClient = orig }()

	upgradeHTTPClient = &http.Client{
		Timeout: 1 * time.Second,
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return nil, fmt.Errorf("connection refused")
			},
		},
	}

	_, err := fetchDesktopLatestFrom("https://registry.npmjs.org")
	require.Error(t, err)
}

func TestFetchDesktopLatestFrom_DecodeError(t *testing.T) {
	orig := upgradeHTTPClient
	defer func() { upgradeHTTPClient = orig }()

	// Server returns 200 but invalid JSON → json.Decode fails.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("not valid json"))
	}))
	defer ts.Close()

	upgradeHTTPClient = ts.Client()
	_, err := fetchDesktopLatestFrom(ts.URL)
	require.Error(t, err)
}

func TestFetchDesktopLatestFrom_EmptyTarballSkipsPlatform(t *testing.T) {
	orig := upgradeHTTPClient
	defer func() { upgradeHTTPClient = orig }()

	// All platforms return an empty tarball → skipped, result stays empty.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"version":"0.1.0","dist":{"tarball":"","integrity":""}}`))
	}))
	defer ts.Close()

	upgradeHTTPClient = ts.Client()
	res, err := fetchDesktopLatestFrom(ts.URL)
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Empty(t, res.Version)
	assert.Empty(t, res.Downloads)
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

func TestFetchDesktopLatest_UsesRegistryBase(t *testing.T) {
	orig := upgradeHTTPClient
	defer func() { upgradeHTTPClient = orig }()

	// Serve every requested registry path from a test server.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"version":"0.5.0","dist":{"tarball":"https://registry.npmjs.org/pkg/-/x.tgz"}}`))
	}))
	defer ts.Close()

	upgradeHTTPClient = ts.Client()
	origTransport := upgradeHTTPClient.Transport
	upgradeHTTPClient.Transport = &rewritingTransport{targetURL: ts.URL, orig: origTransport}
	defer func() { upgradeHTTPClient.Transport = origTransport }()

	// Force non-China registry base so requests go to npmjs.org (rewritten to ts).
	origChina := platform.ChinaMirrorChecked.Load()
	defer platform.ChinaMirrorChecked.Store(origChina)
	platform.ChinaMirrorChecked.Store(2) // non-China

	res, err := FetchDesktopLatest()
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, "0.5.0", res.Version)
	assert.NotEmpty(t, res.Downloads)
}

func TestFetchDesktopLatest_FallsBackToUserMirrorOn404(t *testing.T) {
	// Default registry returns 404 for every platform; user mirror returns a
	// valid package. FetchDesktopLatest must fall through to the mirror.
	failServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer failServer.Close()

	mirrorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"version":"0.9.0","dist":{"tarball":"https://mirror.example.com/pkg/-/x.tgz"}}`))
	}))
	defer mirrorServer.Close()

	orig := upgradeHTTPClient
	defer func() { upgradeHTTPClient = orig }()
	upgradeHTTPClient = &http.Client{Transport: &failoverTransport{
		defaultBase: failServer.URL,
		mirrorBase:  mirrorServer.URL,
	}}

	origChina := platform.ChinaMirrorChecked.Load()
	defer platform.ChinaMirrorChecked.Store(origChina)
	platform.ChinaMirrorChecked.Store(2) // non-China → default base = npmjs

	origEnv := os.Getenv("NPM_CONFIG_REGISTRY")
	defer os.Setenv("NPM_CONFIG_REGISTRY", origEnv)
	os.Setenv("NPM_CONFIG_REGISTRY", mirrorServer.URL)

	res, err := FetchDesktopLatest()
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, "0.9.0", res.Version)
	require.NotEmpty(t, res.Downloads)
}

func TestFetchDesktopLatest_AllSourcesFail(t *testing.T) {
	failServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer failServer.Close()

	orig := upgradeHTTPClient
	defer func() { upgradeHTTPClient = orig }()
	upgradeHTTPClient = &http.Client{Transport: &failoverTransport{
		defaultBase: failServer.URL,
		mirrorBase:  failServer.URL,
	}}

	origChina := platform.ChinaMirrorChecked.Load()
	defer platform.ChinaMirrorChecked.Store(origChina)
	platform.ChinaMirrorChecked.Store(2)

	origEnv := os.Getenv("NPM_CONFIG_REGISTRY")
	defer os.Setenv("NPM_CONFIG_REGISTRY", origEnv)
	os.Setenv("NPM_CONFIG_REGISTRY", failServer.URL)

	_, err := FetchDesktopLatest()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "all registry sources failed")
}

func TestFetchUpgradeInfo_DefaultSuccessSkipsMirror(t *testing.T) {
	// When the default registry succeeds, the user mirror must not be contacted.
	var mirrorHit bool
	failServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"version":"0.7.0","dist":{"tarball":"https://registry.npmjs.org/pkg/-/x.tgz"}}`))
	}))
	defer failServer.Close()

	orig := upgradeHTTPClient
	defer func() { upgradeHTTPClient = orig }()
	upgradeHTTPClient = &http.Client{Transport: &failoverTransport{
		defaultBase: failServer.URL,
		mirrorBase:  failServer.URL, // mirror host routed here too; flag below
		mirrorHit:   &mirrorHit,
	}}

	origChina := platform.ChinaMirrorChecked.Load()
	defer platform.ChinaMirrorChecked.Store(origChina)
	platform.ChinaMirrorChecked.Store(2)

	origEnv := os.Getenv("NPM_CONFIG_REGISTRY")
	defer os.Setenv("NPM_CONFIG_REGISTRY", origEnv)
	os.Setenv("NPM_CONFIG_REGISTRY", "https://user-mirror.example.com")

	info, err := fetchUpgradeInfo()
	require.NoError(t, err)
	assert.Equal(t, "0.7.0", info.LatestVersion)
	assert.False(t, mirrorHit, "user mirror should not be contacted when default succeeds")
}
