package service

import (
	"testing"

	"clawbench/internal/store"
)

// MigrateGroupMemberDisabled converts the legacy "removed member" representation
// (archived=1 on a group_member row) into the reversible disabled flag, and must
// leave every other archived row alone.
func TestMigrateGroupMemberDisabled(t *testing.T) {
	setupGroupDB(t)
	project := "/tmp/grouptest-disabled-migration"
	if _, err := store.ProjectIDForPath(project); err != nil {
		t.Fatalf("ProjectIDForPath: %v", err)
	}
	groupID, hostID, err := CreateGroup(project, "G", "codebuddy", "agent-host", "Host")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	// Two plain members: one is left ACTIVE, one is set to the legacy archived
	// shape directly (a pre-disable database).
	activeID, err := AddGroupMember(project, groupID, "claude", "agent-a", "A")
	if err != nil {
		t.Fatalf("AddGroupMember: %v", err)
	}
	legacyID, err := AddGroupMember(project, groupID, "claude", "agent-b", "B")
	if err != nil {
		t.Fatalf("AddGroupMember: %v", err)
	}
	if _, err := store.WriteExec("UPDATE chat_sessions SET archived = 1 WHERE id = ?", legacyID); err != nil {
		t.Fatalf("simulate legacy archived member: %v", err)
	}
	// An archived CHAT session must keep its archived flag: the user archived
	// that conversation, which is a different concept.
	chatID, err := CreateSession(project, "codebuddy", "archived chat", "codebuddy", "", "default", "chat")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := ArchiveSession(project, "codebuddy", chatID); err != nil {
		t.Fatalf("ArchiveSession: %v", err)
	}

	if !MigrateGroupMemberDisabled() {
		t.Fatal("migration must report success")
	}

	var archived, disabled int
	if err := store.ReadDB().QueryRow(
		"SELECT archived, disabled FROM chat_sessions WHERE id = ?", legacyID,
	).Scan(&archived, &disabled); err != nil {
		t.Fatalf("read converted member: %v", err)
	}
	if archived != 0 || disabled != 1 {
		t.Fatalf("legacy member must become archived=0 disabled=1, got archived=%d disabled=%d", archived, disabled)
	}

	// The already-active member and the host must be untouched.
	for _, id := range []string{activeID, hostID} {
		var a, m int
		if err := store.ReadDB().QueryRow("SELECT archived, disabled FROM chat_sessions WHERE id = ?", id).Scan(&a, &m); err != nil {
			t.Fatalf("read member %s: %v", id, err)
		}
		if a != 0 || m != 0 {
			t.Fatalf("member %s must stay active and enabled, got archived=%d disabled=%d", id, a, m)
		}
	}

	// The archived CHAT keeps archived=1 and is not disabled.
	var chatArchived, chatDisabled int
	if err := store.ReadDB().QueryRow(
		"SELECT archived, disabled FROM chat_sessions WHERE id = ?", chatID,
	).Scan(&chatArchived, &chatDisabled); err != nil {
		t.Fatalf("read chat session: %v", err)
	}
	if chatArchived != 1 || chatDisabled != 0 {
		t.Fatalf("an archived chat must not be converted: archived=%d disabled=%d", chatArchived, chatDisabled)
	}

	// Idempotent: a second run changes nothing.
	if !MigrateGroupMemberDisabled() {
		t.Fatal("second run must report success")
	}
	if err := store.ReadDB().QueryRow("SELECT archived, disabled FROM chat_sessions WHERE id = ?", legacyID).Scan(&archived, &disabled); err != nil {
		t.Fatalf("re-read converted member: %v", err)
	}
	if archived != 0 || disabled != 1 {
		t.Fatalf("second run must be a no-op, got archived=%d disabled=%d", archived, disabled)
	}
}
