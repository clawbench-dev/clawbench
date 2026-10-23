package service_test

import (
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/store"

	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// setupTestDBForSessionSharePayload creates an in-memory DB with every table the
// snapshot builder reads: chat_sessions, chat_history, chat_tool_calls,
// chat_thinking and summaries.
func setupTestDBForSessionSharePayload(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)

	for _, ddl := range []string{
		`CREATE TABLE IF NOT EXISTS projects (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	path TEXT NOT NULL,
	forge_bind_opt_out INTEGER NOT NULL DEFAULT 0,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(path)
);
CREATE TABLE IF NOT EXISTS chat_sessions (
			id TEXT PRIMARY KEY,
			project_id INTEGER NOT NULL DEFAULT 0,
			backend TEXT NOT NULL DEFAULT '',
			title TEXT NOT NULL DEFAULT '',
			agent_name TEXT NOT NULL DEFAULT '',
			agent_avatar TEXT NOT NULL DEFAULT '',
			agent_id TEXT NOT NULL DEFAULT '',
			agent_source TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL DEFAULT '',
			transport TEXT NOT NULL DEFAULT '',
			auto_approve INTEGER NOT NULL DEFAULT 0,
			archived INTEGER NOT NULL DEFAULT 0,
			session_type TEXT NOT NULL DEFAULT 'chat',
			group_id TEXT NOT NULL DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			context_state TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS chat_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL DEFAULT 0,
			role TEXT NOT NULL,
			content TEXT NOT NULL DEFAULT '',
			files TEXT,
			session_id TEXT NOT NULL DEFAULT '',
			backend TEXT NOT NULL DEFAULT '',
			agent_id TEXT DEFAULT '',
			injected INTEGER NOT NULL DEFAULT 0,
			streaming INTEGER NOT NULL DEFAULT 0,
			indexed INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS chat_tool_calls (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			message_id INTEGER NOT NULL,
			session_id TEXT NOT NULL DEFAULT '',
			tool_id TEXT NOT NULL,
			name TEXT NOT NULL DEFAULT '',
			input TEXT NOT NULL DEFAULT '{}',
			output TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT '',
			done INTEGER NOT NULL DEFAULT 0,
			summary TEXT NOT NULL DEFAULT '',
			duration_ms INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(tool_id, message_id)
		)`,
		`CREATE TABLE IF NOT EXISTS chat_thinking (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			message_id INTEGER NOT NULL,
			session_id TEXT NOT NULL DEFAULT '',
			think_id TEXT NOT NULL,
			seq INTEGER NOT NULL DEFAULT 0,
			text TEXT NOT NULL DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(think_id, message_id, seq)
		)`,
		`CREATE TABLE IF NOT EXISTS summaries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			target_type TEXT NOT NULL,
			target_id INTEGER NOT NULL,
			summary TEXT NOT NULL DEFAULT '',
			summary_cards TEXT NOT NULL DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(target_type, target_id)
		)`,
		`CREATE TABLE IF NOT EXISTS queued_messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			project_id INTEGER NOT NULL DEFAULT 0,
			backend TEXT NOT NULL DEFAULT '',
			queue_id TEXT NOT NULL,
			content TEXT NOT NULL,
			files TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
	} {
		_, err = db.Exec(ddl)
		require.NoError(t, err)
	}

	// The payload derives projectRoot from chat_sessions -> projects, so the row
	// must exist. (Pre-live-assembly the root was passed in by the handler, which
	// is why this seed is new.)
	_, err = db.Exec(`INSERT INTO projects (id, path) VALUES (?, ?)`, testProjectID, testProjectRoot)
	require.NoError(t, err)

	cleanup := store.SetDBForTest(db, db)
	t.Cleanup(cleanup)
	return db
}

const testProjectRoot = "/home/u/proj"

// testProjectID is the projects row every fixture session points at. The
// assembled payload derives projectRoot from that JOIN (an anonymous request has
// no project cookie), so the row must exist and carry testProjectRoot.
const testProjectID = 1

// testShareCreatedAt is the share row's timestamp, passed into every Build call.
// It is a fixed instant (not time.Now) because the assembled bytes have to be
// stable across requests — that is what the ETag the handler derives depends on.
var testShareCreatedAt = time.Date(2026, 10, 22, 9, 30, 0, 0, time.UTC)

// seedSession inserts a chat_sessions row. sessionID stays a parameter so the
// helper mirrors the schema rather than hard-coding one id.
func seedSession(t *testing.T, db *sql.DB, sessionID string) { //nolint:unparam // general-purpose seed helper; all current callers use "s1"
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO chat_sessions (id, project_id, backend, title, agent_id, model)
		 VALUES (?, ?, 'codebuddy', 'Fix the login bug', 'codebuddy', 'claude-sonnet-4')`,
		sessionID, testProjectID,
	)
	require.NoError(t, err)
}

