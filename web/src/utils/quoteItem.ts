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
import { extractSpeakableText } from '@/utils/speakableText'
import { stripGroupProtocolTags, stripGoalSatisfiedTag } from '@/utils/groupRouting.ts'

/**
 * The text a "quote this message" action should capture — the SAME text the
 * user reads, minus the host's private notes (bcc).
 *
 * Why strip for a group: a quote becomes a `kind:'quote'` attachment, and
 * attachments are rendered into a group member's injected context
 * (`RenderQuoteBlock`). A note that reached a quote would therefore reach EVERY
 * member — the exact leak the feature exists to prevent. This is the injection
 * boundary, so it uses the fail-closed stripper (any bcc-like shape).
 *
 * Why NOT strip MENTIONS for a single chat: a single chat has no group
 * protocol, so mention stripping has zero security value and truncates an
 * ordinary reply that merely DISCUSSES the tag syntax (an unclosed literal
 * `<clawbench-mention` in prose dropped the whole tail — regression, message
 * 58879). `isGroup` is the session-type gate; callers pass their
 * `isGroupSession`.
 *
 * Single chat DOES strip the auto-cruise completion signal
 * (`<clawbench-goal-satisfied/>`): it is a self-closing, content-free protocol
 * tag the AI is instructed to emit, never part of the reply's prose, so it must
 * not be copied or quoted (it would ride into the quote attachment and hence
 * the next prompt). `stripGoalSatisfiedTag` touches ONLY that tag — it never
 * truncates mention-like prose, so 58879 cannot recur.
 *
 *   - assistant: the speakable text (skips tool/thinking noise), else the summary;
 *   - user: the message content.
 */
export function quotableMessageText(
  role: string | undefined,
  blocks: Array<Record<string, unknown>> | undefined,
  content: string | undefined,
  summary: string | undefined,
  isGroup: boolean,
): string {
  const raw = role === 'user'
    ? extractSpeakableText(blocks || []) || content || ''
    : extractSpeakableText(blocks || []) || summary || ''
  return (isGroup ? stripGroupProtocolTags(raw) : stripGoalSatisfiedTag(raw)).trim()
}

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
export type QuoteSourceKind = 'file' | 'url' | 'message' | 'selection' | 'terminal' | 'meeting' | 'mail'

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
  /**
   * Machine-readable source locators (see `FileEntry.locators`).
   *
   * An open bag: `QUOTE_LOCATOR_KEYS` documents the keys this build understands
   * and gives autocomplete, but a key it has never heard of still round-trips —
   * which is what lets a new source be added without touching any converter.
   */
  locators?: QuoteLocators
}

/**
 * The locator keys this build understands, with what each one points at.
 *
 * Documentation and autocomplete only — NOT a closed list. A quote may carry
 * other keys and they must survive every conversion; that is the whole point of
 * the bag. (The backend renders unknown keys too, sorted after the known ones.)
 */
export const QUOTE_LOCATOR_KEYS = {
  /** Commit SHA a git-history or CI-pipeline quote came from. */
  commit: 'commit',
  /** Scheduled task id a task quote came from. */
  task: 'task',
  /** Chat session id a message/session quote came from. */
  session: 'session',
  /** DB chat-message id the quote was taken from (scroll-to-message). */
  message: 'message',
  /** Task execution id, when the quote came from one run's detail view. */
  execution: 'execution',
  /** Meeting token, when the quote came from a meeting record. */
  meeting: 'meeting',
  /** Local mail message id, when the quote came from an email. */
  mail: 'mail',
  /** Project-relative folder holding the source material (a meeting's notes). */
  dir: 'dir',
} as const

/**
 * The locator bag on a quote.
 *
 * `Record<string, string>` rather than a closed union: the wire format is an
 * open bag so a new source needs no change in any converter, and typing it as a
 * closed set would put the hazard back (the type would silently drop the new
 * key at the first conversion). Values are always strings so the prompt renders
 * them verbatim.
 */
