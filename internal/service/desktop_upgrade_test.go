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

		// Every candidate must be an npm tarball. The payload is no longer a
		// GitHub release asset, so a github.com URL here would 404 and silently
		// fall back to the ~150MB full download.
		for _, u := range urls {
			assert.Truef(t, strings.Contains(u, "registry.npmjs.org/@xulongzhe/clawbench-desktop-") && strings.HasSuffix(u, ".tgz"),
				"platform %q: payload must be an npm tarball, got %q", key, u)
			assert.NotContainsf(t, u, "github.com", "platform %q: payload must not point at a GitHub release asset: %q", key, u)
		}
	}

	// ABSENT, not an empty list: the client distinguishes "no payload for this
	// platform" (fall back to the full package) from "server too old to know".
	for _, key := range []string{"darwin-x64", "darwin-arm64"} {
		_, ok := res.Payloads[key]
		assert.Falsef(t, ok, "macOS must not advertise a payload (%q)", key)
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

func TestDesktopPayloadURLs_NpmOnly(t *testing.T) {
	// The payload is published to npm only — it is not attached to the GitHub
	// Release (a ~3MB zip beside the ~150MB full package misleads anyone
	// browsing the assets). Both regions must therefore yield the npm tarball
	// and nothing else, so a reintroduced github.com candidate fails here.
	const tag = "v0.98.0"

	withChina(t, true)
	cn := desktopPayloadURLs("linux/amd64", tag)
	require.Len(t, cn, 1)
	assert.True(t, strings.HasSuffix(cn[0], ".tgz"), "China: expected the npm tarball, got %q", cn[0])
	assert.Contains(t, cn[0], platform.NpmMirrorRegistry+"/")

	withChina(t, false)
	row := desktopPayloadURLs("linux/amd64", tag)
	require.Len(t, row, 1)
	assert.Contains(t, row[0], "registry.npmjs.org", "elsewhere: expected the npm tarball, got %q", row[0])
	for _, u := range row {
		assert.NotContains(t, u, "github.com", "the payload must never point at a GitHub release asset: %q", u)
	}
}

func TestDesktopPayloadURLs_EmptyWhenPlatformHasNoPayload(t *testing.T) {
	// macOS has no payload archive, so there is nothing to download. The whole
	// candidate list must be empty — not "npm only" — otherwise the client would
	// offer an upgrade whose download can never succeed.
	withChina(t, false)

	assert.Empty(t, desktopPayloadURLs("darwin/arm64", "v0.98.0"))
	assert.Empty(t, desktopPayloadURLs("plan9/386", "v0.98.0"))
}

func TestDesktopAssetNameFromBase_SharedByBothArchives(t *testing.T) {
	// The full package name is built by appending the tag; the payload has no
	// release asset name any more (it is npm-only), so only this shape is
	// asserted.
	assert.Equal(t, "clawbench-desktop-linux-x64-v1.0.0.zip", desktopAssetName("linux/amd64", "v1.0.0"))
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
