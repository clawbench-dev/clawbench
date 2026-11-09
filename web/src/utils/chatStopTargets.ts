/**
 * Who is speaking RIGHT NOW — the stop menu's list.
 *
 * Derived from the rendered messages (`streaming` + `agentId`), not from a
 * backend query: that is exactly the set the user sees on screen, so the menu
 * can never offer a member whose bubble already closed.
 *
 * The HOST is excluded on purpose. It drives the routing of the round, so
 * "stop the host" is not "one member goes quiet" — it ends the turn. When the
 * host is the only one speaking the list stays empty, which makes the stop
 * button keep its whole-turn cancel (two clicks) instead of offering a
 * single-row menu that behaves differently from what its label promises.
 */

/** One member who is speaking right now, as the stop menu needs it. */
export interface StopTarget {
  /** Member row id (what the endpoint takes). */
  id: string
  name: string
  backend: string
  avatar: string
  /** Epoch ms when this speech started; 0 = unknown (no "已说 N 秒"). */
  startedAt: number
}

/** Minimal shape of a rendered message this derivation needs. */
export interface StopTargetMessage {
  role?: string
  streaming?: boolean
  agentId?: string
  createdAt?: string
}

export interface CollectStopTargetsOptions {
  /** Only group chats have per-member stop. */
  isGroupSession: boolean
  /** The group's host member row id (empty for a free-mode group). */
  hostMemberId?: string
  /** The group roster — authoritative for display names. */
  roster?: Array<{ id: string; name?: string; backend?: string; avatar?: string }>
  /** Speaker resolver (member row id → identity); may be null. */
  resolveSpeaker?: ((id: string) => { name?: string; backend?: string; avatar?: string } | null) | null
  /** Name to show when neither the roster nor the resolver knows the member. */
  fallbackName: string
}

export function collectStopTargets(
  messages: StopTargetMessage[] | null | undefined,
  opts: CollectStopTargetsOptions,
): StopTarget[] {
  if (!opts.isGroupSession) return []
  const roster = new Map((opts.roster || []).map((m) => [String(m.id), m]))
  const hostId = opts.hostMemberId ? String(opts.hostMemberId) : ''
  const resolve = opts.resolveSpeaker

  return (messages || [])
    .filter((m) => m.role === 'assistant' && m.streaming === true && !!m.agentId)
    .filter((m) => !hostId || String(m.agentId) !== hostId)
    .map((m) => {
      const id = String(m.agentId)
      const member = roster.get(id)
      const speaker = typeof resolve === 'function' ? resolve(id) : null
      return {
        id,
        name: member?.name || speaker?.name || opts.fallbackName,
        backend: speaker?.backend || member?.backend || '',
        avatar: speaker?.avatar || member?.avatar || '',
        startedAt: m.createdAt ? Date.parse(m.createdAt) : 0,
      }
    })
}
