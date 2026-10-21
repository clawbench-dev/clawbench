import { describe, expect, it } from 'vitest'
import { buildHtmlPreviewSrc, installPreviewScrollbar, installPreviewSearchStyles } from '@/utils/htmlPreview'

/**
 * buildHtmlPreviewSrc is the single source of truth for how an HTML document is
 * loaded into a preview iframe (shared by the full-screen FileViewer and the
 * quick code-link preview pane). Its two rules — real URL for project-relative
 * files, empty string (→ srcdoc fallback) for external files — are load-bearing;
 * see the module header for why.
 */
describe('buildHtmlPreviewSrc', () => {
  it('builds a project-relative URL with a cache-buster', () => {
    const url = buildHtmlPreviewSrc('public/index.html', 1234)
    expect(url).toBe('/api/fs/raw/public/index.html?t=1234')
  })

  it('returns empty for an absolute (external) path so the caller falls back to srcdoc', () => {
    // The URL form would resolve relative references against the project root
    // and silently load the WRONG file when one of the same name exists there.
    expect(buildHtmlPreviewSrc('/tmp/external/index.html', 1234)).toBe('')
  })

  it('returns empty for an empty path', () => {
    expect(buildHtmlPreviewSrc('', 1234)).toBe('')
  })

  it('changes the URL when the timestamp changes (cache-buster)', () => {
    expect(buildHtmlPreviewSrc('a.html', 1)).not.toBe(buildHtmlPreviewSrc('a.html', 2))
  })
})

/**
 * installPreviewScrollbar gives the previewed document the app's scrollbar bar.
 * jsdom can't evaluate `::-webkit-scrollbar`, but the DOM contract can be
 * tested for real: the sheet lands in <head>, first, exactly once.
 */
describe('installPreviewScrollbar', () => {
  /** An iframe whose contentDocument is a plain document we control. */
  function makeIframe(headHtml = '<title>t</title>'): HTMLIFrameElement {
    const iframe = document.createElement('iframe')
    document.body.appendChild(iframe)
    const doc = iframe.contentDocument!
    doc.head.innerHTML = headHtml
    return iframe
  }

  it('injects the app bar into the preview document head', () => {
    const iframe = makeIframe()
    expect(installPreviewScrollbar(iframe)).toBe(true)
    const style = iframe.contentDocument!.head.querySelector('style[data-clawbench-scrollbar]')
    expect(style, 'the shared sheet must be injected').not.toBeNull()
    expect(style!.textContent).toContain('::-webkit-scrollbar')
    expect(style!.textContent).toContain('::-webkit-scrollbar-button')
    iframe.remove()
  })

  it('inserts FIRST so the document\'s own scrollbar rules win', () => {
    // Contract: apply the app bar only where the document has not expressed its
    // own preference. The injected sheet must precede the document's own head
    // content, so the latter wins by source order.
    const iframe = makeIframe('<style>.x{}</style>')
    installPreviewScrollbar(iframe)
    const head = iframe.contentDocument!.head
    expect(head.firstElementChild!.getAttribute('data-clawbench-scrollbar')).not.toBeNull()
    // The document's own style must still be after ours.
    const own = [...head.children].find(
      (el) => el.tagName === 'STYLE' && !el.hasAttribute('data-clawbench-scrollbar'),
    )
    expect(own).toBeTruthy()
    expect(
      head.children[0].compareDocumentPosition(own!) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy()
    iframe.remove()
  })

  it('is idempotent — a reload does not stack copies', () => {
    const iframe = makeIframe()
    installPreviewScrollbar(iframe)
    installPreviewScrollbar(iframe)
    installPreviewScrollbar(iframe)
    const styles = iframe.contentDocument!.head.querySelectorAll('style[data-clawbench-scrollbar]')
    expect(styles).toHaveLength(1)
    iframe.remove()
  })

  it('returns false for a missing iframe instead of throwing', () => {
    expect(installPreviewScrollbar(null)).toBe(false)
  })
})

/**
 * installPreviewSearchStyles gives the previewed document the match-highlight
 * colours. The mark classes come from documentSearch.ts; without this sheet a
 * find inside an HTML preview would wrap matches in invisible <mark>s.
 */
describe('installPreviewSearchStyles', () => {
  function makeIframe(headHtml = '<title>t</title>'): HTMLIFrameElement {
    const iframe = document.createElement('iframe')
    document.body.appendChild(iframe)
    iframe.contentDocument!.head.innerHTML = headHtml
    return iframe
  }

  it('injects the highlight sheet into the preview document', () => {
    const iframe = makeIframe()
    expect(installPreviewSearchStyles(iframe, '#ff0000')).toBe(true)
    const style = iframe.contentDocument!.head.querySelector('style[data-clawbench-search]')
    expect(style).not.toBeNull()
    // Both classes the engine applies must be styled.
    expect(style!.textContent).toContain('mark.md-search-match')
    expect(style!.textContent).toContain('mark.md-search-match-active')
    // The accent is inlined (the preview document cannot read the host's vars).
    expect(style!.textContent).toContain('#ff0000')
    // The active-match flash must be defined IN the frame: `line-flash` lives in
    // the host document and does not apply here.
    expect(style!.textContent).toContain('@keyframes clawbench-search-flash')
    expect(style!.textContent).toMatch(/\.search-match-flash\s*{[\s\S]*?animation: clawbench-search-flash/)
    // Reduced motion drops the blink (the static tint still marks the match).
    expect(style!.textContent).toMatch(/@media \(prefers-reduced-motion: reduce\)[\s\S]*?animation: none !important/)
    iframe.remove()
  })

  it('is idempotent — a reload does not stack copies', () => {
    const iframe = makeIframe()
    installPreviewSearchStyles(iframe, '#ff0000')
    installPreviewSearchStyles(iframe, '#ff0000')
    expect(iframe.contentDocument!.head.querySelectorAll('style[data-clawbench-search]')).toHaveLength(1)
    iframe.remove()
  })

  it('appends (so a broad mark rule in the document still loses to ours)', () => {
    const iframe = makeIframe('<style>mark{}</style>')
    installPreviewSearchStyles(iframe, '#ff0000')
    const head = iframe.contentDocument!.head
    const ours = head.querySelector('style[data-clawbench-search]')!
    const own = [...head.children].find(
      (el) => el.tagName === 'STYLE' && !el.hasAttribute('data-clawbench-search'),
    )!
    expect(
      own.compareDocumentPosition(ours) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy()
    iframe.remove()
  })

  it('returns false for a missing iframe', () => {
    expect(installPreviewSearchStyles(null)).toBe(false)
  })
})