// seedGroupSession seeds a session whose session_type is 'group', so the
// protocol-stripping branches (which are gated on the type) take effect.
func seedGroupSession(t *testing.T, db *sql.DB, sessionID string) {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO chat_sessions (id, project_id, backend, title, agent_id, model, session_type)
		 VALUES (?, ?, 'codebuddy', 'Group chat', '', '', 'group')`,
		sessionID, testProjectID,
	)
	require.NoError(t, err)
}

// seedMessage inserts a finalized message and returns its id. sessionID stays a
// parameter so the helper mirrors the schema rather than hard-coding one id.
//
// queued != 0 seeds a queued message instead: those no longer live in
// chat_history (the dedicated queued_messages table holds them until dequeue),
// so the snapshot must exclude them simply because they are not history rows.
func seedMessage(t *testing.T, db *sql.DB, sessionID, role, content string, streaming, queued int) int64 {
	t.Helper()
	if queued != 0 {
		res, err := db.Exec(
			`INSERT INTO queued_messages (project_id, session_id, backend, queue_id, content)
			 VALUES (?, ?, 'codebuddy', ?, ?)`,
			testProjectID, sessionID, content, content,
		)
		require.NoError(t, err)
		id, err := res.LastInsertId()
		require.NoError(t, err)
		return id
	}
	res, err := db.Exec(
		`INSERT INTO chat_history (project_id, session_id, role, content, backend, streaming)
		 VALUES (?, ?, ?, ?, 'codebuddy', ?)`,
		testProjectID, sessionID, role, content, streaming,
	)
	require.NoError(t, err)
	id, err := res.LastInsertId()
	require.NoError(t, err)
	return id
}

// buildAllForSession assembles the payload for a session's ENTIRE finalized
// history. BuildSessionSharePayload takes the resolved selection (creation
// resolves it), so tests that want "everything" must resolve first.
func buildAllForSession(t *testing.T, sessionID string) (string, int, error) {
	t.Helper()
	ids, err := service.ResolveSessionShareSelection(sessionID, nil)
	require.NoError(t, err)
	return service.BuildSessionSharePayload(sessionID, ids, testShareCreatedAt)
}

// decodePayload parses the snapshot and returns it as a generic map so tests can
// assert on the wire shape (not on Go structs, which would hide field-name bugs).
func decodePayload(t *testing.T, raw string) map[string]any {
	t.Helper()
	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &out))
	return out
}

func payloadMessages(t *testing.T, raw string) []map[string]any {
	t.Helper()
	out := decodePayload(t, raw)
	msgs, ok := out["messages"].([]any)
	require.True(t, ok, "payload must carry a messages array")
	res := make([]map[string]any, 0, len(msgs))
	for _, m := range msgs {
		res = append(res, m.(map[string]any))
	}
	return res
}

// contentBlocks extracts the blocks array of a message's JSON content string.
func contentBlocks(t *testing.T, msg map[string]any) []map[string]any {
	t.Helper()
	content, ok := msg["content"].(string)
	require.True(t, ok, "content must stay a JSON string")
	var wrapper struct {
		Blocks []map[string]any `json:"blocks"`
	}
	require.NoError(t, json.Unmarshal([]byte(content), &wrapper))
	return wrapper.Blocks
}

// THE core regression: chat_history stores tool_use blocks with input/output
// stripped and thinking blocks with only a think_id. A snapshot that does not
// re-inline them from the side tables renders empty tool cards and thinking
// chips with no text.
// The cap must be enforced by BuildSessionSharePayload itself, not merely
// available as a helper: the trimmer being correct is useless if the entry
// point stops calling it. Asserts on the returned bytes, which is what the
// handler stores.
func TestSessionSharePayload_TrimsOverCapAtTheEntryPoint(t *testing.T) {
	db := setupTestDBForSessionSharePayload(t)
	defer func() { _ = db.Close() }()

	seedSession(t, db, "s1")
	huge := strings.Repeat("x", 18<<20) // 18 MiB, over the 16 MiB cap on its own
	msgID := seedMessage(t, db, "s1", "assistant", `{"blocks":[
		{"type":"tool_use","id":"toolu_cap","name":"Read","status":"success","done":true}
	],"metadata":{}}`, 0, 0)

	_, err := db.Exec(
		`INSERT INTO chat_tool_calls (message_id, session_id, tool_id, name, input, output, status, done)
		 VALUES (?, 's1', 'toolu_cap', 'Read', '{}', ?, 'success', 1)`,
		msgID, huge,
	)
	require.NoError(t, err)

	raw, _, err := buildAllForSession(t, "s1")
	require.NoError(t, err)

	require.LessOrEqual(t, len(raw), 16<<20,
		"the entry point must enforce the cap, not just the helper")

	// The trim must reach the stored bytes: marker present, block flagged.
	blocks := contentBlocks(t, payloadMessages(t, raw)[0])
	require.NotEmpty(t, blocks)
	assert.Equal(t, true, blocks[0]["truncated"],
		"the viewer needs the flag to say the content was cut")
	assert.Contains(t, blocks[0]["output"], "truncated for sharing",
		"the output must carry the marker so the cut is visible")
}

// Summaries, summary cards and inline tool inputs are carried VERBATIM. These
// used to assert relativized paths ("the project root must not appear anywhere"),
// which pinned a sanitizer that could never be complete: it only rewrote strings
// matching the project root / home dir, so tool output, credentials and file
// contents sailed past it. Serving the transcript as-is is the honest contract.
func TestSessionSharePayload_ServesSummaryTextVerbatim(t *testing.T) {
	db := setupTestDBForSessionSharePayload(t)
	defer func() { _ = db.Close() }()

	seedSession(t, db, "s1")
	msgID := seedMessage(t, db, "s1", "assistant", `{"blocks":[{"type":"text","text":"done"}],"metadata":{}}`, 0, 0)

	const summary = "I edited /home/u/proj/src/secret.ts and read /home/u/.ssh/id_rsa"
	_, err := db.Exec(
		`INSERT INTO summaries (target_type, target_id, summary) VALUES ('chat_message', ?, ?)`,
		msgID, summary,
	)
	require.NoError(t, err)

	raw, _, err := buildAllForSession(t, "s1")
	require.NoError(t, err)

	msg := payloadMessages(t, raw)[0]
	got, _ := msg["summary"].(string)
	assert.Equal(t, summary, got, "the summary must be exactly what the agent wrote")
}

func TestSessionSharePayload_ServesSummaryCardInputsVerbatim(t *testing.T) {
	db := setupTestDBForSessionSharePayload(t)
	defer func() { _ = db.Close() }()

	seedSession(t, db, "s1")
	msgID := seedMessage(t, db, "s1", "assistant", `{"blocks":[{"type":"text","text":"done"}],"metadata":{}}`, 0, 0)

	cards := `{
		"tools": [{"name":"Read","id":"t1","input":{"file_path":"/home/u/proj/src/a.ts","command":"cat /home/u/.ssh/id_rsa"}}],
		"askQuestions": [{
			"header":"Pick","multiSelect":false,
			"question":"should I edit /home/u/proj/src/secret.ts ?",
			"options":[{"label":"/home/u/proj/src/x.ts","description":"touch /home/u/proj/src/y.ts"}]
		}]
	}`
	_, err := db.Exec(
		`INSERT INTO summaries (target_type, target_id, summary, summary_cards)
		 VALUES ('chat_message', ?, 'a summary', ?)`,
		msgID, cards,
	)
	require.NoError(t, err)

	raw, _, err := buildAllForSession(t, "s1")
	require.NoError(t, err)

	assert.Contains(t, raw, "/home/u/proj/src/a.ts")
	assert.Contains(t, raw, "cat /home/u/.ssh/id_rsa")
	assert.Contains(t, raw, "/home/u/proj/src/secret.ts")
	assert.Contains(t, raw, "/home/u/proj/src/y.ts")
}

// A tool_use block's INLINE input (AskUserQuestion / PermissionApproval keep it
// inline and have no chat_tool_calls row) must survive for the card to render.
func TestSessionSharePayload_KeepsInteractiveToolInlineInput(t *testing.T) {
	db := setupTestDBForSessionSharePayload(t)
	defer func() { _ = db.Close() }()

	seedSession(t, db, "s1")
	// Inline input, and deliberately NO chat_tool_calls row.
	seedMessage(t, db, "s1", "assistant", `{"blocks":[
		{"type":"tool_use","id":"ask1","name":"AskUserQuestion","done":true,
		 "input":{"toolInput":{"command":"cat /home/u/.ssh/id_rsa"},"file_path":"/home/u/proj/src/a.ts"}}
	],"metadata":{}}`, 0, 0)

	raw, _, err := buildAllForSession(t, "s1")
	require.NoError(t, err)

	blocks := contentBlocks(t, payloadMessages(t, raw)[0])
	require.NotEmpty(t, blocks)
	input, ok := blocks[0]["input"].(map[string]any)
	require.True(t, ok, "the inline input must survive for the card to render")
	toolInput, _ := input["toolInput"].(map[string]any)
	assert.Equal(t, "cat /home/u/.ssh/id_rsa", toolInput["command"])
	assert.Equal(t, "/home/u/proj/src/a.ts", input["file_path"])
}

func TestSessionSharePayload_InlinesToolIOAndThinkingText(t *testing.T) {
	db := setupTestDBForSessionSharePayload(t)
	defer func() { _ = db.Close() }()

	seedSession(t, db, "s1")
	msgID := seedMessage(t, db, "s1", "assistant", `{"blocks":[
		{"type":"thinking","think_id":"th_1","done":true},
		{"type":"tool_use","id":"toolu_1","name":"Read","status":"success","done":true},
		{"type":"text","text":"Done."}
	],"metadata":{"model":"claude-sonnet-4"}}`, 0, 0)

	// The slim stored block has no input/output; the side table is authoritative.
	_, err := db.Exec(
		`INSERT INTO chat_tool_calls (message_id, session_id, tool_id, name, input, output, status, done, summary, duration_ms)
		 VALUES (?, 's1', 'toolu_1', 'Read', ?, ?, 'success', 1, 'Read a file', 42)`,
		msgID, `{"file_path":"/home/u/proj/src/a.ts"}`, "file contents here",
	)
	require.NoError(t, err)

	_, err = db.Exec(
		`INSERT INTO chat_thinking (message_id, session_id, think_id, seq, text) VALUES (?, 's1', 'th_1', 0, ?)`,
		msgID, "Let me think about this.",
	)
	require.NoError(t, err)

	raw, count, err := buildAllForSession(t, "s1")
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	blocks := contentBlocks(t, payloadMessages(t, raw)[0])
	require.Len(t, blocks, 3)

	thinking := blocks[0]
	assert.Equal(t, "Let me think about this.", thinking["text"], "thinking text must be re-inlined")
	assert.Equal(t, "th_1", thinking["think_id"], "think_id must survive so collapse state keys still work")

	tool := blocks[1]
	assert.Equal(t, "/home/u/proj/src/a.ts", tool["input"].(map[string]any)["file_path"], "input must be inlined verbatim")
	assert.Equal(t, "file contents here", tool["output"], "output must be inlined")
	assert.Equal(t, "success", tool["status"])
	assert.Equal(t, true, tool["done"])
	assert.EqualValues(t, 42, tool["duration_ms"])
	assert.Equal(t, "Read a file", tool["summary"])

	// Text blocks pass through.
	assert.Equal(t, "Done.", blocks[2]["text"])
}

// Both the summary AND the original content must be present, otherwise the share
// page cannot offer the summary/original toggle the app has.
func TestSessionSharePayload_IncludesSummaryAndOriginal(t *testing.T) {
	db := setupTestDBForSessionSharePayload(t)
	defer func() { _ = db.Close() }()

	seedSession(t, db, "s1")
	msgID := seedMessage(t, db, "s1", "assistant", `{"blocks":[{"type":"text","text":"The full answer."}]}`, 0, 0)

	_, err := db.Exec(
		`INSERT INTO summaries (target_type, target_id, summary, summary_cards) VALUES ('chat_message', ?, ?, ?)`,
		msgID, "A short summary.", `{"tools":[{"name":"Read","id":"t1"}]}`,
	)
	require.NoError(t, err)

	raw, _, err := buildAllForSession(t, "s1")
	require.NoError(t, err)

	msg := payloadMessages(t, raw)[0]
	assert.Equal(t, "A short summary.", msg["summary"])
	assert.NotNil(t, msg["summaryCards"])
	// The original blocks are still there alongside the summary.
	assert.Equal(t, "The full answer.", contentBlocks(t, msg)[0]["text"])
}

// Scheduled-task cards cannot work on the share page (no /api/tasks access), so
// TaskIDs must be stripped from the snapshot rather than left to render as a
// permanently-loading skeleton.
func TestSessionSharePayload_StripsTaskIDs(t *testing.T) {
	db := setupTestDBForSessionSharePayload(t)
	defer func() { _ = db.Close() }()

	seedSession(t, db, "s1")
	msgID := seedMessage(t, db, "s1", "assistant", `{"blocks":[]}`, 0, 0)

	_, err := db.Exec(
		`INSERT INTO summaries (target_type, target_id, summary, summary_cards) VALUES ('chat_message', ?, ?, ?)`,
		msgID, "s", `{"taskIDs":[7,8],"tools":[{"name":"Read"}]}`,
	)
	require.NoError(t, err)

	raw, _, err := buildAllForSession(t, "s1")
	require.NoError(t, err)

	cards := payloadMessages(t, raw)[0]["summaryCards"].(map[string]any)
	assert.Nil(t, cards["taskIDs"], "taskIDs must not reach the snapshot")
	assert.NotNil(t, cards["tools"], "other cards must be preserved")
}

// In-flight messages must never enter the snapshot: freezing a half-written
// reply would show the viewer a truncated answer.
func TestSessionSharePayload_ExcludesStreamingAndQueued(t *testing.T) {
	db := setupTestDBForSessionSharePayload(t)
	defer func() { _ = db.Close() }()

	seedSession(t, db, "s1")
	seedMessage(t, db, "s1", "user", "hello", 0, 0)
	seedMessage(t, db, "s1", "assistant", "done", 0, 0)
	seedMessage(t, db, "s1", "assistant", "half-written", 1, 0)
	seedMessage(t, db, "s1", "user", "waiting in queue", 0, 1)

	raw, count, err := buildAllForSession(t, "s1")
	require.NoError(t, err)
	assert.Equal(t, 2, count, "only the two finalized messages are shareable")

	for _, m := range payloadMessages(t, raw) {
		assert.NotEqual(t, "half-written", m["content"])
		assert.NotEqual(t, "waiting in queue", m["content"])
	}
}

func TestSessionSharePayload_KeepsSpeakerAttribution(t *testing.T) {
	db := setupTestDBForSessionSharePayload(t)
	defer func() { _ = db.Close() }()

	seedSession(t, db, "s1")
	res, err := db.Exec(
		`INSERT INTO chat_history (project_id, session_id, role, content, backend, streaming, agent_id)
		 VALUES (?, 's1', 'assistant', 'A1', 'codebuddy', 0, 'member-row-a')`,
		testProjectID,
	)
	require.NoError(t, err)
	_, err = res.LastInsertId()
	require.NoError(t, err)

	raw, _, err := buildAllForSession(t, "s1")
	require.NoError(t, err)

	msgs := payloadMessages(t, raw)
	require.Len(t, msgs, 1)
	assert.Equal(t, "member-row-a", msgs[0]["agentId"], "group speaker attribution must survive the share snapshot")
}

func TestSessionSharePayload_MessageIDSelection(t *testing.T) {
	db := setupTestDBForSessionSharePayload(t)
	defer func() { _ = db.Close() }()

	seedSession(t, db, "s1")
	id1 := seedMessage(t, db, "s1", "user", "first", 0, 0)
	seedMessage(t, db, "s1", "assistant", "second", 0, 0)
	id3 := seedMessage(t, db, "s1", "user", "third", 0, 0)

	// Selection order is not trusted: the snapshot must come out chronological.
	raw, count, err := service.BuildSessionSharePayload("s1", []int64{id3, id1}, testShareCreatedAt)
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	msgs := payloadMessages(t, raw)
	require.Len(t, msgs, 2)
	assert.Equal(t, "first", msgs[0]["content"], "ids must be restored to chronological order")
	assert.Equal(t, "third", msgs[1]["content"])
}

// A share can select non-adjacent messages. The payload must then say how many
// messages were left out before each one, because the reader cannot tell a
// contiguous transcript from a gappy one by looking at it — the omitted turns
// leave no trace.
func TestSessionSharePayload_ReportsSkippedCountsForGaps(t *testing.T) {
	db := setupTestDBForSessionSharePayload(t)
	defer func() { _ = db.Close() }()

	seedSession(t, db, "s1")
	// A message BEFORE the selection. Without it the first selected message sits
	// at position 0, where "skipped before" is 0 for two different reasons
	// (correctly nothing precedes it, and a broken `pos - (-1) - 1` also yields
	// 0) — the test could not tell a correct implementation from a broken one.
	seedMessage(t, db, "s1", "user", "before the share", 0, 0)
	id1 := seedMessage(t, db, "s1", "user", "q1", 0, 0)
	seedMessage(t, db, "s1", "assistant", "a1", 0, 0) // skipped
	seedMessage(t, db, "s1", "user", "q2", 0, 0)      // skipped
	id4 := seedMessage(t, db, "s1", "assistant", "a2", 0, 0)
	id5 := seedMessage(t, db, "s1", "user", "q3", 0, 0) // adjacent to id4

	raw, count, err := service.BuildSessionSharePayload("s1", []int64{id1, id4, id5}, testShareCreatedAt)
	require.NoError(t, err)
	require.Equal(t, 3, count)

	msgs := payloadMessages(t, raw)
	require.Len(t, msgs, 3)
	// The first SELECTED message has nothing selected before it: a divider there
	// would imply the share starts mid-conversation rather than at the first
	// message the creator picked. The unselected message before it is outside the
	// share's scope entirely, not a gap within it.
	assert.NotContains(t, msgs[0], "skippedBefore", "the first message must not report a gap")
	// Two messages sit between id1 and id4.
	assert.Equal(t, float64(2), msgs[1]["skippedBefore"])
	// id5 is adjacent to id4 — omitted when zero, so a contiguous run renders
	// exactly as it did before this field existed.
	assert.NotContains(t, msgs[2], "skippedBefore", "adjacent messages must not report a gap")
}

// Ids are not contiguous in chat_history (other sessions' inserts interleave,
// deletions leave holes), so the count must come from the session's actual
// ordering rather than id arithmetic.
func TestSessionSharePayload_SkippedCountIgnoresIDArithmetic(t *testing.T) {
	db := setupTestDBForSessionSharePayload(t)
	defer func() { _ = db.Close() }()

	seedSession(t, db, "s1")
	seedSession(t, db, "other")
	id1 := seedMessage(t, db, "s1", "user", "first", 0, 0)
	// These belong to ANOTHER session: they inflate the id range without adding
	// any messages to this conversation.
	seedMessage(t, db, "other", "user", "unrelated 1", 0, 0)
	seedMessage(t, db, "other", "user", "unrelated 2", 0, 0)
	id2 := seedMessage(t, db, "s1", "assistant", "second", 0, 0)

	raw, _, err := service.BuildSessionSharePayload("s1", []int64{id1, id2}, testShareCreatedAt)
	require.NoError(t, err)

	msgs := payloadMessages(t, raw)
	require.Len(t, msgs, 2)
	assert.NotContains(t, msgs[1], "skippedBefore",
		"the two messages are adjacent in THIS session; other sessions' ids must not create a gap")
}

// Selecting everything must produce no dividers at all — the common case has to
// stay byte-identical to what it was before the field existed.
func TestSessionSharePayload_ContiguousSelectionHasNoGaps(t *testing.T) {
	db := setupTestDBForSessionSharePayload(t)
	defer func() { _ = db.Close() }()

	seedSession(t, db, "s1")
	for _, text := range []string{"one", "two", "three"} {
		seedMessage(t, db, "s1", "user", text, 0, 0)
	}
	// Empty means "everything finalized"; the creation path resolves it to the
	// explicit list before storing, so do the same here.
	ids, err := service.ResolveSessionShareSelection("s1", nil)
	require.NoError(t, err)

	raw, _, err := service.BuildSessionSharePayload("s1", ids, testShareCreatedAt)
	require.NoError(t, err)

	for i, m := range payloadMessages(t, raw) {
		assert.NotContains(t, m, "skippedBefore", "message %d must not report a gap", i)
	}
}

// The creation path is STRICT: an id that names no finalized message must be
// rejected, so the stored selection always matches what the user checked.
func TestResolveSessionShareSelection_RejectsUnknownMessageID(t *testing.T) {
	db := setupTestDBForSessionSharePayload(t)
	defer func() { _ = db.Close() }()

	seedSession(t, db, "s1")
	seedMessage(t, db, "s1", "user", "hi", 0, 0)

	_, err := service.ResolveSessionShareSelection("s1", []int64{99999})
	require.ErrorIs(t, err, service.ErrSessionShareUnknownMessage)
}

// Selecting a streaming message is a client bug (the UI disables it); the server
// rejects rather than silently dropping, so the stored selection always matches
// what the user believed they selected.
func TestResolveSessionShareSelection_RejectsStreamingMessageID(t *testing.T) {
	db := setupTestDBForSessionSharePayload(t)
	defer func() { _ = db.Close() }()

	seedSession(t, db, "s1")
	seedMessage(t, db, "s1", "user", "hi", 0, 0)
	streamingID := seedMessage(t, db, "s1", "assistant", "typing...", 1, 0)

	_, err := service.ResolveSessionShareSelection("s1", []int64{streamingID})
	require.ErrorIs(t, err, service.ErrSessionShareUnknownMessage)
}

// An empty selection means "everything finalized", resolved into the explicit
// list here so the stored share cannot grow as the conversation continues.
func TestResolveSessionShareSelection_EmptyMeansEveryFinalized(t *testing.T) {
	db := setupTestDBForSessionSharePayload(t)
	defer func() { _ = db.Close() }()

	seedSession(t, db, "s1")
	id1 := seedMessage(t, db, "s1", "user", "first", 0, 0)
	id2 := seedMessage(t, db, "s1", "assistant", "second", 0, 0)
	// A streaming row must never be resolved into the selection.
	seedMessage(t, db, "s1", "assistant", "typing...", 1, 0)

	ids, err := service.ResolveSessionShareSelection("s1", nil)
	require.NoError(t, err)
	assert.Equal(t, []int64{id1, id2}, ids)
}

// Out-of-order input is re-sorted and duplicates are dropped: sharing the same
// message twice would render the bubble twice.
func TestResolveSessionShareSelection_SortsAndDeduplicates(t *testing.T) {
	db := setupTestDBForSessionSharePayload(t)
	defer func() { _ = db.Close() }()

	seedSession(t, db, "s1")
	id1 := seedMessage(t, db, "s1", "user", "first", 0, 0)
	seedMessage(t, db, "s1", "assistant", "second", 0, 0)
	id3 := seedMessage(t, db, "s1", "user", "third", 0, 0)

	ids, err := service.ResolveSessionShareSelection("s1", []int64{id3, id1, id3})
	require.NoError(t, err)
	assert.Equal(t, []int64{id1, id3}, ids)
}

func TestResolveSessionShareSelection_EmptySession(t *testing.T) {
	db := setupTestDBForSessionSharePayload(t)
	defer func() { _ = db.Close() }()

	seedSession(t, db, "s1")
	_, err := service.ResolveSessionShareSelection("s1", nil)
	require.ErrorIs(t, err, service.ErrSessionShareEmptySelection)
}

// The READ path is lenient: a share survives its messages being rewound or
// hard-deleted, it just shows less.
func TestSessionSharePayload_UnknownSession(t *testing.T) {
	db := setupTestDBForSessionSharePayload(t)
	defer func() { _ = db.Close() }()

	_, _, err := service.BuildSessionSharePayload("nope", []int64{1}, testShareCreatedAt)
	require.ErrorIs(t, err, service.ErrSessionShareSessionGone)
}

func TestSessionSharePayload_EmptySession(t *testing.T) {
	db := setupTestDBForSessionSharePayload(t)
	defer func() { _ = db.Close() }()

	seedSession(t, db, "s1")
	_, _, err := service.BuildSessionSharePayload("s1", []int64{1}, testShareCreatedAt)
	require.ErrorIs(t, err, service.ErrSessionShareNothingToShow)
}

// A selection whose messages were all deleted must not error out the reader with
// a 500 — it means the link has nothing left to show, which the handler 404s.
func TestSessionSharePayload_AllSelectedMessagesDeleted(t *testing.T) {
	db := setupTestDBForSessionSharePayload(t)
	defer func() { _ = db.Close() }()

	seedSession(t, db, "s1")
	goneID := seedMessage(t, db, "s1", "user", "will be rewound", 0, 0)
	_, err := db.Exec("DELETE FROM chat_history WHERE id = ?", goneID)
	require.NoError(t, err)
	// A later message that was never selected must not resurrect the link.
	seedMessage(t, db, "s1", "user", "not selected", 0, 0)

	_, _, err = service.BuildSessionSharePayload("s1", []int64{goneID}, testShareCreatedAt)
	require.ErrorIs(t, err, service.ErrSessionShareNothingToShow)
}

// Partially deleted selection: the survivors render, the missing ids are skipped.
func TestSessionSharePayload_SkipsDeletedMessages(t *testing.T) {
	db := setupTestDBForSessionSharePayload(t)
	defer func() { _ = db.Close() }()

	seedSession(t, db, "s1")
	id1 := seedMessage(t, db, "s1", "user", "keep me", 0, 0)
	goneID := seedMessage(t, db, "s1", "assistant", "rewound", 0, 0)
	id3 := seedMessage(t, db, "s1", "user", "keep me too", 0, 0)
	_, err := db.Exec("DELETE FROM chat_history WHERE id = ?", goneID)
	require.NoError(t, err)

	raw, count, err := service.BuildSessionSharePayload("s1", []int64{id1, goneID, id3}, testShareCreatedAt)
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	msgs := payloadMessages(t, raw)
	require.Len(t, msgs, 2)
	assert.Equal(t, "keep me", msgs[0]["content"])
	assert.Equal(t, "keep me too", msgs[1]["content"])
}

// A share outlives archiving: archiving keeps every message, so the link must
// keep rendering. This is the regression that a naive GetSessionFullInfo call
// would introduce (it filters archived = 0 and would 500 the request).
func TestSessionSharePayload_ArchivedSessionStillBuilds(t *testing.T) {
	db := setupTestDBForSessionSharePayload(t)
	defer func() { _ = db.Close() }()

	seedSession(t, db, "s1")
	id := seedMessage(t, db, "s1", "user", "still here", 0, 0)
	_, err := db.Exec("UPDATE chat_sessions SET archived = 1 WHERE id = 's1'")
	require.NoError(t, err)

	raw, count, err := service.BuildSessionSharePayload("s1", []int64{id}, testShareCreatedAt)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	assert.Contains(t, raw, "still here")
}

// The assembled bytes are what the handler's ETag is derived from, so two
// requests for an unchanged conversation MUST be byte-identical. time.Now() in
// the payload (the pre-live-assembly behavior) would break this silently.
func TestSessionSharePayload_IsByteStable(t *testing.T) {
	db := setupTestDBForSessionSharePayload(t)
	defer func() { _ = db.Close() }()

	seedSession(t, db, "s1")
	seedMessage(t, db, "s1", "user", "hello", 0, 0)
	seedMessage(t, db, "s1", "assistant", "world", 0, 0)

	first, _, err := buildAllForSession(t, "s1")
	require.NoError(t, err)
	second, _, err := buildAllForSession(t, "s1")
	require.NoError(t, err)
	assert.Equal(t, first, second)
}

// Only the selected messages' tool output is inlined. Guards the message-scoped
// side-table queries: a session-wide scan would ship the unselected tool output.
func TestSessionSharePayload_InlinesOnlySelectedToolCalls(t *testing.T) {
	db := setupTestDBForSessionSharePayload(t)
	defer func() { _ = db.Close() }()

	seedSession(t, db, "s1")
	id1 := seedMessage(t, db, "s1", "assistant", `{"blocks":[{"type":"tool_use","id":"t1","name":"Read"}]}`, 0, 0)
	seedMessage(t, db, "s1", "assistant", `{"blocks":[{"type":"tool_use","id":"t2","name":"Read"}]}`, 0, 0)
	_, err := db.Exec(`INSERT INTO chat_tool_calls (message_id, session_id, tool_id, name, input, output)
		VALUES (?, 's1', 't1', 'Read', '{"path":"a"}', 'SELECTED-OUTPUT')`, id1)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO chat_tool_calls (message_id, session_id, tool_id, name, input, output)
		VALUES (?, 's1', 't2', 'Read', '{"path":"b"}', 'UNSELECTED-OUTPUT')`, id1+1)
	require.NoError(t, err)

	raw, _, err := service.BuildSessionSharePayload("s1", []int64{id1}, testShareCreatedAt)
	require.NoError(t, err)
	assert.Contains(t, raw, "SELECTED-OUTPUT")
	assert.NotContains(t, raw, "UNSELECTED-OUTPUT")
}

