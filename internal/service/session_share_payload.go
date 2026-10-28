package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"clawbench/internal/store"

	"clawbench/internal/grouprouting"
	"clawbench/internal/model"
)

// Session share payload assembly.
//
// The JSON is assembled on EVERY public request from the live tables; nothing
// about the conversation is stored in session_shares beyond the selected message
// ids. Two consequences drive the design:
//
//  1. Full tool input/output and thinking text must be INLINED. chat_history.content
//     stores tool_use blocks with input/output stripped (model.ContentBlock.
//     MarshalJSON) and thinking blocks with only a think_id; the real payload
//     lives in chat_tool_calls / chat_thinking. A naive re-marshal of
//     model.ContentBlock would silently produce a page full of empty tool
//     cards, so this file works on map[string]any and never round-trips through
//     ContentBlock.
//
//  2. Both the reading summary AND the original blocks are included, so the
//     share page can offer the same summary/original toggle as the app without
//     a second request.
//
// Because the assembly is live, it must be DETERMINISTIC: two requests for an
// unchanged conversation have to produce byte-identical output, or the ETag the
// handler derives from it never matches and every request pays full cost. That
// is why createdAt is the share row's timestamp rather than time.Now().
//
// Execution plan (chat_sessions.context_state / ACP cached state) is deliberately
// NOT included: it is transient, never persisted per-message, and has no
// representation in ContentBlock.

