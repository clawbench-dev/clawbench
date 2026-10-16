package service

import (
	"database/sql"
	"encoding/json"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTestDBForAskCardMigration builds an in-memory DB with just the summaries
// table the migration touches.
func setupTestDBForAskCardMigration(t *testing.T) func() {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	_, _ = db.Exec(`
		CREATE TABLE IF NOT EXISTS summaries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			target_type TEXT NOT NULL,
			target_id   INTEGER NOT NULL,
			summary     TEXT NOT NULL,
			summary_cards TEXT NOT NULL DEFAULT '',
			created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(target_type, target_id)
		);
	`)
	cleanup := store.SetDBForTest(db, db)
	return func() { cleanup(); _ = db.Close() }
}

// insertCards persists a raw summary_cards blob for a chat_message target.
func insertCards(t *testing.T, targetID int64, cardsJSON string) {
	t.Helper()
	_, err := store.UnsafeDBForTest().Exec(
		"INSERT INTO summaries (target_type, target_id, summary, summary_cards) VALUES ('chat_message', ?, 'text', ?)",
		targetID, cardsJSON,
	)
	require.NoError(t, err)
}

func readCards(t *testing.T, targetID int64) model.SummaryCards {
	t.Helper()
	var raw string
	require.NoError(t, store.ReadDB().QueryRow(
		"SELECT summary_cards FROM summaries WHERE target_type='chat_message' AND target_id = ?", targetID,
	).Scan(&raw))
	var cards model.SummaryCards
	require.NoError(t, json.Unmarshal([]byte(raw), &cards))
	return cards
}

func TestMigrateAskSummaryCardsDone_ForcesDoneOnLegacyRow(t *testing.T) {
	teardown := setupTestDBForAskCardMigration(t)
	defer teardown()

	// The exact legacy shape: SummaryTool.Done is omitempty, so a false value
	// left no key at all.
	insertCards(t, 1, `{"tools":[{"name":"AskUserQuestion","id":"call_1","input":{"questions":[]}}]}`)

	require.True(t, MigrateAskSummaryCardsDone())

	cards := readCards(t, 1)
	require.Len(t, cards.Tools, 1)
	assert.True(t, cards.Tools[0].Done, "legacy ask card must be normalized to done=true")
	// The rest of the card must survive untouched.
	assert.Equal(t, "call_1", cards.Tools[0].ID)
	assert.Equal(t, "AskUserQuestion", cards.Tools[0].Name)
}

// The wire shape is what the frontend reads: omitempty drops the key when the
// flag is false, so the assertion must be on the marshaled form, not the struct.
func TestMigrateAskSummaryCardsDone_EmitsDoneKey(t *testing.T) {
	teardown := setupTestDBForAskCardMigration(t)
	defer teardown()

	insertCards(t, 1, `{"tools":[{"name":"AskUserQuestion","id":"call_1"}]}`)
	require.True(t, MigrateAskSummaryCardsDone())

	var raw string
	require.NoError(t, store.ReadDB().QueryRow(
		"SELECT summary_cards FROM summaries WHERE target_type='chat_message' AND target_id = 1",
	).Scan(&raw))
	assert.Contains(t, raw, `"done":true`, "the persisted blob must carry done:true, not omit it")
}

// Every other field of the row (and of the cards payload) must round-trip: the
// migration is a flag normalization, not a rewrite.
func TestMigrateAskSummaryCardsDone_PreservesOtherFields(t *testing.T) {
	teardown := setupTestDBForAskCardMigration(t)
	defer teardown()

	insertCards(t, 1, `{"tools":[
		{"name":"AskUserQuestion","id":"a1","input":{"questions":[{"question":"q?","options":[{"label":"L","description":"D"}]}]}},
		{"name":"AskUserQuestion","id":"a2","done":true}
	],"taskIDs":[7],"createdFiles":[{"path":"/p/a.go","toolIDs":["w1"]}],
	"warnings":[{"type":"warning","text":"t","reason":"restart"}],
	"askQuestions":[{"header":"H","question":"q","options":[{"label":"L"}]}]}`)

	require.True(t, MigrateAskSummaryCardsDone())

	cards := readCards(t, 1)
	require.Len(t, cards.Tools, 2)
	assert.True(t, cards.Tools[0].Done)
	assert.True(t, cards.Tools[1].Done)
	assert.Equal(t, "a1", cards.Tools[0].ID)
	assert.Equal(t, "q?", cards.Tools[0].Input["questions"].([]any)[0].(map[string]any)["question"])
	assert.Equal(t, []int64{7}, cards.TaskIDs)
	require.Len(t, cards.CreatedFiles, 1)
	assert.Equal(t, "/p/a.go", cards.CreatedFiles[0].Path)
	require.Len(t, cards.Warnings, 1)
	assert.Equal(t, "restart", cards.Warnings[0].Reason)
	require.Len(t, cards.AskQuestions, 1)
	assert.Equal(t, "H", cards.AskQuestions[0].Header)
}

// A non-ask tool entry must not be touched, and a row already carrying
// done:true must not be rewritten at all.
func TestMigrateAskSummaryCardsDone_LeavesOtherToolsAndDoneRowsAlone(t *testing.T) {
	teardown := setupTestDBForAskCardMigration(t)
	defer teardown()

	insertCards(t, 1, `{"tools":[{"name":"Bash","id":"b1"}]}`)
	insertCards(t, 2, `{"tools":[{"name":"AskUserQuestion","id":"a1","done":true}]}`)
	insertCards(t, 3, `{"taskIDs":[1]}`)
	insertCards(t, 4, `not json at all`)

	require.True(t, MigrateAskSummaryCardsDone())

	// Row 1: Bash entry untouched (still no done key).
	var raw1 string
	require.NoError(t, store.ReadDB().QueryRow(
		"SELECT summary_cards FROM summaries WHERE target_id = 1").Scan(&raw1))
	assert.Equal(t, `{"tools":[{"name":"Bash","id":"b1"}]}`, raw1)

	// Row 4: undecodable blob left exactly as found, not replaced.
	var raw4 string
	require.NoError(t, store.ReadDB().QueryRow(
		"SELECT summary_cards FROM summaries WHERE target_id = 4").Scan(&raw4))
	assert.Equal(t, "not json at all", raw4)

	assert.True(t, readCards(t, 2).Tools[0].Done)
}

// The migration is idempotent: a second run finds nothing left to change.
func TestMigrateAskSummaryCardsDone_Idempotent(t *testing.T) {
	teardown := setupTestDBForAskCardMigration(t)
	defer teardown()

	insertCards(t, 1, `{"tools":[{"name":"AskUserQuestion","id":"a1"}]}`)
	require.True(t, MigrateAskSummaryCardsDone())
	first := readCards(t, 1)

	require.True(t, MigrateAskSummaryCardsDone())
	assert.Equal(t, first, readCards(t, 1))
}

// A mixed payload where only some ask entries lack the flag: exactly those get
// normalized, and the count is not inflated.
func TestMigrateAskSummaryCardsDone_MixedEntries(t *testing.T) {
	teardown := setupTestDBForAskCardMigration(t)
	defer teardown()

	insertCards(t, 1, `{"tools":[
		{"name":"AskUserQuestion","id":"a1"},
		{"name":"AskUserQuestion","id":"a2","done":true},
		{"name":"Bash","id":"b1"}
	]}`)

	require.True(t, MigrateAskSummaryCardsDone())

	cards := readCards(t, 1)
	require.Len(t, cards.Tools, 3)
	assert.True(t, cards.Tools[0].Done)
	assert.True(t, cards.Tools[1].Done)
	assert.Equal(t, "b1", cards.Tools[2].ID)
	assert.False(t, cards.Tools[2].Done)
}

// The write-path fix and the migration must agree: a card produced by
// extractSummaryCards is already normalized, so the migration is a no-op on it.
func TestMigrateAskSummaryCardsDone_AgreesWithWriter(t *testing.T) {
	teardown := setupTestDBForAskCardMigration(t)
	defer teardown()

	// A block that never received a tool_result — the production shape.
	cards := extractSummaryCards([]model.ContentBlock{{
		Type: "tool_use", Name: "AskUserQuestion", ID: "ask-1", Done: false,
		Input: map[string]any{"questions": []any{map[string]any{"question": "go?"}}},
	}})
	raw, err := json.Marshal(cards)
	require.NoError(t, err)
	insertCards(t, 1, string(raw))
	before := readCards(t, 1)

	require.True(t, MigrateAskSummaryCardsDone())
	assert.Equal(t, before, readCards(t, 1), "writer output needs no migration")
}