// The transcript is served verbatim: absolute paths, tool output and attachments
// all reach the reader exactly as stored. The only thing that must NOT leak is
// the session's own project path, which was never part of the payload shape.
func TestSessionSharePayload_ServesPathsVerbatim(t *testing.T) {
	db := setupTestDBForSessionSharePayload(t)
	defer func() { _ = db.Close() }()

	seedSession(t, db, "s1")
	msgID := seedMessage(t, db, "s1", "assistant", `{"blocks":[
		{"type":"tool_use","id":"t1","name":"Read"},
		{"type":"text","text":"Read /home/u/proj/src/a.ts and /home/u/.ssh/id_rsa"}
	]}`, 0, 0)

	_, err := db.Exec(
		`INSERT INTO chat_tool_calls (message_id, session_id, tool_id, name, input, output, status, done)
		 VALUES (?, 's1', 't1', 'Read', ?, ?, 'success', 1)`,
		msgID,
		`{"file_path":"/home/u/proj/src/a.ts"}`,
		"contents of /home/u/proj/src/a.ts",
	)
	require.NoError(t, err)

	// Attachment with an absolute path.
	_, err = db.Exec(`UPDATE chat_history SET files = ? WHERE id = ?`,
		`[{"path":"/home/u/proj/docs/plan.md","isDir":false}]`, msgID)
	require.NoError(t, err)

	raw, _, err := buildAllForSession(t, "s1")
	require.NoError(t, err)

	msg := payloadMessages(t, raw)[0]
	blocks := contentBlocks(t, msg)
	assert.Equal(t, "/home/u/proj/src/a.ts", blocks[0]["input"].(map[string]any)["file_path"])
	assert.Contains(t, blocks[0]["output"], "/home/u/proj/src/a.ts")
	assert.Contains(t, blocks[1]["text"], "/home/u/proj/src/a.ts")
	assert.Contains(t, blocks[1]["text"], "/home/u/.ssh/id_rsa")

	files := msg["files"].([]any)
	assert.Equal(t, "/home/u/proj/docs/plan.md", files[0].(map[string]any)["path"])

	// The session block carries no project path field at all.
	session := decodePayload(t, raw)["session"].(map[string]any)
	assert.NotContains(t, session, "projectPath")
	assert.NotContains(t, session, "project_path")
}

