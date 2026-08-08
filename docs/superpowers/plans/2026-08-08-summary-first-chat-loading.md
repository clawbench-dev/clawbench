# Chat Summary-First Loading Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Opening a chat loads only reading summaries (not full content) by default, with card metadata (tools/scheduled-tasks/ask-question) stored backend-side, so memory/network stays bounded and the frontend renders exactly what the backend persists.

**Architecture:** The backend already persists summaries in `summaries.summary`. We add a `summary_cards` JSON column to hold card metadata (tool_use, scheduled-task IDs, ask-question blocks), computed in `AsyncSummarize`. `GetChatHistoryPaged` gains a `view=summary` mode that omits `content` for summarized, non-streaming messages. The frontend's `loadHistory` always passes `?view=summary`; toggling to original lazily fetches one message via the existing `/api/rag/message?id=` and caches it in the message object. ContentBlocks summary mode reads `msg.summary` + `msg.summaryCards` instead of traversing blocks.

**Tech Stack:** Go (net/http, database/sql, sqlite3), Vue 3 (Composition API), Vitest, Go test.

---

## File Map

**Backend (Go):**
- `internal/model/chat.go` — add `SummaryCards` type; add `SummaryCards *SummaryCards` field to `ChatMessage`.
- `internal/service/database.go` — migration `ALTER TABLE summaries ADD COLUMN summary_cards TEXT`; extend `GetSummary`/`SaveSummary` (or add new funcs) to read/write cards.
- `internal/service/summary.go` — extract card metadata from blocks in `AsyncSummarize`; persist with summary.
- `internal/service/chat.go` — add `view` param to `GetChatHistoryPaged`; filter content in `enrichMessagesWithSummaries`/`scanMessages`.
- `internal/handler/chat.go` — parse `view` query param, pass to `GetChatHistoryPaged`.
- `internal/ws/protocol.go` — extend `SummaryUpdateData` with `SummaryCards`.
- `internal/summarize/task.go` — (if needed) add pure card-extraction helper + tests.

**Frontend (Vue):**
- `web/src/composables/useChatSession.ts` — add `&view=summary` to loadHistory URL.
- `web/src/utils/chatSessionUtils.ts` — parse `summaryCards`; apply in `parseMessages`/`applySummaryUpdate`.
- `web/src/components/chat/ChatPanelContent.vue` — `handleToggleSummary` lazy-fetch via `/api/rag/message?id=`; `handleSummaryUpdate` apply cards.
- `web/src/components/chat/ContentBlocks.vue` — summary mode renders `msg.summary` + `msg.summaryCards`; remove block-traversal summary branch.
- `web/src/components/chat/ChatMessageItem.vue` — pass `summaryCards` prop; wire lazy-load state.
- `web/src/composables/useChatStream.ts` — `summary_update` handler applies cards.
- Tests in `web/src/utils/__tests__/` and `web/src/components/chat/__tests__/`.

---

### Task 1: Add `SummaryCards` model type and `ChatMessage` field

**Files:**
- Modify: `internal/model/chat.go`
- Test: `internal/model/chat_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/model/chat_test.go`:

```go
package model

import (
	"encoding/json"
	"testing"
)

func TestSummaryCardsRoundTrip(t *testing.T) {
	cards := SummaryCards{
		Tools: []SummaryTool{{
			Name:  "Bash",
			ID:    "tool-1",
			Input: map[string]any{"command": "ls"},
		}},
		TaskIDs: []int64{42},
		AskQuestions: []AskQuestionCard{{
			Text:    "Continue?",
			Options: []string{"Yes", "No"},
		}},
	}
	raw, err := json.Marshal(cards)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back SummaryCards
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(back.Tools) != 1 || back.Tools[0].Name != "Bash" {
		t.Fatalf("tools mismatch: %+v", back.Tools)
	}
	if len(back.TaskIDs) != 1 || back.TaskIDs[0] != 42 {
		t.Fatalf("taskIDs mismatch: %+v", back.TaskIDs)
	}
	if len(back.AskQuestions) != 1 || back.AskQuestions[0].Text != "Continue?" {
		t.Fatalf("askQuestions mismatch: %+v", back.AskQuestions)
	}
}

func TestChatMessageSummaryCardsMarshal(t *testing.T) {
	m := ChatMessage{ID: 1, Role: "assistant", Summary: strPtr("hi"), SummaryCards: &SummaryCards{TaskIDs: []int64{1}}}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if obj["summary"] != "hi" {
		t.Fatalf("summary missing: %v", obj["summary"])
	}
	if obj["summaryCards"] == nil {
		t.Fatalf("summaryCards missing: %v", obj["summaryCards"])
	}
}

func strPtr(s string) *string { return &s }
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/model/ -run TestSummaryCardsRoundTrip -v`
Expected: FAIL — undefined: SummaryCards

- [ ] **Step 3: Implement the types**

Add to `internal/model/chat.go`:

