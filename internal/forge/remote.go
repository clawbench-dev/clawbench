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

// API schemes. Self-hosted instances are commonly served over plain http on an
// internal network, so http is a first-class choice rather than an error.
const (
	SchemeHTTP  = "http"
	SchemeHTTPS = "https"
	// DefaultScheme is assumed when neither the binding nor the instance hint
	// says otherwise. It matches what every platform-operated host uses, so an
	// unknown host behaves exactly as it did before schemes were tracked.
	DefaultScheme = SchemeHTTPS
)

// NormalizeScheme clamps a scheme to a supported value, returning "" for
// anything unrecognized (including ""). Callers treat "" as "not specified"
// rather than as https, so a hint is never invented where none was given.
func NormalizeScheme(scheme string) string {
	switch strings.ToLower(strings.TrimSpace(scheme)) {
	case SchemeHTTP:
		return SchemeHTTP
	case SchemeHTTPS:
		return SchemeHTTPS
	default:
		return ""
	}
}

// ResolveScheme picks the scheme to reach a host with. Precedence is the
// binding's own scheme, then the instance-level hint, then https.
//
// The hint exists for the case the binding cannot answer: an ssh or scp remote
// ("git@gitlab.internal:team/repo.git") says nothing about the HTTP API scheme,
// so an http-only instance would be probed over https and fail with an opaque
// TLS error. The credential the user configured for that host is the one place
// the scheme is known, and it is recorded there as the hint.
func ResolveScheme(binding, hint string) string {
	if s := NormalizeScheme(binding); s != "" {
		return s
	}
	if s := NormalizeScheme(hint); s != "" {
		return s
	}
	return DefaultScheme
}

// Remote is a parsed git remote pointing at a forge repository.
type Remote struct {
	Platform Platform
	// Host is the lowercased host, including a port when the remote specifies
	// one (e.g. "git.acme.internal:8443"). Used as the credential scope key.
	Host string
	// Scheme is the API scheme to reach Host with: "http" or "https", or "" when
	// the remote does not say. It is deliberately separate from Host because
	// Host is an identity — it keys credentials, snapshots, rate limits and read
	// state — whereas the scheme is a property of how one deployment happens to
	// be exposed. Folding the scheme into Host would change every one of those
	// keys, and would let "http://h" and "https://h" become two different
	// repositories when they are the same one.
	//
	// Note this is NOT the git transport: an "ssh://" remote yields "", because
	// speaking ssh to a host says nothing about whether its API is http or
	// https. See ResolveScheme for how "" is resolved.
	Scheme string
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
	return StripPort(r.Host) == GitHubHost || StripPort(r.Host) == GitLabHost
}

// StripPort removes a trailing ":port" from a host, leaving the hostname. A
// host without a port is returned unchanged.
//
// This is what makes "github.com:443" recognizable as github.com. Note the
// deliberate contrast with isOfficialForgeHost in the handler package, which
// matches the host exactly and therefore treats "github.com:8443" as unofficial:
// the question there is whether to trust the host's endpoints, and a port we
// did not choose is not the platform's.
func StripPort(host string) string {
	if i := strings.LastIndex(host, ":"); i >= 0 {
		return host[:i]
	}
	return host
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
// "http://" is accepted alongside "https://" so a self-hosted instance on an
// internal network can be bound; the parsed Remote carries the scheme so the
// API is reached the same way the remote describes. The ssh and scp forms carry
// no API scheme (see splitRemote), which leaves Remote.Scheme empty for the
// resolver to fill in.
//
// Host is classified as GitHub only when it is github.com; every other host is
// treated as GitLab (self-hosted GitLab is a first-class target). Any host is
// accepted here — including private-network addresses and internal names that
// do not resolve. The UI warns before binding a non-official host, since
// binding sends that host the stored credential.
func ParseRemoteURL(raw string) (Remote, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Remote{}, ErrUnsupportedRemote
	}

	host, path, scheme, err := splitRemote(raw)
	if err != nil {
		return Remote{}, err
	}

	host = strings.ToLower(host)
	// Reject a scheme-less scp host that still carries credentials or is empty.
	if host == "" || strings.Contains(host, "@") {
		return Remote{}, ErrInvalidRemote
	}

	segments, err := remotePathSegments(path)
	if err != nil {
		return Remote{}, err
	}

	platform := PlatformForHost(host)

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

	return Remote{Platform: platform, Host: host, Scheme: scheme, Owner: owner, Repo: repo}, nil
}