// A path outside both the project and home is served whole, like every other.
func TestSessionSharePayload_OutsidePathServedWhole(t *testing.T) {
	db := setupTestDBForSessionSharePayload(t)
	defer func() { _ = db.Close() }()

	seedSession(t, db, "s1")
	msgID := seedMessage(t, db, "s1", "user", "see this", 0, 0)
	_, err := db.Exec(`UPDATE chat_history SET files = ? WHERE id = ?`,
		`[{"path":"/etc/secret/credentials.txt","isDir":false}]`, msgID)
	require.NoError(t, err)

	raw, _, err := buildAllForSession(t, "s1")
	require.NoError(t, err)

	files := payloadMessages(t, raw)[0]["files"].([]any)
	assert.Equal(t, "/etc/secret/credentials.txt", files[0].(map[string]any)["path"])
}

// External URLs are not local paths and must survive untouched.
func TestSessionSharePayload_KeepsExternalURLAttachments(t *testing.T) {
	db := setupTestDBForSessionSharePayload(t)
	defer func() { _ = db.Close() }()

	seedSession(t, db, "s1")
	msgID := seedMessage(t, db, "s1", "user", "see this PR", 0, 0)
	_, err := db.Exec(`UPDATE chat_history SET files = ? WHERE id = ?`,
		`[{"path":"owner/repo#12","kind":"url","url":"https://github.com/owner/repo/pull/12"}]`, msgID)
	require.NoError(t, err)

	raw, _, err := buildAllForSession(t, "s1")
	require.NoError(t, err)

	files := payloadMessages(t, raw)[0]["files"].([]any)
	entry := files[0].(map[string]any)
	assert.Equal(t, "https://github.com/owner/repo/pull/12", entry["url"])
	assert.Equal(t, "owner/repo#12", entry["path"], "url entries keep their label, not a filesystem path")
}

