import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import MarkdownPreviewBody from '@/components/file/MarkdownPreviewBody.vue'
import { ATTACH_DRAG_MIME } from '@/utils/attachDrag'
import { useChatContext } from '@/composables/useChatContext'

const { attachedFiles, clearAll, addAttachedFile } = useChatContext()

/** Seed an attached file so the toggle-off path can be exercised. */
function addTestAttachment(path: string) {
  addAttachedFile(path)
}

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      file: {
        codePreview: {
          title: 'Code Preview',
          loading: 'Loading...',
          retry: 'Retry',
          linesRemaining: '{n} remaining',
          loadingMoreLines: 'Loading more...',
        },
      },
    },
  },
})

function mountBody(overrides: Record<string, unknown> = {}) {
  const loadMoreAbove = vi.fn()
  const loadMoreBelow = vi.fn()
  const wrapper = mount(MarkdownPreviewBody, {
    global: { plugins: [i18n] },
    props: {
      status: 'ready',
      errorMessageText: '',
      errorCode: null,
      renderedHtml: '<h1>Hello</h1>\n<p>World</p>',
      filePath: 'docs/guide.md',
      remainingAbove: 0,
      remainingBelow: 0,
      loadingDirection: null,
      loadMoreAbove,
      loadMoreBelow,
      ...overrides,
    },
  })
  return { wrapper, loadMoreAbove, loadMoreBelow }
}

/** Give the scroll container real geometry so the threshold math is testable. */
function setGeometry(
  el: Element,
  geo: { scrollTop: number; scrollHeight: number; clientHeight: number }
) {
  for (const [key, value] of Object.entries(geo)) {
    Object.defineProperty(el, key, { configurable: true, writable: true, value })
  }
}

