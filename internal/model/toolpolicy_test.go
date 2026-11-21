package model

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestToolPolicy_ZeroValueGrantsNothing pins the fail-closed default.
//
// This is the single most important property of the type: a policy that was
// never configured must not grant access. If the zero value ever meant "all",
// every newly discovered built-in agent would silently be able to write files
// and run commands before the user saw a single switch.
func TestToolPolicy_ZeroValueGrantsNothing(t *testing.T) {
	var p ToolPolicy
	got := p.Normalized()
	assert.Empty(t, got.Allow)
	assert.False(t, got.Has(CapFSRead))
	assert.False(t, got.Has(CapFSWrite))
	assert.False(t, got.Has(CapExec))
}

// TestToolPolicy_NormalizedAllowIsNeverNil pins the wire shape.
//
// The policy is serialized to the client, where `null` becomes `undefined` and
// `new Set(undefined)` throws — the settings page would fail to render for
// every agent that has no grant (which is every freshly created one).
func TestToolPolicy_NormalizedAllowIsNeverNil(t *testing.T) {
	assert.NotNil(t, ToolPolicy{}.Normalized().Allow)
	assert.NotNil(t, ToolPolicy{Allow: nil}.Normalized().Allow)

	encoded, err := EncodeToolPolicy(ToolPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(encoded), &raw); err != nil {
		t.Fatal(err)
	}
	if _, isArray := raw["allow"].([]any); !isArray {
		t.Fatalf("allow must serialize as an array, got %T", raw["allow"])
	}
}

// TestToolPolicy_NormalizedClosesImplications pins that the stored policy is
// always the truth.
//
// A UI that lets someone tick "run commands" while leaving "write files" off
// produces exec-without-write. Keeping that shape would make the settings page
// claim the agent cannot write while the agent writes through bash. Closing the
// set means the displayed grant is what the agent can actually do.
func TestToolPolicy_NormalizedClosesImplications(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{"exec implies write and read", []string{CapExec}, []string{CapFSRead, CapFSWrite, CapExec}},
		{"write implies read", []string{CapFSWrite}, []string{CapFSRead, CapFSWrite}},
		{"read alone stays read", []string{CapFSRead}, []string{CapFSRead}},
		{"empty stays empty", nil, []string{}},
		{"unknown names are dropped", []string{"fs.delete", CapFSRead}, []string{CapFSRead}},
		{"duplicates collapse", []string{CapFSRead, CapFSRead}, []string{CapFSRead}},
		{"order is canonical", []string{CapExec, CapFSRead}, []string{CapFSRead, CapFSWrite, CapExec}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ToolPolicy{Allow: tc.in}.Normalized()
			assert.Equal(t, tc.want, got.Allow)
		})
	}
}

// TestToolPolicy_BudgetDefaults pins that an unset round / tool-call budget
// means UNLIMITED (zero), not a hidden number: the turn runs until the model
// stops or the user cancels. Only the per-command timeout keeps a default.
func TestToolPolicy_BudgetDefaults(t *testing.T) {
	var p ToolPolicy
	assert.Equal(t, 0, p.Rounds())
	assert.Equal(t, 0, p.ToolCalls())
	assert.Equal(t, 0, p.Subagents())
	assert.Equal(t, DefaultBashTimeoutSec, p.CommandTimeout())

	p = ToolPolicy{MaxRounds: 3, MaxToolCalls: 5, MaxSubagents: 2, BashTimeoutSec: 9}
	assert.Equal(t, 3, p.Rounds())
	assert.Equal(t, 5, p.ToolCalls())
	assert.Equal(t, 2, p.Subagents())
	assert.Equal(t, 9, p.CommandTimeout())
}

// TestToolPolicy_MaxSubagentsIsCarriedAndClamped pins the field that makes the
// sub-agent fan-out a setting rather than a constant: Normalized runs on every
// save and load, so a field it forgets to copy would silently reset to
// unlimited the first time a user touched another control.
func TestToolPolicy_MaxSubagentsIsCarriedAndClamped(t *testing.T) {
	assert.Equal(t, 6, ToolPolicy{MaxSubagents: 6}.Normalized().MaxSubagents)
	assert.Equal(t, 0, ToolPolicy{MaxSubagents: -1}.Normalized().MaxSubagents)
}

