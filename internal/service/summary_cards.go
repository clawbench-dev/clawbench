package service

import (
	"regexp"
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

// isAutoExpandTool reports whether a tool_use block should be shown as a card
// in summary view. Mirrors the frontend shouldAutoExpandTool set.
func isAutoExpandTool(name string) bool {
	n := strings.ToLower(name)
	return n == "askuserquestion" || n == "permissionapproval"
}

// extractSummaryCards walks content blocks and builds the compact card
// metadata persisted in summaries.summary_cards. Only tool_use blocks that
// auto-expand, scheduled-task IDs, and <ask-question> cards are retained.
func extractSummaryCards(blocks []model.ContentBlock) *model.SummaryCards {
	cards := &model.SummaryCards{}
	for _, b := range blocks {
		switch b.Type {
		case "tool_use":
			if isAutoExpandTool(b.Name) {
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
		var id int64
		for _, c := range m[1] {
			id = id*10 + int64(c-'0')
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
