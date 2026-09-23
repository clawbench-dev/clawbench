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
// Needed by every test that asserts candidate ORDER: releaseAssetURLs and
// desktopPayloadURLs both flip their ordering on platform.IsChinaMainland(),
// which reads a package-level cache. Without pinning it, such a test passes or
// fails depending on where the machine running it happens to be — a green CI
// run and a red local run (or vice versa) with no code difference.
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
	assert.Empty(t, res.Payloads, "dev build must not offer payload URLs")
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

func TestFetchDesktopLatest_PayloadsCoverOnlySignablePlatforms(t *testing.T) {
	// The payload archive reuses the installed Electron runtime, so it is only
	// offered where replacing app.asar is safe. macOS is excluded because it
	// breaks the code-signature seal of the .app bundle.
	withVersion(t, "v0.98.0")
	withChina(t, false)

	res, err := FetchDesktopLatest()
	require.NoError(t, err)

	for _, key := range []string{"linux-x64", "linux-arm64", "win32-x64"} {
		urls, ok := res.Payloads[key]
		require.Truef(t, ok, "missing payload for %q", key)
		require.NotEmptyf(t, urls, "payload for %q has no candidate URLs", key)

		// Every candidate must be one of the two known sources. A payload from
		// an unknown host would be an unvetted download, so assert the set
		// rather than merely that the list is non-empty.
		var hasGitHub, hasNpm bool
		for _, u := range urls {
			switch {
			case strings.Contains(u, "github.com/clawbench-dev/clawbench/releases/download/v0.98.0/"):
				hasGitHub = true
			case strings.Contains(u, "registry.npmjs.org/@xulongzhe/clawbench-desktop-") && strings.HasSuffix(u, ".tgz"):
				hasNpm = true
			default:
				t.Errorf("platform %q: unexpected candidate %q", key, u)
			}
		}
		assert.Truef(t, hasGitHub, "platform %q: github release must always be a candidate", key)
		assert.Truef(t, hasNpm, "platform %q: npm registry must be a candidate", key)
	}

	// ABSENT, not an empty list: the client distinguishes "no payload for this
	// platform" (fall back to the full package) from "server too old to know".
	for _, key := range []string{"darwin-x64", "darwin-arm64"} {
		_, ok := res.Payloads[key]
		assert.Falsef(t, ok, "macOS must not advertise a payload (%q)", key)
	}
}

func TestFetchDesktopLatest_PayloadAssetNamesMatchCI(t *testing.T) {
	// Same contract as the full-package names: the GitHub asset must match the
	// payload `zip`/`Compress-Archive` steps in release.yml exactly or every
	// payload download 404s — and because the client falls back to the full
	// package, that failure would be invisible.
	//
	// The region is pinned because npm sorts ahead of github in China, so the
	// .zip is no longer necessarily urls[0].
	withVersion(t, "v0.98.0")
	withChina(t, false)

	res, err := FetchDesktopLatest()
	require.NoError(t, err)

	want := map[string]string{
		"linux-x64":   "clawbench-desktop-linux-x64-payload-v0.98.0.zip",
		"linux-arm64": "clawbench-desktop-linux-arm64-payload-v0.98.0.zip",
		"win32-x64":   "clawbench-desktop-windows-x64-payload-v0.98.0.zip",
	}
	for key, asset := range want {
		urls := res.Payloads[key]
		require.NotEmptyf(t, urls, "missing %q", key)
		assert.Truef(t, strings.HasSuffix(urls[0], "/"+asset),
			"platform %q: expected asset %q first outside China, got %q", key, asset, urls[0])
	}
}

