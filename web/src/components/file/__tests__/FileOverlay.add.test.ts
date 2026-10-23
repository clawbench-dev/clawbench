import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { defineComponent } from 'vue'
import FileOverlay from '@/components/file/FileOverlay.vue'
import { _resetForTest as _resetWideForTest } from '@/composables/useWideScreenLayout'

// FileOverlay renders the panel skeleton for its loading mask, which reads a
// translated label — so the component needs a real i18n instance. Without one
// `useI18n()` throws at setup and every case in this file fails.
const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: { en: { common: { loading: 'Loading' } } },
})

// jsdom's default innerWidth (1024) is ≥ the wide-screen threshold, which
// forces docked mode and hides the bottom drawer. This test file asserts the
// narrow-screen (drawer) path, so force a narrow viewport. (The wide-screen
// dock path is covered in FileOverlay.test.ts.) Resetting the wide-screen
// module forces the next getWideScreenState() to recompute from the stub.
function setNarrowViewport() {
  Object.defineProperty(window, 'innerWidth', { value: 800, configurable: true, writable: true })
  Object.defineProperty(window, 'devicePixelRatio', { value: 1, configurable: true, writable: true })
  Object.defineProperty(window.screen, 'width', { value: 800, configurable: true, writable: true })
  Object.defineProperty(window.screen, 'height', { value: 1280, configurable: true, writable: true })
  _resetWideForTest()
}

const FileViewerStub = defineComponent({
  name: 'FileViewer',
  props: ['file', 'tocOpen', 'searchOpen', 'markdownViewMode', 'externalLoading'],
  emits: ['delete', 'showDetails', 'openGitHistory', 'toggleToc', 'toggleSearch', 'toggleView', 'refresh', 'openFile', 'overlayClose', 'navigateBack', 'navigateForward', 'shareExternal'],
  template: '<div class="file-viewer-stub" />',
})

const TocDrawerStub = defineComponent({
  name: 'TocDrawer',
  props: ['open', 'file', 'pdfOutline'],
  emits: ['close', 'jump', 'jumpPage'],
  template: '<div v-if="open" class="toc-drawer-stub" />',
})

const SearchDrawerStub = defineComponent({
  name: 'SearchDrawer',
  props: ['open', 'file', 'viewMode'],
  emits: ['close', 'jump'],
  template: '<div v-if="open" class="search-drawer-stub" />',
})

const GitHistoryDrawerStub = defineComponent({
  name: 'GitHistoryDrawer',
  props: ['open', 'mode', 'file'],
  emits: ['close', 'openFile'],
  template: '<div v-if="open" class="git-history-stub" />',
})

const stubs = {
  FileViewer: FileViewerStub,
  TocDrawer: TocDrawerStub,
  TocDock: true,
  SearchDrawer: SearchDrawerStub,
  GitHistoryDrawer: GitHistoryDrawerStub,
  // The loading mask is the panel skeleton now (it was a centered spinner), so
  // the stub name has to follow or the assertion looks for a node that is gone.
  PanelSkeleton: { name: 'PanelSkeleton', props: ['variant', 'rows', 'label'], template: '<div class="panel-skeleton-stub" />' },
  Transition: { template: '<div><slot /></div>' },
}

