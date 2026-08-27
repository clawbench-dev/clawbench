/**
 * Extracts plain text from user message content.
 *
 * Message content is stored in several shapes depending on the source (normal
 * chat vs ACP session sync/replay) and on historical bugs that embedded raw
 * JSON into text fields, so this function must not assume a single format:
 *
 *   - Plain text (e.g. "hello world") → returned unchanged.
 *   - Block-format JSON ({"blocks":[{"type":"text","text":"..."}]}) → text of
 *     all text blocks joined with a space.
 *   - Nested dirty data: a text block whose text field is itself a JSON string
 *     (e.g. an ACP notification JSON or a content array serialized into text).
 *     Recursively unwraps until real text is found.
 *   - Bare content-array JSON ([{"type":"text","text":"..."}]).
 *   - ACP notification wrapper ({"content":{"text":"hi","type":"text"},...,
 *     "sessionUpdate":"user_message_chunk"}).
 *
 * Returns the original content unchanged when nothing extractable is found.
 */

/** Extract text from a decoded JSON value, recursively unwrapping known wrappers. */
function extractTextFromValue(value: unknown): string {
  if (typeof value === 'string') {
    const trimmed = value.trim()
    // A string may itself be an embedded JSON serialization (historical dirty
    // data). Unwrap it; otherwise return as-is.
    if (trimmed && (trimmed.startsWith('{') || trimmed.startsWith('['))) {
      try {
        const inner = JSON.parse(trimmed)
        const nested = extractTextFromValue(inner)
        if (nested.trim()) return nested
      } catch { /* not JSON, fall through */ }
    }
    return value
  }
  if (Array.isArray(value)) {
    return joinExtractedTexts(value.map(el => extractTextFromValue(el)).filter(Boolean))
  }
  if (value && typeof value === 'object') {
    const obj = value as Record<string, unknown>
    // 1. {"blocks":[...]} — standard block content.
    if (Array.isArray(obj.blocks)) {
      return extractTextsFromArray(obj.blocks)
    }
    // 2. ACP notification wrapper: {"content":{"text":"hi","type":"text"},...}.
    //    Historical bug stored the whole ACP notification JSON as text.
    if ('sessionUpdate' in obj && obj.content !== undefined) {
      const inner = extractTextFromValue(obj.content)
      if (inner.trim()) return inner
    }
    // 3. {"text":"..."} — a content block serialized by itself.
    if (obj.text !== undefined) {
      const inner = extractTextFromValue(obj.text)
      if (inner.trim()) return inner
    }
  }
  return ''
}

/** Extract text from each element of an array, honoring "text only" semantics. */
function extractTextsFromArray(arr: unknown[]): string {
  const texts: string[] = []
  for (const el of arr) {
    if (el && typeof el === 'object' && !Array.isArray(el)) {
      const typ = (el as Record<string, unknown>).type
      if (typeof typ === 'string' && typ !== '' && typ !== 'text') {
        // thinking/tool_use/warning blocks don't carry user text.
        continue
      }
    }
    const s = extractTextFromValue(el)
    if (s) texts.push(s)
  }
  return joinExtractedTexts(texts)
}

function joinExtractedTexts(texts: string[]): string {
  return texts.join(' ')
}

export function extractPlainText(content: string): string {
  if (!content) return ''
  const trimmed = content.trim()
  if (!trimmed) return content
  // Fast path: not JSON at all → plain text.
  if (!trimmed.startsWith('{') && !trimmed.startsWith('[')) return content
  try {
    const parsed = JSON.parse(trimmed)
    const text = extractTextFromValue(parsed)
    // Distinguish "recognized wrapper with no text" (empty) from "unrecognized
    // JSON that should be shown as-is" (original content).
    if (isKnownWrapper(parsed)) return text
    if (text.trim()) return text
  } catch { /* not valid JSON, fall through */ }
  return content
}

/** Whether the parsed JSON is a known content wrapper we own (blocks array,
 * content-array, ACP notification, or standalone text block). */
function isKnownWrapper(value: unknown): boolean {
  if (Array.isArray(value)) return true
  if (value && typeof value === 'object') {
    const obj = value as Record<string, unknown>
    return 'blocks' in obj || 'sessionUpdate' in obj || 'text' in obj
  }
  return false
}

/**
 * Formats a user message for display in the index list.
 * Returns the full plain text, or [Attachment] label for attachment-only messages.
 */
export function formatUserMsg(msg: { content?: string; files?: string[] }, attachmentLabel: string): string {
  const text = extractPlainText(msg.content || '')
  if (!text && msg.files && msg.files.length > 0) {
    return `[${attachmentLabel}]`
  }
  return text
}
