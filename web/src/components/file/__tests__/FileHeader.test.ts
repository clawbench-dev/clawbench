import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick, computed, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import FileHeader from '../FileHeader.vue'
import { useFileShare } from '@/composables/useFileShare'

// useFileShare is a module-level singleton; isolate it per test.
const { markShared, markUnshared, resetFileShareState } = useFileShare()

beforeEach(() => {
  resetFileShareState()
  // The share-highlight watcher fires a server query on mount; keep it inert.
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: () => Promise.resolve({}) }))
})

afterEach(() => {
  resetFileShareState()
  vi.unstubAllGlobals()
})

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
          shareLink: 'Share link',
          shareLinkActive: 'Share link (active)',
          exportHtml: 'Export HTML',
          edit: 'Edit',
          finishEditing: 'Finish editing',
          details: 'Details',
          setAsBackground: 'Set as theme background',
        },
        overlay: { back: 'Back', forward: 'Forward' },
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

// Mock useFileRefresh: refresh-button spin driven by shared isRefreshing ref.
// Tests set .value before mounting and re-mount to flip the state.
const { mockIsRefreshing } = vi.hoisted(() => ({ mockIsRefreshing: { value: false } }))
vi.mock('@/composables/useFileRefresh', () => ({
  isRefreshing: mockIsRefreshing,
}))

// Mock useAppMode
vi.mock('@/composables/useAppMode.ts', () => ({
  useAppMode: () => ({ isAppMode: { value: false } }),
}))

// Mock wide-screen state — drag-to-chat (draggable file name) is gated on it
const mockIsWideScreen = ref(true)
vi.mock('@/composables/useWideScreenLayout', () => ({
  getWideScreenState: () => ({ isWideScreen: mockIsWideScreen }),
}))

