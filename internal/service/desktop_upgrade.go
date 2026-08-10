package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// desktopPlatformPkg maps GOOS/GOARCH to the desktop npm platform package,
// mirroring desktop/src/shared/registry.ts.
var desktopPlatformPkg = map[string]string{
	"linux/amd64":   "@xulongzhe/clawbench-desktop-linux-x64",
	"linux/arm64":   "@xulongzhe/clawbench-desktop-linux-arm64",
	"darwin/amd64":  "@xulongzhe/clawbench-desktop-darwin-x64",
	"darwin/arm64":  "@xulongzhe/clawbench-desktop-darwin-arm64",
	"windows/amd64": "@xulongzhe/clawbench-desktop-win32-x64",
}

// desktopDownloadKey is the response key for each platform (matches preload arch keys).
var desktopDownloadKey = map[string]string{
	"linux/amd64":   "linux-x64",
	"linux/arm64":   "linux-arm64",
	"darwin/amd64":  "darwin-x64",
	"darwin/arm64":  "darwin-arm64",
	"windows/amd64": "win32-x64",
}

// DesktopLatestResult is the response of GET /api/desktop/latest.
type DesktopLatestResult struct {
	Version   string            `json:"version"`
	Downloads map[string]string `json:"downloads"`
}

// fetchDesktopLatestFrom queries the npm registry base for each desktop platform
// package and returns the latest version plus per-platform tarball URLs.
// base is injectable for tests (httptest server).
func fetchDesktopLatestFrom(base string) (*DesktopLatestResult, error) {
	res := &DesktopLatestResult{Downloads: make(map[string]string)}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	for osArch, pkg := range desktopPlatformPkg {
		url := fmt.Sprintf("%s/%s/latest", base, pkg)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		resp, err := upgradeHTTPClient.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			_ = resp.Body.Close()
			// Platform package not published (or registry error) — skip it.
			continue
		}
		var npmResp npmRegistryResponse
		if err := json.NewDecoder(resp.Body).Decode(&npmResp); err != nil {
			_ = resp.Body.Close()
			return nil, err
		}
		_ = resp.Body.Close()

		tarball := npmResp.Dist.Tarball
		if tarball == "" {
			continue
		}
		if res.Version == "" || npmResp.Version > res.Version {
			res.Version = npmResp.Version
		}
		res.Downloads[desktopDownloadKey[osArch]] = rewriteTarballURL(tarball, base)
	}
	return res, nil
}

// FetchDesktopLatest queries the npm registry for the current region.
func FetchDesktopLatest() (*DesktopLatestResult, error) {
	return fetchDesktopLatestFrom(getRegistryBase())
}

// rewriteTarballURL points the tarball at the same registry base used for the query.
func rewriteTarballURL(tarball, base string) string {
	const npmjs = "https://registry.npmjs.org"
	if base != npmjs && len(tarball) > len(npmjs) && tarball[:len(npmjs)] == npmjs {
		return base + tarball[len(npmjs):]
	}
	return tarball
}
