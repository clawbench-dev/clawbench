package service

import (
	"database/sql"
	"encoding/json"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file is in package service (not service_test) because it drives the
// unexported sanitizers and the block inliner directly. Going through
// BuildSessionSharePayload would only reach the branches a fixture happens to
// hit; these helpers are the ones that decide what a public snapshot contains,
// so each fallback is asserted on its own.

// ─── In-package DB fixtures ──────────────────────────────────────────────────
//
// The service_test package has its own richer fixtures; these are the minimal
// copies needed to reach the unexported functions from inside the package.

const helperProjectRoot = "/home/u/proj"

func setupShareHelperDB(t *testing.T) *sql.DB {
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
		SessionSharesDDL,
	} {
		_, err = db.Exec(ddl)
		require.NoError(t, err)
	}

	cleanup := SetDBForTest(db, db)
	t.Cleanup(cleanup)
	return db
}

func seedHelperSession(t *testing.T, db *sql.DB, sessionID string) {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO chat_sessions (id, project_path, backend, title, agent_id, model)
		 VALUES (?, ?, 'codebuddy', 'Fix the login bug', 'codebuddy', 'claude-sonnet-4')`,
		sessionID, helperProjectRoot,
	)
	require.NoError(t, err)
}

func seedHelperMessage(t *testing.T, db *sql.DB, sessionID, role, content string) int64 {
	t.Helper()
	res, err := db.Exec(
		`INSERT INTO chat_history (project_path, session_id, role, content, backend)
		 VALUES (?, ?, ?, ?, 'codebuddy')`,
		helperProjectRoot, sessionID, role, content,
	)
	require.NoError(t, err)
	id, err := res.LastInsertId()
	require.NoError(t, err)
	return id
}

// ─── sanitizeSummaryCards / sanitizeFileChanges ──────────────────────────────

func TestSanitizeSummaryCards_NilIsNil(t *testing.T) {
	assert.Nil(t, sanitizeSummaryCards(nil, "/p", "/h"))
}

func TestSanitizeSummaryCards_RelativizesPathsAndDropsTaskIDs(t *testing.T) {
	cards := &model.SummaryCards{
		TaskIDs:       []int64{7, 8},
		CreatedFiles:  model.SummaryFileChanges{{Path: "/p/created.go", ToolIDs: []string{"t1"}}},
		ModifiedFiles: model.SummaryFileChanges{{Path: "/p/modified.go"}},
		Warnings:      []model.SummaryWarning{{Type: "warning", Text: "see /p/warned.go"}},
	}

	out := sanitizeSummaryCards(cards, "/p", "/h")
	require.NotNil(t, out)
	assert.Nil(t, out.TaskIDs, "task cards are out of scope on the share page")
	assert.Equal(t, "created.go", out.CreatedFiles[0].Path)
	assert.Equal(t, []string{"t1"}, out.CreatedFiles[0].ToolIDs, "tool ids are opaque and drive the diff drawer")
	assert.Equal(t, "modified.go", out.ModifiedFiles[0].Path)
	assert.Equal(t, "see ./warned.go", out.Warnings[0].Text)
}

// Empty card lists must not be replaced with non-nil empty slices: the JSON
// tags use omitempty, and a non-nil empty slice would start emitting [].
func TestSanitizeSummaryCards_LeavesEmptyListsAlone(t *testing.T) {
	out := sanitizeSummaryCards(&model.SummaryCards{}, "/p", "/h")
	require.NotNil(t, out)
	assert.Nil(t, out.CreatedFiles)
	assert.Nil(t, out.ModifiedFiles)
	assert.Nil(t, out.Warnings)
}

// ─── sanitizeShareValue ──────────────────────────────────────────────────────

func TestSanitizeShareValue_WalksArraysAndLeavesScalars(t *testing.T) {
	in := map[string]any{
		"files": []any{"/p/a.ts", map[string]any{"path": "/p/b.ts"}},
		"count": float64(3),
		"ok":    true,
		"none":  nil,
	}

	out := sanitizeShareValue(in, "/p", "/h").(map[string]any)
	assert.Equal(t, []any{"./a.ts", map[string]any{"path": "./b.ts"}}, out["files"])
	assert.Equal(t, float64(3), out["count"], "non-string leaves pass through untouched")
	assert.Equal(t, true, out["ok"])
	assert.Nil(t, out["none"])
}

// ─── sanitizeShareString ─────────────────────────────────────────────────────

func TestSanitizeShareString_EmptyStaysEmpty(t *testing.T) {
	assert.Empty(t, sanitizeShareString("", "/p", "/h"))
}

// A root of "/" trims to "" and must be skipped rather than turning every
// leading separator into "./".
func TestSanitizeShareString_EmptyRootsAreNoops(t *testing.T) {
	assert.Equal(t, "/p/a", sanitizeShareString("/p/a", "/", "/"))
}

// ─── relativizeSharePath ─────────────────────────────────────────────────────

func TestRelativizeSharePath_EmptyStaysEmpty(t *testing.T) {
	assert.Empty(t, relativizeSharePath("", "/p", "/h"))
}

func TestRelativizeSharePath_UnderHomeBecomesTilde(t *testing.T) {
	assert.Equal(t, "~/other/x.ts", relativizeSharePath("/home/u/other/x.ts", "/home/u/proj", "/home/u"))
}

// A sibling directory of the project is NOT under it (Rel yields ../proj2/x.ts);
// with no usable homeDir the path must degrade to its base name rather than
// escaping as a relative path.
func TestRelativizeSharePath_SiblingFallsBackToBaseName(t *testing.T) {
	assert.Equal(t, "x.ts", relativizeSharePath("/home/u/proj2/x.ts", "/home/u/proj", "/nonexistent-home"))
}

// ─── clipRunes ───────────────────────────────────────────────────────────────

func TestClipRunes_NonPositiveReturnsEmpty(t *testing.T) {
	assert.Empty(t, clipRunes("abc", 0))
	assert.Empty(t, clipRunes("abc", -1))
}

// ─── inlineMessageContent fallbacks ──────────────────────────────────────────

// A body that starts with "{" but is not valid JSON must fall back to the
// sanitized original instead of being dropped.
func TestInlineMessageContent_InvalidJSONFallsBack(t *testing.T) {
	msg := model.ChatMessage{ID: 1, Content: `{not json at /p/a`}
	assert.Equal(t, `{not json at ./a`, inlineMessageContent(msg, nil, map[thinkingKey]string(nil), "/p", "/h"))
}

func TestInlineMessageContent_NoBlocksKeyFallsBack(t *testing.T) {
	msg := model.ChatMessage{ID: 1, Content: `{"foo":"/p/a"}`}
	assert.Equal(t, `{"foo":"./a"}`, inlineMessageContent(msg, nil, map[thinkingKey]string(nil), "/p", "/h"))
}

func TestInlineMessageContent_BlocksNotAnArrayFallsBack(t *testing.T) {
	msg := model.ChatMessage{ID: 1, Content: `{"blocks":null}`}
	assert.Equal(t, `{"blocks":null}`, inlineMessageContent(msg, nil, map[thinkingKey]string(nil), "/p", "/h"))
}

// A non-object entry in the blocks array is skipped, not fatal.
func TestInlineMessageContent_NonObjectBlockIsSkipped(t *testing.T) {
	msg := model.ChatMessage{ID: 1, Content: `{"blocks":[123,{"type":"text","text":"/p/a"}]}`}
	got := inlineMessageContent(msg, nil, map[thinkingKey]string(nil), "/p", "/h")
	assert.Contains(t, got, "./a")
	assert.Contains(t, got, "123", "the non-object entry must survive untouched")
}

func TestInlineMessageContent_ThinkingWithoutThinkIDIsLeftAlone(t *testing.T) {
	msg := model.ChatMessage{ID: 7, Content: `{"blocks":[{"type":"thinking","done":true}]}`}
	got := inlineMessageContent(msg, nil, map[thinkingKey]string{{7, ""}: "secret"}, "/p", "/h")
	assert.NotContains(t, got, "secret", "a thinking block with no think_id cannot be matched")
}

func TestInlineMessageContent_ToolWithoutIDIsLeftAlone(t *testing.T) {
	msg := model.ChatMessage{ID: 7, Content: `{"blocks":[{"type":"tool_use","name":"Read"}]}`}
	got := inlineMessageContent(msg, map[toolCallKey]ToolCallRecord{{7, ""}: {Output: "leak"}}, map[thinkingKey]string(nil), "/p", "/h")
	assert.NotContains(t, got, "leak", "a tool_use block with no id cannot be matched to a side row")
}

// text / warning / error blocks all carry their display text in "text".
func TestInlineMessageContent_SanitizesTextWarningErrorAndMetadata(t *testing.T) {
	msg := model.ChatMessage{ID: 3, Content: `{"blocks":[
		{"type":"text","text":"t /p/a"},
		{"type":"warning","text":"w /p/b"},
		{"type":"error","text":"e /p/c"}
	],"metadata":{"url":"https://x/p/d"}}`}

	got := inlineMessageContent(msg, nil, map[thinkingKey]string(nil), "/p", "/h")
	assert.Contains(t, got, "./a")
	assert.Contains(t, got, "./b")
	assert.Contains(t, got, "./c")
	assert.Contains(t, got, "https://x./d", "metadata strings are sanitized too")
}

// Tool input is arbitrary JSON: arrays and scalar leaves must both be walked.
func TestInlineMessageContent_InlinesToolInputArraysAndScalars(t *testing.T) {
	msg := model.ChatMessage{ID: 5, Content: `{"blocks":[{"type":"tool_use","id":"t1"}]}`}
	calls := map[toolCallKey]ToolCallRecord{
		{5, "t1"}: {
			MessageID:  5,
			ToolID:     "t1",
			Input:      json.RawMessage(`{"files":["/p/a.ts",{"nested":"/p/b.ts"}],"count":2,"flag":false}`),
			Output:     "output /p/c.ts",
			Status:     "success",
			Done:       true,
			DurationMs: 9,
			Summary:    "summary /p/d.ts",
		},
	}

	got := inlineMessageContent(msg, calls, map[thinkingKey]string(nil), "/p", "/h")
	assert.Contains(t, got, "./a.ts")
	assert.Contains(t, got, "./b.ts", "nested maps inside arrays are sanitized")
	assert.Contains(t, got, `"count":2`)
	assert.Contains(t, got, "./c.ts")
	assert.Contains(t, got, "./d.ts")
	assert.Contains(t, got, `"duration_ms":9`)
}

// ─── loadSummariesForMessages ────────────────────────────────────────────────

// An empty selection short-circuits before touching the DB, so the caller can
// rely on it even when no DB is configured.
func TestLoadSummariesForMessages_EmptyIDsShortCircuits(t *testing.T) {
	summaries, cards, err := loadSummariesForMessages(nil)
	require.NoError(t, err)
	assert.Empty(t, summaries)
	assert.Empty(t, cards)
}

func TestLoadSummariesForMessages_QueryErrorSurfaces(t *testing.T) {
	db := setupShareHelperDB(t)
	defer func() { _ = db.Close() }()

	_, err := db.Exec("DROP TABLE summaries")
	require.NoError(t, err)

	_, _, err = loadSummariesForMessages([]int64{1})
	require.Error(t, err, "a broken summaries table must surface, not silently drop the cards")
}

// A summary_cards blob that is not valid JSON is skipped: the summary text is
// still useful on its own.
func TestLoadSummariesForMessages_IgnoresUnparseableCards(t *testing.T) {
	db := setupShareHelperDB(t)
	defer func() { _ = db.Close() }()

	seedHelperSession(t, db, "s1")
	msgID := seedHelperMessage(t, db, "s1", "assistant", "hi")
	_, err := db.Exec(
		`INSERT INTO summaries (target_type, target_id, summary, summary_cards) VALUES ('chat_message', ?, ?, ?)`,
		msgID, "the summary", "{not json",
	)
	require.NoError(t, err)

	summaries, cards, err := loadSummariesForMessages([]int64{msgID})
	require.NoError(t, err)
	assert.Equal(t, "the summary", summaries[msgID])
	assert.NotContains(t, cards, msgID, "unparseable cards must be dropped, not half-decoded")
}

// ─── GetSessionMessagesForSelection guards ───────────────────────────────────

func TestGetSessionMessagesForSelection_EmptySessionIDIsAnError(t *testing.T) {
	_, err := GetSessionMessagesForSelection("")
	require.Error(t, err)
}

func TestGetSessionMessagesForSelection_QueryErrorSurfaces(t *testing.T) {
	db := setupShareHelperDB(t)
	defer func() { _ = db.Close() }()

	_, err := db.Exec("DROP TABLE chat_history")
	require.NoError(t, err)

	_, err = GetSessionMessagesForSelection("s1")
	require.Error(t, err)
}

// ─── BuildSessionSharePayload error propagation ──────────────────────────────

// Each side table is read separately, so a failure in any one must abort the
// snapshot rather than silently shipping a snapshot missing that data.
func TestBuildSessionSharePayload_SideTableErrorsSurface(t *testing.T) {
	cases := []struct {
		name  string
		table string
	}{
		{"messages", "chat_history"},
		{"tool calls", "chat_tool_calls"},
		{"thinking", "chat_thinking"},
		{"summaries", "summaries"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := setupShareHelperDB(t)
			defer func() { _ = db.Close() }()

			seedHelperSession(t, db, "s1")
			seedHelperMessage(t, db, "s1", "user", "hi")
			_, err := db.Exec("DROP TABLE " + tc.table)
			require.NoError(t, err)

			_, _, err = BuildSessionSharePayload("s1", nil, helperProjectRoot, "/home/u")
			require.Error(t, err, "a missing %s table must fail the snapshot", tc.table)
		})
	}
}

// ─── trimPayloadToCap branches ───────────────────────────────────────────────

// Several candidates force the longest-first comparator to actually run.
func TestTrimPayloadToCap_SortsCandidatesLongestFirst(t *testing.T) {
	big := make([]byte, 9<<20)
	for i := range big {
		big[i] = 'a'
	}
	small := make([]byte, 9<<20)
	for i := range small {
		small[i] = 'b'
	}

	mk := func(out []byte) string {
		b, err := json.Marshal(map[string]any{
			"blocks": []any{map[string]any{"type": "tool_use", "id": "t", "output": string(out)}},
		})
		require.NoError(t, err)
		return string(b)
	}

	payload := &SessionSharePayload{
		Version: 1,
		Messages: []SessionShareMessage{
			{ID: 1, Role: "assistant", Content: mk(big)},
			{ID: 2, Role: "assistant", Content: mk(small)},
		},
	}
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	require.Greater(t, len(raw), 16<<20)

	out := trimPayloadToCap(raw, payload)
	assert.LessOrEqual(t, len(out), 16<<20)
}

// Content that is not a JSON wrapper, or a wrapper without a blocks array, is
// not a trimming candidate.
func TestTrimPayloadToCap_SkipsUntrimmableMessages(t *testing.T) {
	prose := make([]byte, 18<<20) // comfortably over the 16 MiB cap
	for i := range prose {
		prose[i] = 'p'
	}

	payload := &SessionSharePayload{
		Version: 1,
		Messages: []SessionShareMessage{
			{ID: 1, Role: "user", Content: string(prose)},                                  // not JSON
			{ID: 2, Role: "assistant", Content: `{"blocks":"nope"}`},                       // blocks not an array
			{ID: 3, Role: "assistant", Content: `{"blocks":[1,2,3]}`},                      // entries not objects
			{ID: 4, Role: "assistant", Content: `{"blocks":[{"type":"text","text":"x"}]}`}, // not tool_use
		},
	}
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	require.Greater(t, len(raw), 16<<20)

	out := trimPayloadToCap(raw, payload)
	assert.Equal(t, string(raw), string(out), "nothing is trimmable, so the content must be preserved")
}

// An output already at/below the floor is left alone rather than gutted.
func TestTrimPayloadToCap_StopsAtFloor(t *testing.T) {
	// Two candidates, each with a large output; trimming must terminate and
	// never cut below the floor.
	chunk := make([]byte, 8<<20)
	for i := range chunk {
		chunk[i] = 'c'
	}
	mk := func() string {
		b, err := json.Marshal(map[string]any{
			"blocks": []any{map[string]any{"type": "tool_use", "id": "t", "output": string(chunk)}},
		})
		require.NoError(t, err)
		return string(b)
	}

	payload := &SessionSharePayload{
		Version: 1,
		Messages: []SessionShareMessage{
			{ID: 1, Role: "assistant", Content: mk()},
			{ID: 2, Role: "assistant", Content: mk()},
		},
	}
	raw, err := json.Marshal(payload)
	require.NoError(t, err)

	out := trimPayloadToCap(raw, payload)
	assert.LessOrEqual(t, len(out), 16<<20)

	var back SessionSharePayload
	require.NoError(t, json.Unmarshal(out, &back))
	for _, m := range back.Messages {
		var wrapper struct {
			Blocks []map[string]any `json:"blocks"`
		}
		require.NoError(t, json.Unmarshal([]byte(m.Content), &wrapper))
		require.NotEmpty(t, wrapper.Blocks)
		outStr, _ := wrapper.Blocks[0]["output"].(string)
		assert.GreaterOrEqual(t, len(outStr), sessionShareOutputTrimFloor,
			"a trimmed output must never fall below the floor")
	}
}

// When the longest output hits the floor and the payload is STILL over the cap,
// the trimmer must go on to the next-longest candidate. Stopping at the first
// floor (instead of continuing) would return an over-cap snapshot — the exact
// failure the write-back fix was about.
func TestTrimPayloadToCap_ContinuesPastAFloorStoppedCandidate(t *testing.T) {
	mk := func(n int) string {
		buf := make([]byte, n)
		for i := range buf {
			buf[i] = 'c'
		}
		b, err := json.Marshal(map[string]any{
			"blocks": []any{map[string]any{"type": "tool_use", "id": "t", "output": string(buf)}},
		})
		require.NoError(t, err)
		return string(b)
	}

	// The first candidate can be cut only down to the floor, which is not enough
	// on its own; the second must then be trimmed too.
	payload := &SessionSharePayload{
		Version: 1,
		Messages: []SessionShareMessage{
			{ID: 1, Role: "assistant", Content: mk(20 << 20)},
			{ID: 2, Role: "assistant", Content: mk(20 << 20)},
		},
	}
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	require.Greater(t, len(raw), 16<<20)

	out := trimPayloadToCap(raw, payload)
	assert.LessOrEqual(t, len(out), 16<<20,
		"the trimmer must keep going after a candidate stops at the floor")
}

// A closed DB must not turn into a silent success anywhere in the read path.
func TestSessionShareReads_ClosedDBSurfacesErrors(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	require.NoError(t, db.Close())
	cleanup := SetDBForTest(db, db)
	t.Cleanup(cleanup)

	_, err = GetSessionMessagesForSelection("s1")
	require.Error(t, err)

	_, _, err = loadSummariesForMessages([]int64{1})
	require.Error(t, err)

	_, err = loadToolCallsByMessage("s1")
	require.Error(t, err)

	_, err = loadThinkingByMessage("s1")
	require.Error(t, err)
}
