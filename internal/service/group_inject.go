package service

import (
	"fmt"
	"strings"

	"clawbench/internal/askquestion"
	"clawbench/internal/grouprouting"
	"clawbench/internal/model"
)

// group_inject.go builds the incremental context a group member sees before it
// speaks. Members only exchange SPEECH TEXT — tool calls and thinking are
// private (design §5.2 / decision #9).

// ParticipantInfo is one active participant shown in the injection header so a
// member knows who it is discussing with (decision #41). The header is rendered
// once at the top, not repeated per message.
type ParticipantInfo struct {
	Name      string
	Specialty string
}

// buildInjectionText renders the group timeline slice a member should see.
//
//   - msgs is the group timeline in id order.
//   - cursor is the member's seen_cursor: only messages with id > cursor are
//     included (the member's own past speech is excluded by the author filter).
//   - self is the speaking member's ROW id (matches msg.AgentID).
//   - names maps a member row id -> display name; unknown ids fall back to a
//     short placeholder so a removed/renamed member's speech is still labeled.
//   - leftIDs marks members that have left: their speech stays on the timeline
//     (the cursor is not reset on leave) but is labeled "（已离场）" so the
//     reader does not treat it as an active participant (decision #39).
//   - roster lists the OTHER active participants for the header. It is omitted
//     entirely when no participant has a specialty (a bare "参与者：" line is
//     pure noise).
//   - instruction is the host's directive to this member, rendered as a
//     high-priority final paragraph (empty = omitted).
//   - bcc is the host's PRIVATE note to THIS member only, rendered after the
//     public instruction as an even-higher-priority closing paragraph (empty =
//     omitted). The caller filters the host's notes down to this member's name,
//     so no other member ever receives another member's note.
//
// Every agent's speech has its mention tags rendered as readable "@name" prose
// (see renderMentionsReadable): a public mention's body survives, a private one
// is dropped (fail-closed), and the end signal is removed. This is mode-agnostic
// — in free mode a member's @ must be visible to the relay chain, and in host
// mode it keeps members from imitating the raw routing format.
//
// User messages are rendered as "User: ..."; member speech as "<name>: ...";
// membership changes (role='system') as "[系统] ..." so they are never mistaken
// for user speech.
func buildInjectionText(msgs []model.ChatMessage, cursor int64, self string, names map[string]string, leftIDs map[string]bool, roster []ParticipantInfo, instruction, bcc string) string {
	var b strings.Builder
	if header := participantHeader(roster); header != "" {
		b.WriteString(header)
		b.WriteString("\n")
	}
	for _, m := range msgs {
		if m.ID <= cursor {
			continue
		}
		if m.AgentID != "" && m.AgentID == self {
			continue // never replay the member's own past speech
		}
		text := strings.TrimSpace(stripAskQuestionTags(ExtractPlainText(m.Content)))
		questions := askQuestionTexts(m.Content)
		if text == "" && len(questions) == 0 {
			continue
		}
		if line := renderTimelineLine(m, text, questions, names, leftIDs); line != "" {
			b.WriteString(line)
		}
	}
	if instruction != "" || bcc != "" {
		appendHostDirectives(&b, instruction, bcc)
	}
	return b.String()
}

// renderTimelineLine renders one timeline row for a member's injected context.
// A role='system' row is a membership change (decisions #40/#43): its agent_id
// is "" by design, so it must be checked BEFORE the user branch — otherwise the
// `AgentID == ""` fallback captures it and tells the reader the USER said
// "Claude 加入了讨论". Mis-attributing a membership event as user speech
// corrupts the reader's decisions. Extracted from buildInjectionText to keep
// that function's complexity down.
//
// Agent speech has its mention tags rendered readable (public bodies survive,
// private notes dropped, end signal removed) — see renderMentionsReadable.
//
// questions holds the ask-question cards this message carried (already
// rendered as plain text). They are appended as a directional line so the
// reader knows the question is addressed to the USER, not to it — without that
// a member reads the question as ordinary discussion and may answer it.
func renderTimelineLine(m model.ChatMessage, text string, questions []string, names map[string]string, leftIDs map[string]bool) string {
	if m.Role == "system" {
		return "[系统] " + text + "\n"
	}
	if m.Role == roleUser || m.AgentID == "" {
		// A user message's attachments are rendered into the INJECTION only
		// (decision #65): the bubble stays clean, but a member must know a
		// file was attached or it cannot discuss it. Same formatter as single
		// chat so the two cannot drift.
		// The prefix is the user's reserved participant name (English, language
		// neutral) — it is the SAME name the host addresses in a routing tag,
		// so a member sees the user as an addressable participant, not as an
		// anonymous "user:" line.
		//
		// A user's @-mention carries a member ROW ID (the frontend writes ids),
		// so it is rendered readable AND resolved to the display name — a
		// member must see "@Alice", not the raw protocol tag with a UUID.
		userText := renderMentionsReadable(userTextWithAttachments(text, m.Files), names)
		return groupUserTarget() + ": " + userText + "\n"
	}
	name := names[m.AgentID]
	if name == "" {
		name = "成员"
	}
	if leftIDs[m.AgentID] {
		name += "（已离场）"
	}
	line := name + ": " + renderMentionsReadable(text, names)
	if q := renderAskQuestions(name, questions); q != "" {
		if strings.TrimSpace(text) != "" {
			line += "\n"
		}
		line += q
	}
	return line + "\n"
}

