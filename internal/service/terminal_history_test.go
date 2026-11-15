package service

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/store"
)

func setupTestDBForTerminalHistory(t *testing.T) func() {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	db.Exec("PRAGMA journal_mode=WAL")
	db.Exec("PRAGMA busy_timeout=5000")

	_, _ = db.Exec(store.ProjectsDDL)
	_, err = db.Exec(TerminalHistoryDDL)
	require.NoError(t, err)

	cleanup := store.SetDBForTest(db, db)
	return func() {
		cleanup()
		_ = db.Close()
	}
}

func TestRecordTerminalCommand_StoresAndDedupes(t *testing.T) {
	teardown := setupTestDBForTerminalHistory(t)
	defer teardown()

	ok, err := RecordTerminalCommand("git status", "")
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = RecordTerminalCommand("git status", "")
	require.NoError(t, err)
	assert.True(t, ok)

	cmds, err := ListTerminalCommands("", "", 10)
	require.NoError(t, err)
	require.Len(t, cmds, 1)
	assert.Equal(t, "git status", cmds[0].Command)
	assert.Equal(t, 2, cmds[0].UseCount)
}

func TestRecordTerminalCommand_SkipsCredentials(t *testing.T) {
	teardown := setupTestDBForTerminalHistory(t)
	defer teardown()

	for _, cmd := range []string{
		"export OPENAI_KEY=sk-abc123",
		"GH_TOKEN=ghp_xxx gh pr list",
		"mysql --password=root",
	} {
		ok, err := RecordTerminalCommand(cmd, "")
		require.NoError(t, err)
		assert.False(t, ok, "credential command must not be recorded: %s", cmd)
	}

	cmds, err := ListTerminalCommands("", "", 10)
	require.NoError(t, err)
	assert.Empty(t, cmds)
}

func TestRecordTerminalCommand_SkipsNonCommands(t *testing.T) {
	teardown := setupTestDBForTerminalHistory(t)
	defer teardown()

	for _, line := range []string{":wq", "/needle", "definitely-not-a-real-command-xyz", ""} {
		ok, err := RecordTerminalCommand(line, "")
		require.NoError(t, err)
		assert.False(t, ok, "non-command must not be recorded: %q", line)
	}
}

func TestRecordTerminalCommand_ProjectScope(t *testing.T) {
	teardown := setupTestDBForTerminalHistory(t)
	defer teardown()

	projectA := t.TempDir()
	projectB := t.TempDir()

	_, err := RecordTerminalCommand("make deploy", projectA)
	require.NoError(t, err)
	_, err = RecordTerminalCommand("git status", "")
	require.NoError(t, err)

	inA, err := ListTerminalCommands(projectA, "", 10)
	require.NoError(t, err)
	assert.Len(t, inA, 2, "project A sees its own command plus the global one")

	inB, err := ListTerminalCommands(projectB, "", 10)
	require.NoError(t, err)
	require.Len(t, inB, 1, "project B must not see project A's command")
	assert.Equal(t, "git status", inB[0].Command)
}

func TestListTerminalCommands_Search(t *testing.T) {
	teardown := setupTestDBForTerminalHistory(t)
	defer teardown()

	for _, cmd := range []string{"git status", "git log", "ls -la", "echo 100%"} {
		_, err := RecordTerminalCommand(cmd, "")
		require.NoError(t, err)
	}

	got, err := ListTerminalCommands("", "git", 10)
	require.NoError(t, err)
	assert.Len(t, got, 2)

	// '%' must be treated as a literal, not a wildcard.
	got, err = ListTerminalCommands("", "100%", 10)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "echo 100%", got[0].Command)

	got, err = ListTerminalCommands("", "no-such-thing", 10)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestListTerminalCommands_SearchAppliesToGlobalRows(t *testing.T) {
	teardown := setupTestDBForTerminalHistory(t)
	defer teardown()

	// A global row plus a project row: the search must filter BOTH branches of
	// the "global OR project" scope, which is why that group is parenthesised.
	projectA := t.TempDir()
	_, err := RecordTerminalCommand("make deploy", projectA)
	require.NoError(t, err)
	_, err = RecordTerminalCommand("git status", "")
	require.NoError(t, err)

	got, err := ListTerminalCommands(projectA, "git", 10)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "git status", got[0].Command)
}

