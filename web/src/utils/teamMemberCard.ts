/**
 * Resolve a team member's Agent card from the live message list.
 *
 * Why this lookup exists: the team roster (`teamUpdate`) identifies a member by
 * NAME only — it carries `description`, `agentType`, tokens, and no tool-call
 * id. The member's FULL prompt, however, is not in the roster at all: it lives
 * in the `chat_tool_calls` row of the Agent card that spawned the member. So
 * opening a member's detail sheet needs that card's tool-call id (plus the
 * message that owns it, which the endpoint requires).
 *
 * Two attribution sources, in order of reliability:
 *
 *  1. the card's own `member_name`. The backend fills it in as the spawn
 *     frame's attribution lands, so it is the authoritative source.
 *  2. a CHILD block's `member_name`. Messages persisted before (1) existed have
 *     an empty value on the card, but the children that follow it still carry
 *     the member. Deriving from them makes old history work with no migration.
 *
 * Scanning is newest-first so a respawned member resolves to its MOST RECENT
 * card, which is what a user clicking a live roster row expects.
 */

export interface MemberAgentCard {
  toolId: string
  msgId: string | number
}

/** Minimal shape of a rendered message this lookup depends on. */
interface MessageLike {
  id?: string | number
  blocks?: Array<Record<string, unknown>> | null
}

/** Tool names that host a sub-agent timeline. Case-insensitive. */
function isAgentToolName(name: unknown): boolean {
  const lower = String(name ?? '').toLowerCase()
  return lower === 'agent' || lower === 'task'
}

/**
 * Find the Agent card that spawned `memberName`, or null when this message
 * history contains no such card (e.g. the team was created in an earlier
 * session, or the card was never persisted).
 */
export function findMemberAgentCard(
  messages: MessageLike[] | null | undefined,
  memberName: string,
): MemberAgentCard | null {
  if (!memberName || !Array.isArray(messages)) return null

  for (let i = messages.length - 1; i >= 0; i--) {
    const msg = messages[i]
    const blocks = msg?.blocks
    if (!Array.isArray(blocks) || blocks.length === 0) continue

    // Parent id → first child's member name. Built once per message so the
    // card scan below is a map lookup rather than a nested search.
    const childMemberByParent = new Map<string, string>()
    for (const b of blocks) {
      const pid = b?.parent_tool_call_id
      if (typeof pid !== 'string' || pid === '') continue
      const childMember = b?.member_name
      if (typeof childMember !== 'string' || childMember === '') continue
      if (!childMemberByParent.has(pid)) childMemberByParent.set(pid, childMember)
    }

    for (let j = blocks.length - 1; j >= 0; j--) {
      const b = blocks[j]
      if (b?.type !== 'tool_use') continue
      if (!isAgentToolName(b?.name)) continue
      const id = b?.id
      if (typeof id !== 'string' || id === '') continue

      const own = b?.member_name
      const owner =
        typeof own === 'string' && own !== '' ? own : childMemberByParent.get(id)
      if (owner === memberName) {
        return { toolId: id, msgId: msg.id ?? '' }
      }
    }
  }

  return null
}