// TestToolPolicy_BashTimeoutIsCapped pins the upper bound: a per-command
// timeout measured in hours would let one tool call outlive the turn's own
// deadline and hide the fact that it is stuck.
func TestToolPolicy_BashTimeoutIsCapped(t *testing.T) {
	got := ToolPolicy{BashTimeoutSec: MaxBashTimeoutSec + 1}.Normalized()
	assert.Equal(t, MaxBashTimeoutSec, got.BashTimeoutSec)

	got = ToolPolicy{BashTimeoutSec: -5}.Normalized()
	assert.Equal(t, 0, got.BashTimeoutSec)
}

func TestToolPolicy_ValidateRejectsNegativeBudgets(t *testing.T) {
	assert.NoError(t, ValidateToolPolicy(ToolPolicy{}))
	assert.Error(t, ValidateToolPolicy(ToolPolicy{MaxRounds: -1}))
	assert.Error(t, ValidateToolPolicy(ToolPolicy{MaxToolCalls: -1}))
	assert.Error(t, ValidateToolPolicy(ToolPolicy{MaxSubagents: -1}))
	assert.Error(t, ValidateToolPolicy(ToolPolicy{BashTimeoutSec: MaxBashTimeoutSec + 1}))
}

// TestToolPolicy_EncodeDecodeRoundTrip pins that a stored policy reads back
// identical, and that the encoded form is always normalized.
func TestToolPolicy_EncodeDecodeRoundTrip(t *testing.T) {
	in := ToolPolicy{Allow: []string{CapExec}, MaxRounds: 7, MaxSubagents: 4}
	encoded, err := EncodeToolPolicy(in)
	require.NoError(t, err)

	// The encoded form must already be closed, so a reader that does not call
	// Normalized still sees the true grant.
	var raw map[string]any
	require.NoError(t, json.Unmarshal([]byte(encoded), &raw))
	assert.Equal(t, []any{CapFSRead, CapFSWrite, CapExec}, raw["allow"])

	out := DecodeToolPolicy(encoded)
	assert.Equal(t, []string{CapFSRead, CapFSWrite, CapExec}, out.Allow)
	assert.Equal(t, 7, out.MaxRounds)
	assert.Equal(t, 4, out.MaxSubagents)
}

// TestToolPolicy_RestrictToWorkDirSurvivesNormalized pins the bug this setting
// shipped with: Normalized runs on every save and every load, so a field it
// forgets to copy is silently reset to false the first time a user touches any
// OTHER control on the same panel.
//
// Both polarities are checked. A test that only covers true would still pass if
// Normalized hard-coded false; one that only covers false would pass if it
// hard-coded true.
func TestToolPolicy_RestrictToWorkDirSurvivesNormalized(t *testing.T) {
	assert.True(t, ToolPolicy{Allow: []string{CapFSRead}, RestrictToWorkDir: true}.Normalized().RestrictToWorkDir,
		"a confinement the user turned on must not be dropped by Normalized")
	assert.False(t, ToolPolicy{Allow: []string{CapFSRead}}.Normalized().RestrictToWorkDir,
		"the default must stay 'not confined'")
}

// TestToolPolicy_RestrictToWorkDirDefaultsToOpen pins which way the zero value
// points.
//
// The field stores the RESTRICTION, so the default (an agent that never touched
// the switch) is "not confined". Flipping the field's meaning would silently
// fence every existing agent on upgrade, so the direction is worth pinning.
func TestToolPolicy_RestrictToWorkDirDefaultsToOpen(t *testing.T) {
	assert.False(t, ToolPolicy{}.ConfinesToWorkDir())
	assert.True(t, ToolPolicy{RestrictToWorkDir: true}.ConfinesToWorkDir())

	// A stored policy round-trips through JSON, which is how the handler and the
	// database both carry it.
	encoded, err := EncodeToolPolicy(ToolPolicy{Allow: []string{CapFSRead}, RestrictToWorkDir: true})
	require.NoError(t, err)
	assert.True(t, DecodeToolPolicy(encoded).ConfinesToWorkDir())
}

// TestDecodeToolPolicy_DegradesToNoGrants pins the fail-closed read path.
//
// A corrupt column must never be read as "everything allowed" — that would turn
// a database hiccup into a privilege escalation.
func TestDecodeToolPolicy_DegradesToNoGrants(t *testing.T) {
	for _, raw := range []string{"", "null", "not json", "[]"} {
		got := DecodeToolPolicy(raw)
		assert.Empty(t, got.Allow, "input %q must grant nothing", raw)
	}
}

