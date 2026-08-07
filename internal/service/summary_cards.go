package service

import (
	"regexp"
	"strings"

	"clawbench/internal/model"
)

var (
	scheduledTaskIDRe = regexp.MustCompile(`<scheduled-task\s+id="(\d+)"`)
	askQuestionRe     = regexp.MustCompile(`(?s)<ask-question>(.*?)</ask-question>`)
	askOptionRe       = regexp.MustCompile(`(?s)<option>\s*(?:<label>)?(.*?)(?:</label>)?\s*</option>`)
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
					Name:  b.Name,
					ID:    b.ID,
					Input: b.Input,
				})
			}
		case contentKeyText:
			for _, m := range scheduledTaskIDRe.FindAllStringSubmatch(b.Text, -1) {
				var id int64
				for _, c := range m[1] {
					id = id*10 + int64(c-'0')
				}
				cards.TaskIDs = append(cards.TaskIDs, id)
			}
			for _, m := range askQuestionRe.FindAllStringSubmatch(b.Text, -1) {
				inner := m[1]
				card := model.AskQuestionCard{Text: stripXMLTags(inner)}
				for _, om := range askOptionRe.FindAllStringSubmatch(inner, -1) {
					card.Options = append(card.Options, strings.TrimSpace(stripXMLTags(om[1])))
				}
				cards.AskQuestions = append(cards.AskQuestions, card)
			}
		}
	}
	return cards
}

func stripXMLTags(s string) string {
	var b strings.Builder
	depth := 0
	for i := range s {
		if s[i] == '<' {
			depth++
			continue
		}
		if s[i] == '>' {
			if depth > 0 {
				depth--
			}
			continue
		}
		if depth == 0 {
			b.WriteByte(s[i])
		}
	}
	return strings.TrimSpace(b.String())
}
