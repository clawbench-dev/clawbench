package service_test

import (
	"testing"

	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	tagProjectA = "/proj/a"
	tagProjectB = "/proj/b"
)

// namesOf is a tiny projection helper so assertions read as tag names.
func namesOf(tags []service.SessionTag) []string {
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		out = append(out, t.Name)
	}
	return out
}

func TestNormalizeSessionTagName(t *testing.T) {
	// Trimming/collapsing matters because the name is the user-facing identity:
	// "  bug  " and "bug" must not become two candidates in the dialog.
	assert.Equal(t, "bug", service.NormalizeSessionTagName("  bug  "))
	assert.Equal(t, "needs review", service.NormalizeSessionTagName("needs   review"))
	assert.Equal(t, "needs review", service.NormalizeSessionTagName("\tneeds\nreview "))
	assert.Equal(t, "", service.NormalizeSessionTagName("   "))
	assert.Equal(t, "", service.NormalizeSessionTagName(""))

	// Case folding is part of the identity. The DB collation is BINARY, so
	// without this "bug" and "Bug" would be two distinct rows in one project —
	// two chips that look identical to the user and cannot be told apart.
	assert.Equal(t, "bug", service.NormalizeSessionTagName("Bug"))
	assert.Equal(t, "bug", service.NormalizeSessionTagName("  BUG  "))
	assert.Equal(t, "needs review", service.NormalizeSessionTagName("Needs   Review"))
}

// TestSetSessionTags_CaseVariantsCollapseToOne pins the user-visible
// consequence of case folding: sending "bug" and "Bug" must yield ONE tag, not
// two near-identical chips on the session row.
func TestSetSessionTags_CaseVariantsCollapseToOne(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, tagProjectA, "codebuddy", "s1")

	require.NoError(t, service.SetSessionTags(sid, tagProjectA, []service.SessionTagRef{
		{Name: "bug"}, {Name: "Bug"}, {Name: "BUG"},
	}))

	tags, err := service.GetSessionTags(sid)
	require.NoError(t, err)
	assert.Equal(t, []string{"bug"}, namesOf(tags))

	all, err := service.ListSessionTags(tagProjectA)
	require.NoError(t, err)
	require.Len(t, all, 1)
	assert.Equal(t, 1, all[0].Count)
}

func TestSetSessionTags_CreatesAndLinks(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, tagProjectA, "codebuddy", "s1")

	require.NoError(t, service.SetSessionTags(sid, tagProjectA, []service.SessionTagRef{
		{Name: "bug", Scope: service.SessionTagScopeProject},
		{Name: "urgent", Scope: service.SessionTagScopeProject},
	}))

	tags, err := service.GetSessionTags(sid)
	require.NoError(t, err)
	assert.Equal(t, []string{"bug", "urgent"}, namesOf(tags))
	for _, tag := range tags {
		assert.Equal(t, service.SessionTagScopeProject, tag.Scope)
		assert.Equal(t, tagProjectA, tag.ProjectPath)
	}
}

func TestSetSessionTags_ReplacesFullSet(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, tagProjectA, "codebuddy", "s1")

	require.NoError(t, service.SetSessionTags(sid, tagProjectA, []service.SessionTagRef{
		{Name: "bug"}, {Name: "urgent"},
	}))
	// Authoritative payload: dropping "bug" must unlink it.
	require.NoError(t, service.SetSessionTags(sid, tagProjectA, []service.SessionTagRef{
		{Name: "urgent"}, {Name: "later"},
	}))

	tags, err := service.GetSessionTags(sid)
	require.NoError(t, err)
	assert.Equal(t, []string{"later", "urgent"}, namesOf(tags))
}

func TestSetSessionTags_EmptyClearsAll(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, tagProjectA, "codebuddy", "s1")
	require.NoError(t, service.SetSessionTags(sid, tagProjectA, []service.SessionTagRef{{Name: "bug"}}))

	require.NoError(t, service.SetSessionTags(sid, tagProjectA, nil))

	tags, err := service.GetSessionTags(sid)
	require.NoError(t, err)
	assert.Empty(t, tags)

	// Clearing the last link must NOT delete the registry entry — otherwise the
	// label would vanish from the candidate list just because no session uses
	// it right now.
	all, err := service.ListSessionTags(tagProjectA)
	require.NoError(t, err)
	assert.Equal(t, []string{"bug"}, namesOf(all))
}

func TestSetSessionTags_IgnoresBlankAndDuplicateNames(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, tagProjectA, "codebuddy", "s1")

	require.NoError(t, service.SetSessionTags(sid, tagProjectA, []service.SessionTagRef{
		{Name: "  bug  "},
		{Name: "bug"}, // duplicate after normalization
		{Name: "   "}, // blank
		{Name: ""},
	}))

	tags, err := service.GetSessionTags(sid)
	require.NoError(t, err)
	assert.Equal(t, []string{"bug"}, namesOf(tags))
}

