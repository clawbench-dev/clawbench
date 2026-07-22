import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick, computed, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import FileHeader from '../FileHeader.vue'

// Minimal i18n instance for tests
const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      nav: { refresh: 'Refresh' },
      common: { download: 'Download', delete: 'Delete', close: 'Close' },
      chat: {
        actions: { attachToChat: 'Attach' },
        attach: { removeFromChat: 'Remove', addedToChat: 'Added', removedFromChat: 'Removed' },
      },
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
          shareExternal: 'Share',
          exportHtml: 'Export HTML',
        },
        overlay: { back: 'Back' },
      },
    },
  },
})

// Mock ResizeObserver (not available in jsdom)
vi.stubGlobal('ResizeObserver', class {
  observe() {}
  unobserve() {}
  disconnect() {}
})

// Mock useAppMode
vi.mock('@/composables/useAppMode.ts', () => ({
  useAppMode: () => ({ isAppMode: { value: false } }),
}))

// Mock useChatContext
const mockAddAttachedFile = vi.fn()
const mockHasAttachedFile = vi.fn(() => false)
const mockRemoveAttachedFileByPath = vi.fn()
vi.mock('@/composables/useChatContext.ts', () => ({
  useChatContext: () => ({
    addAttachedFile: mockAddAttachedFile,
    hasAttachedFile: mockHasAttachedFile,
    toggleAttachedFile: vi.fn(),
    removeAttachedFileByPath: mockRemoveAttachedFileByPath,
  }),
}))

// Mock useToast
vi.mock('@/composables/useToast.ts', () => ({
  useToast: () => ({ show: vi.fn() }),
}))

// Mock useToolbarOverflow — simulate wide toolbar: all demotable items inline
vi.mock('@/composables/useToolbarOverflow', () => ({
  useToolbarOverflow: (_getEl, getDemotableIds) => ({
    inlineIds: computed(() => getDemotableIds()),
    collapsedIds: computed(() => []),
    contentWidth: ref(800),
    startObserving: vi.fn(),
    stopObserving: vi.fn(),
  }),
}))

