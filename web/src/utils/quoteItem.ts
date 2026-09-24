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

/**
 * Where a quote came from. Drives the drawer's "jump to source" affordance and
 * the type icon/label shown on the card and drawer.
 *
 * 'selection' is a free-form selection with no addressable source (a region that
 * carries no file path, URL or message id). 'terminal' is the same shape but
 * kept SEPARATE so it can be labelled and iconed as a terminal quote — the two
 * are otherwise indistinguishable (both have no path), and lumping them
 * together would force a generic "selected text" label on a terminal quote.
 *
 * The kind is carried explicitly rather than inferred, because inference falls
 * back to 'message' for anything without a url/path — which would label a
 * terminal quote as "chat message" in the drawer.
 */
export type QuoteSourceKind = 'file' | 'url' | 'message' | 'selection' | 'terminal'

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
  /** Commit SHA when the quote came from a git-history or CI-pipeline view. */
  commitSha?: string
  /** Scheduled task id when the quote came from a task view. */
  taskId?: number
  /** Chat session id when the quote came from a chat message. */
  sessionId?: string
  /** Task execution id when the quote came from one run's detail view. */
  executionId?: string
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
  commitSha?: string
  taskId?: number
  sessionId?: string
  executionId?: string
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
    commitSha: q.commitSha,
    taskId: q.taskId,
    sessionId: q.sessionId,
    executionId: q.executionId,
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
    // Prefer the persisted kind. Inference cannot tell a terminal quote from a
    // chat quote (both have no url and no path), so a round-tripped 'selection'
    // would come back as 'message' and the drawer would mislabel it.
    sourceKind: f.sourceKind || inferSourceKind({ url, filePath: f.path }),
    // Source locators. Read back explicitly (never inferred): the quoted text
    // carries no trace of them, so a reloaded quote would otherwise be
    // unjumpable.
    messageId: f.messageId,
    commitSha: f.commitSha,
    taskId: f.taskId,
    sessionId: f.sessionId,
    executionId: f.executionId,
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
    // Persisted so a reloaded quote keeps its real source: a terminal quote has
    // no url and no path, so without this it would be inferred as a chat quote.
    ...(q.sourceKind ? { sourceKind: q.sourceKind } : {}),
    // Source locators. These are the only route back to the commit/task/
    // message, so each must be written or the sent quote is unjumpable.
    ...(q.commitSha ? { commitSha: q.commitSha } : {}),
    ...(q.taskId ? { taskId: q.taskId } : {}),
    ...(q.sessionId ? { sessionId: q.sessionId } : {}),
    ...(q.messageId ? { messageId: q.messageId } : {}),
    ...(q.executionId ? { executionId: q.executionId } : {}),
  }
}

/** Whether an entry in a sent message's files array is a quote. */
export function isQuoteFileEntry(f: FileEntry): boolean {
  return isQuoteEntry(f)
}

/**
 * The kind of source a quote came from, for the type icon + label shown on the
 * card and in the detail drawer.
 *
 * Finer-grained than `sourceKind`: `sourceKind` says how to OPEN the quote,
 * this says what to CALL it. A forge PR and a forge issue are both
 * `sourceKind: 'url'` but read very differently, and a CI run and a git diff
 * are both commits.
 */
export type QuoteDisplayType =
  | 'task'      // scheduled task
  | 'exec'      // one task execution/run
  | 'diff'      // git commit diff
  | 'pipeline'  // CI pipeline run
  | 'pr'        // pull/merge request
  | 'issue'     // issue
  | 'link'      // other external link
  | 'terminal'  // terminal selection
  | 'chat'      // chat message
  | 'file'      // file / code
  | 'selection' // free-form selection with no addressable source

/**
 * Fence-language values that our own surfaces stamp as TYPE MARKERS rather than
 * as a code language (see the `data-quote-language` attributes). They are
 * checked first because they state the surface's intent explicitly, instead of
 * inferring it from which locators happen to be present.
 *
 * A real code fence is never named one of these, so the two uses of `language`
 * do not collide in practice.
 */
const TYPE_MARKER_LANGUAGE: Record<string, QuoteDisplayType> = {
  'pipeline': 'pipeline',
  'task-exec': 'exec',
  'task': 'task',
  'diff': 'diff',
  'pr': 'pr',
  'issue': 'issue',
}

/**
 * Resolve what kind of source a quote came from.
 *
 * Order is most-specific-first, because a quote can carry several locators at
 * once: a git-diff quote has BOTH a file path and a commit, and the user who
 * selected a hunk means the commit, not the file.
 */
export function resolveQuoteType(q: Pick<QuoteItem,
  'language' | 'url' | 'filePath' | 'sourceKind' | 'commitSha' | 'taskId' | 'executionId'
>): QuoteDisplayType {
  // 1. An explicit type marker from the surface that produced the quote.
  const marked = q.language ? TYPE_MARKER_LANGUAGE[q.language] : undefined
  if (marked) return marked

  // 2. Locators, most specific first.
  if (q.executionId) return 'exec'
  if (q.taskId) return 'task'
  // A CI run carries both its address and the commit it built; a git diff only
  // has the commit. That is what separates the two.
  if (q.commitSha) return q.url ? 'pipeline' : 'diff'
  if (q.url) return 'link'

  // 3. sourceKind, for sources with no locator at all.
  if (q.sourceKind === 'terminal') return 'terminal'
  if (q.sourceKind === 'message') return 'chat'
  if (q.filePath) return 'file'
  return 'selection'
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
 * This must stay in sync with the dispatch order in
 * `jumpToQuoteSource` (ChatPanelContent.vue): every locator it can act on has
 * to be accepted here, or the button is hidden for a quote that could in fact
 * be opened.
 *
 * A quote with no locator at all (a bare text selection, e.g. from a terminal
 * or a settings pane) has nowhere to go, so the button is hidden rather than
 * rendered as a no-op.
 */
export function canJumpToSource(q: QuoteItem): boolean {
  if (q.commitSha) return true
  if (q.taskId) return true
  if (q.sessionId) return true
  if (q.sourceKind === 'message') return false
  if (q.sourceKind === 'selection') return false
  if (q.sourceKind === 'url') return !!q.url
  return !!q.filePath
}
