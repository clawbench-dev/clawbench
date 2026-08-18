import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import FileOverlay from '../FileOverlay.vue'

const stubs = {
  FileViewer: { template: '<div class="file-viewer-stub"><slot/></div>' },
  LoadingIndicator: true,
  TocDrawer: true,
  SearchDrawer: true,
  GitHistoryDrawer: true,
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

  it('exposes pdfScrollToPage and focusSearchInput', () => {
    const wrapper = mount(FileOverlay, {
      props: { overlayOpen: true, currentFile: {} },
      global: { stubs },
    })
    expect(typeof wrapper.vm.pdfScrollToPage).toBe('function')
    expect(typeof wrapper.vm.focusSearchInput).toBe('function')
  })
})
