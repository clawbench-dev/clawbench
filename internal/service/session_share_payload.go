package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"clawbench/internal/model"
)

// Session share snapshot building.
//
// The snapshot is the whole contract between the authenticated creator and the
// anonymous viewer: once written, nothing else is read from the database. It
// therefore has to be self-sufficient, which drives two decisions that are easy
// to get wrong:
//
//  1. Full tool input/output and thinking text are INLINED. chat_history.content
//     stores tool_use blocks with input/output stripped (model.ContentBlock.
//     MarshalJSON) and thinking blocks with only a think_id; the real payload
//     lives in chat_tool_calls / chat_thinking. A naive re-marshal of
//     model.ContentBlock would silently produce a snapshot full of empty tool
//     cards, so this file works on map[string]any and never round-trips through
//     ContentBlock.
//
//  2. Both the reading summary AND the original blocks are included, so the
//     share page can offer the same summary/original toggle as the app without
//     a second request.
//
// Execution plan (chat_sessions.context_state / ACP cached state) is deliberately
// NOT included: it is transient, never persisted per-message, and has no
// representation in ContentBlock.

const (
	// sessionSharePayloadVersion is bumped when the payload shape changes in a
	// way the viewer must know about.
	sessionSharePayloadVersion = 1

	// maxSessionSharePayloadBytes caps the stored snapshot. Tool outputs dominate
	// the size, so over the cap the longest outputs are trimmed first (see
	// trimPayloadToCap) rather than failing the share outright.
	maxSessionSharePayloadBytes = 16 << 20 // 16 MiB

	// sessionShareOutputTrimFloor is the size below which a tool output is never
	// trimmed, so trimming degrades gracefully instead of gutting everything.
	sessionShareOutputTrimFloor = 4 << 10 // 4 KiB

	// sessionSharePreviewRunes bounds one selection-list preview line.
	sessionSharePreviewRunes = 200

	// maxSessionShareTrimIterations bounds the per-candidate halving loop. Each
	// iteration must at least halve one output, so the natural bound is ~log2 of
	// the largest output; this is a belt-and-braces stop so a future regression
	// in the shrink invariant fails fast instead of spinning forever inside a
	// request. 64 is far above what any real payload needs.
	maxSessionShareTrimIterations = 64
)

// ErrSessionShareEmptySelection is returned when the requested selection
// resolves to no shareable messages (e.g. every selected message is still
// streaming). The handler maps it to a 400 with a specific message.
var ErrSessionShareEmptySelection = errors.New("session share selection is empty")

// ErrSessionShareUnknownMessage is returned when the request names a message id
// that is not a finalized message of the session (unknown, streaming, or
// belonging to a different session). Rejecting rather than silently dropping
// keeps the frozen snapshot faithful to what the user saw selected.
var ErrSessionShareUnknownMessage = errors.New("session share selection names an unknown message")

// SessionSharePayload is the frozen snapshot stored in session_shares.payload.
type SessionSharePayload struct {
	Version   int                   `json:"version"`
	CreatedAt time.Time             `json:"createdAt"`
	Session   SessionShareSession   `json:"session"`
	Messages  []SessionShareMessage `json:"messages"`
}

// SessionShareSession is the session-level metadata shown in the share header.
// project_path is deliberately absent: the viewer has no use for it and it
// would leak the creator's directory layout.
type SessionShareSession struct {
	Title     string     `json:"title"`
	Backend   string     `json:"backend"`
	AgentID   string     `json:"agentId,omitempty"`
	Model     string     `json:"model,omitempty"`
	CreatedAt *time.Time `json:"createdAt,omitempty"`
}

// SessionShareMessage mirrors model.ChatMessage's JSON shape so the viewer can
// feed the payload straight into the existing chat render pipeline. Content
// stays a JSON string (the same encoding chat_history uses) because
// parseAssistantContent on the frontend expects it.
type SessionShareMessage struct {
	ID           int64               `json:"id"`
	Role         string              `json:"role"`
	Content      string              `json:"content"`
	Files        []model.FileEntry   `json:"files,omitempty"`
	CreatedAt    time.Time           `json:"createdAt"`
	Summary      *string             `json:"summary,omitempty"`
	SummaryCards *model.SummaryCards `json:"summaryCards,omitempty"`
}

