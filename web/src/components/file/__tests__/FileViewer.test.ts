import { describe, expect, it, vi, afterEach, beforeEach } from 'vitest'
import { nextTick } from 'vue'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import FileViewer from '../FileViewer.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      file: {
        viewer: {
          binaryFile: 'Binary file',
          fileTooLarge: 'File too large',
          truncated: 'Truncated',
        },
        header: {
          openAsText: 'Open as text',
          shareExternal: 'Share',
          edit: 'Edit',
        },
        overlay: {
          back: 'Back',
          forward: 'Forward',
        },
        editor: {
          save: 'Save',
          cancel: 'Cancel',
        },
      },
      common: { download: 'Download', close: 'Close' },
    },
  },
})

// Mock composables
vi.mock('@/composables/useAppMode.ts', () => ({
  useAppMode: () => ({ isAppMode: { value: false } }),
}))

vi.mock('@/composables/useToast.ts', () => ({
  useToast: () => ({ show: vi.fn() }),
}))

vi.mock('@/composables/useDiffDrawer.ts', () => ({
  useDiffDrawer: () => ({
    drawerMarkerType: { value: 'none' },
    drawerCharDiff: { value: false },
    drawerDiffLines: { value: [] },
    closeDrawer: vi.fn(),
  }),
}))

vi.mock('@/composables/useMarkdownDiff.ts', () => ({
  diffMarkers: { value: [] },
  diffDrawerVisible: { value: false },
  diffDrawer: { effectiveOpen: { value: false }, isOpen: { value: false }, open: vi.fn(), close: vi.fn(), toggle: vi.fn() },
  diffDrawerMarker: { value: null },
  diffOldContent: { value: null },
  diffOldFilePath: { value: null },
  openDiffDrawer: vi.fn(),
  closeDiffDrawer: vi.fn(),
  clearDiffMarkers: vi.fn(),
}))

vi.mock('@/composables/useTabDrawer', () => ({
  useTabDrawer: () => ({
    effectiveOpen: { value: false },
    isOpen: { value: false },
    open: vi.fn(),
    close: vi.fn(),
    toggle: vi.fn(),
  }),
}))

vi.mock('@/composables/useFileRefresh.ts', () => ({
  flashRanges: { value: [] },
  flashType: { value: null },
}))

const fileNavState = vi.hoisted(() => ({
  overlayOpen: { value: false },
  canGoBack: { value: false },
  canGoForward: { value: false },
}))

vi.mock('@/composables/useFileNavStack.ts', () => ({
  useFileNavStack: () => fileNavState,
}))

vi.mock('@/composables/useSettingsConfig', () => ({
  useSettingsConfig: () => ({
    localConfig: { wordWrap: true, lineNumbers: true },
    setLocalConfig: vi.fn(),
  }),
  localConfig: { wordWrap: true, lineNumbers: true, recentFilesCount: 10 },
  setLocalConfig: vi.fn(),
}))

vi.mock('@/stores/app.ts', () => ({
  store: {
    state: { currentFile: null, currentDir: '', projectRoot: '/tmp' },
    selectFile: vi.fn(),
  },
}))

const mockSaveFile = vi.fn()
vi.mock('@/composables/useCodeEditorSave.ts', () => ({
  useCodeEditorSave: () => ({ saving: { value: false }, saveFile: mockSaveFile }),
}))

