package backends_test

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/ai"
	"clawbench/internal/ai/backends"
	_ "clawbench/internal/ai/backends/all"
)

// TestExtractMetaUsage_Dispatch checks that every backend family parses its own
// _meta shape through the registry dispatch, not through the generic fallback.
//
// The fixture is the payload a real Claude/Codex bridge sends: an aggregate
// token_count (whose cache counters live OUTSIDE inputTokens), plus a
// model_usage array carrying a different, larger model. It is deliberately NOT
// a shape both implementations agree on — an earlier fixture
// ({"quota":{"token_count":{"inputTokens":5}}}) happened to yield 5 under the
// generic recursive scan as well, so it passed even when claude had no
// ExtractMeta hook at all.
func TestExtractMetaUsage_Dispatch(t *testing.T) {
	cbMeta := map[string]any{"usage": map[string]any{"prompt_tokens": 10}}
	ext := ai.ExtractMetaUsageForTest("codebuddy", cbMeta)
	require.NotNil(t, ext)
	assert.Equal(t, 10, ext.Usage.InputTokens)

	claudeMeta := func() map[string]any {
		return map[string]any{"quota": map[string]any{
			"token_count": map[string]any{
				"inputTokens":           100,
				"outputTokens":          10,
				"cachedInputTokens":     900,
				"cachedWriteTokens":     40,
				"totalTokens":           1050,
				"reasoningOutputTokens": 0,
			},
			"model_usage": []any{
				map[string]any{
					"model": "claude-sonnet-4-6",
					"token_count": map[string]any{
						"inputTokens":       500,
						"outputTokens":      5,
						"cachedInputTokens": 0,
						"totalTokens":       505,
					},
				},
			},
		}}
	}

	// Every agent on the Claude/Codex bridge stack must produce the SAME
	// normalized result: the aggregate (input folded with the cache counters,
	// cache split recorded) and the response model from model_usage. qoder and
	// qodercn are Claude Code forks that must re-export claude's hook rather
	// than lose it (losing it is how claude itself ended up unregistered).
	for _, backend := range []string{"claude", "codex", "qoder", "qodercn"} {
		ext := ai.ExtractMetaUsageForTest(backend, claudeMeta())
		require.NotNil(t, ext, backend)
		require.NotNil(t, ext.Usage, backend)
		assert.Equal(t, 1040, ext.Usage.InputTokens, backend+" input must fold in cached read+write")
		assert.Equal(t, 10, ext.Usage.OutputTokens, backend)
		assert.Equal(t, 900, ext.Usage.CacheHitTokens, backend)
		assert.Equal(t, 100, ext.Usage.CacheMissTokens, backend)
		require.NotNil(t, ext.Trace, backend+" must record the model_usage breakdown")
		assert.Equal(t, "claude-sonnet-4-6", ext.Trace.ResponseModelID, backend)
	}

	// Unknown backend falls through to generic scan.
	ext = ai.ExtractMetaUsageForTest("mystery-agent", map[string]any{"deep": map[string]any{"outputTokens": 7}})
	require.NotNil(t, ext)
	assert.Equal(t, 7, ext.Usage.OutputTokens)

	// OpenCode reports no _meta extensions.
	assert.Nil(t, ai.ExtractMetaUsageForTest("opencode", map[string]any{"timestamp": "t"}))
}

// TestExtractMetaUsage_GenericScanIsNotTheClaudeAnswer pins the difference the
// previous fixture hid: the generic scan reads whichever token_count key it
// reaches first (map order → sometimes the aggregate, sometimes the per-model
// entry) and knows nothing about response model ids. If claude stops declaring
// ExtractMeta, its answer becomes this one and the test above fails.
func TestExtractMetaUsage_GenericScanIsNotTheClaudeAnswer(t *testing.T) {
	meta := map[string]any{"quota": map[string]any{
		"token_count": map[string]any{
			"inputTokens":       100,
			"outputTokens":      10,
			"cachedInputTokens": 900,
			"totalTokens":       1010,
		},
		"model_usage": []any{
			map[string]any{
				"model":       "some-concrete-model",
				"token_count": map[string]any{"inputTokens": 500, "outputTokens": 5},
			},
		},
	}}

	claude := ai.ExtractMetaUsageForTest("claude", meta)
	generic := ai.ExtractMetaUsageForTest("mystery-agent", meta)
	require.NotNil(t, claude)
	require.NotNil(t, generic)

	assert.Equal(t, 1000, claude.Usage.InputTokens, "claude folds the cache read into input")
	assert.Equal(t, 900, claude.Usage.CacheHitTokens)
	require.NotNil(t, claude.Trace)
	assert.Equal(t, "some-concrete-model", claude.Trace.ResponseModelID)

	assert.Nil(t, generic.Trace, "the generic scan cannot identify the response model")
	assert.Zero(t, generic.Usage.CacheHitTokens, "the generic scan does not derive a cache split")
}

