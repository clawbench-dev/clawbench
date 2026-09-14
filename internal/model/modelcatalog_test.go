package model

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// isolateModelSources empties the registry for the duration of a test and
// returns a restore function. Clearing the registry without restoring it would
// break every other test in the package, since backend init() runs only once.
func isolateModelSources(t *testing.T) func() {
	t.Helper()
	snapshot := snapshotModelSources()
	resetModelSources()
	return func() { restoreModelSources(snapshot) }
}

// ---------------------------------------------------------------------------
// ResolveModels — the single merge point for CLI-discovered and ACP-reported
// model lists. ACP membership is authoritative (the agent knows what it can
// actually run); the CLI list supplies stable display order and names.
// ---------------------------------------------------------------------------

func TestResolveModels_BothEmpty(t *testing.T) {
	assert.Nil(t, ResolveModels(nil, nil, ""))
	assert.Nil(t, ResolveModels([]AgentModel{}, []AgentModel{}, ""))
}

func TestResolveModels_CLIOnly(t *testing.T) {
	cli := []AgentModel{
		{ID: "claude-sonnet-4-6", Name: "Claude Sonnet 4.6", Default: true},
		{ID: "claude-opus-4-5", Name: "Claude Opus 4.5"},
	}
	got := ResolveModels(cli, nil, "")

	require.Len(t, got, 2)
	assert.Equal(t, "claude-sonnet-4-6", got[0].ID)
	assert.Equal(t, "Claude Sonnet 4.6", got[0].Name)
	assert.True(t, got[0].Default, "CLI default flag must be preserved when ACP reports nothing")
	assert.False(t, got[1].Default)
}

func TestResolveModels_ACPOnly(t *testing.T) {
	acp := []AgentModel{
		{ID: "glm-5.3", Name: "GLM 5.3"},
		{ID: "glm-5.2", Name: "GLM 5.2"},
	}
	got := ResolveModels(nil, acp, "")

	require.Len(t, got, 2)
	assert.Equal(t, "glm-5.3", got[0].ID)
	assert.True(t, got[0].Default, "first ACP model becomes default when nothing else marks one")
}

func TestResolveModels_ACPMembershipWins(t *testing.T) {
	// The agent reports it can only run opus; the CLI list is stale and
	// includes sonnet. Sonnet must NOT be offered.
	cli := []AgentModel{
		{ID: "claude-sonnet-4-6", Name: "Claude Sonnet 4.6", Default: true},
		{ID: "claude-opus-4-5", Name: "Claude Opus 4.5"},
	}
	acp := []AgentModel{{ID: "claude-opus-4-5", Name: "Opus (via env redirect)"}}

	got := ResolveModels(cli, acp, "")

	require.Len(t, got, 1)
	assert.Equal(t, "claude-opus-4-5", got[0].ID)
	assert.Equal(t, "Opus (via env redirect)", got[0].Name, "ACP display name wins for a matching ID")
	assert.True(t, got[0].Default)
}

func TestResolveModels_ACPOnlyModelsAppended(t *testing.T) {
	cli := []AgentModel{
		{ID: "claude-sonnet-4-6", Name: "Claude Sonnet 4.6", Default: true},
		{ID: "claude-opus-4-5", Name: "Claude Opus 4.5"},
	}
	acp := []AgentModel{
		{ID: "claude-opus-4-5", Name: "Opus"},
		{ID: "kimi-k3", Name: "Kimi K3"}, // runtime-only, not in CLI list
	}

	got := ResolveModels(cli, acp, "")

	require.Len(t, got, 2, "sonnet is dropped, opus kept, kimi appended")
	assert.Equal(t, "claude-opus-4-5", got[0].ID)
	assert.Equal(t, "kimi-k3", got[1].ID)
	assert.True(t, got[0].Default, "CLI default flag survives the ACP filter")
}

func TestResolveModels_CurrentIDWinsOverCLIDefault(t *testing.T) {
	cli := []AgentModel{
		{ID: "a", Name: "A", Default: true},
		{ID: "b", Name: "B"},
	}
	got := ResolveModels(cli, nil, "b")

	require.Len(t, got, 2)
	assert.False(t, got[0].Default)
	assert.True(t, got[1].Default, "session's current model overrides the CLI default")
}