// renderAskQuestions renders the ask-question cards one member addressed to the
// USER as a directional line, so another member reads it as a question awaiting
// the human's answer rather than as discussion it should join.
func renderAskQuestions(speakerName string, questions []string) string {
	var b strings.Builder
	for _, q := range questions {
		q = strings.TrimSpace(q)
		if q == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("[" + speakerName + " 向用户提问，等待用户回答] ")
		b.WriteString(q)
	}
	return b.String()
}

// askQuestionTexts extracts the questions a persisted message put to the user,
// rendered as readable plain text (question + options), from EITHER persisted
// shape:
//
//   - a tool_use block named AskUserQuestion — the CONVERTED form
//     postProcessBlocks writes for a well-formed payload (the normal case);
//   - a text block still carrying the raw <clawbench-ask-question> tag — the
//     pre-conversion form, matched so a conversion gap or an unparseable
//     payload still surfaces the question rather than dropping it.
//
// Rendering reuses askquestion.PlainText (the same summary TTS and the
// recommendation prompt use), so the injected wording cannot drift from the
// spoken one. Non-question tool_use blocks yield nothing.
func askQuestionTexts(content string) []string {
	blocks, err := parseMessageBlocks(content)
	if err != nil {
		return nil
	}
	var out []string
	for _, b := range blocks {
		switch b.Type {
		case eventTypeToolUse:
			if !strings.EqualFold(b.Name, "AskUserQuestion") {
				continue
			}
			if items := askquestion.NormalizeInput(b.Input); len(items) > 0 {
				if s := strings.TrimSpace(askquestion.PlainText(items)); s != "" {
					out = append(out, s)
				}
			}
		case contentKeyText:
			for _, m := range askquestion.Extract(b.Text) {
				if !m.Parsed {
					continue
				}
				if s := strings.TrimSpace(askquestion.PlainText(m.Items)); s != "" {
					out = append(out, s)
				}
			}
		}
	}
	return out
}

// stripAskQuestionTags removes every parsed <clawbench-ask-question> span from a
// text block's plain text, so the raw protocol never reaches another member (it
// would invite imitation, the same incident the mention tags caused — decision
// #67). The question itself is not lost: it is re-added by renderAskQuestions
// as readable prose. An UNPARSED span is left untouched (the askquestion
// "never drop content" contract) — it degrades to readable Markdown, which is
// also what a member sees.
func stripAskQuestionTags(text string) string {
	if !strings.Contains(text, "<clawbench-ask-question") {
		return text
	}
	return askquestion.Strip(text, askquestion.Extract(text))
}

// appendHostDirectives renders the host's closing instructions: the PUBLIC
// directive (every addressed member receives it) first, then the PRIVATE notes
// (this member alone) after it — the "叠加" contract. Each is omitted when
// empty. Extracted from buildInjectionText to keep that function's branching
// under the complexity budget.
//
// bcc arrives ALREADY attributed: joinPendingBcc prefixes each note with its own
// sender line, because the sender is not always the host (a free-mode peer, or
// the user, may be the author). Adding a blanket "主持人密送给你" header here
// would mis-attribute those — it used to, which is exactly what this split
// fixes.
func appendHostDirectives(b *strings.Builder, instruction, bcc string) {
	if instruction != "" {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("主持人要求你：")
		b.WriteString(instruction)
		b.WriteString("\n")
	}
	if bcc != "" {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(bcc)
		b.WriteString("\n")
	}
}

// pendingBccHeader renders the "who sent this" prefix for one delivered note.
//
// An empty SenderRole means the row predates the sender_role column (or the
// sender could not be resolved). Those fall back to the host wording, which is
// what every note used to say — so an undelivered note written before the
// upgrade still reads exactly as it did.
//
// It lives here, next to the other injection framing, because these strings are
// fed to a MODEL, never rendered to a user (see the CJK allowlist in
// i18n_hardcoded_guard_test.go).
func pendingBccHeader(p pendingBcc) string {
	switch p.SenderRole {
	case bccRoleMember:
		name := strings.TrimSpace(p.SenderName)
		if name == "" {
			name = "成员"
		}
		return name + " 密送给你（其他成员看不到）："
	case bccRoleUser:
		return groupUserTarget() + " 密送给你（其他成员看不到）："
	default: // bccRoleHost, or "" for a pre-upgrade row
		return "主持人密送给你（其他成员看不到）："
	}
}

