// Package source owns the "where do we download this from" policy for every
// server-side download: npm packages (the CLI and agent installers), GitHub
// Release assets (desktop and fused packages), and Node.js binaries.
//
// It is deliberately pure: it reads environment variables and the user-level
// ~/.npmrc, but performs no network requests and runs no commands. Callers that
// need to probe a registry (the agent installer) do so with the candidate list
// returned here.
package source

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"clawbench/internal/platform"
)

// Public registry bases. The signature verifier keeps its own "which registry
// carries npm's signing keys" rule; these constants only say where to download.
const (
	NpmJsBase     = "https://registry.npmjs.org"
	NpmMirrorBase = "https://registry.npmmirror.com"
)

// GitHub release prefix proxies. Community-run, so they are candidates only;
// the direct github.com URL is always present.
const (
	githubProxyPrefix = "https://gh-proxy.com/"
	githubFastPrefix  = "https://ghfast.top/"
)

// Candidate is one download source. Name is for logs and error messages; Base
// is a normalized address without a trailing slash (empty for a direct GitHub
// URL).
type Candidate struct {
	Name string
	Base string
}

// Test seams. Production code must not replace these.
var (
	regionProbe = platform.IsChinaMainland
	getenv      = os.Getenv
	userHomeDir = platform.UserHomeDir
)

// Region reports whether this machine appears to be in mainland China.
func Region() bool { return regionProbe() }

// Npm returns the ordered npm registry candidates:
//
//	user-configured → region-preferred → the other public registry
//
// Duplicates are removed. A user-configured source is therefore always first;
// when none is configured the region's mirror is first.
func Npm() []Candidate {
	var out []Candidate
	if user := UserNpmRegistry(); user != "" {
		out = append(out, Candidate{Name: "user", Base: user})
	}
	if Region() {
		out = append(out,
			Candidate{Name: "region-mirror", Base: NpmMirrorBase},
			Candidate{Name: "official", Base: NpmJsBase},
		)
	} else {
		out = append(out,
			Candidate{Name: "official", Base: NpmJsBase},
			Candidate{Name: "mirror", Base: NpmMirrorBase},
		)
	}
	return dedupe(out)
}

// ReleaseURLs returns the full candidate URLs for one GitHub Release asset:
// prefix proxies first in mainland China, direct github.com first elsewhere.
// repo is "owner/name", tag is the release tag, asset is the asset filename.
func ReleaseURLs(repo, tag, asset string) []string {
	direct := "https://github.com/" + strings.Trim(repo, "/") +
		"/releases/download/" + url.PathEscape(tag) + "/" + url.PathEscape(asset)

	prefixes := []string{githubProxyPrefix, githubFastPrefix}
	out := make([]string, 0, len(prefixes)+1)
	if Region() {
		for _, prefix := range prefixes {
			out = append(out, prefix+direct)
		}
		return append(out, direct)
	}
	out = append(out, direct)
	for _, prefix := range prefixes {
		out = append(out, prefix+direct)
	}
	return out
}

// UserNpmRegistry returns the user's explicitly configured npm registry, or "".
// The order is NPM_CONFIG_REGISTRY, npm_config_registry, then the user-level
// ~/.npmrc registry= line.
func UserNpmRegistry() string {
	for _, key := range []string{"NPM_CONFIG_REGISTRY", "npm_config_registry"} {
		if base := normalizeRegistryBase(getenv(key)); base != "" {
			return base
		}
	}
	return normalizeRegistryBase(readNpmrcRegistry())
}

// URL joins the candidate's base with path, preserving a base path prefix and
// avoiding a doubled slash.
func (c Candidate) URL(path string) string {
	return strings.TrimRight(c.Base, "/") + "/" + strings.TrimLeft(path, "/")
}

// normalizeRegistryBase trims and validates a registry value into
// "<scheme>://<host>[:port][/path]" without a trailing slash, or "".
//
// https is always accepted. http is accepted only for loopback, because a
// plaintext registry can rewrite both metadata and the tarball URL it points
// at. Credentials embedded in the URL are rejected: the value is logged, and a
// URL with a password would leak it.
func normalizeRegistryBase(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	u, err := url.Parse(value)
	if err != nil || u.Host == "" || u.User != nil {
		return ""
	}
	if u.Scheme != "https" && (u.Scheme != "http" || !isLoopbackHost(u.Hostname())) {
		return ""
	}
	base := u.Scheme + "://" + u.Host + strings.TrimRight(u.Path, "/")
	return base
}

func isLoopbackHost(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// readNpmrcRegistry reads the registry= line from the user-level ~/.npmrc.
func readNpmrcRegistry() string {
	data, err := os.ReadFile(filepath.Join(userHomeDir(), ".npmrc"))
	if err != nil {
		return ""
	}
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if strings.TrimSpace(key) == "registry" {
			return strings.TrimSpace(val)
		}
	}
	return ""
}

func dedupe(in []Candidate) []Candidate {
	seen := make(map[string]struct{}, len(in))
	out := in[:0]
	for _, c := range in {
		if c.Base == "" {
			continue
		}
		if _, ok := seen[c.Base]; ok {
			continue
		}
		seen[c.Base] = struct{}{}
		out = append(out, c)
	}
	return out
}
