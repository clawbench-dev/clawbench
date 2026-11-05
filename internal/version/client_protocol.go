package version

import (
	_ "embed"
	"fmt"
	"strconv"
	"strings"
)

// clientProtocolFile is the ONE source of truth for the shell<->server
// compatibility number. Every side reads this same file at build time:
//
//   - Go embeds it here and reports it from /api/health and /api/desktop/latest;
//   - the Android build bakes it into BuildConfig.CLIENT_PROTOCOL;
//   - the desktop shell mirrors it in desktop/src/shared/protocol.ts, with a
//     parity test that fails if the two drift.
//
// Bump it ONLY for a breaking change to the contract a shell depends on — the
// native bridge surface, the startup/upgrade endpoints, or the login page
// handshake. The web UI and the WebSocket event schema are NOT part of it: they
// are served by the server and are therefore always the same version as the
// server.
//
// While the number is unchanged, a release may ship the previous release's APK
// and desktop package untouched (see the client-guard job in release.yml) and
// the two gates keep accepting them regardless of the version string.
//
//go:embed client_protocol.txt
var clientProtocolFile string

// ClientProtocol is the parsed value of client_protocol.txt.
//
// A malformed file is a build-time mistake, so it panics instead of quietly
// reporting 0 — a 0 would make every client look "unknown" and silently fall
// back to the strict version comparison, which is exactly the behavior this
// number exists to replace.
var ClientProtocol = parseClientProtocol(clientProtocolFile)

func parseClientProtocol(raw string) int {
	trimmed := strings.TrimSpace(raw)
	n, err := strconv.Atoi(trimmed)
	if err != nil || n <= 0 {
		panic(fmt.Sprintf("internal/version/client_protocol.txt must contain a positive integer, got %q", raw))
	}
	return n
}

// ClientBuildVersion is the version of the APK/desktop package this server
// ships, when it differs from the server's own version.
//
// A release that did not touch the clients reuses the previous release's
// artifacts (see release.yml's client-guard job). Those binaries still carry
// their ORIGINAL version, and the desktop installer keys the side-by-side
// install directory off this value — installing a v0.129 app under app-0.130
// makes app.getVersion() disagree with the pointer, and the startup handoff
// then DELETES the pointer, silently reverting the upgrade.
//
// Injected with -ldflags "-X ...ClientBuildVersion=<tag>". Empty means "the
// client was built for this release", i.e. same as Get().
var ClientBuildVersion = ""

// GetClientBuildVersion returns the version of the client build this server
// serves, falling back to the server version for ordinary (rebuilt) releases.
func GetClientBuildVersion() string {
	if ClientBuildVersion != "" {
		return ClientBuildVersion
	}
	return Get()
}
