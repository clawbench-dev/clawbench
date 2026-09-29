import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import { annotateExternalLinkTargets, annotateExternalLinkTargetsIn, isNewTabLink } from '@/composables/useExternalLinkAnnotation'
import { useAppMode } from '@/composables/useAppMode'

const ORIGIN = 'https://clawbench.local'

/** Run the annotator over an HTML fragment and return the result. */
function annotate(html: string): string {
  return annotateExternalLinkTargets(html)
}

/** Convenience: the opening <a …> tag of the first anchor in `html`. */
function firstAnchorTag(html: string): string {
  const doc = new DOMParser().parseFromString(html, 'text/html')
  return doc.querySelector('a')?.outerHTML ?? ''
}

describe('useExternalLinkAnnotation', () => {
  const originalLocation = window.location

  beforeEach(() => {
    // jsdom's default origin is http://localhost:3000; pin it so the
    // same-origin assertions are deterministic. Location properties are
    // prototype getters, so only the two fields the module reads are stubbed.
    Object.defineProperty(window, 'location', {
      configurable: true,
      value: { origin: ORIGIN, protocol: 'https:' },
    })
  })

  afterEach(() => {
    Object.defineProperty(window, 'location', { configurable: true, value: originalLocation })
    vi.restoreAllMocks()
  })

  describe('isNewTabLink', () => {
    it('accepts http/https links outside the origin', () => {
      expect(isNewTabLink('https://example.com/page', ORIGIN)).toBe(true)
      expect(isNewTabLink('http://example.com/page', ORIGIN)).toBe(true)
    })

    it('rejects same-origin absolute links', () => {
      // In-app navigation: a new tab would spawn a second app instance, which
      // the single-tab guard then blocks.
      expect(isNewTabLink(`${ORIGIN}/settings`, ORIGIN)).toBe(false)
    })

    it('treats protocol-relative links by host', () => {
      expect(isNewTabLink('//example.com/x', ORIGIN)).toBe(true)
      expect(isNewTabLink('//clawbench.local/x', ORIGIN)).toBe(false)
    })

    it('rejects non-http schemes and relative forms', () => {
      for (const href of ['#anchor', 'mailto:a@b.com', 'tel:+123', 'file:///etc/passwd', 'docs/a.md', '/api/fs/file?target=x', 'blob:https://x/1', 'data:text/html,<b>']) {
        expect(isNewTabLink(href, ORIGIN), href).toBe(false)
      }
    })

    it('treats http(s) links as external when the origin is unknown', () => {
      expect(isNewTabLink('https://example.com', '')).toBe(true)
    })
  })

  describe('annotateExternalLinkTargets', () => {
    it('stamps target and rel on external links', () => {
      const out = annotate('<a href="https://example.com">site</a>')
      const tag = firstAnchorTag(out)
      expect(tag).toContain('target="_blank"')
      // reverse tabnabbing guard
      expect(tag).toContain('rel="noopener noreferrer"')
      expect(tag).toContain('href="https://example.com"')
    })

    it('annotates protocol-relative links', () => {
      const tag = firstAnchorTag(annotate('<a href="//example.com/x">x</a>'))
      expect(tag).toContain('target="_blank"')
    })

    it('leaves same-origin, relative, anchor, mailto, file and api links alone', () => {
      const cases = [
        `<a href="${ORIGIN}/settings">app</a>`,
        '<a href="#section">jump</a>',
        '<a href="docs/a.md">doc</a>',
        '<a href="mailto:dev@example.com">mail</a>',
        '<a href="tel:+15551234567">call</a>',
        '<a href="file:///etc/passwd">file</a>',
        '<a href="/api/fs/file?target=x">api</a>',
        '<a href="blob:https://x/1">blob</a>',
        '<a href="data:text/html,x">data</a>',
      ]
      for (const html of cases) {
        const out = annotate(html)
        // `target=` is asserted via the attribute form (target="_blank"); the
        // api-link case legitimately carries `?target=` as a query param.
        expect(out, html).not.toContain('target="_blank"')
        expect(out, html).not.toContain('rel=')
      }
    })

    it('merges noopener/noreferrer into an existing rel without duplicating tokens', () => {
      const out = annotate('<a href="https://example.com" rel="noopener">x</a>')
      const rel = firstAnchorTag(out).match(/rel="([^"]*)"/)?.[1] ?? ''
      const tokens = rel.split(/\s+/).filter(Boolean)
      expect(tokens.filter(t => t === 'noopener')).toHaveLength(1)
      expect(tokens).toContain('noreferrer')
    })

    it('preserves an unrelated rel token', () => {
      const out = annotate('<a href="https://example.com" rel="external">x</a>')
      expect(firstAnchorTag(out)).toContain('external')
    })

    it('handles several links with mixed disposition in one pass', () => {
      const out = annotate(
        '<p><a href="https://a.com">a</a> <a href="/rel.md">b</a> <a href="https://b.com">c</a></p>'
      )
      const doc = new DOMParser().parseFromString(out, 'text/html')
      const anchors = Array.from(doc.querySelectorAll('a'))
      expect(anchors[0].getAttribute('target')).toBe('_blank')
      expect(anchors[1].hasAttribute('target')).toBe(false)
      expect(anchors[2].getAttribute('target')).toBe('_blank')
    })

    it('is idempotent — a second pass does not duplicate attributes', () => {
      const once = annotate('<a href="https://example.com">x</a>')
      const twice = annotate(once)
      expect(twice).toBe(once)
    })

    it('returns empty input unchanged', () => {
      expect(annotate('')).toBe('')
    })

    it('annotates links nested in markup (headings, list items, table cells)', () => {
      const out = annotate(
        '<h2><a href="https://a.com">t</a></h2><ul><li><a href="https://b.com">l</a></li></ul>' +
        '<table><tbody><tr><td><a href="https://c.com">c</a></td></tr></tbody></table>'
      )
      const doc = new DOMParser().parseFromString(out, 'text/html')
      for (const a of Array.from(doc.querySelectorAll('a'))) {
        expect(a.getAttribute('target')).toBe('_blank')
      }
    })
  })

  describe('app-mode gate', () => {
    it('skips annotation in native app mode', () => {
      const { isAppMode } = useAppMode()
      const original = isAppMode.value
      isAppMode.value = true
      try {
        const doc = new DOMParser().parseFromString('<a href="https://example.com">x</a>', 'text/html')
        expect(annotateExternalLinkTargetsIn(doc)).toBe(false)
        // The native hosts route external links themselves; a target the
        // WebView cannot honour would make the link dead.
        expect(doc.querySelector('a')!.hasAttribute('target')).toBe(false)
        expect(annotate('<a href="https://example.com">x</a>')).not.toContain('target=')
      } finally {
        isAppMode.value = original
      }
    })
  })
})
