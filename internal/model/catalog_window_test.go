package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// withCatalogSnapshot installs a catalog for the duration of one test and
// restores the previous one.
func withCatalogSnapshot(t *testing.T, providers []CatalogProvider) {
	t.Helper()
	prev := CatalogSnapshot()
	t.Cleanup(func() { ReplaceCatalog(prev) })
	cat := &Catalog{}
	if providers != nil {
		cat.Providers = make(map[string]CatalogProvider, len(providers))
		for _, p := range providers {
			cat.Providers[p.ID] = p
		}
	}
	ReplaceCatalog(cat)
}

// TestModelContextWindow pins the rule that makes this value safe to use as a
// budget: the SMALLEST published window wins when providers disagree, because a
// caller assuming more than the endpoint really has gets a hard API error
// mid-conversation. Unknown is 0, never a guess.
func TestModelContextWindow(t *testing.T) {
	withCatalogSnapshot(t, []CatalogProvider{
		{ID: "p1", Models: []CatalogModel{
			{ID: "wide", Context: 200000},
			{ID: "narrow", Context: 128000},
			{ID: "nocontext", Context: 0},
		}},
		{ID: "p2", Models: []CatalogModel{
			{ID: "narrow", Context: 32000}, // disagrees: the smaller one must win
			{ID: "p1/narrow", Context: 1},  // a prefixed id is a different entry
		}},
	})

	require.Equal(t, 200000, ModelContextWindow("wide"))
	require.Equal(t, 32000, ModelContextWindow("narrow"),
		"the smallest published window is the safe budget")
	require.Zero(t, ModelContextWindow("nocontext"), "a missing window is unknown, not tiny")
	require.Zero(t, ModelContextWindow("never-heard-of"))
	require.Zero(t, ModelContextWindow(""), "an empty id cannot match anything")

	// A "<provider>/<model>" form resolves to the bare id when the prefix names
	// a catalog provider — several backends address models that way.
	require.Equal(t, 32000, ModelContextWindow("p1/narrow"),
		"the provider prefix must be stripped when it names a provider")

	// No catalog at all is the "unknown" case too, not a panic.
	ReplaceCatalog(nil)
	require.Zero(t, ModelContextWindow("wide"))
}

// TestOutputTokenBudget pins the ceiling rule: a model's own limit is used when
// the catalog publishes one, and it is clamped — asking for more than the
// ceiling makes some endpoints reject the call or reserve quota.
func TestOutputTokenBudget(t *testing.T) {
	withCatalogSnapshot(t, []CatalogProvider{
		{ID: "p1", Models: []CatalogModel{
			{ID: "small", Output: 4096},
			{ID: "huge", Output: MaxOutputTokenBudget * 4},
			{ID: "unset", Output: 0},
		}},
	})

	require.Equal(t, 4096, OutputTokenBudget("small"))
	require.Equal(t, MaxOutputTokenBudget, OutputTokenBudget("huge"),
		"a published limit above the ceiling is clamped")
	require.Equal(t, MaxOutputTokenBudget, OutputTokenBudget("unset"),
		"an unset limit asks for the ceiling, which is only an upper bound")
	require.Equal(t, MaxOutputTokenBudget, OutputTokenBudget("never-heard-of"))
}

// TestAgentEffectiveModeID: the built-in agent has no agent to ask for its mode,
// so an unset preference must fall back to the mode its tool grant expresses —
// otherwise every new built-in session starts with nothing selected. An ACP agent
// must stay empty (the agent reports its own default at session/new).
func TestAgentEffectiveModeID(t *testing.T) {
	acp := &Agent{PreferredMode: ""}
	require.Empty(t, acp.EffectiveModeID())

	// Zero-value policy = every capability off; Normalized() then expresses the
	// read-only default rather than an empty string.
	builtin := &Agent{BuiltinBackend: true}
	require.Equal(t, builtin.ToolPolicy.Normalized().DefaultMode(), builtin.EffectiveModeID())
	require.NotEmpty(t, builtin.EffectiveModeID())

	// An explicit preference always wins.
	builtin.PreferredMode = "plan"
	require.Equal(t, "plan", builtin.EffectiveModeID())
	acp.PreferredMode = "yolo"
	require.Equal(t, "yolo", acp.EffectiveModeID())
}
