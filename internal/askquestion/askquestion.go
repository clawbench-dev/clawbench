// Package askquestion is the single canonical implementation of
// <clawbench-ask-question> payload handling.
//
// Before this package there were five independent parsers (two in Go, three in
// TS) that disagreed on closing-tag tolerance and code-fence exclusion. That
// disagreement was observable: the same message could render a card in one
// layer and leak raw markup in another, and — worst — a payload that the
// detector accepted but the parser rejected had its tag stripped anyway,
// deleting the question from the conversation entirely.
//
// Two payload shapes reach this package:
//
//   - Path A: a native tool call whose input is JSON. Handled by NormalizeInput.
//   - Path B: a <clawbench-ask-question> tag embedded in assistant text (the
//     shape the system prompt mandates, see internal/model/agent.go), whose
//     payload is native Markdown. Handled by Extract.
//
// Only that one payload format is accepted. There is no fallback reader: an
// earlier version tolerated a bespoke XML shape and recovered JSON, and the
// extra acceptance hid malformed output instead of surfacing it.
//
// The package deliberately imports nothing from this module, so every layer
// (ai, service, summarize, handler) can depend on it without an import cycle.
// The TypeScript mirror lives in web/src/utils/askQuestion.ts and the two are
// kept in sync by testdata/parity_corpus.json — the same convention
// internal/version/compare.go uses with web/src/utils/version.ts.
package askquestion

// Option is one selectable answer.
type Option struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// Item is one question.
type Item struct {
	Header      string   `json:"header"`
	MultiSelect bool     `json:"multiSelect"`
	Question    string   `json:"question"`
	Options     []Option `json:"options"`
}

// Match is one <clawbench-ask-question> span located in a text block.
//
// Parsed == false means the span could not be understood. Callers MUST keep
// Raw in the visible text in that case — never strip it. Silently dropping an
// unparseable question is the defect this package exists to prevent.
type Match struct {
	// Start/End bound the span in the source text. When Parsed is false the
	// span is advisory only and must not be removed.
	Start int
	End   int
	// Raw is the matched source text (text[Start:End]).
	Raw string
	// Items is non-empty only when Parsed is true.
	Items []Item
	// Parsed reports whether the payload was understood.
	Parsed bool
	// Reason is a Reason* code describing why an unparsed span was left alone.
	Reason string
	// Fallback is the text to show in place of an unparsed span: the payload
	// with its tag wrapper removed, so it renders as ordinary Markdown. It is
	// set only when Parsed is false and is never empty for a non-empty span.
	//
	// Showing the inner text (rather than the raw tag) means a parse failure
	// degrades to readable prose instead of exposing markup. No content is
	// lost either way — Fallback always contains everything the span held.
	Fallback string
}

// Reason codes for an unparsed span. Exported so consumers outside this
// package (notably internal/service, which does not disable goconst) never
// have to inline the literals.
const (
	// ReasonNoStandardClose means the payload ended without a standard
	// </clawbench-ask-question>, so there is no payload to parse.
	ReasonNoStandardClose = "no_standard_close"
	// ReasonParseFailed means the span was bounded but yielded no question.
	ReasonParseFailed = "parse_failed"
)

// KeyQuestions is the canonical key for the question array in a tool input. It
// is exported so the several producers of that shape cannot drift apart.
const KeyQuestions = "questions"