```go
// SummaryTool is a compact record of a tool_use block present in a reading
// summary view. input is included for interactive tools (AskUserQuestion,
// PermissionApproval) that need it for card rendering.
type SummaryTool struct {
	Name  string         `json:"name"`
	ID    string         `json:"id,omitempty"`
	Input map[string]any `json:"input,omitempty"`
}

// AskQuestionCard is a compact representation of an <ask-question> block
// detected in a text block, used to render the question card in summary view.
type AskQuestionCard struct {
	Text    string   `json:"text"`
	Options []string `json:"options"`
}

// SummaryCards holds the structured card metadata persisted alongside the
// reading summary text. Tools are auto-expand tool_use blocks; TaskIDs are the
// scheduled-task IDs referenced by <scheduled-task> tags; AskQuestions are
// <ask-question> XML cards. Populated at summarization time and stored in the
// summaries.summary_cards column.
type SummaryCards struct {
	Tools        []SummaryTool     `json:"tools,omitempty"`
	TaskIDs      []int64           `json:"taskIDs,omitempty"`
	AskQuestions []AskQuestionCard `json:"askQuestions,omitempty"`
}
```

Add field to `ChatMessage`:

```go
	Summary     *string         `json:"summary,omitempty"`      // reading summary (nil=not summarized, ""=too short, non-empty=summary)
	SummaryCards *SummaryCards  `json:"summaryCards,omitempty"` // structured card metadata for summary view
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/model/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/model/chat.go internal/model/chat_test.go
git commit -m "feat(model): add SummaryCards type and ChatMessage field"
```

---

### Task 2: Add `summary_cards` column migration + read/write helpers

**Files:**
- Modify: `internal/service/database.go`
- Test: `internal/service/database_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/service/database_test.go`:

```go
func TestSaveGetSummaryWithCards(t *testing.T) {
	cards := &model.SummaryCards{TaskIDs: []int64{7, 8}}
	if err := SaveSummaryWithCards("chat_message", 9001, "summary text", cards); err != nil {
		t.Fatalf("SaveSummaryWithCards: %v", err)
	}
	gotSummary, gotCards, found := GetSummaryWithCards("chat_message", 9001)
	if !found {
		t.Fatalf("expected found")
	}
	if gotSummary != "summary text" {
		t.Fatalf("summary mismatch: %q", gotSummary)
	}
	if gotCards == nil || len(gotCards.TaskIDs) != 2 {
		t.Fatalf("cards mismatch: %+v", gotCards)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/service/ -run TestSaveGetSummaryWithCards -v`
Expected: FAIL — undefined: SaveSummaryWithCards / GetSummaryWithCards / no summary_cards column

- [ ] **Step 3: Implement migration**

In the schema-migration section of `database.go` (find the block containing other `ALTER TABLE` migrations, near `ADD COLUMN summary` for task_executions around line 496), add:

```go
	// Migrate: add summary_cards column for structured summary card metadata
	_ = db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('summaries') WHERE name='summary_cards'").Scan(&hasSummaryCards)
	if hasSummaryCards == 0 {
		if _, err := WriteExec("ALTER TABLE summaries ADD COLUMN summary_cards TEXT NOT NULL DEFAULT ''"); err != nil {
			return fmt.Errorf("failed to add summary_cards column: %w", err)
		}
	}
```

Declare `var hasSummaryCards int` alongside the other migration vars. Also ensure the fresh-install `CREATE TABLE IF NOT EXISTS summaries` (line 343) gains `summary_cards TEXT NOT NULL DEFAULT ''`:

```go
		CREATE TABLE IF NOT EXISTS summaries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			target_type TEXT NOT NULL,
			target_id   INTEGER NOT NULL,
			summary     TEXT NOT NULL,
			summary_cards TEXT NOT NULL DEFAULT '',
			created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(target_type, target_id)
		);
```

- [ ] **Step 4: Implement read/write helpers**

Replace `GetSummary` and `SaveSummary` (database.go:1261-1283) to also handle cards, keeping the existing signatures delegating for compatibility:

