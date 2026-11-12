/**
 * Per-member round status for the group member panel.
 *
 * Everything here is derived from the rendered messages — the backend has no
 * per-member status field, and adding one would create a second source of truth
 * that can disagree with the bubbles on screen.
 *
 * "This round" = everything after the LAST user message. A group run starts
 * with the user's message, so that boundary is exactly what the reader sees as
 * "this round"; auto-cruise keeps extending the same round until the user
 * speaks again, which matches "this member has already contributed".
 *
 * The status of a member follows its LATEST reply in the round:
 *   stopped  — the user cut that speech short, or the whole turn was cancelled
 *   speaking — that reply is still streaming
 *   done     — the reply finished
 * A member with no reply in the round is `waiting`; a disabled member is
 * `disabled` regardless of history (it is not scheduled at all).
 */

export type GroupMemberRoundStatus = 'speaking' | 'done' | 'stopped' | 'waiting' | 'disabled'

/** Minimal roster shape this derivation needs. */
export interface GroupMemberStatusInput {
  id: string
  disabled?: boolean
}

/** Minimal message shape this derivation needs (see ChatMessage). */
export interface GroupMemberStatusMessage {
  role?: string
  streaming?: boolean
  stopped?: boolean
  /** The WHOLE turn was cancelled mid-reply (different from `stopped`, which is
   *  one member being cut). Both mean "this reply never finished", so the panel
   *  must not call it 已答完. */
  cancelled?: boolean
  agentId?: string
  id?: string | number
  createdAt?: string
}

export interface GroupMemberStatusEntry {
  id: string
  status: GroupMemberRoundStatus
  /** Latest reply this member produced in the WHOLE session — the jump target.
   *  Null when the member has never spoken (nothing to jump to). */
  lastMessageId: string | number | null
  /** Timestamp of that reply, for the idle row's "last reply …". */
  lastMessageAt: string
}

/** True when the id can be used as a jump target (optimistic bubbles carry a
 *  client-side string id before the DB row exists; jumping to those is fine
 *  only while they are loaded, and they are). */
function usableId(id: string | number | undefined): string | number | null {
  if (id === undefined || id === null || id === '') return null
  return id
}

/**
 * collectGroupMemberStatus derives one entry per roster member, in the order
 * given (callers reorder for display).
 */
export function collectGroupMemberStatus(
  members: GroupMemberStatusInput[] | null | undefined,
  messages: GroupMemberStatusMessage[] | null | undefined,
): GroupMemberStatusEntry[] {
  const list = messages || []

  // Round boundary: after the last user message. With no user message at all
  // (a background/cruise-only run) the whole list counts as the round.
  let roundStart = 0
  for (let i = list.length - 1; i >= 0; i--) {
    if (list[i]?.role === 'user') { roundStart = i + 1; break }
  }

  // Latest assistant reply per member across the whole session, one pass.
  const lastById = new Map<string, GroupMemberStatusMessage>()
  for (const m of list) {
    if (m?.role !== 'assistant' || !m.agentId) continue
    lastById.set(String(m.agentId), m)
  }

  // Latest reply per member WITHIN the round.
  const roundLastById = new Map<string, GroupMemberStatusMessage>()
  for (let i = roundStart; i < list.length; i++) {
    const m = list[i]
    if (m?.role !== 'assistant' || !m.agentId) continue
    roundLastById.set(String(m.agentId), m)
  }

  return (members || []).map((member) => {
    const id = String(member.id)
    const last = lastById.get(id)
    const roundLast = roundLastById.get(id)

    let status: GroupMemberRoundStatus = 'waiting'
    if (member.disabled) {
      status = 'disabled'
    } else if (roundLast) {
      // A reply that never finished is NOT "done": one member was stopped, or
      // the whole turn was cancelled mid-stream.
      if (roundLast.stopped || roundLast.cancelled) status = 'stopped'
      else if (roundLast.streaming) status = 'speaking'
      else status = 'done'
    }

    return {
      id,
      status,
      lastMessageId: usableId(last?.id),
      lastMessageAt: last?.createdAt || '',
    }
  })
}
