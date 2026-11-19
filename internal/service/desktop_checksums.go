package service

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// desktopChecksumsFilename is the release asset carrying the SHA256 of every
// desktop archive (full packages and the fused package).
const desktopChecksumsFilename = "SHA256SUMS"

// desktopChecksumsTimeout bounds the manifest fetch. It runs inside
// /api/desktop/latest and /api/upgrade/check, both of which the UI expects to
// answer quickly; a missing manifest is not fatal (the client just skips
// verification), so a short deadline is the right trade.
const desktopChecksumsTimeout = 3 * time.Second

var (
	// Test seams.
	desktopChecksumsURL = func(tag string) string {
		return "https://github.com/" + desktopReleaseRepo +
			"/releases/download/" + url.PathEscape(tag) + "/" + desktopChecksumsFilename
	}
	desktopChecksumsHTTPClient = &http.Client{Timeout: desktopChecksumsTimeout}

	// desktopChecksumsFetch is a test seam: the API tests must not reach
	// github.com, and the manifest tests replace it with the real fetcher.
	desktopChecksumsFetch = fetchDesktopChecksums

	desktopChecksumsMu    sync.Mutex
	desktopChecksumsByTag = map[string]map[string]string{}
)

// desktopChecksums returns the parsed SHA256SUMS for a tag, or nil when it
// cannot be fetched. Results (including failures) are cached for the process
// lifetime: a release tag is immutable, and both API endpoints ask for the same
// map.
//
// The manifest is fetched from github.com DIRECTLY, never through the release
// prefix proxies. If the zip and the checksum both came from the same proxy, a
// compromised proxy could replace both and the check would prove nothing.
func desktopChecksums(tag string) map[string]string {
	if tag == "" {
		return nil
	}

	desktopChecksumsMu.Lock()
	if cached, ok := desktopChecksumsByTag[tag]; ok {
		desktopChecksumsMu.Unlock()
		return cached
	}
	desktopChecksumsMu.Unlock()

	checksums := desktopChecksumsFetch(tag)

	desktopChecksumsMu.Lock()
	desktopChecksumsByTag[tag] = checksums
	desktopChecksumsMu.Unlock()
	return checksums
}

func fetchDesktopChecksums(tag string) map[string]string {
	ctx, cancel := context.WithTimeout(context.Background(), desktopChecksumsTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, desktopChecksumsURL(tag), http.NoBody)
	if err != nil {
		slog.Warn("desktop: cannot build checksum request", "tag", tag, "error", err)
		return nil
	}
	resp, err := desktopChecksumsHTTPClient.Do(req)
	if err != nil {
		slog.Warn("desktop: checksum manifest unreachable", "tag", tag, "error", err)
		return nil
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		slog.Warn("desktop: checksum manifest returned non-200", "tag", tag, "status", resp.StatusCode)
		return nil
	}

	checksums, err := parseSHA256SUMS(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		slog.Warn("desktop: checksum manifest unreadable", "tag", tag, "error", err)
		return nil
	}
	if len(checksums) == 0 {
		slog.Warn("desktop: checksum manifest had no usable lines", "tag", tag)
		return nil
	}
	return checksums
}

// parseSHA256SUMS parses coreutils-style lines: "<64 lowercase hex>  <name>".
// Malformed lines are skipped; a file with no valid line yields an empty map.
func parseSHA256SUMS(r io.Reader) (map[string]string, error) {
	out := map[string]string{}
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 2 {
			continue
		}
		hash, name := fields[0], fields[1]
		if !isSHA256Hex(hash) {
			continue
		}
		out[name] = hash
	}
	return out, scanner.Err()
}

func isSHA256Hex(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// desktopPackageChecksums returns only the full-package entries for a tag.
func desktopPackageChecksums(tag string) map[string]string {
	all := desktopChecksums(tag)
	if len(all) == 0 {
		return nil
	}
	out := map[string]string{}
	for osArch := range desktopAssetBase {
		name := desktopAssetName(osArch, tag)
		if hash, ok := all[name]; ok {
			out[name] = hash
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// FusedDesktopChecksums returns the fused-package entries for a tag, used by
// /api/upgrade/check next to fused_downloads.
func FusedDesktopChecksums(tag string) map[string]string {
	all := desktopChecksums(tag)
	if len(all) == 0 {
		return nil
	}
	out := map[string]string{}
	for osArch := range desktopFusedAssetBase {
		name := desktopFusedAssetName(osArch, tag)
		if hash, ok := all[name]; ok {
			out[name] = hash
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