export type QuoteLocators = Record<string, string>

/** Read a locator as a string ('' when absent). */
export function locatorOf(q: { locators?: QuoteLocators }, key: string): string {
  return q.locators?.[key] ?? ''
}

/** Read a numeric locator as a number (0 when absent or not a number). */
export function numericLocatorOf(q: { locators?: QuoteLocators }, key: string): number {
  const raw = q.locators?.[key]
  if (!raw) return 0
  const n = Number(raw)
  return Number.isFinite(n) ? n : 0
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
  locators?: QuoteLocators
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
    // Copied whole: a new locator key needs no change here.
    ...(q.locators ? { locators: { ...q.locators } } : {}),
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
    // Source locators, copied whole (never inferred): the quoted text carries no
    // trace of them, so a reloaded quote would otherwise be unjumpable.
    ...(f.locators ? { locators: { ...f.locators } } : {}),
  }
}

/**
 * Build the payload for a quote taken from a chat message.
 *
 * This is the ONE builder for both ways a chat message gets quoted:
 *   - the meta-bar "quote this message" button (whole message), and
 *   - selecting text inside a message (a snippet of it).
 *
 * They were written separately and had already drifted: the button's path
 * omitted `sessionId` entirely, so the resulting quote could not be reopened
 * (a message id is only unique within its session). Keeping one builder means
 * the two entries produce the same kind of quote, which is what the user sees —
 * they are the same card either way.
 *
 * Session id/title are passed IN rather than read from useSessionIdentity here,
 * so this module stays a pure utility with no composable dependency (it is
 * imported by modules whose tests mock narrowly, and useSessionIdentity already
 * imports useChatContext, so importing it back would create a cycle).
 *
 * The label is the session TITLE because that is what the user recognises;
 * the id is what makes it addressable, and it travels separately as `session`.
 * With no title we fall back to the id so the card is never blank.
 *
 * `message` is omitted when unavailable (an optimistic message has no DB id):
 * the quote is still valid, it just cannot be scrolled back to.
 */
export function buildMessageQuote(
  text: string,
  session: { id?: string; title?: string },
  messageId?: number,
): Pick<QuoteItem, 'text' | 'filePath' | 'language' | 'startLine' | 'endLine' | 'sourceKind' | 'locators'> {
  const sessionId = session.id || ''
  const sessionTitle = (session.title || '').trim()
  const locators: QuoteLocators = {}
  if (sessionId) locators.session = sessionId
  if (typeof messageId === 'number' && Number.isFinite(messageId) && messageId > 0) {
    locators.message = String(messageId)
  }
  return {
    text,
    // The label is the session title (what the user recognises); the id is
    // carried separately. Falling back to the id keeps the card from rendering
    // blank when a session has no title yet.
    filePath: sessionTitle || sessionId,
    language: '',
    startLine: 0,
    endLine: 0,
    sourceKind: 'message',
    ...(Object.keys(locators).length ? { locators } : {}),
  }
}

/**
 * Build a quote that references a whole object (a file, a forge issue/PR, a
 * commit, a scheduled task) rather than a free-form selection.
 *
 * This is the ONE builder for every entry-point button that quotes something
 * it is already displaying: the file viewer header, the issue/PR detail header,
 * a commit row, a task row, and a code preview's selection. They all used to
 * assemble the payload by hand, which is how a missing locator (or a second id
 * format) crept in per surface.
 *
 * `text` is EMPTY by default: "quote this file" references the object, and the
 * prompt then carries the path/address so the AI reads it, instead of inlining
 * content the user never selected. A surface that DOES have the selected
 * content in hand (the code preview) passes it, along with the line range.
 *
 * The id is a preview placeholder; the real id is minted when the quote is
 * staged (addStagedQuote) or handed to the delivery helper (mintPendingQuoteId).
 */
