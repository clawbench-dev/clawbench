package service

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestThinkingCRUD(t *testing.T) {
	dbDir := t.TempDir()
	if err := initTestDB(dbDir); err != nil {
		t.Fatalf("initTestDB: %v", err)
	}
	defer func() {
		db.Close()
		dbRead.Close()
	}()

	sessionID := "thinking-sess-001"
	_, _ = db.Exec("INSERT INTO chat_sessions (id, project_path, backend, title) VALUES (?, ?, ?, ?)",
		sessionID, "/test", "test", "Test Session")
	res, err := db.Exec("INSERT INTO chat_history (project_path, role, content, session_id, backend) VALUES (?, ?, ?, ?, ?)",
		"/test", "assistant", `{"blocks":[]}`, sessionID, "test")
	if err != nil {
		t.Fatalf("insert message: %v", err)
	}
	msgID, _ := res.LastInsertId()

	t.Run("insert new thinking", func(t *testing.T) {
		if err := UpsertThinking(msgID, sessionID, "th_abc123", "thinking text"); err != nil {
			t.Fatalf("UpsertThinking: %v", err)
		}
		rec, err := GetThinking("th_abc123", msgID)
		if err != nil {
			t.Fatalf("GetThinking: %v", err)
		}
		require.NotNil(t, rec, "GetThinking returned nil")
		if rec.ThinkID != "th_abc123" || rec.Text != "thinking text" || rec.MessageID != msgID || rec.SessionID != sessionID {
			t.Errorf("record mismatch: %+v", rec)
		}
	})

	t.Run("upsert overwrites text", func(t *testing.T) {
		if err := UpsertThinking(msgID, sessionID, "th_abc123", "updated text"); err != nil {
			t.Fatalf("UpsertThinking: %v", err)
		}
		rec, _ := GetThinking("th_abc123", msgID)
		if rec.Text != "updated text" {
			t.Errorf("Text = %q, want updated text", rec.Text)
		}
	})

	t.Run("get missing returns nil", func(t *testing.T) {
		rec, err := GetThinking("th_missing", msgID)
		if err != nil || rec != nil {
			t.Errorf("expected nil,nil got %+v,%v", rec, err)
		}
	})

	t.Run("get by session fallback", func(t *testing.T) {
		rec, err := GetThinkingBySession("th_abc123", sessionID)
		if err != nil || rec == nil || rec.Text != "updated text" {
			t.Errorf("GetThinkingBySession failed: rec=%+v err=%v", rec, err)
		}
		rec2, err := GetThinkingBySession("th_abc123", "other-session")
		if err != nil || rec2 != nil {
			t.Errorf("expected nil for other session, got %+v,%v", rec2, err)
		}
	})

	t.Run("delete by message", func(t *testing.T) {
		if err := DeleteThinkingByMessage(msgID); err != nil {
			t.Fatalf("DeleteThinkingByMessage: %v", err)
		}
		rec, _ := GetThinking("th_abc123", msgID)
		if rec != nil {
			t.Error("expected nil after delete")
		}
	})
}

