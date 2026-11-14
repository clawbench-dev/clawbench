/**
 * Search-result highlighting shared by the session search drawer and the
 * knowledge base panel.
 *
 * Both render a chunk as Markdown HTML, so the server's rune-based
 * `match_positions` cannot be applied by slicing the source string any more.
 * Instead we extract the matched TERMS from the source and mark those terms in
 * the rendered DOM. This keeps formatting intact and never injects HTML from
 * the search query.
 */

/** Extract the matched substrings from rune-based positions. */
export function termsFromRunePositions(
  text: string,
  positions: Array<{ start: number; end: number }> | undefined,
): string[] {
  if (!text || !positions || positions.length === 0) return []
  const runes = [...text]
  const runeToIndex: number[] = []
  let idx = 0
  for (let i = 0; i < runes.length; i++) {
    runeToIndex.push(idx)
    idx += runes[i].length
  }
  runeToIndex.push(idx)
  return [
    ...new Set(
      positions
        .map((p) =>
          text.slice(
            runeToIndex[Math.min(Math.max(p.start, 0), runes.length)],
            runeToIndex[Math.min(Math.max(p.end, 0), runes.length)],
          ),
        )
        .filter((t) => t.length > 0),
    ),
  ]
}

/**
 * Wrap every occurrence of `terms` in a `<mark>`. Existing marks are removed
 * first so this can be re-applied after a re-render without nesting.
 */
export function highlightTermsInElement(
  el: HTMLElement,
  terms: string[],
  markClass = 'search-hl',
): void {
  if (!el || terms.length === 0) return

  // Remove previous highlights and merge their text back into the parent.
  el.querySelectorAll(`mark.${markClass}`).forEach((m) => {
    const parent = m.parentNode
    if (parent) {
      parent.replaceChild(document.createTextNode(m.textContent || ''), m)
      parent.normalize()
    }
  })

  const walker = document.createTreeWalker(el, NodeFilter.SHOW_TEXT, null)
  const textNodes: Text[] = []
  while (walker.nextNode()) textNodes.push(walker.currentNode as Text)

  for (const node of textNodes) {
    const content = node.textContent || ''
    const lowerContent = content.toLowerCase()
    const ranges: Array<{ start: number; end: number }> = []

    for (const term of terms) {
      const lowerTerm = term.toLowerCase()
      let at = lowerContent.indexOf(lowerTerm)
      while (at !== -1) {
        ranges.push({ start: at, end: at + term.length })
        at = lowerContent.indexOf(lowerTerm, at + 1)
      }
    }
    if (ranges.length === 0) continue

    ranges.sort((a, b) => a.start - b.start)
    const parent = node.parentNode
    if (!parent) continue

    let last = 0
    const frag = document.createDocumentFragment()
    for (const r of ranges) {
      if (r.start < last) continue
      if (r.start > last) frag.appendChild(document.createTextNode(content.slice(last, r.start)))
      const mark = document.createElement('mark')
      mark.className = markClass
      mark.textContent = content.slice(r.start, r.end)
      frag.appendChild(mark)
      last = r.end
    }
    if (last < content.length) frag.appendChild(document.createTextNode(content.slice(last)))
    parent.replaceChild(frag, node)
  }
}