func TestSessionSharePayload_SessionMetadataAndVersion(t *testing.T) {
	db := setupTestDBForSessionSharePayload(t)
	defer func() { _ = db.Close() }()

	seedSession(t, db, "s1")
	seedMessage(t, db, "s1", "user", "hi", 0, 0)

	raw, _, err := buildAllForSession(t, "s1")
	require.NoError(t, err)

	out := decodePayload(t, raw)
	assert.EqualValues(t, 1, out["version"])
	// Must be the caller's timestamp, not merely "non-empty": the zero time
	// serializes to "0001-01-01T00:00:00Z", which satisfied NotEmpty and let a
	// broken parseShareTime ship a year-1 createdAt for every share.
	assert.Equal(t, testShareCreatedAt.Format(time.RFC3339), out["createdAt"])

	session := out["session"].(map[string]any)
	assert.Equal(t, "Fix the login bug", session["title"])
	assert.Equal(t, "codebuddy", session["backend"])
	assert.Equal(t, "codebuddy", session["agentId"])
	assert.Equal(t, "claude-sonnet-4", session["model"])
}

// Messages must not carry internal identifiers (session id, queue id, project
// path) into an anonymous view.
func TestSessionSharePayload_StripsInternalMessageFields(t *testing.T) {
	db := setupTestDBForSessionSharePayload(t)
	defer func() { _ = db.Close() }()

	seedSession(t, db, "s1")
	seedMessage(t, db, "s1", "user", "hi", 0, 0)

	raw, _, err := buildAllForSession(t, "s1")
	require.NoError(t, err)

	msg := payloadMessages(t, raw)[0]
	for _, field := range []string{"sessionId", "projectPath", "queueId", "queued", "streaming", "indexed", "backend"} {
		assert.NotContains(t, msg, field, "field %q must not be serialized into the snapshot", field)
	}
	assert.Contains(t, msg, "id", "the message id is needed as a render key")
	assert.Contains(t, msg, "createdAt")
}

