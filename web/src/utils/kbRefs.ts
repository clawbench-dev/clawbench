/**
 * Knowledge-base citations attached to one assistant message.
 *
 * The server stores the exact passages it prepended to the prompt in the
 * message metadata (`metadata.kbRefs`). This module normalizes that unknown
 * JSON into a shape the chat bubble can render, dropping malformed entries
 * instead of letting one bad row break the whole message.
 */
export interface ChatKBRef {
  /** Optional: messages written before chunk ids existed have none. */
  chunkId?: number
  documentId: number
  documentTitle: string
  collectionName: string
  location: string
  text: string
}

function str(value: unknown): string {
  return typeof value === 'string' ? value : ''
}

/**
 * Parse `metadata.kbRefs`. An entry without text is useless as a citation (the
 * reader would see a title and an empty box), so it is dropped. Everything else
 * is coerced to a string/number; unknown extra fields are ignored.
 */
export function parseKBRefs(raw: unknown): ChatKBRef[] {
  if (!Array.isArray(raw)) return []
  const out: ChatKBRef[] = []
  for (const item of raw) {
    if (!item || typeof item !== 'object') continue
    const r = item as Record<string, unknown>
    const text = str(r.text).trim()
    if (!text) continue
    out.push({
      chunkId: Number(r.chunkId) || 0,
      documentId: Number(r.documentId) || 0,
      documentTitle: str(r.documentTitle),
      collectionName: str(r.collectionName),
      location: str(r.location),
      text,
    })
  }
  return out
}