func TestSetSessionTags_ExistingTagKeepsOriginalScope(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, tagProjectA, "codebuddy", "s1")

	require.NoError(t, service.SetSessionTags(sid, tagProjectA, []service.SessionTagRef{
		{Name: "shared", Scope: service.SessionTagScopeProject},
	}))
	// Re-tagging as global must NOT widen the existing project label: doing so
	// would leak it into every other project's candidate list.
	require.NoError(t, service.SetSessionTags(sid, tagProjectA, []service.SessionTagRef{
		{Name: "shared", Scope: service.SessionTagScopeGlobal},
	}))

	tags, err := service.GetSessionTags(sid)
	require.NoError(t, err)
	require.Len(t, tags, 1)
	assert.Equal(t, service.SessionTagScopeProject, tags[0].Scope)
	assert.Equal(t, tagProjectA, tags[0].ProjectPath)
}

func TestSetSessionTags_UnknownScopeDefaultsToProject(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, tagProjectA, "codebuddy", "s1")

	require.NoError(t, service.SetSessionTags(sid, tagProjectA, []service.SessionTagRef{
		{Name: "bug", Scope: "bogus"},
	}))

	tags, err := service.GetSessionTags(sid)
	require.NoError(t, err)
	require.Len(t, tags, 1)
	// Defaulting to project is the conservative choice: a typo must not make a
	// label global (visible in every project).
	assert.Equal(t, service.SessionTagScopeProject, tags[0].Scope)
}

func TestListSessionTags_ProjectIsolationAndGlobalVisibility(t *testing.T) {
	setupDB(t)
	sidA := helperCreateSession(t, tagProjectA, "codebuddy", "sA")
	sidB := helperCreateSession(t, tagProjectB, "codebuddy", "sB")

	require.NoError(t, service.SetSessionTags(sidA, tagProjectA, []service.SessionTagRef{
		{Name: "localA", Scope: service.SessionTagScopeProject},
		{Name: "shared", Scope: service.SessionTagScopeGlobal},
	}))
	require.NoError(t, service.SetSessionTags(sidB, tagProjectB, []service.SessionTagRef{
		{Name: "localB", Scope: service.SessionTagScopeProject},
	}))

	// Project A sees its own label + the global one, never B's.
	tagsA, err := service.ListSessionTags(tagProjectA)
	require.NoError(t, err)
	assert.Equal(t, []string{"locala", "shared"}, namesOf(tagsA))

	// Project B sees its own label + the global one, never A's.
	tagsB, err := service.ListSessionTags(tagProjectB)
	require.NoError(t, err)
	assert.Equal(t, []string{"localb", "shared"}, namesOf(tagsB))
}

func TestListSessionTags_GlobalTagSurvivesWithEmptyProjectPath(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, tagProjectA, "codebuddy", "s1")
	require.NoError(t, service.SetSessionTags(sid, tagProjectA, []service.SessionTagRef{
		{Name: "shared", Scope: service.SessionTagScopeGlobal},
	}))

	tags, err := service.ListSessionTags(tagProjectB)
	require.NoError(t, err)
	require.Len(t, tags, 1)
	// Stored at project_path='' so it is never matched by the project filter,
	// only by the scope='global' branch.
	assert.Equal(t, "", tags[0].ProjectPath)
	assert.Equal(t, service.SessionTagScopeGlobal, tags[0].Scope)
}

func TestListSessionTags_SameNameInTwoProjectsCoexist(t *testing.T) {
	setupDB(t)
	sidA := helperCreateSession(t, tagProjectA, "codebuddy", "sA")
	sidB := helperCreateSession(t, tagProjectB, "codebuddy", "sB")

	// Same label text, different projects: must be two rows (UNIQUE(name,
	// project_path)), and each project must only see its own.
	require.NoError(t, service.SetSessionTags(sidA, tagProjectA, []service.SessionTagRef{{Name: "bug"}}))
	require.NoError(t, service.SetSessionTags(sidB, tagProjectB, []service.SessionTagRef{{Name: "bug"}}))

	tagsA, err := service.ListSessionTags(tagProjectA)
	require.NoError(t, err)
	assert.Equal(t, []string{"bug"}, namesOf(tagsA))

	tagsB, err := service.ListSessionTags(tagProjectB)
	require.NoError(t, err)
	assert.Equal(t, []string{"bug"}, namesOf(tagsB))
}

func TestListSessionTags_CountsLinkedSessions(t *testing.T) {
	setupDB(t)
	s1 := helperCreateSession(t, tagProjectA, "codebuddy", "s1")
	s2 := helperCreateSession(t, tagProjectA, "codebuddy", "s2")

	require.NoError(t, service.SetSessionTags(s1, tagProjectA, []service.SessionTagRef{{Name: "bug"}}))
	require.NoError(t, service.SetSessionTags(s2, tagProjectA, []service.SessionTagRef{{Name: "bug"}}))

	tags, err := service.ListSessionTags(tagProjectA)
	require.NoError(t, err)
	require.Len(t, tags, 1)
	assert.Equal(t, 2, tags[0].Count)
}