func TestResolveModels_CurrentIDNotInListIsIgnored(t *testing.T) {
	cli := []AgentModel{
		{ID: "a", Name: "A", Default: true},
		{ID: "b", Name: "B"},
	}
	got := ResolveModels(cli, nil, "ghost")

	require.Len(t, got, 2)
	assert.True(t, got[0].Default, "unknown current ID must not clear the default entirely")
}

func TestResolveModels_DuplicateIDsAreCollapsed(t *testing.T) {
	// Duplicates within the CLI list.
	cliDup := ResolveModels([]AgentModel{
		{ID: "a", Name: "A", Default: true},
		{ID: "a", Name: "A dup"},
	}, nil, "")
	require.Len(t, cliDup, 1)
	assert.Equal(t, "a", cliDup[0].ID)

	// Duplicates within the ACP list.
	acpDup := ResolveModels(nil, []AgentModel{
		{ID: "b", Name: "B"},
		{ID: "b", Name: "B dup"},
	}, "")
	require.Len(t, acpDup, 1)
	assert.Equal(t, "b", acpDup[0].ID)
}

func TestResolveModels_BlankIDsAreDropped(t *testing.T) {
	got := ResolveModels(
		[]AgentModel{{ID: "", Name: "junk"}, {ID: "a", Name: "A"}},
		nil, "",
	)

	require.Len(t, got, 1)
	assert.Equal(t, "a", got[0].ID)

	got = ResolveModels(nil, []AgentModel{{ID: "  ", Name: "blank"}, {ID: "b", Name: "B"}}, "")
	require.Len(t, got, 1)
	assert.Equal(t, "b", got[0].ID)
}

func TestResolveModels_EmptyACPNameFallsBackToCLIName(t *testing.T) {
	cli := []AgentModel{{ID: "a", Name: "Nice Name"}}
	acp := []AgentModel{{ID: "a", Name: ""}}

	got := ResolveModels(cli, acp, "")

	require.Len(t, got, 1)
	assert.Equal(t, "Nice Name", got[0].Name, "empty ACP name must not blank out a good CLI name")
}

func TestResolveModels_DoesNotMutateInputs(t *testing.T) {
	cli := []AgentModel{{ID: "a", Name: "A", Default: true}}
	acp := []AgentModel{{ID: "a", Name: "A from ACP"}}

	got := ResolveModels(cli, acp, "")

	assert.Equal(t, "A", cli[0].Name, "input slice must not be mutated")
	assert.True(t, cli[0].Default)
	require.Len(t, got, 1)
	assert.Equal(t, "A from ACP", got[0].Name)
}

// ---------------------------------------------------------------------------
// Model source registry
// ---------------------------------------------------------------------------

func TestModelSourceRegistry_RegisterAndLookup(t *testing.T) {
	restore := isolateModelSources(t)
	defer restore()

	RegisterModelSource(StaticSource("reg-test", "reg-test-cli", []AgentModel{{ID: "m1", Name: "M1"}}))

	src, ok := LookupModelSource("reg-test")
	require.True(t, ok)
	assert.Equal(t, "reg-test", src.Backend())
	assert.True(t, HasModelSource("reg-test"))

	_, ok = LookupModelSource("nope")
	assert.False(t, ok)
	assert.False(t, HasModelSource("nope"))
}

func TestModelSourceRegistry_LastRegistrationWins(t *testing.T) {
	restore := isolateModelSources(t)
	defer restore()

	RegisterModelSource(StaticSource("dup", "", []AgentModel{{ID: "old"}}))
	RegisterModelSource(StaticSource("dup", "", []AgentModel{{ID: "new"}}))

	src, ok := LookupModelSource("dup")
	require.True(t, ok)
	models, _ := src.Discover()
	require.Len(t, models, 1)
	assert.Equal(t, "new", models[0].ID)
}

func TestStaticSource_ReturnsCatalogWithDefaultMarked(t *testing.T) {
	src := StaticSource("s", "", []AgentModel{
		{ID: "a", Name: "A"},
		{ID: "b", Name: "B", Default: true},
	})

	models, detail := src.Discover()

	assert.Empty(t, detail)
	require.Len(t, models, 2)
	assert.False(t, models[0].Default, "explicit catalog default flag is respected")
	assert.True(t, models[1].Default)
}

