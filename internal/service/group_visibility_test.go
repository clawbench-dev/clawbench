package service

import (
	"testing"

	"clawbench/internal/store"
)

// TestGroupVisibilityAcrossListSearchCount verifies decision #37: groups are
// user-visible (appear in list/search/overview and count toward the session
// limit) while hidden members are not.
func TestGroupVisibilityAcrossListSearchCount(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/grouplist"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}

	groupID, hostMemberID, err := CreateGroup(project, "G", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	memberID, err := AddGroupMember(project, groupID, "claude", "agent-a", "A")
	if err != nil {
		t.Fatalf("AddGroupMember: %v", err)
	}

	// GetSessions: group present, members absent.
	sessions, err := GetSessions(project, "")
	if err != nil {
		t.Fatalf("GetSessions: %v", err)
	}
	ids := map[string]bool{}
	for _, s := range sessions {
		ids[s.ID] = true
	}
	if !ids[groupID] {
		t.Fatal("group must appear in GetSessions")
	}
	if ids[hostMemberID] || ids[memberID] {
		t.Fatal("group members must NOT appear in GetSessions")
	}

	// GetSessionCount: group counts, members do not.
	count, err := GetSessionCount(project)
	if err != nil {
		t.Fatalf("GetSessionCount: %v", err)
	}
	if count != 1 {
		t.Fatalf("session count=%d, want 1 (group counts, members do not)", count)
	}
}

// TestGroupAppearsInBrowseAndSearch covers the parameterized store queries
// (GetRecentSessions / SearchSessionsByTitle), which the literal-literal grep
// approach would have missed.
func TestGroupAppearsInBrowseAndSearch(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/groupbrowse"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, hostMemberID, err := CreateGroup(project, "Browse Group", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}

	recent, _, err := store.GetRecentSessions(project, 0, "", "", "", "", "", "", "")
	if err != nil {
		t.Fatalf("GetRecentSessions: %v", err)
	}
	seenGroup := false
	for _, s := range recent {
		if s.ID == groupID {
			seenGroup = true
		}
		if s.ID == hostMemberID {
			t.Fatal("member must not appear in browse")
		}
	}
	if !seenGroup {
		t.Fatal("group must appear in GetRecentSessions")
	}

	found, err := store.SearchSessionsByTitle(project, []string{"Browse"}, 0, "", "", "", "", "", "")
	if err != nil {
		t.Fatalf("SearchSessionsByTitle: %v", err)
	}
	hit := false
	for _, s := range found {
		if s.ID == groupID {
			hit = true
		}
	}
	if !hit {
		t.Fatal("group must be searchable by title")
	}
}
