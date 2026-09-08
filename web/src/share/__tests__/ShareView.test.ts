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
vi.mock('@/components/media/ImagePreview.vue', () => ({ default: baseStub('ImagePreview') }))
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
        renderedView: 'Rendered preview',
        sourceView: 'View source',
      },
      common: { download: 'Download' },
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

async function mountShare(file: Record<string, unknown>) {
  ;(globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValue({
    ok: true,
    json: async () => file,
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