// TestClaudeFamilyBehaviorsAreShared is the structural guard for the bug that
// produced A1: the claude hook set was copied by hand into each fork, so adding
// a hook to one copy silently left the others (and once claude itself) on the
// generic fallback. Comparing function pointers proves the forks RE-EXPORT
// claude's set instead of re-declaring it — a fourth copy cannot appear without
// failing here, and a hook added to claude automatically covers every fork.
func TestClaudeFamilyBehaviorsAreShared(t *testing.T) {
	root := backends.LookupACPBehavior("claude")
	require.NotNil(t, root, "claude must declare its ACP behavior")

	// The whole point of A1: the bridge's _meta parser is part of the hook set.
	require.NotNil(t, root.ExtractMeta,
		"claude must register the _meta parser or every claude turn loses usage/model attribution")
	require.NotNil(t, root.ParseToolCall)
	require.NotNil(t, root.ParseToolCallUpdate)

	// qoder / qodercn are Claude Code forks and must RE-EXPORT the whole hook
	// set: identical function values, so a hook added to claude reaches them
	// automatically.
	for _, fork := range []string{"qoder", "qodercn"} {
		behavior := backends.LookupACPBehavior(fork)
		require.NotNil(t, behavior, "%s must declare its ACP behavior", fork)

		assert.Equal(t, reflect.ValueOf(root.ExtractMeta).Pointer(), reflect.ValueOf(behavior.ExtractMeta).Pointer(),
			"%s must re-export claude's _meta parser (same function value), not a copy", fork)
		assert.Equal(t, reflect.ValueOf(root.ParseToolCall).Pointer(), reflect.ValueOf(behavior.ParseToolCall).Pointer(), fork)
		assert.Equal(t, reflect.ValueOf(root.ParseToolCallUpdate).Pointer(), reflect.ValueOf(behavior.ParseToolCallUpdate).Pointer(), fork)
	}

	// codex shares only the _meta parser (its tool calls go through the
	// generic mapper), so pin the _meta half that it does rely on.
	codex := backends.LookupACPBehavior("codex")
	require.NotNil(t, codex, "codex must declare its ACP behavior")
	assert.Equal(t, reflect.ValueOf(root.ExtractMeta).Pointer(), reflect.ValueOf(codex.ExtractMeta).Pointer(),
		"codex shares the bridge _meta shape and must re-use claude's parser")
}

// TestWireQuirks_AreDeclaredByTheOwningBackend pins the two backends whose
// wire quirks the core used to special-case with `backend == "codebuddy"`.
//
// Both are now declared on the owner's ACPBehavior, so the guard is on the
// DECLARATION: dropping it from codebuddy would silently turn its credits into
// dollars in the cost statistics and let the previous turn's replayed text leak
// into the next reply, and neither would fail anywhere else.
func TestWireQuirks_AreDeclaredByTheOwningBackend(t *testing.T) {
	codebuddy := backends.LookupACPBehavior("codebuddy")
	require.NotNil(t, codebuddy, "codebuddy must declare its ACP behavior")
	assert.True(t, codebuddy.CostFieldCarriesCredit,
		"codebuddy ships credit in the standard cost field; without this flag it is persisted as USD")
	require.NotNil(t, codebuddy.IsReplayedTurnMeta,
		"codebuddy must declare the replay filter or the previous turn's tail leaks into the next reply")

	// No other backend may claim either quirk. Claude-family forks in
	// particular share codebuddy's tool/meta parsers nowhere, but codex shares
	// claude's _meta shape — an accidental copy of these flags would make one
	// of them drop real content.
	for _, id := range []string{"claude", "codex", "qoder", "qodercn", "cursor", "opencode", "kimi", "pi"} {
		behavior := backends.LookupACPBehavior(id)
		if behavior == nil {
			continue
		}
		assert.Falsef(t, behavior.CostFieldCarriesCredit,
			"%s reports a real monetary cost and must not reuse the credit flag", id)
		assert.Nilf(t, behavior.IsReplayedTurnMeta,
			"%s has no comparable cross-turn requestId, so it must not declare the replay filter", id)
	}
}

// TestOrphanCmdline_DeclaredByTheOwningBackend is the structural guard for the
// fallback that cleans up agent processes left behind by a crash.
//
// The three shapes below used to be a table inside internal/ai. Losing one is
// silent rather than loud: cleanup simply stops recognizing that backend's
// leftovers, and the process keeps running until the machine is rebooted. So
// each backend's OWN registration is pinned here — a backend that drops its
// declaration, or a new one that forgets to add it, fails this test instead of
// leaking processes in the field.
func TestOrphanCmdline_DeclaredByTheOwningBackend(t *testing.T) {
	cases := []struct {
		backend string
		binary  string
		arg     string
		why     string
	}{
		{"codebuddy", "codebuddy", "--acp", "spawns `codebuddy --acp`"},
		{"claude", "claude", "--acp", "spawns the claude ACP adapter"},
		{"codex", "codex", "app-server", "codex-acp delegates every turn to a `codex app-server` child"},
	}
	for _, tc := range cases {
		p := backends.Lookup(tc.backend)
		require.NotNil(t, p, "backend %q must be registered", tc.backend)
		assert.Equal(t, tc.binary, p.OrphanCmdline.Binary,
			"%s %s, so its orphan pattern must name that binary", tc.backend, tc.why)
		assert.Equal(t, tc.arg, p.OrphanCmdline.Arg,
			"%s %s, so its orphan pattern must name that argument", tc.backend, tc.why)
	}
}

// A backend that declares no orphan shape must stay absent from the registry:
// an empty half would match every command line and turn startup cleanup into
// "kill everything". Registration is what enforces it; this pins that the
// majority of backends (which spawn nothing, or spawn only under the env
// marker) do not accidentally arm a wildcard.
func TestOrphanCmdline_UnarmedForBackendsThatDeclareNothing(t *testing.T) {
	for _, id := range []string{"builtin", "custom-acp"} {
		p := backends.Lookup(id)
		if p == nil {
			continue // not registered in this build
		}
		assert.Empty(t, p.OrphanCmdline.Binary, "%s must not declare an orphan binary", id)
		assert.Empty(t, p.OrphanCmdline.Arg, "%s must not declare an orphan argument", id)
	}
}
