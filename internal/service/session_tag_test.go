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
	assert.Equal(t, []string{"localA", "shared"}, namesOf(tagsA))

	// Project B sees its own label + the global one, never A's.
	tagsB, err := service.ListSessionTags(tagProjectB)
	require.NoError(t, err)
	assert.Equal(t, []string{"localB", "shared"}, namesOf(tagsB))
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

	require.NoError(t, service.DeleteSessionTag("bug", tagProjectA))

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
	require.NoError(t, service.DeleteSessionTag("shared", tagProjectB))

	tags, err := service.ListSessionTags(tagProjectA)
	require.NoError(t, err)
	assert.Empty(t, tags)
}

func TestDeleteSessionTag_CannotDeleteAnotherProjectsLabel(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, tagProjectA, "codebuddy", "s1")
	require.NoError(t, service.SetSessionTags(sid, tagProjectA, []service.SessionTagRef{{Name: "localA"}}))

	// B must not be able to delete A's label — it cannot even see it.
	err := service.DeleteSessionTag("localA", tagProjectB)
	require.Error(t, err)

	tags, err := service.ListSessionTags(tagProjectA)
	require.NoError(t, err)
	assert.Equal(t, []string{"localA"}, namesOf(tags))
}

func TestDeleteSessionTag_UnknownNameErrors(t *testing.T) {
	setupDB(t)
	err := service.DeleteSessionTag("nope", tagProjectA)
	require.Error(t, err)
}

func TestDeleteSessionTag_BlankNameErrors(t *testing.T) {
	setupDB(t)
	require.Error(t, service.DeleteSessionTag("   ", tagProjectA))
}

func TestDeleteSessionTag_NormalizesName(t *testing.T) {
	setupDB(t)
	sid := helperCreateSession(t, tagProjectA, "codebuddy", "s1")
	require.NoError(t, service.SetSessionTags(sid, tagProjectA, []service.SessionTagRef{{Name: "bug"}}))

	// Deletion must accept the same whitespace-padded spelling the dialog may
	// hand back.
	require.NoError(t, service.DeleteSessionTag("  bug  ", tagProjectA))

	tags, err := service.ListSessionTags(tagProjectA)
	require.NoError(t, err)
	assert.Empty(t, tags)
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