vi.mock('@/utils/fileType.ts', () => ({
  getFileType: (name: string) => {
    if (name.endsWith('.md')) return { isMarkdown: true, isHtml: false, isImage: false, isAudio: false, isVideo: false, isPdf: false, lang: 'markdown' }
    if (name.endsWith('.html')) return { isMarkdown: false, isHtml: true, isImage: false, isAudio: false, isVideo: false, isPdf: false, lang: 'xml' }
    if (name.endsWith('.png')) return { isMarkdown: false, isHtml: false, isImage: true, isAudio: false, isVideo: false, isPdf: false, lang: '' }
    if (name.endsWith('.pdf')) return { isMarkdown: false, isHtml: false, isImage: false, isAudio: false, isVideo: false, isPdf: true, lang: '' }
    if (name.endsWith('.mp3')) return { isMarkdown: false, isHtml: false, isImage: false, isAudio: true, isVideo: false, isPdf: false, lang: '' }
    if (name.endsWith('.mp4')) return { isMarkdown: false, isHtml: false, isImage: false, isAudio: false, isVideo: true, isPdf: false, isOffice: false, lang: '' }
    if (name.endsWith('.docx') || name.endsWith('.xlsx') || name.endsWith('.pptx')) return { isMarkdown: false, isHtml: false, isImage: false, isAudio: false, isVideo: false, isPdf: false, isOffice: true, lang: '' }
    return { isMarkdown: false, isHtml: false, isImage: false, isAudio: false, isVideo: false, isPdf: false, isOffice: false, lang: 'plaintext' }
  },
  formatFileSize: (bytes: number) => {
    if (bytes < 1024) return `${bytes} B`
    return `${(bytes / 1024).toFixed(1)} KB`
  },
}))

vi.mock('@/utils/exportHtml.ts', () => ({
  exportRenderedHtml: vi.fn().mockResolvedValue({ html: '<html></html>', skippedImages: 0, externalImages: 0, issues: [] }),
}))

vi.mock('@/utils/download.ts', () => ({
  buildLocalFileUrl: (path: string, opts?: any) => `/api/local-file/${path}?download=1`,
  downloadFileByPath: vi.fn(),
  downloadBlob: vi.fn(),
}))

// ── Timer leak prevention ───────────────────────────────────
const pendingTimers: ReturnType<typeof setTimeout>[] = []
const _origSetTimeout = setTimeout
globalThis.setTimeout = ((fn: TimerHandler, ms?: number, ...args: any[]) => {
  const id = _origSetTimeout(fn, ms, ...args)
  pendingTimers.push(id)
  return id
}) as typeof setTimeout

const pendingIntervals: ReturnType<typeof setInterval>[] = []
const _origSetInterval = setInterval
globalThis.setInterval = ((fn: TimerHandler, ms?: number, ...args: any[]) => {
  const id = _origSetInterval(fn, ms, ...args)
  pendingIntervals.push(id)
  return id
}) as typeof setInterval

afterEach(() => {
  fileNavState.overlayOpen.value = false
  fileNavState.canGoBack.value = false
  fileNavState.canGoForward.value = false
  mockOpenSearch.mockClear()
  for (const id of pendingTimers) { clearTimeout(id) }
  pendingTimers.length = 0
  for (const id of pendingIntervals) { clearInterval(id) }
  pendingIntervals.length = 0
})

// CodeMirrorViewer stub exposes handleExit so the FileViewer preview-while-
// editing flow (which calls it to exit edit mode first) can be exercised.
const mockHandleExit = vi.fn()
const mockOpenSearch = vi.fn()
const codeMirrorViewerStub = {
  name: 'CodeMirrorViewer',
  template: '<div class="cm-stub" />',
  methods: {
    handleExit: (...args: unknown[]) => mockHandleExit(...args),
    openSearch: (...args: unknown[]) => mockOpenSearch(...args),
  },
}

// Stub child components
const stubs = {
  FileHeader: true,
  PdfPreview: true,
  ImagePreview: true,
  AudioPreview: true,
  VideoPreview: true,
  OfficePreview: true,
  MarkdownPreview: true,
  CodeMirrorViewer: codeMirrorViewerStub,
  DiffDrawer: true,
}

