package forge

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseRemoteURL_HTTPS(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		wantPlat  Platform
		wantHost  string
		wantOwner string
		wantRepo  string
		wantErr   bool
	}{
		{
			name:      "github https with .git",
			raw:       "https://github.com/acme/widgets.git",
			wantPlat:  PlatformGitHub,
			wantHost:  "github.com",
			wantOwner: "acme",
			wantRepo:  "widgets",
		},
		{
			name:      "github https without .git",
			raw:       "https://github.com/acme/widgets",
			wantPlat:  PlatformGitHub,
			wantHost:  "github.com",
			wantOwner: "acme",
			wantRepo:  "widgets",
		},
		{
			name:      "gitlab self-hosted with port",
			raw:       "https://git.acme.internal:8443/team/widgets.git",
			wantPlat:  PlatformGitLab,
			wantHost:  "git.acme.internal:8443",
			wantOwner: "team",
			wantRepo:  "widgets",
		},
		{
			name:      "gitlab multi-level group",
			raw:       "https://gitlab.com/group/subgroup/team/widgets.git",
			wantPlat:  PlatformGitLab,
			wantHost:  "gitlab.com",
			wantOwner: "group/subgroup/team",
			wantRepo:  "widgets",
		},
		{
			name:      "trailing slash stripped",
			raw:       "https://github.com/acme/widgets/",
			wantPlat:  PlatformGitHub,
			wantHost:  "github.com",
			wantOwner: "acme",
			wantRepo:  "widgets",
		},
		{
			name:      "case-insensitive host",
			raw:       "https://GitHub.com/acme/widgets.git",
			wantPlat:  PlatformGitHub,
			wantHost:  "github.com",
			wantOwner: "acme",
			wantRepo:  "widgets",
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
			name:    "unsupported http scheme rejected",
			raw:     "http://github.com/acme/widgets.git",
			wantErr: true,
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
			assert.Equal(t, tt.wantOwner, got.Owner)
			assert.Equal(t, tt.wantRepo, got.Repo)
		})
	}
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
