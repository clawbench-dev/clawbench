package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"clawbench/internal/model"
)

// Fork/rewind sessions copy conversation history into ClawBench's own DB but
// start a fresh AI-side session, so the history has to be re-injected as text
// prepended to the next prompt. A long conversation would otherwise blow past
// the model's context window, so injection is bounded by a character budget:
// recent messages are kept verbatim, older ones are dropped, and a notice tells
// the model that compression happened (silence would make it think history was
// lost and re-ask questions it already answered).
//
// Only structural trimming (L0) and a tail window (L1) are applied — no LLM
// summarization. See docs/spec for the configurable budget
// (chat.fork_context_budget).

// defaultForkContextBudgetChars mirrors model.DefaultForkContextBudget for the
// case where the global was never populated (tests, early startup). Kept in
// sync with model.DefaultForkContextBudget.
const defaultForkContextBudgetChars = model.DefaultForkContextBudget

// forkToolInputValueMaxRunes gates L0 input trimming: a strip-key value is only
// replaced when it exceeds this many runes. Threshold-gating keeps ordinary
// inputs ({"command":"ls"}, {"file_path":"..."}) byte-identical, so trimming is
// deterministic and does not depend on whether the budget was exceeded.
const forkToolInputValueMaxRunes = 200

// forkTruncatedSuffix is the suffix truncateRunes appends. Duplicated here so
// the tail-window budget can reserve room for it; truncateRunes itself is
// shared with tool-output truncation and its behavior must not change.
const forkTruncatedSuffix = "...(truncated)"

// forkToolInputStripKeys are content-valued input keys whose payload is
// typically a whole file body or patch text. They are replaced by an
// "[omitted N chars]" marker, while locator keys (file_path, command, pattern,
// path, query, ...) are preserved so the model still knows what was touched.
var forkToolInputStripKeys = []string{
	"content",
	"old_string",
	"new_string",
	"old_str",
	"new_str",
	"old_text",
	"new_text",
	contentKeyText,
	"code",
	"body",
}

// ForkContextOptions controls how fork history is rendered and bounded. The
// zero value is a valid "unbounded, lowercase roles, no header/footer" config
// once BudgetChars falls back to the default.
type ForkContextOptions struct {
	// Header and Footer wrap the injected history. The handler path uses both
	// to bracket the block; the service (task engine) path uses neither.
	Header, Footer string
	// CapitalizeRoles renders "User"/"Assistant" instead of the raw lowercase
	// role. The handler path capitalizes; the service path does not.
	CapitalizeRoles bool
	// PlainTextFallback recovers text from content that is not a
	// {"blocks":[...]} wrapper via ExtractPlainText. Without it such messages
	// are skipped entirely.
	PlainTextFallback bool
	// BudgetChars bounds the total rendered string in runes. <= 0 falls back to
	// defaultForkContextBudgetChars.
	BudgetChars int
}

// ForkContextMessage is one rendered history entry: a role label and its body.
type ForkContextMessage struct {
	Role string
	Body string
}

// BuildForkContextWithOptions reads a session's history from the DB, renders it,
// and bounds the result to the configured budget.
func BuildForkContextWithOptions(sessionID string, o ForkContextOptions) string {
	messages, err := GetMessagesBySessionIDRaw(sessionID)
	if err != nil || len(messages) == 0 {
		return ""
	}

	// Batch-fetch tool call details for the session (input/output are stored
	// separately in chat_tool_calls, not in content JSON).
	toolCalls, err := GetToolCallsBySession(sessionID)
	if err != nil {
		toolCalls = nil // proceed without tool details; blocks get slim version
	}
	toolCallMap := make(map[string]*ToolCallRecord, len(toolCalls))
	for i := range toolCalls {
		toolCallMap[toolCalls[i].ToolID] = &toolCalls[i]
	}

	return BoundForkContext(RenderForkContextMessages(messages, toolCallMap, o), o)
}