describe('FileOverlay', () => {
  setNarrowViewport()

  function mountOverlay(props: Record<string, unknown> = {}) {
    return mount(FileOverlay, {
      props: {
        overlayOpen: false,
        currentFile: null,
        fileLoading: false,
        tocOpen: false,
        searchOpen: false,
        markdownViewMode: 'rendered',
        fileHistoryOpen: false,
        tocFile: null,
        pdfOutline: [],
        ...props,
      },
      global: { stubs, plugins: [i18n] },
    })
  }

  it('renders when overlayOpen is true', () => {
    const wrapper = mountOverlay({ overlayOpen: true })
    expect(wrapper.find('.file-overlay').exists()).toBe(true)
  })

  it('does not render the overlay when overlayOpen is false', () => {
    const wrapper = mountOverlay({ overlayOpen: false })
    expect(wrapper.find('.file-overlay').exists()).toBe(false)
  })

  it('shows loading indicator when fileLoading is true', () => {
    const wrapper = mountOverlay({ overlayOpen: true, fileLoading: true })
    expect(wrapper.findComponent({ name: 'PanelSkeleton' }).exists()).toBe(true)
  })

  it('renders FileViewer stub when overlay is open', () => {
    const wrapper = mountOverlay({ overlayOpen: true, currentFile: { path: 'a.txt' } })
    expect(wrapper.find('.file-viewer-stub').exists()).toBe(true)
  })

  it('renders TocDrawer when tocOpen is true', () => {
    const wrapper = mountOverlay({ overlayOpen: true, tocOpen: true })
    expect(wrapper.find('.toc-drawer-stub').exists()).toBe(true)
  })

  it('renders SearchDrawer when searchDrawerOpen is true', () => {
    const wrapper = mountOverlay({ overlayOpen: true, searchDrawerOpen: true })
    expect(wrapper.find('.search-drawer-stub').exists()).toBe(true)
  })

  it('does not render SearchDrawer when only searchOpen (inline search UI) is true', () => {
    const wrapper = mountOverlay({ overlayOpen: true, searchOpen: true })
    expect(wrapper.find('.search-drawer-stub').exists()).toBe(false)
  })

  it('emits closeSearch (not toggleSearch) when SearchDrawer closes', async () => {
    const wrapper = mountOverlay({ overlayOpen: true, searchDrawerOpen: true })
    const drawer = wrapper.findComponent(SearchDrawerStub)
    drawer.vm.$emit('close')
    await wrapper.vm.$nextTick()
    expect(wrapper.emitted('closeSearch')).toBeTruthy()
    expect(wrapper.emitted('toggleSearch')).toBeFalsy()
  })

  it('renders GitHistoryDrawer when fileHistoryOpen is true', () => {
    const wrapper = mountOverlay({ overlayOpen: true, fileHistoryOpen: true })
    expect(wrapper.find('.git-history-stub').exists()).toBe(true)
  })

  it('exposes pdfScrollToPage, pdfOutline, focusSearchInput', () => {
    const wrapper = mountOverlay({ overlayOpen: true })
    expect(typeof (wrapper.vm as any).pdfScrollToPage).toBe('function')
    expect(typeof (wrapper.vm as any).focusSearchInput).toBe('function')
    expect((wrapper.vm as any).pdfOutline).toBeDefined()
  })

  it('handles file-open button click inside overlay', async () => {
    const wrapper = mountOverlay({ overlayOpen: true })
    const btn = document.createElement('button')
    btn.className = 'chat-file-open-btn'
    btn.setAttribute('data-file-path', '/foo/bar.ts')
    btn.setAttribute('data-line-start', '10')
    btn.setAttribute('data-line-end', '20')
    wrapper.find('.file-overlay-body').element.appendChild(btn)
    await (wrapper.vm as any).handleContentClick({ target: btn, preventDefault: vi.fn(), stopPropagation: vi.fn() })
    const emitted = wrapper.emitted('openFile')
    expect(emitted).toBeTruthy()
    expect((emitted as any)[0][0]).toEqual({ path: '/foo/bar.ts', lineStart: 10, lineEnd: 20 })
  })

  it('handles annotated file-path span click', async () => {
    const wrapper = mountOverlay({ overlayOpen: true })
    const span = document.createElement('span')
    span.className = 'chat-file-path'
    span.setAttribute('data-file-path', '/a/b/c.md')
    wrapper.find('.file-overlay-body').element.appendChild(span)
    await (wrapper.vm as any).handleContentClick({ target: span, preventDefault: vi.fn(), stopPropagation: vi.fn() })
    const emitted = wrapper.emitted('openFile')
    expect(emitted).toBeTruthy()
    expect((emitted as any)[0][0]).toEqual({ path: '/a/b/c.md', lineStart: undefined, lineEnd: undefined })
  })

  it('does nothing on click outside file-path elements', async () => {
    const wrapper = mountOverlay({ overlayOpen: true })
    const div = document.createElement('div')
    div.textContent = 'plain'
    wrapper.find('.file-overlay-body').element.appendChild(div)
    await (wrapper.vm as any).handleContentClick({ target: div, preventDefault: vi.fn(), stopPropagation: vi.fn() })
    expect(wrapper.emitted('openFile')).toBeFalsy()
  })
})
