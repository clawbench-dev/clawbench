package rag

import (
	"encoding/json"
	"strings"
	"unicode"

	"clawbench/internal/model"
	"clawbench/internal/summarize"
)

// TextChunk represents a chunked segment of text with its token count.
type TextChunk struct {
	Text       string
	TokenCount int
	Index      int
}

// ExtractTextFromContent extracts text blocks from a chat message content.
// For user messages, content is plain text.
// For assistant messages, content is JSON with ContentBlocks.
func ExtractTextFromContent(content, role string) string {
	if role == "user" {
		return strings.TrimSpace(content)
	}

	// Assistant message: parse JSON and extract only the conclusion
	var msg struct {
		Blocks []model.ContentBlock `json:"blocks"`
	}
	if err := json.Unmarshal([]byte(content), &msg); err != nil {
		// Not valid JSON — treat as plain text fallback
		return strings.TrimSpace(content)
	}

	return strings.TrimSpace(summarize.ExtractLastAnswerFromBlocks(msg.Blocks))
}

// ChunkText splits text into overlapping chunks of approximately chunkSize tokens.
// Uses a simple whitespace-based token estimation (1 token ≈ 0.75 words for English,
// 1 token ≈ 1.5 chars for CJK). This is a rough approximation sufficient for chunking.
func ChunkText(text string, chunkSize, chunkOverlap int) []TextChunk {
	if text == "" || chunkSize <= 0 {
		return nil
	}

	tokens := estimateTokens(text)
	if tokens <= chunkSize {
		return []TextChunk{{
			Text:       text,
			TokenCount: tokens,
			Index:      0,
		}}
	}

	// Split into runes for proper CJK handling
	runes := []rune(text)
	var chunks []TextChunk
	chunkIdx := 0

	start := 0
	for start < len(runes) {
		// Estimate character position for chunkSize tokens
		// Approximation: ~4 chars per token (mixed CJK/English average)
		estChars := chunkSize * 4
		end := start + estChars
		if end > len(runes) {
			end = len(runes)
		}

		// Try to break at a sentence/paragraph boundary
		if end < len(runes) {
			end = findBreakPoint(runes, start, end)
		}

		chunkText := strings.TrimSpace(string(runes[start:end]))
		if chunkText != "" {
			chunks = append(chunks, TextChunk{
				Text:       chunkText,
				TokenCount: estimateTokens(chunkText),
				Index:      chunkIdx,
			})
			chunkIdx++
		}

		// Move start back by overlap
		overlapChars := chunkOverlap * 4
		nextStart := end - overlapChars
		if nextStart <= start {
			nextStart = end
		}
		if nextStart >= len(runes) {
			break
		}
		start = nextStart
	}

	return chunks
}

// findBreakPoint finds a good break point near the estimated end position.
// Prefers paragraph breaks, then sentence endings, then whitespace.
func findBreakPoint(runes []rune, start, end int) int { //nolint:gocyclo // multi-strategy break point search
	// Search backwards from end for a good break point
	searchStart := end - min(200, end-start) // Look back up to 200 runes
	if searchStart < start {
		searchStart = start
	}

	// Priority 1: Double newline (paragraph break)
	for i := end; i > searchStart; i-- {
		if i < len(runes)-1 && runes[i] == '\n' && runes[i+1] == '\n' {
			return i + 2
		}
	}

	// Priority 2: Single newline
	for i := end; i > searchStart; i-- {
		if runes[i] == '\n' {
			return i + 1
		}
	}

	// Priority 3: Sentence-ending punctuation
	for i := end; i > searchStart; i-- {
		r := runes[i]
		if r == '.' || r == '。' || r == '！' || r == '？' || r == '!' || r == '?' {
			return i + 1
		}
	}

	// Priority 4: Whitespace
	for i := end; i > searchStart; i-- {
		if unicode.IsSpace(runes[i]) {
			return i + 1
		}
	}

	// No good break point found — use the estimated position
	return end
}

// estimateTokens provides a rough token count estimation.
// CJK characters count as ~1.5 tokens each, English words as ~1.3 tokens each.
func estimateTokens(text string) int {
	cjkCount := 0
	wordCount := 0
	inWord := false

	for _, r := range text {
		if isCJK(r) {
			cjkCount++
			if inWord {
				wordCount++
				inWord = false
			}
		} else if unicode.IsSpace(r) {
			if inWord {
				wordCount++
				inWord = false
			}
		} else {
			inWord = true
		}
	}
	if inWord {
		wordCount++
	}

	// CJK: ~1.5 chars per token, English: ~1.3 words per token (conservative)
	cjkTokens := int(float64(cjkCount) / 1.5)
	enTokens := int(float64(wordCount) * 1.3)

	return cjkTokens + enTokens
}

// isCJK checks if a rune is a CJK character.
func isCJK(r rune) bool {
	return unicode.In(r, unicode.Han, unicode.Hangul, unicode.Hiragana, unicode.Katakana)
}
