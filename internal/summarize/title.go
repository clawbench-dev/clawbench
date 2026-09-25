package summarize

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"
)

// sessionTitlePrompt is the system prompt for auto-generating a chat session
// title from the user's messages. It asks for a single short title with no
// decoration, in the requested language.
const sessionTitlePrompt = `You are a chat session title generator. Based on the user's messages, produce a single short title that captures what the conversation is about.

Requirements:
1. Output only the title — no quotes, no markdown, no "Title:" prefix, no trailing punctuation.
2. Keep it short: at most ~20 Chinese characters, or at most ~8 English words.
3. Describe the user's subject or goal; never describe the conversation itself (e.g. "User asks about X").
4. Output in the requested language.
5. Output a single line — never a list, never multiple candidates.`

// MaxSessionTitleRunes caps the generated title length. Session titles are
// rendered in narrow list rows, so anything longer is truncated.
const MaxSessionTitleRunes = 50

// maxTitlePayloadRunes caps how much user text is sent to the model. A long
// session's full user history can be huge; the opening messages carry the topic,
// so the first N runes are kept and the rest dropped.
const maxTitlePayloadRunes = 8000

// GenerateSessionTitle produces a concise session title from the user's
// messages. It reuses the recommendation pass channel (DoRecommendPass) so both
// the OpenAI-compatible and Anthropic summarizers work without adding a new
// provider method: the title prompt is the system prompt and the joined user
// messages are the payload.
// Returns an error if the summarizer backend does not support LLM passes.
func GenerateSessionTitle(ctx context.Context, s Summarizer, userMessages []string, language string) (string, error) {
	pp, ok := s.(recommendPassProvider)
	if !ok {
		return "", fmt.Errorf("summarizer backend does not support session title generation")
	}
	payload := buildTitlePayload(userMessages)
	if payload == "" {
		return "", fmt.Errorf("no user messages to generate a title from")
	}
	prompt := sessionTitlePrompt + "\n\nOutput in " + languageName(language) + "."
	out, err := pp.DoRecommendPass(ctx, prompt, "", payload)
	if err != nil {
		return "", err
	}
	title := sanitizeSessionTitle(out)
	if title == "" {
		return "", fmt.Errorf("summarizer returned an empty title")
	}
	return title, nil
}

// buildTitlePayload joins the user's messages into the payload for the title
// pass. Empty messages are skipped; the result is capped at maxTitlePayloadRunes
// (keeping the opening messages, which establish the topic).
func buildTitlePayload(userMessages []string) string {
	var b strings.Builder
	for _, m := range userMessages {
		m = strings.TrimSpace(m)
		if m == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(m)
	}
	joined := strings.TrimSpace(b.String())
	if runes := []rune(joined); len(runes) > maxTitlePayloadRunes {
		joined = string(runes[:maxTitlePayloadRunes])
	}
	return joined
}

// sanitizeSessionTitle turns the model's raw output into a single-line title:
// keeps the first non-empty line, strips markdown heading/bullet markers and
// matching wrappers (bold, quotes, backticks), collapses whitespace, and
// truncates to MaxSessionTitleRunes.
func sanitizeSessionTitle(raw string) string {
	var line string
	for _, l := range strings.Split(raw, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			line = l
			break
		}
	}
	// Markdown heading marker: '#' repeated, followed by a space. The space is
	// required so a title like "#1 优先修复" is not mangled into "1 优先修复".
	if rest := strings.TrimLeft(line, "#"); rest != line && strings.HasPrefix(rest, " ") {
		line = strings.TrimSpace(rest)
	}
	// Bullet markers and a "Title:" / "标题：" preamble some models add anyway.
	line = strings.TrimSpace(strings.TrimPrefix(line, "- "))
	line = strings.TrimSpace(strings.TrimPrefix(line, "* "))
	line = strings.TrimSpace(strings.TrimPrefix(line, "Title:"))
	line = strings.TrimSpace(strings.TrimPrefix(line, "标题："))
	line = strings.TrimSpace(strings.TrimPrefix(line, "标题:"))
	// Matching wrappers only — never strip from one side alone, so a title
	// legitimately ending in a quote keeps its character.
	for _, pair := range [][2]string{{"**", "**"}, {`"`, `"`}, {"'", "'"}, {"`", "`"}} {
		if len(line) > len(pair[0])+len(pair[1]) && strings.HasPrefix(line, pair[0]) && strings.HasSuffix(line, pair[1]) {
			line = line[len(pair[0]) : len(line)-len(pair[1])]
		}
	}
	line = strings.Join(strings.Fields(line), " ")
	if utf8.RuneCountInString(line) > MaxSessionTitleRunes {
		line = string([]rune(line)[:MaxSessionTitleRunes])
	}
	return strings.TrimSpace(line)
}
