package summarize

import (
	"context"
	"log/slog"
)

// AutoJunkRatio is the junk ratio at or above which AutoSummarizer routes a
// message to the LLM backend instead of the simple cleaner. The ratio is
// removedRunes/originalRunes as reported by StripMarkdownStats: how much of the
// original text (code blocks, tables, formulas, links, markup) was unreadable
// and had to be stripped. When most of a message was junk, the cleaned
// remainder is usually a few disconnected fragments that read badly aloud — an
// LLM can stitch them into coherent speech. When most of the message is already
// prose, stripping is enough.
//
// Set from config (summarize.auto_junk_ratio) at startup and on hot-reload,
// mirroring InlineCodeMaxLen / MaxSummarizeRunes.
var AutoJunkRatio = 0.5

// autoMaxKeptRunes is the second routing axis: even a spotless message is sent
// to the LLM when its cleaned prose exceeds this many runes. The simple
// summarizer truncates to SimpleMaxSummarizeRunes (1000) — speaking 3000 runes
// of clean prose verbatim is worse than a 1000-rune condensation, and the
// truncation would silently drop the tail. Kept equal to SimpleMaxSummarizeRunes
// so "too long for simple to handle" and "simple would truncate" coincide.
const autoMaxKeptRunes = SimpleMaxSummarizeRunes

// AutoSummarizer routes each text between a cheap non-LLM cleaner and an LLM
// summarizer based on the content itself:
//
//   - if the junk ratio (stripped/removed runes) is >= AutoJunkRatio, the
//     cleaned text is too fragmented to read as-is → use the LLM; or
//   - if the cleaned prose is longer than autoMaxKeptRunes, it is too long to
//     read verbatim → use the LLM;
//   - otherwise the cleaned text is short and coherent → return it directly
//     (no LLM call, no latency, no cost).
//
// The LLM branch receives the ORIGINAL text (not the pre-stripped one): the
// TTS system prompt already instructs the model to omit code and formatting, and
// handing it clean structure lets it decide what to drop, rather than inheriting
// the cleaner's fragmenting decisions. The LLM's own pipeline still truncates to
// MaxSummarizeRunes as a token guard.
type AutoSummarizer struct {
	simple Summarizer
	api    Summarizer
}

// NewAuto creates an AutoSummarizer. api may be nil (no LLM configured), in
// which case every message falls back to the simple cleaner.
func NewAuto(simple, api Summarizer) *AutoSummarizer {
	if simple == nil {
		simple = NewSimple()
	}
	return &AutoSummarizer{simple: simple, api: api}
}

// Summarize routes text to the simple cleaner or the LLM summarizer.
// language is passed through to whichever backend handles the request.
func (a *AutoSummarizer) Summarize(ctx context.Context, text string, language string) (string, error) {
	if a.api == nil {
		return a.simple.Summarize(ctx, text, language)
	}

	cleaned, original, kept := StripMarkdownStats(text)
	var junkRatio float64
	if original > 0 {
		junkRatio = float64(original-kept) / float64(original)
	}

	useAPI := junkRatio >= AutoJunkRatio || kept > autoMaxKeptRunes
	backend := "simple"
	if useAPI {
		backend = "api"
	}
	slog.Info(
		"tts auto summarize routing",
		slog.String("backend", backend),
		slog.Float64("junk_ratio", junkRatio),
		slog.Int("original_runes", original),
		slog.Int("kept_runes", kept),
	)

	if !useAPI {
		return cleaned, nil
	}
	return a.api.Summarize(ctx, text, language)
}

// Compile-time assertion that AutoSummarizer satisfies the Summarizer contract.
var _ Summarizer = (*AutoSummarizer)(nil)