func TestGetTagsForSessions_BatchAndMissingKeys(t *testing.T) {
	setupDB(t)
	s1 := helperCreateSession(t, tagProjectA, "codebuddy", "s1")
	s2 := helperCreateSession(t, tagProjectA, "codebuddy", "s2")
	s3 := helperCreateSession(t, tagProjectA, "codebuddy", "s3")

	require.NoError(t, service.SetSessionTags(s1, tagProjectA, []service.SessionTagRef{
		{Name: "beta"}, {Name: "alpha"},
	}))
	require.NoError(t, service.SetSessionTags(s2, tagProjectA, []service.SessionTagRef{{Name: "alpha"}}))

	m, err := service.GetTagsForSessions([]string{s1, s2, s3})
	require.NoError(t, err)
	// Name-ordered within a session.
	assert.Equal(t, []string{"alpha", "beta"}, namesOf(m[s1]))
	assert.Equal(t, []string{"alpha"}, namesOf(m[s2]))
	// A session with no tags must be absent (not an empty slice) — callers
	// treat a missing key as "no tags".
	_, ok := m[s3]
	assert.False(t, ok)
}

func TestGetTagsForSessions_EmptyAndDuplicateInputs(t *testing.T) {
	setupDB(t)

	m, err := service.GetTagsForSessions(nil)
	require.NoError(t, err)
	assert.Empty(t, m)

	s1 := helperCreateSession(t, tagProjectA, "codebuddy", "s1")
	require.NoError(t, service.SetSessionTags(s1, tagProjectA, []service.SessionTagRef{{Name: "alpha"}}))

	// Duplicates and empty strings must not produce duplicate rows or an SQL
	// error (the IN-list is de-duplicated before expansion).
	m, err = service.GetTagsForSessions([]string{s1, s1, "", s1})
	require.NoError(t, err)
	assert.Equal(t, []string{"alpha"}, namesOf(m[s1]))
}

func TestDeleteSessionTag_RemovesFromAllSessions(t *testing.T) {
	setupDB(t)
	s1 := helperCreateSession(t, tagProjectA, "codebuddy", "s1")
	s2 := helperCreateSession(t, tagProjectA, "codebuddy", "s2")
	require.NoError(t, service.SetSessionTags(s1, tagProjectA, []service.SessionTagRef{{Name: "bug"}, {Name: "keep"}}))
	require.NoError(t, service.SetSessionTags(s2, tagProjectA, []service.SessionTagRef{{Name: "bug"}}))

	require.NoError(t, service.DeleteSessionTag("bug", tagProjectA, service.SessionTagScopeProject))

	// The label is gone from both sessions...
	t1, err := service.GetSessionTags(s1)
	require.NoError(t, err)
	assert.Equal(t, []string{"keep"}, namesOf(t1))
	t2, err := service.GetSessionTags(s2)
	require.NoError(t, err)
	assert.Empty(t, t2)

	// ...and from the candidate registry.
	all, err := service.ListSessionTags(tagProjectA)
	require.NoError(t, err)
	assert.Equal(t, []string{"keep"}, namesOf(all))
}

func TestDeleteSessionTag_GlobalIsDeletableFromAnyProject(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, tagProjectA, "codebuddy", "s1")
	require.NoError(t, service.SetSessionTags(sid, tagProjectA, []service.SessionTagRef{
		{Name: "shared", Scope: service.SessionTagScopeGlobal},
	}))

	// A global label is visible everywhere, so any project may delete it.
	require.NoError(t, service.DeleteSessionTag("shared", tagProjectB, service.SessionTagScopeGlobal))

	tags, err := service.ListSessionTags(tagProjectA)
	require.NoError(t, err)
	assert.Empty(t, tags)
}

func TestDeleteSessionTag_CannotDeleteAnotherProjectsLabel(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, tagProjectA, "codebuddy", "s1")
	require.NoError(t, service.SetSessionTags(sid, tagProjectA, []service.SessionTagRef{{Name: "localA"}}))

	// B must not be able to delete A's label — it cannot even see it.
	err := service.DeleteSessionTag("localA", tagProjectB, service.SessionTagScopeProject)
	require.Error(t, err)

	tags, err := service.ListSessionTags(tagProjectA)
	require.NoError(t, err)
	assert.Equal(t, []string{"locala"}, namesOf(tags))
}

func TestDeleteSessionTag_UnknownNameErrors(t *testing.T) {
	setupDB(t)
	err := service.DeleteSessionTag("nope", tagProjectA, service.SessionTagScopeProject)
	require.Error(t, err)
}

