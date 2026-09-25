/**
 * Extracts plain text from user message content.
 *
 * Message content is stored in several shapes depending on the source (normal
 * chat vs ACP session sync/replay) and on historical bugs that embedded raw
 * JSON into text fields, so this function must not assume a single format:
 *
 *   - Plain text (e.g. "hello world") → returned unchanged.
 *   - Block-format JSON ({"blocks":[{"type":"text","text":"..."}]}) → text of
 *     all text blocks joined with a space (index/drawer single-line previews).
 *     Note: the Go-side ExtractPlainText joins with "\n\n" — both are valid
 *     for their contexts; the frontend single-line previews favor spaces.
 *   - Nested dirty data: a text block whose text field is itself a JSON string
 *     (e.g. an ACP notification JSON or a content array serialized into text).
 *     Recursively unwraps until real text is found.
 *   - Bare content-array JSON ([{"type":"text","text":"..."}]).
 *   - ACP notification wrapper ({"content":{"text":"hi","type":"text"},...,
 *     "sessionUpdate":"user_message_chunk"}).
 *
 * Returns the original content unchanged when nothing extractable is found.
 * Recursion is depth-capped so pathologically nested JSON degrades gracefully.
 */

/** Maximum recursive unwrap depth (real dirty data is ≤2–3 levels). */
const MAX_UNWRAP_DEPTH = 8

/** Extract text from a decoded JSON value, recursively unwrapping known wrappers. */
function extractTextFromValue(value: unknown, depth = 0): string {
  if (depth > MAX_UNWRAP_DEPTH) return ''
  if (typeof value === 'string') {
    const trimmed = value.trim()
    // A string may itself be an embedded JSON serialization (historical dirty
    // data). Unwrap it; otherwise return as-is.
    if (trimmed && (trimmed.startsWith('{') || trimmed.startsWith('['))) {
      try {
        const inner = JSON.parse(trimmed)
        const nested = extractTextFromValue(inner, depth + 1)
        if (nested.trim()) return nested
      } catch { /* not JSON, fall through */ }
    }
    return value
  }
  if (Array.isArray(value)) {
    return extractTextsFromArray(value, depth)
  }
  if (value && typeof value === 'object') {
    const obj = value as Record<string, unknown>
    // 1. {"blocks":[...]} — standard block content.
    if (Array.isArray(obj.blocks)) {
      return extractTextsFromArray(obj.blocks, depth)
    }
    // 2. ACP notification wrapper: {"content":{"text":"hi","type":"text"},...}.
    //    Historical bug stored the whole ACP notification JSON as text.
    if ('sessionUpdate' in obj && obj.content !== undefined) {
      const inner = extractTextFromValue(obj.content, depth + 1)
      if (inner.trim()) return inner
    }
    // 3. {"text":"..."} — a content block serialized by itself.
    if (obj.text !== undefined) {
      const inner = extractTextFromValue(obj.text, depth + 1)
      if (inner.trim()) return inner
    }
  }
  return ''
}

