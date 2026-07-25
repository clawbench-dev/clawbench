package rag

import "testing"

func TestTextMatchPositions_EnglishSingleTerm(t *testing.T) {
	positions := textMatchPositions("quick", "The quick brown fox")
	if len(positions) == 0 {
		t.Fatal("expected match positions")
	}
	if positions[0].Start != 4 || positions[0].End != 9 {
		t.Errorf("expected [4:9], got [%d:%d]", positions[0].Start, positions[0].End)
	}
}

func TestTextMatchPositions_EnglishCaseInsensitive(t *testing.T) {
	positions := textMatchPositions("Quick", "the quick brown fox")
	if len(positions) == 0 {
		t.Fatal("expected match positions for case-insensitive match")
	}
	if positions[0].Start != 4 || positions[0].End != 9 {
		t.Errorf("expected [4:9], got [%d:%d]", positions[0].Start, positions[0].End)
	}
}

func TestTextMatchPositions_EnglishMultipleTerms(t *testing.T) {
	positions := textMatchPositions("quick brown", "The quick brown fox jumps quickly")
	if len(positions) < 2 {
		t.Fatalf("expected at least 2 match ranges, got %d", len(positions))
	}
	// "quick" at [4:9], "brown" at [10:15], "quickly" contains "quick" at [26:32]
}

func TestTextMatchPositions_ChineseText(t *testing.T) {
	positions := textMatchPositions("数据库查询", "使用数据库查询进行全文检索")
	if len(positions) == 0 {
		t.Fatal("expected match positions for Chinese text")
	}
	runes := []rune("使用数据库查询进行全文检索")
	for _, mp := range positions {
		if mp.Start < 0 || mp.End > len(runes) {
			t.Errorf("invalid position: %+v (text has %d runes)", mp, len(runes))
		}
		if mp.Start >= mp.End {
			t.Errorf("start >= end: %+v", mp)
		}
	}
}

func TestTextMatchPositions_MixedCJKLatin(t *testing.T) {
	positions := textMatchPositions("API错误", "处理API错误日志中的问题")
	if len(positions) == 0 {
		t.Fatal("expected match positions for mixed text")
	}
}

func TestTextMatchPositions_NoMatch(t *testing.T) {
	positions := textMatchPositions("xyz", "Hello world")
	if len(positions) != 0 {
		t.Errorf("expected no matches, got %d", len(positions))
	}
}

func TestTextMatchPositions_EmptyInputs(t *testing.T) {
	if textMatchPositions("", "text") != nil {
		t.Error("expected nil for empty query")
	}
	if textMatchPositions("query", "") != nil {
		t.Error("expected nil for empty text")
	}
	if textMatchPositions("", "") != nil {
		t.Error("expected nil for both empty")
	}
}

func TestTextMatchPositions_OverlappingRanges(t *testing.T) {
	positions := textMatchPositions("aa", "aaa")
	// "aa" matches at byte [0:2], then search continues from byte 2 where only "a" remains.
	// So only 1 match: [0:2] in rune offsets.
	if len(positions) != 1 {
		t.Fatalf("expected 1 range, got %d", len(positions))
	}
	if positions[0].Start != 0 || positions[0].End != 2 {
		t.Errorf("expected [0:2], got [%d:%d]", positions[0].Start, positions[0].End)
	}
}

func TestTextMatchPositions_WholeQueryFallback(t *testing.T) {
	// If segmented terms don't individually match but the whole query does,
	// the whole-query fallback should still highlight
	positions := textMatchPositions("error handling", "error handling in Go")
	if len(positions) == 0 {
		t.Fatal("expected match positions for whole query fallback")
	}
	// Should match "error" and "handling" at minimum
}

func TestTextMatchPositions_MultipleOccurrences(t *testing.T) {
	positions := textMatchPositions("test", "test one test two test")
	// "test" appears 3 times at non-overlapping positions
	if len(positions) != 3 {
		t.Errorf("expected 3 non-overlapping ranges, got %d", len(positions))
	}
}

func TestTextMatchPositions_MultipleNonOverlapping(t *testing.T) {
	positions := textMatchPositions("go", "go language and go tools")
	if len(positions) != 2 {
		t.Errorf("expected 2 non-overlapping ranges, got %d", len(positions))
	}
}

func TestMergeRanges(t *testing.T) {
	tests := []struct {
		name   string
		input  []MatchRange
		expect []MatchRange
	}{
		{"empty", nil, nil},
		{"single", []MatchRange{{1, 3}}, []MatchRange{{1, 3}}},
		{"adjacent", []MatchRange{{1, 3}, {3, 5}}, []MatchRange{{1, 5}}},
		{"overlapping", []MatchRange{{1, 4}, {2, 5}}, []MatchRange{{1, 5}}},
		{"disjoint", []MatchRange{{1, 3}, {5, 7}}, []MatchRange{{1, 3}, {5, 7}}},
		{"unsorted", []MatchRange{{5, 7}, {1, 3}}, []MatchRange{{1, 3}, {5, 7}}},
		{"triple_merge", []MatchRange{{1, 3}, {3, 5}, {5, 8}}, []MatchRange{{1, 8}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mergeRanges(tt.input)
			if len(result) != len(tt.expect) {
				t.Fatalf("expected %v, got %v", tt.expect, result)
			}
			for i, r := range result {
				if r != tt.expect[i] {
					t.Errorf("position %d: expected %+v, got %+v", i, tt.expect[i], r)
				}
			}
		})
	}
}