func TestDeleteSessionTag_BlankNameErrors(t *testing.T) {
	setupDB(t)
	require.Error(t, service.DeleteSessionTag("   ", tagProjectA, service.SessionTagScopeProject))
}

func TestDeleteSessionTag_NormalizesName(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, tagProjectA, "codebuddy", "s1")
	require.NoError(t, service.SetSessionTags(sid, tagProjectA, []service.SessionTagRef{{Name: "bug"}}))

	// Deletion must accept the same whitespace-padded spelling the dialog may
	// hand back.
	require.NoError(t, service.DeleteSessionTag("  bug  ", tagProjectA, service.SessionTagScopeProject))

	tags, err := service.ListSessionTags(tagProjectA)
	require.NoError(t, err)
	assert.Empty(t, tags)
}

// ── Shadowing: same name as both a global and a project tag ────────────────

// TestSetSessionTags_DoesNotMigrateOntoShadowingGlobal pins the fix for a
// silent re-scope. Once a global tag with the same name appears, a global-first
// lookup would move the session onto the global definition on the next save,
// orphaning the project's own label (zero links) — after which deleting "it"
// would destroy the global tag instead.
func TestSetSessionTags_DoesNotMigrateOntoShadowingGlobal(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, tagProjectA, "codebuddy", "s1")

	// Session starts with a PROJECT-scoped "bug".
	require.NoError(t, service.SetSessionTags(sid, tagProjectA, []service.SessionTagRef{
		{Name: "bug", Scope: service.SessionTagScopeProject},
	}))

	// A global "bug" appears (created from another project).
	other := helperCreateSession(t, tagProjectB, "codebuddy", "sB")
	require.NoError(t, service.SetSessionTags(other, tagProjectB, []service.SessionTagRef{
		{Name: "bug", Scope: service.SessionTagScopeGlobal},
	}))

	// Re-saving the SAME selection must keep the session on its own project tag.
	require.NoError(t, service.SetSessionTags(sid, tagProjectA, []service.SessionTagRef{
		{Name: "bug", Scope: service.SessionTagScopeProject},
	}))

	tags, err := service.GetSessionTags(sid)
	require.NoError(t, err)
	require.Len(t, tags, 1)
	assert.Equal(t, service.SessionTagScopeProject, tags[0].Scope,
		"session must not be silently migrated onto the shadowing global tag")
}

// TestDeleteSessionTag_ProjectScopeDoesNotDestroyShadowingGlobal is the other
// half: deleting what the user saw as a project label must not remove a global
// label shared by every project (and must not leave the project label behind
// looking like the delete failed).
func TestDeleteSessionTag_ProjectScopeDoesNotDestroyShadowingGlobal(t *testing.T) {
	setupDB(t)
	sidA := helperCreateSession(t, tagProjectA, "codebuddy", "sA")
	sidB := helperCreateSession(t, tagProjectB, "codebuddy", "sB")

	require.NoError(t, service.SetSessionTags(sidA, tagProjectA, []service.SessionTagRef{
		{Name: "bug", Scope: service.SessionTagScopeProject},
	}))
	require.NoError(t, service.SetSessionTags(sidB, tagProjectB, []service.SessionTagRef{
		{Name: "bug", Scope: service.SessionTagScopeGlobal},
	}))

	// The user in project A deletes the PROJECT-scoped "bug" they can see.
	require.NoError(t, service.DeleteSessionTag("bug", tagProjectA, service.SessionTagScopeProject))

	// Project B's session must still carry the global "bug"...
	tB, err := service.GetSessionTags(sidB)
	require.NoError(t, err)
	require.Len(t, tB, 1, "global tag must survive a project-scoped delete")
	assert.Equal(t, service.SessionTagScopeGlobal, tB[0].Scope)

	// ...and A's own project label is gone (not "resurrected").
	tA, err := service.GetSessionTags(sidA)
	require.NoError(t, err)
	assert.Empty(t, tA)

	// The candidate list for A still shows the global definition (it is visible
	// everywhere) but no project-scoped "bug" remains.
	candidatesA, err := service.ListSessionTags(tagProjectA)
	require.NoError(t, err)
	require.Len(t, candidatesA, 1)
	assert.Equal(t, service.SessionTagScopeGlobal, candidatesA[0].Scope)
}

