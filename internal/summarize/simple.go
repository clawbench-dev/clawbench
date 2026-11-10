package summarize

import "context"

// SimpleSummarizer performs no AI-based summarization at all: it only cleans the
// text up so it can be read aloud. It is the zero-cost, zero-latency path used
// when no LLM summarizer is configured, and the fallback when one fails.
//
// It deliberately does NOT truncate. The caller's contract is "if you could not
// condense it, read the whole thing" — a cap here silently turned a long reply
// into a fragment, and because the audio starts at the first character kept, a
// tail-keeping cap made playback begin mid-sentence ("前半段没读，直接从中段读").
// Deciding that a text is too long to read verbatim is a ROUTING decision made
// by AutoSummarizer (autoMaxKeptRunes), not something this cleaner may do.
//
// When preserveMarkdown is true, StripMarkdown is skipped — used for display
// summaries (chat/task execution) where formatting matters.
type SimpleSummarizer struct {
	preserveMarkdown bool
}

// NewSimple creates a SimpleSummarizer that strips markdown (for TTS).
func NewSimple() *SimpleSummarizer {
	return &SimpleSummarizer{preserveMarkdown: false}
}

// NewSimplePreserveMarkdown creates a SimpleSummarizer that preserves markdown
// formatting (for display summaries where code blocks and formatting matter).
func NewSimplePreserveMarkdown() *SimpleSummarizer {
	return &SimpleSummarizer{preserveMarkdown: true}
}

// Summarize returns the cleaned text, uncut.
// The language parameter is ignored — simple summarizer has no language awareness.
func (s *SimpleSummarizer) Summarize(_ context.Context, text string, _ string) (string, error) {
	if s.preserveMarkdown {
		return text, nil
	}
	return StripMarkdown(text), nil
}
