package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetchDesktopLatestFrom(t *testing.T) {
	orig := upgradeHTTPClient
	defer func() { upgradeHTTPClient = orig }()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pkg := r.URL.Path
		version := "0.1.0"
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
