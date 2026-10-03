import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { computed, nextTick, ref } from 'vue'
import { createI18n } from 'vue-i18n'

// The component tree now renders FileChangeNav, which resolves its labels via
// vue-i18n — the app always installs it, so the test must too (matches the
// sibling MarkdownPreview tests).
const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: {} } })

const { openFilePath, closePreview, handleDblClick, pipelineHtml } = vi.hoisted(() => ({
  openFilePath: vi.fn(),
  closePreview: vi.fn(),
  handleDblClick: vi.fn(),
  pipelineHtml: { value: '' },
}))

vi.mock('@/composables/useMarkdownRenderer.ts', () => ({
  renderMermaidInElement: vi.fn().mockResolvedValue(undefined),
}))

vi.mock('@/composables/usePlatformDetect.ts', () => ({
  useWideScreenLayout: () => ({ isWideScreen: ref(true) }),
}))

vi.mock('@/composables/useDoubleClickCopy.ts', () => ({
  useDoubleClickCopy: () => ({ handleDblClick }),
}))

vi.mock('@/composables/useQuoteQuestion.ts', () => ({
  useQuoteQuestion: () => ({ showBar: vi.fn() }),
}))

vi.mock('@/composables/useFilePathAnnotation.ts', () => ({
  useFilePathAnnotation: () => ({
    verifyFilePaths: vi.fn(),
    resolveRelativePath: vi.fn((href) => href),
    openFilePath,
    parseFileUri: vi.fn(),
    readLineTargetFromEl: (el: Element) => {
      const startAttr = el.getAttribute('data-line-start')
      const endAttr = el.getAttribute('data-line-end')
      return {
        filePath: el.getAttribute('data-file-path'),
        lineStart: startAttr ? parseInt(startAttr, 10) : undefined,
        lineEnd: endAttr ? parseInt(endAttr, 10) : undefined,
        lineRanges: el.getAttribute('data-line-ranges') || undefined,
      }
    },
  }),
}))

vi.mock('@/composables/useCodeBlockHeader.ts', () => ({
  handleCodeBlockClick: vi.fn().mockReturnValue(false),
  handleTableBlockClick: vi.fn().mockReturnValue(false),
}))

vi.mock('@/stores/app.ts', () => ({
  store: { state: { projectRoot: '/project', homeDir: '/home/user' } },
}))

vi.mock('@/composables/useMarkdownRenderPipeline.ts', () => ({
  buildMarkdownPreviewDom: () => ({
    html: pipelineHtml.value || '<p><span class="chat-file-path" data-file-path="docs/guides" data-path-type="dir">docs/guides</span><span class="chat-file-path file-target" data-file-path="docs/guide.md" data-path-type="file">docs/guide.md</span></p>',
    detectedPaths: [],
  }),
}))

vi.mock('@/composables/useTableRowExpand.ts', () => ({
  useTableRowExpand: () => ({
    tableRowModal: ref(null),
    closeTableRowModal: vi.fn(),
    tableRowPrev: vi.fn(),
    tableRowNext: vi.fn(),
    handleTableRowClick: vi.fn().mockReturnValue(false),
    onTableMouseDown: vi.fn(),
    onTableTouchStart: vi.fn(),
  }),
}))

vi.mock('@/composables/useMarkdownDiff.ts', () => ({
  diffMarkers: ref([]),
  clearDiffMarkers: vi.fn(),
  extractBlocks: vi.fn().mockReturnValue([]),
  extractBlockElements: vi.fn().mockReturnValue([]),
}))

// Change-navigation wiring: keep the real modules but make the two calls the
// nav depends on observable (restore-on-switch + clear-button baseline drop).
vi.mock('@/composables/useFileRefresh.ts', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/composables/useFileRefresh.ts')>()
  return { ...actual, syncMarkersFor: vi.fn() }
})

vi.mock('@/composables/useFileChangeBaseline.ts', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/composables/useFileChangeBaseline.ts')>()
  return { ...actual, clearBaseline: vi.fn() }
})

vi.mock('@/composables/useDiffMarkerClick.ts', () => ({
  handleDiffMarkerClick: vi.fn().mockReturnValue(false),
}))

vi.mock('@/composables/useCodeLinkPreview.ts', () => ({
  useCodeLinkPreview: () => ({
    enabled: computed(() => true),
    isTouchDevice: () => false,
    handleClick: vi.fn(),
    close: closePreview,
  }),
  handleVerifiedFilePathClick: vi.fn().mockReturnValue(false),
}))

