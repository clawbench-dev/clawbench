/**
 * The single normalized shape for a "quote conversation" attachment, plus the
 * adapters that map it to and from the two representations the app already has.
 *
 * Quotes exist in two places with two different shapes:
 *   - the chat input's staged list (`StagedQuote` in useChatContext.ts), and
 *   - a sent message's `files` array (`FileEntry` with kind === 'quote').
 *
 * Rather than let each surface grow its own card markup, both are mapped to
 * `QuoteItem` and rendered by one `QuoteCard.vue`. Keeping the mapping here (not
 * in the components) means the card never needs to know which side it is on.
 */
import type { FileEntry } from '@/utils/fileAttachmentUtils'
import { isQuoteEntry } from '@/utils/fileAttachmentUtils'

/** Where a quote came from. Drives the drawer's "jump to source" affordance. */
export type QuoteSourceKind = 'file' | 'url' | 'message'

/** The normalized quote shape used by every quote surface. */
export interface QuoteItem {
  /** Stable identity (used to address the entry when editing a sent note). */
  id: string
  /** The quoted content, verbatim. */
  text: string
  /** The user's annotation. Empty string when none. */
  note: string
  /** Source label: a file path, an "owner/repo#123", or '' for a chat quote. */
  filePath: string
  /** Fence language (e.g. "go", "issue"). May be empty. */
  language: string
  /** 1-based file lines; 0 when not applicable. */
  startLine: number
  endLine: number
  /** External address for a forge-sourced quote. */
  url?: string
  /** Which jump affordance (if any) the drawer should offer. */
  sourceKind: QuoteSourceKind
  /** DB message id when the quote was taken from a chat message. */
  messageId?: number
}

/** The staged-quote shape owned by useChatContext (structurally compatible). */
export interface StagedQuoteLike {
  id: string
  text: string
  filePath: string
  language: string
  startLine: number
  endLine: number
  note?: string
  url?: string
  sourceKind?: QuoteSourceKind
  messageId?: number
}

/**
 * Derive the source kind for a quote that did not carry one explicitly.
 *
 * A staged quote produced before this field existed (or by a caller that omits
 * it) is still meaningful: a url means forge, a filePath means file, and
 * neither means it came from a chat message.
 */
function inferSourceKind(q: { url?: string; filePath?: string; sourceKind?: QuoteSourceKind }): QuoteSourceKind {
  if (q.sourceKind) return q.sourceKind
  if (q.url) return 'url'
  if (q.filePath) return 'file'
  return 'message'
}

/** Map the chat input's staged quote to the normalized shape. */
export function fromStagedQuote(q: StagedQuoteLike): QuoteItem {
  return {
    id: q.id,
    text: q.text || '',
    note: q.note || '',
    filePath: q.filePath || '',
    language: q.language || '',
    startLine: q.startLine || 0,
    endLine: q.endLine || 0,
    url: q.url,
    sourceKind: inferSourceKind(q),
    messageId: q.messageId,
  }
}

/** Map a sent message's quote entry to the normalized shape. */
export function fromFileEntry(f: FileEntry): QuoteItem {
  const url = f.url
  return {
    id: f.id || '',
    text: f.text || '',
    note: f.note || '',
    filePath: f.path || '',
    language: f.language || '',
    startLine: f.startLine || 0,
    endLine: f.endLine || 0,
    url,
    sourceKind: inferSourceKind({ url, filePath: f.path }),
  }
}

/**
 * Build a quote that references a whole object (a file or a forge issue/PR)
 * rather than a text selection.
 *
 * This is what the entry-point buttons (file browser, issue/PR detail, CI run)
 * produce: "quote this file" is the same interaction as quoting selected text,
 * differing only in that the quoted thing is the object itself. `text` is
 * therefore EMPTY — the prompt then carries the path/address and the AI reads
 * it, instead of inlining content the user never selected.
 *
 * The id is a preview placeholder; the real id is minted when the quote is
 * staged (addStagedQuote).
 */
export function quoteItemFromTarget(target: {
  filePath?: string
  url?: string
  label?: string
  language?: string
}): QuoteItem {
  const filePath = target.filePath || target.label || ''
  return {
    id: '',
    text: '',
    note: '',
    filePath,
    language: target.language || '',
    startLine: 0,
    endLine: 0,
    url: target.url,
    sourceKind: inferSourceKind({ url: target.url, filePath }),
  }
}

/**
 * Map a normalized quote to the outgoing attachment entry.
 *
 * The id is carried through because the backend needs it to address the entry
 * when the annotation is edited after sending.
 */
export function toFileEntry(q: QuoteItem): FileEntry {
  return {
    path: q.filePath,
    kind: 'quote',
    id: q.id,
    text: q.text,
    note: q.note,
    language: q.language,
    startLine: q.startLine || undefined,
    endLine: q.endLine || undefined,
    ...(q.url ? { url: q.url } : {}),
  }
}

/** Whether an entry in a sent message's files array is a quote. */
export function isQuoteFileEntry(f: FileEntry): boolean {
  return isQuoteEntry(f)
}

/**
 * Materialise staged quotes into the attachment entries a send carries.
 *
 * This is the single conversion point for BOTH send paths (direct and
 * enqueue). The enqueue path used to rely on the quotes having been baked into
 * the message text, so removing that baking without this helper would silently
 * drop every quote whenever the session was already running — the hardest case
 * to notice.
 */
export function materializeQuotes(quotes: StagedQuoteLike[]): FileEntry[] {
  return quotes.map(fromStagedQuote).map(toFileEntry)
}

/**
 * A short label for a quote card: the basename of a file path, or the label
 * itself for a forge/chat quote. Returns '' when there is nothing to show.
 */
export function quoteLabel(q: QuoteItem): string {
  if (!q.filePath) return ''
  if (q.sourceKind === 'file') {
    const parts = q.filePath.split('/')
    return parts[parts.length - 1] || q.filePath
  }
  return q.filePath
}

/**
 * The line-range suffix shown after a file label, e.g. ":10" or ":10-20".
 * Empty when there is no line info (forge/chat quotes, whole-file quotes).
 */
export function quoteLineRange(q: QuoteItem): string {
  if (!q.startLine) return ''
  if (q.endLine && q.endLine !== q.startLine) return `:${q.startLine}-${q.endLine}`
  return `:${q.startLine}`
}

/**
 * Whether the drawer should offer a jump-to-source action.
 *
 * A chat-message quote has no file and no URL to open, so the button is hidden
 * rather than rendered as a no-op.
 */
export function canJumpToSource(q: QuoteItem): boolean {
  if (q.sourceKind === 'message') return false
  if (q.sourceKind === 'url') return !!q.url
  return !!q.filePath
}
