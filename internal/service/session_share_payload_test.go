package service_test

import (
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

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
		`CREATE TABLE IF NOT EXISTS chat_sessions (
			id TEXT PRIMARY KEY,
			project_path TEXT NOT NULL DEFAULT '',
			backend TEXT NOT NULL DEFAULT '',
			title TEXT NOT NULL DEFAULT '',
			agent_id TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL DEFAULT '',
			transport TEXT NOT NULL DEFAULT '',
			auto_approve INTEGER NOT NULL DEFAULT 0,
			archived INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS chat_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_path TEXT NOT NULL DEFAULT '',
			role TEXT NOT NULL,
			content TEXT NOT NULL DEFAULT '',
			files TEXT,
			session_id TEXT NOT NULL DEFAULT '',
			backend TEXT NOT NULL DEFAULT '',
			streaming INTEGER NOT NULL DEFAULT 0,
			indexed INTEGER NOT NULL DEFAULT 0,
			queue_id TEXT NOT NULL DEFAULT '',
			queued INTEGER NOT NULL DEFAULT 0,
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
	} {
		_, err = db.Exec(ddl)
		require.NoError(t, err)
	}

	cleanup := service.SetDBForTest(db, db)
	t.Cleanup(cleanup)
	return db
}

const testProjectRoot = "/home/u/proj"

// seedSession inserts a chat_sessions row. sessionID stays a parameter so the
// helper mirrors the schema rather than hard-coding one id.
func seedSession(t *testing.T, db *sql.DB, sessionID string) { //nolint:unparam // general-purpose seed helper; all current callers use "s1"
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO chat_sessions (id, project_path, backend, title, agent_id, model)
		 VALUES (?, ?, 'codebuddy', 'Fix the login bug', 'codebuddy', 'claude-sonnet-4')`,
		sessionID, testProjectRoot,
	)
	require.NoError(t, err)
}

