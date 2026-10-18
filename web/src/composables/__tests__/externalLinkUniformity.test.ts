import { describe, it, expect, beforeAll, beforeEach, vi } from 'vitest'
import { configureMarkedRenderer } from '@/utils/markedConfig'
import { renderMarkdownHtml } from '@/composables/useMarkdownRenderer'
import { annotateLocalhostUrlsIn } from '@/composables/useLocalhostAnnotation'
import { EXTERNAL_LINK_CLASS } from '@/utils/externalLinkNode'

/**
 * EVERY path that renders an external URL must produce the SAME element.
 *
 * This file exists because they did not. External links reach the DOM through
 * three independent mechanisms, and converting only one of them produced a
 * half-migrated UI: markdown links became selectable text while bare domains
 * (`github.com/foo/bar`) and localhost URLs stayed `<a>` — still clickable, and
 * on Android still impossible to copy with a long-press.
 *
 * A per-mechanism test cannot catch that class of bug; only asserting the SAME
 * contract across all three can. Keep these cases together.
 */

vi.mock('@/composables/useAppMode', () => ({
  useAppMode: () => ({ isAppMode: { value: true } }),
}))
vi.mock('@/composables/usePortForward', () => ({
  usePortForward: () => ({
    sshInfo: { value: { enabled: true } },
    transportAllowsH2: { value: true },
  }),
}))

const parse = (html: string) => new DOMParser().parseFromString(html, 'text/html')

/** Every element that claims to be an external link. */
const externalLinks = (html: string) => Array.from(
  parse(html).querySelectorAll<HTMLElement>(`.${EXTERNAL_LINK_CLASS}`),
)

/** Anchors that still point at an external URL — these must be ZERO. */
const externalAnchors = (html: string) => Array.from(
  parse(html).querySelectorAll<HTMLAnchorElement>('a[href]'),
).filter((a) => /^(?:https?:)?\/\//i.test(a.getAttribute('href') || ''))

describe('external link rendering is uniform across every path', () => {
  beforeAll(() => configureMarkedRenderer())
  beforeEach(() => vi.clearAllMocks())

  /**
   * The cases that must all agree. `label` is the mechanism, kept in the name
   * so a failure says which one regressed.
   */
  const markdownCases: Array<[string, string, string]> = [
    ['markdown link', '[site](https://example.com/a)', 'https://example.com/a'],
    ['markdown autolink', 'see https://example.com/a now', 'https://example.com/a'],
    ['bare domain', 'github.com/foo/bar', 'https://github.com/foo/bar'],
    ['bare domain in a list', '- github.com/foo/bar', 'https://github.com/foo/bar'],
    ['bare domain in a blockquote', '> github.com/foo/bar', 'https://github.com/foo/bar'],
    ['bare domain in bold', '**github.com/foo/bar**', 'https://github.com/foo/bar'],
    ['bare domain in parentheses', 'see (github.com/foo/bar) here', 'https://github.com/foo/bar'],
    ['bare domain at a sentence end', 'see github.com/foo/bar.', 'https://github.com/foo/bar'],
  ]

  for (const [label, markdown, expectedHref] of markdownCases) {
    it(`${label} renders as text, not an anchor`, () => {
      const html = renderMarkdownHtml(markdown)

      const links = externalLinks(html)
      expect(links.length, `${label} produced no external link`).toBeGreaterThan(0)
      expect(links[0].getAttribute('data-href')).toBe(expectedHref)
      // The regression this file exists for: an external <a> left behind.
      expect(externalAnchors(html), `${label} still produced an anchor`).toHaveLength(0)
    })
  }

  it('a markdown link keeps its label as the visible text', () => {
    const [link] = externalLinks(renderMarkdownHtml('[click me](https://example.com/a)'))
    expect(link.textContent).toBe('click me')
  })

  it('a bare domain shows the URL itself (no scheme added to the text)', () => {
    const [link] = externalLinks(renderMarkdownHtml('github.com/foo/bar'))
    expect(link.textContent).toBe('github.com/foo/bar')
    // ...but the href it will open is absolute.
    expect(link.getAttribute('data-href')).toBe('https://github.com/foo/bar')
  })

  /** Localhost URLs go through a THIRD path; all three forms must agree. */
  describe('localhost (separate annotation pass)', () => {
    const localhostCases: Array<[string, string]> = [
      ['bare text', '<p>see http://localhost:5173/x now</p>'],
      ['an anchor', '<p><a href="http://localhost:5173/x">http://localhost:5173/x</a></p>'],
      ['inline code', '<p><code>http://localhost:5173/x</code></p>'],
    ]

    for (const [label, input] of localhostCases) {
      it(`${label} becomes exactly one external link`, () => {
        const doc = parse(input)
        annotateLocalhostUrlsIn(doc)
        const html = doc.body.innerHTML

        // Exactly ONE: the walker used to re-match the URL inside the span it
        // had just created, producing nested md-external-link elements and two
        // open buttons.
        expect(externalLinks(html), `${label} nested or duplicated`).toHaveLength(1)
        expect(html.match(/chat-url-open-btn"/g) || []).toHaveLength(1)
        expect(externalAnchors(html)).toHaveLength(0)
      })
    }
  })

  /**
   * The exceptions are deliberate and must not be "fixed" into spans:
   *   - media URLs are rewritten into players by matching `<a href>`
   *   - local links keep their anchor for the annotation/navigation layers
   */
  describe('deliberate exceptions keep their anchor', () => {
    it('external audio keeps an anchor so the player converter can match', () => {
      const html = renderMarkdownHtml('[play](https://example.com/song.mp3)')
      expect(html).toContain('<audio')
      expect(externalLinks(html)).toHaveLength(0)
    })

    it('external video keeps an anchor so the player converter can match', () => {
      const html = renderMarkdownHtml('[play](https://example.com/clip.mp4)')
      expect(html).toContain('<video')
      expect(externalLinks(html)).toHaveLength(0)
    })

    it('a relative file path stays an anchor', () => {
      const html = renderMarkdownHtml('[x](src/a.ts)')
      expect(parse(html).querySelector('a[href="src/a.ts"]')).not.toBeNull()
      expect(externalLinks(html)).toHaveLength(0)
    })

    it('an in-page anchor stays an anchor', () => {
      const html = renderMarkdownHtml('[x](#section)')
      expect(parse(html).querySelector('a[href="#section"]')).not.toBeNull()
      expect(externalLinks(html)).toHaveLength(0)
    })
  })
})