// Keep the real share-link behavior but make the interceptor observable, so a
// regression that drops the call from MarkdownPreview.handleClick is caught.
vi.mock('@/share/shareLinks', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/share/shareLinks')>()
  return { ...actual, handleShareLinkClick: vi.fn(actual.handleShareLinkClick) }
})

import MarkdownPreview from '../MarkdownPreview.vue'
import { SHARE_OPEN_FILE_EVENT, SHARE_PATH_ATTR } from '@/share/shareLinks'
import { diffMarkers, extractBlockElements } from '@/composables/useMarkdownDiff.ts'
import { syncMarkersFor } from '@/composables/useFileRefresh.ts'
import { clearBaseline } from '@/composables/useFileChangeBaseline.ts'

describe('MarkdownPreview path clicks', () => {
  beforeEach(() => {
    openFilePath.mockReset()
    closePreview.mockReset()
    handleDblClick.mockReset()
    pipelineHtml.value = ''
  })

  it('opens a verified directory when its path text is clicked', async () => {
    const wrapper = mount(MarkdownPreview, {
      props: {
        file: { path: '/project/README.md', content: '[docs](docs/guides)' },
        viewMode: 'rendered',
      },
      global: {
        plugins: [i18n],
        stubs: {
          TableRowModal: true,
          MarkdownSearchBar: true,
          CodeLinkPreview: true,
        },
      },
    })

    const path = wrapper.find('.chat-file-path[data-path-type="dir"]')
    expect(path.exists()).toBe(true)
    await path.trigger('click')

    expect(closePreview).toHaveBeenCalledOnce()
    expect(openFilePath).toHaveBeenCalledWith('docs/guides', undefined, undefined, 'file')
  })

  it('does not directly open file when clicking on a file path text', async () => {
    const wrapper = mount(MarkdownPreview, {
      props: {
        file: { path: '/project/README.md', content: '[doc](docs/guide.md)' },
        viewMode: 'rendered',
      },
      global: {
        plugins: [i18n],
        stubs: {
          TableRowModal: true,
          MarkdownSearchBar: true,
          CodeLinkPreview: true,
        },
      },
    })

    const filePath = wrapper.find('.chat-file-path.file-target')
    expect(filePath.exists()).toBe(true)
    await filePath.trigger('click')

    // Since handleVerifiedFilePathClick was mocked to false and dblClick was not triggered,
    // openFilePath should NOT be called directly by single clicking the file path text
    expect(openFilePath).not.toHaveBeenCalled()
  })

  it('opens the file and stops the click from reaching ancestor handlers', async () => {
    // Mirror production: useDoubleClickCopy resolves an anchor click
    // *synchronously* inside the click dispatch — that dispatch is the only
    // moment stopPropagation can still have an effect.
    handleDblClick.mockImplementation((_event: any, onOpenFile: any) => {
      onOpenFile('docs/guide.md', 5, 10)
    })

    const host = document.createElement('div')
    document.body.appendChild(host)
    const onAncestorClick = vi.fn()
    host.addEventListener('click', onAncestorClick)

    const wrapper = mount(MarkdownPreview, {
      attachTo: host,
      props: {
        file: { path: '/project/README.md', content: '[doc](docs/guide.md)' },
        viewMode: 'rendered',
      },
      global: {
        plugins: [i18n],
        stubs: {
          TableRowModal: true,
          MarkdownSearchBar: true,
          CodeLinkPreview: true,
        },
      },
    })

    try {
      const filePath = wrapper.find('.chat-file-path.file-target')
      expect(filePath.exists()).toBe(true)
      await filePath.trigger('click')

      expect(closePreview).toHaveBeenCalled()
      expect(openFilePath).toHaveBeenCalledWith('docs/guide.md', 5, 10, 'file')
      // The shell registers its own click handlers above the preview; without
      // stopPropagation one tap would both open the file and trigger them.
      expect(onAncestorClick).not.toHaveBeenCalled()
    } finally {
      wrapper.unmount()
      host.remove()
    }
  })

  it('intercepts a share link click before the auth-bound openFilePath fallback', async () => {
    // Share mode annotates relative links with data-share-path. A plain click
    // must dispatch the share-open-file event instead of falling through to
    // openFilePath (which resolves against an empty project root and calls
    // auth-protected endpoints an anonymous reader cannot use).
    pipelineHtml.value =
      `<p><a ${SHARE_PATH_ATTR}="/repo/docs/guide.md" href="/share/tok1?path=x">Guide</a></p>`

    const wrapper = mount(MarkdownPreview, {
      props: {
        file: { path: '/repo/README.md', content: '[Guide](./docs/guide.md)' },
        viewMode: 'rendered',
      },
      global: {
        plugins: [i18n],
        stubs: { TableRowModal: true, MarkdownSearchBar: true, CodeLinkPreview: true },
      },
    })

    const opened: string[] = []
    const onOpen = (e: Event) => opened.push((e as CustomEvent).detail.path)
    window.addEventListener(SHARE_OPEN_FILE_EVENT, onOpen)
    try {
      await wrapper.find(`a[${SHARE_PATH_ATTR}]`).trigger('click')
    } finally {
      window.removeEventListener(SHARE_OPEN_FILE_EVENT, onOpen)
    }

    expect(opened).toEqual(['/repo/docs/guide.md'])
    expect(openFilePath).not.toHaveBeenCalled()
  })
})