```go
// GetSummary looks up a reading summary by target type and target ID.
// Returns (summary, found). Empty summary = text was too short.
func GetSummary(targetType string, targetID int64) (string, bool) {
	s, _, ok := GetSummaryWithCards(targetType, targetID)
	return s, ok
}

// SaveSummary persists a reading summary for a target (chat message or task execution).
// summary = "" means text was too short; non-empty is the actual summary.
func SaveSummary(targetType string, targetID int64, summary string) error {
	return SaveSummaryWithCards(targetType, targetID, summary, nil)
}

// GetSummaryWithCards returns summary text and card metadata.
// Returns (summary, cards, found). cards is nil when no cards persisted.
func GetSummaryWithCards(targetType string, targetID int64) (string, *model.SummaryCards, bool) {
	var summary string
	var cardsJSON string
	err := dbRead.QueryRow(
		"SELECT summary, COALESCE(summary_cards, '') FROM summaries WHERE target_type = ? AND target_id = ?",
		targetType, targetID,
	).Scan(&summary, &cardsJSON)
	if err != nil {
		return "", nil, false
	}
	var cards *model.SummaryCards
	if cardsJSON != "" {
		cards = &model.SummaryCards{}
		if jerr := json.Unmarshal([]byte(cardsJSON), cards); jerr != nil {
			// Ignore malformed cards — treat as empty rather than failing the read.
			cards = nil
		}
	}
	return summary, cards, true
}

// SaveSummaryWithCards persists summary text and card metadata.
func SaveSummaryWithCards(targetType string, targetID int64, summary string, cards *model.SummaryCards) error {
	cardsJSON := ""
	if cards != nil {
		raw, err := json.Marshal(cards)
		if err != nil {
			return err
		}
		cardsJSON = string(raw)
	}
	_, err := WriteExec(
		"INSERT OR REPLACE INTO summaries (target_type, target_id, summary, summary_cards, created_at) VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)",
		targetType, targetID, summary, cardsJSON,
	)
	return err
}
```

Ensure `encoding/json` is imported in database.go.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/service/ -run TestSaveGetSummaryWithCards -v`
Expected: PASS

- [ ] **Step 6: Run full service tests to confirm no regression**

Run: `go test ./internal/service/`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/service/database.go internal/service/database_test.go
git commit -m "feat(service): add summary_cards column and read/write helpers"
```

---

### Task 3: Extract card metadata from blocks at summarization time

**Files:**
- Modify: `internal/service/summary.go`
- Create: `internal/service/summary_cards.go`, `internal/service/summary_cards_test.go`
- Modify: `internal/service/session_runtime.go` (simple path)
- Modify: `internal/ws/protocol.go`

- [ ] **Step 1: Write the failing test for the extraction helper**

Create `internal/service/summary_cards_test.go`:

```go
package service

import (
	"testing"

	"clawbench/internal/model"
)

func TestExtractSummaryCards(t *testing.T) {
	blocks := []model.ContentBlock{
		{Type: "thinking", Text: "reasoning"},
		{Type: "tool_use", Name: "Bash", ID: "t1", Input: map[string]any{"command": "ls"}},
		{Type: "tool_use", Name: "AskUserQuestion", ID: "t2", Input: map[string]any{"question": "go?"}},
		{Type: "text", Text: "Answer <scheduled-task id=\"42\">x</scheduled-task> <ask-question>continue?</ask-question>"},
	}
	cards := extractSummaryCards(blocks)
	if len(cards.Tools) != 2 {
		t.Fatalf("expected 2 tools, got %d: %+v", len(cards.Tools), cards.Tools)
	}
	if len(cards.TaskIDs) != 1 || cards.TaskIDs[0] != 42 {
		t.Fatalf("taskIDs mismatch: %+v", cards.TaskIDs)
	}
	if len(cards.AskQuestions) != 1 {
		t.Fatalf("expected 1 ask-question, got %d: %+v", len(cards.AskQuestions), cards.AskQuestions)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/service/ -run TestExtractSummaryCards -v`
Expected: FAIL — undefined: extractSummaryCards

- [ ] **Step 3: Implement extraction helper**

Create `internal/service/summary_cards.go`:

```go
package service

import (
	"regexp"
	"strings"

	"clawbench/internal/model"
)

var (
	scheduledTaskIDRe = regexp.MustCompile(`<scheduled-task\s+id="(\d+)"`)
	askQuestionRe     = regexp.MustCompile(`(?s)<ask-question>(.*?)</ask-question>`)
	askOptionRe       = regexp.MustCompile(`(?s)<option>\s*(?:<label>)?(.*?)(?:</label>)?\s*</option>`)
)

// isAutoExpandTool reports whether a tool_use block should be shown as a card
// in summary view. Mirrors the frontend shouldAutoExpandTool set.
func isAutoExpandTool(name string) bool {
	n := strings.ToLower(name)
	return n == "askuserquestion" || n == "permissionapproval"
}

// extractSummaryCards walks content blocks and builds the compact card
// metadata persisted in summaries.summary_cards. Only tool_use blocks that
// auto-expand, scheduled-task IDs, and <ask-question> cards are retained.
func extractSummaryCards(blocks []model.ContentBlock) *model.SummaryCards {
	cards := &model.SummaryCards{}
	for _, b := range blocks {
		switch b.Type {
		case "tool_use":
			if isAutoExpandTool(b.Name) {
				cards.Tools = append(cards.Tools, model.SummaryTool{
					Name:  b.Name,
					ID:    b.ID,
					Input: b.Input,
				})
			}
		case "text":
			for _, m := range scheduledTaskIDRe.FindAllStringSubmatch(b.Text, -1) {
				var id int64
				// ids are positive digits; parse manually to avoid strconv import noise
				for _, c := range m[1] {
					id = id*10 + int64(c-'0')
				}
				cards.TaskIDs = append(cards.TaskIDs, id)
			}
			for _, m := range askQuestionRe.FindAllStringSubmatch(b.Text, -1) {
				inner := m[1]
				card := model.AskQuestionCard{Text: stripXMLTags(inner)}
				for _, om := range askOptionRe.FindAllStringSubmatch(inner, -1) {
					card.Options = append(card.Options, strings.TrimSpace(stripXMLTags(om[1])))
				}
				cards.AskQuestions = append(cards.AskQuestions, card)
			}
		}
	}
	return cards
}

func stripXMLTags(s string) string {
	var b strings.Builder
	depth := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '<' {
			depth++
			continue
		}
		if s[i] == '>' {
			if depth > 0 {
				depth--
			}
			continue
		}
		if depth == 0 {
			b.WriteByte(s[i])
		}
	}
	return strings.TrimSpace(b.String())
}
```

