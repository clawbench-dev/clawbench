package service

import (
	"testing"

	"clawbench/internal/model"
)

// These constants MIRROR the built-in backend registry (zcode/register.go and
// codebuddy/cli.go). They are hardcoded, not derived from GetAgentSpecialty:
// deriving them would let a missing agent pass every alias assertion silently.
// This service test binary does not run the startup agent refresh, so the
// registry is empty here and seedSpecialtyAgents below injects these exact
// values. The test therefore cannot observe the real registry — if a built-in
// specialty changes, update the constant AND its registry entry together.
const (
	zcodeSpecialty     = "智谱 GLM 编码代理"
	codebuddySpecialty = "全栈开发助手"
	zcodeAlias         = "ZCode（" + zcodeSpecialty + "）"
	codebuddyAlias     = "Codebuddy（" + codebuddySpecialty + "）"
)

// seedSpecialtyAgents registers the agents the alias assertions depend on, and
// removes the ones it added afterwards. The service test binary does not run the
// startup agent refresh, so without this the registry is empty and every alias
// registration would be skipped (the exact silent pass this test guards against).
func seedSpecialtyAgents(t *testing.T) {
	t.Helper()
	prevList := model.GetAgentList()
	prevMap := make(map[string]*model.Agent, len(prevList))
	for _, a := range prevList {
		prevMap[a.ID] = a
	}
	nextMap := make(map[string]*model.Agent, len(prevMap)+2)
	for id, a := range prevMap {
		nextMap[id] = a
	}
	nextList := append([]*model.Agent(nil), prevList...)
	agents := []*model.Agent{
		{ID: "zcode", Name: "ZCode", Specialty: zcodeSpecialty},
		{ID: "codebuddy", Name: "Codebuddy", Specialty: codebuddySpecialty},
	}
	added := false
	for _, a := range agents {
		if nextMap[a.ID] != nil {
			continue
		}
		nextMap[a.ID] = a
		nextList = append(nextList, a)
		added = true
	}
	if !added {
		return
	}
	model.ReplaceAgents(nextMap, nextList)
	t.Cleanup(func() { model.ReplaceAgents(prevMap, prevList) })
}

func TestMemberLookups_SpecialtyAliasAndParenthesesPreservation(t *testing.T) {
	seedSpecialtyAgents(t)
	members := []GroupMember{
		{ID: "m1", Name: "agy(zhu)", AgentID: "antigravity"},
		{ID: "m2", Name: "agy(hw)", AgentID: "antigravity"},
		{ID: "m3", Name: "ZCode", AgentID: "zcode"},
		{ID: "m4", Name: "Codebuddy", AgentID: "codebuddy"},
	}

	byID, byName := memberLookups(members)

	// 1. Direct name lookup must succeed
	if m, ok := lookupMember("agy(zhu)", byID, byName); !ok || m.ID != "m1" {
		t.Fatalf("lookupMember agy(zhu) failed: got %+v, ok=%v", m, ok)
	}
	if m, ok := lookupMember("agy(hw)", byID, byName); !ok || m.ID != "m2" {
		t.Fatalf("lookupMember agy(hw) failed: got %+v, ok=%v", m, ok)
	}
	if m, ok := lookupMember("ZCode", byID, byName); !ok || m.ID != "m3" {
		t.Fatalf("lookupMember ZCode failed: got %+v, ok=%v", m, ok)
	}

	// 2. Specialty-annotated alias (copied from the prompt) must resolve EXACTLY.
	// lookupMemberExact, not lookupMember: the latter's trailing-parenthesis
	// fallback would strip the alias back to the bare name and succeed even if
	// the alias registration were removed, which is the silent pass this
	// assertion exists to catch.
	if m, ok := lookupMemberExact(zcodeAlias, byID, byName); !ok || m.ID != "m3" {
		t.Fatalf("lookupMemberExact %q failed: got %+v, ok=%v", zcodeAlias, m, ok)
	}
	if m, ok := lookupMemberExact(codebuddyAlias, byID, byName); !ok || m.ID != "m4" {
		t.Fatalf("lookupMemberExact %q failed: got %+v, ok=%v", codebuddyAlias, m, ok)
	}

	// 3. Leading @ symbol should be tolerated
	if m, ok := lookupMember("@ZCode", byID, byName); !ok || m.ID != "m3" {
		t.Fatalf("lookupMember @ZCode failed: got %+v, ok=%v", m, ok)
	}

	// 4. Distinction between agy(zhu) and agy(hw) must be preserved
	mZhu, okZhu := lookupMember("agy(zhu)", byID, byName)
	mHw, okHw := lookupMember("agy(hw)", byID, byName)
	if !okZhu || !okHw || mZhu.ID == mHw.ID {
		t.Fatalf("agy(zhu) and agy(hw) collided or failed: zhu=%+v, hw=%+v", mZhu, mHw)
	}

	// 5. Arbitrary TRAILING parentheses descriptions fall back to the bare name
	if m, ok := lookupMember("ZCode（自定义描述）", byID, byName); !ok || m.ID != "m3" {
		t.Fatalf("lookupMember ZCode（自定义描述） failed: got %+v, ok=%v", m, ok)
	}
	if m, ok := lookupMember("Codebuddy(任意英文括号)", byID, byName); !ok || m.ID != "m4" {
		t.Fatalf("lookupMember Codebuddy(任意英文括号) failed: got %+v, ok=%v", m, ok)
	}

	// 6. Only a TRAILING group is stripped. `...）补充` must not resolve, otherwise
	// Go would route targets the frontend refuses to render (parity with
	// web/src/utils/groupRouting.ts stripTrailingSpecParentheses, which has the
	// same case).
	if m, ok := lookupMember("ZCode（自定义描述）补充", byID, byName); ok {
		t.Fatalf("lookupMember ZCode（自定义描述）补充 unexpectedly resolved to %+v", m)
	}
}