// A tool_use block whose side-table row is missing (e.g. a tool that never
// completed before an upgrade) must still render, just without the payload.
func TestSessionSharePayload_ToolBlockWithoutSideRowSurvives(t *testing.T) {
	db := setupTestDBForSessionSharePayload(t)
	defer func() { _ = db.Close() }()

	seedSession(t, db, "s1")
	seedMessage(t, db, "s1", "assistant", `{"blocks":[{"type":"tool_use","id":"orphan","name":"Bash","done":true}]}`, 0, 0)

	raw, _, err := buildAllForSession(t, "s1")
	require.NoError(t, err)

	blocks := contentBlocks(t, payloadMessages(t, raw)[0])
	require.Len(t, blocks, 1)
	assert.Equal(t, "Bash", blocks[0]["name"])
	assert.Equal(t, "orphan", blocks[0]["id"], "the block must not be dropped")
}

// Plain-text user messages are not JSON wrappers and must pass through verbatim.
func TestSessionSharePayload_PlainTextUserMessage(t *testing.T) {
	db := setupTestDBForSessionSharePayload(t)
	defer func() { _ = db.Close() }()

	seedSession(t, db, "s1")
	seedMessage(t, db, "s1", "user", "just some text, not json", 0, 0)

	raw, _, err := buildAllForSession(t, "s1")
	require.NoError(t, err)

	assert.Equal(t, "just some text, not json", payloadMessages(t, raw)[0]["content"])
}

// A user message stored in block form is served verbatim too.
func TestSessionSharePayload_UserBlockContentIsVerbatim(t *testing.T) {
	db := setupTestDBForSessionSharePayload(t)
	defer func() { _ = db.Close() }()

	seedSession(t, db, "s1")
	seedMessage(t, db, "s1", "user", `{"blocks":[{"type":"text","text":"look at /home/u/proj/x.go"}]}`, 0, 0)

	raw, _, err := buildAllForSession(t, "s1")
	require.NoError(t, err)

	blocks := contentBlocks(t, payloadMessages(t, raw)[0])
	assert.Equal(t, "look at /home/u/proj/x.go", blocks[0]["text"])
}

// ─── Selection listing ───────────────────────────────────────────────────────

// The dialog needs every chat_history message, including in-flight (streaming)
// ones, so it can show them disabled with a reason instead of hiding them.
// Queued messages are absent by construction: they have no chat_history row
// until they are dequeued, so there is nothing for the dialog to disable.
func TestGetSessionMessagesForSelection_IncludesInFlight(t *testing.T) {
	db := setupTestDBForSessionSharePayload(t)
	defer func() { _ = db.Close() }()

	seedSession(t, db, "s1")
	seedMessage(t, db, "s1", "user", "hello there", 0, 0)
	seedMessage(t, db, "s1", "assistant", `{"blocks":[{"type":"text","text":"a reply"}]}`, 0, 0)
	seedMessage(t, db, "s1", "assistant", "typing", 1, 0)

	items, err := service.GetSessionMessagesForSelection("s1")
	require.NoError(t, err)
	require.Len(t, items, 3, "in-flight messages must be listed, not hidden")

	assert.False(t, items[0].Streaming)
	assert.Equal(t, "hello there", items[0].Preview)
	assert.True(t, items[2].Streaming, "streaming flag must be reported")

	// A JSON block wrapper must be flattened to readable text for the preview.
	assert.Equal(t, "a reply", items[1].Preview)
	assert.Equal(t, "assistant", items[1].Role)
}

func TestGetSessionMessagesForSelection_PreviewIsTruncated(t *testing.T) {
	db := setupTestDBForSessionSharePayload(t)
	defer func() { _ = db.Close() }()

	seedSession(t, db, "s1")
	seedMessage(t, db, "s1", "user", strings.Repeat("字", 500), 0, 0)

	items, err := service.GetSessionMessagesForSelection("s1")
	require.NoError(t, err)
	require.Len(t, items, 1)

	runes := []rune(items[0].Preview)
	assert.LessOrEqual(t, len(runes), 200, "preview must be bounded")
	assert.Equal(t, 200, len(runes))
}

// A group user message carries the @ protocol, including `private` (密送) notes.
// The selection preview is shown in the share dialog, so the note must be
// stripped — otherwise a note addressed to one member is previewed to everyone.
func TestGetSessionMessagesForSelection_GroupStripsPrivateNote(t *testing.T) {
	db := setupTestDBForSessionSharePayload(t)
	defer func() { _ = db.Close() }()

	seedGroupSession(t, db, "g1")
	seedMessage(t, db, "g1", "user",
		`<clawbench-mention targets="m-b"></clawbench-mention> <clawbench-mention targets="m-b" private>你的词是西瓜</clawbench-mention> 你先说`,
		0, 0)

	items, err := service.GetSessionMessagesForSelection("g1")
	require.NoError(t, err)
	require.Len(t, items, 1)

	assert.NotContains(t, items[0].Preview, "你的词是西瓜", "a private note must never be previewed")
	assert.NotContains(t, items[0].Preview, "clawbench-mention")
	assert.Contains(t, items[0].Preview, "你先说")
}

