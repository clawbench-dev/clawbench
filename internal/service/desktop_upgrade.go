package service

import (
	"clawbench/internal/platform/source"
	"clawbench/internal/version"
)

// desktopReleaseRepo is the GitHub repository that hosts the desktop release
// assets produced by release.yml's build-desktop-* jobs.
const desktopReleaseRepo = "clawbench-dev/clawbench"

// Platform keys (GOOS/GOARCH) shared by the maps below.
const (
	platformLinuxAMD64   = "linux/amd64"
	platformLinuxARM64   = "linux/arm64"
	platformDarwinAMD64  = "darwin/amd64"
	platformDarwinARM64  = "darwin/arm64"
	platformWindowsAMD64 = "windows/amd64"
)

// desktopAssetBase maps GOOS/GOARCH to the release asset basename, without the
// version. The tag is appended by desktopAssetName. Keep in sync with the
// `zip -r` / `Compress-Archive` steps in release.yml.
var desktopAssetBase = map[string]string{
	platformLinuxAMD64:   "clawbench-desktop-linux-x64",
	platformLinuxARM64:   "clawbench-desktop-linux-arm64",
	platformDarwinAMD64:  "clawbench-desktop-darwin-x64",
	platformDarwinARM64:  "clawbench-desktop-darwin-arm64",
	platformWindowsAMD64: "clawbench-desktop-windows-x64",
}

// desktopAssetName builds the published asset filename for a platform, e.g.
// "clawbench-desktop-linux-x64-v0.99.1.zip".
//
// The tag is part of the name because release.yml puts it there, so a file in a
// Downloads folder is self-identifying. That makes the URL version-specific:
// there is no stable ".../latest/download/<name>" form for these assets, which
// is why the download URLs are always built from the tag the server reports
// rather than from a constant.
func desktopAssetName(osArch, tag string) string {
	base, ok := desktopAssetBase[osArch]
	if !ok {
		return ""
	}
	return base + "-" + tag + ".zip"
}

// desktopFusedAssetBase maps GOOS/GOARCH to the basename of the all-in-one
// ("fused") package: the same app as desktopAssetBase, plus the ClawBench
// server binary at the archive root (see extraFiles in electron-builder.yml).
//
// Only Windows publishes one. The package is assembled in release.yml from the
// thin zip of the SAME release, so it exists even when the desktop shell itself
// was reused from a previous tag — the server is what changes then.
var desktopFusedAssetBase = map[string]string{
	platformWindowsAMD64: "clawbench-desktop-windows-x64-local",
}

// desktopFusedAssetName builds the published filename of the all-in-one
// package, e.g. "clawbench-desktop-windows-x64-local-v0.99.1.zip".
//
// Keep in sync with the "Assemble fused desktop (Windows)" step in release.yml.
func desktopFusedAssetName(osArch, tag string) string {
	base, ok := desktopFusedAssetBase[osArch]
	if !ok {
		return ""
	}
	return base + "-" + tag + ".zip"
}

// FusedDesktopDownloads returns the candidate download URLs of the all-in-one
// package for a release tag, keyed by the client's platform key (the same keys
// as DesktopLatestResult.Downloads).
//
// This is what a fused install upgrades with: it must never be handed the thin
// package (that would delete the only server it has). An empty tag or a
// platform with no fused package yields an empty map, which callers read as
// "nothing to offer" rather than building a URL that 404s.
func FusedDesktopDownloads(tag string) map[string][]string {
	out := map[string][]string{}
	if tag == "" {
		return out
	}
	for osArch := range desktopFusedAssetBase {
		asset := desktopFusedAssetName(osArch, tag)
		if asset == "" {
			continue
		}
		out[desktopDownloadKey[osArch]] = releaseAssetURLs(tag, asset)
	}
	return out
}

// desktopDownloadKey is the response key for each platform. It matches the
// keys the web client derives from the user agent (detectPlatformKey).
var desktopDownloadKey = map[string]string{
	platformLinuxAMD64:   "linux-x64",
	platformLinuxARM64:   "linux-arm64",
	platformDarwinAMD64:  "darwin-x64",
	platformDarwinARM64:  "darwin-arm64",
	platformWindowsAMD64: "win32-x64",
}

// DesktopLatestResult is the response of GET /api/desktop/latest.
type DesktopLatestResult struct {
	Version string `json:"version"`
	// Tag is the GitHub release tag the downloads point at, or "" when the
	// server is a dev build with no matching release (Downloads is then empty).
	Tag string `json:"tag"`
	// ClientVersion is the version of the desktop package these URLs actually
	// ship. It equals Version for an ordinary release, and the previous
	// release's version when this release reused that build untouched (see
	// release.yml's client-guard job). The installer keys the side-by-side
	// install directory off it: installing a v0.129 app under app-0.130 makes
	// app.getVersion() disagree with the pointer, and the startup handoff then
	// deletes the pointer, silently reverting the upgrade.
	ClientVersion string `json:"clientVersion"`
	// Protocol is the client/server compatibility number (see
	// internal/version/client_protocol.txt). The desktop gate compares it with
	// the number compiled into the shell instead of comparing version strings,
	// which is what lets an unchanged client keep working against a newer
	// server.
	Protocol int `json:"protocol"`
	// Downloads maps a platform key to an ordered list of candidate URLs. The
	// client tries them in order, so one dead mirror degrades the download
	// rather than breaking it.
	Downloads map[string][]string `json:"downloads"`
	// Checksums maps an asset filename to its SHA256, from the release's
	// SHA256SUMS. Absent on dev builds and on servers that cannot reach the
	// manifest; the client then skips verification.
	Checksums map[string]string `json:"checksums,omitempty"`
}

// releaseAssetURLs returns the candidate download URLs for one release asset,
// ordered by what is likely to work from the server's region. The direct
// github.com URL is always present, last for China and first elsewhere.
func releaseAssetURLs(tag, asset string) []string {
	return source.ReleaseURLs(desktopReleaseRepo, tag, asset)
}

// FetchDesktopLatest builds the desktop download info for the running server
// version.
//
// It deliberately does NOT query an npm registry. The desktop client ships in
// lockstep with the server — the server is what tells the client which version
// to run — so the server's own version IS the latest desktop version.
//
// It DOES fetch the release's SHA256SUMS from GitHub (best-effort, cached per
// tag) so the client can verify the archive. A failed fetch yields no checksums
// and does not block the response; the client then downloads over https only.
func FetchDesktopLatest() (*DesktopLatestResult, error) {
	v := version.Get()
	tag := version.ReleaseTag(v)
	res := &DesktopLatestResult{
		Version:       v,
		Tag:           tag,
		ClientVersion: version.GetClientBuildVersion(),
		Protocol:      version.ClientProtocol,
		Downloads:     map[string][]string{},
	}
	if tag == "" {
		// Dev or untagged build: no release exists, so offer nothing rather
		// than links that would 404.
		return res, nil
	}
	for osArch := range desktopAssetBase {
		asset := desktopAssetName(osArch, tag)
		res.Downloads[desktopDownloadKey[osArch]] = releaseAssetURLs(tag, asset)
	}
	res.Checksums = desktopPackageChecksums(tag)
	return res, nil
}