// SessionMessagePreview is one row of the share dialog's message list. It is
// returned for EVERY message of the session — including streaming/queued ones —
// so the dialog can show them disabled with a reason instead of hiding them
// (a message that silently vanishes from the list is confusing).
type SessionMessagePreview struct {
	ID        int64  `json:"id"`
	Role      string `json:"role"`
	Preview   string `json:"preview"`
	Streaming bool   `json:"streaming"`
	Queued    bool   `json:"queued"`
	CreatedAt string `json:"createdAt"`
}

// GetSessionMessagesForSelection lists every message of a session (finalized or
// not) in chronological order, with a plain-text preview for the dialog.
func GetSessionMessagesForSelection(sessionID string) ([]SessionMessagePreview, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("session id is required")
	}
	rows, err := ReadDB().Query(
		`SELECT id, role, content, streaming, queued, created_at FROM chat_history
		 WHERE session_id = ? ORDER BY id ASC`,
		sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("list session messages for selection: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]SessionMessagePreview, 0)
	for rows.Next() {
		var (
			id        int64
			role      string
			content   string
			streaming int
			queued    int
			createdAt string
		)
		if err := rows.Scan(&id, &role, &content, &streaming, &queued, &createdAt); err != nil {
			return nil, fmt.Errorf("scan session message for selection: %w", err)
		}
		items = append(items, SessionMessagePreview{
			ID:        id,
			Role:      role,
			Preview:   clipRunes(ExtractPlainText(content), sessionSharePreviewRunes),
			Streaming: streaming != 0,
			Queued:    queued != 0,
			CreatedAt: createdAt,
		})
	}
	return items, rows.Err()
}

// BuildSessionSharePayload freezes a session into a snapshot JSON string.
//
// messageIDs selects which messages to include; an empty slice means "every
// finalized message". Only finalized messages (streaming = 0 AND queued = 0)
// are shareable: a half-written reply would freeze mid-sentence. Selecting a
// non-finalized or unknown id is an error rather than a silent drop.
//
// projectRoot and homeDir are used to relativize absolute paths in the payload.
// Returns the payload and the number of messages included.
func BuildSessionSharePayload(sessionID string, messageIDs []int64, projectRoot, homeDir string) (string, int, error) {
	info := GetSessionFullInfo(sessionID)
	if info == nil {
		return "", 0, fmt.Errorf("session %s not found", sessionID)
	}

	// GetMessagesBySessionIDRaw already filters to finalized messages and keeps
	// content un-stripped (unlike the paged reader, which replaces summarized
	// content with an empty blocks array).
	messages, err := GetMessagesBySessionIDRaw(sessionID)
	if err != nil {
		return "", 0, fmt.Errorf("load session messages: %w", err)
	}
	if len(messages) == 0 {
		return "", 0, ErrSessionShareEmptySelection
	}

	messages, err = selectShareMessages(messages, messageIDs)
	if err != nil {
		return "", 0, err
	}

	toolCalls, err := loadToolCallsByMessage(sessionID)
	if err != nil {
		return "", 0, err
	}
	thinking, err := loadThinkingByMessage(sessionID)
	if err != nil {
		return "", 0, err
	}
	summaries, cards, err := loadSummariesForMessages(messageIDsOf(messages))
	if err != nil {
		return "", 0, err
	}

	out := SessionSharePayload{
		Version:   sessionSharePayloadVersion,
		CreatedAt: time.Now().UTC(),
		Session: SessionShareSession{
			Title:   info.Title,
			Backend: info.Backend,
			AgentID: info.AgentID,
			Model:   info.Model,
		},
		Messages: make([]SessionShareMessage, 0, len(messages)),
	}
	for i := range messages {
		out.Messages = append(out.Messages,
			buildShareMessage(messages[i], toolCalls, thinking, summaries, cards, projectRoot, homeDir))
	}

	raw, err := json.Marshal(out)
	if err != nil {
		return "", 0, fmt.Errorf("marshal session share payload: %w", err)
	}
	raw = trimPayloadToCap(raw, &out)
	return string(raw), len(out.Messages), nil
}

// selectShareMessages narrows the session's finalized messages to the client's
// selection, restoring chronological order regardless of the order sent. An
// empty selection means "every message". An id matching no finalized message is
// an error rather than a silent drop, so the frozen snapshot always matches what
// the user believed they selected.
func selectShareMessages(messages []model.ChatMessage, messageIDs []int64) ([]model.ChatMessage, error) {
	if len(messageIDs) == 0 {
		return messages, nil
	}
	byID := make(map[int64]model.ChatMessage, len(messages))
	for _, m := range messages {
		byID[m.ID] = m
	}
	selected := make([]model.ChatMessage, 0, len(messageIDs))
	for _, id := range messageIDs {
		m, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("%w: message %d", ErrSessionShareUnknownMessage, id)
		}
		selected = append(selected, m)
	}
	// Restore chronological order regardless of the order the client sent.
	sort.Slice(selected, func(i, j int) bool { return selected[i].ID < selected[j].ID })
	return selected, nil
}

