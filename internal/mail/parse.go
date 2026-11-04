package mail

import (
	"bytes"
	"html"
	"regexp"
	"strings"

	"github.com/jhillyerd/enmime"
)

// Parse decodes a complete RFC 5322 message into the normalized shape the UI
// consumes. enmime handles the parts that are easy to get wrong: RFC 2047
// encoded headers, GBK/GB18030 bodies, quoted-printable, base64, nested
// multiparts and content-id inline images.
func Parse(raw []byte) (*ParsedMessage, error) {
	env, err := enmime.ReadEnvelope(bytes.NewReader(raw))
	if err != nil {
		return nil, &Error{Kind: KindServer, Op: "parse message", Err: err}
	}

	pm := &ParsedMessage{
		Subject: env.GetHeader("Subject"),
		Text:    env.Text,
		HTML:    env.HTML,
	}

	seen := make(map[string]bool)
	add := func(p *enmime.Part, inline bool) {
		key := p.PartID
		if key == "" {
			key = p.FileName + "|" + p.ContentID
		}
		if seen[key] {
			return
		}
		seen[key] = true
		name := p.FileName
		if name == "" && p.ContentID == "" {
			return // a nameless non-inline part carries nothing the user can act on
		}
		if name == "" {
			name = "inline-" + p.ContentID
		}
		pm.Attachments = append(pm.Attachments, Attachment{
			PartID:      p.PartID,
			Filename:    name,
			ContentType: p.ContentType,
			Size:        int64(len(p.Content)),
			ContentID:   p.ContentID,
			Inline:      inline,
			Content:     p.Content,
		})
	}
	for _, p := range env.Attachments {
		add(p, false)
	}
	for _, p := range env.Inlines {
		add(p, true)
	}
	// multipart/related images often carry no Content-Disposition at all, so
	// they end up in OtherParts while the HTML still references them by cid:.
	for _, p := range env.OtherParts {
		if p.ContentID != "" {
			add(p, true)
		}
	}
	return pm, nil
}

var (
	// RE2 has no backreferences, so script and style need separate patterns.
	scriptRe  = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	styleRe   = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	commentRe = regexp.MustCompile(`(?s)<!--.*?-->`)
	blockTag  = regexp.MustCompile(`(?i)</?(p|div|br|tr|li|h[1-6]|table|blockquote)\b[^>]*>`)
	anyTag    = regexp.MustCompile(`(?s)<[^>]*>`)
	spaceRe   = regexp.MustCompile(`[ \t\r\f\v]+`)
	nlRe      = regexp.MustCompile(`\n{2,}`)
)

// Snippet returns a one-line preview from the best available body text.
func Snippet(text, htmlBody string, maxRunes int) string {
	s := strings.TrimSpace(text)
	if s == "" {
		s = plainFromHTML(htmlBody)
	}
	s = spaceRe.ReplaceAllString(s, " ")
	s = nlRe.ReplaceAllString(s, "\n")
	s = strings.TrimSpace(s)
	if maxRunes > 0 {
		r := []rune(s)
		if len(r) > maxRunes {
			s = string(r[:maxRunes])
		}
	}
	return s
}

func plainFromHTML(s string) string {
	if s == "" {
		return ""
	}
	s = scriptRe.ReplaceAllString(s, " ")
	s = styleRe.ReplaceAllString(s, " ")
	s = commentRe.ReplaceAllString(s, " ")
	s = blockTag.ReplaceAllString(s, "\n")
	s = anyTag.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	return strings.TrimSpace(s)
}