Note: This helper must mirror the frontend detection rules. See `web/src/utils/renderToolDetail.ts:1523` (auto-expand set) and `web/src/utils/streamPerf.ts` (`detectAskQuestion`, `extractScheduledTaskIds`) for the canonical definitions. If the frontend tag formats differ from the regexes above, align them here.

- [ ] **Step 4: Update AsyncSummarize to persist cards**

In `internal/service/summary.go`, `AsyncSummarize`, after computing `summary`, compute cards and persist with them. Replace the `SaveSummary` calls:

```go
		cards := extractSummaryCards(blocks)
		if err := SaveSummaryWithCards(targetType, targetID, summary, cards); err != nil {
			slog.Warn(
				"failed to save summary",
				slog.String("target_type", targetType),
				slog.Int64("target_id", targetID),
				slog.String("err", err.Error()),
			)
		}
```

Also, in the short-text branch (line 47), persist cards too (text too short still may have cards):

```go
		if utf8.RuneCountInString(text) < summarize.ShortTextThreshold {
			cards := extractSummaryCards(blocks)
			if err := SaveSummaryWithCards(targetType, targetID, "", cards); err != nil {
				slog.Warn("failed to save summary (short text)", ...)
			}
			return
		}
```

- [ ] **Step 5: Update the summary_update WS event to carry cards**

In `internal/service/summary.go` broadcast block, extend the payload:

```go
			mgr.BroadcastEvent(ws.ServerMessage{
				Type:  ws.MessageTypeEvent,
				ID:    ws.GenerateEventID(),
				Event: "summary_update",
				Data: ws.SummaryUpdateData{
					TargetType:   targetType,
					TargetID:     targetID,
					Summary:      summary,
					SummaryCards: cards,
					ProjectPath:  projectPath,
					SessionID:    sessionID,
				},
			})
```

Update `internal/ws/protocol.go`:

```go
type SummaryUpdateData struct {
	TargetType   string                `json:"targetType"`
	TargetID     int64                 `json:"targetID"`
	Summary      string                `json:"summary"`
	SummaryCards *model.SummaryCards   `json:"summaryCards,omitempty"`
	ProjectPath  string                `json:"projectPath,omitempty"`
	SessionID    string                `json:"sessionID,omitempty"`
}
```

Add `import "clawbench/internal/model"` to protocol.go.

- [ ] **Step 6: Update simple-summary path**

In `internal/service/session_runtime.go` `summarizeChatSimple` (line 526), persist cards too:

```go
func summarizeChatSimple(msg *model.ChatMessage, blocks []model.ContentBlock, projectPath, sessionID string) {
	text := summarize.ExtractLastAnswerFromBlocks(blocks)
	if err := SaveSummaryWithCards("chat_message", msg.ID, text, extractSummaryCards(blocks)); err != nil {
		slog.Warn("failed to save simple summary", ...)
	}
}
```

(It already returns early on empty text; keep that. Persist cards regardless of text emptiness when text is non-empty.)

- [ ] **Step 7: Run all related tests**

Run: `go test ./internal/service/ ./internal/ws/`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add internal/service/summary.go internal/service/summary_cards.go internal/service/summary_cards_test.go internal/service/session_runtime.go internal/ws/protocol.go
git commit -m "feat(service): extract and persist summary card metadata"
```

---

### Task 4: `view=summary` filtering in history load

**Files:**
- Modify: `internal/service/chat.go`
- Modify: `internal/handler/chat.go`
- Test: `internal/service/chat_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/service/chat_test.go`:

```go
func TestGetChatHistoryPagedViewSummaryOmitsContent(t *testing.T) {
	// Setup: insert a session with an assistant message that has a summary.
	// (Use the project's test helpers for DB setup — see existing chat_test.go
	// for how sessions/messages are created.)
	// sessionID, projectPath := ...
	// Insert assistant message with content blocks JSON.
	// SaveSummaryWithCards("chat_message", msgID, "sum text", &model.SummaryCards{TaskIDs: []int64{1}})

	// summaryView := true
	// msgs, _, err := GetChatHistoryPaged(projectPath, backend, sessionID, 0, 0, summaryView)
	// if err != nil { t.Fatal(err) }
	// find the assistant msg; assert msgs[i].Content == "" and msgs[i].Summary != nil
	// assert msgs[i].SummaryCards != nil

	// fullView := false
	// msgs2, _, _ := GetChatHistoryPaged(projectPath, backend, sessionID, 0, 0, fullView)
	// assert msgs2[i].Content != ""
}
```

Use the existing DB test helpers in `chat_test.go` to create a session + message. If no helper exists, reuse the pattern from other `*_test.go` in `internal/service`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/service/ -run TestGetChatHistoryPagedViewSummaryOmitsContent -v`
Expected: FAIL — cannot use 6 args with GetChatHistoryPaged (signature mismatch)