// RenderForkContextMessages converts stored messages into ordered history
// entries (chronological). Text blocks are kept as-is, tool_use blocks are
// rendered as structured JSON with their input trimmed, and thinking/warning/
// error blocks are skipped.
func RenderForkContextMessages(msgs []model.ChatMessage, toolCallMap map[string]*ToolCallRecord, o ForkContextOptions) []ForkContextMessage {
	trimmed := trimToolCallInputs(toolCallMap)
	entries := make([]ForkContextMessage, 0, len(msgs))

	for _, m := range msgs {
		if m.Role != roleUser && m.Role != roleAssistant {
			continue
		}
		role := m.Role
		if o.CapitalizeRoles {
			role = strings.ToUpper(role[:1]) + role[1:]
		}

		var wrapper struct {
			Blocks []model.ContentBlock `json:"blocks"`
		}
		// Unmarshal first rather than gating on a `{"blocks":` prefix: the
		// prefix check only holds because json.Marshal emits sorted keys, and it
		// would silently drop history written with leading whitespace or by an
		// indenting encoder. A nil Blocks slice means "not a block wrapper" —
		// an empty `[]` still counts as one, matching the pre-unification paths.
		if json.Unmarshal([]byte(m.Content), &wrapper) != nil || wrapper.Blocks == nil {
			// Non-block content: recover plain text. The unified extractor
			// prevents nested JSON serializations (bare content arrays, ACP
			// notification wrappers from sync replay) leaking raw JSON.
			if !o.PlainTextFallback {
				continue
			}
			content := ExtractPlainText(m.Content)
			if content == "" {
				continue
			}
			entries = append(entries, ForkContextMessage{Role: role, Body: content})
			continue
		}

		parts := extractMessageParts(wrapper.Blocks, trimmed)
		if len(parts) == 0 {
			continue
		}
		entries = append(entries, ForkContextMessage{Role: role, Body: strings.Join(parts, "\n\n")})
	}
	return entries
}

// BoundForkContext renders entries into the final injected string.
//
// Selection is priority-based rather than a plain trailing window:
//
//   - User entries are chosen first, newest-first, and an entry that does not
//     fit is TRUNCATED rather than dropped — a partial instruction still
//     constrains the model, a missing one does not. Measured against a real
//     installation, user messages average ~51 characters while assistant entries
//     average ~12 KB (they carry the tool_use input/output that dominates the
//     budget), so keeping all of them costs almost nothing.
//   - Assistant entries then fill whatever budget is left, newest-first. They are
//     skipped rather than truncated: their bodies are tool payloads, where a
//     partial excerpt rarely helps.
//
// So "every user message is preserved" holds for every user message that fits;
// a single user message larger than the whole budget is truncated, not dropped.
//
// The returned string is bounded by BudgetChars whenever the budget can hold the
// wrapper (header + notice + footer) plus one rune. Below that floor the wrapper
// alone already exceeds the budget, so only the wrapper is emitted — the model
// still learns that history was compressed. Measured floor: 118 runes without a
// header/footer, 281 with the handler's wrapper. Both config layers keep real
// budgets far above it (PATCH validator rejects < 1, the frontend enforces
// min 1000, the default is 100000).
func BoundForkContext(entries []ForkContextMessage, o ForkContextOptions) string {
	if len(entries) == 0 {
		return ""
	}

	budget := o.BudgetChars
	if budget <= 0 {
		budget = defaultForkContextBudgetChars
	}

	// Reserve the wrapper and the notice's upper bound (as if every entry were
	// omitted), so the notice can never push the rendered string over budget.
	// The over-reservation is at most a few digits.
	reserve := utf8.RuneCountInString(o.Header) +
		utf8.RuneCountInString(o.Footer) +
		utf8.RuneCountInString(forkOmissionNotice(len(entries)))
	avail := budget - reserve
	if avail < 1 {
		// Below the floor: nothing can be selected, and the guards below keep
		// even a truncated entry out so the overflow stays at the wrapper.
		avail = 1
	}

	// chosen maps entry index -> body to render (already truncated when needed).
	// Two passes fill it, so rendering walks entries in index order to restore
	// chronology.
	chosen := selectForkEntries(entries, avail)

	// Everything not kept was dropped.
	omitted := len(entries) - len(chosen)

	var sb strings.Builder
	sb.WriteString(o.Header)
	if omitted > 0 {
		sb.WriteString(forkOmissionNotice(omitted))
	}
	for i := range entries {
		body, ok := chosen[i]
		if !ok {
			continue
		}
		sb.WriteString(entries[i].Role)
		sb.WriteString(": ")
		sb.WriteString(body)
		sb.WriteString("\n\n")
	}
	sb.WriteString(o.Footer)
	return sb.String()
}

