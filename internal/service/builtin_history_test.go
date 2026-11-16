package service

import (
	"encoding/json"
	"strings"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/store"

	_ "clawbench/internal/ai/backends/builtin"

	"database/sql"

	_ "modernc.org/sqlite"
)

// setupTestDBForBuiltinHistory creates an in-memory DB with the two tables this
// file's code path touches.
//
// It is a local fixture rather than a shared one because AddChatMessage updates
// chat_sessions.updated_at and creates a session row: a fixture missing those
// columns would fail for a reason unrelated to the behaviour under test.
func setupTestDBForBuiltinHistory(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	db.SetMaxOpenConns(1)

	stmts := []string{
		store.ProjectsDDL,
		`CREATE TABLE IF NOT EXISTS chat_sessions (
			id TEXT PRIMARY KEY,
			project_id INTEGER NOT NULL,
			backend TEXT NOT NULL,
			title TEXT NOT NULL,
			agent_id TEXT DEFAULT '',
			agent_source TEXT DEFAULT 'default',
			model TEXT DEFAULT '',
			external_session_id TEXT DEFAULT '',
			session_type TEXT NOT NULL DEFAULT 'chat',
			title_source TEXT NOT NULL DEFAULT '',
			context_state TEXT NOT NULL DEFAULT '{}',
			archived INTEGER NOT NULL DEFAULT 0,
			disabled INTEGER NOT NULL DEFAULT 0,
			removed INTEGER NOT NULL DEFAULT 0,
			pinned INTEGER NOT NULL DEFAULT 0,
			sort_order INTEGER NOT NULL DEFAULT 0,
			last_read_at DATETIME,
			awaiting_user_at DATETIME,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(backend, id)
		)`,
		`CREATE TABLE IF NOT EXISTS chat_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL,
			role TEXT NOT NULL,
			content TEXT NOT NULL,
			files TEXT,
			session_id TEXT,
			backend TEXT NOT NULL DEFAULT 'claude',
			agent_id TEXT DEFAULT '',
			injected INTEGER NOT NULL DEFAULT 0,
			streaming INTEGER NOT NULL DEFAULT 0,
			indexed INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			completed_at DATETIME
		)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("create fixture schema: %v", err)
		}
	}

	cleanup := store.SetDBForTest(db, db)
	t.Cleanup(func() {
		cleanup()
		_ = db.Close()
	})
	return db
}

// seedHistory inserts finalized messages into the fixture DB.
//
// It also creates the chat_sessions row, because the compaction state
// (context_state) lives on that row: a fixture with only chat_history rows would
// make every summary write silently match zero rows.
func seedHistory(t *testing.T, sessionID string, turns [][2]string) {
	t.Helper()
	if _, err := store.WriteExec(
		`INSERT OR IGNORE INTO chat_sessions (id, project_id, backend, title) VALUES (?, 0, 'builtin', '')`,
		sessionID,
	); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	for _, turn := range turns {
		content, err := json.Marshal(map[string]any{
			"blocks": []map[string]any{{"type": "text", "text": turn[1]}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := AddChatMessage("/tmp/p", "builtin", sessionID, turn[0], string(content), nil, false, ""); err != nil {
			t.Fatalf("seed message: %v", err)
		}
	}
}

// TestBuildBuiltinHistory_ReplaysTurnsInOrder pins the basic contract: the
// model's context is ClawBench's own transcript, in order.
func TestBuildBuiltinHistory_ReplaysTurnsInOrder(t *testing.T) {
	setupTestDBForBuiltinHistory(t)

	seedHistory(t, "s1", [][2]string{
		{"user", "what is this repo"},
		{"assistant", "a multi-end AI workbench"},
		{"user", "where is the entry point"},
		{"assistant", "cmd/server/main.go"},
	})

	got := buildBuiltinHistory("s1", 0, builtinSummary{})
	if len(got) != 4 {
		t.Fatalf("expected 4 turns, got %d", len(got))
	}
	if got[0].Role != roleUser || got[0].Text != "what is this repo" {
		t.Fatalf("first turn wrong: %+v", got[0])
	}
	if got[3].Role != roleAssistant || got[3].Text != "cmd/server/main.go" {
		t.Fatalf("last turn wrong: %+v", got[3])
	}
}

// TestBuildBuiltinHistory_ExcludesCurrentTurn pins the boundary that keeps the
// prompt from appearing twice.
//
// The user message is persisted BEFORE the request is built, so a history that
// included it would hand the model the same question twice (once as history,
// once as the prompt). The model then answers a doubled instruction, which
// reads to the user as the agent "repeating the question back".
func TestBuildBuiltinHistory_ExcludesCurrentTurn(t *testing.T) {
	setupTestDBForBuiltinHistory(t)

	seedHistory(t, "s5", [][2]string{
		{"user", "first question"},
		{"assistant", "first answer"},
		{"user", "current question"},
	})

	// Find the current message's id, as the handler would after persisting it.
	var currentID int64
	if err := store.ReadDB().QueryRow(
		"SELECT id FROM chat_history WHERE session_id = ? AND content LIKE ? ORDER BY id DESC LIMIT 1",
		"s5", "%current question%",
	).Scan(&currentID); err != nil {
		t.Fatalf("find current message: %v", err)
	}

	got := buildBuiltinHistory("s5", currentID, builtinSummary{})
	if len(got) != 2 {
		t.Fatalf("expected only the two prior turns, got %d: %+v", len(got), got)
	}
	for _, m := range got {
		if strings.Contains(m.Text, "current question") {
			t.Fatal("the current turn must not be replayed as history")
		}
	}

	// Without the boundary (a caller that forgot to pass it) the current
	// message IS included — the documented degradation, asserted so a future
	// reader knows it is intentional rather than a silent bug.
	all := buildBuiltinHistory("s5", 0, builtinSummary{})
	if len(all) != 3 {
		t.Fatalf("without a boundary the history includes the current turn, got %d", len(all))
	}
}

// TestBuildBuiltinHistory_EmptyForNewSession pins that a brand-new session
// replays nothing rather than a header.
func TestBuildBuiltinHistory_EmptyForNewSession(t *testing.T) {
	setupTestDBForBuiltinHistory(t)
	if got := buildBuiltinHistory("nope", 0, builtinSummary{}); got != nil {
		t.Fatalf("expected no history, got %+v", got)
	}
	if got := buildBuiltinHistory("", 0, builtinSummary{}); got != nil {
		t.Fatalf("an empty session id must yield nothing, got %+v", got)
	}
}

// TestBuildBuiltinHistory_AnnouncesTruncation pins that the model is told
// earlier turns exist but were dropped.
//
// Without the notice the model treats the visible window as the whole
// conversation and confidently answers "you never mentioned that" about a turn
// that exists.
func TestBuildBuiltinHistory_AnnouncesTruncation(t *testing.T) {
	setupTestDBForBuiltinHistory(t)

	// Each turn is ~1000 runes; 60 turns exceed the 40k budget.
	long := strings.Repeat("x", 1000)
	var turns [][2]string
	for i := 0; i < 60; i++ {
		role := roleUser
		if i%2 == 1 {
			role = roleAssistant
		}
		turns = append(turns, [2]string{role, long})
	}
	seedHistory(t, "s2", turns)

	got := buildBuiltinHistory("s2", 0, builtinSummary{})
	if len(got) == 0 {
		t.Fatal("expected a truncated window, got nothing")
	}
	if !strings.Contains(got[0].Text, "Earlier conversation omitted") {
		t.Fatalf("the first entry must announce the truncation, got %q", got[0].Text[:min(60, len(got[0].Text))])
	}
	// The most recent turns must survive: that is what the next reply depends on.
	if len(got) < 10 {
		t.Fatalf("the tail must be kept, got only %d entries", len(got))
	}
}

// TestBuildBuiltinHistory_StaysWithinBudget pins the actual bound, which is
// what keeps a long session from overflowing the model's window.
func TestBuildBuiltinHistory_StaysWithinBudget(t *testing.T) {
	setupTestDBForBuiltinHistory(t)

	long := strings.Repeat("y", 2000)
	var turns [][2]string
	for i := 0; i < 50; i++ {
		turns = append(turns, [2]string{roleUser, long})
	}
	seedHistory(t, "s3", turns)

	total := 0
	for _, m := range buildBuiltinHistory("s3", 0, builtinSummary{}) {
		total += len([]rune(m.Text))
	}
	// The budget plus the one-message overshoot allowance and the notice.
	if total > builtinHistoryBudgetChars+2000 {
		t.Fatalf("history must stay within the budget, got %d runes", total)
	}
}

// TestBuildBuiltinHistory_SkipsNonTextTurns pins that a turn with no renderable
// text does not produce an empty message (which some APIs reject).
func TestBuildBuiltinHistory_SkipsNonTextTurns(t *testing.T) {
	setupTestDBForBuiltinHistory(t)

	// A tool-only assistant turn: blocks with no text.
	content, _ := json.Marshal(map[string]any{
		"blocks": []map[string]any{{"type": "tool_use", "name": "read", "id": "c1"}},
	})
	if _, err := AddChatMessage("/tmp/p", "builtin", "s4", roleAssistant, string(content), nil, false, ""); err != nil {
		t.Fatal(err)
	}

	if got := buildBuiltinHistory("s4", 0, builtinSummary{}); len(got) != 0 {
		t.Fatalf("a tool-only turn must not be replayed as an empty message, got %+v", got)
	}
}

// TestBuildBuiltinHistory_SkipsMessagesTheSummaryCovers pins the boundary that
// makes compaction happen ONCE.
//
// Without it, every turn would replay the same history, compact it again, and
// store a new summary — a model call per turn that also degrades the facts each
// time (a summary of a summary).
func TestBuildBuiltinHistory_SkipsMessagesTheSummaryCovers(t *testing.T) {
	setupTestDBForBuiltinHistory(t)

	seedHistory(t, "s6", [][2]string{
		{"user", "old question one"},
		{"assistant", "old answer one"},
		{"user", "old question two"},
		{"assistant", "old answer two"},
		{"user", "new question"},
		{"assistant", "new answer"},
	})

	// The summary covers everything up to and including "old answer two".
	var boundary int64
	if err := store.ReadDB().QueryRow(
		"SELECT id FROM chat_history WHERE session_id = ? AND content LIKE ? ORDER BY id DESC LIMIT 1",
		"s6", "%old answer two%",
	).Scan(&boundary); err != nil {
		t.Fatalf("find boundary: %v", err)
	}

	got := buildBuiltinHistory("s6", 0, builtinSummary{Text: "sum", UpToMessageID: boundary})
	if len(got) != 2 {
		t.Fatalf("only the messages after the summary must be replayed, got %d: %+v", len(got), got)
	}
	for _, m := range got {
		if strings.Contains(m.Text, "old ") {
			t.Fatalf("a covered message must not be replayed, got %q", m.Text)
		}
	}
	if !strings.Contains(got[0].Text, "new question") {
		t.Fatalf("the newest turns must survive, got %+v", got)
	}
}

// TestBuildBuiltinHistory_ZeroBoundaryReplaysEverything pins the "never
// compacted" state: a summary with no boundary is not a summary.
func TestBuildBuiltinHistory_ZeroBoundaryReplaysEverything(t *testing.T) {
	setupTestDBForBuiltinHistory(t)
	seedHistory(t, "s7", [][2]string{
		{"user", "q1"}, {"assistant", "a1"},
	})

	got := buildBuiltinHistory("s7", 0, builtinSummary{})
	if len(got) != 2 {
		t.Fatalf("expected the full history, got %d", len(got))
	}
}

// TestLastCoveredMessageID pins the boundary computation.
func TestLastCoveredMessageID(t *testing.T) {
	setupTestDBForBuiltinHistory(t)
	seedHistory(t, "s8", [][2]string{
		{"user", "q1"}, {"assistant", "a1"}, {"user", "q2"},
	})

	var newest int64
	if err := store.ReadDB().QueryRow(
		"SELECT MAX(id) FROM chat_history WHERE session_id = ?", "s8",
	).Scan(&newest); err != nil {
		t.Fatal(err)
	}
	if got := lastCoveredMessageID("s8", 0); got != newest {
		t.Fatalf("expected the newest id %d, got %d", newest, got)
	}
	// A boundary excludes the message it points at (that message is this turn's
	// own user message, which is not part of the history).
	if got := lastCoveredMessageID("s8", newest); got >= newest {
		t.Fatalf("a before-id must exclude that message, got %d for before %d", got, newest)
	}
	if got := lastCoveredMessageID("unknown-session", 0); got != 0 {
		t.Fatalf("an unknown session must yield 0, got %d", got)
	}
}

// TestBuiltinSummaryRoundTrip pins that the summary and its boundary survive the
// storage layer together.
func TestBuiltinSummaryRoundTrip(t *testing.T) {
	setupTestDBForBuiltinHistory(t)

	// The row must exist for context_state to be patchable.
	seedHistory(t, "s9", [][2]string{{"user", "q"}})

	want := builtinSummary{Text: "the user wants X", UpToMessageID: 42}
	SetBuiltinSummary("s9", want)

	got := GetBuiltinSummary("s9")
	if got.Text != want.Text || got.UpToMessageID != want.UpToMessageID {
		t.Fatalf("round trip mismatch: got %+v, want %+v", got, want)
	}

	// An unknown session reads as "never compacted", not as a partial value.
	if s := GetBuiltinSummary("nope"); s.Text != "" || s.UpToMessageID != 0 {
		t.Fatalf("an unknown session must read as empty, got %+v", s)
	}
	// An empty summary must not be written (it would mark a boundary with
	// nothing behind it, silently dropping history).
	SetBuiltinSummary("s9", builtinSummary{Text: "  ", UpToMessageID: 99})
	if s := GetBuiltinSummary("s9"); s.UpToMessageID == 99 {
		t.Fatal("an empty summary must not be stored")
	}
}

// TestBuildChatRequest_BuiltinSkipsForkInjection pins the bug that made
// compaction loop forever.
//
// A self-hosted backend replays history as real messages. If the fork-context
// path also ran, the same history arrived TWICE — once as a rendered "previous
// conversation" block (the larger of the two), once as messages. Every turn then
// re-overflowed the window and compacted again, so the agent paid a summary call
// per turn and never got under the limit.
func TestBuildChatRequest_BuiltinSkipsForkInjection(t *testing.T) {
	setupTestDBForBuiltinHistory(t)

	seedHistory(t, "fork-sess", [][2]string{
		{"user", "first question"},
		{"assistant", "first answer"},
	})

	req := BuildChatRequest("next question", "fork-sess", "/tmp/p", builtinBackendID,
		builtinBackendID, "", "", "", "", "/tmp/p", false)

	if req.ForkContext != "" {
		t.Fatalf("the built-in backend must not receive fork context (it replays history instead), got %d chars",
			len(req.ForkContext))
	}
	if len(req.History) != 2 {
		t.Fatalf("the history must be replayed as messages, got %d", len(req.History))
	}
}

// TestBuildChatRequest_ExternalBackendKeepsForkInjection pins the other side:
// every external CLI still needs the rendered block, because it has no way to
// read our rows.
func TestBuildChatRequest_ExternalBackendKeepsForkInjection(t *testing.T) {
	setupTestDBForBuiltinHistory(t)

	seedHistory(t, "fork-ext", [][2]string{
		{"user", "first question"},
		{"assistant", "first answer"},
	})

	req := BuildChatRequest("next question", "fork-ext", "/tmp/p", "claude",
		"claude", "", "", "", "", "/tmp/p", false)

	if len(req.History) != 0 {
		t.Fatalf("an external backend must not receive replayed history, got %d", len(req.History))
	}
}

// TestBuiltinHistoryEnabled pins that only the built-in backend replays
// history. Every external backend keeps its own transcript and resumes it by
// id; replaying ours would duplicate the conversation.
func TestBuiltinHistoryEnabled(t *testing.T) {
	if !builtinHistoryEnabled("builtin") {
		t.Fatal("the built-in backend must replay history")
	}
	for _, other := range []string{"claude", "codebuddy", "pi", "opencode", ""} {
		if builtinHistoryEnabled(other) {
			t.Fatalf("%q must not replay history", other)
		}
	}
}

// TestBuiltinBackendIDMatchesSpec pins the duplicated constant against the
// registered backend, so a rename cannot leave history silently disabled.
func TestBuiltinBackendIDMatchesSpec(t *testing.T) {
	if model.FindSpecByBackend(builtinBackendID) == nil {
		t.Fatalf("no backend is registered as %q; the constant has drifted from the plugin id",
			builtinBackendID)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
