/**
 * Pure functions extracted from useQuoteQuestion composable.
 * These have no Vue reactivity dependencies and can be tested in isolation.
 */

/**
 * Get the closest Element matching a selector from a node.
 * The node may be a Text node, so we use parentElement first.
 */
export function closestElement(node: Node | null, selector: string): HTMLElement | null {
  if (!node) return null
  const el = (node instanceof HTMLElement ? node : node.parentElement)
  return el?.closest?.(selector) ?? null
}

/**
 * Resolve a source line for a DOM node: prefers the precise `.code-line`
 * annotation, then falls back to the block-level `[data-source-line]` a
 * rendered markdown block carries (its own starting source line).
 */
function lineOfNode(node: Node | null): number {
  const codeLine = closestElement(node, '.code-line')
  if (codeLine) {
    const n = parseInt(codeLine.getAttribute('data-line') || '0')
    if (n > 0) return n
  }
  const block = closestElement(node, '[data-source-line]')
  if (block) {
    const n = parseInt(block.getAttribute('data-source-line') || '0')
    if (n > 0) return n
  }
  return 0
}

/**
 * Get line numbers from a selection range inside a preview.
 * Walks up from the anchor/focus nodes to find a `.code-line[data-line]`
 * (exact line) or a rendered markdown block's `[data-source-line]` (block
 * start). Returns the min/max across the two selection edges — 0 when the
 * selection does not touch any line-annotated content.
 *
 * The returned numbers are 1-based FILE lines: precise for code lines, the
 * enclosing block's start line for rendered markdown (a rendered block cannot
 * expose finer granularity). Both map directly onto the raw/CodeMirror doc, so
 * quote-card navigation and the fenced-code line suffix are always in file
 * coordinates — no offset between the two views.
 */
export function getLineInfo(selection: Selection): { startLine: number; endLine: number } {
  const anchorLine = lineOfNode(selection.anchorNode)
  const focusLine = lineOfNode(selection.focusNode)
  if (!anchorLine || !focusLine) return { startLine: 0, endLine: 0 }
  return {
    startLine: Math.min(anchorLine, focusLine),
    endLine: Math.max(anchorLine, focusLine),
  }
}

/**
 * Get the file path and language from the container element.
 */
export function getFileInfo(container: HTMLElement): { filePath: string; language: string } {
  const codePreview = container.closest('.raw-content-pre')
  if (codePreview) {
    const filePath = codePreview.getAttribute('data-file-path') || ''
    const language = codePreview.getAttribute('data-language') || ''
    return { filePath, language }
  }
  const markdownBody = container.closest('.markdown-body')
  if (markdownBody) {
    const filePath = markdownBody.getAttribute('data-file-path') || ''
    return { filePath, language: '' }
  }
  const officeBody = container.closest('.office-preview-body')
  if (officeBody) {
    const filePath = officeBody.getAttribute('data-file-path') || ''
    return { filePath, language: '' }
  }
  return { filePath: '', language: '' }
}

/**
 * Truncate quote text to a maximum length, appending an ellipsis if truncated.
 * Extracted from QuoteQuestionBar.vue's fullQuoteText computed.
 */
export function truncateQuoteText(text: string, maxLen = 150): string {
  return text.length > maxLen ? text.slice(0, maxLen) + '…' : text
}

/**
 * Check if the input text is non-empty after trimming.
 * Extracted from QuoteQuestionBar.vue's canSend computed.
 */
export function canSendInput(inputText: string): boolean {
  return inputText.trim().length > 0
}

/**
 * Strip the project root prefix from an absolute in-project path so it can be
 * opened as a project-relative file. Returns the path unchanged when it is not
 * under the given root (e.g. external paths starting with '/').
 */
export function relativizeProjectPath(filePath: string, projectRoot: string): string {
  if (!filePath) return filePath
  if (projectRoot && filePath.startsWith(projectRoot + '/')) {
    return filePath.slice(projectRoot.length + 1)
  }
  return filePath
}

/**
 * A labelled quote source region, as read from `data-quote-*` attributes. */
