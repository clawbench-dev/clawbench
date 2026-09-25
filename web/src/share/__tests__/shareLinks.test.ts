import { describe, expect, it, beforeEach, afterEach } from 'vitest'
import {
  annotateShareLinks,
  annotateShareLinksIn,
  handleShareLinkClick,
  resolveShareTarget,
  shareDeepLink,
  SHARE_OPEN_FILE_EVENT,
  SHARE_PATH_ATTR,
} from '@/share/shareLinks'
import { setShareToken } from '@/share/shareMode'

afterEach(() => {
  setShareToken(null)
})

describe('resolveShareTarget', () => {
  it('resolves a sibling reference against an absolute dir', () => {
    expect(resolveShareTarget('/repo/docs', 'guide.md')).toBe('/repo/docs/guide.md')
  })

  it('collapses . and .. segments', () => {
    expect(resolveShareTarget('/repo/docs', './guide.md')).toBe('/repo/docs/guide.md')
    expect(resolveShareTarget('/repo/docs', '../images/a.png')).toBe('/repo/images/a.png')
    expect(resolveShareTarget('/repo/docs/sub', '../../x.md')).toBe('/repo/x.md')
  })

  it('does not climb above the filesystem root', () => {
    expect(resolveShareTarget('/a', '../../etc/passwd')).toBe('/etc/passwd')
  })

  it('keeps a Windows drive as the leading segment (no stray leading slash)', () => {
    // A leading "/" would make the backend treat it as a POSIX absolute path.
    expect(resolveShareTarget('E:/proj/docs', 'guide.md')).toBe('E:/proj/docs/guide.md')
    expect(resolveShareTarget('E:\\proj\\docs', 'guide.md')).toBe('E:/proj/docs/guide.md')
  })

  it('preserves the root for a depth-1 base', () => {
    expect(resolveShareTarget('/', 'a.md')).toBe('/a.md')
  })
})

describe('shareDeepLink', () => {
  it('builds a token-scoped deep link with the encoded target', () => {
    setShareToken('tok1')
    expect(shareDeepLink('/repo/docs/中文.md')).toBe(
      '/share/tok1?path=' + encodeURIComponent('/repo/docs/中文.md')
    )
  })

  it('points at the share itself when no target is given', () => {
    setShareToken('tok1')
    expect(shareDeepLink('')).toBe('/share/tok1')
  })
})

describe('annotateShareLinksIn / annotateShareLinks', () => {
  beforeEach(() => setShareToken('tok1'))

  it('tags and rewrites a local relative link', () => {
    const html = annotateShareLinks('<p><a href="./docs/guide.md">Guide</a></p>', '/repo')
    expect(html).toContain(SHARE_PATH_ATTR + '="/repo/docs/guide.md"')
    expect(html).toContain('class="share-file-link"')
    expect(html).toContain('href="/share/tok1?path=%2Frepo%2Fdocs%2Fguide.md"')
  })

  it('leaves external, anchor, absolute and data: links untouched', () => {
    const cases = [
      '<a href="https://x.com/a.md">x</a>',
      '<a href="//cdn.x.com/a.md">x</a>',
      '<a href="#section">x</a>',
      '<a href="/site-root.md">x</a>',
      '<a href="data:text/plain,hi">x</a>',
      '<a href="mailto:a@b.c">x</a>',
    ]
    for (const html of cases) {
      const out = annotateShareLinks(html, '/repo/docs')
      expect(out).not.toContain(SHARE_PATH_ATTR)
      expect(out).not.toContain('share-file-link')
    }
  })

  it('carries a line range through to the deep link target', () => {
    // A trailing :N is part of the path grammar; the resolved target keeps it
    // out of the path so the backend can stat the file itself.
    const html = annotateShareLinks('<a href="guide.md:12">g</a>', '/repo/docs')
    expect(html).toContain(SHARE_PATH_ATTR + '="/repo/docs/guide.md"')
  })

  it('annotates multiple links in one document', () => {
    const doc = new DOMParser().parseFromString(
      '<a href="a.md">a</a><a href="sub/b.md">b</a><a href="https://x.com">c</a>',
      'text/html'
    )
    const n = annotateShareLinksIn(doc, '/repo/docs')
    expect(n).toBe(2)
  })

  it('is a no-op without a base dir', () => {
    const html = '<a href="a.md">a</a>'
    expect(annotateShareLinks(html, '')).toBe(html)
  })

  it('is a no-op on empty input', () => {
    expect(annotateShareLinks('', '/repo/docs')).toBe('')
  })
})

