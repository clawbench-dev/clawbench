import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { nextTick } from 'vue'
import { createI18n } from 'vue-i18n'

// ── Child component mocks ──────────────────────────────────────────────────
// ShareView mounts a chain of heavy preview components (PdfPreview pulls
// pdfjs-dist, MarkdownPreview pulls the whole markdown render pipeline with
// KaTeX/Mermaid globals). Stub each so the test only exercises ShareView's
// own view-selection / toggle / TOC routing logic.
const baseStub = vi.hoisted(() => (name: string, extra = '') => ({
  name,
  props: ['file'],
  template: `<div class="${name.toLowerCase()}-stub">${extra}</div>`,
}))

vi.mock('@/components/common/LoadingIndicator.vue', () => ({
  default: { name: 'LoadingIndicator', props: ['size'], template: '<div class="loading-indicator-stub" />' },
}))
vi.mock('@/components/common/FileIcon.vue', () => ({
  default: { name: 'FileIcon', props: ['path'], template: '<div class="file-icon-stub">{{ path }}</div>' },
}))
vi.mock('@/components/media/PdfPreview.vue', () => ({ default: baseStub('PdfPreview') }))
vi.mock('@/components/media/Lightbox.vue', () => ({
  default: { name: 'Lightbox', template: '<div class="lightbox-stub" />' },
}))
vi.mock('@/components/media/AudioPreview.vue', () => ({ default: baseStub('AudioPreview') }))
vi.mock('@/components/media/VideoPreview.vue', () => ({ default: baseStub('VideoPreview') }))
vi.mock('@/components/file/MarkdownPreview.vue', () => ({
  default: {
    name: 'MarkdownPreview',
    props: ['file', 'viewMode', 'wordWrap'],
    template: '<div class="markdown-preview-stub" :data-view-mode="viewMode">{{ file?.name }}</div>',
  },
}))

vi.mock('@/stores/app.ts', () => ({
  store: { state: { projectRoot: '', homeDir: '' } },
}))

// OfficePreview / OpenApiPreview / CodeMirrorViewer are loaded through
// defineAsyncComponent (a delay timer would leak in the test env), so instead
// of module-mocking them they are stubbed per-mount via VTU's `stubs` option —
// VTU replaces the async wrapper type before its loader ever runs.
const asyncComponentStubs = {
  CodeMirrorViewer: {
    name: 'CodeMirrorViewer',
    props: ['file', 'content', 'language', 'wordWrap', 'showLineNumbers', 'editable'],
    template: '<div class="cm-viewer-stub">{{ language }}</div>',
  },
  OfficePreview: baseStub('OfficePreview'),
  OpenApiPreview: baseStub('OpenApiPreview'),
}

import ShareView from '@/share/ShareView.vue'
import { setShareToken } from '@/share/shareMode'

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      share: {
        loading: 'Loading...',
        download: 'Download original',
        toggleToc: 'Toggle table of contents',
        toc: 'Contents',
        invalidTitle: 'Cannot view this share',
        invalidUrl: 'Invalid link format',
        notFound: 'Not found',
        noPreview: 'No preview',
        tooLarge: 'Too large to preview',
        renderedView: 'Rendered preview',
        sourceView: 'View source',
        back: 'Back',
        linkUnavailable: 'This linked file is unavailable or outside the shared scope',
        linkUnavailableTitle: 'Cannot open this file',
      },
      common: { download: 'Download' },
      imageBlock: { view: 'View image', openFile: 'Open file' },
    },
  },
})

const originalFetch = globalThis.fetch
const mountedWrappers: Array<{ unmount: () => void }> = []

beforeEach(() => {
  window.history.replaceState({}, '', '/share/tokShareTest')
  setShareToken(null)
  globalThis.fetch = vi.fn().mockResolvedValue({
    ok: true,
    json: async () => ({ name: 'README.md', path: '/repo/README.md', content: '# Hello\nworld' }),
  })
})

afterEach(() => {
  globalThis.fetch = originalFetch
  window.history.replaceState({}, '', '/')
  vi.restoreAllMocks()
  for (const w of mountedWrappers.splice(0)) w.unmount()
})

