package rag

import (
	"sort"
	"strings"
	"unicode/utf8"
)

// textMatchPositions finds occurrences of query terms in text and returns
// character-level (rune offset) match positions. Uses segmentation for CJK support.
// Falls back to whole-query matching if no segmented terms match.
func textMatchPositions(queryText, chunkText string) []MatchRange {
	if queryText == "" || chunkText == "" {
		return nil
	}

	var ranges []MatchRange

	// Try segmented terms first
	terms := strings.Fields(SegmentText(queryText))
	for _, term := range terms {
		ranges = append(ranges, findTermInRunes(term, chunkText)...)
	}

	// If no segmented term matched, try the whole query as-is
	if len(ranges) == 0 {
		ranges = findTermInRunes(queryText, chunkText)
	}

	return mergeRanges(ranges)
}

// findTermInRunes finds all occurrences of term in the text (case-insensitive)
// and returns rune-offset match ranges.
func findTermInRunes(term, chunkText string) []MatchRange {
	termLower := strings.ToLower(term)
	if termLower == "" {
		return nil
	}

	chunkLower := strings.ToLower(chunkText)
	var ranges []MatchRange

	// Search at byte level in lowercase, convert to rune offsets
	start := 0
	for {
		idx := strings.Index(chunkLower[start:], termLower)
		if idx < 0 {
			break
		}
		byteStart := start + idx
		byteEnd := byteStart + len(termLower)

		// Convert byte offsets to rune offsets
		runeStart := utf8.RuneCountInString(chunkText[:byteStart])
		runeEnd := runeStart + utf8.RuneCountInString(chunkText[byteStart:byteEnd])

		ranges = append(ranges, MatchRange{Start: runeStart, End: runeEnd})
		start = byteEnd
	}

	return ranges
}

// mergeRanges merges overlapping and adjacent MatchRange entries.
func mergeRanges(ranges []MatchRange) []MatchRange {
	if len(ranges) <= 1 {
		return ranges
	}
	sort.Slice(ranges, func(i, j int) bool { return ranges[i].Start < ranges[j].Start })
	merged := []MatchRange{ranges[0]}
	for _, r := range ranges[1:] {
		last := &merged[len(merged)-1]
		if r.Start <= last.End {
			if r.End > last.End {
				last.End = r.End
			}
		} else {
			merged = append(merged, r)
		}
	}
	return merged
}
