// Package forge provides a unified, read-only abstraction over source-code
// hosting platforms (GitHub, GitLab) for browsing issues and PR/MR content.
package forge

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// Platform identifies a supported forge platform.
type Platform string

const (
	PlatformGitHub Platform = "github"
	PlatformGitLab Platform = "gitlab"
)

// Official hosts for each platform.
const (
	GitHubHost = "github.com"
	GitLabHost = "gitlab.com"
)

// Remote is a parsed git remote pointing at a forge repository.
type Remote struct {
	Platform Platform
	// Host is the lowercased host, including a port when the remote specifies
	// one (e.g. "git.acme.internal:8443"). Used as the credential scope key.
	Host string
	// Owner is the namespace path without the repository segment. GitHub always
	// has a single segment; GitLab may have several (multi-level groups).
	Owner string
	// Repo is the final path segment with any ".git" suffix removed.
	Repo string
}

// Slug returns the canonical "owner/repo" identifier.
func (r Remote) Slug() string {
	return r.Owner + "/" + r.Repo
}

// IsOfficialHost reports whether the remote points at a platform-operated host
// (github.com / gitlab.com) rather than a self-hosted instance.
func (r Remote) IsOfficialHost() bool {
	hostname := r.Host
	if i := strings.LastIndex(hostname, ":"); i >= 0 {
		hostname = hostname[:i]
	}
	return hostname == GitHubHost || hostname == GitLabHost
}

var (
	// ErrUnsupportedRemote is returned for remotes that are not a forge URL
	// (local paths, file:// URLs, unsupported schemes).
	ErrUnsupportedRemote = errors.New("unsupported remote url")
	// ErrInvalidRemote is returned for forge URLs with an unusable path shape.
	ErrInvalidRemote = errors.New("invalid forge remote url")
)

// ParseRemoteURL parses a git remote URL into a Remote. It accepts HTTPS and
// SSH forms (both "ssh://" and the scp-like "git@host:path" form) and rejects
// local paths, file:// and other schemes.
//
// Host is classified as GitHub only when it is github.com; every other host is
// treated as GitLab (self-hosted GitLab is a first-class target). Callers must
// still run CheckHostSafety and require explicit user confirmation before
// sending credentials to a non-official host.
func ParseRemoteURL(raw string) (Remote, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Remote{}, ErrUnsupportedRemote
	}

	host, path, err := splitRemote(raw)
	if err != nil {
		return Remote{}, err
	}

	host = strings.ToLower(host)
	// Reject a scheme-less scp host that still carries credentials or is empty.
	if host == "" || strings.Contains(host, "@") {
		return Remote{}, ErrInvalidRemote
	}

	path = strings.Trim(path, "/")
	path = strings.TrimSuffix(path, ".git")
	if path == "" {
		return Remote{}, ErrInvalidRemote
	}

	segments := strings.Split(path, "/")
	for _, seg := range segments {
		if seg == "" {
			return Remote{}, ErrInvalidRemote
		}
		// GitLab reserves "-" as the web-UI path separator; it can never be a
		// namespace or repository name, so its presence means this is a web URL
		// (e.g. /group/repo/-/tree/main) rather than a clone URL.
		if seg == "-" {
			return Remote{}, ErrInvalidRemote
		}
	}
	if len(segments) < 2 {
		return Remote{}, ErrInvalidRemote
	}

	platform := PlatformGitLab
	hostname := host
	if i := strings.LastIndex(hostname, ":"); i >= 0 {
		hostname = hostname[:i]
	}
	if hostname == GitHubHost {
		platform = PlatformGitHub
	}

	// GitHub namespaces are always a single owner segment. A deeper path means
	// this is not a GitHub clone URL (e.g. a /tree/main web URL).
	if platform == PlatformGitHub && len(segments) != 2 {
		return Remote{}, ErrInvalidRemote
	}

	repo := segments[len(segments)-1]
	owner := strings.Join(segments[:len(segments)-1], "/")
	if owner == "" || repo == "" {
		return Remote{}, ErrInvalidRemote
	}

	return Remote{Platform: platform, Host: host, Owner: owner, Repo: repo}, nil
}

// splitRemote extracts the host and path from a supported remote form.
func splitRemote(raw string) (host, path string, err error) {
	// ssh:// or https:// form.
	if strings.Contains(raw, "://") {
		u, perr := url.Parse(raw)
		if perr != nil {
			return "", "", ErrUnsupportedRemote
		}
		switch strings.ToLower(u.Scheme) {
		case "https", "ssh":
		default:
			return "", "", ErrUnsupportedRemote
		}
		if u.Host == "" {
			return "", "", ErrInvalidRemote
		}
		return u.Host, u.Path, nil
	}

	// scp-like form: [user@]host:path
	if at := strings.Index(raw, "@"); at >= 0 {
		rest := raw[at+1:]
		colon := strings.Index(rest, ":")
		if colon <= 0 {
			return "", "", ErrUnsupportedRemote
		}
		host = rest[:colon]
		path = rest[colon+1:]
		if host == "" || path == "" {
			return "", "", ErrInvalidRemote
		}
		return host, path, nil
	}

	// Anything else (local path, bare host, unsupported scheme) is not a forge
	// remote we can use.
	return "", "", ErrUnsupportedRemote
}

// String implements fmt.Stringer for logging.
func (r Remote) String() string {
	return fmt.Sprintf("%s:%s/%s", r.Platform, r.Host, r.Slug())
}
