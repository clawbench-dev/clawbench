import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { defineComponent } from 'vue'
import FileOverlay from '@/components/file/FileOverlay.vue'

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
  SearchDrawer: SearchDrawerStub,
  GitHistoryDrawer: GitHistoryDrawerStub,
  LoadingIndicator: true,
  Transition: { template: '<div><slot /></div>' },
}

describe('FileOverlay', () => {
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
      global: { stubs },
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
    const stub = wrapper.findComponent({ name: 'LoadingIndicator' })
    expect(stub.exists()).toBe(true)
  })

  it('renders FileViewer stub when overlay is open', () => {
    const wrapper = mountOverlay({ overlayOpen: true, currentFile: { path: 'a.txt' } })
    expect(wrapper.find('.file-viewer-stub').exists()).toBe(true)
  })

  it('renders TocDrawer when tocOpen is true', () => {
    const wrapper = mountOverlay({ overlayOpen: true, tocOpen: true })
    expect(wrapper.find('.toc-drawer-stub').exists()).toBe(true)
  })

  it('renders SearchDrawer when searchOpen is true', () => {
    const wrapper = mountOverlay({ overlayOpen: true, searchOpen: true })
    expect(wrapper.find('.search-drawer-stub').exists()).toBe(true)
  })

  it('emits closeSearch (not toggleSearch) when SearchDrawer closes', async () => {
    const wrapper = mountOverlay({ overlayOpen: true, searchOpen: true })
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