async function mountShare(
  file: Record<string, unknown>,
  fetchImpl?: (url: string) => Promise<{ ok: boolean; json: () => Promise<unknown> }>
) {
  ;(globalThis.fetch as ReturnType<typeof vi.fn>).mockImplementation(async (url: string | URL | Request) => {
    if (fetchImpl) return fetchImpl(String(url))
    return {
      ok: true,
      json: async () => file,
    }
  })
  const wrapper = mount(ShareView, {
    global: { plugins: [i18n], stubs: asyncComponentStubs },
  })
  mountedWrappers.push(wrapper)
  await flushPromises()
  await nextTick()
  await flushPromises()
  return wrapper
}

// CodeMirrorViewer acks each `cm-scroll-to-line` it applies with a matching
// `cm-scroll-to-line-handled`. The real component does this internally, but the
// test stub does not — register a harness ack so ShareView's retry loop stops
// after the first dispatch instead of running its rAF frame budget.
function ackLineScrolls() {
  const onRequest = (e: Event) => {
    const detail = (e as CustomEvent).detail
    if (typeof detail?.requestId === 'number' && typeof detail.line === 'number') {
      window.dispatchEvent(new CustomEvent('cm-scroll-to-line-handled', { detail: { requestId: detail.requestId } }))
    }
  }
  window.addEventListener('cm-scroll-to-line', onRequest)
  return () => window.removeEventListener('cm-scroll-to-line', onRequest)
}

