package ai

import (
	"context"
	"encoding/json"
	"log/slog"
	"regexp"
	"strings"

	"clawbench/internal/model"

	"github.com/google/uuid"
)

// SendStreamEvent sends an event to the stream channel.
// Non-blocking: if the channel is full (no SSE client reading), the event is dropped.
// This is safe because content is persisted to DB independently.
// Returns false if the context was cancelled.
func SendStreamEvent(ctx context.Context, ch chan<- StreamEvent, event StreamEvent) bool {
	select {
	case ch <- event:
		return true
	case <-ctx.Done():
		return false
	default:
		// Channel full — drop the event, DB persistence ensures no data loss
		toolID := ""
		if event.Tool != nil {
			toolID = event.Tool.ID
		}
		slog.Warn(
			"SSE event dropped — channel full",
			slog.String("type", event.Type),
			slog.String("tool_id", toolID),
		)
		return true
	}
}

// SendFinalStreamEvent sends a terminal event (done/cancelled/error) to the stream channel
// without checking context cancellation. This ensures the SSE client always receives
// the terminal event even after the CLI context has been cancelled (e.g. ExitPlanMode).
func SendFinalStreamEvent(ch chan<- StreamEvent, event StreamEvent) {
	select {
	case ch <- event:
	default:
		slog.Warn(
			"SSE terminal event dropped — channel full",
			slog.String("type", event.Type),
		)
	}
}

// StringsContainsAnyBlock checks if any text ContentBlock contains the given substring.
func StringsContainsAnyBlock(blocks []model.ContentBlock, substr string) bool {
	for _, b := range blocks {
		if b.Type == "text" && strings.Contains(b.Text, substr) {
			return true
		}
	}
	return false
}

