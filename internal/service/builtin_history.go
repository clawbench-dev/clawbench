package service

import (
	"encoding/json"
	"strings"

	"clawbench/internal/agentcore"
	"clawbench/internal/agentcore/compaction"
	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/store"
)

// builtinBackendID mirrors builtin.BackendID.
//
// Duplicated as a constant rather than imported so this package does not depend
// on the backend plugin (which imports ai and model, and would create a cycle
// once it needs a service seam). A drift test pins the two together.
const builtinBackendID = "builtin"

// isBuiltinBackend reports whether backendID is the in-process built-in agent.
func isBuiltinBackend(backendID string) bool {
	return backendID == builtinBackendID
}

// buildBuiltinHistory renders a session's prior turns for a self-hosted
// backend, using the fallback budget for models whose window is unknown.
//
// Every external backend keeps its own transcript and is resumed by session id,
// so replaying ours would duplicate the conversation. The built-in backend has
// no such transcript: it runs in this process, so ClawBench's own history IS its
// context. That is why this is filled here (service owns the DB) and carried on
// ChatRequest.History.
//
// beforeMessageID excludes this turn's own user message, which is persisted
// BEFORE the request is built. Without the boundary the prompt appears twice —
// once from history, once as the prompt — and the model answers a question it
// was asked twice, which reads as a doubled instruction.
//
// summary is the stored compaction state. When it covers earlier messages, only
// the messages AFTER its boundary are replayed: the summary already stands for
// the rest, and replaying them too would put back exactly the tokens compaction
// removed — and make the next turn compact again, forever.
//
// Tool calls ride along. Dropping them was the root of the 2026-11-20
// hallucination: the model saw a transcript of its own claims ("I changed the
// file", "tests pass") with no tool record to contradict it, so it continued
// the fiction. Inputs and outputs are truncated per call so one large file read
// cannot eat the whole budget.
func buildBuiltinHistory(sessionID string, beforeMessageID int64, summary builtinSummary) []ai.HistoryMessage {
	return buildBuiltinHistoryWithBudget(sessionID, beforeMessageID, summary, builtinHistoryFallbackTokens)
}

// buildBuiltinHistoryWithBudget is buildBuiltinHistory with an explicit token
// budget. Zero means unlimited: the engine compacts when the model's window is
// known, and the caller should not also drop turns (that is the silent loss this
// package was written to stop).
//
// The budget is a TOKEN estimate, not a character count: "40k runes" means
// roughly 20k tokens of Chinese and 10k of English, so the same constant gave
// the model wildly different amounts of history depending on the language.
func buildBuiltinHistoryWithBudget(sessionID string, beforeMessageID int64, summary builtinSummary, budgetTokens int) []ai.HistoryMessage {
	if sessionID == "" {
		return nil
	}
	messages, err := GetMessagesBySessionIDRaw(sessionID)
	if err != nil || len(messages) == 0 {
		return nil
	}

	turns := collectBuiltinTurns(messages, beforeMessageID, summary, builtinToolCalls(messages))
	if len(turns) == 0 {
		return nil
	}

	start := builtinHistoryStart(turns, budgetTokens)
	out := make([]ai.HistoryMessage, 0, len(turns)-start)
	if start > 0 {
		// Tell the model that earlier turns exist but were omitted, so it does
		// not treat the visible history as the whole conversation.
		out = append(out, ai.HistoryMessage{
			Role: roleUser,
			Text: "[Earlier conversation omitted to fit the context window. " +
				"The visible messages continue from here.]",
		})
	}
	for _, t := range turns[start:] {
		out = append(out, ai.HistoryMessage{
			Role:  t.role,
			Text:  t.text,
			ID:    t.id,
			Turn:  t.turn,
			Tools: t.tools,
			Files: t.files,
		})
	}
	return out
}

// builtinTurn is one replayable turn: its role, its flattened text, the
// completed tool calls it made, and the chat_history row id it came from.
type builtinTurn struct {
	role string
	text string
	// tools are the completed calls this assistant turn made, in request order.
	tools []ai.HistoryToolCall
	// id is the chat_history row id, carried so a compaction can report WHICH
	// stored turn it folded (see builtinSummaryFromCompaction).
	id int64
	// turn is this entry's zero-based position in the replayable list. It maps
	// the engine's boundary (an index into the EXPANDED message list) back to
	// the stored turn the summary actually covers.
	turn int
	// files are the turn's attachments. The stored prompt names them, but the
	// backend needs the paths to send an attached image as a picture.
	files []model.FileEntry
}

