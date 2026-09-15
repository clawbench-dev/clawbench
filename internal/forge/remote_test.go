package forge

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseRemoteURL_HTTPS(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		wantPlat   Platform
		wantHost   string
		wantScheme string
		wantOwner  string
		wantRepo   string
		wantErr    bool
	}{
		{
			name:       "github https with .git",
			raw:        "https://github.com/acme/widgets.git",
			wantPlat:   PlatformGitHub,
			wantScheme: SchemeHTTPS,
			wantHost:   "github.com",
			wantOwner:  "acme",
			wantRepo:   "widgets",
		},
		{
			name:       "github https without .git",
			raw:        "https://github.com/acme/widgets",
			wantPlat:   PlatformGitHub,
			wantScheme: SchemeHTTPS,
			wantHost:   "github.com",
			wantOwner:  "acme",
			wantRepo:   "widgets",
		},
		{
			name:       "gitlab self-hosted with port",
			raw:        "https://git.acme.internal:8443/team/widgets.git",
			wantPlat:   PlatformGitLab,
			wantScheme: SchemeHTTPS,
			wantHost:   "git.acme.internal:8443",
			wantOwner:  "team",
			wantRepo:   "widgets",
		},
		{
			name:       "gitlab multi-level group",
			raw:        "https://gitlab.com/group/subgroup/team/widgets.git",
			wantPlat:   PlatformGitLab,
			wantScheme: SchemeHTTPS,
			wantHost:   "gitlab.com",
			wantOwner:  "group/subgroup/team",
			wantRepo:   "widgets",
		},
		{
			name:       "trailing slash stripped",
			raw:        "https://github.com/acme/widgets/",
			wantPlat:   PlatformGitHub,
			wantScheme: SchemeHTTPS,
			wantHost:   "github.com",
			wantOwner:  "acme",
			wantRepo:   "widgets",
		},
		{
			name:       "case-insensitive host",
			raw:        "https://GitHub.com/acme/widgets.git",
			wantPlat:   PlatformGitHub,
			wantScheme: SchemeHTTPS,
			wantHost:   "github.com",
			wantOwner:  "acme",
			wantRepo:   "widgets",
		},
		{
			name:    "github with too many segments rejected",
			raw:     "https://github.com/a/b/c.git",
			wantErr: true,
		},
		{
			name:    "github web url rejected",
			raw:     "https://github.com/acme/widgets/tree/main",
			wantErr: true,
		},
		{
			name:    "gitlab web url with dash rejected",
			raw:     "https://gitlab.com/group/widgets/-/tree/main",
			wantErr: true,
		},
		{
			name:    "missing repo rejected",
			raw:     "https://github.com/acme",
			wantErr: true,
		},
		{
			name:    "empty rejected",
			raw:     "",
			wantErr: true,
		},
		{
			name:    "host with credentials rejected",
			raw:     "https://user:pass@/acme/widgets.git",
			wantErr: true,
		},
		{
			name:    "empty path segment rejected",
			raw:     "https://github.com/acme//widgets.git",
			wantErr: true,
		},
		{
			name:       "plain http accepted for self-hosted instance",
			raw:        "http://gitlab.internal:8080/team/widgets.git",
			wantPlat:   PlatformGitLab,
			wantHost:   "gitlab.internal:8080",
			wantScheme: SchemeHTTP,
			wantOwner:  "team",
			wantRepo:   "widgets",
		},
		{
			name:    "bare host rejected",
			raw:     "github.com",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseRemoteURL(tt.raw)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantPlat, got.Platform)
			assert.Equal(t, tt.wantHost, got.Host)
			assert.Equal(t, tt.wantScheme, got.Scheme,
				"the remote's own scheme must survive parsing so the API is reached the same way")
			assert.Equal(t, tt.wantOwner, got.Owner)
			assert.Equal(t, tt.wantRepo, got.Repo)
		})
	}
}

// TestParseRemoteURL_SchemeSeparateFromHost pins the invariant that makes
// schemes safe to add: the scheme never leaks into Host. Host keys credentials,
// snapshots and read state, so "http://h" and "https://h" must resolve to ONE
// repository, not two.
func TestParseRemoteURL_SchemeSeparateFromHost(t *testing.T) {
	httpRemote, err := ParseRemoteURL("http://gitlab.internal/team/widgets.git")
	require.NoError(t, err)
	httpsRemote, err := ParseRemoteURL("https://gitlab.internal/team/widgets.git")
	require.NoError(t, err)

	assert.Equal(t, httpRemote.Host, httpsRemote.Host,
		"the same instance over either scheme must share one host key")
	assert.Equal(t, httpRemote.Slug(), httpsRemote.Slug())
	assert.Equal(t, SchemeHTTP, httpRemote.Scheme)
	assert.Equal(t, SchemeHTTPS, httpsRemote.Scheme)
}

