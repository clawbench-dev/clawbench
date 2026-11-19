package source

import (
	"os"
	"path/filepath"
	"testing"
)

func withSeams(t *testing.T, region bool, env map[string]string, npmrc string) {
	t.Helper()
	prevRegion, prevGetenv, prevHome := regionProbe, getenv, userHomeDir
	t.Cleanup(func() {
		regionProbe, getenv, userHomeDir = prevRegion, prevGetenv, prevHome
	})
	regionProbe = func() bool { return region }
	getenv = func(key string) string { return env[key] }
	dir := t.TempDir()
	if npmrc != "" {
		if err := os.WriteFile(filepath.Join(dir, ".npmrc"), []byte(npmrc), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	userHomeDir = func() string { return dir }
}

func bases(in []Candidate) []string {
	out := make([]string, len(in))
	for i, c := range in {
		out[i] = c.Base
	}
	return out
}

func TestNpmOrder(t *testing.T) {
	t.Run("china", func(t *testing.T) {
		withSeams(t, true, nil, "")
		got := bases(Npm())
		want := []string{NpmMirrorBase, NpmJsBase}
		assertStrings(t, got, want)
	})
	t.Run("abroad", func(t *testing.T) {
		withSeams(t, false, nil, "")
		got := bases(Npm())
		want := []string{NpmJsBase, NpmMirrorBase}
		assertStrings(t, got, want)
	})
}

func TestNpmUserFirst(t *testing.T) {
	const private = "https://npm.corp.example/repository/npm"
	withSeams(t, true, map[string]string{"NPM_CONFIG_REGISTRY": private}, "")
	got := Npm()
	if len(got) != 3 || got[0].Base != private || got[0].Name != "user" {
		t.Fatalf("user source must be first, got %#v", got)
	}
	assertStrings(t, bases(got), []string{private, NpmMirrorBase, NpmJsBase})
}

func TestNpmDeduplicatesUserMirror(t *testing.T) {
	withSeams(t, true, map[string]string{"NPM_CONFIG_REGISTRY": NpmMirrorBase + "/"}, "")
	got := bases(Npm())
	assertStrings(t, got, []string{NpmMirrorBase, NpmJsBase})
}

func TestUserNpmRegistryFromNpmrc(t *testing.T) {
	withSeams(t, false, nil, "# comment\nregistry=https://registry.example.com/npm/\n")
	if got := UserNpmRegistry(); got != "https://registry.example.com/npm" {
		t.Fatalf("UserNpmRegistry() = %q", got)
	}
}

func TestNormalizeRegistryBase(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"https", "https://registry.example.com", "https://registry.example.com"},
		{"trailing slash", "https://registry.example.com/", "https://registry.example.com"},
		{"path prefix", "https://registry.example.com/npm/", "https://registry.example.com/npm"},
		{"loopback http", "http://localhost:4873", "http://localhost:4873"},
		{"loopback ipv4 http", "http://127.0.0.1:4873", "http://127.0.0.1:4873"},
		{"loopback ipv6 http", "http://[::1]:4873", "http://[::1]:4873"},
		{"public http rejected", "http://registry.example.com", ""},
		{"ftp rejected", "ftp://registry.example.com", ""},
		{"no host", "https://", ""},
		{"credentials rejected", "https://user:pass@registry.example.com", ""},
		{"empty", "  ", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeRegistryBase(tc.in); got != tc.want {
				t.Fatalf("normalizeRegistryBase(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestReleaseURLsOrder(t *testing.T) {
	const (
		repo  = "acme/app"
		tag   = "v1.2.3"
		asset = "app-linux-x64-v1.2.3.zip"
	)
	direct := "https://github.com/acme/app/releases/download/v1.2.3/app-linux-x64-v1.2.3.zip"

	t.Run("china", func(t *testing.T) {
		withSeams(t, true, nil, "")
		got := ReleaseURLs(repo, tag, asset)
		want := []string{githubProxyPrefix + direct, githubFastPrefix + direct, direct}
		assertStrings(t, got, want)
	})
	t.Run("abroad", func(t *testing.T) {
		withSeams(t, false, nil, "")
		got := ReleaseURLs(repo, tag, asset)
		want := []string{direct, githubProxyPrefix + direct, githubFastPrefix + direct}
		assertStrings(t, got, want)
	})
}

func TestCandidateURL(t *testing.T) {
	c := Candidate{Base: "https://registry.example.com/npm/"}
	if got := c.URL("/@scope/pkg/-/pkg-1.0.0.tgz"); got != "https://registry.example.com/npm/@scope/pkg/-/pkg-1.0.0.tgz" {
		t.Fatalf("URL() = %q", got)
	}
}

func TestRegionUsesProbe(t *testing.T) {
	withSeams(t, true, nil, "")
	if !Region() {
		t.Fatal("Region() must follow regionProbe")
	}
}

func assertStrings(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}