- [ ] **Step 3: Change GetChatHistoryPaged signature and logic**

In `internal/service/chat.go`, add a `summaryView bool` param:

```go
func GetChatHistoryPaged(projectPath, backend, sessionID string, limit int, beforeID int, summaryView bool) ([]model.ChatMessage, int, error) {
```

Update the three `return msgs, totalCount, err` / `return messages, totalCount, err` sites to pass `summaryView` into a new scan variant. Replace `scanMessages(rows, sessionID)` with `scanMessagesView(rows, sessionID, summaryView)`.

Add `scanMessagesView` and update `scanMessages`:

```go
func scanMessages(rows *sql.Rows, sessionID string) ([]model.ChatMessage, error) {
	return scanMessagesView(rows, sessionID, false)
}

// scanMessagesView scans rows and, when summaryView is true, strips the heavy
// content from messages that have a reading summary and are not streaming.
func scanMessagesView(rows *sql.Rows, sessionID string, summaryView bool) ([]model.ChatMessage, error) {
	messages := []model.ChatMessage{}
	for rows.Next() {
		var msg model.ChatMessage
		var filesJSON sql.NullString
		var streaming int
		var indexed int
		if err := rows.Scan(&msg.ID, &msg.Role, &msg.Content, &filesJSON, &msg.Backend, &streaming, &msg.CreatedAt, &indexed); err != nil {
			return nil, err
		}
		msg.Streaming = streaming != 0
		msg.Indexed = indexed != 0
		if filesJSON.Valid && filesJSON.String != "" {
			msg.Files = unmarshalFilesJSON(filesJSON.String)
		}
		msg.SessionID = sessionID
		messages = append(messages, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	enrichMessagesWithSummaries(messages, summaryView)
	return messages, nil
}
```

- [ ] **Step 4: Update enrichMessagesWithSummaries to filter content**

In `internal/service/chat.go`, change the signature and the summary query to also select `summary_cards`, and strip content when `summaryView` is true:

```go
func enrichMessagesWithSummaries(messages []model.ChatMessage, summaryView bool) {
	assistantIDs := make([]int64, 0, len(messages))
	for _, msg := range messages {
		if msg.Role == "assistant" {
			assistantIDs = append(assistantIDs, msg.ID)
		}
	}
	if len(assistantIDs) == 0 {
		return
	}

	query := "SELECT target_id, summary, COALESCE(summary_cards, '') FROM summaries WHERE target_type = 'chat_message' AND target_id IN ("
	args := make([]any, len(assistantIDs))
	for i, id := range assistantIDs {
		if i > 0 {
			query += ","
		}
		query += "?"
		args[i] = id
	}
	query += ")"

	rows, err := dbRead.Query(query, args...)
	if err != nil {
		return
	}
	defer rows.Close()

	summaryMap := make(map[int64]string)
	cardMap := make(map[int64]*model.SummaryCards)
	for rows.Next() {
		var targetID int64
		var summary string
		var cardsJSON string
		if err := rows.Scan(&targetID, &summary, &cardsJSON); err != nil {
			continue
		}
		summaryMap[targetID] = summary
		if cardsJSON != "" {
			var cards model.SummaryCards
			if jerr := json.Unmarshal([]byte(cardsJSON), &cards); jerr == nil {
				cardMap[targetID] = &cards
			}
		}
	}

	for i := range messages {
		if messages[i].Role == "assistant" {
			if summary, ok := summaryMap[messages[i].ID]; ok {
				messages[i].Summary = &summary
			}
			if cards, ok := cardMap[messages[i].ID]; ok {
				messages[i].SummaryCards = cards
			}
			if summaryView && messages[i].Summary != nil && !messages[i].Streaming {
				// Omit heavy content for summarized, non-streaming messages.
				messages[i].Content = ""
			}
		}
	}
}
```

Ensure `encoding/json` and `clawbench/internal/model` are imported in chat.go.

Update all other callers of `enrichMessagesWithSummaries` (only `scanMessages` had it — now via `scanMessagesView`). Grep to confirm no other callers exist.

