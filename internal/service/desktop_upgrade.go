package service

import (
	"fmt"
	"net/url"

	"clawbench/internal/platform"
	"clawbench/internal/version"
)

// desktopReleaseRepo is the GitHub repository that hosts the desktop release
// assets produced by release.yml's build-desktop-* jobs.
const desktopReleaseRepo = "clawbench-dev/clawbench"

// desktopAssetBase maps GOOS/GOARCH to the release asset basename, without the
// version. The tag is appended by desktopAssetName. Keep in sync with the
// `zip -r` / `Compress-Archive` steps in release.yml.
var desktopAssetBase = map[string]string{
	"linux/amd64":   "clawbench-desktop-linux-x64",
	"linux/arm64":   "clawbench-desktop-linux-arm64",
	"darwin/amd64":  "clawbench-desktop-darwin-x64",
	"darwin/arm64":  "clawbench-desktop-darwin-arm64",
	"windows/amd64": "clawbench-desktop-windows-x64",
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

// desktopDownloadKey is the response key for each platform. It matches the
// keys the web client derives from the user agent (detectPlatformKey).
var desktopDownloadKey = map[string]string{
	"linux/amd64":   "linux-x64",
	"linux/arm64":   "linux-arm64",
	"darwin/amd64":  "darwin-x64",
	"darwin/arm64":  "darwin-arm64",
	"windows/amd64": "win32-x64",
}

// githubReleaseMirrors are prefix proxies that forward to github.com. They are
// tried before the direct URL for users in mainland China, where release
// downloads from github.com are frequently slow or unreachable.
//
// These are community-run services with no uptime guarantee, so they are only
// candidates: the client walks the list in order, and the direct github.com URL
// is always included as the final fallback.
var githubReleaseMirrors = []string{
	"https://gh-proxy.com/",
	"https://ghproxy.net/",
}

// DesktopLatestResult is the response of GET /api/desktop/latest.
type DesktopLatestResult struct {
	Version string `json:"version"`
	// Tag is the GitHub release tag the downloads point at, or "" when the
	// server is a dev build with no matching release (Downloads is then empty).
	Tag string `json:"tag"`
	// Downloads maps a platform key to an ordered list of candidate URLs. The
	// client tries them in order, so one dead mirror degrades the download
	// rather than breaking it.
	Downloads map[string][]string `json:"downloads"`
}

// releaseAssetURLs returns the candidate download URLs for one release asset,
// ordered by what is likely to work from the server's region. The direct
// github.com URL is always present, last for China and first elsewhere.
func releaseAssetURLs(tag, asset string) []string {
	direct := fmt.Sprintf("https://github.com/%s/releases/download/%s/%s",
		desktopReleaseRepo, url.PathEscape(tag), url.PathEscape(asset))

	mirrors := make([]string, 0, len(githubReleaseMirrors))
	for _, m := range githubReleaseMirrors {
		mirrors = append(mirrors, m+direct)
	}
	if platform.IsChinaMainland() {
		return append(mirrors, direct)
	}
	return append([]string{direct}, mirrors...)
}

// FetchDesktopLatest builds the desktop download info for the running server
// version.
//
// It deliberately does NOT query an external registry. The desktop client ships
// in lockstep with the server — the server is what tells the client which
// version to run — so the server's own version IS the latest desktop version.
// That removes the dependency on npm, whose size limits were the only reason
// these packages were difficult to publish, and on any third-party API.
func FetchDesktopLatest() (*DesktopLatestResult, error) {
	v := version.Get()
	tag := version.ReleaseTag(v)
	res := &DesktopLatestResult{Version: v, Tag: tag, Downloads: map[string][]string{}}
	if tag == "" {
		// Dev or untagged build: no release exists, so offer nothing rather
		// than links that would 404.
		return res, nil
	}
	for osArch := range desktopAssetBase {
		asset := desktopAssetName(osArch, tag)
		res.Downloads[desktopDownloadKey[osArch]] = releaseAssetURLs(tag, asset)
	}
	return res, nil
}