// Keep the real setAttachDragData (verifies the drag payload) but stub the
// ghost element builder/cleanup so tests don't touch the real DOM + timers.
vi.mock('@/utils/attachDrag', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/utils/attachDrag')>()
  return {
    ...actual,
    buildAttachDragImage: vi.fn(() => document.createElement('div')),
    cleanupDragGhost: vi.fn(),
  }
})

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
    ;(wrapper.vm as any).$.setupState.toggleMenu()
    await nextTick()
    expect(getMenuOpen(wrapper)).toBe(true)
  })

  it('closes menu on second dropdown button click', async () => {
    const wrapper = mountHeader({ viewMode: 'source' })
    ;(wrapper.vm as any).$.setupState.toggleMenu()
    await nextTick()
    expect(getMenuOpen(wrapper)).toBe(true)
    ;(wrapper.vm as any).$.setupState.toggleMenu()
    await nextTick()
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

  it('emits toggleStickyScroll when handler is called', async () => {
    const wrapper = mountHeader({ viewMode: 'source' })
    const vm = wrapper.vm as any
    vm.$.setupState.handleToggleStickyScroll()
    await nextTick()
    expect(wrapper.emitted('toggleStickyScroll')).toBeTruthy()
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

  describe('toggleView button', () => {
    it('renders an eye icon with the active class when the rendered preview is shown', () => {
      const wrapper = mountHeader({ file: { name: 'readme.md', path: '/tmp/readme.md', content: '# hi' }, viewMode: 'rendered', editing: false })
      const btn = wrapper.findAll('.header-actions .file-header-btn').find(b => b.attributes('title') === 'Source')
      expect(btn).toBeTruthy()
      expect(btn!.classes()).toContain('active')
    })

    it('renders an eye icon without the active class when the source view is shown', () => {
      const wrapper = mountHeader({ file: { name: 'readme.md', path: '/tmp/readme.md', content: '# hi' }, viewMode: 'source', editing: false })
      const btn = wrapper.findAll('.header-actions .file-header-btn').find(b => b.attributes('title') === 'Rendered')
      expect(btn).toBeTruthy()
      expect(btn!.classes()).not.toContain('active')
    })

    it('is disabled while editing', () => {
      const wrapper = mountHeader({ file: { name: 'readme.md', path: '/tmp/readme.md', content: '# hi' }, viewMode: 'source', editing: true })
      const btn = wrapper.findAll('.header-actions .file-header-btn').find(b => b.attributes('title') === 'Rendered')
      expect(btn).toBeTruthy()
      expect((btn!.element as HTMLButtonElement).disabled).toBe(true)
    })

    it('sits directly beside the edit button with no other buttons in between', () => {
      const wrapper = mountHeader({ file: { name: 'readme.md', path: '/tmp/readme.md', content: '# hi' }, viewMode: 'source', editing: false })
      const btns = wrapper.findAll('.header-actions .file-header-btn')
      const toggleIndex = btns.findIndex(b => b.attributes('title') === 'Rendered')
      const editIndex = btns.findIndex(b => b.attributes('title') === 'Edit')
      expect(toggleIndex).toBeGreaterThanOrEqual(0)
      expect(editIndex).toBe(toggleIndex + 1)
    })
  })

  describe('binary file openAsText button', () => {
    it('renders the button with a real Code2 SVG icon', () => {
      const wrapper = mountHeader({ file: { name: 'app.apk', path: '/tmp/app.apk', content: null, isBinary: true, size: 100 } })
      const btn = wrapper.findAll('.header-actions .file-header-btn').find(b => b.attributes('title') === 'Open as text')
      expect(btn).toBeTruthy()
      // Code2 must be imported: an unresolved <code2> custom element renders
      // with no SVG, leaving a visually empty button (blank gap in the toolbar)
      expect(btn!.find('svg').exists()).toBe(true)
      expect(btn!.find('code2').exists()).toBe(false)
    })

    it('is hidden for non-binary files', () => {
      const wrapper = mountHeader()
      const btn = wrapper.findAll('.header-actions .file-header-btn').find(b => b.attributes('title') === 'Open as text')
      expect(btn).toBeUndefined()
    })
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

  it('shows the spinning feedback when the shared refresh state is active', async () => {
    mockIsRefreshing.value = false
    let wrapper = mountHeader()
    let refreshBtn = wrapper.findAll('.header-actions .file-header-btn').find(b => b.attributes('title') === 'Refresh')
    expect(refreshBtn).toBeTruthy()
    expect(refreshBtn!.classes()).not.toContain('refresh-spin--active')

    // Click emits refresh
    const vm = wrapper.vm as any
    vm.$.setupState.handleRefresh()
    await nextTick()
    expect(wrapper.emitted('refresh')).toHaveLength(1)

    // Shared isRefreshing true → spin visible
    mockIsRefreshing.value = true
    wrapper.unmount()
    wrapper = mountHeader()
    refreshBtn = wrapper.findAll('.header-actions .file-header-btn').find(b => b.attributes('title') === 'Refresh')
    expect(refreshBtn!.classes()).toContain('refresh-spin--active')

    // Shared isRefreshing false → spin ends
    mockIsRefreshing.value = false
    wrapper.unmount()
    wrapper = mountHeader()
    refreshBtn = wrapper.findAll('.header-actions .file-header-btn').find(b => b.attributes('title') === 'Refresh')
    expect(refreshBtn!.classes()).not.toContain('refresh-spin--active')
  })

  it('emits showDetails when file name is clicked', async () => {
    const wrapper = mountHeader()
    const nameEl = wrapper.find('.file-path-hint')
    await nameEl.trigger('click')
    expect(wrapper.emitted('showDetails')).toBeTruthy()
  })

  describe('file details button', () => {
    it('renders as the last toolbar action (after delete)', () => {
      const wrapper = mountHeader()
      const btns = wrapper.findAll('.header-actions .file-header-btn')
      const detailsIdx = btns.findIndex(b => b.attributes('title') === 'Details')
      const deleteIdx = btns.findIndex(b => b.attributes('title') === 'Delete')
      expect(detailsIdx).toBeGreaterThanOrEqual(0)
      expect(deleteIdx).toBeGreaterThanOrEqual(0)
      expect(detailsIdx).toBeGreaterThan(deleteIdx)
    })

    it('emits showDetails when the details button is clicked', async () => {
      const wrapper = mountHeader()
      const btn = wrapper.findAll('.header-actions .file-header-btn').find(b => b.attributes('title') === 'Details')
      expect(btn).toBeTruthy()
      await btn!.trigger('click')
      expect(wrapper.emitted('showDetails')).toBeTruthy()
    })
  })

  describe('file name drag-to-chat', () => {
    it('is draggable on wide screens', () => {
      mockIsWideScreen.value = true
      const wrapper = mountHeader()
      const nameEl = wrapper.find('.file-path-hint')
      expect(nameEl.attributes('draggable')).toBe('true')
      expect(nameEl.classes()).toContain('file-path-draggable')
    })

    it('is not draggable on narrow screens (drag-to-chat requires split view)', () => {
      mockIsWideScreen.value = false
      const wrapper = mountHeader()
      const nameEl = wrapper.find('.file-path-hint')
      expect(nameEl.attributes('draggable')).toBe('false')
      expect(nameEl.classes()).not.toContain('file-path-draggable')
    })

    it('writes the attach drag payload with the file path on dragstart', async () => {
      mockIsWideScreen.value = true
      const wrapper = mountHeader()
      const dt = { setData: vi.fn(), setDragImage: vi.fn(), effectAllowed: '' }
      const nameEl = wrapper.find('.file-path-hint')
      await nameEl.trigger('dragstart', { dataTransfer: dt })
      expect(dt.setData).toHaveBeenCalledWith(
        'application/x-clawbench-attach',
        JSON.stringify({ path: '/tmp/main.ts', isDir: false }),
      )
      expect(dt.setData).toHaveBeenCalledWith('text/plain', '/tmp/main.ts')
      expect(dt.setDragImage).toHaveBeenCalled()
    })

    it('does not write attach data when the file has no path', async () => {
      mockIsWideScreen.value = true
      const wrapper = mountHeader({ file: { name: 'no-path.ts', path: '', content: '' } })
      const dt = { setData: vi.fn(), setDragImage: vi.fn(), effectAllowed: '' }
      const nameEl = wrapper.find('.file-path-hint')
      await nameEl.trigger('dragstart', { dataTransfer: dt })
      expect(dt.setData).not.toHaveBeenCalled()
    })

    it('does not write attach data when the file has no name', async () => {
      mockIsWideScreen.value = true
      const wrapper = mountHeader({ file: { name: '', path: '/tmp/no-name.ts' } })
      const dt = { setData: vi.fn(), setDragImage: vi.fn(), effectAllowed: '' }
      const nameEl = wrapper.find('.file-path-hint')
      await nameEl.trigger('dragstart', { dataTransfer: dt })
      expect(dt.setData).not.toHaveBeenCalled()
    })
  })

  it('emits toggleToc when toc button is clicked', async () => {
    const wrapper = mountHeader({ file: { name: 'main.ts', path: '/tmp/main.ts', content: 'code' } })
    // Locate the TOC button by its title (the button order varies with the
    // inline/collapsed toolbar split). Code files with content always show TOC.
    const tocBtn = wrapper.findAll('.header-actions .file-header-btn').find(b => b.attributes('title') === 'TOC')
    expect(tocBtn).toBeTruthy()
    await tocBtn!.trigger('click')
    expect(wrapper.emitted('toggleToc')).toBeTruthy()
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

  describe('media file filtering', () => {
    it('hides code-only toolbar items for image files', async () => {
      const wrapper = mountHeader({ file: { name: 'photo.png', path: '/tmp/photo.png', content: null } })
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
      const wrapper = mountHeader({ file: { name: 'song.mp3', path: '/tmp/song.mp3', content: null } })
      const vm = wrapper.vm as any
      expect(vm.$.setupState.isMediaFile).toBe(true)
      const ids = vm.$.setupState.toolbarInlineIds
      expect(ids).not.toContain('wordWrap')
      expect(ids).not.toContain('lineNumbers')
    })

    it('hides code-only toolbar items for video files', async () => {
      const wrapper = mountHeader({ file: { name: 'clip.mp4', path: '/tmp/clip.mp4', content: null } })
      const vm = wrapper.vm as any
      expect(vm.$.setupState.isMediaFile).toBe(true)
      const ids = vm.$.setupState.toolbarInlineIds
      expect(ids).not.toContain('wordWrap')
      expect(ids).not.toContain('lineNumbers')
    })

    it('hides code-only toolbar items for PDF files', async () => {
      const wrapper = mountHeader({ file: { name: 'doc.pdf', path: '/tmp/doc.pdf', content: null } })
      const vm = wrapper.vm as any
      expect(vm.$.setupState.isMediaFile).toBe(true)
      const ids = vm.$.setupState.toolbarInlineIds
      expect(ids).not.toContain('wordWrap')
      expect(ids).not.toContain('lineNumbers')
      expect(ids).not.toContain('toggleView')
      // PDF keeps TOC but has no search (no text content)
      expect(ids).toContain('toc')
      expect(ids).not.toContain('search')
    })

    it('shows code toolbar items for text files', async () => {
      const wrapper = mountHeader({ file: { name: 'main.ts', path: '/tmp/main.ts', content: 'code' } })
      const vm = wrapper.vm as any
      expect(vm.$.setupState.isMediaFile).toBe(false)
      const ids = vm.$.setupState.toolbarInlineIds
      expect(ids).toContain('wordWrap')
      expect(ids).toContain('lineNumbers')
    })
  })

  describe('edit button', () => {
    it('shows edit button for editable text file', () => {
      const wrapper = mountHeader()
      const vm = wrapper.vm as any
      expect(vm.$.setupState.isEditable).toBe(true)
      expect(vm.$.setupState.toolbarInlineIds).toContain('edit')
    })

    it('shows edit button for markdown files', () => {
      const wrapper = mountHeader({ file: { name: 'readme.md', path: '/tmp/readme.md', content: '# hi' } })
      const vm = wrapper.vm as any
      expect(vm.$.setupState.isEditable).toBe(true)
      expect(vm.$.setupState.toolbarInlineIds).toContain('edit')
    })

    it('shows edit button and code features for a newly-created empty text file', () => {
      const wrapper = mountHeader({ file: { name: 'newfile.ts', path: '/tmp/newfile.ts', content: '' } })
      const vm = wrapper.vm as any
      expect(vm.$.setupState.hasTextContent).toBe(true)
      expect(vm.$.setupState.isEditable).toBe(true)
      const ids = vm.$.setupState.toolbarInlineIds
      expect(ids).toContain('edit')
      expect(ids).toContain('wordWrap')
      expect(ids).toContain('lineNumbers')
      expect(ids).toContain('refresh')
    })

    it('emits toggleEdit when edit button is clicked', async () => {
      const wrapper = mountHeader()
      const vm = wrapper.vm as any
      vm.$.setupState.handleToggleEdit()
      await nextTick()
      expect(wrapper.emitted('toggleEdit')).toBeTruthy()
    })

    it('applies active class on edit button when editing', async () => {
      const wrapper = mountHeader({ editing: true })
      const activeBtn = wrapper.findAll('.header-actions .file-header-btn').find(b => b.classes().includes('active'))
      expect(activeBtn).toBeTruthy()
    })
  })

  describe('effectiveViewMode', () => {
    it('returns raw for source view on non-markdown file', () => {
      const wrapper = mountHeader({ viewMode: 'source' })
      const vm = wrapper.vm as any
      expect(vm.$.setupState.effectiveViewMode).toBe('raw')
    })

    it('returns rendered for markdown in rendered view without editing', () => {
      const wrapper = mountHeader({ file: { name: 'readme.md', path: '/tmp/readme.md', content: '# hi' }, viewMode: 'rendered', editing: false })
      const vm = wrapper.vm as any
      expect(vm.$.setupState.effectiveViewMode).toBe('rendered')
    })

    it('returns raw when editing from rendered markdown preview', () => {
      const wrapper = mountHeader({ file: { name: 'readme.md', path: '/tmp/readme.md', content: '# hi' }, viewMode: 'rendered', editing: true })
      const vm = wrapper.vm as any
      expect(vm.$.setupState.effectiveViewMode).toBe('raw')
    })

    it('hides export HTML button when editing from rendered markdown', () => {
      const wrapper = mountHeader({ file: { name: 'readme.md', path: '/tmp/readme.md', content: '# hi' }, viewMode: 'rendered', editing: true })
      const vm = wrapper.vm as any
      // When editing from rendered, effectiveViewMode is 'raw', so exportHtml should not be in toolbar
      const ids = vm.$.setupState.toolbarInlineIds
      expect(ids).not.toContain('exportHtml')
    })

    it('shows the search button for any file with text content', async () => {
      // Rendered markdown preview keeps the SearchDrawer button
      const rendered = mountHeader({ file: { name: 'readme.md', path: '/tmp/readme.md', content: '# hi' }, viewMode: 'rendered', editing: false })
      expect((rendered.vm as any).$.setupState.hasSearch).toBe(true)
      expect((rendered.vm as any).$.setupState.toolbarInlineIds).toContain('search')

      // Editing from rendered markdown → CodeMirror source → button still shown
      // (clicking opens CodeMirror's own search panel)
      const editing = mountHeader({ file: { name: 'readme.md', path: '/tmp/readme.md', content: '# hi' }, viewMode: 'rendered', editing: true })
      expect((editing.vm as any).$.setupState.hasSearch).toBe(true)

      // Source view of markdown → CodeMirror raw → button still shown
      const source = mountHeader({ file: { name: 'readme.md', path: '/tmp/readme.md', content: '# hi' }, viewMode: 'source', editing: false })
      expect((source.vm as any).$.setupState.hasSearch).toBe(true)

      // Plain code file → CodeMirror → button still shown
      const code = mountHeader({ file: { name: 'main.ts', path: '/tmp/main.ts', content: 'const x = 1' }, viewMode: 'source' })
      expect((code.vm as any).$.setupState.hasSearch).toBe(true)
    })

    it('hides the search button for media files without text content', () => {
      const wrapper = mountHeader({ file: { name: 'photo.png', path: '/tmp/photo.png', content: null } })
      expect((wrapper.vm as any).$.setupState.hasSearch).toBe(false)
    })
  })

  describe('share link button', () => {
    it('includes shareLink in the toolbar for a regular file', () => {
      const wrapper = mountHeader({ file: { name: 'readme.md', path: '/tmp/readme.md', content: '# hi' }, viewMode: 'rendered', editing: false })
      const ids = (wrapper.vm as any).$.setupState.toolbarInlineIds
      expect(ids).toContain('shareLink')
    })

    it('hides shareLink while editing', () => {
      const wrapper = mountHeader({ file: { name: 'readme.md', path: '/tmp/readme.md', content: '# hi' }, viewMode: 'rendered', editing: true })
      const ids = (wrapper.vm as any).$.setupState.toolbarInlineIds
      expect(ids).not.toContain('shareLink')
    })

    it('emits shareLink when the share link button is clicked', async () => {
      const wrapper = mountHeader({ file: { name: 'readme.md', path: '/tmp/readme.md', content: '# hi' }, viewMode: 'rendered', editing: false })
      const button = wrapper.findAll('.header-actions .file-header-btn').find(b => b.attributes('title') === 'Share link')
      expect(button).toBeTruthy()
      await button!.trigger('click')
      expect(wrapper.emitted('shareLink')).toBeTruthy()
    })

    it('shows the active state when the file is shared', async () => {
      markShared('/tmp/readme.md')
      const wrapper = mountHeader({ file: { name: 'readme.md', path: '/tmp/readme.md', content: '# hi' }, viewMode: 'rendered', editing: false })
      const button = wrapper.findAll('.header-actions .file-header-btn').find(b => b.attributes('title') === 'Share link (active)')
      expect(button).toBeTruthy()
      expect(button!.classes()).toContain('active')
      expect((wrapper.vm as any).$.setupState.isShared).toBe(true)
    })

    it('clears the active state after the share is revoked', async () => {
      markShared('/tmp/readme.md')
      const wrapper = mountHeader({ file: { name: 'readme.md', path: '/tmp/readme.md', content: '# hi' }, viewMode: 'rendered', editing: false })
      expect((wrapper.vm as any).$.setupState.isShared).toBe(true)

      markUnshared('/tmp/readme.md')
      await nextTick()
      expect((wrapper.vm as any).$.setupState.isShared).toBe(false)
    })

    it('is not active for an unshared file', () => {
      const wrapper = mountHeader({ file: { name: 'readme.md', path: '/tmp/readme.md', content: '# hi' }, viewMode: 'rendered', editing: false })
      const button = wrapper.findAll('.header-actions .file-header-btn').find(b => b.attributes('title') === 'Share link')
      expect(button).toBeTruthy()
      expect(button!.classes()).not.toContain('active')
      expect((wrapper.vm as any).$.setupState.isShared).toBe(false)
    })
  })

  describe('set as background', () => {
    it('shows the set-as-background button for supported image files', () => {
      const wrapper = mountHeader({ file: { name: 'photo.png', path: '/tmp/photo.png', content: null, isImage: true } })
      const button = wrapper.findAll('.header-actions .file-header-btn').find(b => b.attributes('title') === 'Set as theme background')
      expect(button).toBeTruthy()
      expect((wrapper.vm as any).$.setupState.isWallpaperSource).toBe(true)
    })

    it('does not show the set-as-background button for non-image files', () => {
      const wrapper = mountHeader({ file: { name: 'main.ts', path: '/tmp/main.ts', content: 'const x = 1' } })
      const button = wrapper.findAll('.header-actions .file-header-btn').find(b => b.attributes('title') === 'Set as theme background')
      expect(button).toBeUndefined()
      expect((wrapper.vm as any).$.setupState.isWallpaperSource).toBe(false)
    })

    it('excludes unsupported image extensions like bmp/tiff', () => {
      const wrapper = mountHeader({ file: { name: 'scan.bmp', path: '/tmp/scan.bmp', content: null, isImage: true } })
      expect((wrapper.vm as any).$.setupState.isWallpaperSource).toBe(false)
    })

    it('emits setAsBackground with the file path when clicked', async () => {
      const wrapper = mountHeader({ file: { name: 'photo.png', path: '/tmp/photo.png', content: null, isImage: true } })
      const button = wrapper.findAll('.header-actions .file-header-btn').find(b => b.attributes('title') === 'Set as theme background')
      expect(button).toBeTruthy()
      await button!.trigger('click')
      const emitted = wrapper.emitted('setAsBackground')
      expect(emitted).toBeTruthy()
      expect(emitted![0]).toEqual(['/tmp/photo.png'])
    })
  })

  describe('wide-screen back navigation', () => {
    beforeEach(() => {
      mockIsWideScreen.value = true
    })

    it('renders the top-left back button with backLabel when canNavigateBack is true', () => {
      const wrapper = mountHeader({ canNavigateBack: true, backLabel: 'Back to Chat' })
      const btn = wrapper.find('.file-header-back-btn')
      expect(btn.exists()).toBe(true)
      expect(btn.attributes('title')).toBe('Back to Chat')
      expect(btn.attributes('aria-label')).toBe('Back to Chat')
    })

    it('falls back to the generic back label for in-file history back', () => {
      const wrapper = mountHeader({ canNavigateBack: false, canGoBackFile: true })
      const btn = wrapper.find('.file-header-back-btn')
      expect(btn.exists()).toBe(true)
      expect(btn.attributes('title')).toBe('Back')
    })

    it('emits navigateBack when the back button is clicked', async () => {
      const wrapper = mountHeader({ canNavigateBack: true })
      await wrapper.get('.file-header-back-btn').trigger('click')
      expect(wrapper.emitted('navigateBack')).toHaveLength(1)
    })

    it('renders the forward button only when in-file history can go forward', async () => {
      const wrapper = mountHeader({ canNavigateBack: true, canGoForwardFile: true })
      const buttons = wrapper.findAll('.file-header-nav .file-header-btn')
      expect(buttons).toHaveLength(2)
      expect(buttons[0].find('svg.lucide-arrow-left').exists()).toBe(true)
      expect(buttons[1].find('svg.lucide-arrow-right').exists()).toBe(true)
      await buttons[1].trigger('click')
      expect(wrapper.emitted('navigateForward')).toHaveLength(1)
    })

    it('hides the whole nav cluster when no back/forward is possible', () => {
      const wrapper = mountHeader({ canNavigateBack: false })
      expect(wrapper.find('.file-header-nav').exists()).toBe(false)
    })

    it('hides the nav cluster on narrow (touch) layouts, which use the floating bar', () => {
      mockIsWideScreen.value = false
      const wrapper = mountHeader({ canNavigateBack: true })
      expect(wrapper.find('.file-header-nav').exists()).toBe(false)
      expect(wrapper.find('.file-header-back-btn').exists()).toBe(false)
    })
  })
})