// trimTrailingBracketGroup is the Go half of the rule mirrored by
// web/src/utils/groupRouting.ts stripTrailingSpecParentheses. It lives in this
// package (not internal/grouprouting), so the shared parity corpus cannot cover
// it; these table cases mirror the TS unit cases instead, including the
// nested/unbalanced inputs where Go's "last same-width opening bracket wins"
// rule differs from a balanced-group regex.
func TestTrimTrailingBracketGroup(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"ZCode（智谱 GLM 编码代理）", "ZCode", true},
		{"Codebuddy(全栈开发助手)", "Codebuddy", true},
		{"agy(zhu)", "agy", true},
		{"agy(zhu)（架构）", "agy(zhu)", true},
		{"ZCode（任意描述）补充", "", false},
		{"ZCode", "", false},
		// Divergence points from the balanced-group regex: Go strips at the
		// LAST same-width opening bracket even when the suffix is nested or
		// unbalanced. TS mirrors these exact expectations.
		{"A（x）y）", "A", true},
		{"A（x（y））", "A（x", true},
		{"A(x)y)", "A", true},
		{"A(x(y))", "A(x", true},
		{"A（x）y)", "", false},
		{"A(x)y）", "", false},
		{"A（x)y）", "A", true},
		{"A(x）y)", "A", true},
		{"A（x（y）", "A（x", true},
		{"A(x(y)", "A(x", true},
	}
	for _, tc := range cases {
		got, ok := trimTrailingBracketGroup(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("trimTrailingBracketGroup(%q) = %q, %v; want %q, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

// TestMemberLookups_BracketedNameAliases pins that only the WRAPPER
// parentheses are translated to ASCII: a name that itself contains full-width
// brackets must keep them in both alias variants.
func TestMemberLookups_BracketedNameAliases(t *testing.T) {
	seedSpecialtyAgents(t)
	byID, byName := memberLookups([]GroupMember{
		{ID: "m1", Name: "agy（zhu）", AgentID: "zcode"},
	})

	full := "agy（zhu）（" + zcodeSpecialty + "）"
	ascii := "agy（zhu）(" + zcodeSpecialty + ")"
	for _, key := range []string{full, ascii} {
		if m, ok := lookupMemberExact(key, byID, byName); !ok || m.ID != "m1" {
			t.Fatalf("lookupMemberExact %q failed: got %+v, ok=%v", key, m, ok)
		}
	}
	// The old implementation translated the name's own brackets too, producing
	// this alias; it must be gone, otherwise it can collide with a real member.
	if _, ok := byName["agy(zhu)("+zcodeSpecialty+")"]; ok {
		t.Fatalf("over-converted alias agy(zhu)(%s) must not be registered", zcodeSpecialty)
	}
}

// TestMemberLookups_RepeatedNamesResolveUniquely pins that two members whose
// names differ only by bracket width still own distinct ASCII-wrapper aliases
// regardless of roster order. Before the wrapper-only fix, both registered
// agy(zhu)(spec) and the last writer won, so a bcc could resolve differently
// depending on member order.
func TestMemberLookups_RepeatedNamesResolveUniquely(t *testing.T) {
	seedSpecialtyAgents(t)
	wide := GroupMember{ID: "m1", Name: "agy（zhu）", AgentID: "zcode"}
	narrow := GroupMember{ID: "m2", Name: "agy(zhu)", AgentID: "zcode"}

	for name, members := range map[string][]GroupMember{
		"forward": {wide, narrow},
		"reverse": {narrow, wide},
	} {
		byID, byName := memberLookups(members)
		cases := []struct{ target, want string }{
			{"agy（zhu）(" + zcodeSpecialty + ")", "m1"},
			{"agy(zhu)(" + zcodeSpecialty + ")", "m2"},
			{"agy（zhu）（" + zcodeSpecialty + "）", "m1"},
			{"agy(zhu)（" + zcodeSpecialty + "）", "m2"},
		}
		for _, tc := range cases {
			if m, ok := lookupMemberExact(tc.target, byID, byName); !ok || m.ID != tc.want {
				t.Errorf("%s: %q -> %s, ok=%v; want %s", name, tc.target, m.ID, ok, tc.want)
			}
		}
	}
}

// Private notes (bcc) must fail closed: only the row id, the exact display name,
// or a registered full alias may match. No arbitrary parenthesized fallback.
func TestMemberLookups_PrivateNoteRequiresExactMatch(t *testing.T) {
	seedSpecialtyAgents(t)
	members := []GroupMember{
		{ID: "m1", Name: "ZCode", AgentID: "zcode"},
	}
	byID, byName := memberLookups(members)

	if m, ok := lookupMemberExact("m1", byID, byName); !ok || m.ID != "m1" {
		t.Fatalf("lookupMemberExact row id failed: got %+v, ok=%v", m, ok)
	}
	if m, ok := lookupMemberExact("ZCode", byID, byName); !ok || m.ID != "m1" {
		t.Fatalf("lookupMemberExact name failed: got %+v, ok=%v", m, ok)
	}
	if m, ok := lookupMemberExact(zcodeAlias, byID, byName); !ok || m.ID != "m1" {
		t.Fatalf("lookupMemberExact alias failed: got %+v, ok=%v", m, ok)
	}
	if m, ok := lookupMemberExact("ZCode（Bob）", byID, byName); ok {
		t.Fatalf("lookupMemberExact retargeted sloppy bcc to %+v", m)
	}
}