func TestDesktopPayloadNpmURLs_ScopeDroppedFromFilename(t *testing.T) {
	// Two npm naming rules that are easy to get wrong: the tarball FILENAME
	// drops the scope, and the VERSION carries no "v" (npm versions are semver,
	// a release tag is not).
	withChina(t, false)

	urls := desktopPayloadNpmURLs("linux/amd64", "v0.98.0")
	require.Len(t, urls, 1)
	assert.Equal(t,
		"https://registry.npmjs.org/@xulongzhe/clawbench-desktop-linux-x64-payload/-/clawbench-desktop-linux-x64-payload-0.98.0.tgz",
		urls[0])
	assert.NotContains(t, urls[0], "/-/%40", "scope must not be escaped into the filename")
	assert.NotContains(t, urls[0], "v0.98.0", "npm version must not carry the tag's v prefix")

	// macOS has no payload, so it has no npm package either.
	assert.Empty(t, desktopPayloadNpmURLs("darwin/arm64", "v0.98.0"))
}

func TestDesktopPayloadNpmURLs_UsesChinaMirrorInChina(t *testing.T) {
	// Reuses the region detection the server self-upgrade already uses, so
	// there is one place deciding which registry is reachable.
	withChina(t, true)

	urls := desktopPayloadNpmURLs("linux/amd64", "v0.98.0")
	require.Len(t, urls, 1)
	assert.True(t, strings.HasPrefix(urls[0], platform.NpmMirrorRegistry+"/"),
		"in China the candidate must come from the npm mirror: %q", urls[0])
	assert.NotContains(t, urls[0], "registry.npmjs.org")
}

func TestDesktopPayloadNpmURLs_ExcludesServerSidePrivateMirror(t *testing.T) {
	// registryCandidates() also carries the SERVER's ~/.npmrc / NPM_CONFIG_REGISTRY.
	// These URLs are consumed by the desktop CLIENT, a different machine, where a
	// server-side private registry is usually unreachable — including it would put
	// a dead candidate in every response.
	withChina(t, false)
	t.Setenv("NPM_CONFIG_REGISTRY", "https://npm.internal.example.com")

	urls := desktopPayloadNpmURLs("linux/amd64", "v0.98.0")
	for _, u := range urls {
		assert.NotContains(t, u, "internal.example.com",
			"the server's private registry must not leak into client-facing URLs: %q", u)
	}
}

func TestDesktopPayloadURLs_OrderingFollowsRegion(t *testing.T) {
	// npm first in China (github.com is unreliable there), github first
	// elsewhere. Both directions are asserted with the region pinned, so this
	// cannot pass merely because the test machine happens to be in one region.
	const tag = "v0.98.0"

	withChina(t, true)
	cn := desktopPayloadURLs("linux/amd64", tag)
	require.NotEmpty(t, cn)
	assert.True(t, strings.HasSuffix(cn[0], ".tgz"), "China: npm tarball should be first, got %q", cn[0])
	assert.Contains(t, cn[len(cn)-1], "github.com", "China: github must remain the final fallback")

	withChina(t, false)
	row := desktopPayloadURLs("linux/amd64", tag)
	require.NotEmpty(t, row)
	assert.Contains(t, row[0], "github.com", "elsewhere: direct github should be first, got %q", row[0])
	assert.True(t, strings.HasSuffix(row[len(row)-1], ".tgz"), "elsewhere: npm should be last, got %q", row[len(row)-1])
}

func TestDesktopPayloadAssetName_CarriesTheTag(t *testing.T) {
	name := desktopPayloadAssetName("linux/amd64", "v0.99.1")
	assert.Equal(t, "clawbench-desktop-linux-x64-payload-v0.99.1.zip", name)
	assert.Contains(t, name, "v0.99.1", "the tag must appear in the payload name")

	// macOS and unknown platforms have no payload.
	assert.Empty(t, desktopPayloadAssetName("darwin/arm64", "v0.99.1"))
	assert.Empty(t, desktopPayloadAssetName("plan9/386", "v0.99.1"))
}

func TestDesktopAssetNameFromBase_SharedByBothArchives(t *testing.T) {
	// Both archive kinds are named through one helper so the two cannot drift
	// into different conventions (which releaseAssets.test.ts also pins).
	assert.Equal(t, "base-v1.0.0.zip", desktopAssetNameFromBase("base", "v1.0.0"))
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