describe('ShareView — view toggle (rendered ⇄ source)', () => {
  it('defaults markdown files to the rendered preview and exposes the toggle', async () => {
    const wrapper = await mountShare({ name: 'README.md', path: '/repo/README.md', content: '# Hello\nbody' })
    expect(wrapper.find('.markdown-preview-stub').exists()).toBe(true)
    const toggle = wrapper.find('.share-view-toggle')
    expect(toggle.exists()).toBe(true)
    // Rendered preview is active by default.
    expect(toggle.classes()).toContain('active')
  })

  it('switches markdown to the CodeMirror source view and back', async () => {
    const wrapper = await mountShare({ name: 'README.md', path: '/repo/README.md', content: '# Hello\nbody' })
    const toggle = wrapper.find('.share-view-toggle')

    await toggle.trigger('click')
    await flushPromises()
    expect(wrapper.find('.markdown-preview-stub').exists()).toBe(false)
    expect(wrapper.find('.cm-viewer-stub').exists()).toBe(true)
    // The editor is read-only and carries the markdown language.
    expect(wrapper.find('.cm-viewer-stub').text()).toContain('markdown')
    expect(toggle.classes()).not.toContain('active')

    await toggle.trigger('click')
    await flushPromises()
    expect(wrapper.find('.markdown-preview-stub').exists()).toBe(true)
    expect(wrapper.find('.cm-viewer-stub').exists()).toBe(false)
  })

  it('marks the content scroller for rendered markdown so wide screens lift the outer cap', async () => {
    // Rendered markdown preview: the scroll container must be full-width so
    // the scrollbar hugs the viewport edge (the 900px reading column is capped
    // by the shared .markdown-body padding rule instead).
    const wrapper = await mountShare({ name: 'README.md', path: '/repo/README.md', content: '# Hello\nbody' })
    expect(wrapper.find('.share-content').attributes('data-markdown-rendered')).toBeDefined()

    // After toggling to the raw source view the attribute is removed — the
    // CodeMirror pane keeps the generic wide-screen cap (no markdown column).
    await wrapper.find('.share-view-toggle').trigger('click')
    await flushPromises()
    expect(wrapper.find('.share-content').attributes('data-markdown-rendered')).toBeUndefined()
  })

  it('does not lift the wide-screen cap for non-markdown files', async () => {
    const wrapper = await mountShare({ name: 'main.go', path: '/repo/main.go', content: 'package main\n' })
    expect(wrapper.find('.share-content').attributes('data-markdown-rendered')).toBeUndefined()
  })

  it('does not expose the toggle for pure code/plain-text files', async () => {
    const wrapper = await mountShare({ name: 'main.go', path: '/repo/main.go', content: 'package main\n' })
    expect(wrapper.find('.share-view-toggle').exists()).toBe(false)
    // Code files render through the code viewer directly (no preview branch).
    expect(wrapper.find('.cm-viewer-stub').exists()).toBe(true)
  })

  it('toggles HTML files between the rendered iframe and source', async () => {
    const wrapper = await mountShare({
      name: 'page.html',
      path: '/repo/page.html',
      content: '<html><body>hi</body></html>',
    })
    expect(wrapper.find('iframe.share-html-iframe').exists()).toBe(true)
    expect(wrapper.find('.share-view-toggle').exists()).toBe(true)

    await wrapper.find('.share-view-toggle').trigger('click')
    await flushPromises()
    expect(wrapper.find('iframe.share-html-iframe').exists()).toBe(false)
    expect(wrapper.find('.cm-viewer-stub').exists()).toBe(true)
  })

  it('toggles OpenAPI specs between the Swagger viewer and source', async () => {
    const wrapper = await mountShare({
      name: 'api.yaml',
      path: '/repo/api.yaml',
      subtype: 'openapi',
      content: 'openapi: 3.0.0',
    })
    expect(wrapper.find('.openapipreview-stub').exists()).toBe(true)
    expect(wrapper.find('.share-view-toggle').exists()).toBe(true)

    await wrapper.find('.share-view-toggle').trigger('click')
    await flushPromises()
    expect(wrapper.find('.openapipreview-stub').exists()).toBe(false)
    expect(wrapper.find('.cm-viewer-stub').exists()).toBe(true)
  })

  it('keeps TOC line jumps working in the raw markdown source view', async () => {
    const wrapper = await mountShare({ name: 'README.md', path: '/repo/README.md', content: '# Hello\nbody' })
    await wrapper.find('.share-view-toggle').trigger('click')
    await flushPromises()

    const listener = vi.fn()
    const stopAck = ackLineScrolls()
    window.addEventListener('cm-scroll-to-line', listener)
    try {
      const item = wrapper.findAll('.share-toc-item')[0]
      expect(item).toBeTruthy()
      await item.trigger('click')
      await nextTick()
      // A CodeMirror-rendered body routes the jump through cm-scroll-to-line.
      expect(listener).toHaveBeenCalledTimes(1)
      const detail = listener.mock.calls[0][0].detail
      expect(detail.line).toBeGreaterThan(0)
      expect(detail.path).toBe('/repo/README.md')
      expect(typeof detail.requestId).toBe('number')
    } finally {
      window.removeEventListener('cm-scroll-to-line', listener)
      stopAck()
    }
  })

  it('jumps code/plain-text files to a TOC line through cm-scroll-to-line', async () => {
    // A pure code file has no rendered preview, so no toggle is shown — but its
    // TOC rail is still active and must route through the code viewer.
    const wrapper = await mountShare({
      name: 'run.sh',
      path: '/repo/run.sh',
      content: '#!/bin/bash\nmain() {\n  echo hi\n}\nmain\n',
    })
    expect(wrapper.find('.share-view-toggle').exists()).toBe(false)

    const listener = vi.fn()
    const stopAck = ackLineScrolls()
    window.addEventListener('cm-scroll-to-line', listener)
    try {
      const item = wrapper.findAll('.share-toc-item')[0]
      expect(item).toBeTruthy()
      await item.trigger('click')
      await nextTick()
      expect(listener).toHaveBeenCalledTimes(1)
      const detail = listener.mock.calls[0][0].detail
      expect(detail.line).toBeGreaterThan(0)
      expect(detail.path).toBe('/repo/run.sh')
    } finally {
      window.removeEventListener('cm-scroll-to-line', listener)
      stopAck()
    }
  })

  it('jumps HTML source-view TOC entries by line after toggling to raw', async () => {
    // Generic source extraction picks indented `key {` style lines, so this
    // content yields a TOC item that only has meaning in the raw source view.
    const wrapper = await mountShare({
      name: 'page.html',
      path: '/repo/page.html',
      content: 'body {\n  color: red;\n}\n<div id="app"></div>',
    })
    await wrapper.find('.share-view-toggle').trigger('click')
    await flushPromises()
    expect(wrapper.find('iframe.share-html-iframe').exists()).toBe(false)
    expect(wrapper.find('.cm-viewer-stub').exists()).toBe(true)

    const listener = vi.fn()
    const stopAck = ackLineScrolls()
    window.addEventListener('cm-scroll-to-line', listener)
    try {
      const item = wrapper.findAll('.share-toc-item')[0]
      expect(item).toBeTruthy()
      await item.trigger('click')
      await nextTick()
      expect(listener).toHaveBeenCalledTimes(1)
      const detail = listener.mock.calls[0][0].detail
      expect(detail.line).toBeGreaterThan(0)
      expect(detail.path).toBe('/repo/page.html')
    } finally {
      window.removeEventListener('cm-scroll-to-line', listener)
      stopAck()
    }
  })
})