func TestAppendThinkingSegment_GetThinkingConcat(t *testing.T) {
	dbDir := t.TempDir()
	if err := initTestDB(dbDir); err != nil {
		t.Fatalf("initTestDB: %v", err)
	}
	defer func() {
		db.Close()
		dbRead.Close()
	}()

	sessionID := "thinking-append-sess"
	_, _ = db.Exec("INSERT INTO chat_sessions (id, project_path, backend, title) VALUES (?, ?, ?, ?)",
		sessionID, "/test", "test", "Test Session")
	res, err := db.Exec("INSERT INTO chat_history (project_path, role, content, session_id, backend) VALUES (?, ?, ?, ?, ?)",
		"/test", "assistant", `{"blocks":[]}`, sessionID, "test")
	if err != nil {
		t.Fatalf("insert message: %v", err)
	}
	msgID, _ := res.LastInsertId()

	t.Run("append chunks then concat in seq order", func(t *testing.T) {
		if err := AppendThinkingSegment(msgID, sessionID, "th_app1", 0, "part1"); err != nil {
			t.Fatalf("append seq0: %v", err)
		}
		if err := AppendThinkingSegment(msgID, sessionID, "th_app1", 1, "part2"); err != nil {
			t.Fatalf("append seq1: %v", err)
		}
		if err := AppendThinkingSegment(msgID, sessionID, "th_app1", 2, "part3"); err != nil {
			t.Fatalf("append seq2: %v", err)
		}
		rec, err := GetThinking("th_app1", msgID)
		if err != nil {
			t.Fatalf("GetThinking: %v", err)
		}
		require.NotNil(t, rec, "GetThinking returned nil")
		if rec.Text != "part1part2part3" {
			t.Errorf("Text = %q, want concatenated part1part2part3", rec.Text)
		}
	})

	t.Run("upsert full text replaces chunks without duplication", func(t *testing.T) {
		if err := UpsertThinking(msgID, sessionID, "th_app1", "full replacement"); err != nil {
			t.Fatalf("UpsertThinking: %v", err)
		}
		rec, _ := GetThinking("th_app1", msgID)
		if rec.Text != "full replacement" {
			t.Errorf("Text = %q, want full replacement (no chunk duplication)", rec.Text)
		}
		// Only one row remains after the full-text upsert.
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM chat_thinking WHERE think_id = 'th_app1'").Scan(&count); err != nil {
			t.Fatalf("count: %v", err)
		}
		if count != 1 {
			t.Errorf("expected 1 row after UpsertThinking, got %d", count)
		}
	})

	t.Run("idempotent append same seq overwrites not duplicates", func(t *testing.T) {
		if err := AppendThinkingSegment(msgID, sessionID, "th_app2", 0, "alpha"); err != nil {
			t.Fatalf("append seq0: %v", err)
		}
		// Simulated retry of the same seq after a failure.
		if err := AppendThinkingSegment(msgID, sessionID, "th_app2", 0, "alpha"); err != nil {
			t.Fatalf("append seq0 retry: %v", err)
		}
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM chat_thinking WHERE think_id = 'th_app2'").Scan(&count); err != nil {
			t.Fatalf("count: %v", err)
		}
		if count != 1 {
			t.Errorf("expected 1 row after idempotent retry, got %d", count)
		}
	})
}

func TestGetThinkingBySessionAll(t *testing.T) {
	dbDir := t.TempDir()
	if err := initTestDB(dbDir); err != nil {
		t.Fatalf("initTestDB: %v", err)
	}
	defer func() {
		db.Close()
		dbRead.Close()
	}()

	sessionID := "thinking-all-sess"
	_, _ = db.Exec("INSERT INTO chat_sessions (id, project_path, backend, title) VALUES (?, ?, ?, ?)",
		sessionID, "/test", "test", "Test Session")
	res, err := db.Exec("INSERT INTO chat_history (project_path, role, content, session_id, backend) VALUES (?, ?, ?, ?, ?)",
		"/test", "assistant", `{"blocks":[]}`, sessionID, "test")
	if err != nil {
		t.Fatalf("insert message: %v", err)
	}
	msgID, _ := res.LastInsertId()

	t.Run("returns empty for session with no thinking", func(t *testing.T) {
		records, err := GetThinkingBySessionAll(sessionID)
		if err != nil {
			t.Fatalf("GetThinkingBySessionAll: %v", err)
		}
		if len(records) != 0 {
			t.Errorf("expected 0 records, got %d", len(records))
		}
	})

	t.Run("returns all thinking records for session", func(t *testing.T) {
		if err := UpsertThinking(msgID, sessionID, "th_001", "first thought"); err != nil {
			t.Fatalf("UpsertThinking: %v", err)
		}
		if err := UpsertThinking(msgID, sessionID, "th_002", "second thought"); err != nil {
			t.Fatalf("UpsertThinking: %v", err)
		}
		records, err := GetThinkingBySessionAll(sessionID)
		if err != nil {
			t.Fatalf("GetThinkingBySessionAll: %v", err)
		}
		if len(records) != 2 {
			t.Fatalf("expected 2 records, got %d", len(records))
		}
		got := map[string]string{}
		for _, r := range records {
			got[r.ThinkID] = r.Text
		}
		if got["th_001"] != "first thought" || got["th_002"] != "second thought" {
			t.Errorf("mismatch: %+v", got)
		}
	})
}

