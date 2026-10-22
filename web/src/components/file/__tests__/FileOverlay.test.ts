import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import FileOverlay from '../FileOverlay.vue'
import { _resetForTest as _resetWideForTest } from '@/composables/useWideScreenLayout'

// FileOverlay now renders the panel skeleton for its loading mask, which reads
// a translated label — so the component needs a real i18n instance.
const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: { en: { common: { loading: 'Loading' } } },
})

// FileViewer stub forwards the inline TOC dock props/events so tests can
// verify FileOverlay passes them through.
const FileViewerStub = {
  name: 'FileViewer',
  props: ['file', 'tocOpen', 'searchOpen', 'markdownViewMode', 'externalLoading', 'tocFile', 'pdfOutline', 'docked'],
  emits: ['delete', 'showDetails', 'openGitHistory', 'toggleToc', 'closeToc', 'toggleSearch', 'toggleView', 'refresh', 'openFile', 'overlayClose', 'navigateBack', 'navigateForward', 'shareExternal', 'jump', 'jumpPage'],
  template: '<div class="file-viewer-stub"><button class="viewer-close-toc-stub" @click="$emit(\'closeToc\')" /></div>',
}

const stubs = {
  FileViewer: FileViewerStub,
  LoadingIndicator: true,
  TocDrawer: true,
  TocDock: true,
  SearchDrawer: true,
  GitHistoryDrawer: true,
  Transition: { template: '<div><slot/></div>' },
}

// jsdom's default innerWidth (1024) is ≥ the wide-screen threshold, which
// would force docked mode regardless of our intent. The physical-width branch
// (cssWidth × devicePixelRatio ≥ 1280 AND landscape screen) can also trip it.
// Override all three inputs per test.
function setViewportWidth(width: number) {
  Object.defineProperty(window, 'innerWidth', { value: width, configurable: true, writable: true })
  Object.defineProperty(window, 'devicePixelRatio', { value: 1, configurable: true, writable: true })
  Object.defineProperty(window.screen, 'width', { value: 1024, configurable: true, writable: true })
  Object.defineProperty(window.screen, 'height', { value: 768, configurable: true, writable: true })
  // Force wide-screen re-init so the next getWideScreenState() recomputes from
  // the stubbed viewport instead of returning the stale first-init value.
  _resetWideForTest()
}

describe('FileOverlay', () => {
  it('renders when overlayOpen is true', () => {
    setViewportWidth(800)
    const wrapper = mount(FileOverlay, {
      props: { overlayOpen: true, currentFile: { path: 'test.txt' } },
      global: { stubs, plugins: [i18n] },
    })
    expect(wrapper.find('.file-overlay').exists()).toBe(true)
  })

  it('does not render when overlayOpen is false', () => {
    setViewportWidth(800)
    const wrapper = mount(FileOverlay, {
      props: { overlayOpen: false },
      global: { stubs, plugins: [i18n] },
    })
    expect(wrapper.find('.file-overlay').exists()).toBe(false)
  })

  it('exposes pdfScrollToPage and focusSearchInput', () => {
    setViewportWidth(800)
    const wrapper = mount(FileOverlay, {
      props: { overlayOpen: true, currentFile: {} },
      global: { stubs, plugins: [i18n] },
    })
    expect(typeof wrapper.vm.pdfScrollToPage).toBe('function')
    expect(typeof wrapper.vm.focusSearchInput).toBe('function')
  })
})

describe('FileOverlay — TOC dock vs drawer', () => {
  it('renders the bottom TocDrawer on narrow screens (docked=false)', () => {
    setViewportWidth(800)
    const wrapper = mount(FileOverlay, {
      props: { overlayOpen: true, currentFile: { path: 'a.md' }, tocOpen: true },
      global: { stubs, plugins: [i18n] },
    })
    expect(wrapper.findComponent({ name: 'TocDrawer' }).exists()).toBe(true)
    // Narrow screen: FileViewer receives docked=false so it won't render TocDock.
    const viewer = wrapper.findComponent({ name: 'FileViewer' })
    expect(viewer.props('docked')).toBe(false)
  })

  it('passes docked=true to FileViewer on wide screens so it can render the inline TocDock', () => {
    setViewportWidth(1400)
    const wrapper = mount(FileOverlay, {
      props: { overlayOpen: true, currentFile: { path: 'a.md' }, tocOpen: true, tocFile: { name: 'a.md' }, pdfOutline: [] },
      global: { stubs, plugins: [i18n] },
    })
    const viewer = wrapper.findComponent({ name: 'FileViewer' })
    expect(viewer.props('docked')).toBe(true)
    expect(viewer.props('tocFile')).toMatchObject({ name: 'a.md' })
    // Bottom drawer is suppressed in docked mode
    expect(wrapper.findComponent({ name: 'TocDrawer' }).exists()).toBe(false)
  })

  it('forwards FileViewer closeToc as closeToc (not toggleToc)', async () => {
    setViewportWidth(1400)
    const wrapper = mount(FileOverlay, {
      props: { overlayOpen: true, currentFile: { path: 'a.md' }, tocOpen: true },
      global: { stubs, plugins: [i18n] },
    })
    await wrapper.find('.viewer-close-toc-stub').trigger('click')
    expect(wrapper.emitted('closeToc')).toBeTruthy()
    expect(wrapper.emitted('toggleToc')).toBeFalsy()
  })

  it('forwards FileViewer jump/jumpPage so the parent can scroll code/PDF', () => {
    setViewportWidth(1400)
    const wrapper = mount(FileOverlay, {
      props: { overlayOpen: true, currentFile: { path: 'a.md' }, tocOpen: true },
      global: { stubs, plugins: [i18n] },
    })
    const viewer = wrapper.findComponent({ name: 'FileViewer' })
    viewer.vm.$emit('jump', 12)
    viewer.vm.$emit('jumpPage', 3)
    // Regression: inline TOC dock clicks must reach App's scrollToLine /
    // handleJumpPdfPage — previously the dock was only wired for close.
    expect(wrapper.emitted('jump')).toEqual([[12, undefined]])
    expect(wrapper.emitted('jumpPage')).toEqual([[3]])
  })
})
