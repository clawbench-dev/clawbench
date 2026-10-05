package service

import (
	"fmt"
	"strings"

	"clawbench/internal/model"
)

// group_inject.go builds the incremental context a group member sees before it
// speaks. Members only exchange SPEECH TEXT — tool calls and thinking are
// private (design §5.2 / decision #9).

// buildInjectionText renders the group timeline slice a member should see.
//
//   - msgs is the group timeline in id order.
//   - cursor is the member's seen_cursor: only messages with id > cursor are
//     included (the member's own past speech is excluded by the author filter).
//   - self is the speaking member's ROW id (matches msg.AgentID).
//   - names maps a member row id -> display name; unknown ids fall back to a
//     short placeholder so a removed/renamed member's speech is still labeled.
//   - instruction is the host's directive to this member, rendered as a
//     high-priority final paragraph (empty = omitted).
//
// User messages are rendered as "用户: ..."; member speech as "<name>: ...".
func buildInjectionText(msgs []model.ChatMessage, cursor int64, self string, names map[string]string, instruction string) string {
	var b strings.Builder
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

// groupInjectionText loads a member's incremental context from the group
// timeline. It is a thin DB wrapper over buildInjectionText used by the
// orchestrator.
func groupInjectionText(groupID, selfMemberRowID string, cursor int64, names map[string]string, instruction string) (string, error) {
	msgs, err := GetMessagesBySessionIDRaw(groupID)
	if err != nil {
		return "", fmt.Errorf("load group timeline: %w", err)
	}
	return buildInjectionText(msgs, cursor, selfMemberRowID, names, instruction), nil
}
