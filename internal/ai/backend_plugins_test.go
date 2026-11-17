package ai_test

// This file exists for two reasons.
//
// 1. Import side effect: importing the backend plugins runs their init(), which
//    registers each backend's ProviderEnv spec (and other per-backend facts)
//    into the model registries. The tests in this directory live in `package
//    ai` and exercise provider-env injection and provider model addressing,
//    which read those registries. An internal test package cannot import
//    backends/all (it would close the ai → backends → ai cycle), so the import
//    lives in the external test package, which is compiled into the same binary
//    and initialized before any test runs.
//
// 2. A guard that the core package stays free of backend-specific files — the
//    whole point of the backend plugin split.

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/ai/backends"
	_ "clawbench/internal/ai/backends/all"
	"clawbench/internal/model"
)

// backendSpecificFilesAllowedInCore are the exceptions that must stay: each one
// is documented at its call site, and each would need a structural change
// rather than a file move to leave.
//
//	acp_meta_codebuddy.go      — core's cancelled-turn usage accumulation test
//	                             uses its parse result as a fixture
//	acp_opencode_children.go   — hangs off ClawBenchACPClient's private map
//	                             (client logic, not event mapping)
//	opencode_error_types.go    — one table shared by the ACP error classifier
//	                             and the CLI parser
var backendSpecificFilesAllowedInCore = map[string]bool{
	"acp_meta_codebuddy.go":    true,
	"acp_opencode_children.go": true,
	"opencode_error_types.go":  true,
}

// TestCorePackageHasNoBackendSpecificFiles fails when a file in internal/ai is
// named after a backend. Backend behaviour belongs in
// internal/ai/backends/<id>/; a new file here means the plugin boundary was
// bypassed, and the next person adding a backend has to edit the core package.
//
// Ids shorter than 3 characters are skipped: "pi" would otherwise match any
// future file whose name happens to contain it.
func TestCorePackageHasNoBackendSpecificFiles(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read core package dir: %v", err)
	}

	ids := make([]string, 0, len(backends.All()))
	for _, p := range backends.All() {
		if len(p.ID) >= 3 {
			ids = append(ids, p.ID)
		}
	}
	if len(ids) == 0 {
		t.Fatal("no backends registered — the guard would be vacuous")
	}

	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		if backendSpecificFilesAllowedInCore[name] {
			continue
		}
		for _, id := range ids {
			if name == id+".go" ||
				strings.HasPrefix(name, id+"_") ||
				strings.Contains(name, "_"+id+"_") ||
				strings.Contains(name, "_"+id+".") {
				t.Errorf("%s is named after backend %q; move it into internal/ai/backends/%s/ "+
					"and reach it through ACPBehavior or the BackendPlugin fields. "+
					"If it genuinely cannot move, add it to backendSpecificFilesAllowedInCore "+
					"with the reason.", name, id, id)
			}
		}
	}
}

// TestRealBackendSpecsCarryCodeBuddyCapabilityFlags is the guard that the
// spec-driven capability flags had been missing.
//
// internal/ai cannot import backends/all (import cycle), so
// acp_conn_lifecycle_test.go drives advertiseTerminalCapability /
// advertiseReadTextFileCapability through a SYNTHETIC spec installed with
// model.SetBackendRegistryForTest. Those tests prove the wiring reads the spec,
// but they would stay green if the real CodeBuddy spec flipped both flags back
// to false — the user-visible regressions (TaskOutput "not found", images read
// as mojibake) would return with every test passing. The assertion has to live
// in this package, which sees the real registry.
func TestRealBackendSpecsCarryCodeBuddyCapabilityFlags(t *testing.T) {
	spec := model.FindSpecByBackend("codebuddy")
	require.NotNil(t, spec, "the real backend registry must contain codebuddy")
	assert.True(t, spec.HideTerminalCapability,
		"CodeBuddy must hide the ACP Terminal capability: host-created terminals never enter its background-task registry, so every TaskOutput fails")
	assert.True(t, spec.HideReadTextFileCapability,
		"CodeBuddy must hide fs.readTextFile: advertising it replaces its native Read tool with a text-only proxy that turns images into mojibake")
}

// TestRealBackendSpecsNeedNoCapabilityHiding pins the other direction: hiding
// these capabilities is a CodeBuddy-specific workaround, so an agent that does
// not swap its tools on them must keep both advertised.
func TestRealBackendSpecsNeedNoCapabilityHiding(t *testing.T) {
	for _, backend := range []string{"claude", "codex", "opencode", "qoder", "hermes", "openclaw"} {
		spec := model.FindSpecByBackend(backend)
		require.NotNil(t, spec, "missing spec for %s", backend)
		assert.False(t, spec.HideTerminalCapability, "%s must keep the Terminal capability", backend)
		assert.False(t, spec.HideReadTextFileCapability, "%s must keep fs.readTextFile", backend)
	}

	// openclaw has its own tool-call parsers but no _meta parser, while
	// hermes declares no behavior at all. In both cases the generic _meta scan
	// must apply — inheriting Claude's parser would read the wrong wire shape.
	var inherited []string
	for _, id := range []string{"openclaw", "hermes"} {
		if behavior := backends.LookupACPBehavior(id); behavior != nil && behavior.ExtractMeta != nil {
			inherited = append(inherited, id)
		}
	}
	assert.Empty(t, inherited,
		"these agents do not share the claude ACP bridge; they must rely on the generic _meta scan, not a copied Claude parser")
}
