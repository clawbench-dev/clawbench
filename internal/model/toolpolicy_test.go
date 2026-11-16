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

// TestToolPolicy_BudgetDefaults pins that an unset budget is bounded, not
// infinite: an agent loop with no round limit can spin until the user notices
// the bill.
func TestToolPolicy_BudgetDefaults(t *testing.T) {
	var p ToolPolicy
	assert.Equal(t, DefaultMaxRounds, p.Rounds())
	assert.Equal(t, DefaultMaxToolCalls, p.ToolCalls())
	assert.Equal(t, DefaultBashTimeoutSec, p.CommandTimeout())

	p = ToolPolicy{MaxRounds: 3, MaxToolCalls: 5, BashTimeoutSec: 9}
	assert.Equal(t, 3, p.Rounds())
	assert.Equal(t, 5, p.ToolCalls())
	assert.Equal(t, 9, p.CommandTimeout())
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
	assert.Error(t, ValidateToolPolicy(ToolPolicy{BashTimeoutSec: MaxBashTimeoutSec + 1}))
}

// TestToolPolicy_EncodeDecodeRoundTrip pins that a stored policy reads back
// identical, and that the encoded form is always normalized.
func TestToolPolicy_EncodeDecodeRoundTrip(t *testing.T) {
	in := ToolPolicy{Allow: []string{CapExec}, MaxRounds: 7}
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