func TestGenerateThinkingID(t *testing.T) {
	a, b := generateThinkingID(), generateThinkingID()
	if a == "" || b == "" {
		t.Fatal("generateThinkingID returned empty")
	}
	if a == b {
		t.Error("two generated IDs should differ")
	}
}

func TestSlimThinkingInContent(t *testing.T) {
	t.Run("extracts thinking and keeps metadata", func(t *testing.T) {
		in := `{"blocks":[
			{"type":"text","text":"intro"},
			{"type":"thinking","text":"deep reasoning","done":true},
			{"type":"tool_use","id":"toolu_x","name":"Bash","done":true}
		],"metadata":{"model":"claude"}}`
		slim, records, err := slimThinkingInContent(in)
		if err != nil {
			t.Fatalf("slimThinkingInContent: %v", err)
		}
		if len(records) != 1 {
			t.Fatalf("records = %d, want 1", len(records))
		}
		if records[0].Text != "deep reasoning" || records[0].ThinkID == "" {
			t.Errorf("record mismatch: %+v", records[0])
		}
		var parsed struct {
			Blocks   []map[string]any `json:"blocks"`
			Metadata map[string]any   `json:"metadata"`
		}
		if err := json.Unmarshal([]byte(slim), &parsed); err != nil {
			t.Fatalf("unmarshal slim: %v", err)
		}
		if parsed.Blocks[1]["think_id"] != records[0].ThinkID {
			t.Errorf("think_id not in slim block: %v", parsed.Blocks[1])
		}
		if _, hasText := parsed.Blocks[1]["text"]; hasText {
			t.Error("slim block should not have text")
		}
		if parsed.Blocks[1]["done"] != true {
			t.Error("slim block should preserve done")
		}
		if parsed.Blocks[0]["text"] != "intro" {
			t.Error("text block should be untouched")
		}
		if parsed.Metadata["model"] != "claude" {
			t.Error("metadata should be preserved")
		}
	})

	t.Run("no thinking returns unchanged", func(t *testing.T) {
		in := `{"blocks":[{"type":"text","text":"hi"}]}`
		slim, records, err := slimThinkingInContent(in)
		if err != nil || len(records) != 0 || slim != in {
			t.Errorf("expected unchanged, got slim=%q records=%v err=%v", slim, records, err)
		}
	})

	t.Run("already slim thinking skipped", func(t *testing.T) {
		in := `{"blocks":[{"type":"thinking","think_id":"th_x","done":true}]}`
		slim, records, err := slimThinkingInContent(in)
		if err != nil || len(records) != 0 || slim != in {
			t.Errorf("expected unchanged, got slim=%q records=%v err=%v", slim, records, err)
		}
	})

	t.Run("slims empty-text thinking with no record", func(t *testing.T) {
		in := `{"blocks":[{"type":"thinking","done":true},{"type":"text","text":"hi"}]}`
		slim, records, err := slimThinkingInContent(in)
		if err != nil {
			t.Fatalf("slimThinkingInContent: %v", err)
		}
		if len(records) != 0 {
			t.Fatalf("records = %d, want 0", len(records))
		}
		if slim == in {
			t.Error("content should be rewritten (think_id added)")
		}
		var parsed struct {
			Blocks []map[string]any `json:"blocks"`
		}
		if err := json.Unmarshal([]byte(slim), &parsed); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if parsed.Blocks[0]["think_id"] == "" {
			t.Errorf("empty-text thinking block should get think_id: %v", parsed.Blocks[0])
		}
	})
}

