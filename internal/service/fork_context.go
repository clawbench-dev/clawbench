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

// defaultForkContextOptions is the option set used by the service-layer
// BuildForkContext: no wrapper text, raw lowercase roles, no plain-text
// fallback — preserving the behavior of the original implementation.
func defaultForkContextOptions() ForkContextOptions {
	return ForkContextOptions{BudgetChars: model.ChatForkContextBudget}
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
// Entries are kept as a trailing window (newest first) until the budget is
// exhausted, but the drop order is role-aware: assistant entries are sacrificed
// before user entries, so every user message is preserved whenever it fits.
//
// The asymmetry is worth exploiting. Measured against a real installation, user
// messages average ~51 characters while assistant entries average ~12 KB — they
// carry the tool_use input/output that dominates the budget. Dropping an
// assistant entry costs a little context; dropping a user entry loses the task
// constraints the model was asked to honor, and all user messages together are
// a rounding error next to the assistant traffic.
//
// The newest entry is never dropped: if it alone exceeds the budget its body is
// hard-truncated instead. (The current turn's user message also travels in
// req.Prompt, so truncating a body cannot lose the user's actual question.)
//
// The returned string is bounded by BudgetChars whenever the budget leaves room
// for the wrapper (header + notice + footer) plus at least one entry. A budget
// below that floor cannot be honored: the wrapper is always emitted so the model
// still learns history was compressed. The effective floor is
// runes(header + notice + footer) + 1.
func BoundForkContext(entries []ForkContextMessage, o ForkContextOptions) string {
	if len(entries) == 0 {
		return ""
	}

	budget := o.BudgetChars
	if budget <= 0 {
		budget = defaultForkContextBudgetChars
	}

	// Reserve the wrapper and the notice's upper bound (as if every entry were
	// omitted) so the notice can never push the rendered string over budget.
	// The over-reservation is at most a few digits.
	reserve := utf8.RuneCountInString(o.Header) +
		utf8.RuneCountInString(o.Footer) +
		utf8.RuneCountInString(forkOmissionNotice(len(entries)))
	avail := budget - reserve
	if avail < 1 {
		// The wrapper alone exceeds the budget. There is nothing to trim — the
		// header/notice/footer are always emitted so the model still learns that
		// history was compressed. The frontend enforces min 1000 and the PATCH
		// validator rejects < 1, so this only arises from a hand-edited config.
		avail = 1
	}

	// Walk newest-first, sacrificing assistant entries when an entry does not
	// fit. kept is newest-first with no gaps, so it renders chronologically when
	// walked backwards.
	var kept []ForkContextMessage
	used := 0

	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		cost := forkEntryCost(e)

		if used+cost <= avail {
			kept = append(kept, e)
			used += cost
			continue
		}

		// Does not fit. An assistant entry is expendable — skip it and keep
		// scanning for older user messages. This applies to the newest entry too:
		// a recent assistant reply is not worth sacrificing the user's earlier
		// instructions for.
		if e.Role != roleUser {
			continue
		}

		// A user entry ends the window: the budget cannot hold it, and dropping
		// user instructions to reach older assistant chatter would invert the
		// priority this function exists to enforce.
		break
	}

	// Nothing fit at all — e.g. a single oversized assistant entry. Fall back to
	// a truncated newest entry so the model still receives recent context rather
	// than an empty history.
	if len(kept) == 0 {
		e := entries[len(entries)-1]
		kept = append(kept, ForkContextMessage{
			Role: e.Role,
			Body: forkTruncateBody(e.Body, avail-forkEntryOverhead(e)),
		})
	}

	// Everything not kept was dropped. Deriving the count instead of tallying it
	// while scanning keeps the notice correct no matter which entries the loop
	// skipped (kept has no gaps, so the arithmetic is exact).
	omitted := len(entries) - len(kept)

	var sb strings.Builder
	sb.WriteString(o.Header)
	if omitted > 0 {
		sb.WriteString(forkOmissionNotice(omitted))
	}
	// Render chronologically: kept is newest-first, so walk it backwards.
	for n := len(kept) - 1; n >= 0; n-- {
		sb.WriteString(kept[n].Role)
		sb.WriteString(": ")
		sb.WriteString(kept[n].Body)
		sb.WriteString("\n\n")
	}
	sb.WriteString(o.Footer)
	return sb.String()
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
// truncation suffix, never returning fewer than one rune of content.
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