// buildShareMessage freezes one message: its content is rewritten with the full
// tool/thinking payload inlined and its attachments relativized. An assistant
// turn additionally carries its reading summary and cards.
func buildShareMessage(
	msg model.ChatMessage,
	toolCalls map[toolCallKey]ToolCallRecord,
	thinking map[thinkingKey]string,
	summaries map[int64]string,
	cards map[int64]*model.SummaryCards,
	projectRoot, homeDir string,
) SessionShareMessage {
	// Assistant content is a JSON wrapper; rewrite it with the full payload
	// inlined. User content may be plain text or the same wrapper, so it goes
	// through the same path and falls back to itself when it is not JSON.
	item := SessionShareMessage{
		ID:        msg.ID,
		Role:      msg.Role,
		Content:   inlineMessageContent(msg, toolCalls, thinking, projectRoot, homeDir),
		CreatedAt: msg.CreatedAt,
	}
	if len(msg.Files) > 0 {
		item.Files = sanitizeFileEntries(msg.Files, projectRoot, homeDir)
	}
	if msg.Role != roleAssistant {
		return item
	}
	if s, ok := summaries[msg.ID]; ok {
		// The reading summary routinely names the files the agent touched
		// ("I edited /home/u/proj/secret.ts"), so it must go through the same
		// path sanitizer as the rest of the snapshot. Storing it verbatim was
		// the largest absolute-path leak in the payload.
		v := sanitizeShareString(s, projectRoot, homeDir)
		item.Summary = &v
	}
	if c, ok := cards[msg.ID]; ok {
		item.SummaryCards = sanitizeSummaryCards(c, projectRoot, homeDir)
	}
	return item
}

// messageIDsOf extracts the ids from a message slice.
func messageIDsOf(messages []model.ChatMessage) []int64 {
	ids := make([]int64, 0, len(messages))
	for _, m := range messages {
		ids = append(ids, m.ID)
	}
	return ids
}

// toolCallKey identifies one tool call: a tool id is unique per message but not
// per session, so both parts are needed.
type toolCallKey struct {
	messageID int64
	toolID    string
}

// thinkingKey identifies one thinking block. think_id is unique per message in
// practice, but keying on both mirrors the tool-call map and the DB unique
// constraint (think_id, message_id, seq).
type thinkingKey struct {
	messageID int64
	thinkID   string
}

// loadToolCallsByMessage returns the session's tool calls keyed by message+tool.
func loadToolCallsByMessage(sessionID string) (map[toolCallKey]ToolCallRecord, error) {
	records, err := GetToolCallsBySession(sessionID)
	if err != nil {
		return nil, fmt.Errorf("load session tool calls: %w", err)
	}
	out := make(map[toolCallKey]ToolCallRecord, len(records))
	for _, r := range records {
		out[toolCallKey{messageID: r.MessageID, toolID: r.ToolID}] = r
	}
	return out, nil
}

