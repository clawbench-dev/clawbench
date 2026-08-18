import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import FileOverlay from '../FileOverlay.vue'

const stubs = {
  FileViewer: { template: '<div class="file-viewer-stub"><slot/></div>' },
  LoadingIndicator: { template: '<div class="loading-stub" v-if="$props.overlay" />' },
  TocDrawer: { template: '<div class="toc-drawer-stub" />' },
  SearchDrawer: { template: '<div class="search-drawer-stub" />' },
  GitHistoryDrawer: { template: '<div class="git-history-stub" />' },
  Transition: { template: '<div><slot/></div>' },
}

describe('FileOverlay', () => {
  it('renders when overlayOpen is true', () => {
    const wrapper = mount(FileOverlay, {
      props: { overlayOpen: true, currentFile: { path: 'test.txt' } },
      global: { stubs },
    })
    expect(wrapper.find('.file-overlay').exists()).toBe(true)
  })

  it('does not render when overlayOpen is false', () => {
    const wrapper = mount(FileOverlay, {
      props: { overlayOpen: false },
      global: { stubs },
    })
    expect(wrapper.find('.file-overlay').exists()).toBe(false)
  })

  it('shows loading indicator when fileLoading is true', () => {
    const wrapper = mount(FileOverlay, {
      props: { overlayOpen: true, fileLoading: true, currentFile: {} },
      global: { stubs },
    })
    expect(wrapper.find('.loading-stub').exists()).toBe(true)
  })

  it('emits openFile on chat-file-open-btn click', async () => {
    const wrapper = mount(FileOverlay, {
      props: { overlayOpen: true, currentFile: {} },
      global: { stubs },
    })
    const body = wrapper.find('.file-overlay-body')
    // Simulate a click on a chat-file-open-btn element
    const btn = document.createElement('button')
    btn.className = 'chat-file-open-btn'
    btn.setAttribute('data-file-path', 'src/main.go')
    btn.setAttribute('data-line-start', '10')
    await body.trigger('click', { target: btn })
    expect(wrapper.emitted('openFile')).toBeTruthy()
  })

  it('exposes pdfScrollToPage and focusSearchInput', () => {
    const wrapper = mount(FileOverlay, {
      props: { overlayOpen: true, currentFile: {} },
      global: { stubs },
    })
    expect(typeof wrapper.vm.pdfScrollToPage).toBe('function')
    expect(typeof wrapper.vm.focusSearchInput).toBe('function')
  })
})