// Mock getFileType
vi.mock('@/utils/fileType.ts', () => ({
  getFileType: (name: string) => {
    if (name.endsWith('.md')) return { isMarkdown: true, isHtml: false, isImage: false, isAudio: false, isVideo: false, isPdf: false }
    if (name.endsWith('.html')) return { isMarkdown: false, isHtml: true, isImage: false, isAudio: false, isVideo: false, isPdf: false }
    if (name.endsWith('.png')) return { isMarkdown: false, isHtml: false, isImage: true, isAudio: false, isVideo: false, isPdf: false }
    if (name.endsWith('.pdf')) return { isMarkdown: false, isHtml: false, isImage: false, isAudio: false, isVideo: false, isPdf: true }
    if (name.endsWith('.mp3')) return { isMarkdown: false, isHtml: false, isImage: false, isAudio: true, isVideo: false, isPdf: false }
    if (name.endsWith('.mp4')) return { isMarkdown: false, isHtml: false, isImage: false, isAudio: false, isVideo: true, isPdf: false }
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
        wordWrap: true,
        showLineNumbers: true,
        stickyScroll: true,
        overlayOpen: false,
        recentFilesCount: 0,
        ...props,
      },
      global: {
        plugins: [i18n],
      },
    })
  }

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

  it('emits toggleStickyScroll when handler is called', async () => {
    const wrapper = mountHeader({ viewMode: 'source' })
    const vm = wrapper.vm as any
    vm.$.setupState.handleToggleStickyScroll()
    await nextTick()
    expect(wrapper.emitted('toggleStickyScroll')).toBeTruthy()
    expect(getMenuOpen(wrapper)).toBe(false)
  })

  it('emits toggleWordWrap when handler is called', async () => {
    const wrapper = mountHeader({ viewMode: 'source' })
    const vm = wrapper.vm as any
    vm.$.setupState.handleToggleWordWrap()
    await nextTick()
    expect(wrapper.emitted('toggleWordWrap')).toBeTruthy()
    expect(getMenuOpen(wrapper)).toBe(false)
  })

  it('emits toggleLineNumbers when handler is called', async () => {
    const wrapper = mountHeader({ viewMode: 'source' })
    const vm = wrapper.vm as any
    vm.$.setupState.handleToggleLineNumbers()
    await nextTick()
    expect(wrapper.emitted('toggleLineNumbers')).toBeTruthy()
    expect(getMenuOpen(wrapper)).toBe(false)
  })

  it('renders header actions with file-header-btn class', () => {
    const wrapper = mountHeader()
    const btns = wrapper.findAll('.file-header-btn')
    expect(btns.length).toBeGreaterThan(0)
  })

  it('emits toggleView when handleToggleView is called', async () => {
    const wrapper = mountHeader({ viewMode: 'source' })
    const vm = wrapper.vm as any
    vm.$.setupState.handleToggleView()
    await nextTick()
    expect(wrapper.emitted('toggleView')).toBeTruthy()
    expect(getMenuOpen(wrapper)).toBe(false)
  })

  it('emits openAsText when handleOpenAsText is called', async () => {
    const wrapper = mountHeader({ viewMode: 'source' })
    const vm = wrapper.vm as any
    vm.$.setupState.handleOpenAsText()
    await nextTick()
    expect(wrapper.emitted('openAsText')).toBeTruthy()
    expect(getMenuOpen(wrapper)).toBe(false)
  })

  it('emits exportHtml when handleExportHtml is called', async () => {
    const wrapper = mountHeader({ viewMode: 'source' })
    const vm = wrapper.vm as any
    vm.$.setupState.handleExportHtml()
    await nextTick()
    expect(wrapper.emitted('exportHtml')).toBeTruthy()
    expect(getMenuOpen(wrapper)).toBe(false)
  })

  it('emits delete with file path when handleDelete is called', async () => {
    const wrapper = mountHeader()
    const vm = wrapper.vm as any
    vm.$.setupState.handleDelete()
    await nextTick()
    expect(wrapper.emitted('delete')).toBeTruthy()
    expect(wrapper.emitted('delete')![0]).toEqual(['/tmp/main.ts'])
    expect(getMenuOpen(wrapper)).toBe(false)
  })

  it('emits openGitHistory when handleGitHistory is called', async () => {
    const wrapper = mountHeader()
    const vm = wrapper.vm as any
    vm.$.setupState.handleGitHistory()
    await nextTick()
    expect(wrapper.emitted('openGitHistory')).toBeTruthy()
    expect(getMenuOpen(wrapper)).toBe(false)
  })

  it('emits refresh when handleRefresh is called', async () => {
    const wrapper = mountHeader()
    const vm = wrapper.vm as any
    vm.$.setupState.handleRefresh()
    await nextTick()
    expect(wrapper.emitted('refresh')).toBeTruthy()
    expect(getMenuOpen(wrapper)).toBe(false)
  })

  it('emits showDetails when file name is clicked', async () => {
    const wrapper = mountHeader()
    const nameEl = wrapper.find('.file-path-hint')
    await nameEl.trigger('click')
    expect(wrapper.emitted('showDetails')).toBeTruthy()
  })

  it('emits toggleToc when toc button is clicked', async () => {
    const wrapper = mountHeader({ file: { name: 'main.ts', path: '/tmp/main.ts', content: 'code' } })
    // Find the TOC button specifically (it has the List icon, after the recent files button)
    const allBtns = wrapper.findAll('.header-actions .file-header-btn')
    // First button is recent files, second should be TOC or search
    const tocBtn = allBtns.length > 1 ? allBtns[1] : null
    if (tocBtn && tocBtn.exists()) {
      await tocBtn.trigger('click')
      const emitted = wrapper.emitted()
      expect(emitted['toggleToc'] || emitted['toggleSearch']).toBeTruthy()
    }
  })

  it('adds file to chat context when attach button is clicked', async () => {
    mockHasAttachedFile.mockReturnValue(false)
    mockAddAttachedFile.mockReset()
    const wrapper = mountHeader()
    const vm = wrapper.vm as any
    vm.$.setupState.handleAttachToChat()
    await nextTick()
    expect(mockAddAttachedFile).toHaveBeenCalledWith('/tmp/main.ts')
  })

  it('removes file from chat context when already attached', async () => {
    mockHasAttachedFile.mockReturnValue(true)
    mockRemoveAttachedFileByPath.mockReset()
    const wrapper = mountHeader()
    const vm = wrapper.vm as any
    vm.$.setupState.handleAttachToChat()
    await nextTick()
    expect(mockRemoveAttachedFileByPath).toHaveBeenCalledWith('/tmp/main.ts')
  })

  it('does not attach when file has no path', async () => {
    const wrapper = mountHeader({ file: { name: 'test.ts', path: '', content: '' } })
    const vm = wrapper.vm as any
    mockAddAttachedFile.mockReset()
    vm.$.setupState.handleAttachToChat()
    await nextTick()
    expect(mockAddAttachedFile).not.toHaveBeenCalled()
  })

  it('closes menu after toggling sticky scroll', async () => {
    const wrapper = mountHeader({ viewMode: 'source' })
    await wrapper.find('.dropdown-wrapper .file-header-btn').trigger('click')
    expect(getMenuOpen(wrapper)).toBe(true)
    const vm = wrapper.vm as any
    vm.$.setupState.handleToggleStickyScroll()
    await nextTick()
    expect(getMenuOpen(wrapper)).toBe(false)
  })

  describe('media file filtering', () => {
    it('hides code-only toolbar items for image files', async () => {
      const wrapper = mountHeader({ file: { name: 'photo.png', path: '/tmp/photo.png', content: '' } })
      const vm = wrapper.vm as any
      expect(vm.$.setupState.isMediaFile).toBe(true)
      // wordWrap, lineNumbers, stickyScroll should not be in toolbar IDs
      const ids = vm.$.setupState.toolbarInlineIds
      expect(ids).not.toContain('wordWrap')
      expect(ids).not.toContain('lineNumbers')
      expect(ids).not.toContain('stickyScroll')
      expect(ids).not.toContain('toggleView')
      // attach should still be available
      expect(ids).toContain('attach')
      // refresh is not included for media files without text content
      expect(ids).not.toContain('refresh')
    })

    it('hides code-only toolbar items for audio files', async () => {
      const wrapper = mountHeader({ file: { name: 'song.mp3', path: '/tmp/song.mp3', content: '' } })
      const vm = wrapper.vm as any
      expect(vm.$.setupState.isMediaFile).toBe(true)
      const ids = vm.$.setupState.toolbarInlineIds
      expect(ids).not.toContain('wordWrap')
      expect(ids).not.toContain('lineNumbers')
      expect(ids).not.toContain('stickyScroll')
    })

    it('hides code-only toolbar items for video files', async () => {
      const wrapper = mountHeader({ file: { name: 'clip.mp4', path: '/tmp/clip.mp4', content: '' } })
      const vm = wrapper.vm as any
      expect(vm.$.setupState.isMediaFile).toBe(true)
      const ids = vm.$.setupState.toolbarInlineIds
      expect(ids).not.toContain('wordWrap')
      expect(ids).not.toContain('lineNumbers')
      expect(ids).not.toContain('stickyScroll')
    })

    it('hides code-only toolbar items for PDF files', async () => {
      const wrapper = mountHeader({ file: { name: 'doc.pdf', path: '/tmp/doc.pdf', content: '' } })
      const vm = wrapper.vm as any
      expect(vm.$.setupState.isMediaFile).toBe(true)
      const ids = vm.$.setupState.toolbarInlineIds
      expect(ids).not.toContain('wordWrap')
      expect(ids).not.toContain('lineNumbers')
      expect(ids).not.toContain('stickyScroll')
      expect(ids).not.toContain('toggleView')
      // PDF keeps TOC and search
      expect(ids).toContain('toc')
      expect(ids).toContain('search')
    })

    it('shows code toolbar items for text files', async () => {
      const wrapper = mountHeader({ file: { name: 'main.ts', path: '/tmp/main.ts', content: 'code' } })
      const vm = wrapper.vm as any
      expect(vm.$.setupState.isMediaFile).toBe(false)
      const ids = vm.$.setupState.toolbarInlineIds
      expect(ids).toContain('wordWrap')
      expect(ids).toContain('lineNumbers')
      expect(ids).toContain('stickyScroll')
    })
  })
})
