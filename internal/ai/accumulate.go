// Package ai implements AI backend abstractions for streaming chat with various CLI tools.
package ai

import (
	"encoding/json"

	"clawbench/internal/model"
)

// AccumulateBlock processes a single StreamEvent and updates the blocks slice.
// Both text and thinking events are coalesced into the most recent block of
// the same type; tool_use events are deduplicated by ID.
//
// When AI models (e.g. GLM-5.1) interleave thinking_delta and text_delta events,
// the last block may not be the same type as the incoming event. Instead of only
// checking the last block, we search backward for the most recent block of the
// same type and merge into it. However, tool_use blocks act as natural boundaries —
// text/thinking after a tool_use should not be merged with text/thinking before it.
// This prevents a single thinking or text block from being fragmented into many
// tiny blocks when events alternate, while preserving the semantic separation
// around tool calls.
//
//nolint:gocognit,gocyclo // complex stream parsing logic
func AccumulateBlock(blocks *[]model.ContentBlock, event StreamEvent) {
	// findLastBlockOfType searches backward for the most recent block of the
	// given type, but stops at tool_use boundaries (they are natural separators).
	findLastBlockOfType := func(typ string) (int, bool) {
		for i := len(*blocks) - 1; i >= 0; i-- {
			if (*blocks)[i].Type == typ {
				return i, true
			}
			// tool_use blocks are natural boundaries — don't merge across them
			if (*blocks)[i].Type == "tool_use" {
				return -1, false
			}
		}
		return -1, false
	}

	switch event.Type {
	case "content":
		// Coalesce incremental content deltas into the most recent text block.
		if idx, found := findLastBlockOfType("text"); found {
			(*blocks)[idx].Text += event.Content
		} else {
			*blocks = append(*blocks, model.ContentBlock{Type: "text", Text: event.Content})
		}
	case "thinking":
		// Coalesce incremental thinking deltas into the most recent thinking block.
		if idx, found := findLastBlockOfType("thinking"); found {
			(*blocks)[idx].Text += event.Content
		} else {
			*blocks = append(*blocks, model.ContentBlock{Type: "thinking", Text: event.Content})
		}
	case "thinking_done":
		// Mark the last thinking block as done — the thinking content is complete.
		// Without this, the frontend spinner stays until the entire response finishes.
		for i := len(*blocks) - 1; i >= 0; i-- {
			if (*blocks)[i].Type == "thinking" {
				(*blocks)[i].Done = true
				break
			}
		}
	case "tool_use":
		if event.Tool != nil {
			// Parse tool input JSON into map
			var input map[string]any
			if event.Tool.Input != "" {
				_ = json.Unmarshal([]byte(event.Tool.Input), &input)
			}
			// Find existing block by tool ID and update, or append new
			found := false
			for i := len(*blocks) - 1; i >= 0; i-- {
				if (*blocks)[i].Type != "tool_use" || (*blocks)[i].ID != event.Tool.ID {
					continue
				}
				// Merge input: preserve existing fields that the update doesn't provide.
				// ACP tool_call_update may carry only content-based description (overwriting
				// the command from the initial tool_call's rawInput), so we merge instead
				// of replace to keep both command and description.
				if len(input) > 0 {
					if (*blocks)[i].Input != nil {
						merged := make(map[string]any)
						for k, v := range (*blocks)[i].Input {
							merged[k] = v
						}
						for k, v := range input {
							merged[k] = v
						}
						(*blocks)[i].Input = merged
					} else {
						(*blocks)[i].Input = input
					}
				}
				// Update name if provided (ACP agents may send name in updates)
				if event.Tool.Name != "" {
					(*blocks)[i].Name = event.Tool.Name
				}
				(*blocks)[i].Done = event.Tool.Done
				if event.Tool.Output != "" {
					(*blocks)[i].Output = event.Tool.Output
				}
				if event.Tool.Status != "" {
					(*blocks)[i].Status = event.Tool.Status
				}
				found = true
				upsertToolCallMeta(&(*blocks)[i])
				break
			}
			if !found {
				if input == nil {
					input = make(map[string]any)
				}
				*blocks = append(*blocks, model.ContentBlock{
					Type:   "tool_use",
					Name:   event.Tool.Name,
					ID:     event.Tool.ID,
					Input:  input,
					Done:   event.Tool.Done,
					Output: event.Tool.Output,
					Status: event.Tool.Status,
				})
				upsertToolCallMeta(&(*blocks)[len(*blocks)-1])
			}
		}
	case "tool_result":
		// tool_result events update the Output/Status of an existing tool_use block
		// and mark it as Done. This handles backends (ACP, Kimi, Claude/Codebuddy
		// stream_event) that send tool results as a separate event after the tool_use.
		if event.Tool != nil {
			// Parse tool input JSON into map (same as tool_use branch)
			var input map[string]any
			if event.Tool.Input != "" {
				_ = json.Unmarshal([]byte(event.Tool.Input), &input)
			}
			for i := len(*blocks) - 1; i >= 0; i-- {
				if (*blocks)[i].Type == "tool_use" && (*blocks)[i].ID == event.Tool.ID {
					// Update input if provided (ACP tool_call_update completed events
					// may carry rawInput that was missing from earlier tool_use events)
					if len(input) > 0 {
						(*blocks)[i].Input = input
					}
					// Update name if provided (ACP agents may send name in updates)
					if event.Tool.Name != "" {
						(*blocks)[i].Name = event.Tool.Name
					}
					(*blocks)[i].Output = event.Tool.Output
					(*blocks)[i].Status = event.Tool.Status
					(*blocks)[i].Done = true
					upsertToolCallMeta(&(*blocks)[i])
					break
				}
			}
		}
	case "warning":
		*blocks = append(*blocks, model.ContentBlock{Type: "warning", Text: event.Content, Reason: event.Reason})
	case "error":
		*blocks = append(*blocks, model.ContentBlock{Type: "warning", Text: event.Error, Reason: event.Reason})
	}
}