describe('ShareView — in-place navigation to referenced files', () => {
  // Files served by the mocked token endpoints, keyed by the ?path= value.
  const SHARED = { name: 'README.md', path: '/repo/README.md', content: '# Root\n[Guide](./docs/guide.md)' }
  const LINKED = { name: 'guide.md', path: '/repo/docs/guide.md', content: '# Guide\nbody' }

  /** Resolve the token endpoint by URL, mimicking the backend's routing. */
  function endpoint(overrides: Record<string, unknown> = {}) {
    const calls: string[] = []
    const impl = async (u: string) => {
      calls.push(u)
      const path = u.includes('?path=') ? decodeURIComponent(u.split('?path=')[1]) : ''
      if (path) {
        const body = path in overrides ? overrides[path] : (path === LINKED.path ? LINKED : SHARED)
        return { ok: body !== null, json: async () => body }
      }
      return { ok: true, json: async () => SHARED }
    }
    return { impl, calls }
  }

  it('opens a ?path= deep link straight to the referenced file', async () => {
    window.history.replaceState({}, '', '/share/tokShareTest?path=' + encodeURIComponent(LINKED.path))
    const { impl, calls } = endpoint()
    const wrapper = await mountShare(SHARED, impl)

    expect(calls.some(c => c.includes('/content?path='))).toBe(true)
    expect(wrapper.find('.markdown-preview-stub').text()).toContain('guide.md')
  })

  it('switches documents in place when a share link is followed', async () => {
    const { impl } = endpoint()
    const wrapper = await mountShare(SHARED, impl)
    expect(wrapper.find('.share-back-btn').exists()).toBe(false)

    window.dispatchEvent(new CustomEvent('share-open-file', { detail: { path: LINKED.path } }))
    await flushPromises()
    await nextTick()
    await flushPromises()

    // The referenced document replaced the shared one, and Back appeared.
    expect(wrapper.find('.markdown-preview-stub').text()).toContain('guide.md')
    expect(wrapper.find('.share-back-btn').exists()).toBe(true)
    // The URL now deep-links to the referenced file (reload/share friendly).
    expect(decodeURIComponent(window.location.search)).toContain(LINKED.path)
  })

  it('returns to the shared file via the Back button', async () => {
    const { impl } = endpoint()
    const wrapper = await mountShare(SHARED, impl)

    window.dispatchEvent(new CustomEvent('share-open-file', { detail: { path: LINKED.path } }))
    await flushPromises()
    await flushPromises()
    expect(wrapper.find('.markdown-preview-stub').text()).toContain('guide.md')

    await wrapper.find('.share-back-btn').trigger('click')
    // jsdom dispatches popstate asynchronously after history.back().
    await vi.waitFor(() => {
      expect(wrapper.find('.markdown-preview-stub').text()).toContain('README.md')
    })
    expect(wrapper.find('.share-back-btn').exists()).toBe(false)
  })

  it('reports an unavailable linked file without losing the share', async () => {
    // The content endpoint 404s for this target (deleted / out of scope / dir).
    const { impl } = endpoint({ [LINKED.path]: null })
    const wrapper = await mountShare(SHARED, impl)

    window.dispatchEvent(new CustomEvent('share-open-file', { detail: { path: LINKED.path } }))
    await flushPromises()
    await flushPromises()

    expect(wrapper.find('.share-error-state').exists()).toBe(true)
    expect(wrapper.find('.share-error-desc').text()).toContain('unavailable')
    // Back is still offered so the reader is not stranded on the dead link.
    expect(wrapper.find('.share-error-back').exists()).toBe(true)
  })

  it('downloads the referenced file (not the shared one) after a switch', async () => {
    const { impl } = endpoint()
    const wrapper = await mountShare(SHARED, impl)
    window.dispatchEvent(new CustomEvent('share-open-file', { detail: { path: LINKED.path } }))
    await flushPromises()
    await flushPromises()

    const href = wrapper.find('a.share-btn[download]').attributes('href') || ''
    expect(href).toContain('/download?path=')
    expect(decodeURIComponent(href)).toContain(LINKED.path)
  })

  it('ignores a share-open-file event for the file already on screen', async () => {
    const { impl, calls } = endpoint()
    const wrapper = await mountShare(SHARED, impl)
    const before = calls.length

    window.dispatchEvent(new CustomEvent('share-open-file', { detail: { path: SHARED.path } }))
    await flushPromises()
    expect(calls.length).toBe(before)
    expect(wrapper.find('.share-back-btn').exists()).toBe(false)
  })
})