export interface QuoteSource {
  label: string
  language: string
  url: string
  /**
   * Machine-readable locators for the origin, collected from every
   * `data-quote-<key>` attribute on the SAME element as the label, so a region
   * either carries a whole identity or none of it.
   *
   * They exist so the quote can jump back to its source and so the AI can
   * address it: the label is a human-readable name ("每日构建"), which is not
   * enough to find anything.
   *
   * Collected by PREFIX rather than by a fixed list of attribute names. The
   * fixed-list version had to be edited in three places to declare one new
   * locator (the attribute, this reader, and the copier), and the meeting token
   * was in fact read but never copied out — the card said "会议" and the jump
   * button did nothing. Now `data-quote-deploy="d-42"` on a region reaches the
   * AI with no code change at all.
   */
  locators: Record<string, string>
}

/** `data-quote-source` / `data-quote-language` / `data-quote-url` are reserved. */
const RESERVED_QUOTE_ATTRS = new Set(['source', 'language', 'url'])

/**
 * Read the quote source label from a container's ancestors.
 *
 * Issue/PR pages are not files, so they cannot use `data-file-path` — that
 * attribute is what marks a markdown body as an attachable file (see
 * mdBlockAttach/mdMermaidAttach) and would sprout "add to chat" buttons on
 * every code block in the issue body. A dedicated attribute keeps the quote
 * labelled without claiming the content is a file.
 *
 * `data-quote-url` carries the object's address so a forge quote can offer a
 * real jump-to-source action. Without it the label alone is not openable.
 *
 * Any other `data-quote-<key>` attribute becomes a locator, verbatim: the
 * attribute name is the key and its value is the value. Known ones today are
 * `commit`, `task-id`, `session-id`, `message-id`, `execution-id`,
 * `meeting-token` — but nothing here enumerates them, so a new one needs no
 * change to this function.
 *
 * Returns null when the container is not inside a labelled region, so callers
 * can fall back to the normal file-path handling.
 */
export function getQuoteSource(container: HTMLElement): QuoteSource | null {
  const el = container.closest<HTMLElement>('[data-quote-source]')
  const label = el?.getAttribute('data-quote-source') || ''
  if (!el || !label) return null
  return {
    label,
    language: el.getAttribute('data-quote-language') || '',
    url: el.getAttribute('data-quote-url') || '',
    locators: readLocatorAttrs(el),
  }
}

/**
 * Collect every `data-quote-<key>` attribute into a locator bag.
 *
 * Attribute names are mapped to prompt/locator vocabulary: `data-quote-task-id`
 * → `task`, `data-quote-message-id` → `message`, etc. That suffix trimming is
 * what keeps the DOM attributes readable (`task-id`, `meeting-token`) while the
 * wire/prompt keys stay terse (`task`, `meeting`).
 *
 * Empty values are dropped: an attribute bound to `''` (a Vue binding whose
 * source is absent) means "no locator", not "locator with an empty value".
 */
function readLocatorAttrs(el: HTMLElement): Record<string, string> {
  const locators: Record<string, string> = {}
  for (const attr of Array.from(el.attributes)) {
    if (!attr.name.startsWith('data-quote-')) continue
    const raw = attr.name.slice('data-quote-'.length)
    if (RESERVED_QUOTE_ATTRS.has(raw)) continue
    const value = attr.value.trim()
    if (!value) continue
    locators[locatorKeyFromAttr(raw)] = value
  }
  return locators
}

/**
 * `task-id` → `task`, `meeting-token` → `meeting`, `execution-id` → `execution`.
 *
 * The DOM keeps a readable `-id`/`-token` suffix so an attribute name says what
 * kind of handle it holds; the locator key drops it, matching the vocabulary the
 * prompt renders (`task: 12`) and the wire bag (`locators.task`).
 */
function locatorKeyFromAttr(raw: string): string {
  return raw.replace(/-(id|token|sha)$/, '')
}

/**
 * Extract the DB message id from a chat row's `data-msg-key`.
 *
 * ChatMessageItem sets `data-msg-key="db-<id>"` on `.chat-message`, and leaves
 * it unset for optimistic/local messages (no DB row yet). Those cannot be
 * addressed later, so they yield undefined rather than a bogus id.
 */
export function messageIdFromKey(key: string | null | undefined): number | undefined {
  if (!key || !key.startsWith('db-')) return undefined
  const id = Number(key.slice(3))
  return Number.isFinite(id) && id > 0 ? id : undefined
}