// remotePathSegments splits and validates a remote's path into namespace/repo
// segments. It rejects empty segments, GitLab's web-UI "-" separator, and paths
// too shallow to identify a repository.
func remotePathSegments(path string) ([]string, error) {
	path = strings.Trim(path, "/")
	path = strings.TrimSuffix(path, ".git")
	if path == "" {
		return nil, ErrInvalidRemote
	}

	segments := strings.Split(path, "/")
	for _, seg := range segments {
		if seg == "" {
			return nil, ErrInvalidRemote
		}
		// GitLab reserves "-" as the web-UI path separator; it can never be a
		// namespace or repository name, so its presence means this is a web URL
		// (e.g. /group/repo/-/tree/main) rather than a clone URL.
		if seg == "-" {
			return nil, ErrInvalidRemote
		}
	}
	if len(segments) < 2 {
		return nil, ErrInvalidRemote
	}
	return segments, nil
}

// PlatformForHost maps a host to its forge platform. The port is stripped
// before comparison so "github.com:443" is still recognized.
//
// Exported so the credential flow can pick the right verifier for a bare host,
// where there is no remote URL to parse.
func PlatformForHost(host string) Platform {
	if StripPort(host) == GitHubHost {
		return PlatformGitHub
	}
	return PlatformGitLab
}

// splitRemote extracts the host, path and API scheme from a supported remote
// form.
//
// The returned scheme is the HTTP API scheme, or "" when the remote does not
// imply one. Only the explicit "http://" / "https://" forms imply it: an
// "ssh://" or scp-like remote describes the git transport, which is unrelated
// to whether the instance's API is served over TLS.
func splitRemote(raw string) (host, path, scheme string, err error) {
	// ssh:// or http(s):// form.
	if strings.Contains(raw, "://") {
		u, perr := url.Parse(raw)
		if perr != nil {
			return "", "", "", ErrUnsupportedRemote
		}
		apiScheme := ""
		switch strings.ToLower(u.Scheme) {
		case "https":
			apiScheme = SchemeHTTPS
		case "http":
			// Plain http is accepted so a self-hosted instance on an internal
			// network can be bound. It is the user's call: the credential is
			// sent to a host they named explicitly.
			apiScheme = SchemeHTTP
		case "ssh":
			// The git transport; says nothing about the API scheme.
		default:
			return "", "", "", ErrUnsupportedRemote
		}
		if u.Host == "" {
			return "", "", "", ErrInvalidRemote
		}
		return u.Host, u.Path, apiScheme, nil
	}

	// scp-like form: [user@]host:path
	if at := strings.Index(raw, "@"); at >= 0 {
		rest := raw[at+1:]
		h, p, found := strings.Cut(rest, ":")
		if !found || h == "" {
			return "", "", "", ErrUnsupportedRemote
		}
		host = h
		path = p
		if host == "" || path == "" {
			return "", "", "", ErrInvalidRemote
		}
		// No API scheme: scp syntax carries only the git transport.
		return host, path, "", nil
	}

	// Anything else (local path, bare host, unsupported scheme) is not a forge
	// remote we can use.
	return "", "", "", ErrUnsupportedRemote
}

// String implements fmt.Stringer for logging.
func (r Remote) String() string {
	return fmt.Sprintf("%s:%s/%s", r.Platform, r.Host, r.Slug())
}