// TestDeleteSessionTag_GlobalScopeRemovesGlobalOnly is the mirror case: asking
// for the global definition deletes that one and leaves a project twin alone.
func TestDeleteSessionTag_GlobalScopeRemovesGlobalOnly(t *testing.T) {
	setupDB(t)
	sidA := helperCreateSession(t, tagProjectA, "codebuddy", "sA")
	sidB := helperCreateSession(t, tagProjectB, "codebuddy", "sB")

	require.NoError(t, service.SetSessionTags(sidA, tagProjectA, []service.SessionTagRef{
		{Name: "bug", Scope: service.SessionTagScopeProject},
	}))
	require.NoError(t, service.SetSessionTags(sidB, tagProjectB, []service.SessionTagRef{
		{Name: "bug", Scope: service.SessionTagScopeGlobal},
	}))

	require.NoError(t, service.DeleteSessionTag("bug", tagProjectA, service.SessionTagScopeGlobal))

	// The project-scoped label in A is untouched.
	tA, err := service.GetSessionTags(sidA)
	require.NoError(t, err)
	require.Len(t, tA, 1)
	assert.Equal(t, service.SessionTagScopeProject, tA[0].Scope)

	// The global label is gone from B.
	tB, err := service.GetSessionTags(sidB)
	require.NoError(t, err)
	assert.Empty(t, tB)
}

func TestDeleteSessionTagsForSession_OnlyThatSessionsLinks(t *testing.T) {
	setupDB(t)
	s1 := helperCreateSession(t, tagProjectA, "codebuddy", "s1")
	s2 := helperCreateSession(t, tagProjectA, "codebuddy", "s2")
	require.NoError(t, service.SetSessionTags(s1, tagProjectA, []service.SessionTagRef{{Name: "bug"}}))
	require.NoError(t, service.SetSessionTags(s2, tagProjectA, []service.SessionTagRef{{Name: "bug"}}))

	require.NoError(t, service.DeleteSessionTagsForSession(s1))

	t1, err := service.GetSessionTags(s1)
	require.NoError(t, err)
	assert.Empty(t, t1)
	// s2 keeps its link; the registry entry survives.
	t2, err := service.GetSessionTags(s2)
	require.NoError(t, err)
	assert.Equal(t, []string{"bug"}, namesOf(t2))
}

func TestHardDeleteSession_RemovesTagLinks(t *testing.T) {
	setupDB(t)
	s1 := helperCreateSession(t, tagProjectA, "codebuddy", "s1")
	s2 := helperCreateSession(t, tagProjectA, "codebuddy", "s2")
	require.NoError(t, service.SetSessionTags(s1, tagProjectA, []service.SessionTagRef{{Name: "bug"}}))
	require.NoError(t, service.SetSessionTags(s2, tagProjectA, []service.SessionTagRef{{Name: "bug"}}))

	require.NoError(t, service.HardDeleteSession(s1))

	// The link table has no FK to chat_sessions, so a hard delete must clean it
	// explicitly — otherwise a destroyed session leaves orphan rows behind and
	// the tag's usage count stays inflated.
	tags, err := service.GetSessionTags(s1)
	require.NoError(t, err)
	assert.Empty(t, tags)

	all, err := service.ListSessionTags(tagProjectA)
	require.NoError(t, err)
	require.Len(t, all, 1)
	assert.Equal(t, 1, all[0].Count)

	// The definition itself must survive (s2 still uses it).
	require.Len(t, all, 1)
}

// ── Tag filtering on the session list ──────────────────────────────────────

// TestListProjectTagsInUse_OnlyCountsSessionsInThisProject pins the filter-bar
// contract: it must offer only tags that a session in THIS project actually
// carries, so every chip the user can click yields a non-empty list.
func TestListProjectTagsInUse_OnlyCountsSessionsInThisProject(t *testing.T) {
	setupDB(t)
	a1 := helperCreateSession(t, tagProjectA, "codebuddy", "a1")
	a2 := helperCreateSession(t, tagProjectA, "codebuddy", "a2")
	b1 := helperCreateSession(t, tagProjectB, "codebuddy", "b1")

	require.NoError(t, service.SetSessionTags(a1, tagProjectA, []service.SessionTagRef{{Name: "bug"}}))
	require.NoError(t, service.SetSessionTags(a2, tagProjectA, []service.SessionTagRef{{Name: "bug"}}))
	require.NoError(t, service.SetSessionTags(b1, tagProjectB, []service.SessionTagRef{{Name: "onlyB"}}))
	// A global tag used only in project B must not appear for project A.
	require.NoError(t, service.SetSessionTags(b1, tagProjectB, []service.SessionTagRef{
		{Name: "globalB", Scope: service.SessionTagScopeGlobal},
	}))

	tags, err := service.ListProjectTagsInUse(tagProjectA)
	require.NoError(t, err)
	require.Len(t, tags, 1)
	assert.Equal(t, "bug", tags[0].Name)
	assert.Equal(t, 2, tags[0].Count)
}

