package service

import (
	"strings"
	"testing"

	"clawbench/internal/version"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withVersion pins internal/version.Version for one test and restores it after.
func withVersion(t *testing.T, v string) {
	t.Helper()
	orig := version.Version
	version.Version = v
	t.Cleanup(func() { version.Version = orig })
}

func TestFetchDesktopLatest_DevBuildOffersNothing(t *testing.T) {
	// A dev/untagged server must not advertise downloads: the URLs would point
	// at a release tag that does not exist.
	withVersion(t, "dev")

	res, err := FetchDesktopLatest()
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, "dev", res.Version)
	assert.Empty(t, res.Tag)
	assert.Empty(t, res.Downloads, "dev build must not offer download URLs")
}

func TestFetchDesktopLatest_ReleaseBuildOffersEveryPlatform(t *testing.T) {
	withVersion(t, "v0.98.0")

	res, err := FetchDesktopLatest()
	require.NoError(t, err)
	assert.Equal(t, "v0.98.0", res.Version)
	assert.Equal(t, "v0.98.0", res.Tag)

	// Every platform the web client can ask for must be present.
	for _, key := range []string{"linux-x64", "linux-arm64", "darwin-x64", "darwin-arm64", "win32-x64"} {
		urls, ok := res.Downloads[key]
		require.Truef(t, ok, "missing platform %q", key)
		require.NotEmptyf(t, urls, "platform %q has no candidate URLs", key)
		for _, u := range urls {
			assert.Containsf(t, u, "clawbench-dev/clawbench/releases/download/v0.98.0/", "bad URL %q", u)
		}
	}
}

func TestFetchDesktopLatest_AssetNamesMatchCI(t *testing.T) {
	// The asset filenames must match the `zip -r` steps in release.yml exactly,
	// or every download 404s.
	withVersion(t, "v0.98.0")

	res, err := FetchDesktopLatest()
	require.NoError(t, err)

	want := map[string]string{
		"linux-x64":    "clawbench-desktop-linux-x64.zip",
		"linux-arm64":  "clawbench-desktop-linux-arm64.zip",
		"darwin-x64":   "clawbench-desktop-darwin-x64.zip",
		"darwin-arm64": "clawbench-desktop-darwin-arm64.zip",
		"win32-x64":    "clawbench-desktop-windows-x64.zip",
	}
	for key, asset := range want {
		urls := res.Downloads[key]
		require.NotEmptyf(t, urls, "missing %q", key)
		assert.Truef(t, strings.HasSuffix(urls[0], "/"+asset),
			"platform %q: expected asset %q, got %q", key, asset, urls[0])
	}
}

func TestReleaseAssetURLs_AlwaysIncludesDirectGitHub(t *testing.T) {
	// Whatever the region, the canonical github.com URL must be among the
	// candidates — mirrors are community-run and may disappear.
	urls := releaseAssetURLs("v0.98.0", "clawbench-desktop-linux-x64.zip")
	require.NotEmpty(t, urls)

	var hasDirect bool
	for _, u := range urls {
		if strings.HasPrefix(u, "https://github.com/clawbench-dev/clawbench/releases/download/") {
			hasDirect = true
		}
	}
	assert.True(t, hasDirect, "direct github.com URL must always be a candidate")
}

func TestReleaseAssetURLs_MirrorPrefixesAreWellFormed(t *testing.T) {
	urls := releaseAssetURLs("v0.98.0", "clawbench-desktop-linux-x64.zip")
	for _, u := range urls {
		assert.Truef(t, strings.HasPrefix(u, "https://"),
			"candidate URL must be https: %q", u)
		assert.Containsf(t, u, "https://github.com/clawbench-dev/clawbench/releases/download/v0.98.0/clawbench-desktop-linux-x64.zip",
			"mirror must wrap the canonical URL verbatim: %q", u)
	}
}