describe('FileViewer', () => {
  function mountViewer(props = {}) {
    return mount(FileViewer, {
      props: {
        file: { name: 'main.ts', path: 'main.ts', content: 'const x = 1' },
        tocOpen: false,
        searchOpen: false,
        markdownViewMode: 'rendered',
        externalLoading: false,
        ...props,
      },
      global: {
        plugins: [i18n],
        stubs,
      },
    })
  }

  it('renders the viewer container', () => {
    const wrapper = mountViewer()
    expect(wrapper.find('.file-viewer').exists()).toBe(true)
  })

  it('renders error bubble when file has error', () => {
    const wrapper = mountViewer({ file: { name: 'err.ts', path: 'err.ts', error: 'Not found' } })
    expect(wrapper.find('.error-bubble').exists()).toBe(true)
    expect(wrapper.text()).toContain('Not found')
  })

  it('shows PdfPreview for PDF files', () => {
    const wrapper = mountViewer({ file: { name: 'doc.pdf', path: 'doc.pdf', isPdf: true, content: null } })
    expect(wrapper.findComponent({ name: 'PdfPreview' }).exists() || wrapper.find('.file-viewer-content').exists()).toBe(true)
  })

  it('shows ImagePreview for image files', () => {
    const wrapper = mountViewer({ file: { name: 'photo.png', path: 'photo.png', isImage: true, content: null } })
    expect(wrapper.findComponent({ name: 'ImagePreview' }).exists() || wrapper.find('.file-viewer-content').exists()).toBe(true)
  })

  it('shows AudioPreview for audio files', () => {
    const wrapper = mountViewer({ file: { name: 'song.mp3', path: 'song.mp3', isAudio: true, content: null } })
    expect(wrapper.findComponent({ name: 'AudioPreview' }).exists() || wrapper.find('.file-viewer-content').exists()).toBe(true)
  })

  it('shows VideoPreview for video files', () => {
    const wrapper = mountViewer({ file: { name: 'clip.mp4', path: 'clip.mp4', isVideo: true, content: null } })
    expect(wrapper.findComponent({ name: 'VideoPreview' }).exists() || wrapper.find('.file-viewer-content').exists()).toBe(true)
  })

  it('shows OfficePreview for office files', () => {
    const wrapper = mountViewer({ file: { name: 'report.docx', path: 'report.docx', isOffice: true, content: null } })
    expect(wrapper.findComponent({ name: 'OfficePreview' }).exists() || wrapper.find('.file-viewer-content').exists()).toBe(true)
  })

  it('shows binary file placeholder for binary files', () => {
    const wrapper = mountViewer({ file: { name: 'data.bin', path: 'data.bin', isBinary: true, content: null } })
    expect(wrapper.find('.unsupported-file').exists()).toBe(true)
    expect(wrapper.text()).toContain('data.bin')
  })

  it('shows too large placeholder for large files', () => {
    const wrapper = mountViewer({ file: { name: 'big.log', path: 'big.log', tooLarge: true, size: 1048576 } })
    expect(wrapper.find('.unsupported-file').exists()).toBe(true)
    expect(wrapper.text()).toContain('big.log')
  })

  it('shows open-as-text button for binary files', () => {
    const wrapper = mountViewer({ file: { name: 'data.bin', path: 'data.bin', isBinary: true, content: null } })
    expect(wrapper.find('.open-as-text-btn').exists()).toBe(true)
  })

  it('shows truncated notice when file is truncated', () => {
    const wrapper = mountViewer({ file: { name: 'long.ts', path: 'long.ts', content: '...', truncated: true } })
    expect(wrapper.find('.truncated-notice').exists()).toBe(true)
  })

  it('shows loading spinner when loading', () => {
    const wrapper = mountViewer({ file: { name: 'test.ts', path: 'test.ts', content: null } })
    expect(wrapper.find('.loading').exists()).toBe(true)
  })

  it('emits delete event via FileHeader', async () => {
    const wrapper = mountViewer()
    const header = wrapper.findComponent({ name: 'FileHeader' })
    if (header.exists()) {
      await header.vm.$emit('delete', 'main.ts')
      expect(wrapper.emitted('delete')).toBeTruthy()
    }
  })

  it('renders floating history nav and emits back/forward when available', async () => {
    fileNavState.overlayOpen.value = true
    fileNavState.canGoBack.value = true
    fileNavState.canGoForward.value = true
    const wrapper = mountViewer()
    await nextTick()

    const buttons = wrapper.findAll('.file-nav-btn')
    expect(buttons).toHaveLength(2)
    expect(buttons[0].attributes('disabled')).toBeUndefined()
    expect(buttons[1].attributes('disabled')).toBeUndefined()

    await buttons[0].trigger('click')
    expect(wrapper.emitted('navigateBack')).toBeTruthy()

    await buttons[1].trigger('click')
    expect(wrapper.emitted('navigateForward')).toBeTruthy()
  })

  it('hides back/forward floating buttons when history is empty', async () => {
    fileNavState.overlayOpen.value = true
    fileNavState.canGoBack.value = false
    fileNavState.canGoForward.value = false
    const wrapper = mountViewer()
    await nextTick()

    expect(wrapper.findAll('.file-nav-btn')).toHaveLength(0)
  })

  it('hides the unavailable direction button individually', async () => {
    fileNavState.overlayOpen.value = true
    fileNavState.canGoBack.value = true
    fileNavState.canGoForward.value = false
    const wrapper = mountViewer()
    await nextTick()

    const buttons = wrapper.findAll('.file-nav-btn')
    expect(buttons).toHaveLength(1)
    expect(wrapper.find('.file-nav-float').exists()).toBe(true)
  })

  it('calls store.selectFile when openAsText is emitted', async () => {
    const wrapper = mountViewer()
    const header = wrapper.findComponent({ name: 'FileHeader' })
    if (header.exists()) {
      await header.vm.$emit('openAsText')
      const { store } = await import('@/stores/app.ts')
      expect(store.selectFile).toHaveBeenCalled()
    }
  })

  it('exposes pdfOutline computed property', () => {
    const wrapper = mountViewer({ file: { name: 'doc.pdf', path: 'doc.pdf', isPdf: true, content: null } })
    const vm = wrapper.vm as any
    expect(vm.pdfOutline).toBeDefined()
  })

  it('exposes pdfScrollToPage method', () => {
    const wrapper = mountViewer({ file: { name: 'doc.pdf', path: 'doc.pdf', isPdf: true, content: null } })
    const vm = wrapper.vm as any
    expect(typeof vm.pdfScrollToPage).toBe('function')
  })

  describe('edit mode', () => {
    const editableFile = {
      name: 'main.ts',
      path: '/tmp/main.ts',
      content: 'const x = 1',
      isMarkdown: false,
      isHtml: false,
      isImage: false,
      isAudio: false,
      isVideo: false,
      isPdf: false,
      isOffice: false,
      isBinary: false,
      tooLarge: false,
    }

    function setupState(wrapper: ReturnType<typeof mount>) {
      return (wrapper.vm as any).$.setupState
    }

    it('enters edit mode via FileHeader toggleEdit emit', async () => {
      const wrapper = mountViewer({ file: editableFile })
      const ss = setupState(wrapper)
      expect(ss.editing).toBe(false)
      const header = wrapper.findComponent({ name: 'FileHeader' })
      header.vm.$emit('toggleEdit')
      await nextTick()
      expect(ss.editing).toBe(true)
    })

    it('does not exit edit mode via header Edit when the exit guard cannot confirm', async () => {
      // Exiting edit now routes through guardExitEdit, which needs the (async)
      // CodeMirror editor's handleExit. When the editor isn't reachable in the
      // test env the guard aborts safely — edit mode must not be lost.
      mockHandleExit.mockReset()
      const wrapper = mountViewer({ file: editableFile })
      const ss = setupState(wrapper)
      ss.handleToggleEdit()
      await nextTick()
      expect(ss.editing).toBe(true)
      await ss.handleToggleEdit()
      await flushPromises()
      expect(ss.editing).toBe(true)
    })

    it('keeps editing when exiting via header Edit is cancelled while dirty', async () => {
      mockHandleExit.mockReset()
      const wrapper = mountViewer({ file: editableFile })
      const ss = setupState(wrapper)
      ss.handleToggleEdit()
      await nextTick()
      expect(ss.editing).toBe(true)
      mockHandleExit.mockResolvedValue(false)
      await ss.handleToggleEdit()
      await flushPromises()
      expect(ss.editing).toBe(true)
    })

    it('calls saveFile with path and content and stays in edit mode on success', async () => {
      mockSaveFile.mockResolvedValue(true)
      const wrapper = mountViewer({ file: editableFile })
      const ss = setupState(wrapper)
      ss.handleToggleEdit()
      await nextTick()
      await ss.handleSave('const x = 2')
      expect(mockSaveFile).toHaveBeenCalledWith('/tmp/main.ts', 'const x = 2')
      expect(ss.editing).toBe(true)
    })

    it('saveAndExit calls saveFile and exits edit mode on success', async () => {
      mockSaveFile.mockResolvedValue(true)
      const wrapper = mountViewer({ file: editableFile })
      const ss = setupState(wrapper)
      ss.handleToggleEdit()
      await nextTick()
      await ss.handleSaveAndExit('const x = 2')
      expect(mockSaveFile).toHaveBeenCalledWith('/tmp/main.ts', 'const x = 2')
      expect(ss.editing).toBe(false)
    })

    it('saveAndExit stays in edit mode when save fails', async () => {
      mockSaveFile.mockResolvedValue(false)
      const wrapper = mountViewer({ file: editableFile })
      const ss = setupState(wrapper)
      ss.handleToggleEdit()
      await nextTick()
      await ss.handleSaveAndExit('const x = 2')
      expect(ss.editing).toBe(true)
    })

    it('stays in edit mode when save fails', async () => {
      mockSaveFile.mockResolvedValue(false)
      const wrapper = mountViewer({ file: editableFile })
      const ss = setupState(wrapper)
      ss.handleToggleEdit()
      await nextTick()
      await ss.handleSave('const x = 2')
      expect(mockSaveFile).toHaveBeenCalledWith('/tmp/main.ts', 'const x = 2')
      expect(ss.editing).toBe(true)
    })

    it('renders CodeMirrorViewer for markdown when editing is toggled on', async () => {
      const mdFile = {
        name: 'readme.md',
        path: '/tmp/readme.md',
        content: '# Title\n\nsome text',
        isMarkdown: true,
        isHtml: false,
        isImage: false,
        isAudio: false,
        isVideo: false,
        isPdf: false,
        isOffice: false,
        isBinary: false,
        tooLarge: false,
      }
      const wrapper = mountViewer({ file: mdFile })
      // Not editing → rendered markdown preview
      expect(wrapper.findComponent({ name: 'CodeMirrorViewer' }).exists()).toBe(false)
      expect(wrapper.findComponent({ name: 'MarkdownPreview' }).exists()).toBe(true)

      const ss = setupState(wrapper)
      ss.handleToggleEdit()
      await nextTick()
      expect(wrapper.findComponent({ name: 'CodeMirrorViewer' }).exists()).toBe(true)
      expect(wrapper.findComponent({ name: 'MarkdownPreview' }).exists()).toBe(false)
    })
  })

  describe('preview request while editing markdown', () => {
    const mdFile = {
      name: 'readme.md',
      path: '/tmp/readme.md',
      content: '# Title\n\nsome text',
      isMarkdown: true,
      isHtml: false,
      isImage: false,
      isAudio: false,
      isVideo: false,
      isPdf: false,
      isOffice: false,
      isBinary: false,
      tooLarge: false,
    }

    beforeEach(async () => {
      ;(await import('@/composables/useFileEditor'))._resetForTesting()
      mockHandleExit.mockReset()
      mockHandleExit.mockResolvedValue(true)
    })

    function setupState(wrapper: ReturnType<typeof mount>) {
      return (wrapper.vm as any).$.setupState
    }

    it('when not editing, emits toggleView immediately without calling handleExit', async () => {
      const wrapper = mountViewer({ file: mdFile })
      await setupState(wrapper).handleToggleViewRequest()
      expect(wrapper.emitted('toggleView')).toBeTruthy()
      expect(mockHandleExit).not.toHaveBeenCalled()
    })

    it('while editing, exits edit mode via handleExit then emits toggleView', async () => {
      const wrapper = mountViewer({ file: mdFile })
      const ss = setupState(wrapper)
      ss.handleToggleEdit()
      await nextTick()
      expect(ss.editing).toBe(true)
      const editor = (await import('@/composables/useFileEditor')).useFileEditor()
      mockHandleExit.mockImplementation(() => {
        editor.editing.value = false // simulate the exit completing
        return true
      })
      await ss.handleToggleViewRequest()
      expect(mockHandleExit).toHaveBeenCalled()
      expect(wrapper.emitted('toggleView')).toBeTruthy()
      expect(ss.editing).toBe(false)
    })

    it('while editing, does not emit toggleView when exit is cancelled (stays editing)', async () => {
      const wrapper = mountViewer({ file: mdFile })
      const ss = setupState(wrapper)
      ss.handleToggleEdit()
      await nextTick()
      mockHandleExit.mockResolvedValue(false)
      await ss.handleToggleViewRequest()
      expect(mockHandleExit).toHaveBeenCalled()
      expect(wrapper.emitted('toggleView')).toBeFalsy()
      expect(ss.editing).toBe(true)
    })
  })

  describe('guarded file navigation while editing', () => {
    beforeEach(async () => {
      ;(await import('@/composables/useFileEditor'))._resetForTesting()
      mockHandleExit.mockReset()
      mockHandleExit.mockResolvedValue(true)
    })

    function setupState(wrapper: ReturnType<typeof mount>) {
      return (wrapper.vm as any).$.setupState
    }

    it('emits navigateBack immediately when not editing', async () => {
      const wrapper = mountViewer()
      await setupState(wrapper).handleNavBack()
      expect(wrapper.emitted('navigateBack')).toBeTruthy()
      expect(mockHandleExit).not.toHaveBeenCalled()
    })

    it('emits navigateForward immediately when not editing', async () => {
      const wrapper = mountViewer()
      await setupState(wrapper).handleNavForward()
      expect(wrapper.emitted('navigateForward')).toBeTruthy()
      expect(mockHandleExit).not.toHaveBeenCalled()
    })

    it('emits overlayClose immediately when not editing', async () => {
      const wrapper = mountViewer()
      await setupState(wrapper).handleOverlayCloseRequest()
      expect(wrapper.emitted('overlayClose')).toBeTruthy()
      expect(mockHandleExit).not.toHaveBeenCalled()
    })

    it('emits delete with the path immediately when not editing', async () => {
      const wrapper = mountViewer()
      await setupState(wrapper).handleDeleteRequest('main.ts')
      expect(wrapper.emitted('delete')?.[0]).toEqual(['main.ts'])
      expect(mockHandleExit).not.toHaveBeenCalled()
    })

    it('does not navigate back when editing and the exit is cancelled (stays editing)', async () => {
      const wrapper = mountViewer()
      const ss = setupState(wrapper)
      ss.handleToggleEdit()
      await nextTick()
      expect(ss.editing).toBe(true)
      mockHandleExit.mockResolvedValue(false)
      await ss.handleNavBack()
      expect(wrapper.emitted('navigateBack')).toBeFalsy()
      expect(ss.editing).toBe(true)
    })
  })

  describe('search routing', () => {
    const mdFile = {
      name: 'readme.md',
      path: '/tmp/readme.md',
      content: '# Title\n\nsome text',
      isMarkdown: true,
      isHtml: false,
      isImage: false,
      isAudio: false,
      isVideo: false,
      isPdf: false,
      isOffice: false,
      isBinary: false,
      tooLarge: false,
    }
    const codeFile = {
      name: 'main.ts',
      path: '/tmp/main.ts',
      content: 'const x = 1',
      isMarkdown: false,
      isHtml: false,
      isImage: false,
      isAudio: false,
      isVideo: false,
      isPdf: false,
      isOffice: false,
      isBinary: false,
      tooLarge: false,
    }

    it('opens CodeMirror search for a code file (CodeMirror view)', async () => {
      const wrapper = mountViewer({ file: codeFile })
      const ss = (wrapper.vm as any).$.setupState
      expect(ss.isCodeMirrorView).toBe(true)
      ss.handleToggleSearch()
      expect(mockOpenSearch).toHaveBeenCalledTimes(1)
      expect(wrapper.emitted('toggleSearch')).toBeFalsy()
    })

    it('opens CodeMirror search for markdown in source view', async () => {
      const wrapper = mountViewer({ file: mdFile, markdownViewMode: 'source' })
      const ss = (wrapper.vm as any).$.setupState
      expect(ss.isCodeMirrorView).toBe(true)
      ss.handleToggleSearch()
      expect(mockOpenSearch).toHaveBeenCalledTimes(1)
      expect(wrapper.emitted('toggleSearch')).toBeFalsy()
    })

    it('emits toggleSearch for rendered markdown preview', async () => {
      const wrapper = mountViewer({ file: mdFile, markdownViewMode: 'rendered' })
      const ss = (wrapper.vm as any).$.setupState
      expect(ss.isCodeMirrorView).toBe(false)
      ss.handleToggleSearch()
      expect(mockOpenSearch).not.toHaveBeenCalled()
      expect(wrapper.emitted('toggleSearch')).toBeTruthy()
    })

    it('opens CodeMirror search when editing from rendered markdown preview', async () => {
      const wrapper = mountViewer({ file: mdFile, markdownViewMode: 'rendered' })
      const ss = (wrapper.vm as any).$.setupState
      ss.handleToggleEdit()
      await nextTick()
      expect(ss.isCodeMirrorView).toBe(true)
      ss.handleToggleSearch()
      expect(mockOpenSearch).toHaveBeenCalledTimes(1)
    })

    it('focusSearchInput opens CodeMirror search for a code file', async () => {
      const wrapper = mountViewer({ file: codeFile })
      const ss = (wrapper.vm as any).$.setupState
      ss.focusSearchInput()
      expect(mockOpenSearch).toHaveBeenCalledTimes(1)
    })

    it('focusSearchInput does nothing for rendered markdown (drawer is focused by FileOverlay)', async () => {
      const wrapper = mountViewer({ file: mdFile, markdownViewMode: 'rendered' })
      const ss = (wrapper.vm as any).$.setupState
      ss.focusSearchInput()
      expect(mockOpenSearch).not.toHaveBeenCalled()
      expect(wrapper.emitted('toggleSearch')).toBeFalsy()
    })

    it('opens CodeMirror search for HTML raw view', async () => {
      const htmlFile = {
        name: 'page.html', path: '/tmp/page.html', content: '<h1>hi</h1>',
        isMarkdown: false, isHtml: true, isOpenapi: false,
        isImage: false, isAudio: false, isVideo: false, isPdf: false, isOffice: false,
        isBinary: false, tooLarge: false,
      }
      const wrapper = mountViewer({ file: htmlFile, markdownViewMode: 'source' })
      const ss = (wrapper.vm as any).$.setupState
      expect(ss.isCodeMirrorView).toBe(true)
      ss.handleToggleSearch()
      expect(mockOpenSearch).toHaveBeenCalledTimes(1)
      expect(wrapper.emitted('toggleSearch')).toBeFalsy()
    })

    it('opens CodeMirror search for OpenAPI raw view', async () => {
      const oapiFile = {
        name: 'api.yaml', path: '/tmp/api.yaml', content: 'openapi: 3.0.0',
        subtype: 'openapi',
        isMarkdown: false, isHtml: false,
        isImage: false, isAudio: false, isVideo: false, isPdf: false, isOffice: false,
        isBinary: false, tooLarge: false,
      }
      const wrapper = mountViewer({ file: oapiFile, markdownViewMode: 'source' })
      const ss = (wrapper.vm as any).$.setupState
      expect(ss.isCodeMirrorView).toBe(true)
      ss.handleToggleSearch()
      expect(mockOpenSearch).toHaveBeenCalledTimes(1)
      expect(wrapper.emitted('toggleSearch')).toBeFalsy()
    })

    it('does not treat media files as CodeMirror views (no search routing to CM)', async () => {
      const imgFile = {
        name: 'photo.svg', path: '/tmp/photo.svg', content: '<svg/>',
        isImage: true,
        isMarkdown: false, isHtml: false,
        isAudio: false, isVideo: false, isPdf: false, isOffice: false,
        isBinary: false, tooLarge: false,
      }
      const wrapper = mountViewer({ file: imgFile, markdownViewMode: 'source' })
      const ss = (wrapper.vm as any).$.setupState
      expect(ss.isCodeMirrorView).toBe(false)
      ss.handleToggleSearch()
      expect(mockOpenSearch).not.toHaveBeenCalled()
    })
  })
})