// TestListProjectTagsInUse_ExcludesArchivedSession pins that the archived
// predicate is load-bearing: the session keeps its link while archived (which
// is exactly the state left behind by archiving), so a query missing
// `archived = 0` would still offer a chip whose filter yields an empty list.
func TestListProjectTagsInUse_ExcludesArchivedSession(t *testing.T) {
	setupDB(t)
	live := helperCreateSession(t, tagProjectA, "codebuddy", "live")
	archived := helperCreateSession(t, tagProjectA, "codebuddy", "archived")

	require.NoError(t, service.SetSessionTags(live, tagProjectA, []service.SessionTagRef{{Name: "keep"}}))
	require.NoError(t, service.SetSessionTags(archived, tagProjectA, []service.SessionTagRef{{Name: "gone"}}))
	require.NoError(t, service.ArchiveSession(tagProjectA, "codebuddy", archived))

	// The link deliberately survives archiving — assert that, so the test keeps
	// testing the predicate rather than the cleanup.
	var links int
	require.NoError(t, service.UnsafeDBForTest().QueryRow(
		"SELECT COUNT(*) FROM session_tag_links WHERE session_id = ?", archived,
	).Scan(&links))
	require.Equal(t, 1, links, "archiving must not unlink; the archived predicate is what excludes it")

	tags, err := service.ListProjectTagsInUse(tagProjectA)
	require.NoError(t, err)
	assert.Equal(t, []string{"keep"}, namesOf(tags))
}

// TestListProjectTagsInUse_ExcludesUnlinkedDefinition covers the other way a
// tag can look configured yet filter to nothing: the definition exists but no
// session links to it any more.
func TestListProjectTagsInUse_ExcludesUnlinkedDefinition(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, tagProjectA, "codebuddy", "s")
	require.NoError(t, service.SetSessionTags(sid, tagProjectA, []service.SessionTagRef{{Name: "keep"}, {Name: "dropped"}}))
	// Remove "dropped" from its only session; the definition stays registered.
	require.NoError(t, service.SetSessionTags(sid, tagProjectA, []service.SessionTagRef{{Name: "keep"}}))

	// Sanity: the definition is still a dialog candidate...
	candidates, err := service.ListSessionTags(tagProjectA)
	require.NoError(t, err)
	assert.Equal(t, []string{"dropped", "keep"}, namesOf(candidates))

	// ...but not a filter-bar option.
	tags, err := service.ListProjectTagsInUse(tagProjectA)
	require.NoError(t, err)
	assert.Equal(t, []string{"keep"}, namesOf(tags))
}

// TestGetSessionsPaged_FiltersByTag verifies the SQL predicate: only sessions
// carrying the tag are returned, and hasMore reflects the FILTERED set (not the
// unfiltered one) so pagination terminates correctly.
func TestGetSessionsPaged_FiltersByTag(t *testing.T) {
	setupDB(t)
	tagged := make([]string, 0, 3)
	for range 3 {
		sid := helperCreateSession(t, tagProjectA, "codebuddy", "t")
		require.NoError(t, service.SetSessionTags(sid, tagProjectA, []service.SessionTagRef{{Name: "bug"}}))
		tagged = append(tagged, sid)
	}
	for range 5 {
		helperCreateSession(t, tagProjectA, "codebuddy", "u")
	}

	got, hasMore, err := service.GetSessionsPaged(tagProjectA, "", 10, "", "", nil, nil, "bug")
	require.NoError(t, err)
	assert.False(t, hasMore, "hasMore must describe the filtered set")
	require.Len(t, got, 3)
	ids := []string{got[0].ID, got[1].ID, got[2].ID}
	for _, id := range tagged {
		assert.Contains(t, ids, id)
	}
}

// TestGetSessionsPaged_TagFilterPaginatesWithoutDuplicates guards the keyset
// arithmetic under a filter: a JOIN-based filter would multiply rows and break
// the "appears exactly once across pages" property.
func TestGetSessionsPaged_TagFilterPaginatesWithoutDuplicates(t *testing.T) {
	setupDB(t)
	for range 5 {
		sid := helperCreateSession(t, tagProjectA, "codebuddy", "t")
		require.NoError(t, service.SetSessionTags(sid, tagProjectA, []service.SessionTagRef{{Name: "bug"}, {Name: "extra"}}))
	}

	seen := map[string]int{}
	var cursor, cursorID string
	var cursorSortOrder *int
	var cursorPinned *bool
	for range 10 {
		got, hasMore, err := service.GetSessionsPaged(tagProjectA, "", 2, cursor, cursorID, cursorSortOrder, cursorPinned, "bug")
		require.NoError(t, err)
		for _, s := range got {
			seen[s.ID]++
		}
		if !hasMore || len(got) == 0 {
			break
		}
		last := got[len(got)-1]
		cursor = last.CreatedAt.Format("2006-01-02 15:04:05")
		cursorID = last.ID
		o := last.SortOrder
		cursorSortOrder = &o
		p := last.Pinned
		cursorPinned = &p
	}
	require.Len(t, seen, 5)
	for id, n := range seen {
		assert.Equalf(t, 1, n, "session %s must appear exactly once", id)
	}
}