- [ ] **Step 5: Update GetChatHistory callers**

`GetChatHistory` (chat.go:22) calls `GetChatHistoryPaged(..., 0, 0)`. Update to pass `false`:

```go
func GetChatHistory(projectPath, backend, sessionID string) ([]model.ChatMessage, error) {
	msgs, _, err := GetChatHistoryPaged(projectPath, backend, sessionID, 0, 0, false)
	return msgs, err
}
```

- [ ] **Step 6: Update the handler to parse `view`**

In `internal/handler/chat.go`, after the `before_id`/`before` parsing (line 149), parse the view param:

```go
		summaryView := r.URL.Query().Get("view") == "summary"
```

Then update the call (line 161):

```go
		messages, totalCount, err := service.GetChatHistoryPaged(projectPath, sessionBackend, sessionID, limit, beforeID, summaryView)
```

- [ ] **Step 7: Run test to verify it passes**

Run: `go test ./internal/service/ -run TestGetChatHistoryPagedViewSummaryOmitsContent -v`
Expected: PASS

- [ ] **Step 8: Run full service + handler tests**

Run: `go test ./internal/service/ ./internal/handler/`
Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add internal/service/chat.go internal/handler/chat.go internal/service/chat_test.go
git commit -m "feat: view=summary omits content for summarized messages"
```

---

### Task 5: Frontend loadHistory passes `view=summary`

**Files:**
- Modify: `web/src/composables/useChatSession.ts`
- Test: `web/src/composables/__tests__/useChatSession.test.ts`

- [ ] **Step 1: Write the failing test**

Check the existing `useChatSession.test.ts` for how the URL/fetch is asserted. Add a test asserting the fetch URL contains `view=summary`. If no fetch-mocking helper exists, follow the existing test patterns in that file (e.g. mock global fetch and assert on `fetch.mock.calls[0][0]`).

- [ ] **Step 2: Run test to verify it fails**

Run: `npx vitest run web/src/composables/__tests__/useChatSession.test.ts`
Expected: FAIL — URL does not contain view=summary

- [ ] **Step 3: Update the URL**

In `web/src/composables/useChatSession.ts` line 445:

```ts
      const url = `/api/ai/chat?session_id=${encodeURIComponent(currentSessionId.value)}&limit=${limit}&view=summary`
```

Also update the recovery-path URL (line 400):

```ts
          recoverResp = await fetch(`/api/ai/chat?limit=${limit}&view=summary`, { signal: recoverCtrl.signal })
```

- [ ] **Step 4: Run test to verify it passes**

Run: `npx vitest run web/src/composables/__tests__/useChatSession.test.ts`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add web/src/composables/useChatSession.ts web/src/composables/__tests__/useChatSession.test.ts
git commit -m "feat(web): loadHistory requests view=summary"
```

---

### Task 6: Frontend parses `summaryCards` and lazy-fetches original

**Files:**
- Modify: `web/src/utils/chatSessionUtils.ts`
- Modify: `web/src/components/chat/ChatPanelContent.vue`
- Modify: `web/src/components/chat/ChatMessageItem.vue`
- Modify: `web/src/components/chat/ChatMessageList.vue`
- Test: `web/src/utils/__tests__/chatSessionUtils.test.ts`, `web/src/components/chat/__tests__/ChatMessageItem.test.ts`

- [ ] **Step 1: Write failing test for parseMessages with summaryCards**

Add to `web/src/utils/__tests__/chatSessionUtils.test.ts`:

