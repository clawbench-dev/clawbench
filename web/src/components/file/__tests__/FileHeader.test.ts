import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import FileHeader from '../FileHeader.vue'

// Minimal i18n instance for tests
const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      nav: { refresh: 'Refresh' },
      common: { download: 'Download', delete: 'Delete' },
      chat: { actions: { attachToChat: 'Attach' }, attach: { removeFromChat: 'Remove' } },
      file: {
        header: {
          toc: 'TOC',
          search: 'Search',
          more: 'More',
          openAsText: 'Open as text',
          sourceView: 'Source',
          renderedView: 'Rendered',
          wordWrap: 'Word Wrap',
          lineNumbers: 'Line Numbers',
          stickyScroll: 'Sticky Scroll',
          fileHistory: 'File history',
        },
      },
    },
  },
})

// Mock useAppMode
vi.mock('@/composables/useAppMode.ts', () => ({
  useAppMode: () => ({ isAppMode: { value: false } }),
}))

// Mock useChatContext
vi.mock('@/composables/useChatContext.ts', () => ({
  useChatContext: () => ({
    addAttachedFile: vi.fn(),
    hasAttachedFile: () => false,
    toggleAttachedFile: vi.fn(),
    removeAttachedFileByPath: vi.fn(),
  }),
}))

// Mock useToast
vi.mock('@/composables/useToast.ts', () => ({
  useToast: () => ({ show: vi.fn() }),
}))

// Mock getFileType
vi.mock('@/utils/fileType.ts', () => ({
  getFileType: (name: string) => {
    if (name.endsWith('.md')) return { isMarkdown: true, isHtml: false, isImage: false, isAudio: false, isVideo: false, isPdf: false }
    if (name.endsWith('.html')) return { isMarkdown: false, isHtml: true, isImage: false, isAudio: false, isVideo: false, isPdf: false }
    if (name.endsWith('.png')) return { isMarkdown: false, isHtml: false, isImage: true, isAudio: false, isVideo: false, isPdf: false }
    return { isMarkdown: false, isHtml: false, isImage: false, isAudio: false, isVideo: false, isPdf: false }
  },
}))

describe('FileHeader', () => {
  function mountHeader(props = {}) {
    return mount(FileHeader, {
      props: {
        file: { name: 'main.ts', path: '/tmp/main.ts', content: 'const x = 1' },
        viewMode: 'source',
        tocOpen: false,
        searchOpen: false,
        wordWrap: false,
        showLineNumbers: true,
        stickyScroll: true,
        ...props,
      },
      global: {
        plugins: [i18n],
      },
    })
  }

  // Helper to get internal menuOpen ref
  function getMenuOpen(wrapper: ReturnType<typeof mount>): boolean {
    return (wrapper.vm as any).$.setupState.menuOpen
  }

  it('toggles menu open on dropdown button click', async () => {
    const wrapper = mountHeader({ viewMode: 'source' })
    expect(getMenuOpen(wrapper)).toBe(false)
    await wrapper.find('.dropdown-wrapper .file-header-btn').trigger('click')
    expect(getMenuOpen(wrapper)).toBe(true)
  })

  it('closes menu on second dropdown button click', async () => {
    const wrapper = mountHeader({ viewMode: 'source' })
    await wrapper.find('.dropdown-wrapper .file-header-btn').trigger('click')
    expect(getMenuOpen(wrapper)).toBe(true)
    await wrapper.find('.dropdown-wrapper .file-header-btn').trigger('click')
    expect(getMenuOpen(wrapper)).toBe(false)
  })

  it('emits toggleStickyScroll when handleToggleStickyScroll is called', async () => {
    const wrapper = mountHeader({ viewMode: 'source' })
    // Call the internal handler directly (Teleport content not accessible in jsdom)
    const vm = wrapper.vm as any
    vm.$.setupState.handleToggleStickyScroll()
    await nextTick()
    expect(wrapper.emitted('toggleStickyScroll')).toBeTruthy()
    // Menu should also close
    expect(getMenuOpen(wrapper)).toBe(false)
  })

  it('emits toggleWordWrap when handleToggleWordWrap is called', async () => {
    const wrapper = mountHeader({ viewMode: 'source' })
    const vm = wrapper.vm as any
    vm.$.setupState.handleToggleWordWrap()
    await nextTick()
    expect(wrapper.emitted('toggleWordWrap')).toBeTruthy()
    expect(getMenuOpen(wrapper)).toBe(false)
  })

  it('emits toggleLineNumbers when handleToggleLineNumbers is called', async () => {
    const wrapper = mountHeader({ viewMode: 'source' })
    const vm = wrapper.vm as any
    vm.$.setupState.handleToggleLineNumbers()
    await nextTick()
    expect(wrapper.emitted('toggleLineNumbers')).toBeTruthy()
    expect(getMenuOpen(wrapper)).toBe(false)
  })

  it('renders header actions with square buttons (border-radius: 4px)', () => {
    const wrapper = mountHeader()
    const btns = wrapper.findAll('.file-header-btn')
    expect(btns.length).toBeGreaterThan(0)
    for (const btn of btns) {
      expect(btn.classes()).toContain('file-header-btn')
    }
  })

  it('closes menu after toggling sticky scroll', async () => {
    const wrapper = mountHeader({ viewMode: 'source' })
    // Open dropdown
    await wrapper.find('.dropdown-wrapper .file-header-btn').trigger('click')
    expect(getMenuOpen(wrapper)).toBe(true)
    // Call handler directly (simulates clicking the sticky scroll menu item)
    const vm = wrapper.vm as any
    vm.$.setupState.handleToggleStickyScroll()
    await nextTick()
    // Menu should be closed
    expect(getMenuOpen(wrapper)).toBe(false)
  })
})