// A single chat has no protocol, so a message that merely DISCUSSES the tag
// syntax keeps its text verbatim in the preview.
func TestGetSessionMessagesForSelection_SingleChatKeepsText(t *testing.T) {
	db := setupTestDBForSessionSharePayload(t)
	defer func() { _ = db.Close() }()

	seedSession(t, db, "s1")
	content := `看这个 <clawbench-mention targets="A" private>字面</clawbench-mention> 语法`
	seedMessage(t, db, "s1", "user", content, 0, 0)

	items, err := service.GetSessionMessagesForSelection("s1")
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, content, items[0].Preview)
}

func TestGetSessionMessagesForSelection_UnknownSessionIsEmpty(t *testing.T) {
	db := setupTestDBForSessionSharePayload(t)
	defer func() { _ = db.Close() }()

	items, err := service.GetSessionMessagesForSelection("nope")
	require.NoError(t, err)
	assert.Empty(t, items)
}

// TestSessionSharePayload_FreezesSpeakerIdentitiesWithAvatars pins the current
// share contract: the snapshot carries each speaker's name and backend AND the
// custom avatar, so the viewer renders the same icon the app shows. The
// snapshot is frozen by the authenticated owner at creation time; the anonymous
// viewer has no other way to resolve an avatar.
func TestSessionSharePayload_FreezesSpeakerIdentitiesWithAvatars(t *testing.T) {
	db := setupTestDBForSessionSharePayload(t)
	defer func() { _ = db.Close() }()

	origAgents := model.Agents
	model.Agents = map[string]*model.Agent{
		"codebuddy": {ID: "codebuddy", Name: "Host Agent", Backend: "codebuddy", Avatar: "<host/>"},
	}
	t.Cleanup(func() { model.Agents = origAgents })

	seedSession(t, db, "s1")
	seedMessage(t, db, "s1", "assistant", `{"blocks":[{"type":"text","text":"hi"}],"metadata":{}}`, 0, 0)

	raw, _, err := buildAllForSession(t, "s1")
	require.NoError(t, err)

	var payload struct {
		SessionAgent *struct {
			Name    string `json:"name"`
			Backend string `json:"backend"`
			Avatar  string `json:"avatar"`
		} `json:"sessionAgent"`
		Speakers map[string]struct {
			Backend string `json:"backend"`
			Avatar  string `json:"avatar"`
		} `json:"speakers"`
	}
	require.NoError(t, json.Unmarshal([]byte(raw), &payload))
	require.NotNil(t, payload.SessionAgent, "the viewer needs the session agent to icon single-agent rows")
	assert.Equal(t, "codebuddy", payload.SessionAgent.Backend)
	assert.Equal(t, "Host Agent", payload.SessionAgent.Name)
	assert.Equal(t, "<host/>", payload.SessionAgent.Avatar, "the share snapshot must freeze custom avatars for the viewer")
}

// A shared GROUP must freeze each member's OWN avatar (agent_avatar) into the
// speaker entry the viewer resolves per message, falling back to the agent's
// shared avatar only when the member has no snapshot. Without this the anonymous
// viewer (which cannot reach /api/agents) would draw a generic icon for a member
// who set a custom one.
func TestSessionSharePayload_FreezesGroupMemberAvatar(t *testing.T) {
	db := setupTestDBForSessionSharePayload(t)
	defer func() { _ = db.Close() }()

	origAgents := model.Agents
	model.Agents = map[string]*model.Agent{
		"claude": {ID: "claude", Name: "Claude", Backend: "claude", Avatar: "<agent/>"},
	}
	t.Cleanup(func() { model.Agents = origAgents })

	// context_state must be valid JSON: GroupMembersForGroups json_extract()s the
	// host pointer out of it, and a malformed value makes that query fail (the
	// builder swallows the error, leaving groupMembers empty).
	_, err := db.Exec(
		`INSERT INTO chat_sessions (id, project_id, backend, title, agent_id, model, session_type, context_state)
		 VALUES ('g1', ?, 'codebuddy', 'Group', '', '', 'group', '{}')`,
		testProjectID,
	)
	require.NoError(t, err)
	// The member carries its OWN avatar snapshot; the agent's is only a fallback.
	_, err = db.Exec(
		`INSERT INTO chat_sessions (id, project_id, backend, title, agent_name, agent_avatar, agent_id, session_type, group_id, archived)
		 VALUES ('m-1', ?, 'claude', '', 'Alice', '<member/>', 'claude', 'group_member', 'g1', 0)`,
		testProjectID,
	)
	require.NoError(t, err)

	seedMessage(t, db, "g1", "assistant", `{"blocks":[{"type":"text","text":"hi"}],"metadata":{}}`, 0, 0)

	raw, _, err := buildAllForSession(t, "g1")
	require.NoError(t, err)

	var payload struct {
		Speakers map[string]struct {
			Avatar string `json:"avatar"`
		} `json:"speakers"`
		GroupMembers []struct {
			ID     string `json:"id"`
			Avatar string `json:"avatar"`
		} `json:"groupMembers"`
	}
	require.NoError(t, json.Unmarshal([]byte(raw), &payload))

	speaker, ok := payload.Speakers["m-1"]
	require.True(t, ok, "the member must appear in the speakers map")
	assert.Equal(t, "<member/>", speaker.Avatar,
		"the member's own avatar snapshot must be frozen into the speaker entry")

	require.Len(t, payload.GroupMembers, 1)
	assert.Equal(t, "m-1", payload.GroupMembers[0].ID)
	assert.Equal(t, "<member/>", payload.GroupMembers[0].Avatar,
		"the avatar stack must draw the same member snapshot")
}

// A shared GROUP must carry the host's member row id so the public viewer can
// mark the host's messages and render their private notes (bcc) card.
func TestSessionSharePayload_GroupCarriesHostMemberID(t *testing.T) {
	db := setupTestDBForSessionSharePayload(t)
	defer db.Close()

	_, err := db.Exec(
		`INSERT INTO chat_sessions (id, project_id, backend, title, agent_id, model, session_type, context_state)
		 VALUES ('g1', 1, 'codebuddy', 'Group', 'codebuddy', 'm', 'group', '{"host_member_id":"row-host-1"}')`)
	require.NoError(t, err)
	_, err = db.Exec(
		`INSERT INTO chat_history (project_id, role, content, session_id, backend, agent_id, streaming, indexed)
		 VALUES (1, 'assistant', '{"blocks":[{"type":"text","text":"hi"}]}', 'g1', 'codebuddy', 'row-host-1', 0, 0)`)
	require.NoError(t, err)

	raw, _, err := buildAllForSession(t, "g1")
	require.NoError(t, err)

	var payload struct {
		Session struct {
			HostMemberID string `json:"hostMemberId"`
		} `json:"session"`
	}
	require.NoError(t, json.Unmarshal([]byte(raw), &payload))
	assert.Equal(t, "row-host-1", payload.Session.HostMemberID)
}

// A plain (non-group) session must not carry a hostMemberId.
func TestSessionSharePayload_PlainSessionHasNoHostMemberID(t *testing.T) {
	db := setupTestDBForSessionSharePayload(t)
	defer db.Close()

	_, err := db.Exec(
		`INSERT INTO chat_sessions (id, project_id, backend, title, agent_id, model) VALUES ('s1', 1, 'claude', 'Plain', 'claude', 'm')`)
	require.NoError(t, err)
	_, err = db.Exec(
		`INSERT INTO chat_history (project_id, role, content, session_id, backend, streaming, indexed)
		 VALUES (1, 'assistant', '{"blocks":[{"type":"text","text":"hi"}]}', 's1', 'claude', 0, 0)`)
	require.NoError(t, err)

	raw, _, err := buildAllForSession(t, "s1")
	require.NoError(t, err)

	var payload struct {
		Session struct {
			HostMemberID string `json:"hostMemberId"`
		} `json:"session"`
	}
	require.NoError(t, json.Unmarshal([]byte(raw), &payload))
	assert.Empty(t, payload.Session.HostMemberID)
}