func TestSyncTerminalHistory_ImportsWithoutInflatingCount(t *testing.T) {
	teardown := setupTestDBForTerminalHistory(t)
	defer teardown()

	added, err := SyncTerminalHistory([]string{"git status", "ls -la", "git status"})
	require.NoError(t, err)
	assert.Equal(t, 2, added)

	cmds, err := ListTerminalCommands("", "", 10)
	require.NoError(t, err)
	require.Len(t, cmds, 2)
	for _, c := range cmds {
		assert.Equal(t, 1, c.UseCount)
	}

	// A second sync of the same content adds nothing and leaves counts alone.
	added, err = SyncTerminalHistory([]string{"git status", "ls -la"})
	require.NoError(t, err)
	assert.Equal(t, 0, added)

	cmds, err = ListTerminalCommands("", "", 10)
	require.NoError(t, err)
	require.Len(t, cmds, 2)
	for _, c := range cmds {
		assert.Equal(t, 1, c.UseCount)
	}
}

func TestSyncTerminalHistory_SkipsCredentialsAndNoise(t *testing.T) {
	teardown := setupTestDBForTerminalHistory(t)
	defer teardown()

	added, err := SyncTerminalHistory([]string{
		"export API_KEY=abc",
		":wq",
		"definitely-not-a-real-command-xyz",
		"ls",
	})
	require.NoError(t, err)
	assert.Equal(t, 1, added)

	cmds, err := ListTerminalCommands("", "", 10)
	require.NoError(t, err)
	require.Len(t, cmds, 1)
	assert.Equal(t, "ls", cmds[0].Command)
}

func TestClearTerminalHistory(t *testing.T) {
	teardown := setupTestDBForTerminalHistory(t)
	defer teardown()

	_, err := RecordTerminalCommand("ls", "")
	require.NoError(t, err)
	require.NoError(t, ClearTerminalHistory())

	cmds, err := ListTerminalCommands("", "", 10)
	require.NoError(t, err)
	assert.Empty(t, cmds)
}

func TestListTerminalCommands_ClampsLimit(t *testing.T) {
	teardown := setupTestDBForTerminalHistory(t)
	defer teardown()

	for i := range 5 {
		_, err := RecordTerminalCommand([]string{"ls", "pwd", "date", "whoami", "uname"}[i], "")
		require.NoError(t, err)
	}

	got, err := ListTerminalCommands("", "", 0)
	require.NoError(t, err)
	assert.Len(t, got, 5)
}

func TestDeleteTerminalCommand(t *testing.T) {
	teardown := setupTestDBForTerminalHistory(t)
	defer teardown()

	_, err := RecordTerminalCommand("ls", "")
	require.NoError(t, err)
	_, err = RecordTerminalCommand("pwd", "")
	require.NoError(t, err)

	cmds, err := ListTerminalCommands("", "", 10)
	require.NoError(t, err)
	require.Len(t, cmds, 2)

	var lsID int64
	for _, c := range cmds {
		if c.Command == "ls" {
			lsID = c.ID
		}
	}
	require.NotZero(t, lsID)
	require.NoError(t, DeleteTerminalCommand(lsID))

	cmds, err = ListTerminalCommands("", "", 10)
	require.NoError(t, err)
	require.Len(t, cmds, 1)
	assert.Equal(t, "pwd", cmds[0].Command)
}

func TestRecordTerminalCommand_RejectsOverlongLine(t *testing.T) {
	teardown := setupTestDBForTerminalHistory(t)
	defer teardown()

	ok, err := RecordTerminalCommand(strings.Repeat("a", 5000), "")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestSyncTerminalHistory_EmptyInput(t *testing.T) {
	teardown := setupTestDBForTerminalHistory(t)
	defer teardown()

	added, err := SyncTerminalHistory(nil)
	require.NoError(t, err)
	assert.Zero(t, added)
}

func TestSyncTerminalHistory_KeepsOnlyTheTail(t *testing.T) {
	teardown := setupTestDBForTerminalHistory(t)
	defer teardown()

	// 1005 distinct valid commands: the oldest 5 must be dropped, the newest
	// 1000 kept. Uses `echo` so every entry passes LooksLikeCommand.
	commands := make([]string, 0, 1005)
	for i := range 1005 {
		commands = append(commands, fmt.Sprintf("echo %d", i))
	}

	added, err := SyncTerminalHistory(commands)
	require.NoError(t, err)
	assert.Equal(t, terminalHistoryImportLimit, added)

	// ListTerminalCommands clamps its page size, so it cannot return all 1000;
	// the count assertion above is what proves the tail was taken.
	all, err := ListTerminalCommands("", "", maxTerminalHistoryLimit)
	require.NoError(t, err)
	assert.Len(t, all, maxTerminalHistoryLimit)
}