// RemoveRejectedToolBlocks strips tool_use blocks that were rejected by the CLI
// (Status=="error" and output contains "not found in agent cli"). These occur when
// the AI model hallucinates tool names (e.g. "/commit" as a slash command, or
// "AskUserQuestion" when <ask-question> XML tags are also emitted). The rejected
// tool_use block and its matching warning are confusing noise for the user.
// Also removes warning blocks containing the "Tool <name> not found in agent cli" pattern.
func RemoveRejectedToolBlocks(blocks []model.ContentBlock) []model.ContentBlock {
	// Collect names of rejected tools from failed tool_use blocks
	rejectedNames := make(map[string]bool)
	for _, block := range blocks {
		if block.Type == "tool_use" && block.Status == "error" && strings.Contains(block.Output, "not found in agent cli") {
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
				"removing rejected tool_use block from CLI",
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

// ConvertAskQuestionBlocks detects <ask-question> tags in text ContentBlocks,
// parses the XML content, and converts them into tool_use ContentBlocks with
// name="AskUserQuestion". Tags are stripped from text; if no text remains the
// block is replaced entirely, otherwise a new tool_use block is appended.
//
// Tolerates three closing-tag variants:
//  1. Standard </ask-question>
//  2. Non-standard closing tags (e.g. </user_query>, obfuscated tags)
//  3. No closing tag at all (tag runs to end-of-text)
//
// Returns the updated blocks slice.
func ConvertAskQuestionBlocks(blocks []model.ContentBlock) []model.ContentBlock {
	// Pre-compiled regexes for the three matching strategies.
	reStandard := regexp.MustCompile(`<ask-question\b[^>]*>([\s\S]*?)</ask-question>`)
	reWrongClose := regexp.MustCompile(`<ask-question\b[^>]*>([\s\S]*?)</[^>]+>`)
	reUnclosed := regexp.MustCompile(`<ask-question\b[^>]*>([\s\S]+)$`)

	findAskMatch := func(text string) (string, int, int) {
		for _, re := range []*regexp.Regexp{reStandard, reWrongClose, reUnclosed} {
			matches := re.FindAllStringSubmatchIndex(text, -1)
			for j := len(matches) - 1; j >= 0; j-- {
				pair := matches[j]
				if candidate := extractXMLCandidate(text[pair[2]:pair[3]]); candidate != "" {
					return candidate, pair[0], pair[1]
				}
			}
		}
		return "", -1, -1
	}

	type conversion struct {
		index     int
		input     map[string]any
		cleanText string
	}
	var conversions []conversion

	for i, block := range blocks {
		if block.Type != "text" || !strings.Contains(block.Text, "<ask-question") {
			continue
		}

		xmlContent, tagStart, tagEnd := findAskMatch(block.Text)
		if xmlContent == "" {
			continue
		}

		input := parseAskQuestionXML(xmlContent)
		if input == nil {
			input = parseAskQuestionJSON(xmlContent)
		}
		if input == nil {
			slog.Error("failed to parse ask-question content (tried XML and JSON)")
			continue
		}

		questions, ok := input["questions"]
		if !ok {
			slog.Error("ask-question missing 'questions' field")
			continue
		}
		questionsArr, ok := questions.([]map[string]any)
		if !ok || len(questionsArr) == 0 {
			slog.Error("ask-question 'questions' must be a non-empty array")
			continue
		}

		cleanText := strings.TrimSpace(block.Text[:tagStart] + block.Text[tagEnd:])
		conversions = append(conversions, conversion{index: i, input: input, cleanText: cleanText})
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

	blocks = RemoveRejectedToolBlocks(blocks)

	return blocks
}

// extractXMLCandidate checks if the content between <ask-question> tags contains
// valid XML with <item> child elements or valid JSON with "questions" array.
func extractXMLCandidate(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if strings.Contains(trimmed, "<item>") || strings.Contains(trimmed, "<item ") {
		if !strings.Contains(trimmed, "<question>") || !strings.Contains(trimmed, "<option>") {
			return ""
		}
		return trimmed
	}
	if strings.HasPrefix(trimmed, "{") && strings.Contains(trimmed, `"questions"`) {
		var data map[string]any
		if err := json.Unmarshal([]byte(trimmed), &data); err != nil {
			return ""
		}
		questions, ok := data["questions"].([]any)
		if !ok || len(questions) == 0 {
			return ""
		}
		for _, q := range questions {
			qm, ok := q.(map[string]any)
			if !ok {
				continue
			}
			if _, hasQ := qm["question"]; hasQ {
				if opts, ok := qm["options"].([]any); ok && len(opts) > 0 {
					return trimmed
				}
			}
		}
		return ""
	}
	return ""
}

// parseAskQuestionXML parses XML-format <ask-question> content into the
// map[string]any format expected by ContentBlock.Input for "AskUserQuestion" tool.
func parseAskQuestionXML(xmlContent string) map[string]any {
	reItem := regexp.MustCompile(`(?s)<item>(.*?)</item>`)
	reHeader := regexp.MustCompile(`(?s)<header>(.*?)</header>`)
	reMultiSelect := regexp.MustCompile(`(?s)<multi-select>(.*?)</multi-select>`)
	reQuestion := regexp.MustCompile(`(?s)<question>(.*?)</question>`)
	reOption := regexp.MustCompile(`(?s)<option>(.*?)</option>`)
	reLabel := regexp.MustCompile(`(?s)<label>(.*?)</label>`)
	reDesc := regexp.MustCompile(`(?s)<description>(.*?)</description>`)

	itemMatches := reItem.FindAllStringSubmatch(xmlContent, -1)
	if len(itemMatches) == 0 {
		return nil
	}

	var questions []map[string]any
	for _, itemMatch := range itemMatches {
		itemContent := itemMatch[1]

		headerMatch := reHeader.FindStringSubmatch(itemContent)
		header := ""
		if headerMatch != nil {
			header = strings.TrimSpace(headerMatch[1])
		}

		multiSelectMatch := reMultiSelect.FindStringSubmatch(itemContent)
		multiSelect := false
		if multiSelectMatch != nil {
			multiSelect = strings.TrimSpace(multiSelectMatch[1]) == "true"
		}

		questionMatch := reQuestion.FindStringSubmatch(itemContent)
		if questionMatch == nil {
			continue
		}
		question := strings.TrimSpace(questionMatch[1])

		optionMatches := reOption.FindAllStringSubmatch(itemContent, -1)
		var options []map[string]any
		for _, optMatch := range optionMatches {
			optContent := optMatch[1]
			labelMatch := reLabel.FindStringSubmatch(optContent)
			if labelMatch == nil {
				continue
			}
			opt := map[string]any{"label": strings.TrimSpace(labelMatch[1])}
			descMatch := reDesc.FindStringSubmatch(optContent)
			if descMatch != nil {
				opt["description"] = strings.TrimSpace(descMatch[1])
			}
			options = append(options, opt)
		}

		if len(options) == 0 {
			continue
		}

		questions = append(questions, map[string]any{
			"header":      header,
			"multiSelect": multiSelect,
			"question":    question,
			"options":     options,
		})
	}

	if len(questions) == 0 {
		return nil
	}

	return map[string]any{"questions": questions}
}

// parseAskQuestionJSON parses JSON-format <ask-question> content into the
// map[string]any format expected by ContentBlock.Input for "AskUserQuestion" tool.
func parseAskQuestionJSON(jsonContent string) map[string]any {
	var data map[string]any
	if err := json.Unmarshal([]byte(jsonContent), &data); err != nil {
		return nil
	}

	rawQuestions, ok := data["questions"].([]any)
	if !ok || len(rawQuestions) == 0 {
		return nil
	}

	var questions []map[string]any
	for _, rq := range rawQuestions {
		item, ok := rq.(map[string]any)
		if !ok {
			continue
		}

		question, _ := item["question"].(string)
		if question == "" {
			continue
		}

		header, _ := item["header"].(string)
		_, multiSelect := item["multiSelect"].(bool)

		rawOptions, ok := item["options"].([]any)
		if !ok || len(rawOptions) == 0 {
			continue
		}

		var options []map[string]any
		for _, ro := range rawOptions {
			opt, ok := ro.(map[string]any)
			if !ok {
				continue
			}
			label, _ := opt["label"].(string)
			if label == "" {
				continue
			}
			entry := map[string]any{"label": label}
			if desc, ok := opt["description"].(string); ok && desc != "" {
				entry["description"] = desc
			}
			options = append(options, entry)
		}

		if len(options) == 0 {
			continue
		}

		questions = append(questions, map[string]any{
			"header":      header,
			"multiSelect": multiSelect,
			"question":    question,
			"options":     options,
		})
	}

	if len(questions) == 0 {
		return nil
	}

	return map[string]any{"questions": questions}
}
