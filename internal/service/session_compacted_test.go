package service

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"clawbench/internal/ai"
	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInitDB_MigratesSessionCompactedColumn drives the REAL migration: it builds
// a chat_sessions table without the compacted column, runs InitDB, and asserts
// the column exists and defaults to 0. Writing the ALTER by hand here instead
// would pass even if the production migration were deleted.
func TestInitDB_MigratesSessionCompactedColumn(t *testing.T) {
	dbDir := t.TempDir()
	clawDir := filepath.Join(dbDir, ".clawbench")
	require.NoError(t, os.MkdirAll(clawDir, 0o755))
	dbPath := filepath.Join(clawDir, "ClawBench.db")

	oldDB, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	// Old schema: chat_sessions WITHOUT compacted (and without every column the
	// later migrations add, so InitDB has real work to do).
	_, err = oldDB.Exec(`
		CREATE TABLE chat_sessions (
			id TEXT PRIMARY KEY,
			project_path TEXT NOT NULL,
			backend TEXT NOT NULL,
			title TEXT NOT NULL,
			session_type TEXT NOT NULL DEFAULT 'chat',
			archived INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(project_path, backend, id)
		);
	`)
	require.NoError(t, err)
	_, err = oldDB.Exec(`INSERT INTO chat_sessions (id, project_path, backend, title) VALUES ('legacy', '/p', 'codebuddy', 'T')`)
	require.NoError(t, err)
	require.NoError(t, oldDB.Close())

	require.NoError(t, initTestDB(dbDir))
	defer func() {
		db.Close()
		dbRead.Close()
	}()

	var hasCompacted int
	require.NoError(t, db.QueryRow(
		"SELECT COUNT(*) FROM pragma_table_info('chat_sessions') WHERE name='compacted'",
	).Scan(&hasCompacted))
	require.Equal(t, 1, hasCompacted, "InitDB must add the compacted column")

	// A pre-existing row must default to 0 (not compacted) so upgrading users do
	// not suddenly get an extra prompt injection on their next message.
	var legacy int
	require.NoError(t, db.QueryRow("SELECT compacted FROM chat_sessions WHERE id = 'legacy'").Scan(&legacy))
	assert.Equal(t, 0, legacy, "pre-existing sessions must default to not-compacted")
}

// TestSessionExecutor_CompactDetectedPersistsFlag pins the wiring from the
// internal compact_detected signal to the DB flag: the executor must persist it
// and swallow the event (it is not client-visible content).
func TestSessionExecutor_CompactDetectedPersistsFlag(t *testing.T) {
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")
	ctx := context.Background()
	cfg := RunConfig{
		Mode:        ModeScheduled,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   sid,
		AgentID:     "test-agent",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}

	events := []ai.StreamEvent{
		{Type: "compact_detected", Content: "acp_meta"},
		{Type: "content", Content: "response"},
		{Type: "done"},
	}
	ch := make(chan ai.StreamEvent, len(events))
	for _, e := range events {
		ch <- e
	}
	close(ch)

	executor := NewSessionExecutor(ctx, cfg)
	result := executor.RunWithChannel(ch)
	require.True(t, result.ReceivedTerminal)

	// The signal must have set the flag (and the event must not have been
	// accumulated as content).
	assert.True(t, ConsumeSessionCompacted(sid),
		"compact_detected must persist the compacted flag for the next turn")

	// The internal signal must never appear in the stored message content.
	var content string
	require.NoError(t, dbRead.QueryRow(
		"SELECT content FROM chat_history WHERE session_id = ? ORDER BY id DESC LIMIT 1", sid,
	).Scan(&content))
	assert.NotContains(t, content, "compact_detected", "the internal signal must not reach message content")
	assert.Contains(t, content, "response", "the real content must still be accumulated")
}

// TestSessionExecutor_CompactDetectedUnknownSessionDoesNotPanic covers the
// best-effort contract: a signal for a session row that no longer exists (e.g.
// deleted mid-turn) must not fail the turn.
func TestSessionExecutor_CompactDetectedUnknownSessionDoesNotPanic(t *testing.T) {
	setupExecutorDB(t)
	MarkSessionCompacted("session-that-does-not-exist")
	assert.False(t, ConsumeSessionCompacted("session-that-does-not-exist"))
}