// A shared GROUP must carry its ACTIVE roster (ordered, host flagged) so the
// public topbar can render the same overlapping avatar stack the in-app group
// header shows. Left members must be excluded and order must be roster order
// (created_at, rowid) — NOT the UUID-sorted order the `speakers` map marshals.
func TestSessionSharePayload_GroupCarriesOrderedActiveMembers(t *testing.T) {
	db := setupTestDBForSessionSharePayload(t)
	defer db.Close()

	_, err := db.Exec(
		`INSERT INTO chat_sessions (id, project_id, backend, title, agent_id, model, session_type, context_state)
		 VALUES ('g1', 1, 'codebuddy', 'Group', 'codebuddy', 'm', 'group', '{"host_member_id":"m-host"}')`)
	require.NoError(t, err)
	// Member rows: the host first (created_at order), then a plain member, then a
	// LEFT member that must be excluded. group_id points each row at the group.
	// The member's display name lives in agent_name (title is left empty).
	insertMember := func(id, name, backend string, archived int, createdAt string) {
		t.Helper()
		_, err := db.Exec(
			`INSERT INTO chat_sessions (id, project_id, backend, title, agent_name, agent_id, session_type, group_id, archived, created_at)
			 VALUES (?, 1, ?, '', ?, ?, 'group_member', 'g1', ?, ?)`,
			id, backend, name, id, archived, createdAt)
		require.NoError(t, err)
	}
	insertMember("m-host", "Host", "codebuddy", 0, "2026-01-01 10:00:00")
	insertMember("m-second", "Second", "claude", 0, "2026-01-01 10:01:00")
	insertMember("m-left", "Gone", "gemini", 1, "2026-01-01 10:02:00")

	_, err = db.Exec(
		`INSERT INTO chat_history (project_id, role, content, session_id, backend, agent_id, streaming, indexed)
		 VALUES (1, 'assistant', '{"blocks":[{"type":"text","text":"hi"}]}', 'g1', 'codebuddy', 'm-host', 0, 0)`)
	require.NoError(t, err)

	raw, _, err := buildAllForSession(t, "g1")
	require.NoError(t, err)

	var payload struct {
		GroupMembers []struct {
			ID      string `json:"id"`
			AgentID string `json:"agentId"`
			Name    string `json:"name"`
			Backend string `json:"backend"`
			IsHost  bool   `json:"isHost"`
		} `json:"groupMembers"`
	}
	require.NoError(t, json.Unmarshal([]byte(raw), &payload))

	require.Len(t, payload.GroupMembers, 2, "left members must be excluded from the stack")
	// Roster order: host first (created_at), then the second member.
	assert.Equal(t, "m-host", payload.GroupMembers[0].ID)
	assert.Equal(t, "Host", payload.GroupMembers[0].Name)
	assert.True(t, payload.GroupMembers[0].IsHost, "the host must be flagged")
	assert.Equal(t, "m-second", payload.GroupMembers[1].ID)
	assert.False(t, payload.GroupMembers[1].IsHost)
	// The left member is absent.
	for _, m := range payload.GroupMembers {
		assert.NotEqual(t, "m-left", m.ID)
	}
}

// A plain (non-group) session must not carry a groupMembers array — the topbar
// then renders the single session-agent icon.
func TestSessionSharePayload_PlainSessionHasNoGroupMembers(t *testing.T) {
	db := setupTestDBForSessionSharePayload(t)
	defer db.Close()

	_, err := db.Exec(
		`INSERT INTO chat_sessions (id, project_id, backend, title, agent_id, model) VALUES ('s1', 1, 'claude', 'Plain', 'claude', 'm')`)
	require.NoError(t, err)
	_, err = db.Exec(
		`INSERT INTO chat_history (project_id, role, content, session_id, backend, streaming, indexed)
		 VALUES (1, 'assistant', '{"blocks":[{"type":"text","text":"hi"}]}', 's1', 'claude', 0, 0)`)
	require.NoError(t, err)

	raw, _, err := buildAllForSession(t, "s1")
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &payload))
	_, present := payload["groupMembers"]
	assert.False(t, present, "a single-agent session must not carry groupMembers")
}

// The viewer must be able to tell a group share from a single-agent share
// WITHOUT looking at the roster: groupMembers is the ACTIVE roster only, so a
// group whose members all left carries an empty (or absent) array. The explicit
// sessionType field is what gates the @-protocol stripping, so a missing value
// would make the reader see raw <clawbench-mention> tags.
func TestSessionSharePayload_SessionTypeIsExplicit(t *testing.T) {
	t.Run("plain session reports chat", func(t *testing.T) {
		db := setupTestDBForSessionSharePayload(t)
		defer db.Close()

		seedSession(t, db, "s1")
		seedMessage(t, db, "s1", "assistant", `{"blocks":[{"type":"text","text":"hi"}]}`, 0, 0)

		raw, _, err := buildAllForSession(t, "s1")
		require.NoError(t, err)

		assert.Equal(t, "chat", sessionTypeOf(t, raw))
	})

	t.Run("group session reports group", func(t *testing.T) {
		db := setupTestDBForSessionSharePayload(t)
		defer db.Close()

		seedGroupSession(t, db, "g1")
		seedMessage(t, db, "g1", "assistant", `{"blocks":[{"type":"text","text":"hi"}]}`, 0, 0)

		raw, _, err := buildAllForSession(t, "g1")
		require.NoError(t, err)

		assert.Equal(t, "group", sessionTypeOf(t, raw))
	})

	// Regression guard for "roster is not a group signal": every member row is
	// archived (left), so GroupMembersForGroups returns nothing, yet the session
	// is still a group and the viewer must strip the routing protocol.
	t.Run("group with every member gone still reports group", func(t *testing.T) {
		db := setupTestDBForSessionSharePayload(t)
		defer db.Close()

		seedGroupSession(t, db, "g1")
		for _, id := range []string{"m-host", "m-second"} {
			_, err := db.Exec(
				`INSERT INTO chat_sessions (id, project_id, backend, title, agent_name, agent_id, session_type, group_id, archived)
				 VALUES (?, 1, 'codebuddy', '', ?, ?, 'group_member', 'g1', 1)`,
				id, id, id)
			require.NoError(t, err)
		}
		seedMessage(t, db, "g1", "user", `<clawbench-mention targets="m-host">hi</clawbench-mention>`, 0, 0)

		raw, _, err := buildAllForSession(t, "g1")
		require.NoError(t, err)

		payload := decodePayload(t, raw)
		// The roster is genuinely empty — this is exactly why the type must be
		// carried separately rather than inferred from the member list.
		members, _ := payload["groupMembers"].([]any)
		assert.Empty(t, members, "all members left, so the active roster is empty")

		session, ok := payload["session"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "group", session["sessionType"],
			"sessionType must survive an empty roster")
	})
}

// sessionTypeOf reads session.sessionType off the wire shape.
func sessionTypeOf(t *testing.T, raw string) string {
	t.Helper()
	payload := decodePayload(t, raw)
	session, ok := payload["session"].(map[string]any)
	require.True(t, ok, "payload must carry a session object")
	value, _ := session["sessionType"].(string)
	return value
}