// loadThinkingByMessage returns the session's thinking text keyed by
// message+think_id. GetThinkingBySessionAll already concatenates seq chunks.
func loadThinkingByMessage(sessionID string) (map[thinkingKey]string, error) {
	records, err := GetThinkingBySessionAll(sessionID)
	if err != nil {
		return nil, fmt.Errorf("load session thinking: %w", err)
	}
	out := make(map[thinkingKey]string, len(records))
	for _, r := range records {
		out[thinkingKey{messageID: r.MessageID, thinkID: r.ThinkID}] = r.Text
	}
	return out, nil
}

// loadSummariesForMessages reads the reading summaries for the given messages.
//
// Deliberately NOT enrichMessagesWithSummaries: that helper strips the message
// content (summarizeContentForView) and fires an async backfill goroutine, both
// of which are wrong here — the snapshot needs the full content AND the summary
// side by side, and a share request must not trigger background work.
func loadSummariesForMessages(messageIDs []int64) (map[int64]string, map[int64]*model.SummaryCards, error) {
	summaries := map[int64]string{}
	cards := map[int64]*model.SummaryCards{}
	if len(messageIDs) == 0 {
		return summaries, cards, nil
	}

	query := "SELECT target_id, summary, COALESCE(summary_cards, '') FROM summaries WHERE target_type = 'chat_message' AND target_id IN ("
	args := make([]any, len(messageIDs))
	for i, id := range messageIDs {
		if i > 0 {
			query += ","
		}
		query += "?"
		args[i] = id
	}
	query += ")"

	rows, err := ReadDB().Query(query, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("load session summaries: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var (
			targetID  int64
			summary   string
			cardsJSON string
		)
		if err := rows.Scan(&targetID, &summary, &cardsJSON); err != nil {
			return nil, nil, fmt.Errorf("scan session summary: %w", err)
		}
		summaries[targetID] = summary
		if cardsJSON != "" {
			var c model.SummaryCards
			if json.Unmarshal([]byte(cardsJSON), &c) == nil {
				cards[targetID] = &c
			}
		}
	}
	return summaries, cards, rows.Err()
}

// inlineMessageContent rewrites a message's stored content so the snapshot is
// self-contained: tool_use blocks regain their input/output and thinking blocks
// regain their text.
//
// Works on map[string]any on purpose. Round-tripping through model.ContentBlock
// would re-apply the slimming MarshalJSON and drop exactly the fields being
// restored here.
func inlineMessageContent(msg model.ChatMessage, toolCalls map[toolCallKey]ToolCallRecord, thinking map[thinkingKey]string, projectRoot, homeDir string) string {
	trimmed := strings.TrimSpace(msg.Content)
	if !strings.HasPrefix(trimmed, "{") {
		// Plain-text user message (or unparseable content) — sanitize and keep.
		return sanitizeShareString(msg.Content, projectRoot, homeDir)
	}

	var wrapper map[string]any
	if err := json.Unmarshal([]byte(trimmed), &wrapper); err != nil {
		return sanitizeShareString(msg.Content, projectRoot, homeDir)
	}

	blocksRaw, ok := wrapper["blocks"]
	if !ok {
		return sanitizeShareString(msg.Content, projectRoot, homeDir)
	}
	blocks, ok := blocksRaw.([]any)
	if !ok {
		// {"blocks":null} and friends: leave the wrapper as-is.
		return sanitizeShareString(msg.Content, projectRoot, homeDir)
	}

	for _, b := range blocks {
		block, ok := b.(map[string]any)
		if !ok {
			continue
		}
		sanitizeShareBlock(block, msg.ID, toolCalls, thinking, projectRoot, homeDir)
	}

	// metadata is kept (the viewer's metadata modal reads model/token/cost from
	// it) but sanitized: it can carry request ids and provider URLs.
	if meta, ok := wrapper["metadata"]; ok {
		wrapper["metadata"] = sanitizeShareValue(meta, projectRoot, homeDir)
	}

	out, err := json.Marshal(wrapper)
	if err != nil {
		return sanitizeShareString(msg.Content, projectRoot, homeDir)
	}
	return string(out)
}