// TestGetSessionsPaged_TagFilterMatchesCaseInsensitively mirrors the case
// folding of tag names: filtering by "BUG" must match a stored "bug".
func TestGetSessionsPaged_TagFilterMatchesCaseInsensitively(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, tagProjectA, "codebuddy", "t")
	require.NoError(t, service.SetSessionTags(sid, tagProjectA, []service.SessionTagRef{{Name: "bug"}}))
	helperCreateSession(t, tagProjectA, "codebuddy", "u")

	got, _, err := service.GetSessionsPaged(tagProjectA, "", 10, "", "", nil, nil, "BUG")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, sid, got[0].ID)
}

// TestGetSessionsPaged_TagFilterRequiresVisibleDefinition pins the visibility
// predicate in the filter's EXISTS clause.
//
// The outer query already restricts rows to this project, so a plain
// cross-project test cannot reach the predicate — it must be driven by a
// session in THIS project that is linked to a definition this project cannot
// see (a project-scoped tag owned by another project). That state is reachable
// when a session's project changes after tagging, so the filter must not treat
// such a link as a match. The link is written directly to model that state.
func TestGetSessionsPaged_TagFilterRequiresVisibleDefinition(t *testing.T) {
	setupDB(t)
	mine := helperCreateSession(t, tagProjectA, "codebuddy", "mine")
	visible := helperCreateSession(t, tagProjectA, "codebuddy", "visible")

	// "visible" is linked to a definition project A can see.
	require.NoError(t, service.SetSessionTags(visible, tagProjectA, []service.SessionTagRef{{Name: "shared"}}))

	// "mine" is linked to project B's project-scoped "shared" — same name, but
	// not visible from project A.
	other := helperCreateSession(t, tagProjectB, "codebuddy", "other")
	require.NoError(t, service.SetSessionTags(other, tagProjectB, []service.SessionTagRef{{Name: "shared"}}))
	var foreignTagID int64
	require.NoError(t, service.UnsafeDBForTest().QueryRow(
		"SELECT id FROM session_tags WHERE name = 'shared' AND scope = 'project' AND project_path = ?",
		tagProjectB,
	).Scan(&foreignTagID))
	_, err := service.UnsafeDBForTest().Exec(
		"INSERT INTO session_tag_links (session_id, tag_id) VALUES (?, ?)", mine, foreignTagID,
	)
	require.NoError(t, err)

	got, _, err := service.GetSessionsPaged(tagProjectA, "", 10, "", "", nil, nil, "shared")
	require.NoError(t, err)
	require.Len(t, got, 1, "only the session linked to a project-A-visible definition matches")
	assert.Equal(t, visible, got[0].ID)
}

// TestFilterSessionsByTag_UnpaginatedPath covers the limit<=0 branch, which
// filters in Go rather than SQL.
func TestFilterSessionsByTag_UnpaginatedPath(t *testing.T) {
	setupDB(t)
	tagged := helperCreateSession(t, tagProjectA, "codebuddy", "tagged")
	helperCreateSession(t, tagProjectA, "codebuddy", "plain")
	require.NoError(t, service.SetSessionTags(tagged, tagProjectA, []service.SessionTagRef{{Name: "bug"}}))

	all, err := service.GetSessions(tagProjectA, "")
	require.NoError(t, err)
	require.Len(t, all, 2)

	got, err := service.FilterSessionsByTag(all, tagProjectA, "bug")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, tagged, got[0].ID)

	// No matches is an empty slice, not an error.
	none, err := service.FilterSessionsByTag(all, tagProjectA, "nope")
	require.NoError(t, err)
	assert.Empty(t, none)
}

// TestListProjectTagsInUse_CountMatchesFilterForShadowedName pins that a chip's
// count equals the number of sessions clicking it returns, even when a global
// and a project definition share the name.
//
// The filter matches by NAME across both visible definitions (a union), so a
// count grouped per-definition would read low: with 2 sessions on the project
// "bug" and 1 on the global "bug", the chip showed "2" while the filter
// returned 3 sessions.
func TestListProjectTagsInUse_CountMatchesFilterForShadowedName(t *testing.T) {
	setupDB(t)
	a1 := helperCreateSession(t, tagProjectA, "codebuddy", "a1")
	a2 := helperCreateSession(t, tagProjectA, "codebuddy", "a2")
	require.NoError(t, service.SetSessionTags(a1, tagProjectA, []service.SessionTagRef{{Name: "bug"}}))
	require.NoError(t, service.SetSessionTags(a2, tagProjectA, []service.SessionTagRef{{Name: "bug"}}))

	// A global "bug" appears, used by one more session in this project.
	b1 := helperCreateSession(t, tagProjectB, "codebuddy", "b1")
	require.NoError(t, service.SetSessionTags(b1, tagProjectB, []service.SessionTagRef{
		{Name: "bug", Scope: service.SessionTagScopeGlobal},
	}))
	a3 := helperCreateSession(t, tagProjectA, "codebuddy", "a3")
	require.NoError(t, service.SetSessionTags(a3, tagProjectA, []service.SessionTagRef{{Name: "bug"}}))

	tags, err := service.ListProjectTagsInUse(tagProjectA)
	require.NoError(t, err)
	require.Len(t, tags, 1, "one chip per name")

	// The invariant that matters: count == what the filter yields.
	got, _, err := service.GetSessionsPaged(tagProjectA, "", 50, "", "", nil, nil, "bug")
	require.NoError(t, err)
	assert.Equal(t, len(got), tags[0].Count,
		"chip count must equal the number of sessions the filter returns")

	// And the reported scope follows the same preference as ListSessionTags
	// (global wins when both definitions share the name).
	assert.Equal(t, service.SessionTagScopeGlobal, tags[0].Scope)
}