const (
	// sessionSharePayloadVersion is bumped when the payload shape changes in a
	// way the viewer must know about. Unlike the snapshot era, a bump now
	// changes every live link at once (there is no per-row stored shape).
	sessionSharePayloadVersion = 1

	// maxSessionSharePayloadBytes caps the assembled payload. Tool outputs
	// dominate the size, so over the cap the longest outputs are trimmed first
	// (see trimPayloadToCap) rather than failing the request outright.
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

// ErrSessionShareUnknownMessage is returned when the CREATION request names a
// message id that is not a finalized message of the session (unknown, streaming,
// or belonging to a different session). Rejecting rather than silently dropping
// keeps the share faithful to what the user saw selected. The read path does not
// use this: a message deleted after the share was created is skipped instead.
var ErrSessionShareUnknownMessage = errors.New("session share selection names an unknown message")

// ErrSessionShareSessionGone is returned when a share's session row no longer
// exists. The handler maps it to 404: from the reader's side it is
// indistinguishable from a revoked link.
var ErrSessionShareSessionGone = errors.New("session share: session no longer exists")

// ErrSessionShareNothingToShow is returned when every message the share selected
// has since been deleted (rewind, hard delete). The handler maps it to 404 — the
// link has no content left, which is the same outcome as revocation.
var ErrSessionShareNothingToShow = errors.New("session share: every shared message was deleted")

// SessionSharePayload is the JSON assembled for one public request.
type SessionSharePayload struct {
	Version   int                   `json:"version"`
	CreatedAt time.Time             `json:"createdAt"`
	Session   SessionShareSession   `json:"session"`
	Messages  []SessionShareMessage `json:"messages"`
	// SessionAgent + Speakers let the public viewer render the real speaker icon
	// per message. The identities always carry the avatar: the viewer has no
	// other way to resolve a speaker's avatar (it cannot call /api/agents), so
	// the avatars ARE embedded in the payload, resolved on every request from
	// the live agent registry. Speakers is keyed by member row id and present
	// only for group sessions.
	SessionAgent *SpeakerIdentity           `json:"sessionAgent,omitempty"`
	Speakers     map[string]SpeakerIdentity `json:"speakers,omitempty"`
	// GroupMembers is the group's SPEAKING roster (disabled members excluded), in
	// roster order, with the host flagged — present only for group sessions. The
	// viewer's topbar renders the same overlapping avatar stack the in-app group
	// header shows. Speakers cannot serve this: it is a map (JSON-marshaled with
	// UUID-sorted keys, so the order is random) that also includes disabled members,
	// and it carries no isHost flag. Each preview carries the member's own avatar (agent_avatar),
	// so the viewer draws the same discs without a DiceBear runtime; a member
	// with no avatar falls back to the per-backend brand icon.
	GroupMembers []model.GroupMemberPreview `json:"groupMembers,omitempty"`
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
	// HostMemberID is the group host's member row id (group sessions only;
	// empty otherwise). The viewer needs it to mark the host's messages and to
	// render their private notes (bcc) card — a group's timeline otherwise
	// cannot tell which speaker is the chair.
	HostMemberID string `json:"hostMemberId,omitempty"`
	// UserNickname is the reserved display name of the human user in this group
	// (chat.user_nickname), read live so the viewer can render @-mention chips
	// for the user without reading the authenticated /api/config.
	UserNickname string `json:"userNickname,omitempty"`
	// SessionType is the chat_sessions.session_type value ("chat" / "group" /
	// "group_member"). The viewer needs an EXPLICIT signal: the roster is not
	// one — GroupMembersForGroups returns only SPEAKING members, so a group whose
	// members are all disabled would look like a single-agent session, and the reader
	// would then see the raw <clawbench-mention> routing tags instead of
	// resolved @-chips.
	SessionType string `json:"sessionType,omitempty"`
}

// SessionShareMessage mirrors model.ChatMessage's JSON shape so the viewer can
// feed the payload straight into the existing chat render pipeline. Content
// stays a JSON string (the same encoding chat_history uses) because
// parseAssistantContent on the frontend expects it.
type SessionShareMessage struct {
	ID      int64             `json:"id"`
	Role    string            `json:"role"`
	Content string            `json:"content"`
	Files   []model.FileEntry `json:"files,omitempty"`
	// AgentID is the speaker's group-member row id (group chats only; empty
	// for ordinary sessions). Preserves speaker attribution in a shared group.
	AgentID      string              `json:"agentId,omitempty"`
	CreatedAt    time.Time           `json:"createdAt"`
	Summary      *string             `json:"summary,omitempty"`
	SummaryCards *model.SummaryCards `json:"summaryCards,omitempty"`
	// SkippedBefore is how many of the session's messages sit between the
	// PREVIOUS shared message and this one, i.e. how many were left out of the
	// selection. Zero for the first shared message and for any run of adjacent
	// messages, so the field is omitted in the common contiguous case.
	//
	// Computed here rather than in the viewer because only the server can see
	// the unselected rows: the payload carries the selection, so a gap is
	// invisible from the client. The viewer draws a divider above a message with
	// a non-zero value — without it a selection like "the question, then the
	// answer from six turns later" reads as if the omitted turns never happened.
	//
	// Counting is by CHRONOLOGICAL GAP in the session's finalized messages, not
	// by id arithmetic: ids are not guaranteed contiguous (other sessions'
	// inserts and deletions interleave), so `id2 - id1 - 1` would overcount.
	SkippedBefore int `json:"skippedBefore,omitempty"`
}

// SessionMessagePreview is one row of the share dialog's message list. It is
// returned for EVERY message of the session — including streaming ones — so the
// dialog can show them disabled with a reason instead of hiding them (a message
// that silently vanishes from the list is confusing). Queued messages are absent
// by construction: they have no chat_history row until dequeued.
type SessionMessagePreview struct {
	ID        int64  `json:"id"`
	Role      string `json:"role"`
	Preview   string `json:"preview"`
	Streaming bool   `json:"streaming"`
	CreatedAt string `json:"createdAt"`
	// AgentID is the speaker's group-member row id for group chats, empty for
	// ordinary single-agent messages. The dialog resolves it against the
	// response's `speakers`/`sessionAgent` to render the real agent icon.
	AgentID string `json:"agentId,omitempty"`
}

// GetSessionMessagesForSelection lists every message of a session (finalized or
// not) in chronological order, with a plain-text preview for the dialog.
func GetSessionMessagesForSelection(sessionID string) ([]SessionMessagePreview, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("session id is required")
	}
	// A group user message may carry the @ protocol, including `private` (密送)
	// notes. The preview is a human-readable line shown in the share-selection
	// dialog, so the protocol is stripped for a group session — a note addressed
	// to one member must not be previewed to everyone. A single chat has no
	// protocol, so its text stays verbatim.
	isGroup := GetSessionType(sessionID) == groupSessionType
	rows, err := store.ReadDB().Query(
		`SELECT id, role, content, streaming, created_at, COALESCE(agent_id, '') FROM chat_history
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
			createdAt string
			agentID   string
		)
		if err := rows.Scan(&id, &role, &content, &streaming, &createdAt, &agentID); err != nil {
			return nil, fmt.Errorf("scan session message for selection: %w", err)
		}
		preview := ExtractPlainText(content)
		if isGroup {
			preview = grouprouting.StripProtocolTags(preview)
		} else {
			// Single chat: drop the auto-cruise completion signal so it does not
			// show up as literal text in the share-selection preview. Same
			// single-chat policy as AssistantConclusion.
			preview = grouprouting.StripGoalSatisfied(preview)
		}
		items = append(items, SessionMessagePreview{
			ID:        id,
			Role:      role,
			Preview:   clipRunes(preview, sessionSharePreviewRunes),
			Streaming: streaming != 0,
			CreatedAt: createdAt,
			AgentID:   agentID,
		})
	}
	return items, rows.Err()
}

// ResolveSessionShareSelection validates a client's message selection against
// the session's finalized messages and returns the RESOLVED id list: non-empty,
// chronological, de-duplicated.
//
// Strict on purpose — this is the CREATION path. An id that names no finalized
// message is an error (the share must match what the user believed they
// selected). An empty selection means "every finalized message", which is
// resolved here into the explicit list so the stored share cannot grow later.
func ResolveSessionShareSelection(sessionID string, messageIDs []int64) ([]int64, error) {
	finalized, err := GetFinalizedMessageIDs(sessionID)
	if err != nil {
		return nil, err
	}
	if len(finalized) == 0 {
		return nil, ErrSessionShareEmptySelection
	}
	if len(messageIDs) == 0 {
		return finalized, nil
	}

	known := make(map[int64]struct{}, len(finalized))
	for _, id := range finalized {
		known[id] = struct{}{}
	}
	seen := make(map[int64]struct{}, len(messageIDs))
	selected := make([]int64, 0, len(messageIDs))
	for _, id := range messageIDs {
		if _, ok := known[id]; !ok {
			return nil, fmt.Errorf("%w: message %d", ErrSessionShareUnknownMessage, id)
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		selected = append(selected, id)
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i] < selected[j] })
	return selected, nil
}

// BuildSessionSharePayload assembles the public JSON for one request.
//
// messageIDs is the stored selection. Ids that no longer resolve (rewound or
// hard-deleted) are SKIPPED rather than failing the request: a link keeps
// working after the conversation is trimmed, it just shows less. When nothing
// survives, ErrSessionShareNothingToShow is returned (the handler 404s).
//
// createdAt is the share row's timestamp, NOT time.Now(): the assembled bytes
// have to be stable across requests or the ETag derived from them never matches.
//
// NOTE: paths are NOT rewritten. The old sanitizer replaced the project root with
// "./" and the home directory with "~/", but it could only ever be cosmetic —
// a tool call's output routinely carries absolute paths, credentials and file
// contents the string replacement never looked at (a single `Read` of a .env
// leaks more than the whole path rewrite ever hid). Rewriting some of them made
// the page look sanitized while it was not, so the transcript is now served
// verbatim and the reader is trusted with what the creator chose to share.
func BuildSessionSharePayload(sessionID string, messageIDs []int64, createdAt time.Time) (string, int, error) {
	// Archived sessions must still resolve: archiving keeps every message, and a
	// share deliberately outlives it.
	info := GetSessionFullInfoAny(sessionID)
	if info == nil {
		return "", 0, ErrSessionShareSessionGone
	}

	// Loads only the selected rows (chunked), so sharing three messages of a
	// 300-message conversation does not read 297 unused content blobs.
	messages, err := GetMessagesByIDsInSession(sessionID, messageIDs)
	if err != nil {
		return "", 0, fmt.Errorf("load session messages: %w", err)
	}
	if len(messages) == 0 {
		return "", 0, ErrSessionShareNothingToShow
	}

	selectedIDs := messageIDsOf(messages)
	// The gaps between selected messages can only be measured against the
	// session's full chronological id list — the payload itself contains just
	// the selection, so the viewer could never reconstruct them.
	//
	// Failure is NOT fatal: the share is still perfectly readable without
	// dividers, and refusing to serve a working link because a secondary
	// annotation query failed would be a worse trade. The count is left at zero
	// for every message in that case (no dividers drawn).
	allIDs, allIDsErr := GetFinalizedMessageIDs(sessionID)
	if allIDsErr != nil {
		allIDs = nil
	}
	toolCalls, err := loadToolCallsByMessage(selectedIDs)
	if err != nil {
		return "", 0, err
	}
	thinking, err := loadThinkingByMessage(selectedIDs)
	if err != nil {
		return "", 0, err
	}
	summaries, cards, err := loadSummariesForMessages(selectedIDs)
	if err != nil {
		return "", 0, err
	}

	// The viewer has no other way to resolve a speaker's custom avatar (it cannot
	// call /api/agents), so every identity carries it. resolveSessionSpeakersWithInfo
	// is used instead of the exported wrapper because it must accept an ARCHIVED
	// session — see GetSessionFullInfoAny.
	sessionAgent, speakers := resolveSessionSpeakersWithInfo(info, sessionID)

	// The viewer's topbar renders the same avatar stack the in-app group header
	// shows: the SPEAKING roster in roster order with the host flagged.
	// GroupMembersForGroups is the exact source the session list uses. Best-effort:
	// a failure must not fail the share (the topbar then falls back to the single
	// session-agent icon).
	//
	// Read once and reused for SessionType: the payload must report the type even
	// when the roster comes back empty (every member disabled), so the viewer cannot
	// use "roster is non-empty" as the group signal.
	sessionType := GetSessionType(sessionID)
	var groupMembers []model.GroupMemberPreview
	if sessionType == groupSessionType {
		if byGroup, gmErr := GroupMembersForGroups([]string{sessionID}); gmErr == nil {
			groupMembers = byGroup[sessionID]
		}
	}

	out := SessionSharePayload{
		Version:   sessionSharePayloadVersion,
		CreatedAt: createdAt.UTC(),
		Session: SessionShareSession{
			Title:        info.Title,
			Backend:      info.Backend,
			AgentID:      info.AgentID,
			Model:        info.Model,
			HostMemberID: GetGroupHostMember(sessionID),
			UserNickname: groupUserTarget(),
			SessionType:  sessionType,
		},
		Messages:     make([]SessionShareMessage, 0, len(messages)),
		SessionAgent: sessionAgent,
		Speakers:     speakers,
		GroupMembers: groupMembers,
	}
	skipped := skippedCountsFor(allIDs, messages)
	for i := range messages {
		item := buildShareMessage(messages[i], toolCalls, thinking, summaries, cards)
		item.SkippedBefore = skipped[i]
		out.Messages = append(out.Messages, item)
	}

	raw, err := json.Marshal(out)
	if err != nil {
		return "", 0, fmt.Errorf("marshal session share payload: %w", err)
	}
	raw = trimPayloadToCap(raw, &out)
	return string(raw), len(out.Messages), nil
}

// buildShareMessage prepares one message: its content is rewritten with the full
// tool/thinking payload inlined. An assistant turn additionally carries its
// reading summary and cards.
func buildShareMessage(
	msg model.ChatMessage,
	toolCalls map[toolCallKey]ToolCallRecord,
	thinking map[thinkingKey]string,
	summaries map[int64]string,
	cards map[int64]*model.SummaryCards,
) SessionShareMessage {
	// Assistant content is a JSON wrapper; rewrite it with the full payload
	// inlined. User content may be plain text or the same wrapper, so it goes
	// through the same path and falls back to itself when it is not JSON.
	item := SessionShareMessage{
		ID:        msg.ID,
		Role:      msg.Role,
		Content:   inlineMessageContent(msg, toolCalls, thinking),
		AgentID:   msg.AgentID,
		CreatedAt: msg.CreatedAt,
	}
	if len(msg.Files) > 0 {
		item.Files = msg.Files
	}
	if msg.Role != roleAssistant {
		return item
	}
	if s, ok := summaries[msg.ID]; ok {
		item.Summary = &s
	}
	if c, ok := cards[msg.ID]; ok {
		item.SummaryCards = prepareSummaryCards(c)
	}
	return item
}

// skippedCountsFor reports, for each selected message, how many of the session's
// other finalized messages fall strictly between it and the previously selected
// one. The first entry is always 0 (nothing precedes the start of the share, so
// there is nothing to divide it from).
//
// `messages` must be in ascending id order, which GetMessagesByIDsInSession
// guarantees. allIDs is the session's full chronological id list; when it is
// nil or does not contain the selected ids (the lookup failed), every count is
// zero and no dividers are drawn — the transcript is still correct, just
// without the annotation.
func skippedCountsFor(allIDs []int64, messages []model.ChatMessage) []int {
	out := make([]int, len(messages))
	if len(messages) == 0 || len(allIDs) == 0 {
		return out
	}

	// position[id] = index in the session's finalized order. Built from allIDs
	// rather than assumed from id arithmetic: ids are not contiguous (other
	// sessions' inserts interleave, deletions leave holes), so `id2 - id1 - 1`
	// would report a gap where there is none.
	position := make(map[int64]int, len(allIDs))
	for i, id := range allIDs {
		position[id] = i
	}

	prevPos := -1
	for i, m := range messages {
		pos, ok := position[m.ID]
		if !ok {
			// The message is finalized but absent from allIDs — only possible if
			// the two queries raced a delete. Treat it as contiguous with the
			// previous one rather than inventing a gap, and keep prevPos so the
			// next message still measures from the last known position.
			continue
		}
		if prevPos >= 0 && pos > prevPos {
			out[i] = pos - prevPos - 1
		}
		prevPos = pos
	}
	return out
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

// loadToolCallsByMessage returns the tool calls of the given messages, keyed by
// message+tool.
func loadToolCallsByMessage(messageIDs []int64) (map[toolCallKey]ToolCallRecord, error) {
	records, err := GetToolCallsByMessageIDs(messageIDs)
	if err != nil {
		return nil, fmt.Errorf("load session tool calls: %w", err)
	}
	out := make(map[toolCallKey]ToolCallRecord, len(records))
	for _, r := range records {
		out[toolCallKey{messageID: r.MessageID, toolID: r.ToolID}] = r
	}
	return out, nil
}

// loadThinkingByMessage returns the thinking text of the given messages, keyed
// by message+think_id. GetThinkingByMessageIDs already concatenates seq chunks.
func loadThinkingByMessage(messageIDs []int64) (map[thinkingKey]string, error) {
	records, err := GetThinkingByMessageIDs(messageIDs)
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
// of which are wrong here — the payload needs the full content AND the summary
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

	rows, err := store.ReadDB().Query(query, args...)
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

// inlineMessageContent rewrites a message's stored content so the payload is
// self-contained: tool_use blocks regain their input/output and thinking blocks
// regain their text.
//
// Works on map[string]any on purpose. Round-tripping through model.ContentBlock
// would re-apply the slimming MarshalJSON and drop exactly the fields being
// restored here.
//
// Content is passed through VERBATIM otherwise — see the note on
// BuildSessionSharePayload for why paths are no longer rewritten.
func inlineMessageContent(msg model.ChatMessage, toolCalls map[toolCallKey]ToolCallRecord, thinking map[thinkingKey]string) string {
	trimmed := strings.TrimSpace(msg.Content)
	if !strings.HasPrefix(trimmed, "{") {
		// Plain-text user message (or unparseable content) — keep as-is.
		return msg.Content
	}

	var wrapper map[string]any
	if err := json.Unmarshal([]byte(trimmed), &wrapper); err != nil {
		return msg.Content
	}

	blocksRaw, ok := wrapper["blocks"]
	if !ok {
		return msg.Content
	}
	blocks, ok := blocksRaw.([]any)
	if !ok {
		// {"blocks":null} and friends: leave the wrapper as-is.
		return msg.Content
	}

	for _, b := range blocks {
		block, ok := b.(map[string]any)
		if !ok {
			continue
		}
		inlineShareBlock(block, msg.ID, toolCalls, thinking)
	}

	out, err := json.Marshal(wrapper)
	if err != nil {
		return msg.Content
	}
	return string(out)
}

// inlineShareBlock restores one content block's heavy fields (tool input/output,
// thinking text) from the side tables, in place.
func inlineShareBlock(
	block map[string]any,
	messageID int64,
	toolCalls map[toolCallKey]ToolCallRecord,
	thinking map[thinkingKey]string,
) {
	switch block["type"] {
	case eventTypeToolUse:
		inlineToolUseBlock(block, messageID, toolCalls)
	case blockTypeThinking:
		thinkID, _ := block["think_id"].(string)
		if thinkID == "" {
			return
		}
		if text, found := thinking[thinkingKey{messageID: messageID, thinkID: thinkID}]; found {
			block["text"] = text
		}
	}
}

// inlineToolUseBlock restores a tool_use block's heavy payload from the side
// table. The stored block already carries name/status/done/summary; the side
// table is authoritative for input/output and for status, which the block may
// not have received if the tool finished after the message was persisted.
//
// Blocks WITHOUT a side-table row keep whatever they already carry: the
// interactive tools (AskUserQuestion / PermissionApproval) are stored with their
// `input` inline (model.ContentBlock.MarshalJSON) and never get a
// chat_tool_calls row, so returning early on !found would drop the card's
// arguments entirely.
func inlineToolUseBlock(
	block map[string]any,
	messageID int64,
	toolCalls map[toolCallKey]ToolCallRecord,
) {
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
			block["input"] = input
		}
	}
	if rec.Output != "" {
		block["output"] = rec.Output
	}
	if rec.Status != "" {
		block["status"] = rec.Status
	}
	block["done"] = rec.Done
	if rec.DurationMs > 0 {
		block["duration_ms"] = rec.DurationMs
	}
	if rec.Summary != "" {
		block["summary"] = rec.Summary
	}
}

// prepareSummaryCards prepares summary cards for the payload.
//
// TaskIDs are dropped: scheduled-task cards are out of scope for the share page
// (the viewer cannot reach /api/tasks, and a task card would render as a
// permanent "loading" skeleton). Everything else the summary view needs —
// tools, warnings, ask-questions, file changes — is kept verbatim.
func prepareSummaryCards(cards *model.SummaryCards) *model.SummaryCards {
	if cards == nil {
		return nil
	}
	out := *cards
	out.TaskIDs = nil
	return &out
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
			return raw // re-marshaling failed: keep the last good payload
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
// payload, while truncated=true was discarded along with the mutation.
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
// caller must keep its last good payload rather than serving a truncated one.
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