// sanitizeShareBlock sanitizes one content block in place, restoring the heavy
// fields the storage layer stripped (tool input/output, thinking text) from the
// side tables and relativizing every path it carries.
func sanitizeShareBlock(
	block map[string]any,
	messageID int64,
	toolCalls map[toolCallKey]ToolCallRecord,
	thinking map[thinkingKey]string,
	projectRoot, homeDir string,
) {
	// A slim tool_use block carries a TOP-LEVEL file_path (see
	// model.SlimBlock.FilePath), separate from the copy inside `input`. It is
	// sanitized here because the switch below only touches the fields it
	// explicitly names, and a path left in this slot would ship the creator's
	// absolute directory layout into a public snapshot.
	if fp, ok := block["file_path"].(string); ok && fp != "" {
		block["file_path"] = relativizeSharePath(fp, projectRoot, homeDir)
	}
	switch block["type"] {
	case eventTypeToolUse:
		sanitizeToolUseBlock(block, messageID, toolCalls, projectRoot, homeDir)
	case blockTypeThinking:
		thinkID, _ := block["think_id"].(string)
		if thinkID == "" {
			return
		}
		if text, found := thinking[thinkingKey{messageID: messageID, thinkID: thinkID}]; found {
			block["text"] = sanitizeShareString(text, projectRoot, homeDir)
		}
	case contentKeyText, blockTypeWarning, eventTypeError:
		if s, ok := block["text"].(string); ok {
			block["text"] = sanitizeShareString(s, projectRoot, homeDir)
		}
	}
}

// sanitizeToolUseBlock restores a tool_use block's heavy payload from the side
// table. The stored block already carries name/status/done/summary; the side
// table is authoritative for input/output and for status, which the block may
// not have received if the tool finished after the message was persisted.
func sanitizeToolUseBlock(
	block map[string]any,
	messageID int64,
	toolCalls map[toolCallKey]ToolCallRecord,
	projectRoot, homeDir string,
) {
	// Sanitize whatever input/output the block already carries BEFORE the
	// side-table lookup, because not every tool_use block has a side-table row.
	//
	// model.ContentBlock.MarshalJSON keeps `input` INLINE for the interactive
	// tools (AskUserQuestion / PermissionApproval) so their cards can render
	// immediately, and ConvertAskQuestionBlocks creates them with no
	// chat_tool_calls row at all. Returning early on !found therefore shipped
	// those inline inputs — which carry tool arguments such as a shell command
	// or a file path — into the public snapshot untouched.
	if raw, ok := block["input"]; ok && raw != nil {
		block["input"] = sanitizeShareValue(deepCopyJSONValue(raw), projectRoot, homeDir)
	}
	if out, ok := block["output"].(string); ok && out != "" {
		block["output"] = sanitizeShareString(out, projectRoot, homeDir)
	}

	toolID, _ := block["id"].(string)
	if toolID == "" {
		return
	}
	rec, found := toolCalls[toolCallKey{messageID: messageID, toolID: toolID}]
	if !found {
		return
	}
	if len(rec.Input) > 0 {
		var input any
		if json.Unmarshal(rec.Input, &input) == nil {
			block["input"] = sanitizeShareValue(input, projectRoot, homeDir)
		}
	}
	if rec.Output != "" {
		block["output"] = sanitizeShareString(rec.Output, projectRoot, homeDir)
	}
	if rec.Status != "" {
		block["status"] = rec.Status
	}
	block["done"] = rec.Done
	if rec.DurationMs > 0 {
		block["duration_ms"] = rec.DurationMs
	}
	if rec.Summary != "" {
		block["summary"] = sanitizeShareString(rec.Summary, projectRoot, homeDir)
	}
}