// TestSlimThinkingInContent_ForcesDoneOnSlimMarker pins the invariant that a
// text-less slim marker must never claim to be in-progress. Such a marker is
// rendered by the frontend as a perpetual "输出中" spinner with no content (the
// text was just moved to chat_thinking, so nothing will ever arrive to finish
// it). Production accumulated ~181k of these because Finalize slimmed
// unconditionally while the live flush path gated on done.
func TestSlimThinkingInContent_ForcesDoneOnSlimMarker(t *testing.T) {
	in := `{"blocks":[
		{"type":"thinking","text":"still open reasoning","done":false},
		{"type":"text","text":"reply"}
	]}`
	slim, records, err := slimThinkingInContent(in)
	if err != nil {
		t.Fatalf("slimThinkingInContent: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1", len(records))
	}
	var parsed struct {
		Blocks []map[string]any `json:"blocks"`
	}
	if err := json.Unmarshal([]byte(slim), &parsed); err != nil {
		t.Fatalf("unmarshal slim: %v", err)
	}
	if parsed.Blocks[0]["done"] != true {
		t.Errorf("slim marker must be done=true, got %v (a done=false marker renders as a permanent spinner)",
			parsed.Blocks[0]["done"])
	}
	if _, hasText := parsed.Blocks[0]["text"]; hasText {
		t.Error("slim marker must not carry text")
	}
}

// TestSlimThinkingInContent_DoneAbsentBecomesTrue covers rows written before the
// done field existed: an absent flag must also become true on the terminal path.
func TestSlimThinkingInContent_DoneAbsentBecomesTrue(t *testing.T) {
	in := `{"blocks":[{"type":"thinking","text":"legacy reasoning"}]}`
	slim, records, err := slimThinkingInContent(in)
	if err != nil {
		t.Fatalf("slimThinkingInContent: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1", len(records))
	}
	var parsed struct {
		Blocks []map[string]any `json:"blocks"`
	}
	if err := json.Unmarshal([]byte(slim), &parsed); err != nil {
		t.Fatalf("unmarshal slim: %v", err)
	}
	if parsed.Blocks[0]["done"] != true {
		t.Errorf("absent done must become true on slim, got %v", parsed.Blocks[0]["done"])
	}
}

func TestPersistThinkingToDB_ParseErrorFallback(t *testing.T) {
	dbDir := t.TempDir()
	if err := initTestDB(dbDir); err != nil {
		t.Fatalf("initTestDB: %v", err)
	}
	defer func() { db.Close(); dbRead.Close() }()

	bad := "not json {"
	got := persistThinkingToDB(bad, 42, "sess-1")
	if got != bad {
		t.Errorf("expected original content back on parse error, got %q", got)
	}
}

func TestSlimThinkingInContent_PreservesParentToolCallID(t *testing.T) {
	// A sub-agent thinking block must keep its parent link through slimming, so
	// a reload can regroup it under the parent Agent card.
	in := `{"blocks":[
		{"type":"thinking","text":"child reasoning","done":true,"parent_tool_call_id":"call_p"}
	]}`
	slim, records, err := slimThinkingInContent(in)
	if err != nil {
		t.Fatalf("slimThinkingInContent: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1", len(records))
	}
	var parsed struct {
		Blocks []map[string]any `json:"blocks"`
	}
	if err := json.Unmarshal([]byte(slim), &parsed); err != nil {
		t.Fatalf("unmarshal slim: %v", err)
	}
	if parsed.Blocks[0]["parent_tool_call_id"] != "call_p" {
		t.Errorf("parent_tool_call_id lost in slim block: %v", parsed.Blocks[0])
	}
	if _, hasText := parsed.Blocks[0]["text"]; hasText {
		t.Error("slim block should not have text")
	}
}