// collectBuiltinTurns filters a session's stored messages down to the turns the
// built-in backend replays.
//
// keptTurns is the number of REPLAYABLE turns seen so far, and it is the list
// index the stored boundary counts against. It is an explicit counter rather
// than len(turns) because the two are not the same number here — and because
// "which list is being counted" is the whole content of this function, so it
// must be readable at the point of use:
//
//   - turns only receives entries the summary has not covered, while keptTurns
//     also counts the turns it did cover. On a session whose stored form
//     contains anything the replay drops, the two diverge — and the boundary
//     belongs to the count, not to len(turns).
//   - the count advances BEFORE the append, so a turn's Turn field is its
//     position in the replayable list, not its position among the kept turns.
//     An off-by-one here either re-summarizes one turn or drops it from the
//     model's context for good.
//
// A turn is replayable when it has text OR completed tool calls. The second
// half is new and matters: an assistant turn that only called tools has no
// text, and skipping it was what left the model with a prose-only record of its
// own work.
func collectBuiltinTurns(messages []model.ChatMessage, beforeMessageID int64, summary builtinSummary, tools map[int64][]ai.HistoryToolCall) []builtinTurn {
	var turns []builtinTurn
	keptTurns := 0
	for _, m := range messages {
		// Exclude this turn: the user message being answered is already in the
		// prompt, and anything newer is not part of the context yet.
		if beforeMessageID > 0 && m.ID >= beforeMessageID {
			continue
		}
		role := m.Role
		if role != roleUser && role != roleAssistant {
			continue
		}
		text := strings.TrimSpace(ExtractPlainText(m.Content))
		if role == roleAssistant {
			// A sub-agent's text lives in the same message with
			// parent_tool_call_id set. Flattening it into the assistant's text
			// would put it back into the parent's context — and duplicate the
			// report, which also rides the task tool result. The child's work
			// belongs to the task card, not to the parent's voice.
			text = strings.TrimSpace(extractTopLevelText(m.Content))
		}
		var calls []ai.HistoryToolCall
		if role == roleAssistant {
			calls = tools[m.ID]
		}
		if text == "" && len(calls) == 0 {
			continue
		}

		// The turn's position in the replayable list, counted whether or not the
		// summary covers it. It is what maps an expanded-message boundary back
		// to a stored turn.
		position := keptTurns
		keptTurns++

		// Exclude everything the stored summary already stands for.
		//
		// The boundary is taken by POSITION when the stored summary knows it
		// (see builtinSummary.CoversThroughTurn). The id-based boundary is a
		// snapshot of the table taken at compaction time, and deleting a message
		// or rewinding the session shifts ids — it would then skip turns the
		// engine deliberately kept verbatim, and the model loses them while the
		// summary does not cover them either.
		if summary.CoversThroughTurn > 0 {
			if position < summary.CoversThroughTurn {
				continue
			}
		} else if summary.UpToMessageID > 0 && m.ID <= summary.UpToMessageID {
			continue
		}

		turns = append(turns, builtinTurn{
			role:  role,
			text:  text,
			tools: calls,
			id:    m.ID,
			turn:  position,
			files: m.Files,
		})
	}
	return turns
}

// builtinToolCalls loads the completed tool calls of every assistant message,
// keyed by message id and ordered as they were requested.
//
// Only rows marked done are included: an unanswered tool_use is rejected by both
// wire protocols, and a cancelled call has no result to replay. Synthetic blocks
// (the AskUserQuestion card is built from a text tag after the turn) have no row
// here, so they are naturally excluded.
func builtinToolCalls(messages []model.ChatMessage) map[int64][]ai.HistoryToolCall {
	ids := make([]int64, 0, len(messages))
	for _, m := range messages {
		if m.Role == roleAssistant {
			ids = append(ids, m.ID)
		}
	}
	records, err := GetToolCallsByMessageIDs(ids)
	if err != nil || len(records) == 0 {
		return nil
	}
	// Child tool calls are stored in the same table (it has no parent column),
	// so the message content blocks are the only place that says which calls
	// belong to a sub-agent. Replaying them as the parent's own would put the
	// child's reads back into the parent's context — exactly what delegation is
	// meant to avoid.
	childCalls := childToolCallIDs(messages)
	out := make(map[int64][]ai.HistoryToolCall, len(records))
	for _, r := range records {
		if !r.Done || strings.TrimSpace(r.Name) == "" {
			continue
		}
		if childCalls[r.ToolID] {
			continue
		}
		out[r.MessageID] = append(out[r.MessageID], ai.HistoryToolCall{
			ID:    r.ToolID,
			Name:  r.Name,
			Input: truncateRunes(string(r.Input), builtinToolInputMaxRunes),
			// A task call's output is the child's report — the deliverable,
			// not a file body — so it keeps the larger cap.
			Output:  truncateRunes(r.Output, builtinOutputCap(r.Name)),
			IsError: strings.EqualFold(r.Status, "error"),
		})
	}
	return out
}

