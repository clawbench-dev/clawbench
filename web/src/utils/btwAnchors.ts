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