// MergeConsecutiveThinkingBlocks merges adjacent thinking blocks, including
// across tool_use boundaries. ACP agents interleave AgentThoughtChunk and
// ToolCall events, causing many small thinking fragments separated by
// tool_use blocks. This post-processing step consolidates them into fewer
// blocks: all thinking before the first non-thinking block merges into one,
// all thinking between non-thinking blocks merges, etc.
//
// Call this after streaming completes (before DB serialization) to clean up
// fragmented thinking blocks produced by ACP backends.
func MergeConsecutiveThinkingBlocks(blocks []model.ContentBlock) []model.ContentBlock {
	if len(blocks) <= 1 {
		return blocks
	}
	var result []model.ContentBlock
	var currentThinking *model.ContentBlock

	flushThinking := func() {
		if currentThinking != nil && currentThinking.Text != "" {
			result = append(result, *currentThinking)
			currentThinking = nil
		}
	}

	for _, b := range blocks {
		if b.Type == "thinking" {
			if currentThinking != nil {
				currentThinking.Text += b.Text
				// If any merged block is done, the combined block is done
				if b.Done {
					currentThinking.Done = true
				}
			} else {
				bCopy := b
				currentThinking = &bCopy
			}
		} else {
			flushThinking()
			result = append(result, b)
		}
	}
	flushThinking()
	return result
}

// upsertToolCallMeta extracts summary/displayName/filePath from a tool_use block's
// merged input and populates the slim fields. The DB upsert is done by the caller
// (SessionExecutor) to avoid import cycles.
func upsertToolCallMeta(block *model.ContentBlock) {
	if block.Type != "tool_use" || block.ID == "" {
		return
	}
	meta := ExtractToolCallMetaFromInput(block.Name, block.ID, block.Input)
	block.Summary = meta.Summary
	block.DisplayName = meta.DisplayName
	block.FilePath = meta.FilePath
}