// jsdom does not implement CSS.escape (used by ShareView.scrollToHeading).
// Provide the standard algorithm so TOC jumps can be exercised in tests.
if (typeof (globalThis as { CSS?: { escape?: unknown } }).CSS === 'undefined') {
  ;(globalThis as { CSS: { escape?: (s: string) => string } }).CSS = {} as never
}
;(globalThis as { CSS: { escape?: (s: string) => string } }).CSS.escape = (s: string) =>
  s.replace(/[^a-zA-Z0-9_-]/g, (c) => '\\' + c)

describe('ShareView — image blocks + narrow TOC drawer', () => {
  it('renders a single-file image as an image-block figure with a view button', async () => {
    const wrapper = await mountShare({ name: 'photo.png', path: '/repo/photo.png', content: '' })
    // The figure reuses the app markdown image-block classes.
    expect(wrapper.find('.image-block-wrapper').exists()).toBe(true)
    expect(wrapper.find('.image-block-header .image-block-view-btn').exists()).toBe(true)
    // No attach/open actions on the public share page.
    expect(wrapper.find('.image-block-attach-btn').exists()).toBe(false)
    expect(wrapper.find('.image-block-open-btn').exists()).toBe(false)
    const img = wrapper.find('.share-image-img')
    expect(img.exists()).toBe(true)
    expect(img.attributes('src')).toContain('/api/share/tokShareTest/local')
  })

  it('renders an SVG file the same way', async () => {
    const wrapper = await mountShare({ name: 'logo.svg', path: '/repo/logo.svg', content: '' })
    expect(wrapper.find('.image-block-header .image-block-view-btn').exists()).toBe(true)
    const img = wrapper.find('.share-image-img')
    expect(img.attributes('src')).toContain('/api/share/tokShareTest/local')
  })

  it('uses a wide-screen inline TOC rail and no drawer by default', async () => {
    // jsdom has no matchMedia → syncNarrow stays false (wide layout).
    const wrapper = await mountShare({ name: 'doc.md', path: '/repo/doc.md', content: '# H\n## S' })
    // tocOpen defaults true on load → rail rendered.
    expect(wrapper.find('.share-body .share-toc').exists()).toBe(true)
    expect(wrapper.find('.share-toc-backdrop').exists()).toBe(false)
    expect(wrapper.find('.share-toc-panel').exists()).toBe(false)
  })

  it('opens a slide-in TOC drawer on narrow screens when the toggle is tapped', async () => {
    vi.stubGlobal('matchMedia', vi.fn((query: string) => ({
      matches: true,
      media: query,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    })))
    try {
      const wrapper = await mountShare({ name: 'doc.md', path: '/repo/doc.md', content: '# H\n## S' })
      // Narrow + tocOpen=false on load → no rail, no drawer yet.
      expect(wrapper.find('.share-body .share-toc').exists()).toBe(false)
      expect(document.body.querySelector('.share-toc-panel')).toBeNull()

      // Tap the TOC top-bar button (title = toggleToc) → drawer appears.
      const tocBtn = wrapper.find('.share-top-actions .share-btn[title="Toggle table of contents"]')
      expect(tocBtn.exists()).toBe(true)
      await tocBtn.trigger('click')
      await nextTick()
      expect(document.body.querySelector('.share-toc-backdrop')).not.toBeNull()
      expect(document.body.querySelector('.share-toc-panel')).not.toBeNull()

      // Backdrop click closes the drawer.
      ;(document.body.querySelector('.share-toc-backdrop') as HTMLElement).dispatchEvent(
        new MouseEvent('click', { bubbles: true, cancelable: true }),
      )
      await nextTick()
      expect(document.body.querySelector('.share-toc-panel')).toBeNull()
    } finally {
      vi.unstubAllGlobals()
    }
  })

  it('closes the drawer after jumping to a TOC item', async () => {
    vi.stubGlobal('matchMedia', vi.fn(() => ({ matches: true, media: '', addEventListener: vi.fn(), removeEventListener: vi.fn() })))
    try {
      const wrapper = await mountShare({ name: 'doc.md', path: '/repo/doc.md', content: '# Hello\n\nbody text' })
      const tocBtn = wrapper.find('.share-top-actions .share-btn[title="Toggle table of contents"]')
      await tocBtn.trigger('click')
      await nextTick()
      expect(document.body.querySelector('.share-toc-panel')).not.toBeNull()

      const item = document.body.querySelector('.share-toc-item') as HTMLElement | null
      expect(item).not.toBeNull()
      item!.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }))
      await nextTick()
      await flushPromises()
      expect(document.body.querySelector('.share-toc-panel')).toBeNull()
    } finally {
      vi.unstubAllGlobals()
    }
  })
})