// ── ReplaceThinkingForMessage: single-transaction batch rewrite ──
//
// Before this function existed, persistThinkingToDB deleted the message's rows
// and then called UpsertThinking once per record — N+1 independent transactions
// with no outer transaction. A 9737-block turn produced 5938 rows and took
// 5.89s (95% of finalize), and a crash between the delete and the inserts lost
// the message's reasoning entirely while the content row kept its slim
// think_id markers.
func TestReplaceThinkingForMessage(t *testing.T) {
	dbDir := t.TempDir()
	if err := initTestDB(dbDir); err != nil {
		t.Fatalf("initTestDB: %v", err)
	}
	defer func() {
		db.Close()
		dbRead.Close()
	}()

	sessionID := "thinking-batch-sess"
	_, _ = db.Exec("INSERT INTO chat_sessions (id, project_path, backend, title) VALUES (?, ?, ?, ?)",
		sessionID, "/test", "test", "Batch Session")
	res, err := db.Exec("INSERT INTO chat_history (project_path, role, content, session_id, backend) VALUES (?, ?, ?, ?, ?)",
		"/test", "assistant", `{"blocks":[]}`, sessionID, "test")
	if err != nil {
		t.Fatalf("insert message: %v", err)
	}
	msgID, _ := res.LastInsertId()

	countRows := func(t *testing.T) int {
		t.Helper()
		var n int
		if err := dbRead.QueryRow("SELECT count(*) FROM chat_thinking WHERE message_id = ?", msgID).Scan(&n); err != nil {
			t.Fatalf("count: %v", err)
		}
		return n
	}

	t.Run("rewrites all rows in one call", func(t *testing.T) {
		// Pre-existing rows, as the 500ms streaming flush would have left them.
		require.NoError(t, UpsertThinking(msgID, sessionID, "th_stale", "stale text"))
		require.NoError(t, UpsertThinking(msgID, sessionID, "th_keep", "old text"))

		recs := []ThinkingRecord{
			{ThinkID: "th_keep", Text: "final keep"},
			{ThinkID: "th_new", Text: "brand new"},
		}
		require.NoError(t, ReplaceThinkingForMessage(msgID, sessionID, recs))

		if got := countRows(t); got != 2 {
			t.Fatalf("row count = %d, want 2", got)
		}
		// The retired think_id must be gone — MergeConsecutiveThinkingBlocks
		// merges adjacent blocks and leaves such orphans behind.
		if rec, _ := GetThinking("th_stale", msgID); rec != nil {
			t.Errorf("stale think_id still present: %+v", rec)
		}
		if rec, _ := GetThinking("th_keep", msgID); rec == nil || rec.Text != "final keep" {
			t.Errorf("th_keep = %+v, want text 'final keep'", rec)
		}
		if rec, _ := GetThinking("th_new", msgID); rec == nil || rec.Text != "brand new" {
			t.Errorf("th_new = %+v, want text 'brand new'", rec)
		}
	})

	t.Run("handles more rows than the bound-variable limit allows in one INSERT", func(t *testing.T) {
		// SQLite caps a statement at 32766 bound variables and this INSERT binds
		// 4 per row (seq is a literal), so an unchunked statement fails past
		// 8191 rows. 9000 exceeds that ceiling, so this fails if the chunking is
		// removed. The production message that triggered this work had 5938 rows,
		// which fits one statement but sits close enough to the edge to matter.
		const n = 9000
		recs := make([]ThinkingRecord, 0, n)
		for i := range n {
			recs = append(recs, ThinkingRecord{
				ThinkID: fmt.Sprintf("th_bulk_%05d", i),
				Text:    fmt.Sprintf("reasoning segment %d", i),
			})
		}
		require.NoError(t, ReplaceThinkingForMessage(msgID, sessionID, recs))
		if got := countRows(t); got != n {
			t.Fatalf("row count = %d, want %d", got, n)
		}
		last, err := GetThinking(fmt.Sprintf("th_bulk_%05d", n-1), msgID)
		require.NoError(t, err)
		require.NotNil(t, last)
		require.Equal(t, fmt.Sprintf("reasoning segment %d", n-1), last.Text)
	})

	t.Run("skips records with empty think_id or text", func(t *testing.T) {
		recs := []ThinkingRecord{
			{ThinkID: "th_ok", Text: "valid"},
			{ThinkID: "", Text: "no id"},
			{ThinkID: "th_empty_text", Text: ""},
		}
		require.NoError(t, ReplaceThinkingForMessage(msgID, sessionID, recs))
		if got := countRows(t); got != 1 {
			t.Fatalf("row count = %d, want 1 (only the valid record)", got)
		}
	})

	t.Run("empty record set is a no-op", func(t *testing.T) {
		require.NoError(t, UpsertThinking(msgID, sessionID, "th_survivor", "still here"))
		require.NoError(t, ReplaceThinkingForMessage(msgID, sessionID, nil))
		if rec, _ := GetThinking("th_survivor", msgID); rec == nil {
			t.Error("empty record set must not delete existing rows")
		}
	})
}