// sanitizeSummaryCards prepares summary cards for the snapshot.
//
// TaskIDs are dropped: scheduled-task cards are out of scope for the share page
// (the viewer cannot reach /api/tasks, and a task card would render as a
// permanent "loading" skeleton). Everything else the summary view needs —
// tools, warnings, ask-questions, file changes — is kept, with paths relativized.
func sanitizeSummaryCards(cards *model.SummaryCards, projectRoot, homeDir string) *model.SummaryCards {
	if cards == nil {
		return nil
	}
	out := *cards
	out.TaskIDs = nil
	// Every sub-object is deep-copied before mutation: `out := *cards` is a
	// shallow copy, so writing through a shared slice/map would mutate the
	// caller's cards (which belong to the live session) as a side effect.
	if len(cards.Tools) > 0 {
		tools := make([]model.SummaryTool, len(cards.Tools))
		for i, tool := range cards.Tools {
			// Deep-copy the input map before sanitizing: it is shared with the
			// live session's cards, and mutating it in place would rewrite the
			// running conversation's data as a side effect of sharing.
			if len(tool.Input) > 0 {
				if sanitized, ok := sanitizeShareValue(deepCopyJSONMap(tool.Input), projectRoot, homeDir).(map[string]any); ok {
					tool.Input = sanitized
				}
			}
			tool.Output = sanitizeShareString(tool.Output, projectRoot, homeDir)
			tools[i] = tool
		}
		out.Tools = tools
	}
	if len(cards.AskQuestions) > 0 {
		questions := make([]model.AskQuestionCard, len(cards.AskQuestions))
		for i, q := range cards.AskQuestions {
			q.Header = sanitizeShareString(q.Header, projectRoot, homeDir)
			q.Question = sanitizeShareString(q.Question, projectRoot, homeDir)
			options := make([]model.AskQuestionOption, len(q.Options))
			for j, opt := range q.Options {
				opt.Label = sanitizeShareString(opt.Label, projectRoot, homeDir)
				opt.Description = sanitizeShareString(opt.Description, projectRoot, homeDir)
				options[j] = opt
			}
			q.Options = options
			questions[i] = q
		}
		out.AskQuestions = questions
	}
	if len(cards.CreatedFiles) > 0 {
		out.CreatedFiles = sanitizeFileChanges(cards.CreatedFiles, projectRoot, homeDir)
	}
	if len(cards.ModifiedFiles) > 0 {
		out.ModifiedFiles = sanitizeFileChanges(cards.ModifiedFiles, projectRoot, homeDir)
	}
	if len(cards.Warnings) > 0 {
		warnings := make([]model.SummaryWarning, len(cards.Warnings))
		for i, w := range cards.Warnings {
			w.Text = sanitizeShareString(w.Text, projectRoot, homeDir)
			warnings[i] = w
		}
		out.Warnings = warnings
	}
	return &out
}

// sanitizeFileChanges relativizes the paths of a created/modified file list.
// ToolIDs are kept: the viewer uses them to key the diff drawer, and they are
// opaque ids with no path content.
func sanitizeFileChanges(changes model.SummaryFileChanges, projectRoot, homeDir string) model.SummaryFileChanges {
	out := make(model.SummaryFileChanges, len(changes))
	for i, c := range changes {
		c.Path = relativizeSharePath(c.Path, projectRoot, homeDir)
		out[i] = c
	}
	return out
}

// sanitizeFileEntries relativizes attachment paths.
func sanitizeFileEntries(entries []model.FileEntry, projectRoot, homeDir string) []model.FileEntry {
	out := make([]model.FileEntry, 0, len(entries))
	for _, e := range entries {
		if e.IsURL() {
			out = append(out, e) // external URL: nothing local to leak
			continue
		}
		e.Path = relativizeSharePath(e.Path, projectRoot, homeDir)
		out = append(out, e)
	}
	return out
}

// deepCopyJSONMap returns a recursive copy of a decoded-JSON map, so
// sanitizeShareValue (which mutates in place) can be applied to a summary
// card's input without rewriting the live session's data.
func deepCopyJSONMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = deepCopyJSONValue(v)
	}
	return out
}

// deepCopyJSONValue recursively copies decoded-JSON slices and maps; scalars are
// returned as-is (they are immutable).
func deepCopyJSONValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		return deepCopyJSONMap(t)
	case []any:
		out := make([]any, len(t))
		for i := range t {
			out[i] = deepCopyJSONValue(t[i])
		}
		return out
	default:
		return v
	}
}