// extractTopLevelText renders the assistant's own text, excluding blocks
// produced by a sub-agent.
//
// It falls back to the legacy extractor for content that is not the
// {"blocks":[...]} shape: those rows predate block storage and have no parent
// attribution to honor.
func extractTopLevelText(content string) string {
	blocks, ok := parseContentBlocks(content)
	if !ok {
		return ExtractPlainText(content)
	}
	texts := make([]string, 0, len(blocks))
	for _, b := range blocks {
		if b.Type != contentKeyText || b.ParentToolCallID != "" {
			continue
		}
		if s := strings.TrimSpace(b.Text); s != "" {
			texts = append(texts, s)
		}
	}
	return strings.Join(texts, "\n\n")
}

// childToolCallIDs collects the ids of tool calls that belong to a sub-agent,
// read from the message content blocks. The tool-call table has no parent
// column, so this is the authoritative link.
func childToolCallIDs(messages []model.ChatMessage) map[string]bool {
	out := make(map[string]bool)
	for _, m := range messages {
		if m.Role != roleAssistant {
			continue
		}
		blocks, ok := parseContentBlocks(m.Content)
		if !ok {
			continue
		}
		for _, b := range blocks {
			if b.Type == eventTypeToolUse && b.ID != "" && b.ParentToolCallID != "" {
				out[b.ID] = true
			}
		}
	}
	return out
}

// parseContentBlocks decodes the {"blocks":[...]} shape. It reports false for
// anything else, so callers can fall back to the legacy text extractor.
func parseContentBlocks(content string) ([]model.ContentBlock, bool) {
	if content == "" {
		return nil, false
	}
	var wrapper struct {
		Blocks []model.ContentBlock `json:"blocks"`
	}
	if err := json.Unmarshal([]byte(content), &wrapper); err != nil {
		return nil, false
	}
	if wrapper.Blocks == nil {
		return nil, false
	}
	return wrapper.Blocks, true
}

// Per-call caps. They keep one large file read or one large write from eating
// the whole history budget; the model needs to know WHAT happened, not the full
// body of a file it already worked on.
const (
	builtinToolInputMaxRunes  = 1000
	builtinToolOutputMaxRunes = 2000
	// builtinTaskToolName is agentcore's delegation tool. Its output is the
	// sub-agent's report — the deliverable, not a file body — so it gets its
	// own cap below.
	builtinTaskToolName = "task"
)

// builtinOutputCap picks the output cap for one replayed call.
func builtinOutputCap(name string) int {
	if strings.EqualFold(name, builtinTaskToolName) {
		return agentcore.MaxSubagentReportRunes
	}
	return builtinToolOutputMaxRunes
}

// builtinHistoryStart returns the index of the first turn that fits the budget.
//
// It walks backwards accumulating TOKENS until the budget is spent: the kept
// tail is what the next reply depends on. A single oversized message is
// truncated rather than dropped when it would otherwise leave nothing — it is
// the turn the user is most likely asking about.
//
// budgetTokens <= 0 means unlimited. That is the normal case when the model's
// window is known: the engine compacts, and a second silent cut here would drop
// turns the summary never mentioned.
func builtinHistoryStart(turns []builtinTurn, budgetTokens int) int {
	if budgetTokens <= 0 {
		return 0
	}
	budget := budgetTokens
	start := len(turns)
	for i := len(turns) - 1; i >= 0; i-- {
		n := builtinTurnTokens(turns[i])
		if n > budget {
			if start == len(turns) {
				turns[i].text = truncateRunes(turns[i].text, budget)
				start = i
			}
			break
		}
		budget -= n
		start = i
	}
	return start
}