// TestReplaceThinkingForMessage_AtomicOnFailure pins the property the old
// delete-then-upsert loop lacked: a failure part-way through must leave the
// previous rows untouched, never a half-rewritten message. Under the old shape
// the DELETE had already committed on its own, so a failure during the inserts
// left the message with no thinking rows at all.
//
// The failure is injected with a BEFORE INSERT trigger that aborts on a marker
// text. That fires after the DELETE inside the transaction, which is exactly the
// window that used to lose data.
func TestReplaceThinkingForMessage_AtomicOnFailure(t *testing.T) {
	dbDir := t.TempDir()
	if err := initTestDB(dbDir); err != nil {
		t.Fatalf("initTestDB: %v", err)
	}
	defer func() {
		db.Close()
		dbRead.Close()
	}()

	sessionID := "thinking-atomic-sess"
	_, _ = db.Exec("INSERT INTO chat_sessions (id, project_path, backend, title) VALUES (?, ?, ?, ?)",
		sessionID, "/test", "test", "Atomic Session")
	res, err := db.Exec("INSERT INTO chat_history (project_path, role, content, session_id, backend) VALUES (?, ?, ?, ?, ?)",
		"/test", "assistant", `{"blocks":[]}`, sessionID, "test")
	if err != nil {
		t.Fatalf("insert message: %v", err)
	}
	msgID, _ := res.LastInsertId()

	// Rows that must survive a failed rewrite.
	require.NoError(t, UpsertThinking(msgID, sessionID, "th_preexisting", "precious"))

	// Abort any insert carrying the marker text.
	_, err = db.Exec(`
		CREATE TRIGGER fail_marked_insert BEFORE INSERT ON chat_thinking
		WHEN NEW.text = 'TRIGGER_FAIL'
		BEGIN SELECT RAISE(ABORT, 'injected failure'); END;
	`)
	require.NoError(t, err)

	bad := []ThinkingRecord{
		{ThinkID: "th_first", Text: "fine"},
		{ThinkID: "th_bad", Text: "TRIGGER_FAIL"},
	}
	err = ReplaceThinkingForMessage(msgID, sessionID, bad)
	require.Error(t, err, "a failing record must surface an error")

	// The whole rewrite rolled back: the pre-existing row is intact and none of
	// the new rows leaked in.
	rec, err := GetThinking("th_preexisting", msgID)
	require.NoError(t, err)
	require.NotNil(t, rec, "pre-existing row must survive a failed batch rewrite")
	require.Equal(t, "precious", rec.Text)

	if leaked, _ := GetThinking("th_first", msgID); leaked != nil {
		t.Errorf("row from the failed transaction leaked: %+v", leaked)
	}

	var n int
	require.NoError(t, dbRead.QueryRow("SELECT count(*) FROM chat_thinking WHERE message_id = ?", msgID).Scan(&n))
	require.Equal(t, 1, n, "only the pre-existing row may remain")
}

