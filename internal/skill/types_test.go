package skill

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestSourceKindString pins the stable identifiers emitted by the HTTP API and
// used by tests. The numeric values are the dedup priority, so the mapping must
// not drift.
func TestSourceKindString(t *testing.T) {
	tests := []struct {
		kind SourceKind
		want string
	}{
		{SourceOwnNative, "own"},
		{SourceUserDir, "user"},
		{SourceGit, "git"},
		{SourceOtherNative, "other"},
		{SourceKind(99), "unknown"},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.want, tc.kind.String())
	}
}

// TestSourceKindPriorityOrder pins that the numeric values encode the dedup
// priority documented on the type: own < user < git < other. A reorder silently
// changes which duplicate skill wins, so guard it explicitly.
func TestSourceKindPriorityOrder(t *testing.T) {
	assert.Less(t, int(SourceOwnNative), int(SourceUserDir))
	assert.Less(t, int(SourceUserDir), int(SourceGit))
	assert.Less(t, int(SourceGit), int(SourceOtherNative))
}

// TestSourceKey pins the registry map key. Native sources are keyed by
// directory alone (NOT by agent) so a directory declared by several agents is
// still scanned once and listed once; every other kind is prefixed by its kind.
func TestSourceKey(t *testing.T) {
	tests := []struct {
		name string
		src  Source
		want string
	}{
		{"own native keyed by dir", Source{Kind: SourceOwnNative, Dir: "/a/skills"}, "native:/a/skills"},
		{"other native shares the native namespace", Source{Kind: SourceOtherNative, Dir: "/a/skills"}, "native:/a/skills"},
		{"user dir", Source{Kind: SourceUserDir, Dir: "/u/skills"}, "user:/u/skills"},
		{"git repo", Source{Kind: SourceGit, Dir: "/r/skills"}, "git:/r/skills"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.src.Key())
		})
	}
}

// TestSourceKey_NativeIgnoresAgentID pins the deliberate omission: the same
// directory read by two different agents must produce ONE key, otherwise its
// skills would be listed once per agent.
func TestSourceKey_NativeIgnoresAgentID(t *testing.T) {
	a := Source{Kind: SourceOwnNative, AgentID: "codebuddy", Dir: "/shared/skills"}
	b := Source{Kind: SourceOwnNative, AgentID: "claude", Dir: "/shared/skills"}
	assert.Equal(t, a.Key(), b.Key())
}

// TestCanonicalName_EdgeCases covers the empty and whitespace-only inputs not
// exercised by scanner_test.go's TestCanonicalName. Identity comparison must
// never panic or collapse distinct names.
func TestCanonicalName_EdgeCases(t *testing.T) {
	assert.Equal(t, "", canonicalName(""))
	assert.Equal(t, "", canonicalName("   "))
	assert.Equal(t, "", canonicalName("/"))
	assert.Equal(t, "a", canonicalName("  /A  "))
}

// TestGlobal_ReturnsSingleton pins that every caller shares one registry, so a
// scan performed for one consumer is visible to the rest.
func TestGlobal_ReturnsSingleton(t *testing.T) {
	assert.Same(t, Global(), Global())
}
