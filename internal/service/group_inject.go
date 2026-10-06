package service

import (
	"fmt"
	"strings"

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
//
// User messages are rendered as "用户: ..."; member speech as "<name>: ...".
func buildInjectionText(msgs []model.ChatMessage, cursor int64, self string, names map[string]string, leftIDs map[string]bool, roster []ParticipantInfo, instruction string) string {
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
		if m.Role == "user" || m.AgentID == "" {
			b.WriteString("用户: ")
			b.WriteString(text)
			b.WriteString("\n")
			continue
		}
		name := names[m.AgentID]
		if name == "" {
			name = "成员"
		}
		b.WriteString(name)
		if leftIDs[m.AgentID] {
			b.WriteString("（已离场）")
		}
		b.WriteString(": ")
		b.WriteString(text)
		b.WriteString("\n")
	}
	if instruction != "" {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("主持人要求你：")
		b.WriteString(instruction)
		b.WriteString("\n")
	}
	return b.String()
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
func groupInjectionText(groupID, selfMemberRowID string, cursor int64, names map[string]string, instruction string) (string, error) {
	msgs, err := GetMessagesBySessionIDRaw(groupID)
	if err != nil {
		return "", fmt.Errorf("load group timeline: %w", err)
	}
	members, err := ListGroupMembers(groupID)
	if err != nil {
		return "", fmt.Errorf("load group members: %w", err)
	}
	leftIDs, roster := participantMetadata(members, selfMemberRowID)
	return buildInjectionText(msgs, cursor, selfMemberRowID, names, leftIDs, roster, instruction), nil
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
