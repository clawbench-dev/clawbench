package service

import (
	"regexp"
	"strconv"
	"strings"

	"clawbench/internal/model"
)

var (
	scheduledTaskIDRe  = regexp.MustCompile(`<scheduled-task\s+id="(\d+)"`)
	askQuestionBlockRe = regexp.MustCompile(`(?s)<ask-question>(.*?)</ask-question>`)
	askItemRe          = regexp.MustCompile(`(?s)<item>(.*?)</item>`)
	askHeaderRe        = regexp.MustCompile(`(?s)<header>(.*?)</header>`)
	askQuestionRe      = regexp.MustCompile(`(?s)<question>(.*?)</question>`)
	askMultiRe         = regexp.MustCompile(`(?s)<multi-select>\s*(\w+)\s*</multi-select>`)
	askOptionRe        = regexp.MustCompile(`(?s)<option>(.*?)</option>`)
	askLabelRe         = regexp.MustCompile(`(?s)<label>(.*?)</label>`)
	askDescRe          = regexp.MustCompile(`(?s)<description>(.*?)</description>`)
)

// isSummaryCardTool reports whether a tool_use block should be persisted into
// summaryCards.tools. Only AskUserQuestion qualifies: PermissionApproval is an
// actionable prompt bound to a live session (responding needs the session ID +
// tool call ID), so in the read-only summary view it would only ever render a
// button that cannot work — it is omitted entirely.
func isSummaryCardTool(name string) bool {
	return strings.EqualFold(name, "askuserquestion")
}

// extractSummaryCards walks content blocks and builds the compact card
// metadata persisted in summaries.summary_cards. Only answerable tool_use
// blocks, scheduled-task IDs, and <ask-question> cards are retained.
func extractSummaryCards(blocks []model.ContentBlock) *model.SummaryCards {
	cards := &model.SummaryCards{}
	for _, b := range blocks {
		// Sub-agent content (parent_tool_call_id set) is rendered nested under its
		// Agent card, not at the top level. In the summary view the heavy blocks
		// are stripped and only these cards survive — so a sub-agent's tool calls
		// must NOT leak into the top-level summary cards (they would masquerade as
		// the main agent's actions). Skip them here; the summary view is a compact
		// top-level digest.
		if b.ParentToolCallID != "" {
			continue
		}
		switch b.Type {
		case "tool_use":
			if isSummaryCardTool(b.Name) {
				cards.Tools = append(cards.Tools, model.SummaryTool{
					Name:   b.Name,
					ID:     b.ID,
					Input:  b.Input,
					Done:   b.Done,
					Status: b.Status,
					Output: b.Output,
				})
			}
			if b.Done {
				extractFileChanges(cards, b)
			}
		case contentKeyText:
			extractFromText(cards, b.Text)
		case blockTypeWarning, eventTypeError:
			// Preserve warning/error banners across the summary view, where the
			// heavy content blocks are stripped (content = {"blocks":[]}).
			cards.Warnings = append(cards.Warnings, model.SummaryWarning{
				Type:        b.Type,
				Text:        b.Text,
				Reason:      b.Reason,
				ErrorCode:   b.ErrorCode,
				HTTPStatus:  b.HTTPStatus,
				ErrorSource: b.ErrorSource,
			})
		}
	}
	return cards
}

// extractFileChanges records created/modified file paths from a done tool_use
// block, mirroring the frontend extractFileChanges semantics (Write → created,
// Edit → modified, deduplicated). Requires a detected file path. The tool call
// ID is captured per path so the frontend can fetch the diff on demand.
func extractFileChanges(cards *model.SummaryCards, b model.ContentBlock) {
	path := b.FilePath
	if path == "" {
		if p, ok := b.Input["file_path"].(string); ok {
			path = p
		}
	}
	if path == "" {
		return
	}
	switch b.Name {
	case "Write":
		cards.CreatedFiles = appendFileChange(cards.CreatedFiles, path, b.ID)
	case "Edit":
		cards.ModifiedFiles = appendFileChange(cards.ModifiedFiles, path, b.ID)
	}
}

// appendFileChange appends a path to the change list, merging the tool ID into
// an existing entry for the same path (deduplicated per path and per ID).
func appendFileChange(list model.SummaryFileChanges, path, toolID string) model.SummaryFileChanges {
	for i := range list {
		if list[i].Path == path {
			if toolID != "" && !containsString(list[i].ToolIDs, toolID) {
				list[i].ToolIDs = append(list[i].ToolIDs, toolID)
			}
			return list
		}
	}
	fc := model.SummaryFileChange{Path: path}
	if toolID != "" {
		fc.ToolIDs = []string{toolID}
	}
	return append(list, fc)
}

// containsString reports whether s is present in slice.
func containsString(slice []string, s string) bool {
	for _, existing := range slice {
		if existing == s {
			return true
		}
	}
	return false
}

// extractFromText parses scheduled-task IDs and ask-question cards from a text block.
func extractFromText(cards *model.SummaryCards, text string) {
	for _, m := range scheduledTaskIDRe.FindAllStringSubmatch(text, -1) {
		id, err := strconv.ParseInt(m[1], 10, 64)
		if err != nil {
			continue
		}
		cards.TaskIDs = append(cards.TaskIDs, id)
	}
	for _, block := range askQuestionBlockRe.FindAllStringSubmatch(text, -1) {
		parseAskQuestionItems(cards, block[1])
	}
}

// parseAskQuestionItems extracts individual <item> cards from an <ask-question> block.
func parseAskQuestionItems(cards *model.SummaryCards, inner string) {
	for _, im := range askItemRe.FindAllStringSubmatch(inner, -1) {
		item := im[1]
		card := model.AskQuestionCard{
			Header:   firstMatch(askHeaderRe, item),
			Question: firstMatch(askQuestionRe, item),
		}
		mm := askMultiRe.FindStringSubmatch(item)
		if len(mm) == 2 {
			card.MultiSelect = strings.TrimSpace(mm[1]) == "true"
		}
		parseAskOptions(&card, item)
		if card.Question != "" && len(card.Options) > 0 {
			cards.AskQuestions = append(cards.AskQuestions, card)
		}
	}
}

// parseAskOptions extracts <option> entries from an <item> block.
func parseAskOptions(card *model.AskQuestionCard, item string) {
	for _, om := range askOptionRe.FindAllStringSubmatch(item, -1) {
		optText := om[1]
		opt := model.AskQuestionOption{Label: firstMatch(askLabelRe, optText)}
		if d := firstMatch(askDescRe, optText); d != "" {
			opt.Description = d
		}
		if opt.Label != "" {
			card.Options = append(card.Options, opt)
		}
	}
}

// firstMatch returns the trimmed text of the first subexpression match, or "".
func firstMatch(re *regexp.Regexp, s string) string {
	m := re.FindStringSubmatch(s)
	if len(m) == 2 {
		return strings.TrimSpace(m[1])
	}
	return ""
}
