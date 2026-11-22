package service

import (
	"strings"
	"testing"

	"clawbench/internal/platform"
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

// withChina pins the cached mainland-China detection for one test and restores
// it after.
//
// Needed by every test that asserts candidate ORDER: releaseAssetURLs flips its
// ordering on platform.IsChinaMainland(), which reads a package-level cache.
// Without pinning it, such a test passes or fails depending on where the
// machine running it happens to be — a green CI run and a red local run (or
// vice versa) with no code difference.
//
// States: 0=unchecked, 1=China, 2=non-China.
func withChina(t *testing.T, china bool) {
	t.Helper()
	orig := platform.ChinaMirrorChecked.Load()
	v := int32(2)
	if china {
		v = 1
	}
	platform.ChinaMirrorChecked.Store(v)
	t.Cleanup(func() { platform.ChinaMirrorChecked.Store(orig) })
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
	assert.Empty(t, res.Checksums, "dev build must not offer checksums")
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
	// or every download 404s. Since those steps interpolate the release tag,
	// the expected names are versioned too.
	withVersion(t, "v0.98.0")

	res, err := FetchDesktopLatest()
	require.NoError(t, err)

	want := map[string]string{
		"linux-x64":    "clawbench-desktop-linux-x64-v0.98.0.zip",
		"linux-arm64":  "clawbench-desktop-linux-arm64-v0.98.0.zip",
		"darwin-x64":   "clawbench-desktop-darwin-x64-v0.98.0.zip",
		"darwin-arm64": "clawbench-desktop-darwin-arm64-v0.98.0.zip",
		"win32-x64":    "clawbench-desktop-windows-x64-v0.98.0.zip",
	}
	for key, asset := range want {
		urls := res.Downloads[key]
		require.NotEmptyf(t, urls, "missing %q", key)
		assert.Truef(t, strings.HasSuffix(urls[0], "/"+asset),
			"platform %q: expected asset %q, got %q", key, asset, urls[0])
	}
}

func TestDesktopAssetName_CarriesTheTag(t *testing.T) {
	// The regression this guards: an unversioned asset name. release.yml names
	// every asset with the tag, so a name built without one would 404 for every
	// user even though the URL itself is well-formed.
	name := desktopAssetName("linux/amd64", "v0.99.1")
	assert.Equal(t, "clawbench-desktop-linux-x64-v0.99.1.zip", name)
	assert.Contains(t, name, "v0.99.1", "the tag must appear in the asset name")

	// An unknown platform has no asset — the empty string lets callers skip it
	// rather than build a URL from a zero value.
	assert.Empty(t, desktopAssetName("plan9/386", "v0.99.1"))
}

func TestReleaseAssetURLs_AlwaysIncludesDirectGitHub(t *testing.T) {
	// Whatever the region, the canonical github.com URL must be among the
	// candidates — mirrors are community-run and may disappear.
	urls := releaseAssetURLs("v0.98.0", "clawbench-desktop-linux-x64-v0.98.0.zip")
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
	const asset = "clawbench-desktop-linux-x64-v0.98.0.zip"
	urls := releaseAssetURLs("v0.98.0", asset)
	for _, u := range urls {
		assert.Truef(t, strings.HasPrefix(u, "https://"),
			"candidate URL must be https: %q", u)
		assert.Containsf(t, u, "https://github.com/clawbench-dev/clawbench/releases/download/v0.98.0/"+asset,
			"mirror must wrap the canonical URL verbatim: %q", u)
	}
}

func TestFetchDesktopLatest_ReportsProtocolAndClientVersion(t *testing.T) {
	// The desktop gate compares the protocol number, not the version string.
	// A response without it silently reverts every client to the strict version
	// comparison — which is the behavior the number exists to replace.
	withVersion(t, "v0.98.0")

	res, err := FetchDesktopLatest()
	require.NoError(t, err)
	assert.Equal(t, version.ClientProtocol, res.Protocol)
	assert.Positive(t, res.Protocol)
	// An ordinary (rebuilt) release ships a client built from this same tag.
	assert.Equal(t, "v0.98.0", res.ClientVersion)
}

func TestFetchDesktopLatest_ClientVersionFollowsTheReusedBuild(t *testing.T) {
	// A release that reused the previous desktop package must report THAT
	// package's version: the installer keys the side-by-side directory off it,
	// and installing a v0.129 app under app-0.130 makes the startup handoff
	// delete the pointer and revert the upgrade.
	withVersion(t, "v0.130.0")
	orig := version.ClientBuildVersion
	version.ClientBuildVersion = "v0.129.0"
	t.Cleanup(func() { version.ClientBuildVersion = orig })

	res, err := FetchDesktopLatest()
	require.NoError(t, err)
	assert.Equal(t, "v0.130.0", res.Version, "the server's own version is unchanged")
	assert.Equal(t, "v0.129.0", res.ClientVersion, "downloads point at the reused build")
}

func TestFetchDesktopLatest_DevBuildStillReportsProtocol(t *testing.T) {
	// Even with no release to offer, the protocol must be present: a client on
	// a dev server still needs to know whether it is compatible.
	withVersion(t, "dev")

	res, err := FetchDesktopLatest()
	require.NoError(t, err)
	assert.Equal(t, version.ClientProtocol, res.Protocol)
}

func TestDesktopIntegratedAssetName_MatchesReleaseWorkflow(t *testing.T) {
	// The integrated package is assembled by the "Assemble integrated desktop"
	// steps in release.yml, which name it exactly this way. A drift here makes
	// every integrated upgrade 404 while the URL still looks well-formed.
	want := map[string]string{
		"windows/amd64": "clawbench-desktop-windows-x64-local-v0.99.1.zip",
		"linux/amd64":   "clawbench-desktop-linux-x64-local-v0.99.1.zip",
		"linux/arm64":   "clawbench-desktop-linux-arm64-local-v0.99.1.zip",
		"darwin/amd64":  "clawbench-desktop-darwin-x64-local-v0.99.1.zip",
		"darwin/arm64":  "clawbench-desktop-darwin-arm64-local-v0.99.1.zip",
	}
	for osArch, name := range want {
		assert.Equal(t, name, desktopIntegratedAssetName(osArch, "v0.99.1"), osArch)
	}
	// An unknown platform must yield the empty string so callers skip it instead
	// of building a dead URL.
	assert.Empty(t, desktopIntegratedAssetName("freebsd/amd64", "v0.99.1"))
}

func TestIntegratedDesktopDownloads_AllDesktopPlatformsAndNeverEmptyTag(t *testing.T) {
	withChina(t, false)

	urls := IntegratedDesktopDownloads("v0.99.1")
	// Every desktop platform ships an integrated package.
	assert.Len(t, urls, 5)
	for _, key := range []string{"win32-x64", "linux-x64", "linux-arm64", "darwin-x64", "darwin-arm64"} {
		require.NotEmpty(t, urls[key], key)
	}
	assert.True(t,
		strings.HasSuffix(urls["win32-x64"][0], "/clawbench-desktop-windows-x64-local-v0.99.1.zip"),
		"expected the integrated asset, got %q", urls["win32-x64"][0])
	assert.True(t,
		strings.HasSuffix(urls["darwin-arm64"][0], "/clawbench-desktop-darwin-arm64-local-v0.99.1.zip"),
		"expected the integrated asset, got %q", urls["darwin-arm64"][0])
	// No thin or payload URL may appear here: an integrated install that
	// installed either would lose its server or leave it stale.
	for _, list := range urls {
		for _, u := range list {
			assert.NotContains(t, u, "-payload")
			assert.Regexp(t, `-local-`, u)
		}
	}

	// An empty tag (dev build, or a registry answer with no release) must not
	// produce a URL at all.
	assert.Empty(t, IntegratedDesktopDownloads(""))
}

func TestIntegratedDesktopDownloads_UsesChinaMirrorsFirst(t *testing.T) {
	withChina(t, true)
	defer withChina(t, false)

	urls := IntegratedDesktopDownloads("v0.99.1")
	require.NotEmpty(t, urls["win32-x64"])
	assert.Contains(t, urls["win32-x64"][0], "gh-proxy.com/", "mainland China should try the mirror first")
	assert.Contains(t, urls["win32-x64"][len(urls["win32-x64"])-1], "github.com/", "direct must stay as the fallback")
}