```ts
import { parseMessages, applySummaryUpdate } from '@/utils/chatSessionUtils'

describe('summaryCards parsing', () => {
  it('attaches summaryCards to assistant messages', () => {
    const raw = [{
      id: 1, role: 'assistant', content: '{"blocks":[]}',
      summary: 'sum', summaryCards: { tools: [{ name: 'Bash', id: 't1' }], taskIDs: [1], askQuestions: [] },
    }]
    const msgs = parseMessages(raw, () => ({ blocks: [] }), [], true)
    expect((msgs[0] as any).summaryCards).toBeTruthy()
    expect((msgs[0] as any).summaryCards.tools[0].name).toBe('Bash')
  })

  it('applySummaryUpdate stores cards', () => {
    const msg: any = { id: 1, role: 'assistant', blocks: [] }
    applySummaryUpdate(msg, 'sum', { tools: [{ name: 'AskUserQuestion' }] }, true)
    expect(msg.summary).toBe('sum')
    expect(msg.summaryCards.tools[0].name).toBe('AskUserQuestion')
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `npx vitest run web/src/utils/__tests__/chatSessionUtils.test.ts`
Expected: FAIL — summaryCards not applied

- [ ] **Step 3: Update parseMessages and applySummaryUpdate**

In `web/src/utils/chatSessionUtils.ts`:

```ts
export function applySummaryUpdate(
  msg: Record<string, unknown>,
  summary: string | null | undefined,
  summaryCards: Record<string, unknown> | null | undefined,
  _atBottom: boolean
): void {
  msg.summary = summary
  if (summaryCards !== undefined && summaryCards !== null) {
    msg.summaryCards = summaryCards
  }
  if (msg.showingSummary === undefined) {
    msg.showingSummary = summary != null && summary !== ''
  }
}
```

In `parseMessages`, `summaryCards` is already a field on the raw msg object (spread), so it carries through automatically — no explicit copy needed. Verify the `onParseAssistantContent` spread doesn't drop it (it only touches `msg.content`). If the raw object is a plain map, `summaryCards` stays.

Update the `applySummaryUpdate` call sites (ChatPanelContent.vue `handleSummaryUpdate` and useChatStream.ts) to pass cards (Step 4/5).

- [ ] **Step 4: Update ChatPanelContent toggle + summary handler**

In `web/src/components/chat/ChatPanelContent.vue`, `handleSummaryUpdate` (line 877):

```ts
function handleSummaryUpdate(e) {
    const data = e.detail
    if (!data?.targetID) return
    const msgId = String(data.targetID)
    const msg = messages.value.find(m => String(m.id) === msgId)
    if (!msg) return
    const atBottom = messageListRef.value?.isAtBottom() ?? true
    applySummaryUpdate(msg, data.summary, data.summaryCards, atBottom)
}
```

`handleToggleSummary` (line 888) — when switching to original and the message has no loaded blocks, lazily fetch:

```ts
async function handleToggleSummary(msgId) {
    const msg = messages.value.find(m => m.id === msgId)
    if (!msg) return
    // Switching to ORIGINAL and blocks not yet loaded -> lazy fetch
    if (msg.showingSummary && (!msg.blocks || msg.blocks.length === 0)) {
      await ensureMessageContent(msg)
    }
    msg.showingSummary = !msg.showingSummary
}

async function ensureMessageContent(msg) {
    if (msg._loadingOriginal) return
    msg._loadingOriginal = true
    try {
      const resp = await fetch(`/api/rag/message?id=${msg.id}`)
      if (!resp.ok) { msg._loadingOriginal = false; return }
      const full = await resp.json()
      const { blocks } = onParseContent(full.content || '')
      msg.blocks = blocks
      if (full.files) msg.files = full.files
    } catch { /* ignore */ }
    msg._loadingOriginal = false
}
```

Wire `onParseContent` — the component has access to `onParseAssistantContent` (from useChatSession or a parser util). Locate how the component already parses assistant content and reuse it. Update the `@toggle-summary` handler flow accordingly.

- [ ] **Step 5: Update useChatStream summary_update handler**

In `web/src/composables/useChatStream.ts`, the handler that sets `existing.summary = data.summary` (lines ~287, 321) should also set cards:

```ts
            if (data.summary !== undefined) existing.summary = data.summary
            if (data.summaryCards !== undefined) existing.summaryCards = data.summaryCards
```

Apply to both `existing` (streaming text present) and `newBlock` branches.

- [ ] **Step 6: Pass summaryCards through ChatMessageItem/List**

In `web/src/components/chat/ChatMessageItem.vue`, pass `summaryCards` to `ContentBlocks`:

```html
        :summary="msg.summary"
        :summaryCards="msg.summaryCards"
        :showingSummary="msg.showingSummary"