describe('MarkdownPreviewBody.vue', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  // The attach actions operate on the module-level useChatContext singleton;
  // reset it after every test (even failing ones) so state never leaks between
  // tests in this file or into sibling files sharing the same worker.
  afterEach(() => {
    clearAll()
  })

  it('renders the markdown HTML inside .markdown-body > .markdown-content', () => {
    const { wrapper } = mountBody()
    expect(wrapper.find('.md-preview-scroll').exists()).toBe(true)
    const body = wrapper.find('.markdown-body.md-preview-body')
    expect(body.exists()).toBe(true)
    expect(body.attributes('data-file-path')).toBe('docs/guide.md')
    const content = body.find('.markdown-content')
    expect(content.exists()).toBe(true)
    expect(content.find('h1').text()).toBe('Hello')
  })

  it('shows loading status with aria-live', () => {
    const { wrapper } = mountBody({ status: 'loading' })
    expect(wrapper.get('[aria-live="polite"]').text()).toContain('Loading')
    expect(wrapper.find('.markdown-content').exists()).toBe(false)
  })

  it('shows error status and emits refresh only for network errors', async () => {
    const { wrapper } = mountBody({ status: 'error', errorCode: 'network', errorMessageText: 'boom' })
    expect(wrapper.find('[role="status"]').text()).toContain('boom')
    await wrapper.find('button').trigger('click')
    expect(wrapper.emitted('refresh')).toHaveLength(1)

    const noRetry = mountBody({ status: 'error', errorCode: 'not-found', errorMessageText: 'missing' })
    expect(noRetry.wrapper.find('button').exists()).toBe(false)
  })

  it('shows a remaining-lines hint and no expand buttons', () => {
    const { wrapper } = mountBody({ remainingAbove: 9, remainingBelow: 20 })

    // The bars exist purely as status lines: loading is driven by scrolling.
    const above = wrapper.find('.expand-above')
    const below = wrapper.find('.expand-below')
    expect(above.exists()).toBe(true)
    expect(below.exists()).toBe(true)
    expect(above.get('.code-preview-expand-hint').text()).toContain('9')
    expect(below.get('.code-preview-expand-hint').text()).toContain('20')

    // Nothing to click any more — the buttons were removed with the line cap.
    expect(wrapper.find('.code-preview-expand-btn').exists()).toBe(false)

    const none = mountBody()
    expect(none.wrapper.find('.expand-above').exists()).toBe(false)
    expect(none.wrapper.find('.expand-below').exists()).toBe(false)
  })

  it('shows a spinner on the side currently loading', () => {
    const below = mountBody({ remainingBelow: 20, loadingDirection: 'below' })
    expect(below.wrapper.find('.expand-below .code-preview-expand-spinner').exists()).toBe(true)

    const above = mountBody({ remainingAbove: 20, loadingDirection: 'above' })
    expect(above.wrapper.find('.expand-above .code-preview-expand-spinner').exists()).toBe(true)
  })

  it('loads more when scrolled to the bottom', async () => {
    const { wrapper, loadMoreBelow } = mountBody({ remainingBelow: 100 })
    const el = wrapper.find('.md-preview-scroll').element
    setGeometry(el, { scrollTop: 800, scrollHeight: 1200, clientHeight: 400 })

    await wrapper.find('.md-preview-scroll').trigger('scroll')
    expect(loadMoreBelow).toHaveBeenCalledTimes(1)
  })

  it('loads above when scrolled to the top', async () => {
    const { wrapper, loadMoreAbove, loadMoreBelow } = mountBody({ remainingAbove: 50, remainingBelow: 50 })
    const el = wrapper.find('.md-preview-scroll').element
    setGeometry(el, { scrollTop: 0, scrollHeight: 4000, clientHeight: 400 })

    await wrapper.find('.md-preview-scroll').trigger('scroll')
    expect(loadMoreAbove).toHaveBeenCalledTimes(1)
    expect(loadMoreBelow).not.toHaveBeenCalled()
  })

  it('stops loading when loadMoreBlocked is set', async () => {
    const { wrapper, loadMoreBelow } = mountBody({ remainingBelow: 100, loadMoreBlocked: true })
    const el = wrapper.find('.md-preview-scroll').element
    setGeometry(el, { scrollTop: 800, scrollHeight: 1200, clientHeight: 400 })

    await wrapper.find('.md-preview-scroll').trigger('scroll')
    expect(loadMoreBelow).not.toHaveBeenCalled()
    expect(wrapper.find('.expand-below .code-preview-expand-hint').text()).toContain('100')
  })

  it('auto-fills the viewport when the document is too short to scroll', async () => {
    const { wrapper, loadMoreBelow } = mountBody({ remainingBelow: 100 })
    const el = wrapper.find('.md-preview-scroll').element
    setGeometry(el, { scrollTop: 0, scrollHeight: 80, clientHeight: 400 })

    await vi.waitFor(() => {
      expect(loadMoreBelow).toHaveBeenCalled()
    })
  })

  it('exposes a scroll container and no-op navigation helpers', () => {
    const { wrapper } = mountBody()
    const exposed = wrapper.vm as unknown as {
      scrollContainer: HTMLElement | null
      scrollToTargetLine: () => void
      scrollLineIntoView: (i: number) => void
    }
    expect(exposed.scrollContainer?.classList.contains('md-preview-scroll')).toBe(true)
    // No-op helpers must not throw (the parent's bodyRef surface expects them).
    expect(() => exposed.scrollToTargetLine()).not.toThrow()
    expect(() => exposed.scrollLineIntoView(3)).not.toThrow()
  })

  it('writes the attach payload when dragging a local markdown image out', async () => {
    const { wrapper } = mountBody({
      renderedHtml: '<p><span class="lightbox-img-wrap"><img class="lightbox-img" src="/api/fs/raw/docs/a.png?t=1" data-attach-src="docs/a.png"></span></p>',
    })

    // A custom MIME payload is set on dragstart.
    const store: Record<string, string> = {}
    const types: string[] = []
    const dataTransfer = {
      effectAllowed: '',
      setData(type: string, value: string) {
        store[type] = value
        if (!types.includes(type)) types.push(type)
      },
      setDragImage: vi.fn(),
      get types() {
        return Object.freeze([...types])
      },
    }
    const img = wrapper.find('img.lightbox-img')
    expect(img.exists()).toBe(true)
    // bubble (default) so the container's delegated handler runs
    await img.trigger('dragstart', { dataTransfer })
    expect(dataTransfer.setDragImage).toHaveBeenCalledTimes(1)
    expect(store[ATTACH_DRAG_MIME]).toBe('{"path":"docs/a.png","isDir":false}')
    expect(store['text/plain']).toBe('docs/a.png')
  })

  it('does not write the attach payload for an external image', async () => {
    const { wrapper } = mountBody({
      renderedHtml: '<p><img class="lightbox-img" src="https://x.com/a.png"></p>',
    })
    const dataTransfer = { setData: vi.fn(), setDragImage: vi.fn() }
    await wrapper.find('img.lightbox-img').trigger('dragstart', { dataTransfer })
    expect(dataTransfer.setData).not.toHaveBeenCalled()
  })

  it('attaches a local image when its header attach button is tapped', async () => {
    const { wrapper } = mountBody({
      renderedHtml: '<div class="image-block-wrapper"><span class="lightbox-img-wrap"><img class="lightbox-img" src="/api/fs/raw/docs/a.png?t=1" data-attach-src="docs/a.png"></span><button class="image-block-attach-btn" type="button"></button></div>',
    })
    await wrapper.find('.image-block-attach-btn').trigger('click')
    expect(attachedFiles.value).toEqual([{ path: 'docs/a.png', isDir: false }])
  })

  it('removes a local image from attachments when its header attach button is tapped again', async () => {
    addTestAttachment('docs/a.png')
    const { wrapper } = mountBody({
      renderedHtml: '<div class="image-block-wrapper"><span class="lightbox-img-wrap"><img class="lightbox-img" src="/api/fs/raw/docs/a.png?t=1" data-attach-src="docs/a.png"></span><button class="image-block-attach-btn" type="button"></button></div>',
    })
    await wrapper.find('.image-block-attach-btn').trigger('click')
    expect(attachedFiles.value).toEqual([])
  })

  it('does not attach when tapping the image body (not the button)', async () => {
    const { wrapper } = mountBody({
      renderedHtml: '<div class="image-block-wrapper"><span class="lightbox-img-wrap"><img class="lightbox-img" src="/api/fs/raw/docs/a.png?t=1" data-attach-src="docs/a.png"></span><button class="image-block-attach-btn" type="button"></button></div>',
    })
    await wrapper.find('img.lightbox-img').trigger('click')
    expect(attachedFiles.value).toEqual([])
  })

  it('open-file button click does not attach (it routes to file navigation)', async () => {
    const { wrapper } = mountBody({
      renderedHtml: '<div class="image-block-wrapper"><span class="lightbox-img-wrap"><img class="lightbox-img" src="/api/fs/raw/docs/a.png?t=1" data-attach-src="docs/a.png"></span><button class="image-block-open-btn" type="button"></button></div>',
    })
    await wrapper.find('.image-block-open-btn').trigger('click')
    // The open-file handler consumes the click; it must NOT toggle an attachment.
    expect(attachedFiles.value).toEqual([])
  })

  it('attaches a markdown line range when a mermaid attach badge is tapped', async () => {
    addTestAttachment('docs/guide.md')
    const { wrapper } = mountBody({
      renderedHtml: '<div class="markdown-content"><div class="mermaid" data-source-line="5" data-mermaid="graph TD; A-->B"><svg></svg><span class="mermaid-attach-badge"><svg></svg></span></div></div>',
    })
    // The component template renders its own .markdown-body.md-preview-body
    // (data-file-path from the filePath prop) around .markdown-content.
    const body = wrapper.find('.markdown-body.md-preview-body')
    expect(body.exists()).toBe(true)
    expect(body.attributes('data-file-path')).toBe('docs/guide.md')
    const badge = body.find('.mermaid-attach-badge')
    expect(badge.exists()).toBe(true)
    await badge.trigger('click')
    // docs/guide.md was already attached whole → adding a range keeps both.
    const entry = attachedFiles.value.find(f => f.startLine === 5)
    expect(entry).toEqual({ path: 'docs/guide.md', isDir: false, startLine: 5, endLine: 7 })
    // Tapping again removes only that range.
    await badge.trigger('click')
    expect(attachedFiles.value.some(f => f.startLine === 5)).toBe(false)
    expect(attachedFiles.value.some(f => f.path === 'docs/guide.md' && f.startLine === undefined)).toBe(true)
  })

  it('attaches the authoritative range when the rendered container carries data-source-end', async () => {
    addTestAttachment('docs/guide.md')
    const { wrapper } = mountBody({
      // Real render pipeline output: the mermaid div carries the closing-fence
      // line (9). The body's trailing blank line makes a body-derived end (7)
      // too short, so the stamped attr must win.
      renderedHtml: '<div class="markdown-content"><div class="mermaid" data-source-line="5" data-source-end="9" data-mermaid="graph TD; A-->B\n"><svg></svg><span class="mermaid-attach-badge"><svg></svg></span></div></div>',
    })
    const badge = wrapper.find('.mermaid-attach-badge')
    expect(badge.exists()).toBe(true)
    await badge.trigger('click')
    const entry = attachedFiles.value.find(f => f.startLine === 5)
    expect(entry).toEqual({ path: 'docs/guide.md', isDir: false, startLine: 5, endLine: 9 })
  })

  it('attaches a code block md line range when its header attach button is tapped', async () => {
    const { wrapper } = mountBody({
      renderedHtml: '<div class="markdown-content"><div class="code-block-wrapper"><div class="code-block-header"><span class="code-block-header-actions"><button class="code-block-attach-btn" data-action="attach"></button></span></div><pre data-source-line="9" data-source-end="12"><code>const a=1</code></pre></div></div>',
    })
    const btn = wrapper.find('.code-block-attach-btn')
    expect(btn.exists()).toBe(true)
    await btn.trigger('click')
    const entry = attachedFiles.value.find(f => f.startLine === 9)
    expect(entry).toEqual({ path: 'docs/guide.md', isDir: false, startLine: 9, endLine: 12 })
    // Tapping again removes only that range.
    await btn.trigger('click')
    expect(attachedFiles.value.some(f => f.startLine === 9)).toBe(false)
  })

  it('attaches a table md line range when its header attach button is tapped', async () => {
    const { wrapper } = mountBody({
      renderedHtml: '<div class="markdown-content"><div class="table-block-wrapper"><div class="table-block-header"><span class="table-block-header-actions"><button class="table-block-attach-btn" data-action="attach"></button></span></div><div class="table-wrap"><table data-source-line="15" data-source-end="18"><tr><td>1</td></tr></table></div></div></div>',
    })
    const btn = wrapper.find('.table-block-attach-btn')
    expect(btn.exists()).toBe(true)
    await btn.trigger('click')
    const entry = attachedFiles.value.find(f => f.startLine === 15)
    expect(entry).toEqual({ path: 'docs/guide.md', isDir: false, startLine: 15, endLine: 18 })
    await btn.trigger('click')
    expect(attachedFiles.value.some(f => f.startLine === 15)).toBe(false)
  })

  // ── SVG media: proportional fill-width sizing on image load ──────────────
  //
  // An SVG FILE's aspect ratio is only knowable once the browser has decoded
  // it, so the figure is (re-)stamped on the media `load` event. Inline <svg>
  // needs no load and is stamped by the render pass instead, which means a
  // regression that drops this `@load.capture` wiring would silently leave
  // every SVG *file* unsized while the render-pass tests stayed green.
  describe('svg file sizing on media load', () => {
    const SVG_FIGURE =
      '<div class="image-block-wrapper"><span class="lightbox-img-wrap">'
      + '<img class="lightbox-img" src="/api/fs/raw/docs/chart.svg?t=1"></span></div>'

    /** jsdom never loads images, so the decoded intrinsic size must be injected. */
    function injectNaturalSize(img: Element, w: number, h: number): void {
      Object.defineProperty(img, 'naturalWidth', { value: w, configurable: true })
      Object.defineProperty(img, 'naturalHeight', { value: h, configurable: true })
    }

    it('stamps an SVG file figure with its ratio once the image has loaded', async () => {
      const { wrapper } = mountBody({ renderedHtml: SVG_FIGURE })
      const figure = wrapper.find('.image-block-wrapper')
      const img = wrapper.find('img.lightbox-img')

      // Not yet decoded → no ratio, so the figure keeps its previous sizing.
      expect(figure.classes()).not.toContain('svg-fit')

      injectNaturalSize(img.element, 400, 100)
      await img.trigger('load')

      expect(figure.classes()).toContain('svg-fit')
      expect((figure.element as HTMLElement).style.getPropertyValue('--svg-ar')).toBe('4')
    })

    it('re-stamps with the corrected ratio when a later load reveals a new size', async () => {
      const { wrapper } = mountBody({ renderedHtml: SVG_FIGURE })
      const img = wrapper.find('img.lightbox-img')
      const figure = wrapper.find('.image-block-wrapper')

      injectNaturalSize(img.element, 400, 100)
      await img.trigger('load')
      expect((figure.element as HTMLElement).style.getPropertyValue('--svg-ar')).toBe('4')

      // A responsive SVG reports a different intrinsic size after a resize /
      // reload; the stamp must overwrite rather than keep the stale ratio.
      injectNaturalSize(img.element, 100, 400)
      await img.trigger('load')
      expect((figure.element as HTMLElement).style.getPropertyValue('--svg-ar')).toBe('0.25')
      expect(figure.classes()).toContain('svg-fit')
    })

    it('leaves a raster image figure unsized on load', async () => {
      const { wrapper } = mountBody({
        renderedHtml: '<div class="image-block-wrapper"><span class="lightbox-img-wrap">'
          + '<img class="lightbox-img" src="/api/fs/raw/docs/photo.png?t=1"></span></div>',
      })
      const img = wrapper.find('img.lightbox-img')
      injectNaturalSize(img.element, 400, 100)
      await img.trigger('load')

      // Only SVG media opts in; a PNG must not be stretched to the column.
      expect(wrapper.find('.image-block-wrapper').classes()).not.toContain('svg-fit')
    })
  })
})
