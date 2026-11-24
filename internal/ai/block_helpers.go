package ai

import (
	"log/slog"
	"strings"

	"clawbench/internal/askquestion"
	"clawbench/internal/model"

	"github.com/google/uuid"
)

// StringsContainsAnyBlock checks if any text ContentBlock contains the given substring.
func StringsContainsAnyBlock(blocks []model.ContentBlock, substr string) bool {
	for _, b := range blocks {
		if b.Type == "text" && strings.Contains(b.Text, substr) {
			return true
		}
	}
	return false
}

// rejectedToolOutputMarkers are the only error texts RemoveRejectedToolBlocks
// still reads. They exist for backends that report a hallucinated tool name as
// a plain FAILED call with no structured signal — their wording is all we get.
// Kept as narrow as possible: a false positive here silently deletes a real
// tool call from the transcript.
var rejectedToolOutputMarkers = []string{
	"not found in agent cli", // ACP agents (Claude Code, CodeBuddy, ...)
}

// RemoveRejectedToolBlocks strips tool_use blocks for tools that DO NOT EXIST.
// These occur when the model hallucinates a tool name (e.g. "/commit" as a
// slash command, or "AskUserQuestion" when the question was meant to go through
// the ask-question card path). The block and its matching warning are noise:
// the user sees a fake failure, and when the invented name collides with a real
// card name it renders as a DUPLICATE of the card that followed.
//
// Detection order is deliberate:
//
//  1. NotFoundThisRun — the backend KNEW the tool was missing (see
//     ai.ToolCall.NotFoundThisRun). Fact, not inference; works regardless of how
//     the backend words the message.
//  2. rejectedToolOutputMarkers — narrow fallback for backends that can only
//     report a failure and leave the reason in the text.
//
// Also removes warning blocks referencing a rejected tool name.
func RemoveRejectedToolBlocks(blocks []model.ContentBlock) []model.ContentBlock {
	// Collect names of rejected tools from failed tool_use blocks
	rejectedNames := make(map[string]bool)
	for _, block := range blocks {
		if block.Type == "tool_use" && block.Status == "error" && isRejectedToolCall(block) {
			rejectedNames[block.Name] = true
		}
	}
	if len(rejectedNames) == 0 {
		return blocks
	}

	filtered := make([]model.ContentBlock, 0, len(blocks))
	for _, block := range blocks {
		// Remove failed tool_use blocks for rejected tool names
		if block.Type == "tool_use" && block.Status == "error" && rejectedNames[block.Name] {
			slog.Info(
				"removing rejected tool_use block",
				slog.String("name", block.Name),
				slog.String("id", block.ID),
				slog.String("output", block.Output),
			)
			continue
		}
		// Remove warning blocks that reference the rejected tool name with "not found"
		if block.Type == "warning" && strings.Contains(block.Text, "not found") {
			matched := false
			for name := range rejectedNames {
				if strings.Contains(block.Text, name) {
					matched = true
					break
				}
			}
			if matched {
				slog.Info(
					"removing rejected-tool warning block",
					slog.String("text", block.Text),
				)
				continue
			}
		}
		filtered = append(filtered, block)
	}
	return filtered
}

// isRejectedToolCall reports whether a failed tool_use block is a call to a
// tool that does not exist at all.
func isRejectedToolCall(block model.ContentBlock) bool {
	if block.NotFoundThisRun {
		return true
	}
	for _, marker := range rejectedToolOutputMarkers {
		if strings.Contains(block.Output, marker) {
			return true
		}
	}
	return false
}

// ConvertAskQuestionBlocks detects <clawbench-ask-question> tags in text ContentBlocks,
// parses their payloads, and converts them into a single tool_use ContentBlock
// with name="AskUserQuestion".
//
// Parsing is delegated to internal/askquestion — the same implementation the
// frontend mirrors (web/src/utils/askQuestion.ts) — so a payload is understood
// identically on both sides.
//
// Two behaviors differ from the previous implementation, both deliberate:
//
//   - Every tag in a text block is converted, not just the last one. 27% of
//     production text blocks contain two or more tags; previously all but the
//     last leaked as raw XML.
//   - All tags of one text block merge into ONE tool block. Emitting one per
//     tag would create a random id and a DB row per tag, and the frontend
//     merges ask cards anyway (shouldMergeAskCards), so a single block produces
//     the same UI with less churn.
//
// An unparseable tag is left in the text block verbatim (never stripped): its
// raw text is the only remaining copy of the question.
//
// Returns the updated blocks slice.
func ConvertAskQuestionBlocks(blocks []model.ContentBlock) []model.ContentBlock {
	// Defensive copy: avoid mutating the caller's slice elements.
	// Without this, modifying blocks[i].Text below would also modify
	// the caller's slice (e.g. SessionExecutor.e.blocks) because
	// Go slices share the underlying array. This caused a bug where
	// buildResult's postProcessBlocks stripped <clawbench-ask-question> tags
	// from e.blocks, then Finalize's postProcessBlocks couldn't
	// detect the tags anymore — resulting in the AskUserQuestion
	// tool_use block being lost from chat_history.content.
	blocks = append([]model.ContentBlock(nil), blocks...)

	type conversion struct {
		index     int
		input     map[string]any
		cleanText string
	}
	var conversions []conversion

	for i := range blocks {
		block := &blocks[i]
		if block.Type != "text" || !strings.Contains(block.Text, "<clawbench-ask-question") {
			continue
		}

		matches := askquestion.Extract(block.Text)
		items := askquestion.AllItems(matches)
		if len(items) == 0 {
			// Nothing parsed. Strip the wrapper anyway so the payload degrades
			// to readable Markdown instead of leaking raw tags into the
			// conversation; Strip replaces each unparsed span with its inner
			// text, so no content is lost.
			if reasons := askquestion.UnparsedReasons(matches); len(reasons) > 0 {
				slog.Warn(
					"clawbench-ask-question payload unparseable, rendering as markdown",
					slog.Any("reasons", reasons),
				)
				if clean := strings.TrimSpace(askquestion.Strip(block.Text, matches)); clean != strings.TrimSpace(block.Text) {
					block.Text = clean
				}
			}
			continue
		}

		conversions = append(conversions, conversion{
			index:     i,
			input:     askquestion.ToInputMap(items),
			cleanText: strings.TrimSpace(askquestion.Strip(block.Text, matches)),
		})
	}

	for i := len(conversions) - 1; i >= 0; i-- {
		c := conversions[i]
		toolBlock := model.ContentBlock{
			Type:  "tool_use",
			Name:  "AskUserQuestion",
			ID:    "ask-" + uuid.New().String(),
			Input: c.input,
			Done:  true,
		}

		if c.cleanText == "" {
			blocks[c.index] = toolBlock
		} else {
			blocks[c.index].Text = c.cleanText
			insertAt := c.index + 1
			blocks = append(blocks[:insertAt], append([]model.ContentBlock{toolBlock}, blocks[insertAt:]...)...)
		}
	}

	// RemoveRejectedToolBlocks is called by postProcessBlocks after this
	// function returns — no need to call it here.

	return blocks
}
