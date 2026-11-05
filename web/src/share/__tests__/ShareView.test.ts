import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

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

// Module-level mock (not a VTU stub): an SFC has no explicit `name` option, so
// a stubs-key match would silently fall through to the real component.
vi.mock('@/components/file/TablePreviewBody.vue', () => ({
  default: {
    name: 'TablePreviewBody',
    props: ['file'],
    template: '<div class="table-preview-stub">{{ file?.name }}</div>',
  },
}))

vi.mock('@/stores/app.ts', () => ({
  store: { state: { projectRoot: '', homeDir: '' } },
}))

// The download button routes through this helper (which streams via XHR and
// drives the progress bar). Mocking it keeps the test on ShareView's own
// URL-selection logic and off the network.
// Spread the real module: ShareView also imports buildLocalFileUrl from it,
// and a partial mock would drop that export (breaking image-block rendering).
const { mockDownloadUrlWithProgress } = vi.hoisted(() => ({
  mockDownloadUrlWithProgress: vi.fn(),
}))
vi.mock('@/utils/download.ts', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/utils/download.ts')>()),
  downloadUrlWithProgress: mockDownloadUrlWithProgress,
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
  // Read-only canvas viewer (the real one is an iframe host). Exposes the
  // readonly prop so the share page can be held to mounting it read-only.
  ExcalidrawViewer: {
    name: 'ExcalidrawViewer',
    props: { file: Object, readonly: { type: Boolean, default: false } },
    template: '<div class="excalidrawviewer-stub" :data-readonly="String(readonly)" />',
  },
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
  it('shows the ClawBench brand logo in the topbar', async () => {
    const wrapper = await mountShare({ name: 'README.md', path: '/repo/README.md', content: '# Hello\nbody' })
    const logo = wrapper.find('.share-topbar .share-logo')
    expect(logo.exists()).toBe(true)
    expect(logo.attributes('src')).toBe('/logo-64.png')
    expect(logo.attributes('alt')).toBe('ClawBench')
  })

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

  it('renders a CSV file as a data table and toggles to the source view', async () => {
    const wrapper = await mountShare({
      name: 'data.csv',
      path: '/repo/data.csv',
      content: 'name,qty\napple,3',
    })

    expect(wrapper.find('.table-preview-stub').exists()).toBe(true)
    expect(wrapper.find('.cm-viewer-stub').exists()).toBe(false)
    // Wide screens must lift the 1080px reading-column cap for a data table.
    expect(wrapper.find('.share-content').attributes('data-table-rendered')).toBeDefined()

    const toggle = wrapper.find('.share-view-toggle')
    expect(toggle.exists()).toBe(true)
    await toggle.trigger('click')
    await flushPromises()
    expect(wrapper.find('.table-preview-stub').exists()).toBe(false)
    expect(wrapper.find('.cm-viewer-stub').exists()).toBe(true)
    expect(wrapper.find('.share-content').attributes('data-table-rendered')).toBeUndefined()
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

  it('marks the content scroller for rendered HTML so wide screens lift the outer cap', async () => {
    // Rendered HTML preview fills its parent iframe, so the scroll container
    // must be full-width — a fixed 1080px column would letterbox arbitrary
    // HTML layouts. Only the TOC rail (when present) reserves space.
    const wrapper = await mountShare({
      name: 'page.html',
      path: '/repo/page.html',
      content: '<html><body>hi</body></html>',
    })
    expect(wrapper.find('iframe.share-html-iframe').exists()).toBe(true)
    expect(wrapper.find('.share-content').attributes('data-html-rendered')).toBeDefined()

    // The document's own links must be clickable: without popups (and the
    // escape, so the new tab is not sandboxed into an opaque origin) a
    // `target="_blank"` click is discarded silently. `allow-same-origin` must
    // stay absent — it would let the document remove its own sandbox.
    const sandbox = wrapper.find('iframe.share-html-iframe').attributes('sandbox') || ''
    expect(sandbox).toContain('allow-scripts')
    expect(sandbox).toContain('allow-popups-to-escape-sandbox')
    expect(sandbox).not.toContain('allow-same-origin')

    // Toggling to the raw source view drops the attribute — CodeMirror keeps
    // the generic wide-screen cap (no self-laying-out page).
    await wrapper.find('.share-view-toggle').trigger('click')
    await flushPromises()
    expect(wrapper.find('.share-content').attributes('data-html-rendered')).toBeUndefined()
  })

  it('does not mark the content scroller as html-rendered for markdown or code', async () => {
    const md = await mountShare({ name: 'README.md', path: '/repo/README.md', content: '# Hello\nbody' })
    expect(md.find('.share-content').attributes('data-html-rendered')).toBeUndefined()
    const code = await mountShare({ name: 'main.go', path: '/repo/main.go', content: 'package main\n' })
    expect(code.find('.share-content').attributes('data-html-rendered')).toBeUndefined()
  })

  it('does not expose the toggle for pure code/plain-text files', async () => {
    const wrapper = await mountShare({ name: 'main.go', path: '/repo/main.go', content: 'package main\n' })
    expect(wrapper.find('.share-view-toggle').exists()).toBe(false)
    // Code files render through the code viewer directly (no preview branch).
    expect(wrapper.find('.cm-viewer-stub').exists()).toBe(true)
  })

  it('wraps the code viewer in a definite-height box so TOC line jumps can scroll', async () => {
    // .share-content is a block overflow:auto scroller. A bare CodeMirrorViewer
    // (flex:1) would size to content height, so CodeMirror's own .cm-scroller
    // never overflows and centeredScrollTop clamps every jump to the top. The
    // fill-viewer box (position:absolute; inset:0) gives the editor a definite
    // height, mirroring the in-app FileViewer's flex chain.
    const wrapper = await mountShare({ name: 'main.go', path: '/repo/main.go', content: 'package main\n' })
    const box = wrapper.find('.share-fill-viewer')
    expect(box.exists()).toBe(true)
    expect(box.find('.cm-viewer-stub').exists()).toBe(true)

    // Same for the source view of a markdown file after toggling to raw.
    const mdWrapper = await mountShare({ name: 'README.md', path: '/repo/README.md', content: '# Hello\nbody' })
    await mdWrapper.find('.share-view-toggle').trigger('click')
    await flushPromises()
    expect(mdWrapper.find('.share-fill-viewer .cm-viewer-stub').exists()).toBe(true)
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

  it('hides the TOC for the rendered HTML preview but restores it in source view', async () => {
    // The rendered HTML preview is a sandboxed <iframe srcdoc>: its inner DOM is
    // unreachable, so a source-derived outline can neither index the rendered
    // headings nor jump to them. The TOC toggle/rail must not be offered there.
    const wrapper = await mountShare({
      name: 'page.html',
      path: '/repo/page.html',
      content: '<h1>Title</h1>\n<h2>Section</h2>',
    })
    expect(wrapper.find('iframe.share-html-iframe').exists()).toBe(true)
    expect(wrapper.find('.share-top-actions .share-btn[title="Toggle table of contents"]').exists()).toBe(false)
    expect(wrapper.find('.share-body .share-toc').exists()).toBe(false)

    // Source view renders through CodeMirror, where entries jump by line — the
    // TOC toggle and rail come back.
    await wrapper.find('.share-view-toggle').trigger('click')
    await flushPromises()
    expect(wrapper.find('.cm-viewer-stub').exists()).toBe(true)
    expect(wrapper.find('.share-top-actions .share-btn[title="Toggle table of contents"]').exists()).toBe(true)
    expect(wrapper.find('.share-body .share-toc').exists()).toBe(true)
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

describe('ShareView — TOC follows the scroll position', () => {
  /** jsdom has no layout, so heading geometry is injected: each heading reports
   *  a viewport top derived from the current scroll offset. */
  function rect(top: number): DOMRect {
    return {
      top, bottom: top + 20, left: 0, right: 0, width: 0, height: 20, x: 0, y: top,
      toJSON: () => ({}),
    } as DOMRect
  }

  function addHeading(content: HTMLElement, id: string, viewportTop: () => number) {
    const el = document.createElement('h2')
    el.id = id
    content.appendChild(el)
    el.getBoundingClientRect = () => rect(viewportTop())
    return el
  }

  /** One animation frame — the scroll handler throttles through rAF. */
  async function nextFrame() {
    await new Promise<void>(resolve => requestAnimationFrame(() => resolve()))
  }

  /** Mount a doc with three sections and wire their geometry to `state`. */
  async function mountWithSections() {
    const wrapper = await mountShare({
      name: 'doc.md',
      path: '/repo/doc.md',
      content: '# Alpha\n\nbody\n\n## Beta\n\nbody\n\n## Gamma\n\nbody',
    })
    const content = wrapper.find('.share-content').element as HTMLElement
    const state = { scrollTop: 0 }
    content.getBoundingClientRect = () => rect(0)
    // jsdom does not implement Element.scrollTo (the TOC jump uses it).
    content.scrollTo = vi.fn()
    // Document tops of the three headings, in page coordinates.
    const tops: Record<string, number> = { alpha: 20, beta: 600, gamma: 1200 }
    for (const id of Object.keys(tops)) addHeading(content, id, () => tops[id] - state.scrollTop)
    return { wrapper, content, state }
  }

  it('highlights the section whose heading last passed the top of the viewport', async () => {
    const { wrapper, content, state } = await mountWithSections()
    const items = wrapper.findAll('.share-toc-item')
    expect(items).toHaveLength(3)
    // Opened at the top → first section is current.
    expect(items[0].classes()).toContain('active')

    state.scrollTop = 700
    content.dispatchEvent(new Event('scroll'))
    await nextFrame()
    expect(items[1].classes()).toContain('active')
    expect(items[0].classes()).not.toContain('active')

    // Back to the very top: the first section wins again.
    state.scrollTop = 0
    content.dispatchEvent(new Event('scroll'))
    await nextFrame()
    expect(items[0].classes()).toContain('active')
  })

  it('keeps the clicked entry highlighted while the jump is still animating', async () => {
    const { wrapper, content, state } = await mountWithSections()
    const items = wrapper.findAll('.share-toc-item')

    await items[2].trigger('click')
    expect(items[2].classes()).toContain('active')

    // The smooth scroll sweeps past the earlier sections; the follow must not
    // steal the highlight back to one of them mid-flight.
    state.scrollTop = 100
    content.dispatchEvent(new Event('scroll'))
    await nextFrame()
    expect(items[2].classes()).toContain('active')
  })

  it('follows the source view through cm-editor-viewport-line', async () => {
    const wrapper = await mountShare({
      name: 'README.md',
      path: '/repo/README.md',
      content: '# Alpha\n\ntext\n\n## Beta\n\nmore',
    })
    const items = wrapper.findAll('.share-toc-item')
    expect(items).toHaveLength(2)
    // Opened at the top → first section is current.
    expect(items[0].classes()).toContain('active')

    // Rendered view has no editor: the event must not move the highlight.
    window.dispatchEvent(new CustomEvent('cm-editor-viewport-line', { detail: { line: 6, path: '/repo/README.md' } }))
    await nextTick()
    expect(items[1].classes()).not.toContain('active')

    await wrapper.find('.share-view-toggle').trigger('click')
    await flushPromises()

    // Source view: highlight the deepest entry at or above the reported line.
    window.dispatchEvent(new CustomEvent('cm-editor-viewport-line', { detail: { line: 6, path: '/repo/README.md' } }))
    await nextTick()
    expect(items[1].classes()).toContain('active')

    window.dispatchEvent(new CustomEvent('cm-editor-viewport-line', { detail: { line: 2, path: '/repo/README.md' } }))
    await nextTick()
    expect(items[0].classes()).toContain('active')
    expect(items[1].classes()).not.toContain('active')
  })

  it('ignores viewport-line events from another file', async () => {
    const wrapper = await mountShare({
      name: 'README.md',
      path: '/repo/README.md',
      content: '# Alpha\n\ntext\n\n## Beta\n\nmore',
    })
    await wrapper.find('.share-view-toggle').trigger('click')
    await flushPromises()
    const items = wrapper.findAll('.share-toc-item')

    window.dispatchEvent(new CustomEvent('cm-editor-viewport-line', { detail: { line: 6, path: '/repo/other.md' } }))
    await nextTick()
    expect(items[1].classes()).not.toContain('active')
    expect(items[0].classes()).toContain('active')
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

    mockDownloadUrlWithProgress.mockClear()
    await wrapper.find('button.share-btn--download').trigger('click')

    expect(mockDownloadUrlWithProgress).toHaveBeenCalledTimes(1)
    const [url] = mockDownloadUrlWithProgress.mock.calls[0]
    expect(url).toContain('/download?path=')
    expect(decodeURIComponent(url)).toContain(LINKED.path)
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
    // A button, not an anchor: the download must route through the progress
    // helper rather than the browser's native download mechanism.
    const btn = unsupported.find('button.share-download-btn')
    expect(btn.exists()).toBe(true)
    expect(unsupported.find('a.share-download-btn').exists()).toBe(false)
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

describe('ShareView — Excalidraw canvas', () => {
  it('renders a .xdraw scene in the canvas viewer, not as raw JSON', async () => {
    const wrapper = await mountShare({
      name: 'flow.xdraw',
      path: '/repo/flow.xdraw',
      content: '{"type":"excalidraw","version":2,"elements":[]}',
      subtype: 'excalidraw',
    })

    const canvas = wrapper.find('.excalidrawviewer-stub')
    expect(canvas.exists()).toBe(true)
    // The share page is anonymous and read-only: the viewer must be mounted in
    // read-only mode (no toolbars, no save button, no write endpoint).
    expect(canvas.attributes('data-readonly')).toBe('true')
    expect(wrapper.find('.cm-viewer-stub').exists()).toBe(false)
    expect(wrapper.find('.share-unsupported').exists()).toBe(false)
    // A diagram has no heading outline, and no source view to toggle to.
    expect(wrapper.find('.share-view-toggle').exists()).toBe(false)
    expect(wrapper.find('.share-body .share-toc').exists()).toBe(false)
  })

  it('treats .excalidraw exactly like .xdraw', async () => {
    const wrapper = await mountShare({
      name: 'flow.excalidraw',
      path: '/repo/flow.excalidraw',
      content: '{"type":"excalidraw","version":2,"elements":[]}',
      subtype: 'excalidraw',
    })
    expect(wrapper.find('.excalidrawviewer-stub').exists()).toBe(true)
  })
})

/**
 * Preview parity guard: every file kind the in-app file viewer can render must
 * also render on the public share page.
 *
 * Two halves, because either one alone can rot:
 *   1. Behaviour — mount the share view with one fixture per kind and require a
 *      real viewer, never the "download only" fallback card.
 *   2. Source — read FileViewer.vue's preview branches and require each kind to
 *      have a branch in ShareView.vue. This is what catches the NEXT type: add
 *      `file.isSvg` to the app viewer and this fails until share handles it.
 */
const PREVIEW_FLAGS: Record<string, string> = {
  isPdf: 'pdf',
  isImage: 'image',
  isAudio: 'audio',
  isVideo: 'video',
  isOffice: 'office',
  isExcalidraw: 'excalidraw',
}

/** Branches that are fallbacks rather than previews (download cards). */
const NON_PREVIEW_FLAGS = new Set(['isBinary'])

const COMPUTED_KINDS: Array<{ kind: string; match: RegExp }> = [
  { kind: 'markdown', match: /\bisMarkdown\b/ },
  { kind: 'html', match: /\bisHtml\b/ },
  { kind: 'openapi', match: /\bisOpenapi\b/ },
  // The app calls its table branch `isTableRenderedView`; the share page uses
  // `isTable` (rendered-vs-source is a single toggle there).
  { kind: 'table', match: /\bisTable(RenderedView)?\b/ },
]

const PREVIEW_FIXTURES: Array<{
  kind: string
  file: Record<string, unknown>
  selector: string
}> = [
  { kind: 'markdown', file: { name: 'a.md', path: '/repo/a.md', content: '# hi' }, selector: '.markdown-preview-stub' },
  { kind: 'table', file: { name: 'a.csv', path: '/repo/a.csv', content: 'a,b\n1,2' }, selector: '.table-preview-stub' },
  { kind: 'pdf', file: { name: 'a.pdf', path: '/repo/a.pdf', content: '' }, selector: '.pdfpreview-stub' },
  { kind: 'image', file: { name: 'a.png', path: '/repo/a.png', content: '' }, selector: '.share-image-view' },
  { kind: 'audio', file: { name: 'a.mp3', path: '/repo/a.mp3', content: '' }, selector: '.audiopreview-stub' },
  { kind: 'video', file: { name: 'a.mp4', path: '/repo/a.mp4', content: '' }, selector: '.videopreview-stub' },
  { kind: 'office', file: { name: 'a.docx', path: '/repo/a.docx', content: '' }, selector: '.officepreview-stub' },
  { kind: 'openapi', file: { name: 'api.yaml', path: '/repo/api.yaml', content: 'openapi: 3.0.0', subtype: 'openapi' }, selector: '.openapipreview-stub' },
  { kind: 'html', file: { name: 'a.html', path: '/repo/a.html', content: '<h1>x</h1>' }, selector: '.share-html-iframe' },
  { kind: 'excalidraw', file: { name: 'flow.xdraw', path: '/repo/flow.xdraw', content: '{"type":"excalidraw","elements":[]}', subtype: 'excalidraw' }, selector: '.excalidrawviewer-stub' },
  { kind: 'code', file: { name: 'a.go', path: '/repo/a.go', content: 'package main' }, selector: '.cm-viewer-stub' },
]

const REPO_ROOT = join(__dirname, '..', '..', '..', '..')

/** Every v-if / v-else-if condition in a component source. */
function branchConditions(source: string): string[] {
  const out: string[] = []
  const re = /v-(?:else-)?if="([^"]*)"/g
  let m: RegExpExecArray | null
  while ((m = re.exec(source))) out.push(m[1])
  return out
}

/** Preview kinds a component branches on (fallbacks excluded). */
function previewKindsIn(source: string): Set<string> {
  const kinds = new Set<string>()
  for (const cond of branchConditions(source)) {
    for (const [flag, kind] of Object.entries(PREVIEW_FLAGS)) {
      // `file.isPdf` in the app viewer, `isExcalidraw` (a computed) in the
      // share view — both are the same preview kind.
      if (new RegExp(`\\b${flag}\\b`).test(cond)) kinds.add(kind)
    }
    for (const { kind, match } of COMPUTED_KINDS) {
      if (match.test(cond)) kinds.add(kind)
    }
  }
  return kinds
}

/** `file.isXxx` flags a component branches on, minus known fallbacks. */
function fileFlagsIn(source: string): Set<string> {
  const flags = new Set<string>()
  for (const cond of branchConditions(source)) {
    const re = /file\.(is[A-Z]\w*)/g
    let m: RegExpExecArray | null
    while ((m = re.exec(cond))) {
      if (!NON_PREVIEW_FLAGS.has(m[1])) flags.add(m[1])
    }
  }
  return flags
}

describe('ShareView — preview parity with the in-app file viewer', () => {
  it('covers every preview kind with a fixture (keeps the tables honest)', () => {
    const expected = new Set([
      ...Object.values(PREVIEW_FLAGS),
      ...COMPUTED_KINDS.map((k) => k.kind),
      'code',
    ])
    expect(new Set(PREVIEW_FIXTURES.map((f) => f.kind))).toEqual(expected)
  })

  it('renders a viewer — not the download card — for every preview kind', async () => {
    for (const fixture of PREVIEW_FIXTURES) {
      const wrapper = await mountShare(fixture.file)
      expect(wrapper.find(fixture.selector).exists(), `${fixture.kind} must render on the share page`).toBe(true)
      expect(wrapper.find('.share-unsupported').exists(), `${fixture.kind} must not fall back to download`).toBe(false)
      wrapper.unmount()
      const idx = mountedWrappers.findIndex((w) => w === wrapper)
      if (idx >= 0) mountedWrappers.splice(idx, 1)
    }
  })

  it('has a share branch for every preview branch in FileViewer.vue', () => {
    const appSource = readFileSync(join(REPO_ROOT, 'web/src/components/file/FileViewer.vue'), 'utf8')
    const shareSource = readFileSync(join(REPO_ROOT, 'web/src/share/ShareView.vue'), 'utf8')

    const shareKinds = previewKindsIn(shareSource)
    for (const kind of previewKindsIn(appSource)) {
      expect(shareKinds.has(kind), `ShareView.vue must render "${kind}" (FileViewer.vue previews it)`).toBe(true)
    }
  })

  it('fails loudly when the app viewer gains an unregistered preview flag', () => {
    // The registry above is the contract: a new `file.isXxx` branch in
    // FileViewer.vue must be added here AND given a share branch, otherwise it
    // silently degrades to a download card for every shared link.
    const appSource = readFileSync(join(REPO_ROOT, 'web/src/components/file/FileViewer.vue'), 'utf8')
    for (const flag of fileFlagsIn(appSource)) {
      expect(Object.keys(PREVIEW_FLAGS), `register file.${flag} in PREVIEW_FLAGS and support it in ShareView.vue`).toContain(flag)
    }
  })
})
