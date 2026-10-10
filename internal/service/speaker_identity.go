package service

import "clawbench/internal/model"

// SpeakerIdentity is the display identity of one message speaker: enough to
// render an AgentIcon (backend + optional custom avatar) plus a name. It is the
// wire shape shared by the conversation-index, share-selection and share-payload
// endpoints so the frontend can resolve a message's icon the same way everywhere.
//
// Avatar is the user-configured SVG string (model.Agent.Avatar), NOT a data URI.
// It is carried on every path (see ResolveSessionSpeakers' includeAvatar): the
// public share snapshot freezes it at creation time so the anonymous viewer can
// render the same icons the app shows. The snapshot is the only copy the viewer
// ever sees — it cannot reach the authenticated agent endpoints.
type SpeakerIdentity struct {
	Name    string `json:"name,omitempty"`
	Backend string `json:"backend"`
	Avatar  string `json:"avatar,omitempty"`
}

// ResolveSessionSpeakers resolves a session's speaker identities ONCE, so each
// endpoint can ship them alongside its messages without an N+1 lookup:
//
//   - sessionAgent: the identity of the SESSION's own agent. Used for ordinary
//     single-agent messages, whose chat_history.agent_id is empty.
//   - speakers: member-row-id → identity, for a GROUP session only. A group
//     message's chat_history.agent_id is the SPEAKER's member row id (NOT a real
//     agent id), so it is resolved through this map.
//
// includeAvatar controls whether each identity carries the agent's custom
// avatar. Both the authenticated in-app endpoints and the public share path
// pass true: the share snapshot is built by the authenticated owner and freezes
// the avatars for the viewer, which has no other way to resolve them.
//
// Either return value may be nil: sessionAgent when the session is unknown,
// speakers when the session is not a group (or has no members). Callers treat a
// nil/missing entry as "no identity" and fall back to a generic icon.
func ResolveSessionSpeakers(sessionID string, includeAvatar bool) (*SpeakerIdentity, map[string]SpeakerIdentity) {
	info := GetSessionFullInfo(sessionID)
	if info == nil {
		return nil, nil
	}
	sessionAgent := speakerFromAgent(info.AgentID, info.Backend, info.Title, includeAvatar)

	if GetSessionType(sessionID) != groupSessionType {
		return &sessionAgent, nil
	}

	// Group: map each member row id to its identity. ListGroupMembers includes
	// LEFT (removed) members on purpose — their past speech is still in the
	// timeline and must keep resolving to their own icon, not the host's.
	members, err := ListGroupMembers(sessionID)
	if err != nil || len(members) == 0 {
		return &sessionAgent, nil
	}
	speakers := make(map[string]SpeakerIdentity, len(members))
	for _, m := range members {
		speakers[m.ID] = speakerFromMember(m, includeAvatar)
	}
	return &sessionAgent, speakers
}

// speakerFromMember builds a member's SpeakerIdentity. Unlike speakerFromAgent,
// the member's stored name (agent_name) is AUTHORITATIVE and is never overridden
// by the live agents.name: a member's name is a snapshot taken when it joined
// the group, so renaming the agent must not rewrite past speech, and duplicate
// instances of one agent (each with its own name) must stay distinguishable.
// The backend and custom avatar still come from the agent registry (avatar is a
// property of the agent, shared by all its instances).
func speakerFromMember(m GroupMember, includeAvatar bool) SpeakerIdentity {
	s := SpeakerIdentity{Backend: m.Backend, Name: m.Name}
	if a := model.GetAgent(m.AgentID); a != nil {
		if a.Backend != "" {
			s.Backend = a.Backend
		}
		if includeAvatar {
			s.Avatar = a.Avatar
		}
	}
	return s
}

// speakerFromAgent builds a SpeakerIdentity from an agent id, falling back to the
// provided backend/name when the agent is not (or no longer) in the registry.
// A custom avatar is only carried when includeAvatar is true.
//
// This is used for a SESSION's own agent (single-agent replies and the group
// row's identity), where the live agent name is correct. Member rows must use
// speakerFromMember instead, which treats the stored name as authoritative.
func speakerFromAgent(agentID, backend, fallbackName string, includeAvatar bool) SpeakerIdentity {
	s := SpeakerIdentity{Backend: backend, Name: fallbackName}
	if a := model.GetAgent(agentID); a != nil {
		if a.Name != "" {
			s.Name = a.Name
		}
		if a.Backend != "" {
			s.Backend = a.Backend
		}
		if includeAvatar {
			s.Avatar = a.Avatar
		}
	}
	return s
}
