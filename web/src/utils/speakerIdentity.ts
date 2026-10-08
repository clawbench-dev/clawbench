/**
 * Resolving a message's speaker identity to a real agent icon.
 *
 * Three message-list surfaces render a generic Bot for assistant messages
 * (the in-app conversation index, the share message-selection dialog, and the
 * public share TOC). This module is the single place that turns a message's
 * `agentId` into the identity to render, so all three agree:
 *
 *   - empty agentId  → the SESSION's own agent (ordinary single-agent message)
 *   - agentId in the speakers map → that group member (a group message's
 *     agentId is the SPEAKER's member row id, not a real agent id)
 *   - agentId present but unknown → null (caller falls back to a generic icon;
 *     attributing it to the host would be a lie)
 *
 * The map is shipped once per response (never per message) because a custom
 * avatar is a whole SVG string — duplicating it on every message would bloat
 * the payload.
 */

/** Display identity of one speaker (mirrors service.SpeakerIdentity). */
export interface SpeakerIdentity {
  name?: string
  backend: string
  avatar?: string
}

/**
 * Build a resolver over a response's `sessionAgent` + `speakers`. Returns a
 * function that maps a message's `agentId` to an identity, or null when nothing
 * usable resolves. A resolver is always returned (never null) so callers can
 * pass it unconditionally; it simply returns null when both inputs are absent.
 */
export function makeSpeakerResolver(
  sessionAgent: SpeakerIdentity | null | undefined,
  speakers: Record<string, SpeakerIdentity> | null | undefined,
): (agentId: string) => SpeakerIdentity | null {
  return (agentId: string): SpeakerIdentity | null => {
    if (agentId) {
      // Group member id. Unknown ⇒ null, NOT the session agent: the speaker was
      // someone else, and showing the host's icon would misattribute the words.
      const hit = speakers?.[agentId]
      return hit && hit.backend ? hit : null
    }
    return sessionAgent && sessionAgent.backend ? sessionAgent : null
  }
}
