package summarize

import "context"

// SimpleSummarizer performs no AI-based summarization.
// It can strip markdown formatting and truncate long text,
// making it a zero-cost, zero-latency summarizer suitable for
// cases where raw cleaned text is acceptable for TTS.
// When preserveMarkdown is true, StripMarkdown is skipped — used
// for display summaries (chat/task execution) where formatting matters.
type SimpleSummarizer struct {
	preserveMarkdown bool
	maxRunes         int
}

// NewSimple creates a SimpleSummarizer that strips markdown (for TTS).
func NewSimple() *SimpleSummarizer {
	return &SimpleSummarizer{
		preserveMarkdown: false,
		maxRunes:         SimpleMaxSummarizeRunes,
	}
}

// NewSimplePreserveMarkdown creates a SimpleSummarizer that preserves markdown
// formatting (for display summaries where code blocks and formatting matter).
func NewSimplePreserveMarkdown() *SimpleSummarizer {
	return &SimpleSummarizer{
		preserveMarkdown: true,
		maxRunes:         SimpleMaxSummarizeRunes,
	}
}

// Summarize condenses text without calling any AI model.
// When preserveMarkdown is false (TTS mode), StripMarkdown is applied before truncation.
// When preserveMarkdown is true (display mode), raw text is truncated as-is.
// Uses SimpleMaxSummarizeRunes (1000) as the default truncation limit.
// The language parameter is ignored — simple summarizer has no language awareness.
func (s *SimpleSummarizer) Summarize(_ context.Context, text string, _ string) (string, error) {
	var cleaned string
	if s.preserveMarkdown {
		cleaned = text
	} else {
		cleaned = StripMarkdown(text)
	}

	maxR := s.maxRunes
	if maxR <= 0 {
		maxR = SimpleMaxSummarizeRunes
	}
	runes := []rune(cleaned)
	if len(runes) > maxR {
		cleaned = string(runes[len(runes)-maxR:])
	}

	return cleaned, nil
}