func TestStaticSource_MarksFirstWhenNoExplicitDefault(t *testing.T) {
	src := StaticSource("s", "", []AgentModel{
		{ID: "a", Name: "A"},
		{ID: "b", Name: "B"},
	})

	models, _ := src.Discover()

	require.Len(t, models, 2)
	assert.True(t, models[0].Default)
}

func TestStaticSource_UnavailableWhenCLIMissing(t *testing.T) {
	src := StaticSource("s", "definitely-not-a-real-cli-xyz", []AgentModel{{ID: "a"}})

	models, detail := src.Discover()

	assert.Nil(t, models, "static source yields nothing when its CLI is absent")
	assert.Empty(t, detail)
}

func TestStaticSource_EmptyCommandIsAlwaysAvailable(t *testing.T) {
	// Backends with no CLI (e.g. mock, ACP-only) still expose a catalog.
	src := StaticSource("s", "", []AgentModel{{ID: "a"}})

	models, _ := src.Discover()

	require.Len(t, models, 1)
}

func TestCLISource_ParsesCommandOutput(t *testing.T) {
	src := NewCLISource("cli-test", CLIOptions{
		Command: "echo",
		Args:    []string{"alpha/beta\ngamma/delta"},
		Parse:   ParseProviderModel,
	})

	models, detail := src.Discover()

	assert.Empty(t, detail)
	require.Len(t, models, 2)
	assert.Equal(t, "alpha/beta", models[0].ID)
	assert.Equal(t, "gamma/delta", models[1].ID)
	assert.True(t, models[0].Default)
}

func TestCLISource_FallsBackWhenCommandFails(t *testing.T) {
	src := NewCLISource("cli-test", CLIOptions{
		Command:  "definitely-not-a-real-cli-xyz",
		Args:     []string{"models"},
		Parse:    ParseProviderModel,
		Fallback: []AgentModel{{ID: "fb", Name: "Fallback"}},
	})

	models, _ := src.Discover()

	require.Len(t, models, 1)
	assert.Equal(t, "fb", models[0].ID)
	assert.True(t, models[0].Default, "fallback list gets its first entry marked default")
}

func TestCLISource_ReturnsNilWhenCommandFailsWithNoFallback(t *testing.T) {
	src := NewCLISource("cli-test", CLIOptions{
		Command: "definitely-not-a-real-cli-xyz",
		Parse:   ParseProviderModel,
	})

	models, detail := src.Discover()

	assert.Nil(t, models)
	assert.Contains(t, detail, "cli-test", "failure detail names the backend")
}

func TestCLISource_TriesAlternateCommands(t *testing.T) {
	src := NewCLISource("cli-test", CLIOptions{
		Commands: []CommandSpec{
			{Command: "definitely-not-a-real-cli-xyz", Args: []string{"models"}},
			{Command: "echo", Args: []string{"p/m"}},
		},
		Parse: ParseProviderModel,
	})

	models, _ := src.Discover()

	require.Len(t, models, 1)
	assert.Equal(t, "p/m", models[0].ID)
}

func TestCLISource_CustomParser(t *testing.T) {
	src := NewCLISource("cli-test", CLIOptions{
		Command: "echo",
		Args:    []string{"ignored"},
		Parse: func(string) []AgentModel {
			return []AgentModel{{ID: "custom", Name: "Custom"}}
		},
	})

	models, _ := src.Discover()

	require.Len(t, models, 1)
	assert.Equal(t, "custom", models[0].ID)
}

func TestPluginSource_ClearsDetailOnSuccess(t *testing.T) {
	src := PluginSource("plug", func() ([]AgentModel, string) {
		return []AgentModel{{ID: "x", Name: "X"}}, "some intermediate note"
	})

	models, detail := src.Discover()

	require.Len(t, models, 1)
	assert.Equal(t, "x", models[0].ID)
	assert.Empty(t, detail, "a successful probe has no failure to report")
}

func TestPluginSource_ReportsDetailOnEmptyResult(t *testing.T) {
	src := PluginSource("plug", func() ([]AgentModel, string) {
		return nil, "probed 3 locations, none had a catalog"
	})

	models, detail := src.Discover()

	assert.Nil(t, models)
	assert.Equal(t, "probed 3 locations, none had a catalog", detail)
}

func TestPluginSource_MarksFirstDefaultWhenUnmarked(t *testing.T) {
	src := PluginSource("plug", func() ([]AgentModel, string) {
		return []AgentModel{{ID: "x"}, {ID: "y"}}, ""
	})

	models, _ := src.Discover()

	require.Len(t, models, 2)
	assert.True(t, models[0].Default)
	assert.False(t, models[1].Default)
}