// TestGetSessionsPaged_TagFilterMatchesGlobalTag covers the GLOBAL half of the
// visibility predicate. Every other paged-filter test uses the default
// (project) scope, so dropping the `t.scope = 'global'` branch left the suite
// green — i.e. filtering by a global tag, the entire point of that scope, was
// untested.
func TestGetSessionsPaged_TagFilterMatchesGlobalTag(t *testing.T) {
	setupDB(t)
	tagged := helperCreateSession(t, tagProjectA, "codebuddy", "tagged")
	helperCreateSession(t, tagProjectA, "codebuddy", "plain")
	require.NoError(t, service.SetSessionTags(tagged, tagProjectA, []service.SessionTagRef{
		{Name: "shared", Scope: service.SessionTagScopeGlobal},
	}))

	got, hasMore, err := service.GetSessionsPaged(tagProjectA, "", 10, "", "", nil, nil, "shared")
	require.NoError(t, err)
	require.Len(t, got, 1, "a global tag must be filterable in any project")
	assert.Equal(t, tagged, got[0].ID)
	assert.False(t, hasMore)
}

// TestGetSessionsPaged_TagFilterGlobalTagFromAnotherProject pins that a global
// tag created elsewhere still matches here — global scope means visible
// everywhere, not only in its project of origin.
func TestGetSessionsPaged_TagFilterGlobalTagFromAnotherProject(t *testing.T) {
	setupDB(t)
	// Created while tagging a session in project B.
	origin := helperCreateSession(t, tagProjectB, "codebuddy", "origin")
	require.NoError(t, service.SetSessionTags(origin, tagProjectB, []service.SessionTagRef{
		{Name: "shared", Scope: service.SessionTagScopeGlobal},
	}))

	// A project A session adopts the same global label.
	mine := helperCreateSession(t, tagProjectA, "codebuddy", "mine")
	require.NoError(t, service.SetSessionTags(mine, tagProjectA, []service.SessionTagRef{{Name: "shared"}}))

	got, _, err := service.GetSessionsPaged(tagProjectA, "", 10, "", "", nil, nil, "shared")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, mine, got[0].ID)
}

// TestFilterSessionsByTag_RequiresVisibleDefinition is the Go-side twin of the
// SQL reachability test: the unpaginated path (limit<=0) filters in memory, and
// its visibility check was unguarded — dropping it left the suite green.
//
// The state is a session in THIS project linked to a definition this project
// cannot see (another project's project-scoped tag). The outer query cannot
// exclude it, so only the visibility check can.
func TestFilterSessionsByTag_RequiresVisibleDefinition(t *testing.T) {
	setupDB(t)
	mine := helperCreateSession(t, tagProjectA, "codebuddy", "mine")
	visible := helperCreateSession(t, tagProjectA, "codebuddy", "visible")
	require.NoError(t, service.SetSessionTags(visible, tagProjectA, []service.SessionTagRef{{Name: "shared"}}))

	// Link "mine" directly to project B's project-scoped "shared".
	other := helperCreateSession(t, tagProjectB, "codebuddy", "other")
	require.NoError(t, service.SetSessionTags(other, tagProjectB, []service.SessionTagRef{{Name: "shared"}}))
	var foreignTagID int64
	require.NoError(t, service.UnsafeDBForTest().QueryRow(
		"SELECT id FROM session_tags WHERE name = 'shared' AND scope = 'project' AND project_path = ?",
		tagProjectB,
	).Scan(&foreignTagID))
	_, err := service.UnsafeDBForTest().Exec(
		"INSERT INTO session_tag_links (session_id, tag_id) VALUES (?, ?)", mine, foreignTagID,
	)
	require.NoError(t, err)

	all, err := service.GetSessions(tagProjectA, "")
	require.NoError(t, err)

	got, err := service.FilterSessionsByTag(all, tagProjectA, "shared")
	require.NoError(t, err)
	require.Len(t, got, 1, "only a project-A-visible definition counts")
	assert.Equal(t, visible, got[0].ID)
}
