package version

import "testing"

func TestReleaseTag(t *testing.T) {
	cases := []struct {
		name    string
		version string
		want    string
	}{
		{"clean release tag", "v0.98.0", "v0.98.0"},
		{"release tag without v", "0.98.0", "v0.98.0"},
		// build.sh appends -MMDDHHMM for dev builds. The suffix is build
		// metadata, not part of the tag — this is still the v0.98.0 release.
		{"build-time suffix stripped", "v0.98.0-07291030", "v0.98.0"},
		// Commits after a tag have no release of their own.
		{"commits after tag", "v0.98.0-5-g830bb6c", ""},
		{"commits after tag + suffix", "v0.98.0-5-g830bb6c-07291030", ""},
		{"dev", "dev", ""},
		{"empty", "", ""},
		{"whitespace", "  v0.98.0  ", "v0.98.0"},
		{"vcs short hash", "830bb6c", ""},
		// A pre-release tag is not something the release workflow publishes.
		{"pre-release", "v1.0.0-rc1", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ReleaseTag(tc.version); got != tc.want {
				t.Errorf("ReleaseTag(%q) = %q, want %q", tc.version, got, tc.want)
			}
		})
	}
}

// TestReleaseTag_MatchesRealBuildShapes pins the two shapes build.sh actually
// produces on a tagged checkout, so a future change to the stamping format
// fails here rather than silently producing 404 download URLs.
func TestReleaseTag_MatchesRealBuildShapes(t *testing.T) {
	// build.sh on an exact tag: FULL_VERSION = VERSION (clean).
	if got := ReleaseTag("v0.98.0"); got != "v0.98.0" {
		t.Errorf("release build: ReleaseTag = %q, want %q", got, "v0.98.0")
	}
	// build.sh off-tag: FULL_VERSION = VERSION-MMDDHHMM where VERSION is a
	// git-describe like "v0.98.0-5-g830bb6c" — no release tag exists.
	if got := ReleaseTag("v0.98.0-5-g830bb6c-07291030"); got != "" {
		t.Errorf("dev build off tag: ReleaseTag = %q, want empty", got)
	}
	// build.sh off-tag but the describe is just the previous tag (no commits
	// since) cannot happen with --exact-match, but the suffixed clean tag can
	// appear in hand-made builds and must still resolve.
	if got := ReleaseTag("v0.98.0-07291030"); got != "v0.98.0" {
		t.Errorf("suffixed clean tag: ReleaseTag = %q, want %q", got, "v0.98.0")
	}
}
