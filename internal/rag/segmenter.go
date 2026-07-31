package rag

import (
	"fmt"
	"strings"

	"github.com/go-ego/gse"
)

var segmenter *gse.Segmenter

// InitSegmenter initializes the gse segmenter for Chinese text segmentation.
// Call once at startup. If it fails, SegmentText falls back to returning original text.
func InitSegmenter() error {
	var seg gse.Segmenter
	if err := seg.LoadDict(); err != nil {
		return fmt.Errorf("load gse dictionary: %w", err)
	}
	segmenter = &seg
	return nil
}

// SegmentText segments text for FTS indexing using CutSearch mode.
// Returns space-separated tokens suitable for SQLite FTS5.
// Falls back to original text if segmenter is not initialized.
func SegmentText(text string) string {
	if segmenter == nil {
		return text
	}
	tokens := segmenter.CutSearch(text, true)
	return strings.Join(tokens, " ")
}

// SegmentTokens segments text and returns the raw token slice.
// Unlike SegmentText which returns a space-joined string, this returns []string
// for use in set-based similarity computations.
func SegmentTokens(text string) []string {
	if text == "" {
		return nil
	}
	if segmenter == nil {
		// No segmenter — fall back to whitespace splitting
		return strings.Fields(text)
	}
	return segmenter.Cut(text, true)
}