// sanitizeShareValue walks a decoded JSON value and sanitizes every string leaf.
func sanitizeShareValue(v any, projectRoot, homeDir string) any {
	switch t := v.(type) {
	case string:
		return sanitizeShareString(t, projectRoot, homeDir)
	case []any:
		for i := range t {
			t[i] = sanitizeShareValue(t[i], projectRoot, homeDir)
		}
		return t
	case map[string]any:
		for k := range t {
			t[k] = sanitizeShareValue(t[k], projectRoot, homeDir)
		}
		return t
	default:
		return v
	}
}

// sanitizeShareString replaces absolute paths in free text with relative ones.
//
// Best-effort by design: the goal is to stop the snapshot from disclosing the
// creator's directory layout, not to guarantee no path-shaped string survives.
// A path under neither projectRoot nor homeDir has nothing to be relativized
// against and is left alone.
func sanitizeShareString(s, projectRoot, homeDir string) string {
	if s == "" {
		return s
	}
	if projectRoot != "" {
		root := strings.TrimRight(projectRoot, `/\`)
		if root != "" {
			// Replace both separator spellings so a Windows path stored with
			// forward slashes is still caught.
			s = strings.ReplaceAll(s, root+`\`, `.\`)
			s = strings.ReplaceAll(s, root+`/`, "./")
		}
	}
	if homeDir != "" {
		home := strings.TrimRight(homeDir, `/\`)
		if home != "" {
			s = strings.ReplaceAll(s, home+`\`, `~\`)
			s = strings.ReplaceAll(s, home+`/`, "~/")
		}
	}
	return s
}

// relativizeSharePath turns an absolute path into a project-relative one, or
// reduces it to its base name when it lies outside the project. Used for
// structured path fields (attachments, file-change lists) where a partial
// string replacement would be wrong.
func relativizeSharePath(p, projectRoot, homeDir string) string {
	if p == "" {
		return p
	}
	if projectRoot != "" {
		if rel, err := filepath.Rel(projectRoot, p); err == nil &&
			rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return filepath.ToSlash(rel)
		}
	}
	if homeDir != "" {
		if rel, err := filepath.Rel(homeDir, p); err == nil &&
			rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "~/" + filepath.ToSlash(rel)
		}
	}
	return filepath.Base(p)
}

// trimPayloadToCap reduces an over-cap payload by shortening the longest tool
// outputs, then re-marshals. A share that degrades is better than a share that
// fails: the alternative (rejecting the request) leaves the user with no way to
// share a long session at all.
//
// Returns the payload to store (possibly trimmed) and, when trimming happened,
// the affected blocks carry truncated=true so the viewer can say so.
func trimPayloadToCap(raw []byte, payload *SessionSharePayload) []byte {
	if len(raw) <= maxSessionSharePayloadBytes {
		return raw
	}

	candidates := collectTrimCandidates(payload)
	if len(candidates) == 0 {
		// Nothing trimmable (size comes from many small blocks or long text).
		// Store as-is rather than destroying content.
		return raw
	}

	// Longest first, so the cut lands on whatever dominates the size.
	sort.SliceStable(candidates, func(i, j int) bool {
		a, _ := candidates[i].block["output"].(string)
		b, _ := candidates[j].block["output"].(string)
		return len(a) > len(b)
	})

	// Halve one output at a time and re-measure after each cut. The previous
	// version re-measured only once per full pass, so the inner early-exit read
	// a stale size and every candidate got halved in lockstep — gutting short
	// outputs even when trimming the single longest would have sufficed.
	for _, c := range candidates {
		next, ok := trimCandidateToFit(raw, payload, c)
		if !ok {
			return raw // re-marshaling failed: keep the last good snapshot
		}
		if raw = next; len(raw) <= maxSessionSharePayloadBytes {
			break
		}
	}
	return raw
}

// trimCandidate identifies one trimmable tool output.
//
// A message body is a JSON *string*, so a candidate must remember which message
// it came from: trimming happens on the decoded wrapper map, and that change
// only reaches the stored payload once it is written back into
// payload.Messages[msgIndex].Content. Skipping the write-back made the trimmer
// a no-op — it re-marshaled the original strings and returned an over-cap
// snapshot, while truncated=true was discarded along with the mutation.
type trimCandidate struct {
	msgIndex int
	wrapper  map[string]any
	block    map[string]any
}

// collectTrimCandidates finds every tool_use block whose output is large enough
// to be worth trimming.
func collectTrimCandidates(payload *SessionSharePayload) []trimCandidate {
	var candidates []trimCandidate
	for i := range payload.Messages {
		trimmed := strings.TrimSpace(payload.Messages[i].Content)
		if !strings.HasPrefix(trimmed, "{") {
			continue
		}
		var wrapper map[string]any
		if json.Unmarshal([]byte(trimmed), &wrapper) != nil {
			continue
		}
		blocks, ok := wrapper["blocks"].([]any)
		if !ok {
			continue
		}
		for _, b := range blocks {
			block, ok := b.(map[string]any)
			if !ok || block["type"] != eventTypeToolUse {
				continue
			}
			if out, ok := block["output"].(string); ok && len(out) > sessionShareOutputTrimFloor {
				candidates = append(candidates, trimCandidate{msgIndex: i, wrapper: wrapper, block: block})
			}
		}
	}
	return candidates
}

// trimCandidateToFit halves one candidate's output repeatedly until the payload
// fits or this output reaches the floor. Each cut is written back into the
// message body and the whole payload re-marshaled, so the measured size is the
// size that will actually be stored.
//
// Reaching the floor stops trimming THIS candidate but must not stop the outer
// loop: the remaining (shorter) outputs are still trimmable and may be what
// brings the payload under the cap. ok=false means a re-marshal failed, so the
// caller must keep its last good snapshot rather than storing a truncated one.
func trimCandidateToFit(raw []byte, payload *SessionSharePayload, c trimCandidate) ([]byte, bool) {
	// Bound the loop independently of the size check: the cut must strictly
	// shrink the output for the loop to be guaranteed to terminate, and a
	// future edit that breaks that invariant should fail fast rather than
	// spin forever holding the request (see the rune/byte note below).
	for i := 0; len(raw) > maxSessionSharePayloadBytes; i++ {
		if i > maxSessionShareTrimIterations {
			break
		}
		out, _ := c.block["output"].(string)
		if len(out) <= sessionShareOutputTrimFloor {
			break
		}
		// Halve by RUNE count, not byte length. clipRunes clips runes, so
		// passing len(out)/2 (bytes) made every iteration a no-op for any
		// output whose bytes-per-rune exceeds 1: len(out)/2 >= rune count, so
		// clipRunes returned the string unchanged and each pass only appended
		// the marker — the output GREW and the size check never came true.
		// A CJK tool output (3 bytes/rune) therefore hung the request forever.
		// Trimming is not allowed to increase the output, so the marker is only
		// appended when the cut actually removed something.
		runes := []rune(out)
		if len(runes) <= 1 {
			break
		}
		clipped := string(runes[:len(runes)/2]) + "\n…[truncated for sharing]"
		// The cut must be a strict shrink including the marker, otherwise the
		// loop could make no progress.
		if len(clipped) >= len(out) {
			break
		}
		c.block["output"] = clipped
		c.block["truncated"] = true

		encoded, err := json.Marshal(c.wrapper)
		if err != nil {
			return raw, false
		}
		payload.Messages[c.msgIndex].Content = string(encoded)

		next, err := json.Marshal(payload)
		if err != nil {
			return raw, false
		}
		raw = next
	}
	return raw, true
}

// TrimPayloadToCapForTest exposes the size trimmer so its behavior can be
// asserted directly. It has no production caller outside BuildSessionSharePayload
// and the trim is invisible from the outside once stored, so without this hook
// the cap could regress silently again.
func TrimPayloadToCapForTest(raw []byte, payload *SessionSharePayload) []byte {
	return trimPayloadToCap(raw, payload)
}

// clipRunes returns at most n runes of s, rune-safe so multi-byte text is
// never cut mid-character. Unlike the session_command truncateRunes it appends
// no suffix: callers add their own marker where one is wanted.
func clipRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