// TestEncodeToolPolicy_NilAllowIsEmptyArray pins the stored shape: a nil slice
// would marshal to "null", and a reader matching on '[]' would miss it.
func TestEncodeToolPolicy_NilAllowIsEmptyArray(t *testing.T) {
	encoded, err := EncodeToolPolicy(ToolPolicy{})
	require.NoError(t, err)
	assert.Contains(t, encoded, `"allow":[]`)
}

// TestToolPolicy_ForMode_NeverWidens pins the direction of the mode switch.
//
// The mode is a ceiling on the session, not a grant: a session that asks for
// "full" on an agent that was only granted fs.read must still be read-only.
// Getting this backwards would let one click in the chat drawer escalate past
// the settings switch the user set on purpose.
func TestToolPolicy_ForMode_NeverWidens(t *testing.T) {
	readOnly := ToolPolicy{Allow: []string{CapFSRead}}.Normalized()

	cases := []struct {
		name string
		p    ToolPolicy
		mode string
		want []string
	}{
		{"read on read-only grant", readOnly, ModeRead, []string{CapFSRead}},
		{"write on read-only grant", readOnly, ModeWrite, []string{CapFSRead}},
		{"full on read-only grant", readOnly, ModeFull, []string{CapFSRead}},
		{"read narrows a full grant", ToolPolicy{Allow: []string{CapExec}}.Normalized(), ModeRead, []string{CapFSRead}},
		{"write narrows a full grant", ToolPolicy{Allow: []string{CapExec}}.Normalized(), ModeWrite, []string{CapFSRead, CapFSWrite}},
		{"full keeps a full grant", ToolPolicy{Allow: []string{CapExec}}.Normalized(), ModeFull, []string{CapFSRead, CapFSWrite, CapExec}},
		// No mode chosen: the grant is used as-is (background tasks, older clients).
		{"empty mode keeps the grant", ToolPolicy{Allow: []string{CapExec}}.Normalized(), "", []string{CapFSRead, CapFSWrite, CapExec}},
		// A leftover mode id from another backend must fail closed, not pass through.
		{"unknown mode fails closed", ToolPolicy{Allow: []string{CapExec}}.Normalized(), "code", []string{CapFSRead}},
		{"zero grant stays empty", ToolPolicy{}.Normalized(), ModeFull, []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.p.ForMode(tc.mode)
			assert.Equal(t, tc.want, got.Allow)
			// The budgets are not part of the mode decision.
			assert.Equal(t, tc.p.MaxRounds, got.MaxRounds)
			assert.Equal(t, tc.p.MaxToolCalls, got.MaxToolCalls)
			assert.Equal(t, tc.p.MaxSubagents, got.MaxSubagents)
			assert.Equal(t, tc.p.BashTimeoutSec, got.BashTimeoutSec)
		})
	}
}

// TestToolPolicy_DefaultMode pins the mode a fresh session starts in: the
// narrowest one that still expresses the grant, so the chip never advertises
// more (or less) than the agent will run.
func TestToolPolicy_DefaultMode(t *testing.T) {
	assert.Equal(t, ModeFull, ToolPolicy{}.DefaultMode())
	assert.Equal(t, ModeRead, ToolPolicy{Allow: []string{CapFSRead}}.DefaultMode())
	assert.Equal(t, ModeWrite, ToolPolicy{Allow: []string{CapFSWrite}}.DefaultMode())
	assert.Equal(t, ModeFull, ToolPolicy{Allow: []string{CapExec}}.DefaultMode())
}

// TestBuiltinModes_AreOrderedByPrivilege pins the display order and that the
// returned slice is a copy (callers register it into the AI registry).
func TestBuiltinModes_AreOrderedByPrivilege(t *testing.T) {
	modes := BuiltinModes()
	require.Len(t, modes, 3)
	assert.Equal(t, ModeRead, modes[0].ID)
	assert.Equal(t, ModeWrite, modes[1].ID)
	assert.Equal(t, ModeFull, modes[2].ID)

	modes[0].ID = "mutated"
	assert.Equal(t, ModeRead, BuiltinModes()[0].ID, "BuiltinModes must return a copy")
}