// TestSessionExecutor_CompactDetectedNotConsumedByItsOwnTurn pins the ordering
// that makes the feature work: the signal arrives DURING the compacting turn, so
// that same turn's request must not consume it — the re-injection belongs to the
// turn that follows.
func TestSessionExecutor_CompactDetectedNotConsumedByItsOwnTurn(t *testing.T) {
	setupExecutorDB(t)
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Name: "Test", Backend: "test"},
	}
	defer func() { model.Agents = nil }()

	sid := setupExecutorSession(t, "test-agent")
	ctx := context.Background()
	cfg := RunConfig{
		Mode:        ModeScheduled,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   sid,
		AgentID:     "test-agent",
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	}

	events := []ai.StreamEvent{
		{Type: "compact_detected", Content: "acp_meta"},
		{Type: "content", Content: "response"},
		{Type: "done"},
	}
	ch := make(chan ai.StreamEvent, len(events))
	for _, e := range events {
		ch <- e
	}
	close(ch)

	executor := NewSessionExecutor(ctx, cfg)
	executor.RunWithChannel(ch)

	// A request built AFTER the turn must observe the flag.
	req := BuildChatRequest("continue", sid, "/test", "test", "test-agent", "", "", "", "", "/test", false)
	assert.True(t, req.Compacted, "the turn after the compaction must re-inject")
}

func TestMarkAndConsumeSessionCompacted(t *testing.T) {
	dbDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dbDir, ".clawbench"), 0o755))
	require.NoError(t, initTestDB(dbDir))
	defer func() {
		db.Close()
		dbRead.Close()
	}()

	_, err := db.Exec(`INSERT INTO chat_sessions (id, project_path, backend, title) VALUES ('s1', '/p', 'codebuddy', 'T')`)
	require.NoError(t, err)

	// Fresh session: nothing to consume.
	assert.False(t, ConsumeSessionCompacted("s1"), "a session that never compacted must not report one")

	MarkSessionCompacted("s1")

	// The requirement is "inject on the NEXT message only": the first consume
	// observes the flag and clears it, so the following turn does NOT re-inject.
	assert.True(t, ConsumeSessionCompacted("s1"), "the turn right after compaction must re-inject")
	assert.False(t, ConsumeSessionCompacted("s1"), "the flag must be consumed by exactly one turn")

	// A second compaction later in the same session must work again.
	MarkSessionCompacted("s1")
	assert.True(t, ConsumeSessionCompacted("s1"), "a later compaction must re-arm the flag")

	// Unknown session: must not panic or report a compaction.
	assert.False(t, ConsumeSessionCompacted("does-not-exist"))
}

// TestConsumeSessionCompacted_ConcurrentConsumersOnlyOneWins pins the atomicity
// of the read-and-clear. Two sends racing on the same session (e.g. a direct
// send and a queue drain) must not BOTH see the flag — that would inject the
// system prompt twice, which is exactly what "next message only" forbids.
func TestConsumeSessionCompacted_ConcurrentConsumersOnlyOneWins(t *testing.T) {
	dbDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dbDir, ".clawbench"), 0o755))
	require.NoError(t, initTestDB(dbDir))
	defer func() {
		db.Close()
		dbRead.Close()
	}()

	_, err := db.Exec(`INSERT INTO chat_sessions (id, project_path, backend, title) VALUES ('race', '/p', 'codebuddy', 'T')`)
	require.NoError(t, err)
	MarkSessionCompacted("race")

	const consumers = 8
	results := make(chan bool, consumers)
	for range consumers {
		go func() { results <- ConsumeSessionCompacted("race") }()
	}
	wins := 0
	for range consumers {
		if <-results {
			wins++
		}
	}
	assert.Equal(t, 1, wins, "exactly one concurrent consumer may observe the compaction")
}