export function quoteItemFromTarget(target: {
  filePath?: string
  url?: string
  label?: string
  language?: string
  /** Selected content, when the surface has it. Omit for whole-object quotes. */
  text?: string
  startLine?: number
  endLine?: number
  /** Explicit origin kind; inferred from url/path when omitted. */
  sourceKind?: QuoteSourceKind
  /**
   * Extra locator keys to attach (merged into the quote's bag).
   *
   * Callers pass only what they know; unknown keys are carried through
   * untouched, so a surface can attach an addressable handle for a source this
   * build has no special handling for yet.
   */
  locators?: QuoteLocators
}): QuoteItem {
  const filePath = target.filePath || target.label || ''
  return {
    id: '',
    text: target.text || '',
    note: '',
    filePath,
    language: target.language || '',
    startLine: target.startLine || 0,
    endLine: target.endLine || 0,
    url: target.url,
    sourceKind: target.sourceKind || inferSourceKind({ url: target.url, filePath }),
    ...(target.locators && Object.keys(target.locators).length
      ? { locators: { ...target.locators } }
      : {}),
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
    // Source locators, copied whole. These are the only route back to the
    // origin, so an enumerated copy here is how a locator gets lost — and with
    // an open bag there is nothing to enumerate.
    ...(q.locators && Object.keys(q.locators).length ? { locators: { ...q.locators } } : {}),
  }
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
  | 'meeting'   // meeting record (minutes / transcript)
  | 'mail'      // email (body quoted, or the message referenced as a whole)
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
  'meeting': 'meeting',
  'mail': 'mail',
}

/**
 * Resolve what kind of source a quote came from.
 *
 * Order is most-specific-first, because a quote can carry several locators at
 * once: a git-diff quote has BOTH a file path and a commit, and the user who
 * selected a hunk means the commit, not the file.
 */
export function resolveQuoteType(q: Pick<QuoteItem,
  'language' | 'url' | 'filePath' | 'sourceKind' | 'locators'
>): QuoteDisplayType {
  // 1. An explicit type marker from the surface that produced the quote.
  const marked = q.language ? TYPE_MARKER_LANGUAGE[q.language] : undefined
  if (marked) return marked

  // 2. Locators, most specific first.
  if (locatorOf(q, 'meeting')) return 'meeting'
  if (locatorOf(q, 'mail')) return 'mail'
  if (locatorOf(q, 'execution')) return 'exec'
  if (locatorOf(q, 'task')) return 'task'
  // A CI run carries both its address and the commit it built; a git diff only
  // has the commit. That is what separates the two.
  if (locatorOf(q, 'commit')) return q.url ? 'pipeline' : 'diff'
  if (q.url) return 'link'

  // 3. sourceKind, for sources with no locator at all.
  if (q.sourceKind === 'terminal') return 'terminal'
  if (q.sourceKind === 'message') return 'chat'
  if (q.filePath) return 'file'
  return 'selection'
}

/** Quote types that address the Forge integration itself (not a plain link). */
const FORGE_QUOTE_TYPES: ReadonlySet<QuoteDisplayType> = new Set<QuoteDisplayType>([
  'issue',
  'pr',
  'pipeline',
])

/**
 * Whether a quote points INTO the Forge integration.
 *
 * These references are the only ones whose destination lives behind
 * `forge.enabled`: switching the integration off stops polling and clears every
 * project's repository binding, so opening the URL afterwards lands the user on
 * a page the app no longer tracks. A `link` quote (a pasted URL, a comment
 * anchor) is not the integration and stays openable.
 */
export function isForgeQuote(q: Pick<QuoteItem, 'language' | 'url' | 'filePath' | 'sourceKind' | 'locators'>): boolean {
  return FORGE_QUOTE_TYPES.has(resolveQuoteType(q))
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
 * Where a quote's jump-to-source action goes, and whether it exists at all.
 *
 * Moved to `utils/quoteJump.ts`: the answer has to come from the SAME registry
 * that performs the jump, or the button and the handler drift apart (that is
 * exactly what used to happen — see that module's header).
 */
