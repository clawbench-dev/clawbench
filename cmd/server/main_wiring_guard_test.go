package main

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file is a source guard for a load-bearing ORDER inside main().
//
// startPlugins() iterates plugins.All(), and RegisterRoutes() asks
// plugins.ForRoute which feature owns each prefix. If registerPlugins() runs
// after either of them, the registry is still empty at that moment:
//
//   - no worker starts (mail stops syncing, forge stops polling, the terminal
//     manager is never created), and
//   - no route is gated.
//
// Nothing fails — the server boots cleanly and simply does less. That was a real
// bug in the first version of this refactor, and no unit test caught it because
// tests call these three functions in their own order (register, then start),
// which is exactly the order the bug violated.
//
// Hence a text guard: it is the only way to pin "these lines stay in this order
// inside main()" without executing main.

// mainGoSource reads cmd/server/main.go (tests run with this package's directory
// as their working directory).
func mainGoSource(t *testing.T) string {
	t.Helper()
	src, err := os.ReadFile("main.go")
	require.NoError(t, err, "read main.go")
	return string(src)
}

// callOffset returns the byte offset of a CALL SITE.
//
// The search strings include the argument list on purpose: a declaration such as
// `func startPlugins(cfg model.Config)` must not match, or the offset would come
// from the definition at the bottom of the file and the ordering assertion would
// pass no matter where the call sits.
func callOffset(t *testing.T, code, call string) int {
	t.Helper()
	idx := strings.Index(code, call)
	require.GreaterOrEqualf(t, idx, 0,
		"%q not found in main.go — the call moved or was renamed; update this guard to match", call)
	return idx
}

func TestMainWiring_PluginsRegisteredBeforeStartAndRoutes(t *testing.T) {
	code := mainGoSource(t)

	registerAt := callOffset(t, code, "registerPlugins(port)")
	startAt := callOffset(t, code, "startPlugins(cfg)")
	routeAt := callOffset(t, code, "handler.RegisterRoutes(mux)")

	assert.Less(t, registerAt, startAt,
		"registerPlugins(port) must run BEFORE startPlugins(cfg): startPlugins iterates plugins.All(), so registering later means no worker ever starts")
	assert.Less(t, registerAt, routeAt,
		"registerPlugins(port) must run BEFORE handler.RegisterRoutes(mux): the route table asks plugins.ForRoute which feature owns each prefix, so registering later leaves every route ungated")
}

// TestMainWiring_StopIsDeferredRightAfterStart pins the other half: the stop
// hook has to be deferred at startup time, next to startPlugins. A `defer
// stopPlugins()` that only runs in some branch would leave PTYs and pollers alive
// on the paths that skip it.
func TestMainWiring_StopIsDeferredRightAfterStart(t *testing.T) {
	code := mainGoSource(t)

	startAt := callOffset(t, code, "startPlugins(cfg)")
	stopAt := callOffset(t, code, "defer stopPlugins()")

	assert.Greater(t, stopAt, startAt, "the stop hook must be registered after the start loop")
	assert.Less(t, stopAt-startAt, 64,
		"defer stopPlugins() must sit right next to startPlugins(cfg) in main(), not somewhere later in the function")
}

// TestMainWiring_TTSProviderBuiltByTheSharedConstructor keeps the startup path
// on newTTSProvider.
//
// main() used to carry its own switch over cfg.TTS.Engine. It knew "edge" and
// "openai" only, so every other engine was silently rebuilt as Edge on the next
// restart while config.yaml still named it — the settings panel looked fine, the
// hot-reload path (which uses newTTSProvider) honored the change, and only a
// restart exposed the mismatch. Two switches means one of them goes stale; this
// guard fails the moment a second one grows back.
func TestMainWiring_TTSProviderBuiltByTheSharedConstructor(t *testing.T) {
	code := mainGoSource(t)

	callOffset(t, code, "ttsProvider := newTTSProvider(cfg)")

	// Only main()'s body is searched: the provider constructors at the bottom
	// of the file legitimately contain `p := speech.NewEdgeTTSProvider()`.
	body := mainFuncBody(t, code)

	// The old copy was recognizable by building a provider inline inside main().
	for _, literal := range []string{
		"speech.NewEdgeTTSProvider()",
		"speech.NewOpenAITTSProvider()",
		"newOpenAITTSProvider(cfg)",
		"switch engine {",
	} {
		assert.NotContains(t, body, literal,
			"main() builds a TTS provider inline (%q) — call newTTSProvider(cfg) instead, or an engine it does not know about silently becomes Edge on restart", literal)
	}
}

// mainFuncBody returns the text of func main() only, so guards can assert about
// the startup path without matching the same constructs in helper functions.
func mainFuncBody(t *testing.T, code string) string {
	t.Helper()
	start := callOffset(t, code, "func main() {")
	rest := code[start:]
	// main() is the first top-level func in the file's ordering here; the next
	// one starts at a line beginning with "func ".
	if end := strings.Index(rest[1:], "\nfunc "); end >= 0 {
		return rest[:end+1]
	}
	return rest
}