// renderMentionsReadable rewrites each public mention as "@targets 内容" and
// drops private ones (fail-closed), then strips the end signal. It is how
// speech is presented to OTHER participants so they can read who was addressed
// and learn the @ syntax from examples — used in BOTH modes:
//
//   - host mode: the host's routing tags are internal protocol; a member that
//     saw them would imitate the format (the real "everyone declares itself the
//     chair" incident), so only the public body survives;
//   - free mode: a member's @ IS the relay signal, so it must be visible to the
//     chain — rendered readable rather than raw so no one copies the tag shape.
//
// names maps a member ROW id to its display name: a USER's @-mention carries the
// row id (the frontend writes ids), so without this the reader would see
// "@<uuid>". An unknown target falls back to the raw target so the routing
// intent stays visible.
func renderMentionsReadable(text string, names map[string]string) string {
	cleaned := grouprouting.ReplaceMentions(text, func(e *grouprouting.MentionEntry) string {
		if e == nil || e.Private {
			return "" // malformed or private: drop the whole span
		}
		labels := make([]string, 0, len(e.Targets))
		for _, t := range e.Targets {
			if n := names[t]; n != "" {
				labels = append(labels, n)
			} else {
				labels = append(labels, t)
			}
		}
		at := "@" + strings.Join(labels, " @")
		if e.Content == "" {
			return at
		}
		return at + " " + e.Content
	})
	return strings.TrimSpace(grouprouting.StripEndTag(cleaned))
}

// userTextWithAttachments prepends the attachment summary to a user message's
// text for the injected context (decision #65). It reuses the single-chat
// formatter (model.ClassifyAttachments + model.ApplyAttachmentPrefixes) rather
// than writing a second format that could drift.
//
// The excludePaths set is nil: in this path the attachment list IS the files
// channel (there is no separate "current file" argument), so nothing must be
// filtered out. A message with no attachments is returned unchanged — the
// formatter already omits empty buckets, but skipping the call keeps the
// common case allocation-free.
func userTextWithAttachments(text string, files []model.FileEntry) string {
	if len(files) == 0 {
		return text
	}
	parts := model.ClassifyAttachments(files, nil)
	return model.ApplyAttachmentPrefixes(text, nil, nil, parts)
}

// participantHeader renders the "参与者：A（描述）、B（描述）" line, or "" when
// no participant carries a specialty (the header would carry no information).
func participantHeader(roster []ParticipantInfo) string {
	anySpecialty := false
	for _, p := range roster {
		if p.Specialty != "" {
			anySpecialty = true
			break
		}
	}
	if !anySpecialty {
		return ""
	}
	rendered := make([]string, 0, len(roster))
	for _, p := range roster {
		if p.Specialty == "" {
			rendered = append(rendered, p.Name)
			continue
		}
		rendered = append(rendered, p.Name+"（"+p.Specialty+"）")
	}
	return "参与者：" + strings.Join(rendered, "、")
}

// groupInjectionTextWithRoster loads a member's incremental context from the
// group timeline and renders it. It is the single DB wrapper over
// buildInjectionText: callers pass the roster they already hold, so the query
// for it is not repeated per member turn.
//
// It reads only the rows AFTER cursor: everything at or below it is discarded
// by buildInjectionText anyway, and a group timeline grows without bound, so
// loading it whole made the cost of one member turn proportional to the entire
// conversation.
func groupInjectionTextWithRoster(groupID, selfMemberRowID string, cursor int64, names map[string]string, members []GroupMember, instruction, bcc string) (string, error) {
	msgs, err := GetMessagesBySessionIDRawAfter(groupID, cursor)
	if err != nil {
		return "", fmt.Errorf("load group timeline: %w", err)
	}
	leftIDs, roster := participantMetadata(members, selfMemberRowID)
	return buildInjectionText(msgs, cursor, selfMemberRowID, names, leftIDs, roster, instruction, bcc), nil
}

// participantMetadata derives the left-member set and the active-participant
// roster (excluding the speaker) from the group's members.
func participantMetadata(members []GroupMember, selfMemberRowID string) (map[string]bool, []ParticipantInfo) {
	leftIDs := make(map[string]bool)
	roster := make([]ParticipantInfo, 0, len(members))
	for _, m := range members {
		if m.Left {
			leftIDs[m.ID] = true
			continue
		}
		if m.ID == selfMemberRowID {
			continue // a member does not need to be told about itself
		}
		roster = append(roster, ParticipantInfo{Name: m.Name, Specialty: GetAgentSpecialty(m.AgentID)})
	}
	return leftIDs, roster
}