// The backend answers 200 + tooLarge (empty content) for files past its inline
// cap instead of rejecting the link — every file is shareable at any size.
// The SPA must degrade to the download card, not report a broken link.
describe('ShareView — over-cap files fall back to download', () => {
  it('shows the download card with the too-large notice, not an invalid-link error', async () => {
    const wrapper = await mountShare({
      name: 'huge.md',
      path: '/repo/huge.md',
      content: '',
      size: 200 * 1024 * 1024,
      tooLarge: true,
    })

    // Not the error state: the link is valid and the file is downloadable.
    expect(wrapper.find('.share-error-state').exists()).toBe(false)
    expect(wrapper.find('.share-error-title').exists()).toBe(false)

    // Download card, with the size-specific notice.
    const unsupported = wrapper.find('.share-unsupported')
    expect(unsupported.exists()).toBe(true)
    expect(unsupported.text()).toContain('Too large to preview')
    const link = unsupported.find('a.share-download-btn')
    expect(link.exists()).toBe(true)
    expect(link.attributes('href')).toContain('/api/share/tokShareTest/download')
  })

  it('does not render an empty markdown preview for an over-cap markdown file', async () => {
    // A .md over the cap still reports its extension, so the rendered branch
    // would match — it must be gated on the content actually being present.
    const wrapper = await mountShare({
      name: 'huge.md',
      path: '/repo/huge.md',
      content: '',
      tooLarge: true,
    })
    expect(wrapper.find('.markdown-preview-stub').exists()).toBe(false)
    expect(wrapper.find('.cm-viewer-stub').exists()).toBe(false)
    // No source/rendered toggle either — there is no source to show.
    expect(wrapper.find('.share-view-toggle').exists()).toBe(false)
    // TOC would be built from absent content.
    expect(wrapper.find('.share-body .share-toc').exists()).toBe(false)
  })

  it('still renders media previews for over-cap files (they stream, not inline)', async () => {
    // Images/audio/video/PDF resolve through the token-scoped /local endpoint
    // with Range support, so a 500MB video previews without being inlined.
    const wrapper = await mountShare({
      name: 'huge.mp4',
      path: '/repo/huge.mp4',
      content: '',
      size: 500 * 1024 * 1024,
      tooLarge: true,
    })
    expect(wrapper.find('.videopreview-stub').exists()).toBe(true)
    expect(wrapper.find('.share-unsupported').exists()).toBe(false)
  })

  it('still renders the PDF preview for an over-cap PDF', async () => {
    const wrapper = await mountShare({
      name: 'huge.pdf',
      path: '/repo/huge.pdf',
      content: '',
      size: 200 * 1024 * 1024,
      tooLarge: true,
    })
    expect(wrapper.find('.pdfpreview-stub').exists()).toBe(true)
    expect(wrapper.find('.share-unsupported').exists()).toBe(false)
  })
})
