/**
 * Grouping helpers for "/btw" side-question anchors.
 *
 * A /btw record is anchored to the message that was last when the question was
 * asked (anchorMessageId). The chat list renders one marker per anchor position,
 * so records must be grouped by that id and the marker must carry a count.
 *
 * anchorMessageId 0 means "asked before the session had any message"; it is a
 * valid anchor and the marker then leads the list. Kept here (not in the
 * component) so the grouping rules are unit-testable.
 */

export interface BtwRecordLike {
  id?: number | string
  anchorMessageId?: number | null
  question?: string
  answer?: string
  error?: string
  createdAt?: string
  /**
   * Optimistic entry: the question was just asked and is still being answered.
   * It has no server id yet and renders as an in-progress anchor.
   */
  pending?: boolean
}

/** The grouping key for a record's anchor id. Null/undefined → "0". */
export function btwAnchorKey(anchorMessageId: number | null | undefined): string {
  const id = typeof anchorMessageId === 'number' && Number.isFinite(anchorMessageId) ? anchorMessageId : 0
  return String(id)
}

/**
 * Group records by anchor, preserving chronological order within each group.
 * Records are expected oldest-first (the list endpoint orders by id).
 */
export function groupBtwRecords<T extends BtwRecordLike>(records: T[] | null | undefined): Record<string, T[]> {
  const out: Record<string, T[]> = {}
  for (const rec of records || []) {
    const key = btwAnchorKey(rec?.anchorMessageId)
    if (!out[key]) out[key] = []
    out[key].push(rec)
  }
  return out
}

/**
 * The anchor key for a chat message. Only a settled numeric DB id is a valid
 * anchor target: an optimistic (`pending-*`) or pre-`stream_start` placeholder
 * id is not stable, so such a message can never match a persisted record.
 * Returns '' when the message cannot host an anchor.
 */
export function messageAnchorKey(msg: { id?: unknown } | null | undefined): string {
  const id = msg?.id
  return typeof id === 'number' && id > 0 ? String(id) : ''
}

/** Number of questions anchored to a message (0 = no marker). */
export function anchorCount(
  anchors: Record<string, unknown[]> | null | undefined,
  msg: { id?: unknown } | null | undefined,
): number {
  const key = messageAnchorKey(msg)
  if (!key) return 0
  return anchors?.[key]?.length || 0
}

/**
 * Whether any record grouped under an anchor key is still being answered.
 *
 * A just-asked question is inserted optimistically (see `pending`), so the
 * marker can show its in-progress state without waiting for the server.
 */
export function anchorKeyPending(
  anchors: Record<string, BtwRecordLike[]> | null | undefined,
  key: string | null | undefined,
): boolean {
  if (!key) return false
  return (anchors?.[key] || []).some(r => r?.pending === true)
}

/** Whether any question anchored to a message is still being answered. */
export function anchorPending(
  anchors: Record<string, BtwRecordLike[]> | null | undefined,
  msg: { id?: unknown } | null | undefined,
): boolean {
  return anchorKeyPending(anchors, messageAnchorKey(msg))
}

/**
 * The anchor key a question asked *now* would receive: the id of the last
 * settled (persisted) message, or "0" when the session has none.
 *
 * Mirrors the backend's `MAX(id) FROM chat_history`, so an optimistic anchor
 * lands exactly where the server record will — otherwise it would visibly jump
 * when the response arrives. Optimistic messages (string `pending-*` ids) are
 * skipped for the same reason: they are not in the DB yet, so the server
 * anchors the question to an earlier message.
 */
export function currentAnchorKey(messages: { id?: unknown }[] | null | undefined): string {
  if (!messages) return '0'
  for (let i = messages.length - 1; i >= 0; i--) {
    const key = messageAnchorKey(messages[i])
    if (key) return key
  }
  return '0'
}
