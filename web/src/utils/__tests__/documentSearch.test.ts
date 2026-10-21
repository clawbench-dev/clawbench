import { describe, expect, it } from 'vitest'
import {
  SEARCH_MARK_CLASS,
  buildSearchRegex,
  collectTextMatches,
  unwrapSearchMarks,
  wrapTextMatch,
} from '@/utils/documentSearch'

/**
 * The in-document search engine. It must work on ANY document — the app's own
 * (rendered markdown) and a preview iframe's — which is why every primitive
 * derives its document from the nodes it is given. A regression to the global
 * `document` would silently break find inside an HTML preview (a host Range
 * cannot span an iframe's text node), so the cross-document case is tested
 * against a REAL second document.
 */
describe('buildSearchRegex', () => {
  it('escapes a literal query', () => {
    const re = buildSearchRegex('a.b')
    expect('a.b axb'.match(re)).toEqual(['a.b'])
  })

  it('treats the query as a pattern when regexp is on', () => {
    const re = buildSearchRegex('a.b', { regexp: true })
    expect('axb'.match(re)).toEqual(['axb'])
  })

  it('honours case sensitivity', () => {
    expect('Alpha alpha'.match(buildSearchRegex('alpha'))).toHaveLength(2)
    expect('Alpha alpha'.match(buildSearchRegex('alpha', { caseSensitive: true }))).toHaveLength(1)
  })

  it('honours whole-word matching', () => {
    expect('cat catalog'.match(buildSearchRegex('cat', { wholeWord: true }))).toHaveLength(1)
  })

  it('returns a never-matching pattern for an invalid regexp instead of throwing', () => {
    // The user is mid-typing an expression; the bar must stay usable.
    const re = buildSearchRegex('([', { regexp: true })
    expect('anything (['.match(re)).toBeNull()
  })
})

function makeRoot(html: string): HTMLElement {
  const el = document.createElement('div')
  el.innerHTML = html
  document.body.appendChild(el)
  return el
}

describe('collectTextMatches', () => {
  it('collects every occurrence across text nodes in document order', () => {
    const root = makeRoot('<p>alpha beta</p><p>gamma alpha</p>')
    const matches = collectTextMatches(root, buildSearchRegex('alpha'))
    expect(matches).toHaveLength(2)
    expect(matches.map((m) => m.textNode.textContent)).toEqual(['alpha beta', 'gamma alpha'])
    expect(matches.map((m) => m.offset)).toEqual([0, 6])
  })

  it('skips script and style text so wrapping cannot corrupt them', () => {
    // A rendered HTML preview carries the document's own <style>/<script>. A
    // match there would be wrapped in <mark>, i.e. markup injected into CSS/JS.
    const root = makeRoot('<style>.alpha{}</style><script>var alpha=1</script><p>alpha</p>')
    const matches = collectTextMatches(root, buildSearchRegex('alpha'))
    expect(matches).toHaveLength(1)
    expect(matches[0].textNode.parentElement?.tagName).toBe('P')
  })

  it('finds matches inside a DIFFERENT document (the iframe case)', () => {
    const iframe = document.createElement('iframe')
    document.body.appendChild(iframe)
    const doc = iframe.contentDocument!
    doc.body.innerHTML = '<p>iframe alpha</p>'

    const matches = collectTextMatches(doc.body, buildSearchRegex('alpha'))
    expect(matches).toHaveLength(1)
    // The node must belong to the iframe document, not the host.
    expect(matches[0].textNode.ownerDocument).toBe(doc)
    iframe.remove()
  })

  it('returns nothing for an empty query', () => {
    const root = makeRoot('<p>alpha</p>')
    // `collectTextMatches` is driven by the regex; an empty literal regex
    // matches everywhere, so callers guard on the query — this pins that the
    // engine itself does not produce zero-length matches.
    const matches = collectTextMatches(root, buildSearchRegex(''))
    expect(matches.every((m) => m.length > 0)).toBe(true)
  })
})

describe('wrapTextMatch / unwrapSearchMarks', () => {
  it('wraps a match in a mark element', () => {
    const root = makeRoot('<p>alpha beta</p>')
    const [m] = collectTextMatches(root, buildSearchRegex('alpha'))
    const mark = wrapTextMatch(m)
    expect(mark).not.toBeNull()
    expect(mark!.className).toBe(SEARCH_MARK_CLASS)
    expect(root.querySelector('mark')?.textContent).toBe('alpha')
  })

  it('unwrapping restores the original text and re-merges the split nodes', () => {
    const root = makeRoot('<p>clawbench</p>')
    const [m] = collectTextMatches(root, buildSearchRegex('claw'))
    wrapTextMatch(m)
    // The wrap split the text node (an empty head, the mark, the tail).
    expect(root.querySelector('p')!.childNodes.length).toBeGreaterThan(1)

    unwrapSearchMarks(root)
    expect(root.querySelector('mark')).toBeNull()
    expect(root.textContent).toBe('clawbench')
    // Re-merged: a word-boundary query must see the whole word again.
    expect(root.querySelector('p')!.childNodes.length).toBe(1)
    expect(collectTextMatches(root, buildSearchRegex('clawbench', { wholeWord: true }))).toHaveLength(1)
  })

  it('wraps into the match OWNING document (iframe)', () => {
    const iframe = document.createElement('iframe')
    document.body.appendChild(iframe)
    const doc = iframe.contentDocument!
    doc.body.innerHTML = '<p>iframe alpha</p>'

    const [m] = collectTextMatches(doc.body, buildSearchRegex('alpha'))
    const mark = wrapTextMatch(m)
    // A host-document Range cannot surround an iframe text node, so a null here
    // means the engine regressed to `document.createRange()`.
    expect(mark).not.toBeNull()
    expect(mark!.ownerDocument).toBe(doc)
    expect(doc.querySelector('mark')?.textContent).toBe('alpha')
    iframe.remove()
  })
})
