package version

import "testing"

func TestClientProtocol_IsParsedFromTheEmbeddedFile(t *testing.T) {
	// Zero would make every client look "unknown" and silently fall back to the
	// strict version comparison the number exists to replace.
	if ClientProtocol <= 0 {
		t.Fatalf("ClientProtocol = %d, want a positive integer", ClientProtocol)
	}
}

func TestParseClientProtocol(t *testing.T) {
	valid := []struct {
		raw  string
		want int
	}{
		{"1", 1},
		{"7", 7},
		{" 12 ", 12},
		{"42\n", 42},
	}
	for _, tc := range valid {
		if got := parseClientProtocol(tc.raw); got != tc.want {
			t.Errorf("parseClientProtocol(%q) = %d, want %d", tc.raw, got, tc.want)
		}
	}

	// Anything that is not a positive integer is a build-time mistake: a 0 or a
	// garbage value would disable the guard instead of failing loudly.
	for _, raw := range []string{"", "  ", "0", "-1", "v2", "1.5", "abc"} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("parseClientProtocol(%q) did not panic", raw)
				}
			}()
			parseClientProtocol(raw)
		}()
	}
}

func TestGetClientBuildVersion_FallsBackToServerVersion(t *testing.T) {
	origVersion, origClient := Version, ClientBuildVersion
	defer func() { Version, ClientBuildVersion = origVersion, origClient }()

	Version = "v0.130.0"
	ClientBuildVersion = ""
	if got := GetClientBuildVersion(); got != "v0.130.0" {
		t.Errorf("GetClientBuildVersion() = %q, want the server version", got)
	}
}

func TestGetClientBuildVersion_UsesInjectedVersion(t *testing.T) {
	// A release that reused the previous client build must report THAT build's
	// version, because the installer keys the side-by-side directory off it.
	origVersion, origClient := Version, ClientBuildVersion
	defer func() { Version, ClientBuildVersion = origVersion, origClient }()

	Version = "v0.130.0"
	ClientBuildVersion = "v0.129.0"
	if got := GetClientBuildVersion(); got != "v0.129.0" {
		t.Errorf("GetClientBuildVersion() = %q, want the injected client version", got)
	}
}