func TestPluginSource_RespectsExplicitDefault(t *testing.T) {
	src := PluginSource("plug", func() ([]AgentModel, string) {
		return []AgentModel{{ID: "x"}, {ID: "y", Default: true}}, ""
	})

	models, _ := src.Discover()

	assert.False(t, models[0].Default)
	assert.True(t, models[1].Default)
}

// ---------------------------------------------------------------------------
// Discovery cache
// ---------------------------------------------------------------------------

func TestDiscoveryCache_CachesWithinTTL(t *testing.T) {
	now := time.Unix(1000, 0)
	calls := 0
	c := newDiscoveryCache(time.Minute)
	c.now = func() time.Time { return now }

	for range 3 {
		models, _ := c.get("b", func() ([]AgentModel, string) {
			calls++
			return []AgentModel{{ID: "m"}}, ""
		})
		require.Len(t, models, 1)
	}

	assert.Equal(t, 1, calls, "repeat lookups inside the TTL must not re-probe")
}

func TestDiscoveryCache_ExpiresAfterTTL(t *testing.T) {
	now := time.Unix(1000, 0)
	calls := 0
	c := newDiscoveryCache(time.Minute)
	c.now = func() time.Time { return now }

	probe := func() ([]AgentModel, string) {
		calls++
		return []AgentModel{{ID: "m"}}, ""
	}

	c.get("b", probe)
	now = now.Add(61 * time.Second)
	c.get("b", probe)

	assert.Equal(t, 2, calls, "lookup after the TTL must re-probe")
}

func TestDiscoveryCache_CachesEmptyResults(t *testing.T) {
	calls := 0
	c := newDiscoveryCache(time.Minute)
	c.now = func() time.Time { return time.Unix(1000, 0) }

	probe := func() ([]AgentModel, string) {
		calls++
		return nil, "why it failed"
	}

	_, detail := c.get("b", probe)
	assert.Equal(t, "why it failed", detail)
	_, detail = c.get("b", probe)
	assert.Equal(t, "why it failed", detail, "failure detail is cached alongside the empty result")

	assert.Equal(t, 1, calls, "a failing probe must not be retried on every call inside the TTL")
}

func TestDiscoveryCache_InvalidateForcesReprobe(t *testing.T) {
	now := time.Unix(1000, 0)
	calls := 0
	c := newDiscoveryCache(time.Minute)
	c.now = func() time.Time { return now }

	probe := func() ([]AgentModel, string) {
		calls++
		return []AgentModel{{ID: "m"}}, ""
	}

	c.get("b", probe)
	c.invalidate("b")
	c.get("b", probe)

	assert.Equal(t, 2, calls)
}

func TestDiscoveryCache_InvalidateAll(t *testing.T) {
	now := time.Unix(1000, 0)
	calls := 0
	c := newDiscoveryCache(time.Minute)
	c.now = func() time.Time { return now }

	probe := func() ([]AgentModel, string) {
		calls++
		return []AgentModel{{ID: "m"}}, ""
	}

	c.get("a", probe)
	c.get("b", probe)
	c.invalidateAll()
	c.get("a", probe)
	c.get("b", probe)

	assert.Equal(t, 4, calls)
}

func TestDiscoveryCache_ReturnsDefensiveCopy(t *testing.T) {
	c := newDiscoveryCache(time.Minute)
	c.now = func() time.Time { return time.Unix(1000, 0) }

	first, _ := c.get("b", func() ([]AgentModel, string) {
		return []AgentModel{{ID: "m", Name: "M", Default: true}}, ""
	})
	first[0].Name = "mutated"

	second, _ := c.get("b", func() ([]AgentModel, string) { return nil, "" })

	require.Len(t, second, 1)
	assert.Equal(t, "M", second[0].Name, "cached entries must not be mutable by callers")
}

func TestDiscoveryCache_ConcurrentAccess(t *testing.T) {
	c := newDiscoveryCache(time.Minute)
	var probes int
	done := make(chan struct{})

	for range 16 {
		go func() {
			defer func() { done <- struct{}{} }()
			models, _ := c.get("shared", func() ([]AgentModel, string) {
				probes++
				return []AgentModel{{ID: "m"}}, ""
			})
			assert.Len(t, models, 1)
		}()
	}
	for range 16 {
		<-done
	}
	// Not asserting an exact probe count: concurrent first-callers may all miss.
	// The point is that the cache must not race (run with -race).
	assert.GreaterOrEqual(t, probes, 1)
}