// builtinTurnTokens estimates one turn's size, tool evidence included.
func builtinTurnTokens(t builtinTurn) int {
	n := compaction.EstimateTokens(t.text)
	for _, c := range t.tools {
		n += compaction.EstimateTokens(c.Name) + compaction.EstimateTokens(c.Input) + compaction.EstimateTokens(c.Output)
	}
	return n
}

// builtinSummaryFromCompaction turns a compaction event into the boundary to
// persist alongside its summary.
//
// The boundary is what the ENGINE folded, not "everything finalized so far".
// The engine deliberately keeps the most recent turns verbatim, and those are
// already-persisted rows with ids — the old "newest id" boundary marked them
// covered while the summary said nothing about them, so the next turn silently
// dropped the turns the model had just been told to keep in mind.
//
// Two boundaries are stored because they fail differently. The position
// (CoversThroughTurn) is measured on the replayed turn list and therefore
// survives a deleted message or a rewind; the id is the older form and is kept
// so a summary written by an older build still resolves. When the event names
// nothing usable, NO boundary is written at all: the next turn replays the
// history and compacts again — one redundant summary call, never lost content.
func builtinSummaryFromCompaction(event ai.StreamEvent, history []ai.HistoryMessage) builtinSummary {
	s := builtinSummary{Text: event.Summary}

	// Prefer the stored id: one assistant turn with tool calls expands into two
	// replayed messages, so the engine's index no longer lines up with this
	// list (it can legitimately be past the end). The id is duplicated on both
	// messages, which makes it the stable handle — and the Turn field then says
	// which STORED turn it was.
	if id := event.CompactedThroughMessageID; id > 0 {
		for i := range history {
			if history[i].ID != id {
				continue
			}
			s.CoversThroughTurn = history[i].Turn + 1
			s.UpToMessageID = id
			return s
		}
		// The engine named an id this caller cannot explain. Mapping the index
		// instead would be a guess, and a wrong guess silently drops stored
		// turns, so write nothing and replay everything next turn.
		return s
	}

	// No id: the boundary landed on a synthetic entry (the fork-context header)
	// or the caller keeps no ids at all. The index is then the only handle.
	last := event.CompactedThroughHistory
	if last < 0 || last >= len(history) {
		return s
	}
	s.CoversThroughTurn = history[last].Turn + 1
	// The turn's own id is the same fact. An entry without one (the fork
	// context header) leaves the id unset, which is harmless: the position is
	// what the next turn uses.
	s.UpToMessageID = history[last].ID
	return s
}

// lastCoveredMessageID returns the highest chat_history id at or below
// beforeMessageID, i.e. the boundary a summary written now would cover.
//
// The engine's summary replaces the messages it was given, but it does not know
// their database ids (it only sees the conversation). This is how the caller
// turns "I compacted the first N messages" into a durable boundary.
func lastCoveredMessageID(sessionID string, beforeMessageID int64) int64 {
	if sessionID == "" || !store.ReadDBReady() {
		return 0
	}
	query := "SELECT COALESCE(MAX(id), 0) FROM chat_history WHERE session_id = ? AND streaming = 0"
	args := []any{sessionID}
	if beforeMessageID > 0 {
		query += " AND id < ?"
		args = append(args, beforeMessageID)
	}
	var id int64
	if err := store.ReadDB().QueryRow(query, args...).Scan(&id); err != nil {
		return 0
	}
	return id
}

// builtinHistoryFallbackTokens bounds the replayed history when the model's
// context window is unknown.
//
// It is a TOKEN budget, not a rune count: 40k runes used to mean ~20k tokens of
// Chinese and ~10k of English, so the same constant gave the model wildly
// different amounts of history depending on the language. 40k tokens is roughly
// twice the old Chinese capacity and still comfortably inside a 64k+ window.
//
// It is only a safety net. When the window IS known the engine compacts, and
// the caller passes 0 (unlimited) rather than cutting turns the summary never
// mentioned.
const builtinHistoryFallbackTokens = 40000

// builtinHistoryEnabled reports whether the backend needs a replayed history.
//
// Kept as a function (rather than inlined at the call site) so the rule lives
// next to buildBuiltinHistory and a future self-hosted backend only has to be
// added here.
func builtinHistoryEnabled(backendName string) bool {
	return isBuiltinBackend(backendName)
}

// compile-time reference so an unused-import failure cannot hide a future
// removal of the model dependency this file's budget logic relies on.
var _ = model.Agent{}
