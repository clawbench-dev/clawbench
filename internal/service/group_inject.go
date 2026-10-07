package service

import (
	"fmt"
	"strings"

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
//   - hostID is the host member's ROW id. Its speech has the routing tag
//     stripped (decision #67): the tag is internal protocol, and leaving it in
//     would invite the member to imitate `<clawbench-speaker>`. Only the tag's
//     background survives; an unparseable tag is kept verbatim (but any
//     well-formed private note is still removed — see hostSpeechForMembers).
//
// User messages are rendered as "用户: ..."; member speech as "<name>: ...";
// membership changes (role='system') as "[系统] ..." so they are never mistaken
// for user speech.
func buildInjectionText(msgs []model.ChatMessage, cursor int64, self string, names map[string]string, leftIDs map[string]bool, roster []ParticipantInfo, instruction, bcc, hostID string) string {
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
		text := strings.TrimSpace(ExtractPlainText(m.Content))
		if text == "" {
			continue
		}
		if line := renderTimelineLine(m, text, names, leftIDs, hostID); line != "" {
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
// "Claude 加入了讨论". The host routes from this context, so mis-attributing a
// membership event as user speech corrupts its decisions. Extracted from
// buildInjectionText to keep that function's complexity down.
func renderTimelineLine(m model.ChatMessage, text string, names map[string]string, leftIDs map[string]bool, hostID string) string {
	if m.Role == "system" {
		return "[系统] " + text + "\n"
	}
	if m.Role == roleUser || m.AgentID == "" {
		// A user message's attachments are rendered into the INJECTION only
		// (decision #65): the bubble stays clean, but a member must know a
		// file was attached or it cannot discuss it. Same formatter as single
		// chat so the two cannot drift.
		return "用户: " + userTextWithAttachments(text, m.Files) + "\n"
	}
	if hostID != "" && m.AgentID == hostID {
		text = hostSpeechForMembers(text)
	}
	name := names[m.AgentID]
	if name == "" {
		name = "成员"
	}
	if leftIDs[m.AgentID] {
		name += "（已离场）"
	}
	return name + ": " + text + "\n"
}

// appendHostDirectives renders the host's closing instructions: the PUBLIC
// directive (every addressed member receives it) first, then the PRIVATE note
// (this member alone) after it — the "叠加" contract. Each is omitted when
// empty. Extracted from buildInjectionText to keep that function's branching
// under the complexity budget.
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
		b.WriteString("主持人密送给你（其他成员看不到）：")
		b.WriteString(bcc)
		b.WriteString("\n")
	}
}

// hostSpeechForMembers renders a host message for a member's context: the
// routing tag is replaced by the text that preceded it (decision #67/#68).
//
// Contracts:
//   - The SPEAKER tag is stripped only when it parses (Found). An unparseable
//     speaker tag is passed through verbatim (same "never lose content" rule as
//     askquestion) rather than guessing where its payload ends.
//   - The END tag is ALWAYS stripped, even when no speaker tag is present. It is
//     internal protocol too, and a message that carries only the end signal has
//     Found=false — gating its removal on Found would leak it to members and
//     invite them to imitate it (decision #67).
//   - Well-formed BCC notes are ALWAYS stripped, for the same reason and by the
//     same rule: a note is addressed to specific members, and the message that
//     carries it reaches every member through this function. A message with no
//     speaker tag (Found=false) still gets its notes removed — that fallback
//     path is the one place a note could otherwise leak to everyone.
//   - The directive is not repeated: it reaches the member once, in the
//     high-priority "主持人要求你：…" paragraph the caller appends. Keeping it
//     in the body too would tell the member the host emphasised it twice.
//
// A message with no tag at all (the host just talking) is returned unchanged.
func hostSpeechForMembers(text string) string {
	res := grouprouting.Parse(text)
	if !res.Found {
		// Fail-closed: a note of ANY shape must not reach a member through this
		// fallback. StripBccTags (display-side) only removes well-formed notes,
		// so it is NOT enough here — an LLM mis-formatting a quote would leak.
		return grouprouting.StripBccSpans(grouprouting.StripEndTag(text))
	}
	return res.Before
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

// groupInjectionText loads a member's incremental context from the group
// timeline. It is a thin DB wrapper over buildInjectionText used by the
// orchestrator.
func groupInjectionText(groupID, selfMemberRowID string, cursor int64, names map[string]string, instruction, bcc string) (string, error) {
	msgs, err := GetMessagesBySessionIDRaw(groupID)
	if err != nil {
		return "", fmt.Errorf("load group timeline: %w", err)
	}
	members, err := ListGroupMembers(groupID)
	if err != nil {
		return "", fmt.Errorf("load group members: %w", err)
	}
	leftIDs, roster := participantMetadata(members, selfMemberRowID)
	// The host's own speech keeps its tags (the host reads them back as its own
	// prior routing decisions); only OTHER members get the stripped form.
	hostID := GetGroupHostMember(groupID)
	if hostID == selfMemberRowID {
		hostID = ""
	}
	return buildInjectionText(msgs, cursor, selfMemberRowID, names, leftIDs, roster, instruction, bcc, hostID), nil
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