// ---------------------------------------------------------------------------
// Discovery failure surfacing
// ---------------------------------------------------------------------------

func TestDiscoverWithDetail_ReportsFailureReason(t *testing.T) {
	restore := isolateModelSources(t)
	defer restore()
	RegisterModelSource(PluginSource("failing", func() ([]AgentModel, string) {
		return nil, "probed 8 directories, none contained product.json"
	}))

	models, detail := DiscoverWithDetail("failing")

	assert.Nil(t, models)
	assert.Equal(t, "probed 8 directories, none contained product.json", detail)
}

func TestDiscoverWithDetail_UnknownBackend(t *testing.T) {
	restore := isolateModelSources(t)
	defer restore()

	models, detail := DiscoverWithDetail("ghost")

	assert.Nil(t, models)
	assert.Empty(t, detail, "a backend with no source registered has no failure story to tell")
}

func TestDiscoverWithDetail_SuccessHasNoDetail(t *testing.T) {
	restore := isolateModelSources(t)
	defer restore()
	RegisterModelSource(StaticSource("ok", "", []AgentModel{{ID: "m"}}))

	models, detail := DiscoverWithDetail("ok")

	require.Len(t, models, 1)
	assert.Empty(t, detail)
}

func TestDiscoverWithDetail_ProbePanicBecomesDetail(t *testing.T) {
	restore := isolateModelSources(t)
	defer restore()
	RegisterModelSource(PluginSource("panicky", func() ([]AgentModel, string) {
		panic(errors.New("boom"))
	}))

	models, detail := DiscoverWithDetail("panicky")

	assert.Nil(t, models, "a panicking probe must not take the process down")
	assert.Contains(t, detail, "boom")
}

// ---------------------------------------------------------------------------
// Source kind + invalidation
// ---------------------------------------------------------------------------

func TestModelSourceKinds(t *testing.T) {
	assert.Equal(t, SourceKindStatic, StaticSource("s", "", nil).Kind())
	assert.Equal(t, SourceKindCLI, NewCLISource("c", CLIOptions{}).Kind())
	assert.Equal(t, SourceKindPlugin, PluginSource("p", nil).Kind())
}

func TestInvalidateDiscoveredModels_SingleBackend(t *testing.T) {
	restore := isolateModelSources(t)
	defer restore()

	calls := 0
	RegisterModelSource(PluginSource("inv-one", func() ([]AgentModel, string) {
		calls++
		return []AgentModel{{ID: "m"}}, ""
	}))

	DiscoverModels("inv-one")
	DiscoverModels("inv-one")
	assert.Equal(t, 1, calls)

	InvalidateDiscoveredModels("inv-one")
	DiscoverModels("inv-one")
	assert.Equal(t, 2, calls, "invalidating one backend must force a re-probe for it")
}

func TestRegisterModelSource_IgnoresInvalidInput(t *testing.T) {
	restore := isolateModelSources(t)
	defer restore()

	RegisterModelSource(nil)
	RegisterModelSource(StaticSource("", "", nil))

	assert.Empty(t, RegisteredModelSources(), "nil sources and empty backend IDs must be rejected")
}

func TestCLISource_NoCommandsConfigured(t *testing.T) {
	src := NewCLISource("empty", CLIOptions{Parse: ParseProviderModel})

	models, detail := src.Discover()

	assert.Nil(t, models)
	assert.Contains(t, detail, "no command configured")
}

func TestCLISource_InvalidPatternReportsDetail(t *testing.T) {
	src := NewCLISource("bad-re", CLIOptions{
		Command: "echo",
		Args:    []string{"anything"},
		Parse: func(string) []AgentModel {
			return ParseRegexCapture("x", RegexOptions{Pattern: "(["})
		},
	})

	models, detail := src.Discover()

	assert.Nil(t, models)
	assert.Contains(t, detail, "no models parsed")
}

func TestPluginSource_NilProbe(t *testing.T) {
	src := PluginSource("nil-probe", nil)

	models, detail := src.Discover()

	assert.Nil(t, models)
	assert.Empty(t, detail)
}