// TestReplaceThinkingForMessage_DuplicateThinkIDLastWins guards a semantic the
// batch must preserve: the previous delete-then-insert-per-record loop resolved a
// repeated think_id by last-wins (each iteration deleted the prior row and wrote
// its own). A plain multi-row INSERT would instead violate
// UNIQUE(think_id, message_id, seq) and abort the whole rewrite.
func TestReplaceThinkingForMessage_DuplicateThinkIDLastWins(t *testing.T) {
	dbDir := t.TempDir()
	if err := initTestDB(dbDir); err != nil {
		t.Fatalf("initTestDB: %v", err)
	}
	defer func() {
		db.Close()
		dbRead.Close()
	}()

	sessionID := "thinking-dup-sess"
	_, _ = db.Exec("INSERT INTO chat_sessions (id, project_path, backend, title) VALUES (?, ?, ?, ?)",
		sessionID, "/test", "test", "Dup Session")
	res, err := db.Exec("INSERT INTO chat_history (project_path, role, content, session_id, backend) VALUES (?, ?, ?, ?, ?)",
		"/test", "assistant", `{"blocks":[]}`, sessionID, "test")
	if err != nil {
		t.Fatalf("insert message: %v", err)
	}
	msgID, _ := res.LastInsertId()

	recs := []ThinkingRecord{
		{ThinkID: "th_dup", Text: "first"},
		{ThinkID: "th_other", Text: "other"},
		{ThinkID: "th_dup", Text: "second"},
	}
	require.NoError(t, ReplaceThinkingForMessage(msgID, sessionID, recs))

	var n int
	require.NoError(t, dbRead.QueryRow("SELECT count(*) FROM chat_thinking WHERE message_id = ?", msgID).Scan(&n))
	require.Equal(t, 2, n, "duplicate think_id must collapse to one row")

	rec, err := GetThinking("th_dup", msgID)
	require.NoError(t, err)
	require.NotNil(t, rec)
	require.Equal(t, "second", rec.Text, "the last occurrence wins, as before")
}

// TestPersistThinkingToDB_SlimsAndReplacesRows covers the end-to-end
// Finalize entry point: it must slim the content, drop rows whose think_id the
// final merge retired, and store exactly the live records.
//
// Scope note: this pins the observable outcome of persistThinkingToDB, NOT that
// the write is transactional — the old per-record shape produced the same rows,
// so reverting the wiring does not fail here. Transactionality is covered by
// TestReplaceThinkingForMessage_AtomicOnFailure.
func TestPersistThinkingToDB_SlimsAndReplacesRows(t *testing.T) {
	dbDir := t.TempDir()
	if err := initTestDB(dbDir); err != nil {
		t.Fatalf("initTestDB: %v", err)
	}
	defer func() {
		db.Close()
		dbRead.Close()
	}()

	sessionID := "thinking-persist-sess"
	_, _ = db.Exec("INSERT INTO chat_sessions (id, project_path, backend, title) VALUES (?, ?, ?, ?)",
		sessionID, "/test", "test", "Persist Session")
	res, err := db.Exec("INSERT INTO chat_history (project_path, role, content, session_id, backend) VALUES (?, ?, ?, ?, ?)",
		"/test", "assistant", `{"blocks":[]}`, sessionID, "test")
	if err != nil {
		t.Fatalf("insert message: %v", err)
	}
	msgID, _ := res.LastInsertId()

	// Rows the streaming flush already persisted, including one whose think_id
	// the final merge retired (must disappear).
	require.NoError(t, AppendThinkingSegment(msgID, sessionID, "th_retired", 0, "old partial"))
	require.NoError(t, AppendThinkingSegment(msgID, sessionID, "th_live", 0, "old partial"))

	// Finalize content: thinking text present for th_live, th_retired gone, and
	// one brand-new block. After persistThinkingToDB the content must be slim
	// and chat_thinking must hold exactly the two live records.
	content := `{"blocks":[
		{"type":"thinking","think_id":"th_live","text":"final reasoning"},
		{"type":"thinking","text":"brand new reasoning"},
		{"type":"text","text":"the answer"}
	]}`

	slim := persistThinkingToDB(content, msgID, sessionID)
	require.NotEqual(t, content, slim, "content should be slimmed")
	require.NotContains(t, slim, "final reasoning", "thinking text must move out of content")
	require.Contains(t, slim, "th_live", "slim block keeps its think_id marker")

	var n int
	require.NoError(t, dbRead.QueryRow("SELECT count(*) FROM chat_thinking WHERE message_id = ?", msgID).Scan(&n))
	require.Equal(t, 2, n, "exactly the two live records")

	if rec, _ := GetThinking("th_retired", msgID); rec != nil {
		t.Errorf("retired think_id must be removed, got %+v", rec)
	}
	live, err := GetThinking("th_live", msgID)
	require.NoError(t, err)
	require.NotNil(t, live)
	require.Equal(t, "final reasoning", live.Text)
}