describe('handleShareLinkClick', () => {
  beforeEach(() => {
    setShareToken('tok1')
    document.body.innerHTML = ''
  })

  function clickOn(html: string, init: MouseEventInit = {}): { handled: boolean; events: string[] } {
    document.body.innerHTML = html
    const anchor = document.body.querySelector('a') as HTMLAnchorElement
    const events: string[] = []
    const onOpen = (e: Event) => events.push((e as CustomEvent).detail.path)
    window.addEventListener(SHARE_OPEN_FILE_EVENT, onOpen)
    // A constructed MouseEvent has no `target` until it is dispatched, and
    // handleShareLinkClick resolves the anchor from event.target — so the click
    // must go through the DOM rather than being passed in directly.
    let handled = false
    const capture = (e: Event) => { handled = handleShareLinkClick(e as MouseEvent) }
    document.body.addEventListener('click', capture)
    try {
      anchor.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true, ...init }))
      return { handled, events }
    } finally {
      document.body.removeEventListener('click', capture)
      window.removeEventListener(SHARE_OPEN_FILE_EVENT, onOpen)
    }
  }

  it('intercepts a plain click and dispatches the open event', () => {
    const { handled, events } = clickOn(
      `<a ${SHARE_PATH_ATTR}="/repo/docs/guide.md" href="/share/tok1?path=x">G</a>`
    )
    expect(handled).toBe(true)
    expect(events).toEqual(['/repo/docs/guide.md'])
  })

  it('leaves modifier clicks to the browser (new tab)', () => {
    for (const init of [{ ctrlKey: true }, { metaKey: true }, { shiftKey: true }]) {
      const { handled, events } = clickOn(
        `<a ${SHARE_PATH_ATTR}="/repo/docs/guide.md" href="/share/tok1?path=x">G</a>`,
        init
      )
      expect(handled).toBe(false)
      expect(events).toEqual([])
    }
  })

  it('ignores clicks that are not on an annotated link', () => {
    const { handled, events } = clickOn('<a href="/share/tok1?path=x">plain</a>')
    expect(handled).toBe(false)
    expect(events).toEqual([])
  })

  it('resolves the annotated link from a click on a child element', () => {
    // The event target may be an inner element (e.g. <code> inside <a>).
    document.body.innerHTML = `<a ${SHARE_PATH_ATTR}="/repo/docs/guide.md" href="/x"><code>G</code></a>`
    const code = document.body.querySelector('code') as HTMLElement
    let handled = false
    const capture = (e: Event) => { handled = handleShareLinkClick(e as MouseEvent) }
    document.body.addEventListener('click', capture)
    try {
      code.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }))
    } finally {
      document.body.removeEventListener('click', capture)
    }
    expect(handled).toBe(true)
  })

  it('returns false for a click outside any anchor', () => {
    document.body.innerHTML = '<p>text</p>'
    const p = document.body.querySelector('p') as HTMLElement
    let handled = true
    const capture = (e: Event) => { handled = handleShareLinkClick(e as MouseEvent) }
    document.body.addEventListener('click', capture)
    try {
      p.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }))
    } finally {
      document.body.removeEventListener('click', capture)
    }
    expect(handled).toBe(false)
  })
})

describe('annotateShareLinksIn — non-anchor elements', () => {
  it('does not annotate images or other tags with href-like attrs', () => {
    setShareToken('tok1')
    const doc = new DOMParser().parseFromString(
      '<img src="a.md"><link rel="x" href="b.md">',
      'text/html'
    )
    expect(annotateShareLinksIn(doc, '/repo/docs')).toBe(0)
  })
})

describe('shareLinks — integration through the markdown pipeline', () => {
  it('annotates a relative link end-to-end via buildMarkdownPreviewDom', async () => {
    const { buildMarkdownPreviewDom } = await import('@/composables/useMarkdownRenderPipeline')
    const { configureMarkedRenderer } = await import('@/utils/markedConfig')
    configureMarkedRenderer()
    setShareToken('tokpipe')
    try {
      const { html, detectedPaths } = buildMarkdownPreviewDom(
        { content: '[Guide](./docs/guide.md)', path: '/repo/readme.md' },
        { isPC: true, imageTimestamp: 1 }
      )
      expect(detectedPaths).toEqual([])
      expect(html).toContain(SHARE_PATH_ATTR + '="/repo/docs/guide.md"')
      expect(html).toContain('href="/share/tokpipe?path=%2Frepo%2Fdocs%2Fguide.md"')
      // The in-app (auth-bound) annotation must still be absent.
      expect(html).not.toContain('chat-file-open-btn')
      expect(html).not.toContain('data-file-path')
    } finally {
      setShareToken(null)
    }
  })
})
