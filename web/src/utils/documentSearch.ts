/**
 * In-document text search: query → regex, text-node match collection, and DOM
 * `<mark>` wrapping. Pure DOM primitives, no Vue.
 *
 * Extracted from PreviewSearchBar so the SAME engine can search a rendered
 * markdown body (host document) and a rendered HTML preview (the preview
 * iframe's own document). The only thing that differs between the two is which
 * document the nodes live in — every primitive below therefore derives its
 * document from the node it is given rather than assuming the app's `document`.
 * Using the host document to create a Range over an iframe's text node throws.
 */

export interface SearchQueryOptions {
  caseSensitive?: boolean
  regexp?: boolean
  wholeWord?: boolean
}

export interface TextMatch {
  textNode: Text
  offset: number
  length: number
}

/** Marks we wrap matches in. Shared so callers can unwrap what we wrapped. */
export const SEARCH_MARK_CLASS = 'md-search-match'

/** The active (currently navigated-to) match additionally carries this class. */
export const SEARCH_MARK_ACTIVE_CLASS = 'md-search-match-active'

/**
 * Build the matching regex for a query.
 *
 * An invalid user-supplied regexp becomes a pattern that can never match rather
 * than throwing: the search bar must stay usable while the user is mid-typing
 * an expression.
 */
export function buildSearchRegex(q: string, opts: SearchQueryOptions = {}): RegExp {
  let source = opts.regexp ? q : q.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  if (opts.wholeWord) source = `\\b${source}\\b`
  try {
    return new RegExp(source, opts.caseSensitive ? 'g' : 'gi')
  } catch {
    return /(?!)/g
  }
}

/**
 * Elements whose text is code, not content.
 *
 * A rendered HTML preview carries the document's own `<style>` and `<script>`
 * blocks. Matching inside them would not merely highlight the wrong text — it
 * would WRAP them in `<mark>`, i.e. inject markup into the document's CSS/JS
 * and change how the preview renders. Markdown never had these tags, so this
 * guard is what makes the shared engine safe for HTML.
 */
const NON_CONTENT_TAGS = new Set(['SCRIPT', 'STYLE', 'NOSCRIPT', 'TEMPLATE', 'TITLE', 'TEXTAREA'])

function isContentTextNode(node: Text): boolean {
  const parent = node.parentElement
  if (!parent) return false
  for (let el: Element | null = parent; el; el = el.parentElement) {
    if (NON_CONTENT_TAGS.has(el.tagName)) return false
  }
  return true
}

/**
 * Every match of `re` inside `root`'s text nodes, in document order.
 *
 * `re` must be global (see buildSearchRegex); its lastIndex is reset per node so
 * the same instance can be reused across the walk.
 */
export function collectTextMatches(root: ParentNode, re: RegExp): TextMatch[] {
  const doc = (root as Node).ownerDocument ?? document
  const walker = doc.createTreeWalker(root as Node, NodeFilter.SHOW_TEXT, null)
  const out: TextMatch[] = []
  while (walker.nextNode()) {
    const textNode = walker.currentNode as Text
    if (!isContentTextNode(textNode)) continue
    const text = textNode.textContent || ''
    re.lastIndex = 0
    let m: RegExpExecArray | null
    while ((m = re.exec(text)) !== null) {
      if (m[0].length === 0) {
        re.lastIndex++
        continue
      }
      out.push({ textNode, offset: m.index, length: m[0].length })
    }
  }
  return out
}

/**
 * Wrap one match in a `<mark class="...">`, returning the element (or null when
 * the range cannot be surrounded — e.g. a match spanning element boundaries).
 *
 * Must be called in REVERSE document order when wrapping many matches: each
 * wrap splits the text node, so offsets collected earlier stay valid only if
 * later ones are wrapped first.
 */
export function wrapTextMatch(
  match: TextMatch,
  className: string = SEARCH_MARK_CLASS,
): HTMLElement | null {
  const { textNode, offset, length } = match
  if (!textNode || !textNode.parentElement) return null
  const doc = textNode.ownerDocument
  if (!doc) return null
  try {
    const range = doc.createRange()
    const end = Math.min(offset + length, textNode.textContent?.length || 0)
    range.setStart(textNode, offset)
    range.setEnd(textNode, end)
    const mark = doc.createElement('mark')
    mark.className = className
    range.surroundContents(mark)
    return mark
  } catch {
    return null
  }
}

/**
 * Remove every `<mark class="...">` under `root`, restoring the original text.
 *
 * Adjacent text nodes are re-merged afterwards: unwrapping splits a node around
 * each mark ("clawbench" → "claw" + "bench"), and a word-boundary query would
 * then match a lone fragment it should not.
 */
export function unwrapSearchMarks(root: ParentNode, className: string = SEARCH_MARK_CLASS): void {
  const container = root as Element
  for (const mark of container.querySelectorAll<HTMLElement>(`mark.${className}`)) {
    const parent = mark.parentNode
    if (!parent) continue
    while (mark.firstChild) parent.insertBefore(mark.firstChild, mark)
    parent.removeChild(mark)
  }
  container.normalize?.()
}