```

In `ContentBlocks.vue`, add prop:

```ts
  summaryCards: { type: Object, default: null },
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `npx vitest run web/src/utils/__tests__/chatSessionUtils.test.ts web/src/components/chat/__tests__/ChatMessageItem.test.ts`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add web/src/utils/chatSessionUtils.ts web/src/utils/__tests__/chatSessionUtils.test.ts web/src/components/chat/ChatPanelContent.vue web/src/components/chat/ChatMessageItem.vue web/src/components/chat/ChatMessageList.vue web/src/components/chat/ContentBlocks.vue web/src/composables/useChatStream.ts
git commit -m "feat(web): parse summaryCards and lazy-fetch original content"
```

---

### Task 7: ContentBlocks summary mode renders summary + summaryCards (no block traversal)

**Files:**
- Modify: `web/src/components/chat/ContentBlocks.vue`
- Test: `web/src/components/chat/__tests__/ContentBlocks.test.ts`

- [ ] **Step 1: Write failing test**

Add to `web/src/components/chat/__tests__/ContentBlocks.test.ts` a test rendering a message in summary mode with `summary` and `summaryCards`, asserting the summary text appears and the card data (tool name, task id, ask-question text) renders — and that blocks are NOT traversed for cards in summary mode.

- [ ] **Step 2: Run test to verify it fails**

Run: `npx vitest run web/src/components/chat/__tests__/ContentBlocks.test.ts`
Expected: FAIL — summary mode doesn't render cards from summaryCards

- [ ] **Step 3: Rewrite the summary-mode template**

In `web/src/components/chat/ContentBlocks.vue`, replace the summary-mode `<template v-if="showingSummary && summary">` block (lines 13-75) so it renders from `summary` + `summaryCards` instead of traversing `blocks`:

```html
    <div v-show="showingSummary && summary" v-html="renderTextBlock(summary || '', msgId, 0, false)"></div>
    <template v-if="showingSummary && summary && summaryCards">
      <!-- Auto-expand tool cards -->
      <div v-for="(tc, tci) in summaryCards.tools" :key="'sum-tool-' + tci" class="chat-tool-call done" :data-category="getToolIcon(tc.name).category">
        <component :is="getToolIcon(tc.name).icon" :size="12" class="tool-icon" />
        <span class="tool-name">{{ toolDisplayName(tc.name, tc.input) }}</span>
        <span v-if="toolCallSummary(tc)" class="tool-summary">{{ toolCallSummary(tc) }}</span>
        <CheckCircle2 :size="14" color="#22c55e" class="tool-check" />
      </div>
      <!-- Scheduled task cards (real-time data fetched on demand) -->
      <template v-if="summaryCards.taskIDs && summaryCards.taskIDs.length">
        <!-- For each taskID, resolve the task via blockTasks/scheduledTaskKeys; render the card -->
      </template>
      <!-- Ask-question cards -->
      <div v-for="(aq, aqi) in summaryCards.askQuestions" :key="'sum-ask-' + aqi" class="chat-tool-call done" data-category="ask">
        <component :is="getToolIcon('AskUserQuestion').icon" :size="12" class="tool-icon" />
        <span class="tool-name">{{ t('tool.askUser.name') }}</span>
        <div class="tool-detail" v-html="formatToolInput({ question: aq.text, options: aq.options }, 'AskUserQuestion')"></div>
      </div>
    </template>
```

For scheduled task cards, reuse the existing `blockTasks` machinery: since `summaryCards.taskIDs` are known, call the same batch-fetch (`fetchBatchTaskData` via `chatRender`) in an `onMounted`/`watch` when summary mode is active, then render using the existing scheduled-task card markup. To keep this task focused, add a helper in the script that, when `summaryCards` is present and has taskIDs, dispatches the same `extractScheduledTasks`-style fetch. Reuse the `taskKeyIndex`/`scheduledTaskKeys` utilities with keys derived from `summaryCards.taskIDs`.

Remove the old block-traversal summary branch. Keep the `v-show` summary text div.

- [ ] **Step 4: Run test to verify it passes**

Run: `npx vitest run web/src/components/chat/__tests__/ContentBlocks.test.ts`
Expected: PASS

- [ ] **Step 5: Run full frontend test suite**

Run: `npx vitest run`
Expected: PASS (fix any snapshot/assert regressions from the template rewrite)

- [ ] **Step 6: Commit**

```bash
git add web/src/components/chat/ContentBlocks.vue web/src/components/chat/__tests__/ContentBlocks.test.ts
git commit -m "feat(web): render summary mode from summary + summaryCards"
```

---

### Task 8: Frontend typecheck + lint + full verification

**Files:** none (verification only)

- [ ] **Step 1: Run Go tests**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 2: Run frontend tests + typecheck + lint**

Run: `npx vitest run`
Run: `npx vue-tsc --noEmit` (or the project's typecheck script)
Run: `npm run lint`
Expected: all PASS

- [ ] **Step 3: Run the pre-push checks**

Run: `./scripts/pre-push-checks.sh`
Expected: PASS (lint + test + build + typecheck + coverage)

- [ ] **Step 4: Commit (if any fixes needed)**

```bash
git add -A
git commit -m "chore: fix typecheck/lint from summary-first feature"
```

---

## Self-Review Notes

- **Spec coverage:** Every grilling decision maps to a task: view=summary (T4, T5), backend omits content (T4), summaryCards column + no old-data rewrite (T2), card extraction at AsyncSummarize (T3), single-message lazy fetch via /api/rag/message (T6), unbounded cache keeping blocks in message object (T6 — blocks stay, no eviction), summary mode reads summary+summaryCards (T7), streaming/无摘要 keeps content (T4 guard `!msg.Streaming`).
- **Placeholder scan:** The DB-test setup comment in T4 Step 1 and the scheduled-task card template in T7 Step 3 are the two thinnest spots. The engineer must consult existing `internal/service/chat_test.go` helpers (already present) and the existing scheduled-task card markup (ContentBlocks.vue lines 30-54) to fill the exact reused markup. These are flagged inline, not left as silent TODOs.
- **Type consistency:** `SaveSummaryWithCards(targetType, targetID, summary, cards)`, `GetSummaryWithCards(...)`, `extractSummaryCards(blocks)`, `applySummaryUpdate(msg, summary, cards, atBottom)` are used consistently across tasks. `GetChatHistoryPaged` gains a trailing `summaryView bool`; all callers updated in T4. `SummaryUpdateData` gains `SummaryCards` in T3; consumed in T6.