// ─────────────────────────────────────────────────────────────────────────────
// Change-navigation wiring. The primitive (useChangeNav) and the pill
// (FileChangeNav) have their own tests; these pin the component ↔ composable
// wiring added at HEAD a9be58a1c so a regression in the glue is caught.
// ─────────────────────────────────────────────────────────────────────────────
describe('MarkdownPreview — change navigation wiring', () => {
  const scrollToMock = vi.fn()

  // Positioned markers are derived from live DOM via extractBlockElements; a
  // controllable stand-in lets us publish markers with chosen tops.
  function blockEl(top: number) {
    const el = document.createElement('div')
    Object.defineProperty(el, 'offsetTop', { configurable: true, value: top })
    Object.defineProperty(el, 'offsetHeight', { configurable: true, value: 10 })
    return el
  }

  const markdownMarker = (id: string) => ({
    id, type: 'modified', label: 'M', blockSelector: '', charDiff: null, ariaLabel: `m${id}`,
  })

  function mountPreview(props: Record<string, unknown> = {}) {
    return mount(MarkdownPreview, {
      props: { file: { path: '/project/README.md', content: '# Title' }, viewMode: 'rendered', ...props },
      global: {
        plugins: [i18n],
        stubs: { TableRowModal: true, MarkdownSearchBar: true, CodeLinkPreview: true },
      },
      // Attach so the rail elements are `isConnected` — flashElement() no-ops on
      // detached elements (it assumes they are about to be removed).
      attachTo: document.body,
    })
  }

  beforeEach(() => {
    openFilePath.mockReset()
    closePreview.mockReset()
    handleDblClick.mockReset()
    pipelineHtml.value = ''
    syncMarkersFor.mockClear()
    clearBaseline.mockClear()
    diffMarkers.value = []
    ;(extractBlockElements as any).mockReturnValue([])
    scrollToMock.mockClear()
    // jsdom does not implement scrollTo on elements; the nav's callback calls it.
    Object.defineProperty(HTMLElement.prototype, 'scrollTo', {
      configurable: true, writable: true, value: scrollToMock,
    })
  })

  it('re-derives markers via syncMarkersFor(path, "markdown", content) on mount and on file switch', async () => {
    const wrapper = mountPreview({ file: { path: '/project/a.md', content: '# A' } })
    await nextTick()
    expect(syncMarkersFor).toHaveBeenCalledWith('/project/a.md', 'markdown', '# A')

    syncMarkersFor.mockClear()
    await wrapper.setProps({ file: { path: '/project/b.md', content: '# B' } })
    await nextTick()
    expect(syncMarkersFor).toHaveBeenCalledWith('/project/b.md', 'markdown', '# B')
  })

  it('re-derives markers for an empty-but-loaded file (content === "")', async () => {
    // Regression (WARN-601): `content === ''` is a valid empty file, not "not
    // loaded". The old truthy guard silently skipped it, so a file edited down
    // to nothing lost its "all deleted" marker on switch-away-and-back.
    mountPreview({ file: { path: '/project/a.md', content: '' } })
    await nextTick()
    expect(syncMarkersFor).toHaveBeenCalledWith('/project/a.md', 'markdown', '')
  })

  it('does not derive markers when the file content is not loaded (null)', async () => {
    mountPreview({ file: { path: '/project/a.md', content: null } })
    await nextTick()
    expect(syncMarkersFor).not.toHaveBeenCalled()
  })

  it('feeds marker tops to the nav and scrolls the body to the selected marker', async () => {
    // positionedMarkers is the nav's target list; next() must scroll the
    // .markdown-body container to that marker's top (16px of headroom).
    ;(extractBlockElements as any).mockReturnValue([
      { el: blockEl(0), tag: 'p', index: 0 },
      { el: blockEl(100), tag: 'p', index: 1 },
      { el: blockEl(200), tag: 'p', index: 2 },
    ])
    diffMarkers.value = [
      markdownMarker('modified-0-p'),
      markdownMarker('modified-1-p'),
      markdownMarker('modified-2-p'),
    ] as any
    const wrapper = mountPreview()
    await nextTick()
    await nextTick()
    expect(wrapper.find('.file-change-nav').text()).toContain('1/3')

    await wrapper.find('.fcn-btn-next').trigger('click')
    await nextTick()

    // index 1 → top 100; minus 16px headroom = 84.
    expect(scrollToMock).toHaveBeenCalledWith({ top: 84, behavior: 'auto' })
  })

  it('flashes the rail of the marker the nav landed on', async () => {
    // The rails render in jsdom (unlike CodeMirror's virtualized gutter), so this
    // exercises the real lookup + flash class on the markdown surface.
    ;(extractBlockElements as any).mockReturnValue([
      { el: blockEl(0), tag: 'p', index: 0 },
      { el: blockEl(100), tag: 'p', index: 1 },
    ])
    diffMarkers.value = [
      markdownMarker('modified-0-p'),
      markdownMarker('modified-1-p'),
    ] as any
    const wrapper = mountPreview()
    await nextTick()
    await nextTick()

    // The flash lands on the next frame (after the jump scroll).
    await wrapper.find('.fcn-btn-next').trigger('click')
    await new Promise(r => setTimeout(r, 50))

    const flashed = wrapper.findAll('.diff-marker-inline.diff-marker-rail-flash')
    expect(flashed.length).toBe(1)
    // index 1 → the second marker (modified-1-p).
    expect(flashed[0].attributes('data-marker-id')).toBe('modified-1-p')
  })

  it('does not flash a rail when the nav cannot move', async () => {
    ;(extractBlockElements as any).mockReturnValue([{ el: blockEl(0), tag: 'p', index: 0 }])
    diffMarkers.value = [markdownMarker('modified-0-p')] as any
    const wrapper = mountPreview()
    await nextTick()
    await nextTick()

    // prev() at index 0 is a no-op.
    await wrapper.find('.fcn-btn-prev').trigger('click')
    await new Promise(r => setTimeout(r, 50))
    expect(wrapper.findAll('.diff-marker-rail-flash').length).toBe(0)
  })

  it('clear button drops the current file baseline and clears positioned markers', async () => {
    ;(extractBlockElements as any).mockReturnValue([{ el: blockEl(0), tag: 'p', index: 0 }])
    diffMarkers.value = [markdownMarker('modified-0-p')] as any
    const wrapper = mountPreview({ file: { path: '/project/a.md', content: '# A' } })
    await nextTick()
    await nextTick()
    expect(wrapper.findAll('.diff-marker-inline').length).toBe(1)

    await wrapper.find('.fcn-btn-clear').trigger('click')
    await nextTick()

    expect(clearBaseline).toHaveBeenCalledWith('/project/a.md')
    expect(wrapper.findAll('.diff-marker-inline').length).toBe(0)
  })

  it('restores rails when switching away and back (markers survive navigation)', async () => {
    // The headline claim: a file with a baseline shows its markers again on
    // return. syncMarkersFor is the derive entry point; make it publish a marker
    // for the target path (as the real one does) and assert the rail renders.
    ;(extractBlockElements as any).mockReturnValue([{ el: blockEl(0), tag: 'p', index: 0 }])
    ;(syncMarkersFor as any).mockImplementation((path: string) => {
      if (path === '/project/a.md') diffMarkers.value = [markdownMarker('modified-0-p')] as any
      else diffMarkers.value = []
    })

    const wrapper = mountPreview({ file: { path: '/project/b.md', content: '# B' } })
    await nextTick()
    await nextTick()
    // B has no baseline → no rails.
    expect(wrapper.findAll('.diff-marker-inline').length).toBe(0)

    // Switch back to A, which has a baseline → its markers must reappear.
    await wrapper.setProps({ file: { path: '/project/a.md', content: '# A' } })
    await nextTick()
    await nextTick()
    expect(syncMarkersFor).toHaveBeenCalledWith('/project/a.md', 'markdown', '# A')
    expect(wrapper.findAll('.diff-marker-inline').length).toBe(1)

    // Clean up the per-test implementation so it does not leak.
    ;(syncMarkersFor as any).mockReset()
  })
})
