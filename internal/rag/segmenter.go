package rag

import (
	"fmt"
	"strings"

	"github.com/go-ego/gse"
)

var segmenter *gse.Segmenter

// InitSegmenter initializes the gse segmenter for Chinese text segmentation.
// Call once at startup. If it fails, SegmentText falls back to returning original text.
//
// LoadDictEmbed loads the dictionary embedded in the gse package at compile time.
// The file-based seg.LoadDict() resolves its dictionary directory from the source
// path baked into the binary at build time (runtime.Caller), so it only works on a
// machine that still has that Go module cache. Released binaries, the Docker image
// and the Android build all run without it, and silently lost Chinese segmentation
// as a result — the embedded dictionary has no runtime dependency.
func InitSegmenter() error {
	var seg gse.Segmenter
	if err := seg.LoadDictEmbed("zh"); err != nil {
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