// selectForkEntries picks which entries to inject and returns a map from entry
// index to the body to render (already truncated where needed). Rendering walks
// the entries in index order, so chronology is preserved regardless of the order
// in which entries were selected here.
//
// Selection is priority-based rather than a plain trailing window:
//
//   - User entries first, newest-first. One that does not fit is truncated
//     rather than dropped, because a partial instruction still constrains the
//     model while a missing one does not. Each is capped at an equal share of the
//     remaining room so a single oversized message (a pasted log, say) cannot
//     consume the budget and evict every older instruction.
//   - Assistant entries then fill the leftover room, newest-first. They are
//     skipped rather than truncated: their bodies are tool payloads, where a
//     partial excerpt rarely helps.
func selectForkEntries(entries []ForkContextMessage, avail int) map[int]string {
	chosen := make(map[int]string, len(entries))
	used := 0

	// Indices of user entries, newest-first.
	var userIdx []int
	for i := len(entries) - 1; i >= 0; i-- {
		if isUserEntry(entries[i]) {
			userIdx = append(userIdx, i)
		}
	}

	for pos, i := range userIdx {
		e := entries[i]
		if cost := forkEntryCost(e); used+cost <= avail {
			chosen[i] = e.Body
			used += cost
			continue
		}
		room := avail - used - forkEntryOverhead(e)
		if remaining := len(userIdx) - pos; remaining > 1 {
			room /= remaining
		}
		if room <= utf8.RuneCountInString(forkTruncatedSuffix) {
			// Not even a truncated body fits. `used` only grows and every entry
			// has the same overhead, so no older user entry can fit either.
			break
		}
		body := forkTruncateBody(e.Body, room)
		chosen[i] = body
		used += forkEntryOverhead(e) + utf8.RuneCountInString(body)
	}

	// Assistant entries fill whatever budget the user entries left.
	for i := len(entries) - 1; i >= 0; i-- {
		if isUserEntry(entries[i]) {
			continue
		}
		e := entries[i]
		if cost := forkEntryCost(e); used+cost <= avail {
			chosen[i] = e.Body
			used += cost
		}
	}

	// Degenerate case: nothing fit at all, i.e. the session's entries are all
	// oversized assistant replies. Truncate the newest so the model still gets
	// recent context rather than an empty history.
	if len(chosen) == 0 {
		last := len(entries) - 1
		if room := avail - forkEntryOverhead(entries[last]); room > utf8.RuneCountInString(forkTruncatedSuffix) {
			chosen[last] = forkTruncateBody(entries[last].Body, room)
		}
	}

	return chosen
}

// isUserEntry reports whether an entry is a user message.
//
// The role may be capitalized for display (ForkContextOptions.CapitalizeRoles,
// used by the web chat path), so this compares case-insensitively. Matching the
// lowercase role constant directly would classify every entry on that path as an
// assistant and silently invert the priority this file exists to enforce.
func isUserEntry(e ForkContextMessage) bool {
	return strings.EqualFold(e.Role, roleUser)
}

// forkEntryOverhead is the per-entry cost that is not the body: the "role: "
// prefix and the "\n\n" separator written after each entry.
func forkEntryOverhead(e ForkContextMessage) int {
	return utf8.RuneCountInString(e.Role) + 2 + 2
}

// forkEntryCost is the full rendered cost of an entry.
func forkEntryCost(e ForkContextMessage) int {
	return forkEntryOverhead(e) + utf8.RuneCountInString(e.Body)
}

// forkTruncateBody truncates a body to fit bodyRoom runes including the
// truncation suffix. Callers must ensure bodyRoom exceeds the suffix length;
// otherwise truncateRunes' own floor would produce a body longer than the room
// reserved for it, pushing the rendered line over the budget.
func forkTruncateBody(body string, bodyRoom int) string {
	limit := bodyRoom - utf8.RuneCountInString(forkTruncatedSuffix)
	if limit < 1 {
		limit = 1
	}
	return truncateRunes(body, limit)
}

