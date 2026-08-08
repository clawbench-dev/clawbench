package service

import (
	"regexp"
	"strings"

	"clawbench/internal/model"
)

var (
	scheduledTaskIDRe = regexp.MustCompile(`<scheduled-task\s+id="(\d+)"`)
	askQuestionBlockRe = regexp.MustCompile(`(?s)<ask-question>(.*?)</ask-question>`)
	askItemRe          = regexp.MustCompile(`(?s)<item>(.*?)</item>`)
	askHeaderRe        = regexp.MustCompile(`(?s)<header>(.*?)</header>`)
	askQuestionRe      = regexp.MustCompile(`(?s)<question>(.*?)</question>`)
	askMultiRe         = regexp.MustCompile(`(?s)<multi-select>\s*(\w+)\s*</multi-select>`)
	askOptionRe        = regexp.MustCompile(`(?s)<option>(.*?)</option>`)
	askLabelRe         = regexp.MustCompile(`(?s)<label>(.*?)</label>`)
	askDescRe          = regexp.MustCompile(`(?s)<description>(.*?)</description>`)
	xmlTagRe           = regexp.MustCompile(`</?[a-zA-Z][^>]*>`)
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
			for _, block := range askQuestionBlockRe.FindAllStringSubmatch(b.Text, -1) {
				inner := block[1]
				// Each <item> is one question card
				for _, im := range askItemRe.FindAllStringSubmatch(inner, -1) {
					item := im[1]
					card := model.AskQuestionCard{
						Header:   firstMatch(askHeaderRe, inner),
						Question: firstMatch(askQuestionRe, item),
					}
					mm := askMultiRe.FindStringSubmatch(item)
					if len(mm) == 2 {
						card.MultiSelect = strings.TrimSpace(mm[1]) == "true"
					}
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
					if card.Question != "" && len(card.Options) > 0 {
						cards.AskQuestions = append(cards.AskQuestions, card)
					}
				}
			}
		}
	}
	return cards
}

// firstMatch returns the trimmed text of the first subexpression match, or "".
func firstMatch(re *regexp.Regexp, s string) string {
	m := re.FindStringSubmatch(s)
	if len(m) == 2 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

// stripXMLTags removes well-formed XML tags from s, keeping bare < and >
// (e.g. comparison operators like "score < 5") intact.
func stripXMLTags(s string) string {
	return strings.TrimSpace(xmlTagRe.ReplaceAllString(s, ""))
}
