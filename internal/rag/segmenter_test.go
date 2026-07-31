package rag

import (
	"strings"
	"testing"
)

func TestSegmentTokens_Chinese(t *testing.T) {
	// TestMain initializes the segmenter, so Cut mode should be active
	tokens := SegmentTokens("你好啊")
	if len(tokens) == 0 {
		t.Fatal("expected non-empty tokens for Chinese text")
	}
	// Check that key tokens are present (gse Cut with HMM should produce "你好" and "啊")
	hasNiHao := false
	hasA := false
	for _, tok := range tokens {
		if tok == "你好" {
			hasNiHao = true
		}
		if tok == "啊" {
			hasA = true
		}
	}
	if !hasNiHao {
		t.Errorf("expected token '你好' in result, got tokens: %v", tokens)
	}
	if !hasA {
		t.Errorf("expected token '啊' in result, got tokens: %v", tokens)
	}
}

func TestSegmentTokens_English(t *testing.T) {
	tokens := SegmentTokens("continue debugging")
	if len(tokens) == 0 {
		t.Fatal("expected non-empty tokens for English text")
	}
	hasContinue := false
	hasDebugging := false
	for _, tok := range tokens {
		if tok == "continue" {
			hasContinue = true
		}
		if tok == "debugging" {
			hasDebugging = true
		}
	}
	if !hasContinue {
		t.Errorf("expected token 'continue' in result, got tokens: %v", tokens)
	}
	if !hasDebugging {
		t.Errorf("expected token 'debugging' in result, got tokens: %v", tokens)
	}
}

func TestSegmentTokens_Empty(t *testing.T) {
	tokens := SegmentTokens("")
	if tokens != nil {
		t.Errorf("expected nil for empty input, got %v", tokens)
	}
}

func TestSegmentTokens_NoSegmenter(t *testing.T) {
	// Temporarily nil the segmenter to test fallback
	orig := segmenter
	segmenter = nil
	defer func() { segmenter = orig }()

	text := "hello world foo"
	tokens := SegmentTokens(text)

	expected := strings.Fields(text)
	if len(tokens) != len(expected) {
		t.Fatalf("expected %d tokens from Fields fallback, got %d", len(expected), len(tokens))
	}
	for i, tok := range tokens {
		if tok != expected[i] {
			t.Errorf("token %d: expected %q, got %q", i, expected[i], tok)
		}
	}
}