// seedMessage inserts a finalized message and returns its id. sessionID stays a
// parameter so the helper mirrors the schema rather than hard-coding one id.
func seedMessage(t *testing.T, db *sql.DB, sessionID, role, content string, streaming, queued int) int64 { //nolint:unparam // general-purpose seed helper; all current callers use "s1"
	t.Helper()
	res, err := db.Exec(
		`INSERT INTO chat_history (project_path, session_id, role, content, backend, streaming, queued)
		 VALUES (?, ?, ?, ?, 'codebuddy', ?, ?)`,
		testProjectRoot, sessionID, role, content, streaming, queued,
	)
	require.NoError(t, err)
	id, err := res.LastInsertId()
	require.NoError(t, err)
	return id
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

	raw, _, err := service.BuildSessionSharePayload("s1", nil, testProjectRoot, "/home/u")
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

// TestSessionSharePayload_SanitizesSummaryText is the regression guard for an
// absolute-path leak through the reading summary.
//
// buildShareMessage stored summaries[msg.ID] verbatim. A summary routinely names
// the files the agent touched ("I edited /home/u/proj/secret.ts"), so the
// creator's directory layout shipped to anonymous viewers — the exact thing the
// path sanitizer exists to prevent. The pre-existing SanitizesPaths test could
// not catch it: its fixture has no summary.
func TestSessionSharePayload_SanitizesSummaryText(t *testing.T) {
	db := setupTestDBForSessionSharePayload(t)
	defer func() { _ = db.Close() }()

	seedSession(t, db, "s1")
	msgID := seedMessage(t, db, "s1", "assistant", `{"blocks":[{"type":"text","text":"done"}],"metadata":{}}`, 0, 0)

	_, err := db.Exec(
		`INSERT INTO summaries (target_type, target_id, summary)
		 VALUES ('chat_message', ?, ?)`,
		msgID, "I edited /home/u/proj/src/secret.ts and read /home/u/.ssh/id_rsa",
	)
	require.NoError(t, err)

	raw, _, err := service.BuildSessionSharePayload("s1", nil, testProjectRoot, "/home/u")
	require.NoError(t, err)

	assert.NotContains(t, raw, testProjectRoot,
		"the project root must not appear anywhere in the snapshot")
	assert.NotContains(t, raw, "/home/u/.ssh",
		"a home-directory path must not survive in the summary")

	msg := payloadMessages(t, raw)[0]
	summary, _ := msg["summary"].(string)
	require.NotEmpty(t, summary, "the summary must still be present, just sanitized")
	assert.Contains(t, summary, "./src/secret.ts",
		"the project path must be relativized, not dropped")
	assert.Contains(t, summary, "~/.ssh/id_rsa",
		"the home path must be relativized, not dropped")
}

// TestSessionSharePayload_SanitizesSummaryCardInputs is the regression guard for
// an absolute-path leak through summary cards.
//
// sanitizeSummaryCards copied the card struct by value (`out := *cards`) and only
// relativized CreatedFiles/ModifiedFiles/Warnings. Tools[].Input and the
// AskQuestions sub-objects were left untouched, so a tool argument or an
// ask-question option quoting an absolute path shipped verbatim.
func TestSessionSharePayload_SanitizesSummaryCardInputs(t *testing.T) {
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

	raw, _, err := service.BuildSessionSharePayload("s1", nil, testProjectRoot, "/home/u")
	require.NoError(t, err)

	assert.NotContains(t, raw, testProjectRoot,
		"the project root must not appear anywhere in the snapshot")
	assert.NotContains(t, raw, "/home/u/.ssh",
		"a home-directory path must not survive inside a summary card")

	// The sanitized values must still be there, relativized.
	assert.Contains(t, raw, "./src/a.ts")
	assert.Contains(t, raw, "~/.ssh/id_rsa")
	assert.Contains(t, raw, "./src/secret.ts")
}

// TestSessionSharePayload_SanitizesInteractiveToolInlineInput is the regression
// guard for a leak through a tool_use block's INLINE input.
//
// model.ContentBlock.MarshalJSON keeps `input` inline for AskUserQuestion and
// PermissionApproval so their cards render immediately, and
// ConvertAskQuestionBlocks creates them with no chat_tool_calls row.
// sanitizeToolUseBlock returned early on `!found`, so those inline inputs — which
// carry tool arguments such as a shell command — reached the public snapshot
// unsanitized.
func TestSessionSharePayload_SanitizesInteractiveToolInlineInput(t *testing.T) {
	db := setupTestDBForSessionSharePayload(t)
	defer func() { _ = db.Close() }()

	seedSession(t, db, "s1")
	// Inline input, and deliberately NO chat_tool_calls row.
	seedMessage(t, db, "s1", "assistant", `{"blocks":[
		{"type":"tool_use","id":"ask1","name":"AskUserQuestion","done":true,
		 "input":{"toolInput":{"command":"cat /home/u/.ssh/id_rsa"},"file_path":"/home/u/proj/src/a.ts"}}
	],"metadata":{}}`, 0, 0)

	raw, _, err := service.BuildSessionSharePayload("s1", nil, testProjectRoot, "/home/u")
	require.NoError(t, err)

	assert.NotContains(t, raw, testProjectRoot,
		"the project root must not appear anywhere in the snapshot")
	assert.NotContains(t, raw, "/home/u/.ssh",
		"an inline tool input must not leak a home-directory path")

	// Still present, just relativized.
	blocks := contentBlocks(t, payloadMessages(t, raw)[0])
	require.NotEmpty(t, blocks)
	input, ok := blocks[0]["input"].(map[string]any)
	require.True(t, ok, "the inline input must survive for the card to render")
	toolInput, _ := input["toolInput"].(map[string]any)
	assert.Equal(t, "cat ~/.ssh/id_rsa", toolInput["command"])
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

	raw, count, err := service.BuildSessionSharePayload("s1", nil, testProjectRoot, "/home/u")
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	blocks := contentBlocks(t, payloadMessages(t, raw)[0])
	require.Len(t, blocks, 3)

	thinking := blocks[0]
	assert.Equal(t, "Let me think about this.", thinking["text"], "thinking text must be re-inlined")
	assert.Equal(t, "th_1", thinking["think_id"], "think_id must survive so collapse state keys still work")

	tool := blocks[1]
	assert.Equal(t, "./src/a.ts", tool["input"].(map[string]any)["file_path"], "input must be inlined AND relativized")
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

	raw, _, err := service.BuildSessionSharePayload("s1", nil, testProjectRoot, "/home/u")
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

	raw, _, err := service.BuildSessionSharePayload("s1", nil, testProjectRoot, "/home/u")
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

	raw, count, err := service.BuildSessionSharePayload("s1", nil, testProjectRoot, "/home/u")
	require.NoError(t, err)
	assert.Equal(t, 2, count, "only the two finalized messages are shareable")

	for _, m := range payloadMessages(t, raw) {
		assert.NotEqual(t, "half-written", m["content"])
		assert.NotEqual(t, "waiting in queue", m["content"])
	}
}

func TestSessionSharePayload_MessageIDSelection(t *testing.T) {
	db := setupTestDBForSessionSharePayload(t)
	defer func() { _ = db.Close() }()

	seedSession(t, db, "s1")
	id1 := seedMessage(t, db, "s1", "user", "first", 0, 0)
	seedMessage(t, db, "s1", "assistant", "second", 0, 0)
	id3 := seedMessage(t, db, "s1", "user", "third", 0, 0)

	// Selection order is not trusted: the snapshot must come out chronological.
	raw, count, err := service.BuildSessionSharePayload("s1", []int64{id3, id1}, testProjectRoot, "/home/u")
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	msgs := payloadMessages(t, raw)
	require.Len(t, msgs, 2)
	assert.Equal(t, "first", msgs[0]["content"], "ids must be restored to chronological order")
	assert.Equal(t, "third", msgs[1]["content"])
}

func TestSessionSharePayload_RejectsUnknownMessageID(t *testing.T) {
	db := setupTestDBForSessionSharePayload(t)
	defer func() { _ = db.Close() }()

	seedSession(t, db, "s1")
	seedMessage(t, db, "s1", "user", "hi", 0, 0)

	_, _, err := service.BuildSessionSharePayload("s1", []int64{99999}, testProjectRoot, "/home/u")
	require.ErrorIs(t, err, service.ErrSessionShareUnknownMessage)
}

// Selecting a streaming message is a client bug (the UI disables it); the server
// rejects rather than silently dropping, so the frozen snapshot always matches
// what the user believed they selected.
func TestSessionSharePayload_RejectsStreamingMessageID(t *testing.T) {
	db := setupTestDBForSessionSharePayload(t)
	defer func() { _ = db.Close() }()

	seedSession(t, db, "s1")
	seedMessage(t, db, "s1", "user", "hi", 0, 0)
	streamingID := seedMessage(t, db, "s1", "assistant", "typing...", 1, 0)

	_, _, err := service.BuildSessionSharePayload("s1", []int64{streamingID}, testProjectRoot, "/home/u")
	require.ErrorIs(t, err, service.ErrSessionShareUnknownMessage)
}

func TestSessionSharePayload_UnknownSession(t *testing.T) {
	db := setupTestDBForSessionSharePayload(t)
	defer func() { _ = db.Close() }()

	_, _, err := service.BuildSessionSharePayload("nope", nil, testProjectRoot, "/home/u")
	require.Error(t, err)
}

func TestSessionSharePayload_EmptySession(t *testing.T) {
	db := setupTestDBForSessionSharePayload(t)
	defer func() { _ = db.Close() }()

	seedSession(t, db, "s1")
	_, _, err := service.BuildSessionSharePayload("s1", nil, testProjectRoot, "/home/u")
	require.ErrorIs(t, err, service.ErrSessionShareEmptySelection)
}

// The snapshot is served anonymously, so the creator's absolute paths must not
// leak: project-relative paths become relative, home paths become ~, and the
// session's own project_path never appears at all.
func TestSessionSharePayload_SanitizesPaths(t *testing.T) {
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

	raw, _, err := service.BuildSessionSharePayload("s1", nil, testProjectRoot, "/home/u")
	require.NoError(t, err)

	assert.NotContains(t, raw, testProjectRoot, "project root must not appear anywhere in the snapshot")
	assert.NotContains(t, raw, "/home/u/.ssh", "home paths must be relativized to ~")

	msg := payloadMessages(t, raw)[0]
	blocks := contentBlocks(t, msg)
	assert.Equal(t, "./src/a.ts", blocks[0]["input"].(map[string]any)["file_path"])
	assert.Contains(t, blocks[0]["output"], "./src/a.ts")
	assert.Contains(t, blocks[1]["text"], "./src/a.ts")
	assert.Contains(t, blocks[1]["text"], "~/.ssh/id_rsa")

	files := msg["files"].([]any)
	assert.Equal(t, "docs/plan.md", files[0].(map[string]any)["path"])

	// The session block carries no project path field at all.
	session := decodePayload(t, raw)["session"].(map[string]any)
	assert.NotContains(t, session, "projectPath")
	assert.NotContains(t, session, "project_path")
}

// A path outside both the project and home has nothing to be relativized
// against; it must degrade to its base name rather than pass through whole.
func TestSessionSharePayload_OutsidePathFallsBackToBaseName(t *testing.T) {
	db := setupTestDBForSessionSharePayload(t)
	defer func() { _ = db.Close() }()

	seedSession(t, db, "s1")
	msgID := seedMessage(t, db, "s1", "user", "see this", 0, 0)
	_, err := db.Exec(`UPDATE chat_history SET files = ? WHERE id = ?`,
		`[{"path":"/etc/secret/credentials.txt","isDir":false}]`, msgID)
	require.NoError(t, err)

	raw, _, err := service.BuildSessionSharePayload("s1", nil, testProjectRoot, "/home/u")
	require.NoError(t, err)

	files := payloadMessages(t, raw)[0]["files"].([]any)
	assert.Equal(t, "credentials.txt", files[0].(map[string]any)["path"])
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

	raw, _, err := service.BuildSessionSharePayload("s1", nil, testProjectRoot, "/home/u")
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

	raw, _, err := service.BuildSessionSharePayload("s1", nil, testProjectRoot, "/home/u")
	require.NoError(t, err)

	out := decodePayload(t, raw)
	assert.EqualValues(t, 1, out["version"])
	assert.NotEmpty(t, out["createdAt"])

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

	raw, _, err := service.BuildSessionSharePayload("s1", nil, testProjectRoot, "/home/u")
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

	raw, _, err := service.BuildSessionSharePayload("s1", nil, testProjectRoot, "/home/u")
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

	raw, _, err := service.BuildSessionSharePayload("s1", nil, testProjectRoot, "/home/u")
	require.NoError(t, err)

	assert.Equal(t, "just some text, not json", payloadMessages(t, raw)[0]["content"])
}

// A user message stored in block form must also get its paths sanitized.
func TestSessionSharePayload_UserBlockContentIsSanitized(t *testing.T) {
	db := setupTestDBForSessionSharePayload(t)
	defer func() { _ = db.Close() }()

	seedSession(t, db, "s1")
	seedMessage(t, db, "s1", "user", `{"blocks":[{"type":"text","text":"look at /home/u/proj/x.go"}]}`, 0, 0)

	raw, _, err := service.BuildSessionSharePayload("s1", nil, testProjectRoot, "/home/u")
	require.NoError(t, err)

	blocks := contentBlocks(t, payloadMessages(t, raw)[0])
	assert.Equal(t, "look at ./x.go", blocks[0]["text"])
}

// ─── Selection listing ───────────────────────────────────────────────────────

// The dialog needs every message, including in-flight ones, so it can show them
// disabled with a reason instead of hiding them.
func TestGetSessionMessagesForSelection_IncludesInFlight(t *testing.T) {
	db := setupTestDBForSessionSharePayload(t)
	defer func() { _ = db.Close() }()

	seedSession(t, db, "s1")
	seedMessage(t, db, "s1", "user", "hello there", 0, 0)
	seedMessage(t, db, "s1", "assistant", `{"blocks":[{"type":"text","text":"a reply"}]}`, 0, 0)
	seedMessage(t, db, "s1", "assistant", "typing", 1, 0)
	seedMessage(t, db, "s1", "user", "queued msg", 0, 1)

	items, err := service.GetSessionMessagesForSelection("s1")
	require.NoError(t, err)
	require.Len(t, items, 4, "in-flight messages must be listed, not hidden")

	assert.False(t, items[0].Streaming)
	assert.False(t, items[0].Queued)
	assert.Equal(t, "hello there", items[0].Preview)
	assert.True(t, items[2].Streaming, "streaming flag must be reported")
	assert.True(t, items[3].Queued, "queued flag must be reported")

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

func TestGetSessionMessagesForSelection_UnknownSessionIsEmpty(t *testing.T) {
	db := setupTestDBForSessionSharePayload(t)
	defer func() { _ = db.Close() }()

	items, err := service.GetSessionMessagesForSelection("nope")
	require.NoError(t, err)
	assert.Empty(t, items)
}