// TestResolveScheme pins the precedence: a binding's own scheme wins over the
// instance hint, and https is the fallback when neither says.
func TestResolveScheme(t *testing.T) {
	assert.Equal(t, SchemeHTTP, ResolveScheme(SchemeHTTP, SchemeHTTPS),
		"the binding's scheme must win over the instance hint")
	assert.Equal(t, SchemeHTTP, ResolveScheme("", SchemeHTTP),
		"an unset binding scheme falls back to the instance hint")
	assert.Equal(t, SchemeHTTPS, ResolveScheme("", ""),
		"an unknown scheme defaults to https, matching every platform-operated host")
	assert.Equal(t, SchemeHTTPS, ResolveScheme("bogus", "bogus"),
		"unrecognized values are treated as unspecified, not passed through")
}

// TestNormalizeScheme pins that an empty input yields empty rather than https,
// so "not specified" is never silently recorded as a real choice.
func TestNormalizeScheme(t *testing.T) {
	assert.Equal(t, "", NormalizeScheme(""))
	assert.Equal(t, "", NormalizeScheme("   "))
	assert.Equal(t, "", NormalizeScheme("ftp"))
	assert.Equal(t, SchemeHTTP, NormalizeScheme("HTTP"))
	assert.Equal(t, SchemeHTTPS, NormalizeScheme(" https "))
}

func TestParseRemoteURL_SSH(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		wantPlat  Platform
		wantHost  string
		wantOwner string
		wantRepo  string
	}{
		{
			name:      "github ssh scp form",
			raw:       "git@github.com:acme/widgets.git",
			wantPlat:  PlatformGitHub,
			wantHost:  "github.com",
			wantOwner: "acme",
			wantRepo:  "widgets",
		},
		{
			name:      "gitlab ssh with subgroup",
			raw:       "git@gitlab.com:group/sub/team/widgets.git",
			wantPlat:  PlatformGitLab,
			wantHost:  "gitlab.com",
			wantOwner: "group/sub/team",
			wantRepo:  "widgets",
		},
		{
			name:      "ssh url scheme form",
			raw:       "ssh://git@git.acme.internal:2222/team/widgets.git",
			wantPlat:  PlatformGitLab,
			wantHost:  "git.acme.internal:2222",
			wantOwner: "team",
			wantRepo:  "widgets",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseRemoteURL(tt.raw)
			require.NoError(t, err)
			assert.Equal(t, tt.wantPlat, got.Platform)
			assert.Equal(t, tt.wantHost, got.Host)
			assert.Empty(t, got.Scheme,
				"ssh describes the git transport, so it must not be mistaken for the API scheme")
			assert.Equal(t, tt.wantOwner, got.Owner)
			assert.Equal(t, tt.wantRepo, got.Repo)
		})
	}
}

func TestRemoteHelpers(t *testing.T) {
	r := Remote{Platform: PlatformGitLab, Host: "gitlab.com", Owner: "group/sub", Repo: "widgets"}
	assert.Equal(t, "group/sub/widgets", r.Slug())
	assert.True(t, r.IsOfficialHost())
	assert.Contains(t, r.String(), "gitlab:gitlab.com/group/sub/widgets")

	selfHosted := Remote{Platform: PlatformGitLab, Host: "git.acme.io:8443", Owner: "t", Repo: "w"}
	assert.False(t, selfHosted.IsOfficialHost(), "self-hosted host must not count as official")
}

func TestParseRemoteURL_NonForgeRejected(t *testing.T) {
	// A local path or a non-forge host must not be silently accepted.
	for _, raw := range []string{
		"/home/user/repo",
		"../relative/repo",
		"file:///home/user/repo",
		"git://example.com/a/b.git",
	} {
		t.Run(raw, func(t *testing.T) {
			_, err := ParseRemoteURL(raw)
			assert.Error(t, err, "should reject non-forge remote: %s", raw)
		})
	}
}
