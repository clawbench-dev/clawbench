package codex

import "strings"

// Codex resume output uses these Unicode tags for thinking blocks.
// Defining them as constants avoids encoding issues in source code.
const (
	codexThinkOpen  = "\u003Cthink\u003E"
	codexThinkClose = "\u003C/think\u003E"
)

// codexSplitThinking separates thinking from content in a codex agent_message.
// Codex may output thinking in two formats:
//  1. MiniMax-style: "antie...content...antie\n\nactual response"
//  2. Codex-style: "thinking text\n\nactual response" (no tags, \n\n separator)
//
// This function handles both by first checking for tags, then falling back to \n\n.
func codexSplitThinking(text string) (thinking, content string) {
	// Check for MiniMax-style tags
	openIdx := strings.Index(text, codexThinkOpen)
	closeIdx := strings.Index(text, codexThinkClose)

	if openIdx >= 0 && closeIdx > openIdx {
		thinking = strings.TrimSpace(text[openIdx+len(codexThinkOpen) : closeIdx])
		// Content is everything after the closing tag
		rest := text[closeIdx+len(codexThinkClose):]
		content = strings.TrimSpace(rest)
		return thinking, content
	}

	// Check if text starts with the open tag (closing tag might be missing)
	if openIdx == 0 && closeIdx < 0 {
		thinking = strings.TrimSpace(text[len(codexThinkOpen):])
		return thinking, content
	}

	// Fallback: split on first \n\n (Codex's native format)
	if idx := strings.Index(text, "\n\n"); idx >= 0 {
		thinking = text[:idx]
		content = text[idx+2:]
		return thinking, content
	}

	// No separator — entire text is content
	content = text
	return thinking, content
}