// forkOmissionNotice tells the model that earlier history was compressed, so it
// does not mistake the shortened transcript for the whole conversation. The
// wording deliberately does not promise that the remaining entries are complete:
// the newest entry is truncated when it alone exceeds the budget.
//
// Only the count varies, so forkOmissionNotice(len(entries)) is a valid upper
// bound for reserving budget space before the actual count is known.
func forkOmissionNotice(omitted int) string {
	return fmt.Sprintf(
		"[Note: %d earlier messages were omitted to fit the context window. The messages below are the most recent available.]\n\n",
		omitted,
	)
}

// TrimToolInput replaces oversized content-valued keys in a tool input with an
// "[omitted N chars]" marker, preserving locator keys. Values at or below
// maxValueRunes are left untouched. The walk descends into nested objects and
// arrays, because backends normalize multi-edit payloads into a nested shape
// (e.g. Pi's {"edits":[{"old_string":...}]}) where the bulk lives below the top
// level. The result is always valid JSON when the input was valid JSON;
// non-object or malformed input is returned unchanged.
func TrimToolInput(input json.RawMessage, maxValueRunes int) json.RawMessage {
	if len(input) == 0 || maxValueRunes <= 0 {
		return input
	}
	var obj any
	if err := json.Unmarshal(input, &obj); err != nil {
		return input
	}

	if !trimToolInputValue(obj, maxValueRunes, 0) {
		return input
	}

	out, err := json.Marshal(obj)
	if err != nil {
		return input
	}
	return out
}

// forkToolInputMaxDepth caps the recursive walk. Tool inputs are shallow in
// practice; the cap keeps a pathologically nested payload from turning into deep
// recursion (json.Unmarshal allows nesting far deeper than anything meaningful
// here).
const forkToolInputMaxDepth = 8

// trimToolInputValue walks a decoded JSON value, replacing oversized
// content-valued keys. It reports whether anything changed.
func trimToolInputValue(v any, maxValueRunes, depth int) bool {
	if depth > forkToolInputMaxDepth {
		return false
	}
	changed := false

	switch node := v.(type) {
	case map[string]any:
		for key, val := range node {
			if s, ok := val.(string); ok && isForkStripKey(key) {
				if n := utf8.RuneCountInString(s); n > maxValueRunes {
					node[key] = fmt.Sprintf("[omitted %d chars]", n)
					changed = true
				}
				continue
			}
			// Recurse into containers so nested edits arrays are covered.
			if trimToolInputValue(val, maxValueRunes, depth+1) {
				changed = true
			}
		}
	case []any:
		// Elements are maps/slices (reference types), so mutations made while
		// descending are visible in node without reassignment.
		for _, val := range node {
			if trimToolInputValue(val, maxValueRunes, depth+1) {
				changed = true
			}
		}
	}
	return changed
}

// isForkStripKey reports whether an input key holds content (a whole file body
// or patch text) rather than a locator such as file_path or command.
func isForkStripKey(key string) bool {
	for _, k := range forkToolInputStripKeys {
		if k == key {
			return true
		}
	}
	return false
}

// trimToolCallInputs returns a copy of toolCallMap whose entries have trimmed
// inputs. Records that need no trimming are shared, not copied. Applied at
// render time (not inside FormatToolUseBlock) so the exported formatter keeps
// its own contract and tests.
func trimToolCallInputs(toolCallMap map[string]*ToolCallRecord) map[string]*ToolCallRecord {
	if len(toolCallMap) == 0 {
		return toolCallMap
	}
	out := make(map[string]*ToolCallRecord, len(toolCallMap))
	for id, tc := range toolCallMap {
		if tc == nil {
			continue
		}
		trimmed := TrimToolInput(tc.Input, forkToolInputValueMaxRunes)
		if string(trimmed) == string(tc.Input) {
			out[id] = tc
			continue
		}
		clone := *tc
		clone.Input = trimmed
		out[id] = &clone
	}
	return out
}