/** Extract text from each element of an array, honoring "text only" semantics. */
function extractTextsFromArray(arr: unknown[], depth: number): string {
  const texts: string[] = []
  for (const el of arr) {
    if (el && typeof el === 'object' && !Array.isArray(el)) {
      const typ = (el as Record<string, unknown>).type
      if (typeof typ === 'string' && typ !== '' && typ !== 'text') {
        // thinking/tool_use/warning blocks don't carry user text.
        continue
      }
    }
    const s = extractTextFromValue(el, depth + 1)
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
 * Maximum characters rendered per row in the conversation index. Longer
 * messages are cut with a trailing ellipsis so every row stays a compact,
 * scannable preview (matches stripMarkdownPreview's default of 100).
 */
export const INDEX_TEXT_MAX_LENGTH = 100

/**
 * Truncate an index-row preview to `maxLen` characters, appending '…' when
 * anything was cut. Iterates code points so surrogate pairs (emoji, rare CJK)
 * are never split in half.
 */
export function truncateIndexText(text: string, maxLen: number = INDEX_TEXT_MAX_LENGTH): string {
  if (maxLen <= 0) return ''
  const chars = [...text]
  return chars.length > maxLen ? chars.slice(0, maxLen).join('') + '…' : text
}

/** Visible labels the index rows may render; both are searchable. */
export interface IndexRowLabels {
  /** Generic label for an attachment-only user row, e.g. "附件" / "Attachment". */
  attachment: string
  /** Placeholder for an assistant reply that carries no text at all. */
  noText: string
}

/** A conversation-index row: role + content + attachments (legacy string[] or FileEntry-like objects). */
type IndexMsg = {
  role?: string
  summary?: string
  content?: string
  /** In-memory chat messages carry parsed blocks instead of raw JSON content. */
  blocks?: Array<{ type?: string; text?: string }>
  files?: Array<string | { path?: string }>
}

/** Text of a message's text blocks, joined with spaces. Empty when there are none. */
function blocksText(blocks?: Array<{ type?: string; text?: string }>): string {
  if (!Array.isArray(blocks)) return ''
  const texts: string[] = []
  for (const b of blocks) {
    if (b && typeof b.text === 'string' && b.text && (b.type === 'text' || b.type === undefined)) {
      texts.push(b.text)
    }
  }
  return texts.join(' ')
}

/**
 * Display text for an assistant index row.
 *
 * The index shows the stored reading summary when one exists. Summaries are
 * generated asynchronously (and backfilled on read), so an older reply may not
 * have one yet — in that case the reply's own text is used as a fallback so the
 * row is never blank for a reply that does have content. Returns "" only when
 * the reply carries no text at all (e.g. a tool-call-only turn).
 *
 * The fallback reads both `content` (raw JSON / plain text, as the API returns)
 * and `blocks` (already-parsed in-memory messages used by the offline fallback).
 */
export function assistantIndexText(msg: { summary?: string; content?: string; blocks?: Array<{ type?: string; text?: string }> }): string {
  return extractPlainText(msg.summary || '')
    || extractPlainText(msg.content || '')
    || blocksText(msg.blocks)
}

/**
 * Formats an index row for display.
 *
 * User rows show their own text truncated to INDEX_TEXT_MAX_LENGTH, or the
 * `[Attachment]` label for attachment-only messages. Assistant rows show their
 * summary (or fallback reply text), or the `noText` placeholder when the reply
 * has no text. Search matching still runs against the untruncated text (see
 * matchIndexMsg), so a query hitting text past the cap still surfaces the row.
 */
export function formatIndexMsg(
  msg: IndexMsg,
  labels: IndexRowLabels,
  maxLen: number = INDEX_TEXT_MAX_LENGTH,
): string {
  if (msg.role === 'assistant') {
    const text = assistantIndexText(msg)
    return text ? truncateIndexText(text, maxLen) : labels.noText
  }
  const text = extractPlainText(msg.content || '')
  if (!text && msg.files && msg.files.length > 0) {
    return `[${labels.attachment}]`
  }
  return truncateIndexText(text, maxLen)
}

/**
 * Case-insensitive substring match against an index row's plain text and/or
 * its attachment path/basename. Used by the conversation-index search box.
 *
 * Matching semantics:
 *   - The message haystack is the same extraction the index row displays (user
 *     text, or an assistant reply's summary/fallback text), so any text visible
 *     in a row is searchable.
 *   - Attachments match on the full normalized path (so a query like "src/foo"
 *     finds "src/foo/bar.ts") and on the basename; both case-insensitively.
 *   - Handles legacy string[] entries and current FileEntry objects ({path}).
 *   - Attachment-only rows display the generic "[{labels.attachment}]" label;
 *     when provided it is also matched, so a user typing that visible label
 *     (e.g. "附件" / "Attachment") finds the row.
 *   - Empty/whitespace query matches everything (no filtering).
 */
export function matchIndexMsg(msg: IndexMsg, query: string, labels?: IndexRowLabels): boolean {
  const q = (query || '').trim().toLowerCase()
  if (!q) return true
  if (!msg) return false

  // 1) Row text — same extraction the row displays.
  if (msg.role === 'assistant') {
    const text = assistantIndexText(msg)
    if (text.toLowerCase().includes(q)) return true
    // The placeholder is visible text too; let a query for it find the row.
    if (!text && labels?.noText && labels.noText.toLowerCase().includes(q)) return true
  } else if (extractPlainText(msg.content || '').toLowerCase().includes(q)) {
    return true
  }

  // 2) Attachment path / basename. Handles string[] (legacy) and {path, isDir}
  //    FileEntry objects (current backend), case-insensitively.
  const files = msg.files
  if (files && files.length) {
    // 2a) The visible "[Attachment]" label of attachment-only rows.
    if (labels?.attachment && labels.attachment.toLowerCase().includes(q)) return true
    for (const f of files) {
      const path = typeof f === 'string'
        ? f
        : (f && typeof f === 'object' && typeof (f as { path?: string }).path === 'string')
          ? (f as { path: string }).path
          : ''
      if (!path) continue
      // Normalize backslashes (Windows) so a forward-slash query still matches.
      const lower = path.replace(/\\/g, '/').toLowerCase()
      if (lower.includes(q)) return true
      const base = lower.slice(lower.lastIndexOf('/') + 1)
      if (base.includes(q)) return true
    }
  }
  return false
}
