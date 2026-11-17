package service

import (
	"strings"

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
// backend.
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
// Only text is carried. Tool calls are omitted on purpose: their results can be
// large file bodies the model has already moved past, and replaying them would
// spend the context window on transcripts the reply already summarizes.
//
// The budget is bounded by runes so a long session cannot overflow the model's
// window. Older turns are dropped from the FRONT (the most recent turns are what
// the next reply depends on), and the cut is announced so the model does not
// assume the conversation started mid-sentence.
func buildBuiltinHistory(sessionID string, beforeMessageID int64, summary builtinSummary) []ai.HistoryMessage {
	if sessionID == "" {
		return nil
	}
	messages, err := GetMessagesBySessionIDRaw(sessionID)
	if err != nil || len(messages) == 0 {
		return nil
	}

	type turn struct {
		role string
		text string
		// id is the chat_history row id, carried so a compaction can report WHICH
		// stored turn it folded (see builtinSummaryFromCompaction).
		id int64
	}
	var turns []turn

	// keptTurns is the number of REPLAYABLE turns seen so far, and it is the
	// list index the stored boundary counts against. It is an explicit counter
	// rather than len(turns) because the two are not the same number here — and
	// because "which list is being counted" is the whole content of this
	// function, so it must be readable at the point of use:
	//
	//   - turns only receives entries that pass every filter below, while
	//     keptTurns also counts the rows the summary position was measured on.
	//     On a session whose stored form contains anything the replay drops, the
	//     two diverge — and the boundary belongs to the count, not to len(turns).
	//   - turns is appended to AFTER the boundary has been consulted, so the
	//     number means "the first N replayed turns are covered", not "the N-th
	//     entry is" — an off-by-one here either re-summarises one turn or drops
	//     it from the model's context for good.
	//
	// The two counters advance together at the bottom of the loop (every row the
	// summary has not covered is, by definition, part of the replayed list) and
	// apart inside the covered branch above.
	keptTurns := 0
	for _, m := range messages {
		// Exclude this turn: the user message being answered is already in the
		// prompt, and anything newer is not part of the context yet.
		if beforeMessageID > 0 && m.ID >= beforeMessageID {
			continue
		}
		// Exclude everything the stored summary already stands for.
		//
		// The boundary is taken by POSITION when the stored summary knows it
		// (see builtinSummary.CoversThroughTurn). The id-based boundary is a
		// snapshot of the table taken at compaction time, and deleting a message
		// or rewinding the session shifts ids — it would then skip turns the
		// engine deliberately kept verbatim, and the model loses them while the
		// summary does not cover them either.
		//
		// The position counts REPLAYABLE turns only (zero-based, exactly like the
		// engine's Spec.History): the boundary the caller stores is "the first N
		// of these", so both sides must count the same list the same way.
		if summary.CoversThroughTurn > 0 {
			if keptTurns < summary.CoversThroughTurn {
				keptTurns++
				continue
			}
		} else if summary.UpToMessageID > 0 && m.ID <= summary.UpToMessageID {
			continue
		}
		role := m.Role
		if role != roleUser && role != roleAssistant {
			continue
		}
		text := strings.TrimSpace(ExtractPlainText(m.Content))
		if text == "" {
			continue
		}
		keptTurns++
		turns = append(turns, turn{role: role, text: text, id: m.ID})
	}
	if len(turns) == 0 {
		return nil
	}

	// Walk backwards accumulating runes until the budget is spent, then emit
	// the kept tail in order.
	budget := builtinHistoryBudgetChars
	start := len(turns)
	for i := len(turns) - 1; i >= 0; i-- {
		n := len([]rune(turns[i].text))
		if n > budget {
			// A single oversized message is truncated rather than dropped: it
			// is the turn the user is most likely asking about.
			if start == len(turns) {
				turns[i].text = truncateRunes(turns[i].text, budget)
				start = i
			}
			break
		}
		budget -= n
		start = i
	}

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
		out = append(out, ai.HistoryMessage{Role: t.role, Text: t.text, ID: t.id})
	}
	return out
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
	last := event.CompactedThroughHistory
	if last < 0 || last >= len(history) {
		return s
	}
	s.CoversThroughTurn = last + 1
	if id := event.CompactedThroughMessageID; id > 0 {
		s.UpToMessageID = id
	} else {
		// The turn's own id is the same fact. An entry without one (the fork
		// context header) leaves the id unset, which is harmless: the position is
		// what the next turn uses.
		s.UpToMessageID = history[last].ID
	}
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

// builtinHistoryBudgetChars bounds the replayed history.
//
// 40k runes is roughly 20-30k tokens of Chinese/English prose — comfortably
// inside every current model's window while leaving room for the system prompt,
// the tool results of the current turn, and the reply itself.
const builtinHistoryBudgetChars = 40000

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
